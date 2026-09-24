package scan

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// fakeVPC serves one canned page per plumbing Describe call.
type fakeVPC struct {
	vpcs    []types.Vpc
	subnets []types.Subnet
	tables  []types.RouteTable
	groups  []types.SecurityGroup
}

func (f fakeVPC) DescribeVpcs(context.Context, *ec2.DescribeVpcsInput, ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
	return &ec2.DescribeVpcsOutput{Vpcs: f.vpcs}, nil
}

func (f fakeVPC) DescribeSubnets(context.Context, *ec2.DescribeSubnetsInput, ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
	return &ec2.DescribeSubnetsOutput{Subnets: f.subnets}, nil
}

func (f fakeVPC) DescribeRouteTables(context.Context, *ec2.DescribeRouteTablesInput, ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error) {
	return &ec2.DescribeRouteTablesOutput{RouteTables: f.tables}, nil
}

func (f fakeVPC) DescribeSecurityGroups(context.Context, *ec2.DescribeSecurityGroupsInput, ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
	return &ec2.DescribeSecurityGroupsOutput{SecurityGroups: f.groups}, nil
}

func nameTag(v string) []types.Tag {
	return []types.Tag{{Key: aws.String("Name"), Value: aws.String(v)}}
}

func TestPlumbingList(t *testing.T) {
	f := fakeVPC{
		vpcs: []types.Vpc{
			{VpcId: aws.String("vpc-def"), IsDefault: aws.Bool(true)},
			{VpcId: aws.String("vpc-app"), IsDefault: aws.Bool(false), Tags: nameTag("app")},
		},
		subnets: []types.Subnet{
			{SubnetId: aws.String("subnet-def"), DefaultForAz: aws.Bool(true)},
			{SubnetId: aws.String("subnet-app"), Tags: nameTag("private-a")},
		},
		tables: []types.RouteTable{
			{RouteTableId: aws.String("rtb-main"), Associations: []types.RouteTableAssociation{{Main: aws.Bool(true)}}},
			{RouteTableId: aws.String("rtb-app"), Associations: []types.RouteTableAssociation{{Main: aws.Bool(false)}}, Tags: nameTag("private")},
		},
		groups: []types.SecurityGroup{
			{GroupId: aws.String("sg-def"), GroupName: aws.String("default")},
			{GroupId: aws.String("sg-web"), GroupName: aws.String("web"), Tags: nameTag("web")},
		},
	}
	for _, tc := range []struct {
		s    Scanner
		typ  string
		keys [2]string
		name string
	}{
		{VPCs{f}, "aws_vpc", [2]string{"vpc-def", "vpc-app"}, "app"},
		{Subnets{f}, "aws_subnet", [2]string{"subnet-def", "subnet-app"}, "private-a"},
		{RouteTables{f}, "aws_route_table", [2]string{"rtb-main", "rtb-app"}, "private"},
		{SecurityGroups{f}, "aws_security_group", [2]string{"sg-def", "sg-web"}, "web"},
	} {
		got, err := tc.s.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatalf("%s: got %d, want 2", tc.typ, len(got))
		}
		def, user := got[0], got[1]
		if def.Type != tc.typ || def.Key != tc.keys[0] || !def.Default {
			t.Errorf("%s default: %+v", tc.typ, def)
		}
		if user.Type != tc.typ || user.Key != tc.keys[1] || user.Default || user.Name != tc.name || user.Idle != "" {
			t.Errorf("%s user-made: %+v", tc.typ, user)
		}
		if len(tc.s.Permissions()) != 1 {
			t.Errorf("%s: permissions %v", tc.typ, tc.s.Permissions())
		}
	}
}
