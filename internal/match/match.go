// Package match decides whether a live resource is in state, by Match Key
// equality only.
package match

import "github.com/wardbox/tofu-drift/internal/state"

// keyFor maps a state resource type to its Match Key, derived from state
// attributes. Types absent here are ignored.
var keyFor = map[string]func(attrs map[string]any) string{
	"aws_ebs_volume": attr("id"),
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
				m[key{r.Type, k}] = r.Address
			}
		}
	}
	return m
}

// Lookup returns the state address for a live resource, or "" if Unmanaged.
func (m Managed) Lookup(typ, k string) string { return m[key{typ, k}] }
