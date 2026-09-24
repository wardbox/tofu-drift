package scan

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// EBS lists EBS volumes. A volume in state "available" is attached to
// nothing and is Idle.
type EBS struct {
	Client ec2.DescribeVolumesAPIClient
}

func (EBS) Permissions() []string { return []string{"ec2:DescribeVolumes"} }

func (s EBS) List(ctx context.Context) ([]LiveResource, error) {
	var out []LiveResource
	p := ec2.NewDescribeVolumesPaginator(s.Client, &ec2.DescribeVolumesInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, v := range page.Volumes {
			tags := ec2Tags(v.Tags)
			r := LiveResource{
				Type:    "aws_ebs_volume",
				Key:     aws.ToString(v.VolumeId),
				Name:    tags["Name"],
				Tags:    tags,
				Created: v.CreateTime,
				Class:   string(v.VolumeType),
				SizeGB:  float64(aws.ToInt32(v.Size)),
			}
			if v.State == types.VolumeStateAvailable {
				r.Idle = "unattached"
			}
			out = append(out, r)
		}
	}
	return out, nil
}

// Snapshots lists the account's own EBS snapshots, at full volume size. One
// whose source volume no longer exists is Idle; a copy, whose source is
// unknown, never is.
type Snapshots struct {
	Client interface {
		ec2.DescribeSnapshotsAPIClient
		ec2.DescribeVolumesAPIClient
	}
}

func (Snapshots) Permissions() []string {
	return []string{"ec2:DescribeSnapshots", "ec2:DescribeVolumes"}
}

func (s Snapshots) List(ctx context.Context) ([]LiveResource, error) {
	volumes := map[string]bool{}
	vp := ec2.NewDescribeVolumesPaginator(s.Client, &ec2.DescribeVolumesInput{})
	for vp.HasMorePages() {
		page, err := vp.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, v := range page.Volumes {
			volumes[aws.ToString(v.VolumeId)] = true
		}
	}
	var out []LiveResource
	p := ec2.NewDescribeSnapshotsPaginator(s.Client, &ec2.DescribeSnapshotsInput{OwnerIds: []string{"self"}})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, v := range page.Snapshots {
			tags := ec2Tags(v.Tags)
			r := LiveResource{
				Type:    "aws_ebs_snapshot",
				Key:     aws.ToString(v.SnapshotId),
				Name:    tags["Name"],
				Tags:    tags,
				Created: v.StartTime,
				SizeGB:  float64(aws.ToInt32(v.VolumeSize)),
			}
			// Copied snapshots carry the placeholder vol-ffffffff: source unknown.
			if src := aws.ToString(v.VolumeId); src != "vol-ffffffff" && !volumes[src] {
				r.Idle = "source volume deleted"
			}
			out = append(out, r)
		}
	}
	return out, nil
}

func ec2Tags(ts []types.Tag) map[string]string {
	m := make(map[string]string, len(ts))
	for _, t := range ts {
		m[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return m
}
