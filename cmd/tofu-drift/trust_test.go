package main

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	tofudrift "github.com/wardbox/tofu-drift"
	"github.com/wardbox/tofu-drift/internal/scan"
)

// realScanners is captured before init stubs newScanners.
var realScanners = newScanners

func TestIAMPolicyCoversScanners(t *testing.T) {
	var p struct {
		Statement []struct {
			Effect string
			Action []string
		}
	}
	if err := json.Unmarshal(tofudrift.IAMPolicy, &p); err != nil {
		t.Fatalf("iam-policy.json: %v", err)
	}
	var allowed []string
	for _, s := range p.Statement {
		if s.Effect == "Allow" {
			allowed = append(allowed, s.Action...)
		}
	}
	for _, s := range realScanners(aws.Config{}) {
		for _, a := range s.Permissions() {
			if !slices.Contains(allowed, a) {
				t.Errorf("%T needs %s, missing from iam-policy.json", s, a)
			}
		}
	}
	for _, a := range allowed {
		_, verb, _ := strings.Cut(a, ":")
		if !strings.HasPrefix(verb, "Describe") && !strings.HasPrefix(verb, "List") && !strings.HasPrefix(verb, "Get") {
			t.Errorf("iam-policy.json grants %s, not read-only", a)
		}
	}
}

func TestScanNoCredentials(t *testing.T) {
	orig := retrieveCredentials
	retrieveCredentials = func(context.Context, aws.Config) error { return errors.New("no EC2 IMDS role found") }
	t.Cleanup(func() { retrieveCredentials = orig })
	out, stderr, err := runScanStderr(t, "", "--state", "../../internal/state/testdata/v4.tfstate")
	if err == nil || errors.Is(err, errFindings) || !strings.Contains(err.Error(), "no EC2 IMDS role found") {
		t.Errorf("want credentials error, got %v", err)
	}
	if out != string(tofudrift.IAMPolicy) {
		t.Errorf("stdout must be the policy, got:\n%s", out)
	}
	if !strings.Contains(stderr, "read-only") {
		t.Errorf("stderr must explain the policy, got %q", stderr)
	}
}

type failingScanner struct{ err error }

func (f failingScanner) List(context.Context) ([]scan.LiveResource, error) { return nil, f.err }
func (failingScanner) Permissions() []string                               { return []string{"ec2:DescribeThings"} }

type hangingScanner struct{}

func (hangingScanner) List(ctx context.Context) ([]scan.LiveResource, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (hangingScanner) Permissions() []string { return nil }

// stubScannerList makes the scan run ss, each with a 10ms timeout.
func stubScannerList(t *testing.T, ss ...scan.Scanner) {
	t.Helper()
	newScanners = func(aws.Config) []scan.Scanner { return ss }
	origTimeout := scannerTimeout
	scannerTimeout = 10 * time.Millisecond
	t.Cleanup(func() { stubbed(nil); scannerTimeout = origTimeout })
}

func TestScanPartialFailure(t *testing.T) {
	stubScannerList(t,
		failingScanner{errors.New("api error UnauthorizedOperation: You are not authorized")},
		hangingScanner{},
		fakeScanner{{Type: "aws_ebs_volume", Key: "vol-stray", Class: "gp3", SizeGB: 100, Idle: "unattached"}},
	)
	out, stderr, err := runScanStderr(t, "", "--state", "../../internal/state/testdata/v4.tfstate")
	if !errors.Is(err, errFindings) || !strings.Contains(out, "vol-stray") {
		t.Fatalf("surviving scanner must still report, got %v:\n%s", err, out)
	}
	for _, want := range []string{
		"notice: skipped main.failingScanner (needs ec2:DescribeThings): api error UnauthorizedOperation",
		"notice: skipped main.hangingScanner: timed out after 10ms",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q:\n%s", want, stderr)
		}
	}
}

func TestScanAllScannersFail(t *testing.T) {
	stubScannerList(t, failingScanner{errors.New("AccessDenied")}, hangingScanner{})
	_, _, err := runScanStderr(t, "", "--state", "../../internal/state/testdata/v4.tfstate")
	if err == nil || errors.Is(err, errFindings) || !strings.Contains(err.Error(), "every scanner failed") {
		t.Errorf("want every-scanner-failed error, got %v", err)
	}
}
