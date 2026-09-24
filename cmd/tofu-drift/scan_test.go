package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/wardbox/tofu-drift/internal/scan"
)

type fakeScanner []scan.LiveResource

func (f fakeScanner) List(context.Context) ([]scan.LiveResource, error) { return f, nil }
func (fakeScanner) Permissions() []string                               { return nil }

// Tests never reach AWS: credentials always present, no live resources unless
// a test stubs some.
func init() {
	retrieveCredentials = func(context.Context, aws.Config) error { return nil }
	stubbed(fakeScanner(nil))
}

func stubbed(ss ...scan.Scanner) {
	newScanners = func(aws.Config) []scan.Scanner { return ss }
}

// stubScanners makes the scan see live for the duration of a test.
func stubScanners(t *testing.T, live ...scan.LiveResource) {
	t.Helper()
	stubbed(fakeScanner(live))
	t.Cleanup(func() { stubbed(fakeScanner(nil)) })
}

func runScan(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out, _, err := runScanStderr(t, "123456789012", args...)
	return out, err
}

// runScanStderr runs scan in us-east-1 with STS stubbed to return account.
func runScanStderr(t *testing.T, account string, args ...string) (string, string, error) {
	t.Helper()
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_CONFIG_FILE", "/nonexistent")
	orig := callerAccount
	callerAccount = func(context.Context, aws.Config) (string, error) { return account, nil }
	t.Cleanup(func() { callerAccount = orig })
	cmd := scanCmd()
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), stderr.String(), err
}

func TestScanTable(t *testing.T) {
	stubScanners(t,
		scan.LiveResource{Type: "aws_ebs_volume", Key: "vol-0aaa", Class: "gp3", SizeGB: 500}, // managed, in use
		scan.LiveResource{Type: "aws_ebs_volume", Key: "vol-stray", Name: "scratch", Class: "gp3", SizeGB: 100, Idle: "unattached"},
		scan.LiveResource{Type: "aws_instance", Key: "i-stray", Class: "t3.large"},
	)
	out, err := runScan(t, "--state", "../../internal/state/testdata/v4.tfstate")
	if !errors.Is(err, errFindings) {
		t.Fatalf("want errFindings, got %v", err)
	}
	if strings.Contains(out, "vol-0aaa") {
		t.Errorf("managed in-use volume reported:\n%s", out)
	}
	for _, want := range []string{"vol-stray", "scratch", "unmanaged+idle", "8.00",
		"i-stray", "60.74", "1.3", "Unmanaged: $69/mo · Idle: $8/mo"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestScanJSON(t *testing.T) {
	stubScanners(t, scan.LiveResource{Type: "aws_ebs_volume", Key: "vol-stray", Class: "gp3", SizeGB: 100, Idle: "unattached"})
	out, err := runScan(t, "--state", "../../internal/state/testdata/v4.tfstate", "--json")
	if !errors.Is(err, errFindings) {
		t.Fatalf("want errFindings, got %v", err)
	}
	var got struct {
		Schema   int               `json:"schema"`
		Scan     map[string]string `json:"scan"`
		Findings []map[string]any  `json:"findings"`
		Totals   map[string]float64
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out)
	}
	if got.Schema != 1 || len(got.Findings) != 1 || got.Findings[0]["id"] != "vol-stray" || got.Findings[0]["idle"] != "unattached" ||
		got.Scan["state_source"] == "" || got.Scan["region"] != "us-east-1" {
		t.Errorf("unexpected report: %s", out)
	}
	for _, k := range []string{"unmanaged_usd_mo", "idle_usd_mo", "kgco2_mo"} {
		if _, ok := got.Totals[k]; !ok {
			t.Errorf("totals missing %s", k)
		}
	}
}

func TestScanLocationNotices(t *testing.T) {
	_, stderr, err := runScanStderr(t, "111111111111", "--state", "testdata/regions.tfstate")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"notice: state references resources in regions not scanned (scanning us-east-1): ap-south-1 (1), eu-west-1 (2)",
		"warning: state ARNs belong to account 222222222222 (1), but credentials are for account 111111111111",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q:\n%s", want, stderr)
		}
	}

	_, stderr, err = runScanStderr(t, "", "--state", "../../internal/state/testdata/v4.tfstate")
	if want := "notice: --state given, skipping drift detection (no root module to plan against)\n"; err != nil || stderr != want {
		t.Errorf("same region, unknown caller: want only the drift notice, got err %v stderr %q", err, stderr)
	}
}

