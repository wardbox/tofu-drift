package report

import (
	"bytes"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/wardbox/tofu-drift/internal/scan"
	"github.com/wardbox/tofu-drift/internal/state"
)

func TestAddLive(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	created := now.AddDate(0, 0, -10)
	r := New(Scan{Region: "us-east-1"}, []state.Resource{
		{Address: "aws_ebs_volume.managed_idle", Type: "aws_ebs_volume", Attributes: map[string]any{"id": "vol-mi"}},
		{Address: "aws_ebs_volume.managed_used", Type: "aws_ebs_volume", Attributes: map[string]any{"id": "vol-mu"}},
	})
	r.AddLive([]scan.LiveResource{
		{Type: "aws_ebs_volume", Key: "vol-mu", Class: "gp3", SizeGB: 500},
		{Type: "aws_ebs_volume", Key: "vol-mi", Class: "gp3", SizeGB: 10, Idle: "unattached"},
		{Type: "aws_ebs_volume", Key: "vol-u", Class: "gp3", SizeGB: 50},
		{Type: "aws_ebs_volume", Key: "vol-ui", Name: "scratch", Class: "gp3", SizeGB: 100, Idle: "unattached", Created: &created},
	}, now)

	var ids []string
	for _, f := range r.Findings {
		ids = append(ids, f.ID)
	}
	// managed+in use is not a finding; the rest sorted by cost descending
	if want := "[vol-ui vol-u vol-mi]"; fmt.Sprint(ids) != want {
		t.Fatalf("findings %v, want %s", ids, want)
	}
	ui, u, mi := r.Findings[0], r.Findings[1], r.Findings[2]
	if !ui.Unmanaged || ui.Idle == nil || *ui.Idle != "unattached" || ui.USDMo != 8 || ui.KgCO2Mo == 0 ||
		ui.Name != "scratch" || ui.AgeDays == nil || *ui.AgeDays != 10 || ui.Address != "" {
		t.Errorf("unmanaged+idle: %+v", ui)
	}
	if !u.Unmanaged || u.Idle != nil || u.AgeDays != nil {
		t.Errorf("unmanaged: %+v", u)
	}
	if mi.Unmanaged || mi.Idle == nil || mi.Address != "aws_ebs_volume.managed_idle" {
		t.Errorf("managed idle: %+v", mi)
	}
	if r.Totals.UnmanagedUSDMo != 12 || math.Abs(r.Totals.IdleUSDMo-8.8) > 1e-9 {
		t.Errorf("totals: %+v", r.Totals)
	}

	var out bytes.Buffer
	if err := r.WriteTable(&out); err != nil {
		t.Fatal(err)
	}
	want := `Drift
ADDRESS  TYPE  CHANGED

Unmanaged & idle
TYPE            ID      NAME     STATUS          AGE  $/MO  kgCO₂/MO
aws_ebs_volume  vol-ui  scratch  unmanaged+idle  10d  8.00  0.1
aws_ebs_volume  vol-u   -        unmanaged       -    4.00  0.0
aws_ebs_volume  vol-mi  -        idle            -    0.80  0.0

Unmanaged: $12/mo · Idle: $9/mo · ~0.1 kgCO₂/mo (≈ 0.00 trans-Atlantic flights)
`
	if out.String() != want {
		t.Errorf("table:\n%s\nwant:\n%s", out.String(), want)
	}
}
