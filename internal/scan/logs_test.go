package scan

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

type fakeLogGroups []types.LogGroup

func (f fakeLogGroups) DescribeLogGroups(context.Context, *cloudwatchlogs.DescribeLogGroupsInput, ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.DescribeLogGroupsOutput, error) {
	return &cloudwatchlogs.DescribeLogGroupsOutput{LogGroups: f}, nil
}

func TestLogGroupsList(t *testing.T) {
	created := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	got, err := LogGroups{fakeLogGroups{
		{LogGroupName: aws.String("/aws/lambda/resize"), StoredBytes: aws.Int64(5<<30 + 5e6), CreationTime: aws.Int64(created.UnixMilli())},
		{LogGroupName: aws.String("app"), StoredBytes: aws.Int64(0), RetentionInDays: aws.Int32(30)},
	}}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	lg, app := got[0], got[1]
	if lg.Type != "aws_cloudwatch_log_group" || lg.Key != "/aws/lambda/resize" || lg.SizeGB != 5.005 ||
		!lg.Created.Equal(created) || lg.Note != "retention never" {
		t.Errorf("never-expiring group: %+v", lg)
	}
	if app.Note != "" || app.Created != nil {
		t.Errorf("group with retention: %+v", app)
	}
}
