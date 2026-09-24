// Package report holds the output schema shared by the table and --json
// renderers. Schema bumps on any breaking change.
package report

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/wardbox/tofu-drift/internal/carbon"
	"github.com/wardbox/tofu-drift/internal/match"
	"github.com/wardbox/tofu-drift/internal/plan"
	"github.com/wardbox/tofu-drift/internal/pricing"
	"github.com/wardbox/tofu-drift/internal/scan"
	"github.com/wardbox/tofu-drift/internal/state"
)

const Schema = 1

type Report struct {
	Schema int `json:"schema"`
	// DriftChecked is true when a refresh-only plan ran; false when drift
	// detection was skipped, so an empty Drift list proves nothing.
	DriftChecked bool      `json:"drift_checked"`
	Scan         Scan      `json:"scan"`
	Findings     []Finding `json:"findings"`
	Totals       Totals    `json:"totals"`

	// Ignore, when set, drops the Unmanaged and Idle Finding of every live
	// resource it matches (Ignore Rules). Drift is never ignored.
	Ignore func(scan.LiveResource) bool `json:"-"`

	managed match.Managed
	// live and drifts back --explain, by Match Key and address.
	live   map[string]scan.LiveResource
	drifts map[string]plan.Drift
}

type Scan struct {
	Account     string `json:"account"`
	Region      string `json:"region"`
	StateSource string `json:"state_source"`
}

// Finding is one row: one resource carrying one or more of drift, unmanaged, idle.
type Finding struct {
	ID        string  `json:"id"`
	Address   string  `json:"address"`
	Type      string  `json:"type"`
	Region    string  `json:"region,omitempty"`
	Name      string  `json:"name"`
	Drift     *Drift  `json:"drift"`
	Unmanaged bool    `json:"unmanaged"`
	Idle      *string `json:"idle"`
	USDMo     float64 `json:"usd_mo"`
	// Approx marks a us-east-1 price used for a region without its own table.
	Approx  bool     `json:"usd_mo_approx,omitempty"`
	KgCO2Mo float64  `json:"kgco2_mo"`
	AgeDays *float64 `json:"age_days"`
	Note    string   `json:"note,omitempty"`
}

type Drift struct {
	Changed []string `json:"changed"`
	Deleted bool     `json:"deleted,omitempty"`
}

type Totals struct {
	UnmanagedUSDMo float64 `json:"unmanaged_usd_mo"`
	IdleUSDMo      float64 `json:"idle_usd_mo"`
	KgCO2Mo        float64 `json:"kgco2_mo"`
}

func New(meta Scan, managed []state.Resource) *Report {
	return &Report{Schema: Schema, Scan: meta, Findings: []Finding{}, managed: match.Index(managed),
		live: map[string]scan.LiveResource{}, drifts: map[string]plan.Drift{}}
}

// AddLive turns live resources into Unmanaged and Idle Findings, costs them
// for the scanned region, and keeps Findings sorted by cost descending.
// Derived Resources are never rows; their cost rolls up into their parent.
func (r *Report) AddLive(live []scan.LiveResource, now time.Time) {
	roots := match.Roots(live)
	// Costs are keyed by index in live: names like "app" repeat across types.
	usd, kg, approx := make([]float64, len(live)), make([]float64, len(live)), make([]bool, len(live))
	for i, l := range live {
		// By type and key for Findings, by key alone for Derived lookups.
		r.live[l.Type+" "+l.Key], r.live[l.Key] = l, l
		root, ok := roots[i]
		if !ok {
			root = i
		}
		u, a := pricing.Monthly(r.Scan.Region, l)
		usd[root] += u
		approx[root] = approx[root] || a
		kg[root] += carbon.Monthly(r.Scan.Region, l)
	}
	for i, l := range live {
		if _, derived := roots[i]; derived {
			continue
		}
		addr := r.managed.Lookup(l.Type, l.Key)
		if addr != "" && l.Idle == "" || r.Ignore != nil && r.Ignore(l) {
			continue
		}
		f := Finding{
			ID:        l.Key,
			Address:   addr,
			Type:      l.Type,
			Region:    cmp.Or(l.Region, r.Scan.Region),
			Name:      l.Name,
			Unmanaged: addr == "",
			USDMo:     usd[i],
			Approx:    approx[i],
			KgCO2Mo:   kg[i],
			Note:      l.Note,
		}
		if l.Idle != "" {
			f.Idle = &l.Idle
			r.Totals.IdleUSDMo += f.USDMo
		}
		if f.Unmanaged {
			r.Totals.UnmanagedUSDMo += f.USDMo
		}
		if l.Created != nil {
			days := float64(int(now.Sub(*l.Created).Hours() / 24))
			f.AgeDays = &days
		}
		r.Totals.KgCO2Mo += f.KgCO2Mo
		r.Findings = append(r.Findings, f)
	}
	_ = r.Sort("cost")
}

