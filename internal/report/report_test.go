package report

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/wardbox/tofu-drift/internal/plan"
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
TYPE            ID      REGION     NAME     STATUS          AGE  $/MO  kgCO₂/MO  NOTE
aws_ebs_volume  vol-ui  us-east-1  scratch  unmanaged+idle  10d  8.00  0.1       -
aws_ebs_volume  vol-u   us-east-1  -        unmanaged       -    4.00  0.0       -
aws_ebs_volume  vol-mi  us-east-1  -        idle            -    0.80  0.0       -

Unmanaged: $12/mo · Idle: $9/mo · ~0.1 kgCO₂/mo (≈ 0.00 trans-Atlantic flights)
`
	if out.String() != want {
		t.Errorf("table:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestIgnoreRules(t *testing.T) {
	r := New(Scan{Region: "us-east-1"}, []state.Resource{
		{Address: "aws_ebs_volume.data", Type: "aws_ebs_volume", Attributes: map[string]any{"id": "vol-mi"}},
	})
	r.Ignore = func(l scan.LiveResource) bool { return l.Type == "aws_ebs_volume" }
	r.AddLive([]scan.LiveResource{
		{Type: "aws_ebs_volume", Key: "vol-mi", Class: "gp3", SizeGB: 10, Idle: "unattached"},
		{Type: "aws_ebs_volume", Key: "vol-u", Class: "gp3", SizeGB: 50},
		{Type: "aws_instance", Key: "i-u", Class: "t3.large"},
	}, time.Now())
	// the rule matches the drifted volume too, but Drift is never ignored
	r.AddDrift([]plan.Drift{{Address: "aws_ebs_volume.data", Type: "aws_ebs_volume",
		Before: map[string]any{"size": 10.0}, After: map[string]any{"size": 20.0}}})

	if len(r.Findings) != 2 {
		t.Fatalf("findings: %+v", r.Findings)
	}
	if i := r.Findings[0]; i.ID != "i-u" {
		t.Errorf("unignored unmanaged instance: %+v", i)
	}
	if d := r.Findings[1]; d.Address != "aws_ebs_volume.data" || d.Drift == nil || d.Idle != nil {
		t.Errorf("drift must survive, without the ignored idle: %+v", d)
	}
	if r.Totals.IdleUSDMo != 0 || math.Abs(r.Totals.UnmanagedUSDMo-r.Findings[0].USDMo) > 1e-9 {
		t.Errorf("ignored rows must not count in totals: %+v", r.Totals)
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

func TestAddLiveData(t *testing.T) {
	r := New(Scan{Region: "us-east-1"}, nil)
	r.AddLive([]scan.LiveResource{
		// Same Match Key on different types: separate rows, separate costs.
		{Type: "aws_db_instance", Key: "app", Class: "db.t3.micro", Storage: "gp3", SizeGB: 20, Derived: []string{"rds:app-1"}},
		{Type: "aws_db_snapshot", Key: "rds:app-1", Class: "automated", SizeGB: 20},
		{Type: "aws_db_snapshot", Key: "before-upgrade", Class: "manual", SizeGB: 20},
		{Type: "aws_elasticache_cluster", Key: "app", Class: "cache.t3.micro", Nodes: 2},
		{Type: "aws_db_instance", Key: "old", Class: "db.t3.micro", Storage: "gp3", SizeGB: 20, Stopped: true, Idle: "stopped"},
		{Type: "aws_s3_bucket", Key: "logs", Region: "global", Note: "size unknown"},
	}, time.Now())

	var out bytes.Buffer
	if err := r.WriteTable(&out); err != nil {
		t.Fatal(err)
	}
	want := `Drift
ADDRESS  TYPE  CHANGED

Unmanaged & idle
TYPE                     ID              REGION     NAME  STATUS          AGE  $/MO   kgCO₂/MO  NOTE
aws_elasticache_cluster  app             us-east-1  -     unmanaged       -    24.82  2.7       -
aws_db_instance          app             us-east-1  -     unmanaged       -    14.71  1.3       -
aws_db_instance          old             us-east-1  -     unmanaged+idle  -    2.30   0.0       -
aws_db_snapshot          before-upgrade  us-east-1  -     unmanaged       -    1.90   0.0       -
aws_s3_bucket            logs            global     -     unmanaged       -    0.00   0.0       size unknown

