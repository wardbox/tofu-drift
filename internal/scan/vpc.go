package scan

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

// VPCs lists VPCs. Plumbing: never costed, never Idle.
type VPCs struct {
	Client ec2.DescribeVpcsAPIClient
}

func (VPCs) Permissions() []string { return []string{"ec2:DescribeVpcs"} }

func (s VPCs) List(ctx context.Context) ([]LiveResource, error) {
	var out []LiveResource
	p := ec2.NewDescribeVpcsPaginator(s.Client, &ec2.DescribeVpcsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, v := range page.Vpcs {
			out = append(out, plumbing("aws_vpc", aws.ToString(v.VpcId), ec2Tags(v.Tags), aws.ToBool(v.IsDefault)))
		}
	}
	return out, nil
}

// Subnets lists subnets; the ones AWS made for the default VPC are
// default-for-AZ.
type Subnets struct {
	Client ec2.DescribeSubnetsAPIClient
}

func (Subnets) Permissions() []string { return []string{"ec2:DescribeSubnets"} }

func (s Subnets) List(ctx context.Context) ([]LiveResource, error) {
	var out []LiveResource
	p := ec2.NewDescribeSubnetsPaginator(s.Client, &ec2.DescribeSubnetsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, v := range page.Subnets {
			out = append(out, plumbing("aws_subnet", aws.ToString(v.SubnetId), ec2Tags(v.Tags), aws.ToBool(v.DefaultForAz)))
		}
	}
	return out, nil
}

// RouteTables lists route tables; each VPC's main route table is AWS-made.
type RouteTables struct {
	Client ec2.DescribeRouteTablesAPIClient
}

func (RouteTables) Permissions() []string { return []string{"ec2:DescribeRouteTables"} }

func (s RouteTables) List(ctx context.Context) ([]LiveResource, error) {
	var out []LiveResource
	p := ec2.NewDescribeRouteTablesPaginator(s.Client, &ec2.DescribeRouteTablesInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, v := range page.RouteTables {
			main := false
			for _, a := range v.Associations {
				main = main || aws.ToBool(a.Main)
			}
			out = append(out, plumbing("aws_route_table", aws.ToString(v.RouteTableId), ec2Tags(v.Tags), main))
		}
	}
	return out, nil
}

// SecurityGroups lists security groups; every VPC's group named "default"
// is AWS-made and cannot be deleted.
type SecurityGroups struct {
	Client ec2.DescribeSecurityGroupsAPIClient
}

func (SecurityGroups) Permissions() []string { return []string{"ec2:DescribeSecurityGroups"} }

func (s SecurityGroups) List(ctx context.Context) ([]LiveResource, error) {
	var out []LiveResource
	p := ec2.NewDescribeSecurityGroupsPaginator(s.Client, &ec2.DescribeSecurityGroupsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, v := range page.SecurityGroups {
			out = append(out, plumbing("aws_security_group", aws.ToString(v.GroupId), ec2Tags(v.Tags), aws.ToString(v.GroupName) == "default"))
		}
	}
	return out, nil
}

func plumbing(typ, key string, tags map[string]string, def bool) LiveResource {
	return LiveResource{Type: typ, Key: key, Name: tags["Name"], Tags: tags, Default: def}
}
