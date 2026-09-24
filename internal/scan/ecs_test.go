package scan

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

// fakeECS serves cluster names by ARN and service ARNs by cluster ARN.
type fakeECS struct {
	clusters map[string]string
	services map[string][]string
}

func (f fakeECS) ListClusters(context.Context, *ecs.ListClustersInput, ...func(*ecs.Options)) (*ecs.ListClustersOutput, error) {
	var arns []string
	for a := range f.clusters {
		arns = append(arns, a)
	}
	return &ecs.ListClustersOutput{ClusterArns: arns}, nil
}

func (f fakeECS) DescribeClusters(_ context.Context, in *ecs.DescribeClustersInput, _ ...func(*ecs.Options)) (*ecs.DescribeClustersOutput, error) {
	var cs []types.Cluster
	for _, a := range in.Clusters {
		cs = append(cs, types.Cluster{ClusterArn: aws.String(a), ClusterName: aws.String(f.clusters[a]),
			Tags: []types.Tag{{Key: aws.String("team"), Value: aws.String("x")}}})
	}
	return &ecs.DescribeClustersOutput{Clusters: cs}, nil
}

func (f fakeECS) ListServices(_ context.Context, in *ecs.ListServicesInput, _ ...func(*ecs.Options)) (*ecs.ListServicesOutput, error) {
	return &ecs.ListServicesOutput{ServiceArns: f.services[aws.ToString(in.Cluster)]}, nil
}

func (f fakeECS) DescribeServices(_ context.Context, in *ecs.DescribeServicesInput, _ ...func(*ecs.Options)) (*ecs.DescribeServicesOutput, error) {
	if len(in.Services) > 10 {
		return nil, fmt.Errorf("DescribeServices takes at most 10 services, got %d", len(in.Services))
	}
	var ss []types.Service
	for _, a := range in.Services {
		ss = append(ss, types.Service{ServiceArn: aws.String(a), ServiceName: aws.String("svc" + a[len(a)-2:]),
			ClusterArn: in.Cluster, CreatedAt: aws.Time(time.Unix(0, 0))})
	}
	return &ecs.DescribeServicesOutput{Services: ss}, nil
}

func TestECSClustersList(t *testing.T) {
	got, err := ECSClusters{fakeECS{clusters: map[string]string{"arn:c/default": "default"}}}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if c := got[0]; c.Type != "aws_ecs_cluster" || c.Key != "default" || c.ARN != "arn:c/default" ||
		c.Name != "default" || !c.Default || c.Tags["team"] != "x" {
		t.Errorf("default cluster: %+v", c)
	}
}

func TestECSServicesList(t *testing.T) {
	var many []string
	for i := range 12 {
		many = append(many, fmt.Sprintf("arn:s/%02d", i))
	}
	got, err := ECSServices{fakeECS{
		clusters: map[string]string{"arn:aws:ecs:us-east-1:1:cluster/app": "app"},
		services: map[string][]string{"arn:aws:ecs:us-east-1:1:cluster/app": many},
	}}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 12 {
		t.Fatalf("got %d services, want 12 across two DescribeServices batches", len(got))
	}
	// The Match Key is the tofu import ID, cluster/service.
	if s := got[11]; s.Type != "aws_ecs_service" || s.Key != "app/svc11" || s.ARN != "arn:s/11" || s.Name != "svc11" || s.Created == nil {
		t.Errorf("service: %+v", s)
	}
}
