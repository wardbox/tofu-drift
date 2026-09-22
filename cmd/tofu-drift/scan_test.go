package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func runScan(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := scanCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
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

func TestScanErrors(t *testing.T) {
	if _, err := runScan(t); err == nil {
		t.Error("missing --state should error")
	}
	if _, err := runScan(t, "--state", "/nonexistent"); err == nil {
		t.Error("missing file should error")
	}
	_, err := runScan(t, "--state", "testdata/v3.tfstate")
	if err == nil || errors.Is(err, errFindings) {
		t.Errorf("v3 state should be an error, got %v", err)
	}
}
