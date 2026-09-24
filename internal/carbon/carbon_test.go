package carbon

import (
	"math"
	"testing"

	"github.com/wardbox/tofu-drift/internal/scan"
)

func TestMonthlyEBS(t *testing.T) {
	// 1 TB SSD in us-east-1: 1 TB × 730 h × 1.2 Wh × 2 replicas × 1.135 PUE / 1000 × 0.379069 kg/kWh
	want := 730 * 1.2 * 2 * 1.135 / 1000 * 0.379069
	got := Monthly("us-east-1", scan.LiveResource{Type: "aws_ebs_volume", Class: "gp3", SizeGB: 1000})
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("ssd: got %v, want %v", got, want)
	}
	// RDS storage is SSD like EBS, billed stopped or not, ×2 for Multi-AZ.
	rds := scan.LiveResource{Type: "aws_db_instance", Storage: "gp3", SizeGB: 1000, Stopped: true, Nodes: 2}
	if got := Monthly("us-east-1", rds); math.Abs(got-2*want) > 1e-9 {
		t.Errorf("rds storage: got %v, want %v", got, 2*want)
	}
	hdd := Monthly("us-east-1", scan.LiveResource{Type: "aws_ebs_volume", Class: "sc1", SizeGB: 1000})
	if hdd >= got || hdd == 0 {
		t.Errorf("hdd should be below ssd and nonzero: %v vs %v", hdd, got)
	}
}

func TestMonthlyFlat(t *testing.T) {
	// 1 TB snapshot: HDD coefficient, like CCF classifies snapshot storage.
	want := 730 * 0.65 * 2 * 1.135 / 1000 * 0.379069
	if got := Monthly("us-east-1", scan.LiveResource{Type: "aws_ebs_snapshot", SizeGB: 1000}); math.Abs(got-want) > 1e-9 {
		t.Errorf("snapshot: got %v, want %v", got, want)
	}
	if got := Monthly("us-east-1", scan.LiveResource{Type: "aws_cloudwatch_log_group", SizeGB: 1000}); math.Abs(got-want) > 1e-9 {
		t.Errorf("log group: got %v, want %v (HDD, like snapshots)", got, want)
	}
	if got := Monthly("us-east-1", scan.LiveResource{Type: "aws_db_snapshot", Class: "manual", SizeGB: 1000}); math.Abs(got-want) > 1e-9 {
		t.Errorf("rds snapshot: got %v, want %v (HDD, like EBS snapshots)", got, want)
	}
	if got := Monthly("us-east-1", scan.LiveResource{Type: "aws_db_snapshot", Class: "automated", SizeGB: 1000}); got != 0 {
		t.Errorf("automated rds snapshot: got %v, want 0 (folded, free up to DB size)", got)
	}
	for _, typ := range []string{"aws_eip", "aws_network_interface", "aws_nat_gateway", "aws_lb", "aws_elb", "aws_ami", "aws_iam_role", "aws_iam_user", "aws_route53_zone", "aws_lambda_function"} {
		if got := Monthly("us-east-1", scan.LiveResource{Type: typ, SizeGB: 1000}); got != 0 {
			t.Errorf("%s: got %v, want 0 (network ignored; AMIs carry carbon via their snapshots)", typ, got)
		}
	}
}

func TestMonthlyEC2(t *testing.T) {
	// t3.large in us-east-1: 2 vCPU × (0.74+3.5)/2 W × 730 h × 1.135 PUE / 1000 × 0.379069 kg/kWh
	want := 2 * (0.74 + 3.5) / 2 * 730 * 1.135 / 1000 * 0.379069
	large := scan.LiveResource{Type: "aws_instance", Class: "t3.large"}
	if got := Monthly("us-east-1", large); math.Abs(got-want) > 1e-9 {
		t.Errorf("running: got %v, want %v", got, want)
	}
	large.Stopped = true
	if got := Monthly("us-east-1", large); got != 0 {
		t.Errorf("stopped: got %v", got)
	}
	// RDS and ElastiCache classes carry vCPUs too; every billed node counts.
	for _, r := range []scan.LiveResource{
		{Type: "aws_db_instance", Class: "db.t3.large", Nodes: 2},
		{Type: "aws_elasticache_replication_group", Class: "cache.t3.medium", Nodes: 2},
	} {
		if got := Monthly("us-east-1", r); math.Abs(got-2*want) > 1e-9 {
			t.Errorf("%s: got %v, want %v", r.Type, got, 2*want)
		}
	}
}

func TestFlights(t *testing.T) {
	if got := Flights(15); math.Abs(got-0.02) > 1e-9 {
		t.Errorf("got %v", got)
	}
}
