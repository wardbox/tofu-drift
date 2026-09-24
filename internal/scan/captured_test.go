package scan

// Happy-path tests against responses recorded from the sandbox stack after
// make-mess.sh (sandbox/capture-fixtures.sh). The hand-written fakes in the
// other tests still cover pagination, errors and shapes the sandbox lacks.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// captured loads testdata/captured/<name>.json into an SDK output struct.
func captured[T any](t *testing.T, name string) *T {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "captured", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return &v
}

// listed runs s and indexes its output by Match Key.
func listed(t *testing.T, s Scanner) map[string]LiveResource {
	t.Helper()
	rs, err := s.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]LiveResource{}
	for _, r := range rs {
		m[r.Key] = r
	}
	return m
}

// Sandbox IDs from the recording.
const (
	sbxInstance  = "i-06368f8a9da7ab277"
	sbxRootVol   = "vol-03296e07969f3197f"
	sbxDataVol   = "vol-064471b497294d84e"
	sbxStrayVol  = "vol-098b6bffba632b1af"
	sbxStrayEIP  = "eipalloc-0a3d468d4f54332a3"
	sbxNATEIP    = "eipalloc-0e418919abec57172"
	sbxNAT       = "nat-070535f9aa983d518"
	sbxNATENI    = "eni-0f1bfa93ad7feabba"
	sbxPrimENI   = "eni-0fff9e7f473e2fbbf"
	sbxAMI       = "ami-0bd49cfac2efd7bac"
	sbxSnapshot  = "snap-099c5c6f033ec4678"
	sbxALB       = "arn:aws:elasticloadbalancing:us-west-1:123456789012:loadbalancer/app/tdsbx-alb/a0fb401e97734c75"
	sbxTargetGrp = "arn:aws:elasticloadbalancing:us-west-1:123456789012:targetgroup/tdsbx-tg/b3486d95889e8ca4"
)

func TestCapturedEC2(t *testing.T) {
	instances := fakeInstances{"": captured[ec2.DescribeInstancesOutput](t, "ec2.DescribeInstances")}
	volumes := fakeEC2{"": captured[ec2.DescribeVolumesOutput](t, "ec2.DescribeVolumes")}

	got := listed(t, EC2{instances})
	i := got[sbxInstance]
	// The data volume was attached later (delete-on-termination false): it stands alone.
	if !i.Stopped || i.Class != "t4g.nano" || i.Name != "tdsbx-instance" || i.Created == nil ||
		!slices.Equal(i.Derived, []string{sbxRootVol, sbxPrimENI}) {
		t.Errorf("stopped instance: %+v", i)
	}

	got = listed(t, EBS{volumes})
	if len(got) != 3 || got[sbxStrayVol].Idle != "unattached" || got[sbxStrayVol].Class != "gp3" || got[sbxStrayVol].SizeGB != 1 ||
		got[sbxStrayVol].Created == nil || got[sbxRootVol].Idle != "" || got[sbxDataVol].Idle != "" {
		t.Errorf("volumes: %+v", got)
	}

	snaps := fakeSnapshots{fakeEC2: volumes, snaps: captured[ec2.DescribeSnapshotsOutput](t, "ec2.DescribeSnapshots").Snapshots}
	got = listed(t, Snapshots{snaps})
	if s := got[sbxSnapshot]; len(got) != 1 || s.Idle != "" || s.SizeGB != 1 || s.Created == nil {
		t.Errorf("snapshot of a live volume: %+v", got)
	}

	images := fakeImages{fakeInstances: instances, images: captured[ec2.DescribeImagesOutput](t, "ec2.DescribeImages").Images}
	got = listed(t, AMIs{images})
	if a := got[sbxAMI]; len(got) != 1 || a.Idle != "no instances" || a.Name != "tdsbx-ami" || a.Created == nil ||
		!slices.Equal(a.Derived, []string{sbxSnapshot}) {
		t.Errorf("AMI: %+v", got)
	}
}

