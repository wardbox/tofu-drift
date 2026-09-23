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

func ec2Tags(ts []types.Tag) map[string]string {
	m := make(map[string]string, len(ts))
	for _, t := range ts {
		m[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return m
}
