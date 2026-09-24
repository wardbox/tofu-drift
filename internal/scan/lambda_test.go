package scan

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

type fakeFunctions []types.FunctionConfiguration

func (f fakeFunctions) ListFunctions(context.Context, *lambda.ListFunctionsInput, ...func(*lambda.Options)) (*lambda.ListFunctionsOutput, error) {
	return &lambda.ListFunctionsOutput{Functions: f}, nil
}

func TestLambdaList(t *testing.T) {
	got, err := LambdaFunctions{fakeFunctions{
		{FunctionName: aws.String("resize"), FunctionArn: aws.String("arn:aws:lambda:us-east-1:1:function:resize"), LastModified: aws.String("2026-01-02T03:04:05.000+0000")},
	}}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if f := got[0]; f.Type != "aws_lambda_function" || f.Key != "resize" || fmt.Sprint(f.Derived) != "[/aws/lambda/resize]" || f.Region != "" ||
		f.Created == nil || !f.Created.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("function: %+v", f)
	}
}