func TestCapturedNetwork(t *testing.T) {
	net := fakeNet{
		addrs: captured[ec2.DescribeAddressesOutput](t, "ec2.DescribeAddresses").Addresses,
		nats:  captured[ec2.DescribeNatGatewaysOutput](t, "ec2.DescribeNatGateways").NatGateways,
		enis:  captured[ec2.DescribeNetworkInterfacesOutput](t, "ec2.DescribeNetworkInterfaces").NetworkInterfaces,
	}
	got := listed(t, EIPs{net})
	if len(got) != 2 || got[sbxStrayEIP].Idle != "unassociated" || got[sbxNATEIP].Idle != "" {
		t.Errorf("EIPs: %+v", got)
	}
	got = listed(t, NATGateways{net})
	if n := got[sbxNAT]; len(got) != 1 || n.Name != "tdsbx-stray-nat" || n.Created == nil ||
		!slices.Equal(n.Derived, []string{sbxNATEIP, sbxNATENI}) {
		t.Errorf("NAT gateway: %+v", got)
	}
	// The ALB's and the NAT gateway's ENIs are requester-managed: skipped.
	got = listed(t, ENIs{net})
	if len(got) != 1 || got[sbxPrimENI].Idle != "" {
		t.Errorf("ENIs: %+v", got)
	}
}

func TestCapturedPlumbing(t *testing.T) {
	vpc := fakeVPC{
		vpcs:    captured[ec2.DescribeVpcsOutput](t, "ec2.DescribeVpcs").Vpcs,
		subnets: captured[ec2.DescribeSubnetsOutput](t, "ec2.DescribeSubnets").Subnets,
		tables:  captured[ec2.DescribeRouteTablesOutput](t, "ec2.DescribeRouteTables").RouteTables,
		groups:  captured[ec2.DescribeSecurityGroupsOutput](t, "ec2.DescribeSecurityGroups").SecurityGroups,
	}
	count := func(rs []LiveResource, err error) (all, defaults int) {
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rs {
			if r.Default {
				defaults++
			}
		}
		return len(rs), defaults
	}
	for _, c := range []struct {
		name          string
		s             Scanner
		all, defaults int
	}{
		{"vpcs", VPCs{vpc}, 1, 0},
		{"subnets", Subnets{vpc}, 2, 0},
		// The sandbox's own table, and the main one AWS made with the VPC.
		{"route tables", RouteTables{vpc}, 2, 1},
		// The sandbox's group, and the VPC's default group.
		{"security groups", SecurityGroups{vpc}, 2, 1},
	} {
		if all, defaults := count(c.s.List(context.Background())); all != c.all || defaults != c.defaults {
			t.Errorf("%s: %d listed, %d default; want %d, %d", c.name, all, defaults, c.all, c.defaults)
		}
	}
}

func TestCapturedLoadBalancers(t *testing.T) {
	lbs := fakeELBv2{
		lbs:     captured[elbv2.DescribeLoadBalancersOutput](t, "elbv2.DescribeLoadBalancers").LoadBalancers,
		groups:  captured[elbv2.DescribeTargetGroupsOutput](t, "elbv2.DescribeTargetGroups").TargetGroups,
		targets: map[string]int{sbxTargetGrp: len(captured[elbv2.DescribeTargetHealthOutput](t, "elbv2.DescribeTargetHealth.b3486d95889e8ca4").TargetHealthDescriptions)},
	}
	got := listed(t, LoadBalancers{lbs})
	if lb := got[sbxALB]; len(got) != 1 || lb.Idle != "no targets" || lb.Class != "application" || lb.Created == nil {
		t.Errorf("ALB with an empty target group: %+v", got)
	}
}

