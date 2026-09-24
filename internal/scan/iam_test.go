package scan

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
)

type fakeIAM struct {
	roles []types.Role
	users []types.User
}

func (f fakeIAM) ListRoles(context.Context, *iam.ListRolesInput, ...func(*iam.Options)) (*iam.ListRolesOutput, error) {
	return &iam.ListRolesOutput{Roles: f.roles}, nil
}

func (f fakeIAM) ListUsers(context.Context, *iam.ListUsersInput, ...func(*iam.Options)) (*iam.ListUsersOutput, error) {
	return &iam.ListUsersOutput{Users: f.users}, nil
}

func TestIAMRolesList(t *testing.T) {
	created := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	got, err := IAMRoles{fakeIAM{roles: []types.Role{
		{RoleName: aws.String("app"), Path: aws.String("/"), Arn: aws.String("arn:aws:iam::1:role/app"), CreateDate: &created},
		{RoleName: aws.String("AWSServiceRoleForRDS"), Path: aws.String("/aws-service-role/rds.amazonaws.com/")},
		{RoleName: aws.String("AWSServiceRoleForSupport"), Path: aws.String("/")},
		{RoleName: aws.String("lambda-exec"), Path: aws.String("/service-role/")},
	}}}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %+v", got)
	}
	app := got[0]
	if app.Type != "aws_iam_role" || app.Key != "app" || app.ARN != "arn:aws:iam::1:role/app" || !app.Created.Equal(created) || app.Default || app.Region != "global" {
		t.Errorf("app: %+v", app)
	}
	if !got[1].Default || !got[2].Default {
		t.Errorf("service-linked roles must be Default: %+v %+v", got[1], got[2])
	}
	if got[3].Default {
		t.Errorf("/service-role/ is user-made: %+v", got[3])
	}
}

func TestIAMUsersList(t *testing.T) {
	got, err := IAMUsers{fakeIAM{users: []types.User{
		{UserName: aws.String("ci"), Arn: aws.String("arn:aws:iam::1:user/ci")},
	}}}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != "aws_iam_user" || got[0].Key != "ci" || got[0].ARN != "arn:aws:iam::1:user/ci" || got[0].Region != "global" {
		t.Errorf("got %+v", got)
	}
}
