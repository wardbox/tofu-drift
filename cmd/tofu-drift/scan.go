package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	elb "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/spf13/cobra"
	tofudrift "github.com/wardbox/tofu-drift"
	"github.com/wardbox/tofu-drift/internal/match"
	"github.com/wardbox/tofu-drift/internal/plan"
	"github.com/wardbox/tofu-drift/internal/report"
	"github.com/wardbox/tofu-drift/internal/scan"
	"github.com/wardbox/tofu-drift/internal/state"
	"golang.org/x/sync/errgroup"
)

// lookPath and runTool find and run the tofu/terraform binary. Swapped in tests.
var (
	lookPath = exec.LookPath
	runTool  = state.RunCommand
)

// newScanners builds the covered-service scanners for cfg's region. Swapped in tests.
var newScanners = func(cfg aws.Config) []scan.Scanner {
	client := ec2.NewFromConfig(cfg)
	return []scan.Scanner{
		scan.EBS{Client: client},
		scan.EC2{Client: client},
		scan.VPCs{Client: client},
		scan.Subnets{Client: client},
		scan.RouteTables{Client: client},
		scan.SecurityGroups{Client: client},
		scan.EIPs{Client: client},
		scan.NATGateways{Client: client},
		scan.ENIs{Client: client},
		scan.Snapshots{Client: client},
		scan.AMIs{Client: client},
		scan.LoadBalancers{Client: elbv2.NewFromConfig(cfg)},
		scan.ClassicLoadBalancers{Client: elb.NewFromConfig(cfg)},
	}
}

// scannerTimeout bounds each scanner's List. Swapped in tests.
var scannerTimeout = 30 * time.Second

// listAll runs every scanner, at most 8 at once, and concatenates the results
// in scanner order. A scanner that fails or times out is skipped with a notice
// on w; it is an error only when every scanner fails.
func listAll(ctx context.Context, w io.Writer, scanners []scan.Scanner) ([]scan.LiveResource, error) {
	results := make([][]scan.LiveResource, len(scanners))
	notices := make([]string, len(scanners))
	var g errgroup.Group
	g.SetLimit(8)
	for i, s := range scanners {
		g.Go(func() error {
			ctx, cancel := context.WithTimeout(ctx, scannerTimeout)
			defer cancel()
			rs, err := s.List(ctx)
			if err == nil {
				results[i] = rs
				return nil
			}
			if ctx.Err() == context.DeadlineExceeded {
				err = fmt.Errorf("timed out after %s", scannerTimeout)
			}
			notices[i] = fmt.Sprintf("notice: skipped %T (needs %s): %v\n", s, strings.Join(s.Permissions(), ", "), err)
			return nil
		})
	}
	_ = g.Wait()
	var all []scan.LiveResource
	failed := 0
	for i, rs := range results {
		all = append(all, rs...)
		if notices[i] != "" {
			fmt.Fprint(w, notices[i])
			failed++
		}
	}
	if failed > 0 && failed == len(scanners) {
		return nil, errors.New("every scanner failed, see notices above")
	}
	return all, nil
}

// retrieveCredentials fails when cfg has no usable AWS credentials. Swapped in tests.
var retrieveCredentials = func(ctx context.Context, cfg aws.Config) error {
	_, err := cfg.Credentials.Retrieve(ctx)
	return err
}

// callerAccount returns the STS caller's account id. Swapped in tests.
var callerAccount = func(ctx context.Context, cfg aws.Config) (string, error) {
	out, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", err
	}
	return aws.ToString(out.Account), nil
}

func scanCmd() *cobra.Command {
	var statePath, region, profile, explain string
	var asJSON, includeDefaults bool
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Report drift, unmanaged and idle resources",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
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
			if err := retrieveCredentials(ctx, cfg); err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "tofu-drift needs read-only AWS credentials. Attach this IAM policy (iam-policy.json) to the role or user you scan with (add s3:GetObject on the state object to read s3:// state):")
				fmt.Fprint(cmd.OutOrStdout(), string(tofudrift.IAMPolicy))
				return fmt.Errorf("no AWS credentials: %w", err)
			}
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			src := &state.Source{
				Dir:      dir,
				LookPath: lookPath,
				Run:      runTool,
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

			if cfg.Region == "" {
				return fmt.Errorf("no AWS region: pass --region or set AWS_REGION")
			}
			meta := report.Scan{Region: cfg.Region, StateSource: source}
			regions, accounts := state.Locations(managed)
			if list := countsExcept(regions, meta.Region); meta.Region != "" && list != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "notice: state references resources in regions not scanned (scanning %s): %s\n", meta.Region, list)
			}
			// Only look up the caller when state has accounts to compare.
			if len(accounts) > 0 {
				meta.Account, err = callerAccount(ctx, cfg)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "notice: skipping state account check, caller identity unavailable: %v\n", err)
				} else if list := countsExcept(accounts, meta.Account); meta.Account != "" && list != "" {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: state ARNs belong to account %s, but credentials are for account %s\n", list, meta.Account)
				}
			}

			var drifts []plan.Drift
			if statePath != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), "notice: --state given, skipping drift detection (no root module to plan against)")
			} else {
				// state pull above already required .tf files and a binary.
				drifts, err = (&plan.Runner{Dir: dir, LookPath: lookPath, Run: runTool}).Drift(ctx)
				if err != nil {
					return err
				}
			}
			if explain != "" {
				i := slices.IndexFunc(drifts, func(d plan.Drift) bool { return d.Address == explain })
				if i < 0 {
					return fmt.Errorf("--explain %q: no Drift with that address", explain)
				}
				drifts[i].Explain(cmd.OutOrStdout())
				return errFindings
			}

			live, err := listAll(ctx, cmd.ErrOrStderr(), newScanners(cfg))
			if err != nil {
				return err
			}
			if !includeDefaults {
				live = slices.DeleteFunc(live, match.Furniture)
			}
			r := report.New(meta, managed)
			r.AddLive(live, time.Now())
			r.AddDrift(drifts)
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
	cmd.Flags().BoolVar(&includeDefaults, "include-defaults", false, "report Default Furniture: the default VPC and its subnets, main route tables, default security groups")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a table")
	cmd.Flags().StringVar(&explain, "explain", "", "print the attribute-level before/after for the Drift at this resource address")
	return cmd
}

// countsExcept formats the counts for every key except skip, sorted, as
// "k1 (n), k2 (n)"; empty when there are none.
func countsExcept(counts map[string]int, skip string) string {
	var parts []string
	for _, k := range slices.Sorted(maps.Keys(counts)) {
		if k != skip {
			parts = append(parts, fmt.Sprintf("%s (%d)", k, counts[k]))
		}
	}
	return strings.Join(parts, ", ")
}
