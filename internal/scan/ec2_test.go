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

// fakeInstances serves canned DescribeInstances pages, keyed by NextToken.
type fakeInstances map[string]*ec2.DescribeInstancesOutput

func (f fakeInstances) DescribeInstances(_ context.Context, in *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	return f[aws.ToString(in.NextToken)], nil
}

func instance(id string, state types.InstanceStateName, volumes ...string) types.Instance {
	i := types.Instance{
		InstanceId:   aws.String(id),
		InstanceType: types.InstanceTypeT3Large,
		State:        &types.InstanceState{Name: state},
	}
	for _, v := range volumes {
		i.BlockDeviceMappings = append(i.BlockDeviceMappings, types.InstanceBlockDeviceMapping{
			Ebs: &types.EbsInstanceBlockDevice{VolumeId: aws.String(v)},
		})
	}
	return i
}

func TestEC2List(t *testing.T) {
	launched := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	web := instance("i-web", types.InstanceStateNameRunning, "vol-root", "vol-data")
	web.LaunchTime = &launched
	web.Tags = []types.Tag{{Key: aws.String("Name"), Value: aws.String("web")}}
	client := fakeInstances{
		"": {
			Reservations: []types.Reservation{{Instances: []types.Instance{
				web,
				instance("i-gone", types.InstanceStateNameTerminated),
			}}},
			NextToken: aws.String("p2"),
		},
		"p2": {
			Reservations: []types.Reservation{{Instances: []types.Instance{
				instance("i-off", types.InstanceStateNameStopped, "vol-off"),
				instance("i-stopping", types.InstanceStateNameStopping),
			}}},
		},
	}

	got, err := EC2{Client: client}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d instances, want 3 (both pages, terminated skipped): %+v", len(got), got)
	}
	run, off, stopping := got[0], got[1], got[2]
	if run.Type != "aws_instance" || run.Key != "i-web" || run.Name != "web" || run.Class != "t3.large" ||
		run.Stopped || run.Created == nil || !run.Created.Equal(launched) || run.Idle != "" ||
		fmt.Sprint(run.Derived) != "[vol-root vol-data]" {
		t.Errorf("running: %+v", run)
	}
	if off.Key != "i-off" || !off.Stopped || fmt.Sprint(off.Derived) != "[vol-off]" {
		t.Errorf("stopped: %+v", off)
	}
	if !stopping.Stopped {
		t.Errorf("stopping bills like stopped: %+v", stopping)
	}
}
