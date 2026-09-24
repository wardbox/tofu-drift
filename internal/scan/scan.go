// Package scan lists live AWS resources. One file per service, each
// implementing Scanner against a narrow SDK client interface.
package scan

import (
	"context"
	"time"
)

// Scanner lists the live resources of one service in one region.
type Scanner interface {
	List(ctx context.Context) ([]LiveResource, error)
	// Permissions are the read-only IAM actions List needs.
	Permissions() []string
}

// LiveResource is one resource found in the account.
type LiveResource struct {
	// Type is the OpenTofu resource type, e.g. aws_ebs_volume.
	Type string
	// Key is the Match Key: the per-type canonical identifier, also the Finding ID.
	Key     string
	ARN     string
	Name    string
	Tags    map[string]string
	Created *time.Time
	// Idle is the idle reason, empty when the resource is in use.
	Idle string
	// Derived are the Match Keys of Derived Resources folded into this one.
	Derived []string
	// Default is AWS's own marker that it created the resource: default VPC,
	// default-for-AZ subnet, main route table, default security group,
	// service-linked IAM role.
	Default bool

	// Cost inputs.
	Class  string // volume type, instance class, ...
	SizeGB float64
	// Stopped instances cost no compute.
	Stopped bool
	// Nodes is how many instances of Class are billed (Multi-AZ standby,
	// cache nodes); 0 means 1.
	Nodes int

	// Region overrides the scanned region, "global" for global services.
	Region string
	// Note is shown with the Finding, e.g. why its cost is $0.
	Note string
}
