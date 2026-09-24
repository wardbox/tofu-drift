package scan

import (
	"context"
	"math"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
)

// LogGroups lists CloudWatch log groups, sized from storedBytes. Never Idle;
// one whose retention was never set keeps data forever and says so in NOTE.
type LogGroups struct {
	Client cloudwatchlogs.DescribeLogGroupsAPIClient
}

func (LogGroups) Permissions() []string { return []string{"logs:DescribeLogGroups"} }

func (s LogGroups) List(ctx context.Context) ([]LiveResource, error) {
	var out []LiveResource
	p := cloudwatchlogs.NewDescribeLogGroupsPaginator(s.Client, &cloudwatchlogs.DescribeLogGroupsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, g := range page.LogGroups {
			r := LiveResource{
				Type: "aws_cloudwatch_log_group", Key: aws.ToString(g.LogGroupName), ARN: aws.ToString(g.Arn),
				// GiB to 3 decimals, so --explain math reads "4.657 GB".
				SizeGB: math.Round(float64(aws.ToInt64(g.StoredBytes))/(1<<30)*1000) / 1000,
			}
			if g.CreationTime != nil {
				t := time.UnixMilli(*g.CreationTime).UTC()
				r.Created = &t
			}
			if g.RetentionInDays == nil {
				r.Note = "retention never"
			}
			out = append(out, r)
		}
	}
	return out, nil
}
