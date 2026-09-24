package scan

import (
	"context"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

// ECSClusters lists ECS clusters. The one named "default" is marked Default:
// AWS creates it on first use.
type ECSClusters struct {
	Client interface {
		ecs.ListClustersAPIClient
		DescribeClusters(context.Context, *ecs.DescribeClustersInput, ...func(*ecs.Options)) (*ecs.DescribeClustersOutput, error)
	}
}

func (ECSClusters) Permissions() []string {
	return []string{"ecs:ListClusters", "ecs:DescribeClusters"}
}

func (s ECSClusters) List(ctx context.Context) ([]LiveResource, error) {
	var out []LiveResource
	p := ecs.NewListClustersPaginator(s.Client, &ecs.ListClustersInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		// ListClusters pages hold at most 100, DescribeClusters' limit.
		if len(page.ClusterArns) == 0 {
			continue
		}
		d, err := s.Client.DescribeClusters(ctx, &ecs.DescribeClustersInput{
			Clusters: page.ClusterArns,
			Include:  []types.ClusterField{types.ClusterFieldTags},
		})
		if err != nil {
			return nil, err
		}
		for _, c := range d.Clusters {
			// Keyed by name, the tofu import ID; ECS returns no creation time.
			name := aws.ToString(c.ClusterName)
			out = append(out, LiveResource{Type: "aws_ecs_cluster", Key: name, ARN: aws.ToString(c.ClusterArn), Name: name,
				Tags: ecsTags(c.Tags), Default: name == "default"})
		}
	}
	return out, nil
}

// ECSServices lists the services of every ECS cluster.
type ECSServices struct {
	Client interface {
		ecs.ListClustersAPIClient
		ecs.ListServicesAPIClient
		ecs.DescribeServicesAPIClient
	}
}

func (ECSServices) Permissions() []string {
	return []string{"ecs:ListClusters", "ecs:ListServices", "ecs:DescribeServices"}
}

func (s ECSServices) List(ctx context.Context) ([]LiveResource, error) {
	var out []LiveResource
	cp := ecs.NewListClustersPaginator(s.Client, &ecs.ListClustersInput{})
	for cp.HasMorePages() {
		clusters, err := cp.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, cluster := range clusters.ClusterArns {
			sp := ecs.NewListServicesPaginator(s.Client, &ecs.ListServicesInput{Cluster: aws.String(cluster)})
			for sp.HasMorePages() {
				page, err := sp.NextPage(ctx)
				if err != nil {
					return nil, err
				}
				// DescribeServices takes at most 10 services.
				for batch := range slices.Chunk(page.ServiceArns, 10) {
					d, err := s.Client.DescribeServices(ctx, &ecs.DescribeServicesInput{
						Cluster:  aws.String(cluster),
						Services: batch,
						Include:  []types.ServiceField{types.ServiceFieldTags},
					})
					if err != nil {
						return nil, err
					}
					for _, svc := range d.Services {
						name := aws.ToString(svc.ServiceName)
						out = append(out, LiveResource{Type: "aws_ecs_service", Key: ECSServiceKey(aws.ToString(svc.ClusterArn), name),
							ARN: aws.ToString(svc.ServiceArn), Name: name, Tags: ecsTags(svc.Tags), Created: svc.CreatedAt})
					}
				}
			}
		}
	}
	return out, nil
}

// ECSServiceKey is the Match Key of an ECS service, the tofu import ID
// "cluster/service". cluster may be the cluster's name or ARN.
func ECSServiceKey(cluster, service string) string {
	return cluster[strings.LastIndex(cluster, "/")+1:] + "/" + service
}

func ecsTags(ts []types.Tag) map[string]string {
	m := map[string]string{}
	for _, t := range ts {
		m[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return m
}
