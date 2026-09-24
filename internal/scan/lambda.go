package scan

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
)

// LambdaFunctions lists Lambda functions. Never costed, never Idle. The
// function's default log group, /aws/lambda/<name>, is a Derived Resource.
type LambdaFunctions struct {
	Client lambda.ListFunctionsAPIClient
}

func (LambdaFunctions) Permissions() []string { return []string{"lambda:ListFunctions"} }

func (s LambdaFunctions) List(ctx context.Context) ([]LiveResource, error) {
	var out []LiveResource
	p := lambda.NewListFunctionsPaginator(s.Client, &lambda.ListFunctionsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, f := range page.Functions {
			name := aws.ToString(f.FunctionName)
			r := LiveResource{
				Type: "aws_lambda_function", Key: name, ARN: aws.ToString(f.FunctionArn),
				Derived: []string{"/aws/lambda/" + name},
			}
			// Lambda reports no creation time; last modified is the closest age.
			if t, err := time.Parse("2006-01-02T15:04:05.000-0700", aws.ToString(f.LastModified)); err == nil {
				r.Created = &t
			}
			out = append(out, r)
		}
	}
	return out, nil
}
