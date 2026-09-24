package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/wardbox/tofu-drift/internal/scan"
)

const v4State = "../../internal/state/testdata/v4.tfstate"

func TestUnmanaged(t *testing.T) {
	old := time.Now().AddDate(-1, 0, 0)
	stubScanners(t,
		scan.LiveResource{Type: "aws_instance", Key: "i-stray", Class: "t3.large"},
		scan.LiveResource{Type: "aws_ebs_volume", Key: "vol-stray", Class: "gp3", SizeGB: 100, Idle: "unattached", Created: &old},
	)
	out, _, err := runCmd(t, unmanagedCmd(), "", "--state", v4State, "--sort", "age")
	if !errors.Is(err, errFindings) {
		t.Fatalf("want errFindings, got %v", err)
	}
	if strings.Contains(out, "Drift") || strings.Index(out, "vol-stray") > strings.Index(out, "i-stray") {
		t.Errorf("want no Drift section, oldest first:\n%s", out)
	}
	if out, _, _ = runCmd(t, unmanagedCmd(), "", "--state", v4State); strings.Index(out, "i-stray") > strings.Index(out, "vol-stray") {
		t.Errorf("default sort is cost:\n%s", out)
	}
	if out, _, _ = runCmd(t, unmanagedCmd(), "", "--state", v4State, "--json"); !strings.Contains(out, `"id": "vol-stray"`) || !strings.Contains(out, `"drift_checked": false`) {
		t.Errorf("--json:\n%s", out)
	}
	if _, _, err := runCmd(t, unmanagedCmd(), "", "--state", v4State, "--sort", "size"); err == nil || errors.Is(err, errFindings) {
		t.Errorf("unknown sort must be an error, got %v", err)
	}
}

func TestUnmanagedIgnoreRules(t *testing.T) {
	stubScanners(t,
		scan.LiveResource{Type: "aws_ebs_volume", Key: "vol-other", Idle: "unattached", Tags: map[string]string{"ManagedBy": "other"}},
		scan.LiveResource{Type: "aws_instance", Key: "i-stray"},
	)
	cfg := filepath.Join(t.TempDir(), "custom.toml")
	if err := os.WriteFile(cfg, []byte("[[ignore]]\ntag = \"ManagedBy=other\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, _ := runCmd(t, unmanagedCmd(), "", "--state", v4State, "--config", cfg)
	if strings.Contains(out, "vol-other") || !strings.Contains(out, "i-stray") {
		t.Errorf("unmanaged must honor Ignore Rules:\n%s", out)
	}
	for _, c := range []*cobra.Command{unmanagedCmd(), importGenCmd()} {
		flag := map[string]string{"unmanaged": "--explain", "import-gen": "--ids"}[c.Name()]
		if _, _, err := runCmd(t, c, "", "--state", v4State, "--config", cfg, flag, "vol-other"); err == nil || errors.Is(err, errFindings) {
			t.Errorf("%s %s of an ignored resource must be an error, got %v", c.Name(), flag, err)
		}
	}
}

func TestUnmanagedSkipsPlan(t *testing.T) {
	inModule(t, errors.New("plan must not run"))
	out, _, err := runCmd(t, unmanagedCmd(), "")
	if err != nil || strings.Contains(out, "Drift") {
		t.Errorf("unmanaged must not plan or show Drift: %v\n%s", err, out)
	}
}

func TestExplainFinding(t *testing.T) {
	stubScanners(t,
		scan.LiveResource{Type: "aws_ebs_volume", Key: "vol-0aaa", Class: "gp3", SizeGB: 500}, // managed, in use
		scan.LiveResource{Type: "aws_ebs_volume", Key: "vol-stray", Class: "gp3", SizeGB: 100, Idle: "unattached"},
	)
	for _, cmd := range []string{"scan", "unmanaged"} {
		c := scanCmd()
		if cmd == "unmanaged" {
			c = unmanagedCmd()
		}
		out, _, err := runCmd(t, c, "", "--state", v4State, "--explain", "vol-stray")
		if !errors.Is(err, errFindings) || !strings.Contains(out, "idle:     unattached") ||
			!strings.Contains(out, "gp3 100 GB × $0.08/GB-mo = $8.00/mo") || !strings.Contains(out, "carbon:") {
			t.Errorf("%s --explain: %v\n%s", cmd, err, out)
		}
	}
	for _, id := range []string{"vol-0aaa", "nope"} {
		if _, _, err := runCmd(t, unmanagedCmd(), "", "--state", v4State, "--explain", id); err == nil || errors.Is(err, errFindings) {
			t.Errorf("--explain %s must be an error (exit 2), got %v", id, err)
		}
	}
}

func TestImportGen(t *testing.T) {
	stubScanners(t,
		scan.LiveResource{Type: "aws_ebs_volume", Key: "vol-0aaa"}, // managed
		scan.LiveResource{Type: "aws_ebs_volume", Key: "vol-stray", Name: "Scratch Disk", Idle: "unattached"},
		scan.LiveResource{Type: "aws_instance", Key: "i-stray"},
	)
	out, _, err := runCmd(t, importGenCmd(), "", "--state", v4State, "--ids", "vol-stray,i-stray")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"rename them before apply", "to = aws_ebs_volume.scratch_disk\n  id = \"vol-stray\"", "to = aws_instance.i-stray"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	for _, args := range [][]string{{"--ids", "vol-0aaa"}, {"--ids", "nope"}, {}} {
		if _, _, err := runCmd(t, importGenCmd(), "", append([]string{"--state", v4State}, args...)...); err == nil {
			t.Errorf("%v must be an error", args)
		}
	}

	// The output is well-formed, canonically formatted HCL.
	tofu, err := exec.LookPath("tofu")
	if err != nil {
		t.Skip("tofu not on PATH")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "imports.tf"), []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := exec.Command(tofu, "-chdir="+dir, "fmt", "-check", "-diff").CombinedOutput(); err != nil {
		t.Errorf("tofu fmt rejected the output: %v\n%s", err, b)
	}
}
