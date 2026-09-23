package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
)

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
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), stderr.String(), err
}

func TestScanTable(t *testing.T) {
	out, err := runScan(t, "--state", "../../internal/state/testdata/v4.tfstate")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`module.storage.aws_ebs_volume.data["a"]`, "5 managed resources · 0 findings", "Unmanaged: $0/mo"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestScanJSON(t *testing.T) {
	out, err := runScan(t, "--state", "../../internal/state/testdata/v4.tfstate", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Schema   int               `json:"schema"`
		Scan     map[string]string `json:"scan"`
		Findings []any             `json:"findings"`
		Totals   map[string]float64
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out)
	}
	if got.Schema != 1 || got.Findings == nil || len(got.Findings) != 0 || got.Scan["state_source"] == "" {
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
	if err != nil || stderr != "" {
		t.Errorf("same region, unknown caller: want no notices, got err %v stderr %q", err, stderr)
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
