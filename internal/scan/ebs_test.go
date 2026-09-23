package scan

import (
	"context"
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
