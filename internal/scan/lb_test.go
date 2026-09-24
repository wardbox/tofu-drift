package scan

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	elb "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing/types"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	v2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
)

// fakeELBv2 serves one page of load balancers and target groups, and target
// counts by target group ARN.
type fakeELBv2 struct {
	lbs     []v2types.LoadBalancer
	groups  []v2types.TargetGroup
	targets map[string]int
}

func (f fakeELBv2) DescribeLoadBalancers(context.Context, *elbv2.DescribeLoadBalancersInput, ...func(*elbv2.Options)) (*elbv2.DescribeLoadBalancersOutput, error) {
	return &elbv2.DescribeLoadBalancersOutput{LoadBalancers: f.lbs}, nil
}

func (f fakeELBv2) DescribeTargetGroups(context.Context, *elbv2.DescribeTargetGroupsInput, ...func(*elbv2.Options)) (*elbv2.DescribeTargetGroupsOutput, error) {
	return &elbv2.DescribeTargetGroupsOutput{TargetGroups: f.groups}, nil
}

func (f fakeELBv2) DescribeTargetHealth(_ context.Context, in *elbv2.DescribeTargetHealthInput, _ ...func(*elbv2.Options)) (*elbv2.DescribeTargetHealthOutput, error) {
	return &elbv2.DescribeTargetHealthOutput{TargetHealthDescriptions: make([]v2types.TargetHealthDescription, f.targets[aws.ToString(in.TargetGroupArn)])}, nil
}

func TestLoadBalancersList(t *testing.T) {
	lb := func(name string, typ v2types.LoadBalancerTypeEnum) v2types.LoadBalancer {
		return v2types.LoadBalancer{LoadBalancerArn: aws.String("arn:" + name), LoadBalancerName: aws.String(name), Type: typ}
	}
	client := fakeELBv2{
		lbs: []v2types.LoadBalancer{
			lb("web", v2types.LoadBalancerTypeEnumApplication),
			lb("empty", v2types.LoadBalancerTypeEnumApplication),
			lb("bare", v2types.LoadBalancerTypeEnumNetwork),
			lb("gwlb", v2types.LoadBalancerTypeEnumGateway),
		},
		groups: []v2types.TargetGroup{
			{TargetGroupArn: aws.String("tg-web-blue"), LoadBalancerArns: []string{"arn:web"}},
			{TargetGroupArn: aws.String("tg-web-green"), LoadBalancerArns: []string{"arn:web"}},
			{TargetGroupArn: aws.String("tg-empty"), LoadBalancerArns: []string{"arn:empty"}},
			{TargetGroupArn: aws.String("tg-loose")},
		},
		targets: map[string]int{"tg-web-blue": 2, "tg-loose": 1},
	}
	got, err := LoadBalancers{client}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %+v, want 3 (gateway skipped)", got)
	}
	web, empty, bare := got[0], got[1], got[2]
	if web.Type != "aws_lb" || web.Key != "arn:web" || web.ARN != "arn:web" || web.Name != "web" ||
		web.Class != "application" || web.Idle != "" {
		t.Errorf("one non-empty target group keeps it busy: %+v", web)
	}
	if empty.Idle != "no targets" {
		t.Errorf("all target groups empty: %+v", empty)
	}
	if bare.Class != "network" || bare.Idle != "" {
		t.Errorf("no target groups (redirect-only, say) is not provably idle: %+v", bare)
	}
}

type fakeCLB []elbtypes.LoadBalancerDescription

func (f fakeCLB) DescribeLoadBalancers(context.Context, *elb.DescribeLoadBalancersInput, ...func(*elb.Options)) (*elb.DescribeLoadBalancersOutput, error) {
	return &elb.DescribeLoadBalancersOutput{LoadBalancerDescriptions: f}, nil
}

func TestClassicLoadBalancersList(t *testing.T) {
	got, err := ClassicLoadBalancers{fakeCLB{
		{LoadBalancerName: aws.String("legacy")},
		{LoadBalancerName: aws.String("live"), Instances: []elbtypes.Instance{{InstanceId: aws.String("i-1")}}},
	}}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if l := got[0]; l.Type != "aws_elb" || l.Key != "legacy" || l.Idle != "no instances" {
		t.Errorf("empty clb: %+v", l)
	}
	if l := got[1]; l.Idle != "" {
		t.Errorf("clb with instances: %+v", l)
	}
}