// Sort orders Findings by cost (descending), type, or age (oldest first,
// unknown age last). Ties keep cost order.
func (r *Report) Sort(by string) error {
	byCost := func(a, b Finding) int {
		return cmp.Or(cmp.Compare(b.USDMo, a.USDMo), strings.Compare(a.ID, b.ID))
	}
	var then func(a, b Finding) int
	switch by {
	case "cost":
		then = func(Finding, Finding) int { return 0 }
	case "type":
		then = func(a, b Finding) int { return strings.Compare(a.Type, b.Type) }
	case "age":
		then = func(a, b Finding) int {
			if a.AgeDays == nil || b.AgeDays == nil {
				return cmp.Compare(ageRank(a), ageRank(b))
			}
			return cmp.Compare(*b.AgeDays, *a.AgeDays)
		}
	default:
		return fmt.Errorf("unknown sort %q: want cost, type or age", by)
	}
	slices.SortStableFunc(r.Findings, func(a, b Finding) int { return cmp.Or(then(a, b), byCost(a, b)) })
	return nil
}

// ageRank puts known ages before unknown ones.
func ageRank(f Finding) int {
	if f.AgeDays == nil {
		return 1
	}
	return 0
}

// AddDrift records Drift Findings. Call after AddLive: a drifted resource
// already reported as Idle gets its Drift on that Finding. Drift is never
// costed and adds nothing to the totals.
func (r *Report) AddDrift(drifts []plan.Drift) {
	for _, d := range drifts {
		r.drifts[d.Address] = d
		drift := &Drift{Changed: d.Changed(), Deleted: d.Deleted}
		i := slices.IndexFunc(r.Findings, func(f Finding) bool { return f.Address == d.Address })
		if i >= 0 {
			r.Findings[i].Drift = drift
			continue
		}
		r.Findings = append(r.Findings, Finding{ID: d.Address, Address: d.Address, Type: d.Type, Drift: drift})
	}
}

func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteTable writes the full report: Drift, then Unmanaged & idle, then the footer.
func (r *Report) WriteTable(w io.Writer) error { return r.writeTable(w, true) }

// WriteUnmanagedTable writes the report without the Drift section.
func (r *Report) WriteUnmanagedTable(w io.Writer) error { return r.writeTable(w, false) }

func (r *Report) writeTable(w io.Writer, withDrift bool) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if withDrift {
		fmt.Fprint(tw, "Drift\nADDRESS\tTYPE\tCHANGED\n")
		drifted := slices.DeleteFunc(slices.Clone(r.Findings), func(f Finding) bool { return f.Drift == nil })
		slices.SortFunc(drifted, func(a, b Finding) int { return strings.Compare(a.Address, b.Address) })
		for _, f := range drifted {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", f.Address, f.Type, f.Drift.summary())
		}
		fmt.Fprint(tw, "\n")
	}
	fmt.Fprint(tw, "Unmanaged & idle\nTYPE\tID\tREGION\tNAME\tSTATUS\tAGE\t$/MO\tkgCO₂/MO\tNOTE\n")
	for _, f := range r.Findings {
		if !f.Unmanaged && f.Idle == nil {
			continue
		}
		usd := fmt.Sprintf("%.2f", f.USDMo)
		if f.Approx {
			usd = "≈" + usd
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%.1f\t%s\n",
			f.Type, f.ID, f.Region, dash(f.Name), f.status(), age(f.AgeDays), usd, f.KgCO2Mo, dash(f.Note))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "\nUnmanaged: $%.0f/mo · Idle: $%.0f/mo · ~%.1f kgCO₂/mo (≈ %.2f trans-Atlantic flights)\n",
		r.Totals.UnmanagedUSDMo, r.Totals.IdleUSDMo, r.Totals.KgCO2Mo, carbon.Flights(r.Totals.KgCO2Mo))
	return err
}

