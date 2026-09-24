package scan

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	ectypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// fakeRDS serves one canned page per RDS Describe call, honouring the
// SnapshotType filter.
type fakeRDS struct {
	instances []rdstypes.DBInstance
	snapshots []rdstypes.DBSnapshot
}

func (f fakeRDS) DescribeDBInstances(context.Context, *rds.DescribeDBInstancesInput, ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error) {
	return &rds.DescribeDBInstancesOutput{DBInstances: f.instances}, nil
}

func (f fakeRDS) DescribeDBSnapshots(_ context.Context, in *rds.DescribeDBSnapshotsInput, _ ...func(*rds.Options)) (*rds.DescribeDBSnapshotsOutput, error) {
	var out []rdstypes.DBSnapshot
	for _, s := range f.snapshots {
		if in.SnapshotType == nil || aws.ToString(in.SnapshotType) == aws.ToString(s.SnapshotType) {
			out = append(out, s)
		}
	}
	return &rds.DescribeDBSnapshotsOutput{DBSnapshots: out}, nil
}

func db(id, status string) rdstypes.DBInstance {
	return rdstypes.DBInstance{
		DBInstanceIdentifier: aws.String(id),
		DBInstanceArn:        aws.String("arn:aws:rds:us-east-1:1:db:" + id),
		DBInstanceClass:      aws.String("db.t3.micro"),
		DBInstanceStatus:     aws.String(status),
		Engine:               aws.String("mysql"),
	}
}

func dbSnap(id, instance, typ string) rdstypes.DBSnapshot {
	return rdstypes.DBSnapshot{
		DBSnapshotIdentifier: aws.String(id),
		DBInstanceIdentifier: aws.String(instance),
		SnapshotType:         aws.String(typ),
		AllocatedStorage:     aws.Int32(20),
	}
}

var rdsFixture = fakeRDS{
	instances: func() []rdstypes.DBInstance {
		created := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
		app := db("app", "available")
		app.InstanceCreateTime = &created
		app.MultiAZ = aws.Bool(true)
		app.TagList = []rdstypes.Tag{{Key: aws.String("Name"), Value: aws.String("app db")}}
		return []rdstypes.DBInstance{app, db("old", "stopped"), func() rdstypes.DBInstance {
			d := db("docs", "available")
			d.Engine = aws.String("docdb")
			return d
		}()}
	}(),
	snapshots: []rdstypes.DBSnapshot{
		dbSnap("rds:app-2026-09-01", "app", "automated"),
		dbSnap("rds:app-2026-09-02", "app", "automated"),
		dbSnap("before-upgrade", "app", "manual"),
	},
}

func TestRDSInstancesList(t *testing.T) {
	got, err := RDSInstances{rdsFixture}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v, want 2 (docdb skipped)", got)
	}
	app, old := got[0], got[1]
	if app.Type != "aws_db_instance" || app.Key != "app" || app.ARN != "arn:aws:rds:us-east-1:1:db:app" ||
		app.Name != "app db" || app.Class != "db.t3.micro" || app.Nodes != 2 || app.Created == nil ||
		app.Idle != "" || app.Stopped ||
		fmt.Sprint(app.Derived) != "[rds:app-2026-09-01 rds:app-2026-09-02]" {
		t.Errorf("running multi-AZ: %+v", app)
	}
	if old.Key != "old" || old.Idle != "stopped" || !old.Stopped || old.Nodes != 1 || len(old.Derived) != 0 {
		t.Errorf("stopped: %+v", old)
	}
}

func TestRDSSnapshotsList(t *testing.T) {
	got, err := RDSSnapshots{rdsFixture}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %+v, want all 3", got)
	}
	manual := got[2]
	if manual.Type != "aws_db_snapshot" || manual.Key != "before-upgrade" || manual.SizeGB != 20 {
		t.Errorf("manual: %+v", manual)
	}
}

type fakeDynamo []string

func (f fakeDynamo) ListTables(context.Context, *dynamodb.ListTablesInput, ...func(*dynamodb.Options)) (*dynamodb.ListTablesOutput, error) {
	return &dynamodb.ListTablesOutput{TableNames: f}, nil
}

func (fakeDynamo) DescribeTable(_ context.Context, in *dynamodb.DescribeTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error) {
	created := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	return &dynamodb.DescribeTableOutput{Table: &dbtypes.TableDescription{
		TableArn: aws.String("arn:aws:dynamodb:us-east-1:1:table/" + aws.ToString(in.TableName)), CreationDateTime: &created,
	}}, nil
}

func TestDynamoDBList(t *testing.T) {
	got, err := DynamoDBTables{fakeDynamo{"users"}}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != "aws_dynamodb_table" || got[0].Key != "users" || got[0].Note != "size unknown" ||
		got[0].ARN != "arn:aws:dynamodb:us-east-1:1:table/users" || got[0].Created == nil {
		t.Errorf("got %+v", got)
	}
}

type fakeCache []ectypes.CacheCluster

func (f fakeCache) DescribeCacheClusters(context.Context, *elasticache.DescribeCacheClustersInput, ...func(*elasticache.Options)) (*elasticache.DescribeCacheClustersOutput, error) {
	return &elasticache.DescribeCacheClustersOutput{CacheClusters: f}, nil
}

func cacheNode(id, group string, nodes int32) ectypes.CacheCluster {
	c := ectypes.CacheCluster{
		CacheClusterId: aws.String(id),
		ARN:            aws.String("arn:aws:elasticache:us-east-1:1:cluster:" + id),
		CacheNodeType:  aws.String("cache.t3.micro"),
		NumCacheNodes:  aws.Int32(nodes),
	}
	if group != "" {
		c.ReplicationGroupId = aws.String(group)
	}
	return c
}

func TestElastiCacheList(t *testing.T) {
	got, err := ElastiCache{fakeCache{
		cacheNode("sessions-001", "sessions", 1),
		cacheNode("memo", "", 3),
		cacheNode("sessions-002", "sessions", 1),
	}}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v, want 2 (replication group members grouped)", got)
	}
	rg, memo := got[0], got[1]
	if memo.Type != "aws_elasticache_cluster" || memo.Key != "memo" || memo.Class != "cache.t3.micro" || memo.Nodes != 3 || memo.ARN == "" {
		t.Errorf("standalone: %+v", memo)
	}
	if rg.Type != "aws_elasticache_replication_group" || rg.Key != "sessions" || rg.Class != "cache.t3.micro" || rg.Nodes != 2 {
		t.Errorf("replication group: %+v", rg)
	}
}

type fakeS3 []s3types.Bucket

func (f fakeS3) ListBuckets(context.Context, *s3.ListBucketsInput, ...func(*s3.Options)) (*s3.ListBucketsOutput, error) {
	return &s3.ListBucketsOutput{Buckets: f}, nil
}

func TestS3List(t *testing.T) {
	created := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	got, err := S3Buckets{fakeS3{{Name: aws.String("logs"), CreationDate: &created}}}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	b := got[0]
	if b.Type != "aws_s3_bucket" || b.Key != "logs" || b.ARN != "arn:aws:s3:::logs" || b.Region != "global" ||
		b.Note != "size unknown" || b.Created == nil {
		t.Errorf("bucket: %+v", b)
	}
}
