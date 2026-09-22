// Package report holds the output schema shared by the table and --json
// renderers. Schema bumps on any breaking change.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/wardbox/tofu-drift/internal/state"
)

const Schema = 1

type Report struct {
	Schema   int       `json:"schema"`
	Scan     Scan      `json:"scan"`
	Findings []Finding `json:"findings"`
	Totals   Totals    `json:"totals"`

	// Managed is the list of resources read from state. Not part of the JSON
	// schema; used by the human renderer until scanners exist.
	Managed []state.Resource `json:"-"`
}

type Scan struct {
	Account     string `json:"account"`
	Region      string `json:"region"`
	StateSource string `json:"state_source"`
}

// Finding is one row: one resource carrying one or more of drift, unmanaged, idle.
type Finding struct {
	ID        string   `json:"id"`
	Address   string   `json:"address"`
	Type      string   `json:"type"`
	Name      string   `json:"name"`
	Drift     *Drift   `json:"drift"`
	Unmanaged bool     `json:"unmanaged"`
	Idle      *string  `json:"idle"`
	USDMo     float64  `json:"usd_mo"`
	KgCO2Mo   float64  `json:"kgco2_mo"`
	AgeDays   *float64 `json:"age_days"`
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
	return &Report{Schema: Schema, Scan: scan, Findings: []Finding{}, Managed: managed}
}

func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func (r *Report) WriteTable(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ADDRESS\tTYPE\tID")
	for _, m := range r.Managed {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", m.Address, m.Type, m.ID)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "\n%d managed resources · %d findings\nUnmanaged: $%.0f/mo · Idle: $%.0f/mo · ~%.1f kgCO₂/mo\n",
		len(r.Managed), len(r.Findings), r.Totals.UnmanagedUSDMo, r.Totals.IdleUSDMo, r.Totals.KgCO2Mo)
	return err
}
