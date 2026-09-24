package state

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var v4, _ = os.ReadFile("testdata/v4.tfstate")

func TestRunCommandStderrTail(t *testing.T) {
	_, err := RunCommand(context.Background(), t.TempDir(), "sh", "-c", "for i in $(seq 1 25); do echo line$i >&2; done; exit 1")
	if err == nil || strings.Contains(err.Error(), "line5\n") || !strings.Contains(err.Error(), "exit status 1: line6\n") ||
		!strings.HasSuffix(err.Error(), "line25") {
		t.Errorf("want last 20 stderr lines, got %v", err)
	}
}

// stubSource returns a Source whose PATH holds only bins and whose runner
// records the invocation and returns out.
func stubSource(t *testing.T, tf bool, bins []string, out []byte) (*Source, *[]string) {
	t.Helper()
	dir := t.TempDir()
	if tf {
		if err := os.WriteFile(filepath.Join(dir, "main.tf"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var ran []string
	return &Source{
		Dir: dir,
		LookPath: func(name string) (string, error) {
			for _, b := range bins {
				if b == name {
					return "/bin/" + name, nil
				}
			}
			return "", errors.New("not found")
		},
		Run: func(_ context.Context, d, bin string, args ...string) ([]byte, error) {
			if d != dir {
				t.Errorf("ran in %q, want %q", d, dir)
			}
			ran = append([]string{bin}, args...)
			return out, nil
		},
		GetS3: func(context.Context, string, string) (io.ReadCloser, error) {
			t.Error("unexpected S3 read")
			return nil, nil
		},
	}, &ran
}

func TestLoadStatePull(t *testing.T) {
	for _, tc := range []struct {
		bins []string
		want string
	}{
		{[]string{"tofu", "terraform"}, "tofu"},
		{[]string{"terraform"}, "terraform"},
	} {
		s, ran := stubSource(t, true, tc.bins, v4)
		rs, src, err := s.Load(context.Background(), "")
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"/bin/" + tc.want, "state", "pull", "-no-color"}; !reflect.DeepEqual(*ran, want) {
			t.Errorf("ran %v, want %v", *ran, want)
		}
		if src != tc.want+" state pull" || len(rs) != 5 {
			t.Errorf("got source %q, %d resources", src, len(rs))
		}
	}
}

func TestLoadErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		tf   bool
		bins []string
		out  string
		flag string
		want string
	}{
		"no tf files":    {false, []string{"tofu"}, "", "", "no .tf files"},
		"no binary":      {true, nil, "", "", "neither tofu nor terraform"},
		"empty state":    {true, []string{"tofu"}, "\n", "", "empty state"},
		"other scheme":   {false, nil, "", "gs://b/k", "unsupported --state"},
		"s3 without key": {false, nil, "", "s3://bucket", "s3://bucket/key"},
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := stubSource(t, tc.tf, tc.bins, []byte(tc.out))
			_, _, err := s.Load(context.Background(), tc.flag)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want error containing %q", err, tc.want)
			}
		})
	}
}

func TestLoadRunFailure(t *testing.T) {
	s, _ := stubSource(t, true, []string{"tofu"}, nil)
	s.Run = func(context.Context, string, string, ...string) ([]byte, error) {
		return nil, errors.New("backend not initialised")
	}
	if _, _, err := s.Load(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "backend not initialised") {
		t.Errorf("got %v", err)
	}
}

func TestLoadS3(t *testing.T) {
	s, ran := stubSource(t, true, []string{"tofu"}, nil)
	s.GetS3 = func(_ context.Context, bucket, key string) (io.ReadCloser, error) {
		if bucket != "my-bucket" || key != "env/prod/terraform.tfstate" {
			t.Errorf("got bucket %q key %q", bucket, key)
		}
		return io.NopCloser(strings.NewReader(string(v4))), nil
	}
	rs, src, err := s.Load(context.Background(), "s3://my-bucket/env/prod/terraform.tfstate")
	if err != nil {
		t.Fatal(err)
	}
	if *ran != nil || src != "s3://my-bucket/env/prod/terraform.tfstate" || len(rs) != 5 {
		t.Errorf("ran %v, source %q, %d resources", *ran, src, len(rs))
	}
}

func TestLoadLocal(t *testing.T) {
	s, ran := stubSource(t, true, []string{"tofu"}, nil)
	rs, src, err := s.Load(context.Background(), "testdata/v4.tfstate")
	if err != nil || *ran != nil || src != "testdata/v4.tfstate" || len(rs) != 5 {
		t.Errorf("err %v, ran %v, source %q, %d resources", err, *ran, src, len(rs))
	}
}

func TestLocations(t *testing.T) {
	rs := []Resource{
		{Attributes: map[string]any{"arn": "arn:aws:ec2:us-east-1:111111111111:instance/i-1"}},
		{Attributes: map[string]any{"arn": "arn:aws:ec2:eu-west-1:111111111111:volume/vol-1"}},
		{Attributes: map[string]any{"arn": "arn:aws:ec2:eu-west-1:222222222222:volume/vol-2"}},
		{Attributes: map[string]any{"arn": "arn:aws:iam::111111111111:role/r"}},
		{Attributes: map[string]any{"arn": "arn:aws:s3:::bucket"}},
		{Attributes: map[string]any{"region": "ap-south-1", "id": "x"}},
		{Attributes: map[string]any{"id": "subnet-0"}},
	}
	regions, accounts := Locations(rs)
	if want := map[string]int{"us-east-1": 1, "eu-west-1": 2, "ap-south-1": 1}; !reflect.DeepEqual(regions, want) {
		t.Errorf("regions %v, want %v", regions, want)
	}
	if want := map[string]int{"111111111111": 3, "222222222222": 1}; !reflect.DeepEqual(accounts, want) {
		t.Errorf("accounts %v, want %v", accounts, want)
	}
}
