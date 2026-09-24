package scan

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
)

type fakeEKS map[string]types.Cluster

func (f fakeEKS) ListClusters(context.Context, *eks.ListClustersInput, ...func(*eks.Options)) (*eks.ListClustersOutput, error) {
	var names []string
	for n := range f {
		names = append(names, n)
	}
	return &eks.ListClustersOutput{Clusters: names}, nil
}

func (f fakeEKS) DescribeCluster(_ context.Context, in *eks.DescribeClusterInput, _ ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
	c := f[aws.ToString(in.Name)]
	return &eks.DescribeClusterOutput{Cluster: &c}, nil
}

func TestEKSClustersList(t *testing.T) {
	got, err := EKSClusters{fakeEKS{"prod": {
		Name:               aws.String("prod"),
		Arn:                aws.String("arn:eks/prod"),
		CreatedAt:          aws.Time(time.Unix(0, 0)),
		Tags:               map[string]string{"team": "x"},
		ResourcesVpcConfig: &types.VpcConfigResponse{ClusterSecurityGroupId: aws.String("sg-eks")},
	}}}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if c := got[0]; c.Type != "aws_eks_cluster" || c.Key != "prod" || c.ARN != "arn:eks/prod" || c.Tags["team"] != "x" || c.Created == nil ||
		len(c.Derived) != 1 || c.Derived[0] != "sg-eks" {
		t.Errorf("cluster security group is Derived: %+v", c)
	}
}
