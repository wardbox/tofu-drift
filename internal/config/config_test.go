package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wardbox/tofu-drift/internal/scan"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "tofu-drift.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestIgnored(t *testing.T) {
	c, err := Load(write(t, `
[[ignore]]
tag = "ManagedBy=other"

[[ignore]]
type = "aws_s3_bucket"

[[ignore]]
arn = "arn:aws:iam::*:role/legacy-*"

[[ignore]]
type = "aws_ebs_volume"
tag = "Team=data"
`), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		l    scan.LiveResource
		want bool
	}{
		{scan.LiveResource{Type: "aws_instance", Tags: map[string]string{"ManagedBy": "other"}}, true},
		{scan.LiveResource{Type: "aws_instance", Tags: map[string]string{"ManagedBy": "tofu"}}, false},
		{scan.LiveResource{Type: "aws_s3_bucket"}, true},
		{scan.LiveResource{Type: "aws_iam_role", ARN: "arn:aws:iam::123456789012:role/legacy-app"}, true},
		{scan.LiveResource{Type: "aws_iam_role", ARN: "arn:aws:iam::123456789012:role/app"}, false},
		{scan.LiveResource{Type: "aws_instance"}, false},
		// every field of one rule must match
		{scan.LiveResource{Type: "aws_ebs_volume", Tags: map[string]string{"Team": "data"}}, true},
		{scan.LiveResource{Type: "aws_instance", Tags: map[string]string{"Team": "data"}}, false},
		{scan.LiveResource{Type: "aws_ebs_volume"}, false},
	} {
		if got := c.Ignored(tc.l); got != tc.want {
			t.Errorf("Ignored(%+v) = %v, want %v", tc.l, got, tc.want)
		}
	}
}

func TestLoadMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "tofu-drift.toml")
	c, err := Load(missing, false)
	if err != nil || len(c.Ignore) != 0 {
		t.Errorf("optional missing file: %+v, %v", c, err)
	}
	if _, err := Load(missing, true); err == nil {
		t.Error("required missing file must error")
	}
}

func TestLoadMalformed(t *testing.T) {
	for _, body := range []string{
		"[[ignore]\n",                           // bad TOML
		"[[ignore]]\ntag = \"ManagedBy\"\n",     // no =
		"[[ignore]]\narn = \"arn:[\"\n",         // bad glob
		"[[ignore]]\n",                          // empty rule would ignore everything
		"[[ignor]]\ntype = \"aws_s3_bucket\"\n", // typo'd table
		"[[ignore]]\ntyp = \"aws_s3_bucket\"\n", // typo'd key
	} {
		if _, err := Load(write(t, body), false); err == nil {
			t.Errorf("want error for %q", body)
		}
	}
}
