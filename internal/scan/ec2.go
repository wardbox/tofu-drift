package scan

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// EC2 lists EC2 instances. EBS volumes and ENIs created with the instance are
// Derived Resources: a stopped instance costs only those volumes.
type EC2 struct {
	Client ec2.DescribeInstancesAPIClient
}

func (EC2) Permissions() []string { return []string{"ec2:DescribeInstances"} }

func (s EC2) List(ctx context.Context) ([]LiveResource, error) {
	var out []LiveResource
	p := ec2.NewDescribeInstancesPaginator(s.Client, &ec2.DescribeInstancesInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, res := range page.Reservations {
			for _, i := range res.Instances {
				var state types.InstanceStateName
				if i.State != nil {
					state = i.State.Name
				}
				if state == types.InstanceStateNameTerminated || state == types.InstanceStateNameShuttingDown {
					continue
				}
				tags := ec2Tags(i.Tags)
				r := LiveResource{
					Type:    "aws_instance",
					Key:     aws.ToString(i.InstanceId),
					Name:    tags["Name"],
					Tags:    tags,
					Created: i.LaunchTime,
					Class:   string(i.InstanceType),
					Stopped: state == types.InstanceStateNameStopped || state == types.InstanceStateNameStopping,
				}
				for _, b := range i.BlockDeviceMappings {
					// Only volumes created with the instance are Derived; ones
					// attached later stand alone and are matched on their own.
					if b.Ebs != nil && aws.ToBool(b.Ebs.DeleteOnTermination) {
						r.Derived = append(r.Derived, aws.ToString(b.Ebs.VolumeId))
					}
				}
				// Same rule for ENIs: the primary one is created with the instance.
				for _, n := range i.NetworkInterfaces {
					if n.Attachment != nil && aws.ToBool(n.Attachment.DeleteOnTermination) {
						r.Derived = append(r.Derived, aws.ToString(n.NetworkInterfaceId))
					}
				}
				out = append(out, r)
			}
		}
	}
	return out, nil
}

// AMIs lists the account's own images. One no live instance was launched
// from is Idle. Its EBS snapshots are Derived Resources: they carry the cost.
type AMIs struct {
	Client interface {
		ec2.DescribeImagesAPIClient
		ec2.DescribeInstancesAPIClient
	}
}

func (AMIs) Permissions() []string { return []string{"ec2:DescribeImages", "ec2:DescribeInstances"} }

func (s AMIs) List(ctx context.Context) ([]LiveResource, error) {
	used := map[string]bool{}
	ip := ec2.NewDescribeInstancesPaginator(s.Client, &ec2.DescribeInstancesInput{})
	for ip.HasMorePages() {
		page, err := ip.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, res := range page.Reservations {
			for _, i := range res.Instances {
				if i.State == nil || i.State.Name != types.InstanceStateNameTerminated {
					used[aws.ToString(i.ImageId)] = true
				}
			}
		}
	}
	var out []LiveResource
	p := ec2.NewDescribeImagesPaginator(s.Client, &ec2.DescribeImagesInput{Owners: []string{"self"}})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, img := range page.Images {
			tags := ec2Tags(img.Tags)
			r := LiveResource{Type: "aws_ami", Key: aws.ToString(img.ImageId), Name: tags["Name"], Tags: tags}
			if r.Name == "" {
				r.Name = aws.ToString(img.Name)
			}
			if t, err := time.Parse(time.RFC3339, aws.ToString(img.CreationDate)); err == nil {
				r.Created = &t
			}
			if !used[r.Key] {
				r.Idle = "no instances"
			}
			for _, b := range img.BlockDeviceMappings {
				if b.Ebs != nil && b.Ebs.SnapshotId != nil {
					r.Derived = append(r.Derived, *b.Ebs.SnapshotId)
				}
			}
			out = append(out, r)
		}
	}
	return out, nil
}
