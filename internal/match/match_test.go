package match

import (
	"fmt"
	"testing"

	"github.com/wardbox/tofu-drift/internal/scan"
	"github.com/wardbox/tofu-drift/internal/state"
)

// rootKeys is Roots as "type/key" of child to "type/key" of root.
func rootKeys(live []scan.LiveResource) map[string]string {
	m := map[string]string{}
	for c, p := range Roots(live) {
		m[live[c].Type+"/"+live[c].Key] = live[p].Type + "/" + live[p].Key
	}
	return m
}

func TestRoots(t *testing.T) {
	roots := rootKeys([]scan.LiveResource{
		{Key: "asg-1", Derived: []string{"i-1"}},
		{Key: "i-1", Derived: []string{"vol-1"}},
		{Key: "vol-1"},
		{Key: "vol-2"},
	})
	if roots["/vol-1"] != "/asg-1" || roots["/i-1"] != "/asg-1" || len(roots) != 2 {
		t.Errorf("roots: %v", roots)
	}
}

// An ASG sharing its EKS cluster's name folds into the cluster, not itself,
// and a same-named ECS cluster stays apart.
func TestRootsNameClash(t *testing.T) {
	roots := rootKeys([]scan.LiveResource{
		{Type: "aws_eks_cluster", Key: "prod"},
		{Type: "aws_autoscaling_group", Key: "prod", Tags: map[string]string{"eks:cluster-name": "prod"}, Derived: []string{"i-1"}},
		{Type: "aws_instance", Key: "i-1"},
		{Type: "aws_ecs_cluster", Key: "prod"},
	})
	want := map[string]string{"aws_autoscaling_group/prod": "aws_eks_cluster/prod", "aws_instance/i-1": "aws_eks_cluster/prod"}
	if fmt.Sprint(roots) != fmt.Sprint(want) {
		t.Errorf("roots: %v, want %v", roots, want)
	}
}

func TestRootsCycle(t *testing.T) {
	roots := Roots([]scan.LiveResource{
		{Key: "a", Derived: []string{"b"}},
		{Key: "b", Derived: []string{"a"}},
	})
	if len(roots) != 2 {
		t.Errorf("a Derived cycle must terminate: %v", roots)
	}
}

func TestRootsByTag(t *testing.T) {
	eks := map[string]string{"eks:cluster-name": "prod"}
	live := []scan.LiveResource{
		{Type: "aws_eks_cluster", Key: "prod", Derived: []string{"sg-eks"}},
		{Type: "aws_security_group", Key: "sg-eks"},
		// Nodegroup ASG by tag, its instances by attribute.
		{Type: "aws_autoscaling_group", Key: "eks-ng", Tags: eks, Derived: []string{"i-ng"}},
		{Type: "aws_instance", Key: "i-ng", Tags: eks},
		// A node outside any ASG, and ENIs of the control plane and the VPC CNI.
		{Type: "aws_instance", Key: "i-node", Tags: eks},
		{Type: "aws_network_interface", Key: "eni-cp", Name: "Amazon EKS prod"},
		{Type: "aws_network_interface", Key: "eni-cni", Tags: map[string]string{"cluster.k8s.amazonaws.com/name": "prod"}},
		// Tag names a cluster not in live (deleted, or its scanner failed): stands alone.
		{Type: "aws_instance", Key: "i-gone", Tags: map[string]string{"eks:cluster-name": "gone"}},
		{Type: "aws_network_interface", Key: "eni-other", Name: "Amazon EKS gone"},
	}
	got := map[string]string{}
	for c, p := range Roots(live) {
		got[live[c].Key] = live[p].Key
	}
	want := map[string]string{"sg-eks": "prod", "eks-ng": "prod", "i-ng": "prod", "i-node": "prod", "eni-cp": "prod", "eni-cni": "prod"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("roots: %v, want %v", got, want)
	}
}

