// Package state parses OpenTofu/Terraform state files (JSON schema v4) into
// the list of Managed Resources they contain. It never writes or locks state.
package state

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

// Resource is one instance of a managed resource recorded in state.
type Resource struct {
	// Address is the full resource address, including module path and index key,
	// e.g. module.net.aws_subnet.private[0].
	Address string
	Type    string
	// ID is the provider-assigned id attribute, if present.
	ID         string
	Tags       map[string]string
	Attributes map[string]any
}

type file struct {
	Version   int `json:"version"`
	Resources []struct {
		Module    string `json:"module"`
		Mode      string `json:"mode"`
		Type      string `json:"type"`
		Name      string `json:"name"`
		Instances []struct {
			IndexKey   any            `json:"index_key"`
			Attributes map[string]any `json:"attributes"`
		} `json:"instances"`
	} `json:"resources"`
}

// Parse reads a v4 state file and returns its managed resource instances in
// file order. Data sources are skipped.
func Parse(r io.Reader) ([]Resource, error) {
	var f file
	if err := json.NewDecoder(r).Decode(&f); err != nil {
		return nil, fmt.Errorf("state: not valid JSON: %w", err)
	}
	if f.Version != 4 {
		return nil, fmt.Errorf("state: unsupported state version %d (only 4 is supported)", f.Version)
	}
	var out []Resource
	for _, res := range f.Resources {
		if res.Mode != "managed" {
			continue
		}
		for _, inst := range res.Instances {
			attrs := inst.Attributes
			if attrs == nil {
				attrs = map[string]any{}
			}
			id, _ := attrs["id"].(string)
			out = append(out, Resource{
				Address:    address(res.Module, res.Type, res.Name, inst.IndexKey),
				Type:       res.Type,
				ID:         id,
				Tags:       stringMap(attrs["tags"]),
				Attributes: attrs,
			})
		}
	}
	return out, nil
}

func address(module, typ, name string, key any) string {
	addr := typ + "." + name
	if module != "" {
		addr = module + "." + addr
	}
	switch k := key.(type) {
	case string:
		addr += "[" + strconv.Quote(k) + "]"
	case float64:
		addr += "[" + strconv.FormatFloat(k, 'f', -1, 64) + "]"
	}
	return addr
}

func stringMap(v any) map[string]string {
	m, _ := v.(map[string]any)
	out := make(map[string]string, len(m))
	for k, val := range m {
		if s, ok := val.(string); ok {
			out[k] = s
		}
	}
	return out
}
