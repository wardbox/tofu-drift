// Package plan runs a refresh-only plan and reads the Drift it reports.
//
// The plan's -json UI stream only names drifted resources; the attribute
// before/after lives in the saved plan, so we plan to a temp file and read it
// back with `show -json`.
package plan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
)

// Drift is one resource changed out of band, as the provider reported it.
type Drift struct {
	Address string
	Type    string
	Deleted bool
	Before  map[string]any
	After   map[string]any
	// Sensitivity markers from the plan: a bool, or objects mirroring the
	// attribute shape with true at sensitive leaves.
	BeforeSensitive any
	AfterSensitive  any
}

// Runner runs the plan in a root module. Same seams as state.Source.
type Runner struct {
	Dir      string
	LookPath func(file string) (string, error)
	Run      func(ctx context.Context, dir, bin string, args ...string) ([]byte, error)
}

// Drift runs a refresh-only plan and returns what it found. Never locks or
// writes state; the saved plan (which holds sensitive values) is removed.
func (r *Runner) Drift(ctx context.Context) ([]Drift, error) {
	for _, name := range []string{"tofu", "terraform"} {
		bin, err := r.LookPath(name)
		if err != nil {
			continue
		}
		tmp, err := os.MkdirTemp("", "tofu-drift-")
		if err != nil {
			return nil, err
		}
		defer func() { _ = os.RemoveAll(tmp) }()
		planFile := filepath.Join(tmp, "refresh.tfplan")
		if _, err := r.Run(ctx, r.Dir, bin, "plan", "-refresh-only", "-lock=false", "-input=false", "-no-color", "-out="+planFile); err != nil {
			return nil, fmt.Errorf("%s plan -refresh-only: %w", name, err)
		}
		out, err := r.Run(ctx, r.Dir, bin, "show", "-json", planFile)
		if err != nil {
			return nil, fmt.Errorf("%s show -json: %w", name, err)
		}
		return Parse(bytes.NewReader(out))
	}
	return nil, errors.New("neither tofu nor terraform found on PATH")
}

// Parse reads the resource_drift entries of `show -json` plan output.
func Parse(r io.Reader) ([]Drift, error) {
	var p struct {
		ResourceDrift []struct {
			Address string `json:"address"`
			Type    string `json:"type"`
			Change  struct {
				Actions         []string       `json:"actions"`
				Before          map[string]any `json:"before"`
				After           map[string]any `json:"after"`
				BeforeSensitive any            `json:"before_sensitive"`
				AfterSensitive  any            `json:"after_sensitive"`
			} `json:"change"`
		} `json:"resource_drift"`
	}
	if err := json.NewDecoder(r).Decode(&p); err != nil {
		return nil, fmt.Errorf("parsing plan: %w", err)
	}
	var ds []Drift
	for _, d := range p.ResourceDrift {
		c := d.Change
		ds = append(ds, Drift{
			Address:         d.Address,
			Type:            d.Type,
			Deleted:         slices.Contains(c.Actions, "delete"),
			Before:          c.Before,
			After:           c.After,
			BeforeSensitive: c.BeforeSensitive,
			AfterSensitive:  c.AfterSensitive,
		})
	}
	return ds, nil
}

// Changed lists the top-level attributes that differ, sorted; nil when the
// resource was deleted.
func (d Drift) Changed() []string {
	if d.Deleted {
		return nil
	}
	var names []string
	for k := range d.Before {
		if !reflect.DeepEqual(d.Before[k], d.After[k]) {
			names = append(names, k)
		}
	}
	for k := range d.After {
		if _, ok := d.Before[k]; !ok {
			names = append(names, k)
		}
	}
	slices.Sort(names)
	return names
}

// sensitive reports whether attribute key is marked sensitive on either side,
// so a value is never shown in cleartext next to its masked counterpart.
func (d Drift) sensitive(key string) bool {
	return marked(attrMarker(d.BeforeSensitive, key)) || marked(attrMarker(d.AfterSensitive, key))
}

func attrMarker(m any, key string) any {
	if obj, ok := m.(map[string]any); ok {
		return obj[key]
	}
	return m
}

// Explain writes the attribute-level before/after, masking sensitive values.
func (d Drift) Explain(w io.Writer) {
	if d.Deleted {
		fmt.Fprintf(w, "%s (%s): deleted out of band\n", d.Address, d.Type)
		return
	}
	changed := d.Changed()
	fmt.Fprintf(w, "%s (%s): %d attributes changed out of band\n", d.Address, d.Type, len(changed))
	for _, k := range changed {
		before, after := "(sensitive)", "(sensitive)"
		if !d.sensitive(k) {
			before, after = jsonString(d.Before[k]), jsonString(d.After[k])
		}
		fmt.Fprintf(w, "  %s\n    - %s\n    + %s\n", k, before, after)
	}
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// marked reports whether a sensitivity marker has any true leaf.
func marked(m any) bool {
	switch m := m.(type) {
	case bool:
		return m
	case map[string]any:
		return slices.ContainsFunc(slices.Collect(maps.Values(m)), marked)
	case []any:
		return slices.ContainsFunc(m, marked)
	}
	return false
}
