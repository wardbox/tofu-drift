package report

import (
	"bytes"
	"fmt"
	"math"
	"strings"
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

func TestAddLiveFoldsDerived(t *testing.T) {
	r := New(Scan{Region: "us-east-1"}, []state.Resource{
		{Address: "aws_instance.app", Type: "aws_instance", Attributes: map[string]any{"id": "i-app"}},
	})
	r.AddLive([]scan.LiveResource{
		{Type: "aws_ebs_volume", Key: "vol-off", Class: "gp3", SizeGB: 100},
		{Type: "aws_instance", Key: "i-off", Class: "t3.large", Stopped: true, Derived: []string{"vol-off"}},
		{Type: "aws_instance", Key: "i-run", Class: "t3.large", Derived: []string{"vol-run"}},
		{Type: "aws_ebs_volume", Key: "vol-run", Class: "gp3", SizeGB: 10},
		// root volume of a managed instance: folded away, not Unmanaged
		{Type: "aws_instance", Key: "i-app", Class: "t3.large", Derived: []string{"vol-app"}},
		{Type: "aws_ebs_volume", Key: "vol-app", Class: "gp3", SizeGB: 8},
	}, time.Now())

	if len(r.Findings) != 2 {
		t.Fatalf("findings: %+v", r.Findings)
	}
	run, off := r.Findings[0], r.Findings[1]
	if run.ID != "i-run" || math.Abs(run.USDMo-(0.0832*730+0.8)) > 1e-9 {
		t.Errorf("running instance: compute plus its volume: %+v", run)
	}
	if off.ID != "i-off" || off.USDMo != 8 || off.KgCO2Mo == 0 {
		t.Errorf("stopped instance: attached EBS only: %+v", off)
	}
}

func TestAddLiveFlatRate(t *testing.T) {
	r := New(Scan{Region: "us-east-1"}, []state.Resource{
		{Address: "aws_ami.golden", Type: "aws_ami", Attributes: map[string]any{"id": "ami-1"}},
	})
	r.AddLive([]scan.LiveResource{
		{Type: "aws_eip", Key: "eipalloc-free", Idle: "unassociated"},
		// NAT folds its EIP: one row, both charges.
		{Type: "aws_nat_gateway", Key: "nat-1", Derived: []string{"eipalloc-nat"}},
		{Type: "aws_eip", Key: "eipalloc-nat"},
		// Managed idle AMI carries its snapshot's cost and carbon.
		{Type: "aws_ami", Key: "ami-1", Idle: "no instances", Derived: []string{"snap-1"}},
		{Type: "aws_ebs_snapshot", Key: "snap-1", SizeGB: 100, Idle: "source volume deleted"},
	}, time.Now())

	var ids []string
	for _, f := range r.Findings {
		ids = append(ids, f.ID)
	}
	if want := "[nat-1 ami-1 eipalloc-free]"; fmt.Sprint(ids) != want {
		t.Fatalf("findings %v, want %s", ids, want)
	}
	nat, ami, eip := r.Findings[0], r.Findings[1], r.Findings[2]
	if math.Abs(nat.USDMo-(0.045+0.005)*730) > 1e-9 || nat.Idle != nil {
		t.Errorf("nat: %+v", nat)
	}
	if ami.Unmanaged || ami.USDMo != 5 || ami.KgCO2Mo == 0 {
		t.Errorf("ami: %+v", ami)
	}
	if eip.USDMo != 3.65 || eip.status() != "unmanaged+idle" {
		t.Errorf("eip: %+v", eip)
	}
}

func TestApproxMarked(t *testing.T) {
	r := New(Scan{Region: "af-south-1"}, nil)
	r.AddLive([]scan.LiveResource{{Type: "aws_instance", Key: "i-far", Class: "t3.large"}}, time.Now())
	if !r.Findings[0].Approx {
		t.Fatalf("unlisted region must be marked approx: %+v", r.Findings[0])
	}
	var out bytes.Buffer
	if err := r.WriteTable(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "≈60.74") {
		t.Errorf("table missing ≈ price:\n%s", out.String())
	}
}