func TestIndexCompute(t *testing.T) {
	idx := Index([]state.Resource{
		{Address: "aws_autoscaling_group.web", Type: "aws_autoscaling_group", Attributes: map[string]any{"id": "web"}},
		{Address: "aws_eks_cluster.prod", Type: "aws_eks_cluster", Attributes: map[string]any{"id": "prod"}},
		{Address: "aws_ecs_cluster.app", Type: "aws_ecs_cluster", Attributes: map[string]any{"id": "arn:c/app", "name": "app"}},
		{Address: "aws_ecs_service.api", Type: "aws_ecs_service", Attributes: map[string]any{"id": "arn:s/api", "name": "api", "cluster": "arn:aws:ecs:us-east-1:1:cluster/app"}},
		{Address: "aws_ecs_service.byname", Type: "aws_ecs_service", Attributes: map[string]any{"name": "web", "cluster": "app"}},
	})
	for typ, want := range map[[2]string]string{
		{"aws_autoscaling_group", "web"}: "aws_autoscaling_group.web",
		{"aws_eks_cluster", "prod"}:      "aws_eks_cluster.prod",
		{"aws_ecs_cluster", "app"}:       "aws_ecs_cluster.app",
		{"aws_ecs_service", "app/api"}:   "aws_ecs_service.api",
		{"aws_ecs_service", "app/web"}:   "aws_ecs_service.byname",
	} {
		if got := idx.Lookup(typ[0], typ[1]); got != want {
			t.Errorf("%v: got %q, want %q", typ, got, want)
		}
	}
}

func TestIndex(t *testing.T) {
	idx := Index([]state.Resource{
		{Address: "aws_ebs_volume.a", Type: "aws_ebs_volume", Attributes: map[string]any{"id": "vol-1"}},
		{Address: "aws_ebs_volume.noid", Type: "aws_ebs_volume", Attributes: map[string]any{}},
		{Address: "aws_widget.x", Type: "aws_widget", Attributes: map[string]any{"id": "vol-2"}},
	})
	if got := idx.Lookup("aws_ebs_volume", "vol-1"); got != "aws_ebs_volume.a" {
		t.Errorf("vol-1: got %q", got)
	}
	if got := idx.Lookup("aws_ebs_volume", "vol-2"); got != "" {
		t.Errorf("unknown state types must be ignored, got %q", got)
	}
	if got := idx.Lookup("aws_ebs_volume", ""); got != "" {
		t.Errorf("empty key must never match, got %q", got)
	}
}

func TestIndexPlumbing(t *testing.T) {
	idx := Index([]state.Resource{
		{Address: "aws_vpc.app", Type: "aws_vpc", Attributes: map[string]any{"id": "vpc-1"}},
		{Address: "aws_subnet.a", Type: "aws_subnet", Attributes: map[string]any{"id": "subnet-1"}},
		{Address: "aws_route_table.a", Type: "aws_route_table", Attributes: map[string]any{"id": "rtb-1"}},
		{Address: "aws_security_group.web", Type: "aws_security_group", Attributes: map[string]any{"id": "sg-1"}},
		{Address: "aws_default_security_group.d", Type: "aws_default_security_group", Attributes: map[string]any{"id": "sg-d"}},
		{Address: "aws_default_route_table.d", Type: "aws_default_route_table", Attributes: map[string]any{"id": "rtb-d"}},
	})
	for typ, want := range map[[2]string]string{
		{"aws_vpc", "vpc-1"}:           "aws_vpc.app",
		{"aws_subnet", "subnet-1"}:     "aws_subnet.a",
		{"aws_route_table", "rtb-1"}:   "aws_route_table.a",
		{"aws_security_group", "sg-1"}: "aws_security_group.web",
		{"aws_security_group", "sg-d"}: "aws_default_security_group.d",
		{"aws_route_table", "rtb-d"}:   "aws_default_route_table.d",
	} {
		if got := idx.Lookup(typ[0], typ[1]); got != want {
			t.Errorf("%v: got %q, want %q", typ, got, want)
		}
	}
}