// inModule runs the test in a root module whose tofu binary is faked: state
// pull returns the v4 fixture, plan succeeds unless planErr is set, and show
// returns the recorded plan.
func inModule(t *testing.T, planErr error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	stateJSON, _ := os.ReadFile("../../internal/state/testdata/v4.tfstate")
	showJSON, _ := os.ReadFile("../../internal/plan/testdata/show.json")
	origLook, origRun := lookPath, runTool
	lookPath = func(name string) (string, error) {
		if name == "tofu" {
			return "/bin/tofu", nil
		}
		return "", exec.ErrNotFound
	}
	runTool = func(_ context.Context, _, _ string, args ...string) ([]byte, error) {
		switch args[0] {
		case "state":
			return stateJSON, nil
		case "plan":
			return nil, planErr
		}
		return showJSON, nil
	}
	t.Cleanup(func() { lookPath, runTool = origLook, origRun })
	t.Chdir(dir)
}

func TestScanDrift(t *testing.T) {
	inModule(t, nil)
	out, err := runScan(t)
	if !errors.Is(err, errFindings) {
		t.Fatalf("drift alone must exit 1, got %v", err)
	}
	for _, want := range []string{
		"aws_db_instance.main    aws_db_instance       3 (instance_class, password, tags)",
		"local_sensitive_file.s  local_sensitive_file  deleted",
		"Unmanaged: $0/mo · Idle: $0/mo",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	out, err = runScan(t, "--explain", "aws_db_instance.main")
	if !errors.Is(err, errFindings) || !strings.Contains(out, `+ "db.t3.large"`) || strings.Contains(out, "hunter") {
		t.Errorf("explain: err %v\n%s", err, out)
	}
	if _, err := runScan(t, "--explain", "aws_instance.nope"); err == nil || errors.Is(err, errFindings) {
		t.Errorf("explain unknown address must error, got %v", err)
	}
}

func TestScanPlanFailure(t *testing.T) {
	inModule(t, errors.New("exit status 1: Error: Backend initialization required"))
	_, err := runScan(t)
	if err == nil || errors.Is(err, errFindings) || !strings.Contains(err.Error(), "Backend initialization required") {
		t.Errorf("plan failure must be an error carrying stderr, got %v", err)
	}
}

func TestScanErrors(t *testing.T) {
	if _, err := runScan(t); err == nil || !strings.Contains(err.Error(), "no .tf files") {
		t.Errorf("no .tf files and no --state should error with guidance, got %v", err)
	}
	if _, err := runScan(t, "--state", "https://example.com/state"); err == nil {
		t.Error("non-s3 URL should error")
	}
	if _, err := runScan(t, "--state", "/nonexistent"); err == nil {
		t.Error("missing file should error")
	}
	_, err := runScan(t, "--state", "testdata/v3.tfstate")
	if err == nil || errors.Is(err, errFindings) {
		t.Errorf("v3 state should be an error, got %v", err)
	}
}

func TestScanDefaultFurniture(t *testing.T) {
	stubScanners(t,
		scan.LiveResource{Type: "aws_security_group", Key: "sg-default", Default: true},
		scan.LiveResource{Type: "aws_security_group", Key: "sg-stray", Name: "old-web"},
	)
	out, err := runScan(t, "--state", "../../internal/state/testdata/v4.tfstate")
	if !errors.Is(err, errFindings) {
		t.Fatalf("want errFindings, got %v", err)
	}
	if strings.Contains(out, "sg-default") || !strings.Contains(out, "sg-stray") {
		t.Errorf("default SG must be hidden, stray SG shown:\n%s", out)
	}

	out, err = runScan(t, "--state", "../../internal/state/testdata/v4.tfstate", "--include-defaults")
	if !errors.Is(err, errFindings) || !strings.Contains(out, "sg-default") {
		t.Errorf("--include-defaults must show the default SG:\n%s", out)
	}
}
