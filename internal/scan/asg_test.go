package scan

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
)

type fakeASG []types.AutoScalingGroup

func (f fakeASG) DescribeAutoScalingGroups(context.Context, *autoscaling.DescribeAutoScalingGroupsInput, ...func(*autoscaling.Options)) (*autoscaling.DescribeAutoScalingGroupsOutput, error) {
	return &autoscaling.DescribeAutoScalingGroupsOutput{AutoScalingGroups: f}, nil
}

func TestAutoScalingGroupsList(t *testing.T) {
	got, err := AutoScalingGroups{fakeASG{{
		AutoScalingGroupName: aws.String("web"),
		AutoScalingGroupARN:  aws.String("arn:asg/web"),
		CreatedTime:          aws.Time(time.Unix(0, 0)),
		Tags:                 []types.TagDescription{{Key: aws.String("eks:cluster-name"), Value: aws.String("prod")}},
		Instances:            []types.Instance{{InstanceId: aws.String("i-1")}, {InstanceId: aws.String("i-2")}},
	}}}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	g := got[0]
	if g.Type != "aws_autoscaling_group" || g.Key != "web" || g.ARN != "arn:asg/web" || g.Name != "web" || g.Created == nil ||
		g.Tags["eks:cluster-name"] != "prod" || len(g.Derived) != 2 || g.Derived[1] != "i-2" {
		t.Errorf("asg: %+v", g)
	}
}
