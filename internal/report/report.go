// Package report holds the output schema shared by the table and --json
// renderers. Schema bumps on any breaking change.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/wardbox/tofu-drift/internal/carbon"
	"github.com/wardbox/tofu-drift/internal/match"
	"github.com/wardbox/tofu-drift/internal/pricing"
	"github.com/wardbox/tofu-drift/internal/scan"
	"github.com/wardbox/tofu-drift/internal/state"
)

const Schema = 1

type Report struct {
	Schema   int       `json:"schema"`
	Scan     Scan      `json:"scan"`
	Findings []Finding `json:"findings"`
	Totals   Totals    `json:"totals"`

	managed match.Managed
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
	Name      string  `json:"name"`
	Drift     *Drift  `json:"drift"`
	Unmanaged bool    `json:"unmanaged"`
	Idle      *string `json:"idle"`
	USDMo     float64 `json:"usd_mo"`
	// Approx marks a us-east-1 price used for a region without its own table.
	Approx  bool     `json:"usd_mo_approx,omitempty"`
	KgCO2Mo float64  `json:"kgco2_mo"`
	AgeDays *float64 `json:"age_days"`
}

type Drift struct {
	Changed []string `json:"changed"`
}

type Totals struct {
	UnmanagedUSDMo float64 `json:"unmanaged_usd_mo"`
	IdleUSDMo      float64 `json:"idle_usd_mo"`
	KgCO2Mo        float64 `json:"kgco2_mo"`
}

func New(scan Scan, managed []state.Resource) *Report {
	return &Report{Schema: Schema, Scan: scan, Findings: []Finding{}, managed: match.Index(managed)}
}

// AddLive turns live resources into Unmanaged and Idle Findings, costs them
// for the scanned region, and keeps Findings sorted by cost descending.
// Derived Resources are never rows; their cost rolls up into their parent.
func (r *Report) AddLive(live []scan.LiveResource, now time.Time) {
	parent := map[string]string{}
	for _, l := range live {
		for _, d := range l.Derived {
			parent[d] = l.Key
		}
	}
	usd, kg := map[string]float64{}, map[string]float64{}
	for _, l := range live {
		root := l.Key
		for p, ok := parent[root]; ok; p, ok = parent[root] {
			root = p
		}
		u, _ := pricing.Monthly(r.Scan.Region, l)
		usd[root] += u
		kg[root] += carbon.Monthly(r.Scan.Region, l)
	}
	for _, l := range live {
		if _, derived := parent[l.Key]; derived {
			continue
		}
		addr := r.managed.Lookup(l.Type, l.Key)
		if addr != "" && l.Idle == "" {
			continue
		}
		_, approx := pricing.Monthly(r.Scan.Region, l)
		f := Finding{
			ID:        l.Key,
			Address:   addr,
			Type:      l.Type,
			Name:      l.Name,
			Unmanaged: addr == "",
			USDMo:     usd[l.Key],
			Approx:    approx,
			KgCO2Mo:   kg[l.Key],
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
	sort.SliceStable(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if a.USDMo != b.USDMo {
			return a.USDMo > b.USDMo
		}
		return a.ID < b.ID
	})
}

func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func (r *Report) WriteTable(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprint(tw, "Drift\nADDRESS\tTYPE\tCHANGED\n")
	fmt.Fprint(tw, "\nUnmanaged & idle\nTYPE\tID\tNAME\tSTATUS\tAGE\t$/MO\tkgCO₂/MO\n")
	for _, f := range r.Findings {
		usd := fmt.Sprintf("%.2f", f.USDMo)
		if f.Approx {
			usd = "≈" + usd
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%.1f\n",
			f.Type, f.ID, dash(f.Name), f.status(), age(f.AgeDays), usd, f.KgCO2Mo)
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
	case f.Idle != nil:
		return "idle"
	}
	return "drift"
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
