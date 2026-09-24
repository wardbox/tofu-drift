package scan

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
)

// AutoScalingGroups lists Auto Scaling groups. Their instances are Derived
// Resources: the group is one row carrying their cost.
type AutoScalingGroups struct {
	Client autoscaling.DescribeAutoScalingGroupsAPIClient
}

func (AutoScalingGroups) Permissions() []string {
	return []string{"autoscaling:DescribeAutoScalingGroups"}
}

func (s AutoScalingGroups) List(ctx context.Context) ([]LiveResource, error) {
	var out []LiveResource
	p := autoscaling.NewDescribeAutoScalingGroupsPaginator(s.Client, &autoscaling.DescribeAutoScalingGroupsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, g := range page.AutoScalingGroups {
			name := aws.ToString(g.AutoScalingGroupName)
			tags := map[string]string{}
			for _, t := range g.Tags {
				tags[aws.ToString(t.Key)] = aws.ToString(t.Value)
			}
			r := LiveResource{Type: "aws_autoscaling_group", Key: name, ARN: aws.ToString(g.AutoScalingGroupARN),
				Name: name, Tags: tags, Created: g.CreatedTime}
			for _, i := range g.Instances {
				r.Derived = append(r.Derived, aws.ToString(i.InstanceId))
			}
			out = append(out, r)
		}
	}
	return out, nil
}