func TestIndexFlatRate(t *testing.T) {
	idx := Index([]state.Resource{
		{Address: "aws_eip.nat", Type: "aws_eip", Attributes: map[string]any{"id": "eipalloc-1"}},
		{Address: "aws_nat_gateway.a", Type: "aws_nat_gateway", Attributes: map[string]any{"id": "nat-1"}},
		{Address: "aws_network_interface.x", Type: "aws_network_interface", Attributes: map[string]any{"id": "eni-1"}},
		{Address: "aws_lb.web", Type: "aws_lb", Attributes: map[string]any{"id": "arn:lb/web", "arn": "arn:lb/web"}},
		{Address: "aws_alb.old", Type: "aws_alb", Attributes: map[string]any{"arn": "arn:lb/old"}},
		{Address: "aws_elb.legacy", Type: "aws_elb", Attributes: map[string]any{"id": "legacy", "name": "legacy"}},
		{Address: "aws_ebs_snapshot.s", Type: "aws_ebs_snapshot", Attributes: map[string]any{"id": "snap-1"}},
		{Address: "aws_ebs_snapshot_copy.c", Type: "aws_ebs_snapshot_copy", Attributes: map[string]any{"id": "snap-2"}},
		{Address: "aws_ami.a", Type: "aws_ami", Attributes: map[string]any{"id": "ami-1"}},
		{Address: "aws_ami_copy.c", Type: "aws_ami_copy", Attributes: map[string]any{"id": "ami-2"}},
		{Address: "aws_ami_from_instance.i", Type: "aws_ami_from_instance", Attributes: map[string]any{"id": "ami-3"}},
	})
	for typ, want := range map[[2]string]string{
		{"aws_eip", "eipalloc-1"}:          "aws_eip.nat",
		{"aws_nat_gateway", "nat-1"}:       "aws_nat_gateway.a",
		{"aws_network_interface", "eni-1"}: "aws_network_interface.x",
		{"aws_lb", "arn:lb/web"}:           "aws_lb.web",
		{"aws_lb", "arn:lb/old"}:           "aws_alb.old",
		{"aws_elb", "legacy"}:              "aws_elb.legacy",
		{"aws_ebs_snapshot", "snap-1"}:     "aws_ebs_snapshot.s",
		{"aws_ebs_snapshot", "snap-2"}:     "aws_ebs_snapshot_copy.c",
		{"aws_ami", "ami-1"}:               "aws_ami.a",
		{"aws_ami", "ami-2"}:               "aws_ami_copy.c",
		{"aws_ami", "ami-3"}:               "aws_ami_from_instance.i",
	} {
		if got := idx.Lookup(typ[0], typ[1]); got != want {
			t.Errorf("%v: got %q, want %q", typ, got, want)
		}
	}
}

func TestIndexData(t *testing.T) {
	idx := Index([]state.Resource{
		// provider v5+: id is the resource id, identifier the name
		{Address: "aws_db_instance.app", Type: "aws_db_instance", Attributes: map[string]any{"id": "db-ABC", "identifier": "app"}},
		{Address: "aws_rds_cluster_instance.a", Type: "aws_rds_cluster_instance", Attributes: map[string]any{"id": "aurora-1", "identifier": "aurora-1"}},
		{Address: "aws_db_snapshot.s", Type: "aws_db_snapshot", Attributes: map[string]any{"id": "before-upgrade", "db_snapshot_identifier": "before-upgrade"}},
		{Address: "aws_dynamodb_table.t", Type: "aws_dynamodb_table", Attributes: map[string]any{"id": "users", "name": "users"}},
		{Address: "aws_elasticache_cluster.c", Type: "aws_elasticache_cluster", Attributes: map[string]any{"id": "memo", "cluster_id": "memo"}},
		{Address: "aws_elasticache_replication_group.g", Type: "aws_elasticache_replication_group", Attributes: map[string]any{"id": "sessions", "replication_group_id": "sessions"}},
		{Address: "aws_s3_bucket.logs", Type: "aws_s3_bucket", Attributes: map[string]any{"id": "logs", "bucket": "logs"}},
	})
	for typ, want := range map[[2]string]string{
		{"aws_db_instance", "app"}:                        "aws_db_instance.app",
		{"aws_db_instance", "aurora-1"}:                   "aws_rds_cluster_instance.a",
		{"aws_db_snapshot", "before-upgrade"}:             "aws_db_snapshot.s",
		{"aws_dynamodb_table", "users"}:                   "aws_dynamodb_table.t",
		{"aws_elasticache_cluster", "memo"}:               "aws_elasticache_cluster.c",
		{"aws_elasticache_replication_group", "sessions"}: "aws_elasticache_replication_group.g",
		{"aws_s3_bucket", "logs"}:                         "aws_s3_bucket.logs",
	} {
		if got := idx.Lookup(typ[0], typ[1]); got != want {
			t.Errorf("%v: got %q, want %q", typ, got, want)
		}
	}
}

