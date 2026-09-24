// Package match decides whether a live resource is in state, by Match Key
// equality only, and which live resources are Default Furniture.
package match

import (
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
}

// liveType maps state types that adopt AWS-made resources to the live type
// they manage.
var liveType = map[string]string{
	"aws_default_vpc":            "aws_vpc",
	"aws_default_subnet":         "aws_subnet",
	"aws_default_route_table":    "aws_route_table",
	"aws_default_security_group": "aws_security_group",
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

// Roots maps the key of every Derived Resource in live to the key of its
// top-level parent, the one resource it is folded into.
func Roots(live []scan.LiveResource) map[string]string {
	parent := map[string]string{}
	for _, l := range live {
		for _, d := range l.Derived {
			parent[d] = l.Key
		}
	}
	roots := make(map[string]string, len(parent))
	for d := range parent {
		root := d
		for p, ok := parent[root]; ok; p, ok = parent[root] {
			root = p
		}
		roots[d] = root
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
}

func isDefault(r scan.LiveResource) bool { return r.Default }

// Furniture reports whether r is Default Furniture, suppressed unless
// --include-defaults.
func Furniture(r scan.LiveResource) bool {
	f, ok := furniture[r.Type]
	return ok && f(r)
}
