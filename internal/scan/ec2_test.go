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
			Ebs: &types.EbsInstanceBlockDevice{VolumeId: aws.String(v), DeleteOnTermination: aws.Bool(true)},
		})
	}
	return i
}

// attach adds a volume attached after launch, which outlives the instance.
func attach(i types.Instance, volume string) types.Instance {
	i.BlockDeviceMappings = append(i.BlockDeviceMappings, types.InstanceBlockDeviceMapping{
		Ebs: &types.EbsInstanceBlockDevice{VolumeId: aws.String(volume), DeleteOnTermination: aws.Bool(false)},
	})
	return i
}

func TestEC2List(t *testing.T) {
	launched := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	// vol-extra was attached by hand: not Derived, reported on its own.
	web := attach(instance("i-web", types.InstanceStateNameRunning, "vol-root", "vol-data"), "vol-extra")
	web.LaunchTime = &launched
	web.NetworkInterfaces = []types.InstanceNetworkInterface{
		{NetworkInterfaceId: aws.String("eni-primary"), Attachment: &types.InstanceNetworkInterfaceAttachment{DeleteOnTermination: aws.Bool(true)}},
		{NetworkInterfaceId: aws.String("eni-extra"), Attachment: &types.InstanceNetworkInterfaceAttachment{DeleteOnTermination: aws.Bool(false)}},
	}
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
		fmt.Sprint(run.Derived) != "[vol-root vol-data eni-primary]" {
		t.Errorf("running: %+v", run)
	}
	if off.Key != "i-off" || !off.Stopped || fmt.Sprint(off.Derived) != "[vol-off]" {
		t.Errorf("stopped: %+v", off)
	}
	if !stopping.Stopped {
		t.Errorf("stopping costs like stopped: %+v", stopping)
	}
}

// fakeImages serves one page of images over an instance fake.
type fakeImages struct {
	fakeInstances
	images []types.Image
}

func (f fakeImages) DescribeImages(_ context.Context, in *ec2.DescribeImagesInput, _ ...func(*ec2.Options)) (*ec2.DescribeImagesOutput, error) {
	if len(in.Owners) != 1 || in.Owners[0] != "self" {
		return nil, fmt.Errorf("owners %v: want only the account's own", in.Owners)
	}
	return &ec2.DescribeImagesOutput{Images: f.images}, nil
}

func TestAMIsList(t *testing.T) {
	running := instance("i-1", types.InstanceStateNameRunning)
	running.ImageId = aws.String("ami-used")
	gone := instance("i-2", types.InstanceStateNameTerminated)
	gone.ImageId = aws.String("ami-idle")
	client := fakeImages{
		fakeInstances: fakeInstances{"": {Reservations: []types.Reservation{{Instances: []types.Instance{running, gone}}}}},
		images: []types.Image{
			{ImageId: aws.String("ami-idle"), Name: aws.String("golden-2025"), CreationDate: aws.String("2026-01-02T00:00:00.000Z"),
				BlockDeviceMappings: []types.BlockDeviceMapping{{Ebs: &types.EbsBlockDevice{SnapshotId: aws.String("snap-root")}}, {VirtualName: aws.String("ephemeral0")}}},
			{ImageId: aws.String("ami-used"), Tags: nameTag("web")},
		},
	}
	got, err := AMIs{client}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	idle, used := got[0], got[1]
	if idle.Type != "aws_ami" || idle.Key != "ami-idle" || idle.Name != "golden-2025" || idle.Idle != "no instances" ||
		idle.Created == nil || !idle.Created.Equal(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)) ||
		fmt.Sprint(idle.Derived) != "[snap-root]" {
		t.Errorf("idle (only a terminated instance used it): %+v", idle)
	}
	if used.Key != "ami-used" || used.Name != "web" || used.Idle != "" || used.Created != nil {
		t.Errorf("used: %+v", used)
	}
}
