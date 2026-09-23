package main

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/spf13/cobra"
	"github.com/wardbox/tofu-drift/internal/report"
	"github.com/wardbox/tofu-drift/internal/state"
)

// callerAccount returns the STS caller's account id. Swapped in tests.
var callerAccount = func(ctx context.Context, cfg aws.Config) (string, error) {
	out, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", err
	}
	return aws.ToString(out.Account), nil
}

func scanCmd() *cobra.Command {
	var statePath, region, profile string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Report drift, unmanaged and idle resources",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			var opts []func(*config.LoadOptions) error
			if region != "" {
				opts = append(opts, config.WithRegion(region))
			}
			if profile != "" {
				opts = append(opts, config.WithSharedConfigProfile(profile))
			}
			cfg, err := config.LoadDefaultConfig(ctx, opts...)
			if err != nil {
				return err
			}
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			src := &state.Source{
				Dir:      dir,
				LookPath: exec.LookPath,
				Run:      state.RunCommand,
				GetS3: func(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
					out, err := s3.NewFromConfig(cfg).GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &key})
					if err != nil {
						return nil, err
					}
					return out.Body, nil
				},
			}
			managed, source, err := src.Load(ctx, statePath)
			if err != nil {
				return err
			}

			scan := report.Scan{Region: cfg.Region, StateSource: source}
			regions, accounts := state.Locations(managed)
			if n := others(regions, scan.Region); scan.Region != "" && n != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "notice: state references resources in regions not scanned (scanning %s): %s\n", scan.Region, n)
			}
			// Only look up the caller when state has accounts to compare. A
			// failed lookup skips the check; missing credentials surface later.
			if len(accounts) > 0 {
				scan.Account, _ = callerAccount(ctx, cfg)
				if n := others(accounts, scan.Account); scan.Account != "" && n != "" {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: state ARNs belong to account %s, but credentials are for account %s\n", n, scan.Account)
				}
			}

			r := report.New(scan, managed)
			out := cmd.OutOrStdout()
			if asJSON {
				err = r.WriteJSON(out)
			} else {
				err = r.WriteTable(out)
			}
			if err != nil {
				return err
			}
			if len(r.Findings) > 0 {
				return errFindings
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&statePath, "state", "", "state file: local path or s3://bucket/key (default: tofu state pull in the current root module)")
	cmd.Flags().StringVar(&region, "region", "", "AWS region to scan (default: from environment or profile)")
	cmd.Flags().StringVar(&profile, "profile", "", "AWS shared config profile")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a table")
	return cmd
}

// others formats the counts for every key except skip, sorted, as
// "k1 (n), k2 (n)"; empty when there are none.
func others(counts map[string]int, skip string) string {
	var parts []string
	for _, k := range slices.Sorted(maps.Keys(counts)) {
		if k != skip {
			parts = append(parts, fmt.Sprintf("%s (%d)", k, counts[k]))
		}
	}
	return strings.Join(parts, ", ")
}
