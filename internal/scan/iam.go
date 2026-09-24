package scan

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

// IAMRoles lists IAM roles. Global. Service-linked roles are Default
// Furniture. Never costed, never Idle.
type IAMRoles struct {
	Client iam.ListRolesAPIClient
}

func (IAMRoles) Permissions() []string { return []string{"iam:ListRoles"} }

func (s IAMRoles) List(ctx context.Context) ([]LiveResource, error) {
	var out []LiveResource
	p := iam.NewListRolesPaginator(s.Client, &iam.ListRolesInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, r := range page.Roles {
			name := aws.ToString(r.RoleName)
			out = append(out, LiveResource{
				Type: "aws_iam_role", Key: name, ARN: aws.ToString(r.Arn), Created: r.CreateDate, Region: "global",
				Default: strings.HasPrefix(aws.ToString(r.Path), "/aws-service-role/") || strings.HasPrefix(name, "AWSServiceRoleFor"),
			})
		}
	}
	return out, nil
}

// IAMUsers lists IAM users. Global. Never costed, never Idle.
type IAMUsers struct {
	Client iam.ListUsersAPIClient
}

func (IAMUsers) Permissions() []string { return []string{"iam:ListUsers"} }

func (s IAMUsers) List(ctx context.Context) ([]LiveResource, error) {
	var out []LiveResource
	p := iam.NewListUsersPaginator(s.Client, &iam.ListUsersInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, u := range page.Users {
			out = append(out, LiveResource{Type: "aws_iam_user", Key: aws.ToString(u.UserName), ARN: aws.ToString(u.Arn), Created: u.CreateDate, Region: "global"})
		}
	}
	return out, nil
}