func TestCapturedServices(t *testing.T) {
	got := listed(t, AutoScalingGroups{fakeASG(captured[autoscaling.DescribeAutoScalingGroupsOutput](t, "autoscaling.DescribeAutoScalingGroups").AutoScalingGroups)})
	if g := got["tdsbx-asg"]; len(got) != 1 || len(g.Derived) != 0 || g.Tags["tofu-drift-sandbox"] != "tdsbx" || g.Created == nil {
		t.Errorf("ASG at zero: %+v", got)
	}

	got = listed(t, DynamoDBTables{capturedDynamo{t}})
	if tb := got["tdsbx-table"]; len(got) != 1 || tb.Created == nil || tb.ARN == "" {
		t.Errorf("table: %+v", got)
	}

	cluster := captured[ecs.DescribeClustersOutput](t, "ecs.DescribeClusters.tdsbx-ecs").Clusters[0]
	if c := *cluster.ClusterName; c != "tdsbx-ecs" || len(captured[ecs.ListServicesOutput](t, "ecs.ListServices.tdsbx-ecs").ServiceArns) != 0 {
		t.Errorf("ECS cluster %s", c)
	}

	got = listed(t, LambdaFunctions{fakeFunctions(captured[lambda.ListFunctionsOutput](t, "lambda.ListFunctions").Functions)})
	if f := got["tdsbx-fn"]; len(got) != 1 || f.Created == nil || !slices.Equal(f.Derived, []string{"/aws/lambda/tdsbx-fn"}) {
		t.Errorf("function (LastModified parsed as its age): %+v", got)
	}

	got = listed(t, LogGroups{fakeLogGroups(captured[cloudwatchlogs.DescribeLogGroupsOutput](t, "logs.DescribeLogGroups").LogGroups)})
	// DescribeLogGroups' arn ends in ":*"; logGroupArn is the group's own ARN.
	if g := got["tdsbx-app"]; len(got) != 2 || g.ARN != "arn:aws:logs:us-west-1:123456789012:log-group:tdsbx-app" ||
		g.Note != "" || g.Created == nil {
		t.Errorf("log groups: %+v", got)
	}

	iamc := fakeIAM{
		roles: captured[iam.ListRolesOutput](t, "iam.ListRoles").Roles,
		users: captured[iam.ListUsersOutput](t, "iam.ListUsers").Users,
	}
	got = listed(t, IAMRoles{iamc})
	if len(got) != 2 || got["tdsbx-lambda"].Default || got["tdsbx-scanner"].Created == nil {
		t.Errorf("roles: %+v", got)
	}
	got = listed(t, IAMUsers{iamc})
	if len(got) != 1 || got["tdsbx-user"].Region != "global" {
		t.Errorf("users: %+v", got)
	}

	zones := captured[route53.ListHostedZonesOutput](t, "route53.ListHostedZones").HostedZones
	got = listed(t, Route53Zones{fakeZones(zones)})
	if z := got["Z10437931QWTHGYJIN0JB"]; len(got) != 1 || z.Name != "tdsbx.sandbox.internal." {
		t.Errorf("private zone: %+v", got)
	}

	got = listed(t, S3Buckets{fakeS3(captured[s3.ListBucketsOutput](t, "s3.ListBuckets").Buckets)})
	if len(got) != 1 {
		t.Errorf("buckets: %+v", got)
	}
}

// capturedDynamo serves the recorded ListTables and DescribeTable responses.
type capturedDynamo struct{ t *testing.T }

func (c capturedDynamo) ListTables(context.Context, *dynamodb.ListTablesInput, ...func(*dynamodb.Options)) (*dynamodb.ListTablesOutput, error) {
	return captured[dynamodb.ListTablesOutput](c.t, "dynamodb.ListTables"), nil
}

func (c capturedDynamo) DescribeTable(_ context.Context, in *dynamodb.DescribeTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error) {
	return captured[dynamodb.DescribeTableOutput](c.t, "dynamodb.DescribeTable."+*in.TableName), nil
}
