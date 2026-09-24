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

// fakeEC2 serves canned DescribeVolumes pages, keyed by NextToken.
type fakeEC2 map[string]*ec2.DescribeVolumesOutput

func (f fakeEC2) DescribeVolumes(_ context.Context, in *ec2.DescribeVolumesInput, _ ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error) {
	return f[aws.ToString(in.NextToken)], nil
}

func TestEBSList(t *testing.T) {
	created := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	client := fakeEC2{
		"": {
			Volumes: []types.Volume{{
				VolumeId:   aws.String("vol-idle"),
				State:      types.VolumeStateAvailable,
				VolumeType: types.VolumeTypeGp3,
				Size:       aws.Int32(100),
				CreateTime: &created,
				Tags:       []types.Tag{{Key: aws.String("Name"), Value: aws.String("scratch")}},
			}},
			NextToken: aws.String("p2"),
		},
		"p2": {
			Volumes: []types.Volume{{
				VolumeId:   aws.String("vol-used"),
				State:      types.VolumeStateInUse,
				VolumeType: types.VolumeTypeGp2,
				Size:       aws.Int32(8),
			}},
		},
	}

	got, err := EBS{Client: client}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d volumes, want 2 (both pages)", len(got))
	}
	idle, used := got[0], got[1]
	if idle.Type != "aws_ebs_volume" || idle.Key != "vol-idle" || idle.Name != "scratch" ||
		idle.Idle != "unattached" || idle.Class != "gp3" || idle.SizeGB != 100 ||
		idle.Created == nil || !idle.Created.Equal(created) || idle.Tags["Name"] != "scratch" {
		t.Errorf("idle volume: %+v", idle)
	}
	if used.Key != "vol-used" || used.Idle != "" || used.Created != nil {
		t.Errorf("in-use volume: %+v", used)
	}
}

// fakeSnapshots serves one page of snapshots over a volume fake.
type fakeSnapshots struct {
	fakeEC2
	snaps []types.Snapshot
}

func (f fakeSnapshots) DescribeSnapshots(_ context.Context, in *ec2.DescribeSnapshotsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSnapshotsOutput, error) {
	if len(in.OwnerIds) != 1 || in.OwnerIds[0] != "self" {
		return nil, fmt.Errorf("owners %v: want only the account's own", in.OwnerIds)
	}
	return &ec2.DescribeSnapshotsOutput{Snapshots: f.snaps}, nil
}

func TestSnapshotsList(t *testing.T) {
	client := fakeSnapshots{
		fakeEC2: fakeEC2{"": {Volumes: []types.Volume{{VolumeId: aws.String("vol-live")}}}},
		snaps: []types.Snapshot{
			{SnapshotId: aws.String("snap-gone"), VolumeId: aws.String("vol-gone"), VolumeSize: aws.Int32(100), Tags: nameTag("old")},
			{SnapshotId: aws.String("snap-live"), VolumeId: aws.String("vol-live"), VolumeSize: aws.Int32(8)},
			{SnapshotId: aws.String("snap-copy"), VolumeId: aws.String("vol-ffffffff"), VolumeSize: aws.Int32(8)},
		},
	}
	got, err := Snapshots{client}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %+v", got)
	}
	idle, live, cp := got[0], got[1], got[2]
	if idle.Type != "aws_ebs_snapshot" || idle.Key != "snap-gone" || idle.Name != "old" ||
		idle.SizeGB != 100 || idle.Idle != "source volume deleted" {
		t.Errorf("source deleted: %+v", idle)
	}
	if live.Key != "snap-live" || live.Idle != "" {
		t.Errorf("live source: %+v", live)
	}
	if cp.Idle != "" {
		t.Errorf("copy, source unknown, is never idle: %+v", cp)
	}
}
