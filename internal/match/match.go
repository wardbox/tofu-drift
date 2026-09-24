// Package match decides whether a live resource is in state, by Match Key
// equality only, and which live resources are Default Furniture.
package match

import (
	"strings"

	"github.com/wardbox/tofu-drift/internal/scan"
	"github.com/wardbox/tofu-drift/internal/state"
)

// keyFor maps a state resource type to its Match Key, derived from state
// attributes. Types absent here are ignored.
var keyFor = map[string]func(attrs map[string]any) string{
	"aws_ebs_volume":             attr("id"),
	"aws_instance":               attr("id"),
	"aws_vpc":                    attr("id"),
	"aws_subnet":                 attr("id"),
	"aws_route_table":            attr("id"),
	"aws_security_group":         attr("id"),
	"aws_default_vpc":            attr("id"),
	"aws_default_subnet":         attr("id"),
	"aws_default_route_table":    attr("id"),
	"aws_default_security_group": attr("id"),
	"aws_eip":                    attr("id"),
	"aws_nat_gateway":            attr("id"),
	"aws_network_interface":      attr("id"),
	"aws_lb":                     attr("arn"),
	"aws_alb":                    attr("arn"),
	"aws_elb":                    attr("name"),
	"aws_ebs_snapshot":           attr("id"),
	"aws_ebs_snapshot_copy":      attr("id"),
	"aws_ami":                    attr("id"),
	"aws_ami_copy":               attr("id"),
	"aws_ami_from_instance":      attr("id"),

	"aws_db_instance":                   attr("identifier"),
	"aws_rds_cluster_instance":          attr("identifier"),
	"aws_db_snapshot":                   attr("db_snapshot_identifier"),
	"aws_dynamodb_table":                attr("name"),
	"aws_elasticache_cluster":           attr("cluster_id"),
	"aws_elasticache_replication_group": attr("replication_group_id"),
	"aws_s3_bucket":                     attr("bucket"),

	"aws_autoscaling_group": attr("id"),
	"aws_eks_cluster":       attr("id"),
	"aws_ecs_cluster":       attr("name"),
	"aws_ecs_service": func(a map[string]any) string {
		if attr("name")(a) == "" {
			return ""
		}
		return scan.ECSServiceKey(attr("cluster")(a), attr("name")(a))
	},
}

// liveType maps state types that adopt AWS-made resources, or are aliases or
// other ways of creating one, to the live type they manage.
var liveType = map[string]string{
	"aws_alb":                    "aws_lb",
	"aws_ebs_snapshot_copy":      "aws_ebs_snapshot",
	"aws_ami_copy":               "aws_ami",
	"aws_ami_from_instance":      "aws_ami",
	"aws_default_vpc":            "aws_vpc",
	"aws_default_subnet":         "aws_subnet",
	"aws_default_route_table":    "aws_route_table",
	"aws_default_security_group": "aws_security_group",
	"aws_rds_cluster_instance":   "aws_db_instance",
}

func attr(name string) func(map[string]any) string {
	return func(a map[string]any) string { s, _ := a[name].(string); return s }
}

type key struct{ typ, matchKey string }

// Managed maps type and Match Key to the state address.
type Managed map[key]string

// Index builds the Match Key index of the resources in state.
func Index(rs []state.Resource) Managed {
	m := Managed{}
	for _, r := range rs {
		if f, ok := keyFor[r.Type]; ok {
			if k := f(r.Attributes); k != "" {
				typ := r.Type
				if t, ok := liveType[typ]; ok {
					typ = t
				}
				m[key{typ, k}] = r.Address
			}
		}
	}
	return m
}

// parentBy finds, per Derived Resource type, a parent the resource does not
// list itself: the parent's type and the Match Key AWS stamped on the child
// as a tag or name. A parent a scanner lists as Derived wins over these.
var parentBy = map[string]func(scan.LiveResource) (typ, key string){
	// Managed nodegroup ASGs and nodes carry the cluster name.
	"aws_autoscaling_group": tag("aws_eks_cluster", "eks:cluster-name"),
	"aws_instance":          tag("aws_eks_cluster", "eks:cluster-name"),
	"aws_network_interface": func(r scan.LiveResource) (string, string) {
		// Control-plane ENIs are described "Amazon EKS <cluster>".
		if c, ok := strings.CutPrefix(r.Name, "Amazon EKS "); ok {
			return "aws_eks_cluster", c
		}
		// VPC CNI ENIs.
		return tag("aws_eks_cluster", "cluster.k8s.amazonaws.com/name")(r)
	},
}

func tag(typ, name string) func(scan.LiveResource) (string, string) {
	return func(r scan.LiveResource) (string, string) { return typ, r.Tags[name] }
}

// Roots maps the index in live of every Derived Resource to the index of its
// top-level parent, the one resource it is folded into. Indexes, not Match
// Keys: names key several types (ASG, EKS cluster, ECS cluster) and can clash.
func Roots(live []scan.LiveResource) map[int]int {
	at := map[key]int{}
	// Derived lists hold untyped AWS IDs (i-, vol-, sg-, eni-, ...).
	byKey := map[string]int{}
	for i, l := range live {
		at[key{l.Type, l.Key}] = i
		byKey[l.Key] = i
	}
	parent := map[int]int{}
	for i, l := range live {
		for _, d := range l.Derived {
			if c, ok := byKey[d]; ok && c != i {
				parent[c] = i
			}
		}
	}
	for i, l := range live {
		f, ok := parentBy[l.Type]
		if _, listed := parent[i]; !ok || listed {
			continue
		}
		// Only fold into a parent that is live, or the child would vanish.
		if typ, k := f(l); k != "" {
			if p, ok := at[key{typ, k}]; ok && p != i {
				parent[i] = p
			}
		}
	}
	roots := make(map[int]int, len(parent))
	for c := range parent {
		root := c
		// Bounded walk: a cycle of Derived links folds into wherever it stops.
		for n := 0; n < len(live); n++ {
			p, ok := parent[root]
			if !ok {
				break
			}
			root = p
		}
		roots[c] = root
	}
	return roots
}

// Lookup returns the state address for a live resource, or "" if Unmanaged.
func (m Managed) Lookup(typ, k string) string { return m[key{typ, k}] }

// furniture decides, per live type, whether a resource is Default Furniture.
// Types absent here never are.
var furniture = map[string]func(scan.LiveResource) bool{
	"aws_vpc":            isDefault,
	"aws_subnet":         isDefault,
	"aws_route_table":    isDefault,
	"aws_security_group": isDefault,
	"aws_ecs_cluster":    isDefault,
}

func isDefault(r scan.LiveResource) bool { return r.Default }

// Furniture reports whether r is Default Furniture, suppressed unless
// --include-defaults.
func Furniture(r scan.LiveResource) bool {
	f, ok := furniture[r.Type]
	return ok && f(r)
}
