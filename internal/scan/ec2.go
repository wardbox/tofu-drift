package scan

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// EC2 lists EC2 instances. EBS volumes created with the instance are Derived
// Resources: a stopped instance costs only those volumes.
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
				out = append(out, r)
			}
		}
	}
	return out, nil
}
