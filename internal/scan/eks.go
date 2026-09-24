package scan

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
)

// EKSClusters lists EKS clusters. The cluster security group EKS creates is a
// Derived Resource; its ENIs and nodegroup instances are found by tag in the
// match package.
type EKSClusters struct {
	Client interface {
		eks.ListClustersAPIClient
		eks.DescribeClusterAPIClient
	}
}

func (EKSClusters) Permissions() []string {
	return []string{"eks:ListClusters", "eks:DescribeCluster"}
}

func (s EKSClusters) List(ctx context.Context) ([]LiveResource, error) {
	var out []LiveResource
	p := eks.NewListClustersPaginator(s.Client, &eks.ListClustersInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, name := range page.Clusters {
			d, err := s.Client.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: aws.String(name)})
			if err != nil {
				return nil, err
			}
			c := d.Cluster
			r := LiveResource{Type: "aws_eks_cluster", Key: name, ARN: aws.ToString(c.Arn), Name: name, Tags: c.Tags, Created: c.CreatedAt}
			if v := c.ResourcesVpcConfig; v != nil && v.ClusterSecurityGroupId != nil {
				r.Derived = append(r.Derived, *v.ClusterSecurityGroupId)
			}
			out = append(out, r)
		}
	}
	return out, nil
}
