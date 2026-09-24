package scan

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// EIPs lists Elastic IPs. One with no association is Idle. Addresses a
// service manages for itself (ALB, Global Accelerator) are skipped.
type EIPs struct {
	Client interface {
		DescribeAddresses(context.Context, *ec2.DescribeAddressesInput, ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error)
	}
}

func (EIPs) Permissions() []string { return []string{"ec2:DescribeAddresses"} }

func (s EIPs) List(ctx context.Context) ([]LiveResource, error) {
	// DescribeAddresses is not paginated.
	out, err := s.Client.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{})
	if err != nil {
		return nil, err
	}
	var rs []LiveResource
	for _, a := range out.Addresses {
		if a.ServiceManaged != "" {
			continue
		}
		tags := ec2Tags(a.Tags)
		r := LiveResource{Type: "aws_eip", Key: aws.ToString(a.AllocationId), Name: tags["Name"], Tags: tags}
		if a.AssociationId == nil {
			r.Idle = "unassociated"
		}
		rs = append(rs, r)
	}
	return rs, nil
}

// NATGateways lists NAT gateways. Costed, never Idle. Their Elastic IPs and
// ENIs are Derived Resources.
type NATGateways struct {
	Client ec2.DescribeNatGatewaysAPIClient
}

func (NATGateways) Permissions() []string { return []string{"ec2:DescribeNatGateways"} }

func (s NATGateways) List(ctx context.Context) ([]LiveResource, error) {
	var out []LiveResource
	p := ec2.NewDescribeNatGatewaysPaginator(s.Client, &ec2.DescribeNatGatewaysInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, n := range page.NatGateways {
			if n.State == types.NatGatewayStateDeleted || n.State == types.NatGatewayStateDeleting || n.State == types.NatGatewayStateFailed {
				continue
			}
			tags := ec2Tags(n.Tags)
			r := LiveResource{Type: "aws_nat_gateway", Key: aws.ToString(n.NatGatewayId), Name: tags["Name"], Tags: tags, Created: n.CreateTime}
			for _, a := range n.NatGatewayAddresses {
				for _, id := range []*string{a.AllocationId, a.NetworkInterfaceId} {
					if id != nil {
						r.Derived = append(r.Derived, *id)
					}
				}
			}
			out = append(out, r)
		}
	}
	return out, nil
}

// ENIs lists network interfaces. One in state "available" is Idle.
// Requester-managed ENIs belong to an AWS service (load balancer, Lambda,
// RDS, VPC endpoint, NAT gateway), which cleans them up; they are skipped.
type ENIs struct {
	Client ec2.DescribeNetworkInterfacesAPIClient
}

func (ENIs) Permissions() []string { return []string{"ec2:DescribeNetworkInterfaces"} }

func (s ENIs) List(ctx context.Context) ([]LiveResource, error) {
	var out []LiveResource
	p := ec2.NewDescribeNetworkInterfacesPaginator(s.Client, &ec2.DescribeNetworkInterfacesInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, n := range page.NetworkInterfaces {
			if aws.ToBool(n.RequesterManaged) {
				continue
			}
			tags := ec2Tags(n.TagSet)
			r := LiveResource{Type: "aws_network_interface", Key: aws.ToString(n.NetworkInterfaceId), Name: tags["Name"], Tags: tags}
			// The description names the creator, e.g. "Amazon EKS <cluster>".
			if r.Name == "" {
				r.Name = aws.ToString(n.Description)
			}
			if n.Status == types.NetworkInterfaceStatusAvailable {
				r.Idle = "unattached"
			}
			out = append(out, r)
		}
	}
	return out, nil
}
