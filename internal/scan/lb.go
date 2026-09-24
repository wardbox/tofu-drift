package scan

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	elb "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
)

// ponytail: LoadBalancers and ClassicLoadBalancers skip DescribeTags, so Name
// is the load balancer name and Tags is empty; fetch tags when tag-based
// Ignore Rules need them.

// LoadBalancers lists ALBs and NLBs. One with target groups, all empty, is
// Idle; one with none may only redirect or return fixed responses, so is not.
// Gateway load balancers are not covered.
type LoadBalancers struct {
	Client interface {
		elbv2.DescribeLoadBalancersAPIClient
		elbv2.DescribeTargetGroupsAPIClient
		elbv2.DescribeTargetHealthAPIClient
	}
}

func (LoadBalancers) Permissions() []string {
	return []string{
		"elasticloadbalancing:DescribeLoadBalancers",
		"elasticloadbalancing:DescribeTargetGroups",
		"elasticloadbalancing:DescribeTargetHealth",
	}
}

func (s LoadBalancers) List(ctx context.Context) ([]LiveResource, error) {
	// busy maps each load balancer with target groups to whether any has a target.
	busy := map[string]bool{}
	tp := elbv2.NewDescribeTargetGroupsPaginator(s.Client, &elbv2.DescribeTargetGroupsInput{})
	for tp.HasMorePages() {
		page, err := tp.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, tg := range page.TargetGroups {
			if len(tg.LoadBalancerArns) == 0 {
				continue
			}
			h, err := s.Client.DescribeTargetHealth(ctx, &elbv2.DescribeTargetHealthInput{TargetGroupArn: tg.TargetGroupArn})
			if err != nil {
				return nil, err
			}
			for _, arn := range tg.LoadBalancerArns {
				busy[arn] = busy[arn] || len(h.TargetHealthDescriptions) > 0
			}
		}
	}
	var out []LiveResource
	p := elbv2.NewDescribeLoadBalancersPaginator(s.Client, &elbv2.DescribeLoadBalancersInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, lb := range page.LoadBalancers {
			if lb.Type == types.LoadBalancerTypeEnumGateway {
				continue
			}
			arn := aws.ToString(lb.LoadBalancerArn)
			r := LiveResource{
				Type:    "aws_lb",
				Key:     arn,
				ARN:     arn,
				Name:    aws.ToString(lb.LoadBalancerName),
				Created: lb.CreatedTime,
				Class:   string(lb.Type),
			}
			if hasTargets, grouped := busy[arn]; grouped && !hasTargets {
				r.Idle = "no targets"
			}
			out = append(out, r)
		}
	}
	return out, nil
}

// ClassicLoadBalancers lists CLBs. One with zero instances is Idle.
type ClassicLoadBalancers struct {
	Client elb.DescribeLoadBalancersAPIClient
}

func (ClassicLoadBalancers) Permissions() []string {
	return []string{"elasticloadbalancing:DescribeLoadBalancers"}
}

func (s ClassicLoadBalancers) List(ctx context.Context) ([]LiveResource, error) {
	var out []LiveResource
	p := elb.NewDescribeLoadBalancersPaginator(s.Client, &elb.DescribeLoadBalancersInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, lb := range page.LoadBalancerDescriptions {
			name := aws.ToString(lb.LoadBalancerName)
			r := LiveResource{Type: "aws_elb", Key: name, Name: name, Created: lb.CreatedTime}
			if len(lb.Instances) == 0 {
				r.Idle = "no instances"
			}
			out = append(out, r)
		}
	}
	return out, nil
}