Unmanaged: $44/mo · Idle: $2/mo · ~4.0 kgCO₂/mo (≈ 0.01 trans-Atlantic flights)
`
	if out.String() != want {
		t.Errorf("table:\n%s\nwant:\n%s", out.String(), want)
	}
	if s3 := r.Findings[4];s3.Region != "global" || s3.Note != "size unknown" {
		t.Errorf("s3: %+v", s3)
	}
	// --explain app shows the first row, with that row's own resource.
	out.Reset()
	if !r.Explain(&out, "app") || !strings.Contains(out.String(), "cost:     cache.t3.micro $0.017/h × 730 h × 2 nodes = $24.82/mo\n") {
		t.Errorf("explain:\n%s", out.String())
	}
	for id, want := range map[string]string{
		"old":            "cost:     db.t3.micro stopped, no compute charge + gp3 20 GB × $0.115/GB-mo = $2.30/mo\n",
		"before-upgrade": "cost:     snapshot 20 GB × $0.095/GB-mo = $1.90/mo\n",
	} {
		out.Reset()
		if !r.Explain(&out, id) || !strings.Contains(out.String(), want) {
			t.Errorf("explain %s:\n%s\nwant line %q", id, out.String(), want)
		}
	}
}

func TestAddLiveFoldsCompute(t *testing.T) {
	eks := map[string]string{"eks:cluster-name": "prod"}
	r := New(Scan{Region: "us-east-1"}, []state.Resource{
		{Address: "aws_eks_cluster.prod", Type: "aws_eks_cluster", Attributes: map[string]any{"id": "prod"}},
	})
	r.AddLive([]scan.LiveResource{
		// Unmanaged ASG: one row carrying its instances and their volumes.
		{Type: "aws_autoscaling_group", Key: "web", Derived: []string{"i-1", "i-2"}},
		{Type: "aws_instance", Key: "i-1", Class: "t3.large", Derived: []string{"vol-1"}},
		{Type: "aws_instance", Key: "i-2", Class: "t3.large"},
		{Type: "aws_ebs_volume", Key: "vol-1", Class: "gp3", SizeGB: 10},
		// Managed EKS cluster: nodegroup, nodes, ENIs and SG all dropped.
		{Type: "aws_eks_cluster", Key: "prod", Derived: []string{"sg-eks"}},
		{Type: "aws_security_group", Key: "sg-eks"},
		{Type: "aws_autoscaling_group", Key: "eks-ng", Tags: eks, Derived: []string{"i-ng"}},
		{Type: "aws_instance", Key: "i-ng", Class: "t3.large", Tags: eks},
		{Type: "aws_network_interface", Key: "eni-cp", Name: "Amazon EKS prod"},
		{Type: "aws_network_interface", Key: "eni-idle", Idle: "unattached", Tags: map[string]string{"cluster.k8s.amazonaws.com/name": "prod"}},
	}, time.Now())

	if len(r.Findings) != 1 {
		t.Fatalf("findings: %+v", r.Findings)
	}
	if f := r.Findings[0]; f.ID != "web" || !f.Unmanaged || math.Abs(f.USDMo-(2*0.0832*730+0.8)) > 1e-9 || f.KgCO2Mo == 0 {
		t.Errorf("unmanaged asg: %+v", f)
	}
	if math.Abs(r.Totals.UnmanagedUSDMo-r.Findings[0].USDMo) > 1e-9 || r.Totals.IdleUSDMo != 0 {
		t.Errorf("totals: %+v", r.Totals)
	}
}

func TestAddLiveUnmanagedEKS(t *testing.T) {
	r := New(Scan{Region: "us-east-1"}, nil)
	r.AddLive([]scan.LiveResource{
		{Type: "aws_eks_cluster", Key: "dev"},
		{Type: "aws_instance", Key: "i-node", Class: "t3.large", Tags: map[string]string{"eks:cluster-name": "dev"}},
		// Same name, other type: its own row, none of the EKS cost.
		{Type: "aws_ecs_cluster", Key: "dev"},
	}, time.Now())
	if len(r.Findings) != 2 || r.Findings[1].Type != "aws_ecs_cluster" || r.Findings[1].USDMo != 0 {
		t.Fatalf("findings: %+v", r.Findings)
	}
	if f := r.Findings[0]; f.ID != "dev" || math.Abs(f.USDMo-(0.10+0.0832)*730) > 1e-9 {
		t.Errorf("control plane plus node: %+v", f)
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

func TestAddLiveLogGroups(t *testing.T) {
	r := New(Scan{Region: "us-east-1"}, []state.Resource{
		{Address: "aws_cloudwatch_log_group.app", Type: "aws_cloudwatch_log_group", Attributes: map[string]any{"name": "app"}},
	})
	r.AddLive([]scan.LiveResource{
		// The function's log group folds into it: one row, the storage cost.
		{Type: "aws_lambda_function", Key: "resize", Derived: []string{"/aws/lambda/resize"}},
		{Type: "aws_cloudwatch_log_group", Key: "/aws/lambda/resize", SizeGB: 10, Note: "retention never"},
		// Its function is gone: reported on its own.
		{Type: "aws_cloudwatch_log_group", Key: "/aws/lambda/gone", SizeGB: 100, Note: "retention never"},
		{Type: "aws_cloudwatch_log_group", Key: "app", SizeGB: 1, Note: "retention never"},
	}, time.Now())

	if len(r.Findings) != 2 {
		t.Fatalf("findings: %+v", r.Findings)
	}
	gone, fn := r.Findings[0], r.Findings[1]
	if gone.ID != "/aws/lambda/gone" || gone.USDMo != 3 || gone.Note != "retention never" || gone.KgCO2Mo == 0 {
		t.Errorf("orphan log group: %+v", gone)
	}
	if fn.ID != "resize" || math.Abs(fn.USDMo-0.3) > 1e-9 || fn.Note != "" {
		t.Errorf("function with its log group: %+v", fn)
	}

	var out bytes.Buffer
	if err := r.WriteTable(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "NOTE\n") || !strings.Contains(out.String(), "3.00  0.0       retention never\n") {
		t.Errorf("table missing NOTE:\n%s", out.String())
	}

	for id, want := range map[string]string{
		"/aws/lambda/gone": "cost:     100 GB × $0.03/GB-mo = $3.00/mo\n          = $3.00/mo",
		"resize": "cost:     requests and duration not estimated = $0.00/mo\n" +
			"          + /aws/lambda/resize: 10 GB × $0.03/GB-mo = $0.30/mo\n          = $0.30/mo",
	} {
		out.Reset()
		if !r.Explain(&out, id) || !strings.Contains(out.String(), want) {
			t.Errorf("explain %s:\n%s\nwant:\n%s", id, out.String(), want)
		}
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

func TestAddDrift(t *testing.T) {
	r := New(Scan{Region: "us-east-1"}, []state.Resource{
		{Address: "aws_ebs_volume.data", Type: "aws_ebs_volume", Attributes: map[string]any{"id": "vol-mi"}},
	})
	r.AddLive([]scan.LiveResource{{Type: "aws_ebs_volume", Key: "vol-mi", Class: "gp3", SizeGB: 10, Idle: "unattached"}}, time.Now())
	r.AddDrift([]plan.Drift{
		{Address: "aws_instance.web", Type: "aws_instance",
			Before: map[string]any{"a": 1.0, "b": 1.0, "c": 1.0, "d": 1.0, "same": 1.0},
			After:  map[string]any{"a": 2.0, "b": 2.0, "c": 2.0, "d": 2.0, "same": 1.0}},
		{Address: "aws_s3_bucket.logs", Type: "aws_s3_bucket", Deleted: true, Before: map[string]any{"id": "logs"}},
		// managed idle volume that also drifted: one Finding carrying both
		{Address: "aws_ebs_volume.data", Type: "aws_ebs_volume",
			Before: map[string]any{"size": 10.0}, After: map[string]any{"size": 20.0}},
	})
	if len(r.Findings) != 3 {
		t.Fatalf("findings: %+v", r.Findings)
	}
	vol := r.Findings[0]
	if vol.ID != "vol-mi" || vol.Idle == nil || vol.Drift == nil || fmt.Sprint(vol.Drift.Changed) != "[size]" {
		t.Errorf("merged finding: %+v", vol)
	}
	web := r.Findings[1]
	if web.ID != "aws_instance.web" || web.Address != "aws_instance.web" || web.Unmanaged || web.Idle != nil || web.USDMo != 0 {
		t.Errorf("drift finding: %+v", web)
	}
	if r.Totals.UnmanagedUSDMo != 0 || r.Totals.IdleUSDMo != 0.8 {
		t.Errorf("drift must not add to totals: %+v", r.Totals)
	}

	var out bytes.Buffer
	if err := r.WriteTable(&out); err != nil {
		t.Fatal(err)
	}
	want := `Drift
ADDRESS              TYPE            CHANGED
aws_ebs_volume.data  aws_ebs_volume  1 (size)
aws_instance.web     aws_instance    4 (a, b, c, …)
aws_s3_bucket.logs   aws_s3_bucket   deleted

Unmanaged & idle
TYPE            ID      REGION     NAME  STATUS  AGE  $/MO  kgCO₂/MO  NOTE
aws_ebs_volume  vol-mi  us-east-1  -     idle    -    0.80  0.0       -

Unmanaged: $0/mo · Idle: $1/mo · ~0.0 kgCO₂/mo (≈ 0.00 trans-Atlantic flights)
`
	if out.String() != want {
		t.Errorf("table:\n%s\nwant:\n%s", out.String(), want)
	}
}