func TestFurniture(t *testing.T) {
	for _, tc := range []struct {
		r    scan.LiveResource
		want bool
	}{
		{scan.LiveResource{Type: "aws_vpc", Default: true}, true},
		{scan.LiveResource{Type: "aws_subnet", Default: true}, true},
		{scan.LiveResource{Type: "aws_route_table", Default: true}, true},
		{scan.LiveResource{Type: "aws_security_group", Default: true}, true},
		{scan.LiveResource{Type: "aws_security_group"}, false},
		{scan.LiveResource{Type: "aws_ecs_cluster", Name: "default", Default: true}, true},
		{scan.LiveResource{Type: "aws_ecs_cluster", Name: "app"}, false},
		{scan.LiveResource{Type: "aws_iam_role", Default: true}, true},
		{scan.LiveResource{Type: "aws_iam_role"}, false},
		// Default only means furniture for types in the table.
		{scan.LiveResource{Type: "aws_ebs_volume", Default: true}, false},
	} {
		if got := Furniture(tc.r); got != tc.want {
			t.Errorf("%+v: got %v, want %v", tc.r, got, tc.want)
		}
	}
}

func TestIndexGlobal(t *testing.T) {
	idx := Index([]state.Resource{
		{Address: "aws_iam_role.app", Type: "aws_iam_role", Attributes: map[string]any{"id": "app", "name": "app"}},
		{Address: "aws_iam_service_linked_role.es", Type: "aws_iam_service_linked_role", Attributes: map[string]any{"id": "arn:aws:iam::1:role/aws-service-role/es.amazonaws.com/AWSServiceRoleForAmazonOpenSearchService", "name": "AWSServiceRoleForAmazonOpenSearchService"}},
		{Address: "aws_iam_user.ci", Type: "aws_iam_user", Attributes: map[string]any{"id": "ci", "name": "ci"}},
		{Address: "aws_route53_zone.main", Type: "aws_route53_zone", Attributes: map[string]any{"id": "Z123", "zone_id": "Z123"}},
		{Address: "aws_lambda_function.resize", Type: "aws_lambda_function", Attributes: map[string]any{"id": "resize", "function_name": "resize"}},
		{Address: "aws_cloudwatch_log_group.app", Type: "aws_cloudwatch_log_group", Attributes: map[string]any{"id": "app", "name": "app"}},
	})
	for typ, want := range map[[2]string]string{
		{"aws_iam_role", "app"}: "aws_iam_role.app",
		{"aws_iam_role", "AWSServiceRoleForAmazonOpenSearchService"}: "aws_iam_service_linked_role.es",
		{"aws_iam_user", "ci"}:              "aws_iam_user.ci",
		{"aws_route53_zone", "Z123"}:        "aws_route53_zone.main",
		{"aws_lambda_function", "resize"}:   "aws_lambda_function.resize",
		{"aws_cloudwatch_log_group", "app"}: "aws_cloudwatch_log_group.app",
	} {
		if got := idx.Lookup(typ[0], typ[1]); got != want {
			t.Errorf("%v: got %q, want %q", typ, got, want)
		}
	}
}
