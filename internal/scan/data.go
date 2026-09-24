package scan

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// sizeUnknown notes types whose cost is storage we do not measure.
const sizeUnknown = "size unknown"

// RDSInstances lists RDS DB instances, Aurora instances included. A stopped
// one is Idle. Its automated snapshots are Derived Resources.
type RDSInstances struct {
	Client interface {
		rds.DescribeDBInstancesAPIClient
		rds.DescribeDBSnapshotsAPIClient
	}
}

func (RDSInstances) Permissions() []string {
	return []string{"rds:DescribeDBInstances", "rds:DescribeDBSnapshots"}
}

func (s RDSInstances) List(ctx context.Context) ([]LiveResource, error) {
	auto := map[string][]string{}
	sp := rds.NewDescribeDBSnapshotsPaginator(s.Client, &rds.DescribeDBSnapshotsInput{SnapshotType: aws.String("automated")})
	for sp.HasMorePages() {
		page, err := sp.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, v := range page.DBSnapshots {
			db := aws.ToString(v.DBInstanceIdentifier)
			auto[db] = append(auto[db], aws.ToString(v.DBSnapshotIdentifier))
		}
	}
	var out []LiveResource
	p := rds.NewDescribeDBInstancesPaginator(s.Client, &rds.DescribeDBInstancesInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, d := range page.DBInstances {
			// DocumentDB and Neptune share this API but are other OpenTofu types.
			if e := aws.ToString(d.Engine); e == "docdb" || e == "neptune" {
				continue
			}
			id := aws.ToString(d.DBInstanceIdentifier)
			tags := rdsTags(d.TagList)
			r := LiveResource{
				Type:    "aws_db_instance",
				Key:     id,
				ARN:     aws.ToString(d.DBInstanceArn),
				Name:    tags["Name"],
				Tags:    tags,
				Created: d.InstanceCreateTime,
				Class:   aws.ToString(d.DBInstanceClass),
				SizeGB:  float64(aws.ToInt32(d.AllocatedStorage)),
				Storage: aws.ToString(d.StorageType),
				Nodes:   1,
				Derived: auto[id],
			}
			if aws.ToBool(d.MultiAZ) {
				r.Nodes = 2 // a standby instance, and its storage, billed like the primary
			}
			if aws.ToString(d.DBInstanceStatus) == "stopped" {
				r.Idle, r.Stopped = "stopped", true
			}
			out = append(out, r)
		}
	}
	return out, nil
}

// RDSSnapshots lists the account's own RDS DB snapshots. Automated ones fold
// into their instance (see RDSInstances).
type RDSSnapshots struct {
	Client rds.DescribeDBSnapshotsAPIClient
}

func (RDSSnapshots) Permissions() []string { return []string{"rds:DescribeDBSnapshots"} }

func (s RDSSnapshots) List(ctx context.Context) ([]LiveResource, error) {
	var out []LiveResource
	p := rds.NewDescribeDBSnapshotsPaginator(s.Client, &rds.DescribeDBSnapshotsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, v := range page.DBSnapshots {
			tags := rdsTags(v.TagList)
			out = append(out, LiveResource{
				Type:    "aws_db_snapshot",
				Key:     aws.ToString(v.DBSnapshotIdentifier),
				ARN:     aws.ToString(v.DBSnapshotArn),
				Name:    tags["Name"],
				Tags:    tags,
				Created: v.SnapshotCreateTime,
				SizeGB:  float64(aws.ToInt32(v.AllocatedStorage)),
				Class:   aws.ToString(v.SnapshotType), // automated, manual, awsbackup, ...
			})
		}
	}
	return out, nil
}

func rdsTags(ts []rdstypes.Tag) map[string]string {
	m := make(map[string]string, len(ts))
	for _, t := range ts {
		m[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return m
}

// DynamoDBTables lists tables, one DescribeTable each for ARN and age. Size
// is not priced, and tags are not fetched.
type DynamoDBTables struct {
	Client interface {
		dynamodb.ListTablesAPIClient
		dynamodb.DescribeTableAPIClient
	}
}

func (DynamoDBTables) Permissions() []string {
	return []string{"dynamodb:ListTables", "dynamodb:DescribeTable"}
}

func (s DynamoDBTables) List(ctx context.Context) ([]LiveResource, error) {
	var out []LiveResource
	p := dynamodb.NewListTablesPaginator(s.Client, &dynamodb.ListTablesInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, name := range page.TableNames {
			d, err := s.Client.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(name)})
			if err != nil {
				return nil, err
			}
			out = append(out, LiveResource{
				Type: "aws_dynamodb_table", Key: name, ARN: aws.ToString(d.Table.TableArn),
				Created: d.Table.CreationDateTime, Note: sizeUnknown,
			})
		}
	}
	return out, nil
}

// ElastiCache lists cache clusters. Members of a replication group are
// reported as that group, one row with every member node counted.
type ElastiCache struct {
	Client elasticache.DescribeCacheClustersAPIClient
}

func (ElastiCache) Permissions() []string { return []string{"elasticache:DescribeCacheClusters"} }

func (s ElastiCache) List(ctx context.Context) ([]LiveResource, error) {
	var out []LiveResource
	groups := map[string]int{} // replication group id → index in out
	p := elasticache.NewDescribeCacheClustersPaginator(s.Client, &elasticache.DescribeCacheClustersInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, c := range page.CacheClusters {
			nodes := int(aws.ToInt32(c.NumCacheNodes))
			group := aws.ToString(c.ReplicationGroupId)
			if group == "" {
				id := aws.ToString(c.CacheClusterId)
				out = append(out, LiveResource{
					Type: "aws_elasticache_cluster", Key: id, ARN: aws.ToString(c.ARN),
					Created: c.CacheClusterCreateTime, Class: aws.ToString(c.CacheNodeType), Nodes: nodes,
				})
				continue
			}
			if i, ok := groups[group]; ok {
				out[i].Nodes += nodes
				continue
			}
			groups[group] = len(out)
			out = append(out, LiveResource{
				Type: "aws_elasticache_replication_group", Key: group,
				Created: c.CacheClusterCreateTime, Class: aws.ToString(c.CacheNodeType), Nodes: nodes,
			})
		}
	}
	return out, nil
}

// S3Buckets lists every bucket in the account, whatever its region, with no
// per-bucket calls: no location, size or tags.
type S3Buckets struct {
	Client s3.ListBucketsAPIClient
}

func (S3Buckets) Permissions() []string { return []string{"s3:ListAllMyBuckets"} }

func (s S3Buckets) List(ctx context.Context) ([]LiveResource, error) {
	var out []LiveResource
	p := s3.NewListBucketsPaginator(s.Client, &s3.ListBucketsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, b := range page.Buckets {
			name := aws.ToString(b.Name)
			out = append(out, LiveResource{
				Type: "aws_s3_bucket", Key: name, ARN: "arn:aws:s3:::" + name,
				Region: "global", Created: b.CreationDate, Note: sizeUnknown,
			})
		}
	}
	return out, nil
}
