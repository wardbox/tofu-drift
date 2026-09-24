package plan

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

// testdata/show.json is `tofu show -json` of a recorded refresh-only plan
// (OpenTofu 1.12, local provider, file deleted out of band) with a hand-added
// aws_db_instance update carrying a sensitive attribute.
func loadShow(t *testing.T) []Drift {
	t.Helper()
	f, err := os.Open("testdata/show.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ds, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

func TestParse(t *testing.T) {
	ds := loadShow(t)
	if len(ds) != 2 {
		t.Fatalf("got %d drifts, want 2", len(ds))
	}
	db, file := ds[0], ds[1]
	if db.Address != "aws_db_instance.main" || db.Type != "aws_db_instance" || db.Deleted {
		t.Errorf("db: %+v", db)
	}
	if got := fmt.Sprint(db.Changed()); got != "[instance_class password tags]" {
		t.Errorf("db changed %s", got)
	}
	if file.Address != "local_sensitive_file.s" || !file.Deleted || file.Changed() != nil {
		t.Errorf("file: %+v", file)
	}
}

func TestExplain(t *testing.T) {
	ds := loadShow(t)
	var out bytes.Buffer
	ds[0].Explain(&out)
	want := `aws_db_instance.main (aws_db_instance): 3 attributes changed out of band
  instance_class
    - "db.t3.micro"
    + "db.t3.large"
  password
    - (sensitive)
    + (sensitive)
  tags
    - {"Name":"main"}
    + {"Name":"main","Owner":"bob"}
`
	if out.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", out.String(), want)
	}

	out.Reset()
	ds[1].Explain(&out)
	if s := out.String(); s != "local_sensitive_file.s (local_sensitive_file): deleted out of band\n" {
		t.Errorf("deleted explain: %q", s)
	}

	// marked on one side only: both sides masked
	out.Reset()
	Drift{Address: "x.y", Type: "x",
		Before: map[string]any{"token": "old-secret"}, After: map[string]any{"token": "new-secret"},
		BeforeSensitive: map[string]any{"token": true}, AfterSensitive: false}.Explain(&out)
	if strings.Contains(out.String(), "secret") {
		t.Errorf("one-sided sensitive leaked:\n%s", out.String())
	}
}

func TestRunner(t *testing.T) {
	show, _ := os.ReadFile("testdata/show.json")
	var ran [][]string
	r := &Runner{
		Dir:      "/mod",
		LookPath: func(name string) (string, error) { return "/bin/" + name, nil },
		Run: func(_ context.Context, dir, bin string, args ...string) ([]byte, error) {
			if dir != "/mod" {
				t.Errorf("ran in %q", dir)
			}
			ran = append(ran, append([]string{bin}, args...))
			if args[0] == "show" {
				return show, nil
			}
			return []byte("human plan output"), nil
		},
	}
	ds, err := r.Drift(context.Background())
	if err != nil || len(ds) != 2 {
		t.Fatalf("drift %v, err %v", ds, err)
	}
	if len(ran) != 2 {
		t.Fatalf("ran %v", ran)
	}
	plan, show2 := strings.Join(ran[0], " "), ran[1]
	planFile := show2[len(show2)-1]
	if want := "/bin/tofu plan -refresh-only -lock=false -input=false -no-color -out=" + planFile; plan != want {
		t.Errorf("plan ran %q, want %q", plan, want)
	}
	if _, err := os.Stat(planFile); !os.IsNotExist(err) {
		t.Errorf("plan file %s left behind", planFile)
	}

	r.Run = func(context.Context, string, string, ...string) ([]byte, error) {
		return nil, errors.New("exit status 1: Error: Backend initialization required")
	}
	if _, err := r.Drift(context.Background()); err == nil || !strings.Contains(err.Error(), "tofu plan -refresh-only: exit status 1: Error: Backend initialization required") {
		t.Errorf("plan failure: %v", err)
	}

	r.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	if _, err := r.Drift(context.Background()); err == nil || !strings.Contains(err.Error(), "neither tofu nor terraform") {
		t.Errorf("no binary: %v", err)
	}
}
