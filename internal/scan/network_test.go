package scan

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// fakeNet serves one canned page per network Describe call.
type fakeNet struct {
	addrs []types.Address
	nats  []types.NatGateway
	enis  []types.NetworkInterface
}

func (f fakeNet) DescribeAddresses(context.Context, *ec2.DescribeAddressesInput, ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error) {
	return &ec2.DescribeAddressesOutput{Addresses: f.addrs}, nil
}

func (f fakeNet) DescribeNatGateways(context.Context, *ec2.DescribeNatGatewaysInput, ...func(*ec2.Options)) (*ec2.DescribeNatGatewaysOutput, error) {
	return &ec2.DescribeNatGatewaysOutput{NatGateways: f.nats}, nil
}

func (f fakeNet) DescribeNetworkInterfaces(context.Context, *ec2.DescribeNetworkInterfacesInput, ...func(*ec2.Options)) (*ec2.DescribeNetworkInterfacesOutput, error) {
	return &ec2.DescribeNetworkInterfacesOutput{NetworkInterfaces: f.enis}, nil
}

func TestEIPList(t *testing.T) {
	got, err := EIPs{fakeNet{addrs: []types.Address{
		{AllocationId: aws.String("eipalloc-free"), Tags: nameTag("spare")},
		{AllocationId: aws.String("eipalloc-used"), AssociationId: aws.String("eipassoc-1")},
		{AllocationId: aws.String("eipalloc-alb"), ServiceManaged: types.ServiceManagedAlb},
	}}}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v, want 2 (service-managed skipped)", got)
	}
	free, used := got[0], got[1]
	if free.Type != "aws_eip" || free.Key != "eipalloc-free" || free.Name != "spare" || free.Idle != "unassociated" {
		t.Errorf("unassociated: %+v", free)
	}
	if used.Key != "eipalloc-used" || used.Idle != "" {
		t.Errorf("associated: %+v", used)
	}
}

func TestNATList(t *testing.T) {
	created := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	got, err := NATGateways{fakeNet{nats: []types.NatGateway{
		{NatGatewayId: aws.String("nat-1"), State: types.NatGatewayStateAvailable, CreateTime: &created,
			NatGatewayAddresses: []types.NatGatewayAddress{{AllocationId: aws.String("eipalloc-nat"), NetworkInterfaceId: aws.String("eni-nat")}}},
		{NatGatewayId: aws.String("nat-gone"), State: types.NatGatewayStateDeleted},
	}}}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %+v, want 1 (deleted skipped)", got)
	}
	n := got[0]
	if n.Type != "aws_nat_gateway" || n.Key != "nat-1" || n.Idle != "" || !n.Created.Equal(created) ||
		fmt.Sprint(n.Derived) != "[eipalloc-nat eni-nat]" {
		t.Errorf("nat: %+v", n)
	}
}

func TestENIList(t *testing.T) {
	got, err := ENIs{fakeNet{enis: []types.NetworkInterface{
		{NetworkInterfaceId: aws.String("eni-free"), Status: types.NetworkInterfaceStatusAvailable, TagSet: nameTag("spare")},
		{NetworkInterfaceId: aws.String("eni-used"), Status: types.NetworkInterfaceStatusInUse, Description: aws.String("Amazon EKS prod")},
		{NetworkInterfaceId: aws.String("eni-lambda"), Status: types.NetworkInterfaceStatusInUse, RequesterManaged: aws.Bool(true)},
	}}}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v, want 2 (requester-managed skipped)", got)
	}
	free, used := got[0], got[1]
	if free.Type != "aws_network_interface" || free.Key != "eni-free" || free.Name != "spare" || free.Idle != "unattached" {
		t.Errorf("available: %+v", free)
	}
	if used.Key != "eni-used" || used.Idle != "" || used.Name != "Amazon EKS prod" {
		t.Errorf("in use: %+v", used)
	}
}
