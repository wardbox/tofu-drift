package state

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Source resolves where state comes from: an explicit --state value (local
// path or s3://bucket/key), or `tofu state pull` in a root module.
type Source struct {
	// Dir is the directory checked for .tf files and used to run state pull.
	Dir      string
	LookPath func(file string) (string, error)
	Run      func(ctx context.Context, dir, bin string, args ...string) ([]byte, error)
	GetS3    func(ctx context.Context, bucket, key string) (io.ReadCloser, error)
}

// RunCommand runs bin in dir and returns its stdout, folding stderr into the
// error on failure.
func RunCommand(ctx context.Context, dir, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) && len(ee.Stderr) > 0 {
		err = fmt.Errorf("%w: %s", err, bytes.TrimSpace(ee.Stderr))
	}
	return out, err
}

// Load reads and parses state. flag is the --state value; empty means
// auto-detect. It also returns a human-readable description of the source.
func (s *Source) Load(ctx context.Context, flag string) ([]Resource, string, error) {
	switch {
	case flag == "":
		return s.pull(ctx)
	case strings.HasPrefix(flag, "s3://"):
		bucket, key, _ := strings.Cut(strings.TrimPrefix(flag, "s3://"), "/")
		if bucket == "" || key == "" {
			return nil, "", fmt.Errorf("--state %q: want s3://bucket/key", flag)
		}
		body, err := s.GetS3(ctx, bucket, key)
		if err != nil {
			return nil, "", fmt.Errorf("reading %s: %w", flag, err)
		}
		defer func() { _ = body.Close() }()
		rs, err := Parse(body)
		return rs, flag, err
	case strings.Contains(flag, "://"):
		return nil, "", fmt.Errorf("unsupported --state %q: pass a local path or s3://bucket/key", flag)
	default:
		f, err := os.Open(flag)
		if err != nil {
			return nil, "", err
		}
		defer f.Close()
		rs, err := Parse(f)
		return rs, flag, err
	}
}

func (s *Source) pull(ctx context.Context) ([]Resource, string, error) {
	tf, _ := filepath.Glob(filepath.Join(s.Dir, "*.tf"))
	tfJSON, _ := filepath.Glob(filepath.Join(s.Dir, "*.tf.json"))
	if len(tf)+len(tfJSON) == 0 {
		return nil, "", errors.New("no .tf files in the current directory: run inside a root module, or pass --state <path|s3://bucket/key>")
	}
	for _, name := range []string{"tofu", "terraform"} {
		bin, err := s.LookPath(name)
		if err != nil {
			continue
		}
		out, err := s.Run(ctx, s.Dir, bin, "state", "pull", "-no-color")
		if err != nil {
			return nil, "", fmt.Errorf("%s state pull: %w", name, err)
		}
		if len(bytes.TrimSpace(out)) == 0 {
			return nil, "", fmt.Errorf("%s state pull returned empty state: apply the module first, or pass --state", name)
		}
		rs, err := Parse(bytes.NewReader(out))
		return rs, name + " state pull", err
	}
	return nil, "", errors.New("neither tofu nor terraform found on PATH: install one, or pass --state <path|s3://bucket/key>")
}

// Locations counts resources per region and per account, read from each
// resource's ARN (falling back to a region attribute). Global ARNs such as
// IAM or S3 contribute no region; S3 ARNs contribute no account.
func Locations(rs []Resource) (regions, accounts map[string]int) {
	regions, accounts = map[string]int{}, map[string]int{}
	for _, r := range rs {
		region, _ := r.Attributes["region"].(string)
		if arn, ok := r.Attributes["arn"].(string); ok {
			// arn:partition:service:region:account:resource
			if p := strings.SplitN(arn, ":", 6); len(p) == 6 {
				region = p[3]
				if p[4] != "" {
					accounts[p[4]]++
				}
			}
		}
		if region != "" {
			regions[region]++
		}
	}
	return regions, accounts
}