func (f Finding) status() string {
	switch {
	case f.Unmanaged && f.Idle != nil:
		return "unmanaged+idle"
	case f.Unmanaged:
		return "unmanaged"
	}
	return "idle"
}

// summary is the CHANGED cell: attribute count and the first few names.
func (d *Drift) summary() string {
	if d.Deleted {
		return "deleted"
	}
	names := d.Changed
	if len(names) > 3 {
		names = append(names[:3:3], "…")
	}
	return fmt.Sprintf("%d (%s)", len(d.Changed), strings.Join(names, ", "))
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func age(days *float64) string {
	if days == nil {
		return "-"
	}
	return fmt.Sprintf("%.0fd", *days)
}

// Explain writes the detail behind the Finding with this ID: the
// attribute-level before/after for a Drift address; type, ARN, tags, creation
// time, idle reason, cost math and carbon for an Unmanaged or Idle Resource.
// False when no Finding has that ID.
func (r *Report) Explain(w io.Writer, id string) bool {
	if d, ok := r.drifts[id]; ok {
		d.Explain(w)
		return true
	}
	i := slices.IndexFunc(r.Findings, func(f Finding) bool { return f.ID == id && (f.Unmanaged || f.Idle != nil) })
	if i < 0 {
		return false
	}
	f := r.Findings[i]
	l := r.live[f.Type+" "+id]
	var tags []string
	for _, k := range slices.Sorted(maps.Keys(l.Tags)) {
		tags = append(tags, k+"="+l.Tags[k])
	}
	created := "-"
	if l.Created != nil {
		created = fmt.Sprintf("%s (%.0f days ago)", l.Created.Format(time.DateOnly), *f.AgeDays)
	}
	idle := "-"
	if f.Idle != nil {
		idle = *f.Idle
	}
	fmt.Fprintf(w, "type:     %s\nid:       %s\narn:      %s\ntags:     %s\ncreated:  %s\nstatus:   %s\nidle:     %s\n",
		f.Type, f.ID, dash(l.ARN), dash(strings.Join(tags, ", ")), created, f.status(), idle)
	fmt.Fprintf(w, "cost:     %s\n", pricing.Math(r.Scan.Region, l))
	for _, k := range l.Derived {
		if d, ok := r.live[k]; ok {
			fmt.Fprintf(w, "          + %s: %s\n", k, pricing.Math(r.Scan.Region, d))
		}
	}
	fmt.Fprintf(w, "          = $%.2f/mo (estimate)\ncarbon:   ~%.1f kgCO₂/mo (estimate)\n", f.USDMo, f.KgCO2Mo)
	return true
}

// WriteImports writes an import block for each Unmanaged Finding in ids,
// addressed aws_<type>.<Name-tag slug, else ID slug>, for the user to feed to
// tofu plan -generate-config-out. Writes nothing if any id is not an
// Unmanaged Finding.
func (r *Report) WriteImports(w io.Writer, ids []string) error {
	var b strings.Builder
	b.WriteString("# Generated by tofu-drift. Addresses are placeholders: rename them before apply.\n" +
		"# Then run: tofu plan -generate-config-out=generated.tf\n")
	seen := map[string]int{}
	for _, id := range ids {
		i := slices.IndexFunc(r.Findings, func(f Finding) bool { return f.ID == id && f.Unmanaged })
		if i < 0 {
			return fmt.Errorf("%s: no Unmanaged Finding with that ID", id)
		}
		f := r.Findings[i]
		addr := f.Type + "." + cmp.Or(slug(f.Name), slug(f.ID))
		if seen[addr]++; seen[addr] > 1 {
			addr += fmt.Sprintf("_%d", seen[addr])
		}
		fmt.Fprintf(&b, "\nimport {\n  to = %s\n  id = %s\n}\n", addr, strconv.Quote(f.ID))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

var nonIdent = regexp.MustCompile(`[^a-z0-9_-]+`)

// slug makes s a valid resource name: lowercase letters, digits, _ and -,
// starting with a letter or _.
func slug(s string) string {
	s = strings.Trim(nonIdent.ReplaceAllString(strings.ToLower(s), "_"), "_")
	if s != "" && (s[0] < 'a' || s[0] > 'z') {
		s = "_" + s
	}
	return s
}
