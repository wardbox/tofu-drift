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
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	elb "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/spf13/cobra"
	tofudrift "github.com/wardbox/tofu-drift"
	driftconfig "github.com/wardbox/tofu-drift/internal/config"
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
		scan.RDSInstances{Client: rds.NewFromConfig(cfg)},
		scan.RDSSnapshots{Client: rds.NewFromConfig(cfg)},
		scan.DynamoDBTables{Client: dynamodb.NewFromConfig(cfg)},
		scan.ElastiCache{Client: elasticache.NewFromConfig(cfg)},
		scan.S3Buckets{Client: s3.NewFromConfig(cfg)},
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

// pipeline is the Scan shared by scan, unmanaged and import-gen, which
// differ only in output.
type pipeline struct {
	statePath, region, profile, configPath string
	includeDefaults                        bool
}

func (p *pipeline) flags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&p.statePath, "state", "", "state file: local path or s3://bucket/key (default: tofu state pull in the current root module)")
	cmd.Flags().StringVar(&p.region, "region", "", "AWS region to scan (default: from environment or profile)")
	cmd.Flags().StringVar(&p.profile, "profile", "", "AWS shared config profile")
	cmd.Flags().BoolVar(&p.includeDefaults, "include-defaults", false, "report Default Furniture: the default VPC and its subnets, main route tables, default security groups")
	cmd.Flags().StringVar(&p.configPath, "config", "tofu-drift.toml", "config file with Ignore Rules (optional unless given)")
}

// run loads config and state, plans for Drift when withDrift, lists live
// resources and builds the Report with Ignore Rules applied.
func (p *pipeline) run(cmd *cobra.Command, withDrift bool) (*report.Report, error) {
	ctx := cmd.Context()
	conf, err := driftconfig.Load(p.configPath, cmd.Flags().Changed("config"))
	if err != nil {
		return nil, err
	}
	var opts []func(*config.LoadOptions) error
	if p.region != "" {
		opts = append(opts, config.WithRegion(p.region))
	}
	if p.profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(p.profile))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}
	if err := retrieveCredentials(ctx, cfg); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "tofu-drift needs read-only AWS credentials. Attach this IAM policy (iam-policy.json) to the role or user you scan with (add s3:GetObject on the state object to read s3:// state):")
		fmt.Fprint(cmd.OutOrStdout(), string(tofudrift.IAMPolicy))
		return nil, fmt.Errorf("no AWS credentials: %w", err)
	}
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
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
	managed, source, err := src.Load(ctx, p.statePath)
	if err != nil {
		return nil, err
	}

	if cfg.Region == "" {
		return nil, fmt.Errorf("no AWS region: pass --region or set AWS_REGION")
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
	switch {
	case !withDrift:
	case p.statePath != "":
		fmt.Fprintln(cmd.ErrOrStderr(), "notice: --state given, skipping drift detection (no root module to plan against)")
	default:
		// state pull above already required .tf files and a binary.
		drifts, err = (&plan.Runner{Dir: dir, LookPath: lookPath, Run: runTool}).Drift(ctx)
		if err != nil {
			return nil, err
		}
	}

	live, err := listAll(ctx, cmd.ErrOrStderr(), newScanners(cfg))
	if err != nil {
		return nil, err
	}
	if !p.includeDefaults {
		live = slices.DeleteFunc(live, match.Furniture)
	}
	r := report.New(meta, managed)
	r.Ignore = conf.Ignored
	r.AddLive(live, time.Now())
	r.AddDrift(drifts)
	return r, nil
}

func scanCmd() *cobra.Command {
	var p pipeline
	var explain string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Report drift, unmanaged and idle resources",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := p.run(cmd, true)
			if err != nil {
				return err
			}
			return render(cmd, r, explain, asJSON, r.WriteTable)
		},
	}
	p.flags(cmd)
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a table")
	cmd.Flags().StringVar(&explain, "explain", "", "explain one Finding: a Drift address, or the AWS ID of an Unmanaged or Idle Resource")
	return cmd
}

func unmanagedCmd() *cobra.Command {
	var p pipeline
	var explain, sortBy string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "unmanaged",
		Short: "Report only unmanaged and idle resources",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Fail on a bad --sort before the slow part.
			if err := report.New(report.Scan{}, nil).Sort(sortBy); err != nil {
				return err
			}
			r, err := p.run(cmd, false)
			if err != nil {
				return err
			}
			_ = r.Sort(sortBy)
			return render(cmd, r, explain, asJSON, r.WriteUnmanagedTable)
		},
	}
	p.flags(cmd)
	cmd.Flags().StringVar(&sortBy, "sort", "cost", "sort by cost, type or age (oldest first; unknown age last)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a table")
	cmd.Flags().StringVar(&explain, "explain", "", "explain one Finding by the AWS ID of an Unmanaged or Idle Resource")
	return cmd
}

func importGenCmd() *cobra.Command {
	var p pipeline
	var ids []string
	cmd := &cobra.Command{
		Use:   "import-gen",
		Short: "Print import blocks for Unmanaged Resources",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := p.run(cmd, false)
			if err != nil {
				return err
			}
			return r.WriteImports(cmd.OutOrStdout(), ids)
		},
	}
	p.flags(cmd)
	cmd.Flags().StringSliceVar(&ids, "ids", nil, "AWS IDs of Unmanaged Resources to import, comma-separated")
	_ = cmd.MarkFlagRequired("ids")
	return cmd
}

// render writes r as --explain, JSON or table, and signals Findings.
func render(cmd *cobra.Command, r *report.Report, explain string, asJSON bool, table func(io.Writer) error) error {
	out := cmd.OutOrStdout()
	var err error
	switch {
	case explain != "":
		if !r.Explain(out, explain) {
			return fmt.Errorf("--explain %q: no Finding with that ID", explain)
		}
		return errFindings
	case asJSON:
		err = r.WriteJSON(out)
	default:
		err = table(out)
	}
	if err != nil {
		return err
	}
	if len(r.Findings) > 0 {
		return errFindings
	}
	return nil
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
