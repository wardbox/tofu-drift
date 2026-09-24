package pricing

import (
	"math"
	"testing"

	"github.com/wardbox/tofu-drift/internal/scan"
)

func TestMonthlyData(t *testing.T) {
	for _, tc := range []struct {
		region string
		r      scan.LiveResource
		want   float64
		approx bool
	}{
		{"us-east-1", scan.LiveResource{Type: "aws_db_instance", Class: "db.t3.micro"}, 0.017 * 730, false},
		{"us-east-1", scan.LiveResource{Type: "aws_db_instance", Class: "db.t3.micro", Nodes: 2}, 2 * 0.017 * 730, false},
		{"af-south-1", scan.LiveResource{Type: "aws_db_instance", Class: "db.t3.micro"}, 0.017 * 730, true},
		{"us-east-1", scan.LiveResource{Type: "aws_db_instance", Class: "db.t3.micro", Stopped: true}, 0, false},
		{"us-east-1", scan.LiveResource{Type: "aws_elasticache_cluster", Class: "cache.t3.micro", Nodes: 3}, 3 * 0.017 * 730, false},
		{"af-south-1", scan.LiveResource{Type: "aws_elasticache_replication_group", Class: "cache.t3.micro", Nodes: 2}, 2 * 0.017 * 730, true},
		{"us-east-1", scan.LiveResource{Type: "aws_s3_bucket"}, 0, false},
		{"us-east-1", scan.LiveResource{Type: "aws_dynamodb_table"}, 0, false},
		// RDS storage bills running or stopped, doubled for Multi-AZ.
		{"us-east-1", scan.LiveResource{Type: "aws_db_instance", Class: "db.t3.micro", Storage: "gp3", SizeGB: 20}, 0.017*730 + 20*0.115, false},
		{"us-east-1", scan.LiveResource{Type: "aws_db_instance", Class: "db.t3.micro", Storage: "gp3", SizeGB: 20, Nodes: 2}, 2 * (0.017*730 + 20*0.115), false},
		{"us-east-1", scan.LiveResource{Type: "aws_db_instance", Class: "db.t3.micro", Storage: "gp3", SizeGB: 20, Stopped: true}, 20 * 0.115, false},
		{"us-east-1", scan.LiveResource{Type: "aws_db_instance", Class: "db.t3.micro", Storage: "io1", SizeGB: 100, Stopped: true}, 100 * 0.125, false},
		{"eu-central-1", scan.LiveResource{Type: "aws_db_instance", Class: "db.t3.micro", Storage: "gp2", SizeGB: 100, Stopped: true}, 100 * 0.137, false},
		// Aurora storage is billed per cluster by usage: not estimated.
		{"us-east-1", scan.LiveResource{Type: "aws_db_instance", Class: "db.t3.micro", Storage: "aurora", SizeGB: 1}, 0.017 * 730, false},
		{"us-east-1", scan.LiveResource{Type: "aws_db_snapshot", Class: "manual", SizeGB: 20}, 20 * 0.095, false},
		{"us-east-1", scan.LiveResource{Type: "aws_db_snapshot", Class: "automated", SizeGB: 20}, 0, false},
	} {
		got, approx := Monthly(tc.region, tc.r)
		if math.Abs(got-tc.want) > 1e-9 || approx != tc.approx {
			t.Errorf("%s %+v: got %v approx=%v, want %v approx=%v", tc.region, tc.r, got, approx, tc.want, tc.approx)
		}
	}
}

func TestMonthlyEBS(t *testing.T) {
	vol := scan.LiveResource{Type: "aws_ebs_volume", Class: "gp3", SizeGB: 100}
	for _, tc := range []struct {
		region string
		want   float64
	}{
		{"us-east-1", 8},
		{"eu-central-1", 9.52},
		{"xx-nowhere-1", 8}, // unlisted region falls back to us-east-1
	} {
		if got, approx := Monthly(tc.region, vol); math.Abs(got-tc.want) > 1e-9 || approx {
			t.Errorf("%s: got %v, want %v", tc.region, got, tc.want)
		}
	}
	if got, _ := Monthly("us-east-1", scan.LiveResource{Type: "aws_widget"}); got != 0 {
		t.Errorf("unpriced type: got %v", got)
	}
}

func TestMonthlyEC2(t *testing.T) {
	large := scan.LiveResource{Type: "aws_instance", Class: "t3.large"}
	for _, tc := range []struct {
		region string
		want   float64
		approx bool
	}{
		{"us-east-1", 0.0832 * 730, false},
		{"xx-nowhere-1", 0.0832 * 730, true}, // unlisted region: us-east-1 price, marked ≈
	} {
		got, approx := Monthly(tc.region, large)
		if math.Abs(got-tc.want) > 1e-9 || approx != tc.approx {
			t.Errorf("%s: got %v approx=%v, want %v approx=%v", tc.region, got, approx, tc.want, tc.approx)
		}
	}
	if got, _ := Monthly("eu-central-1", large); got <= 0.0832*730 {
		t.Errorf("eu-central-1 should use its own, higher price: %v", got)
	}
	stopped := large
	stopped.Stopped = true
	if got, _ := Monthly("us-east-1", stopped); got != 0 {
		t.Errorf("stopped instance costs no compute: %v", got)
	}
	if VCPU("t3.large") != 2 || VCPU("db.t3.large") != 2 || VCPU("cache.t3.medium") != 2 {
		t.Errorf("vcpu: %d %d %d", VCPU("t3.large"), VCPU("db.t3.large"), VCPU("cache.t3.medium"))
	}
}

func TestMonthlyFlat(t *testing.T) {
	for _, tc := range []struct {
		region string
		r      scan.LiveResource
		want   float64
	}{
		{"us-east-1", scan.LiveResource{Type: "aws_eip"}, 3.65},
		{"us-east-1", scan.LiveResource{Type: "aws_nat_gateway"}, 0.045 * 730},
		{"sa-east-1", scan.LiveResource{Type: "aws_nat_gateway"}, 0.093 * 730},
		{"us-east-1", scan.LiveResource{Type: "aws_lb", Class: "application"}, 0.0225 * 730},
		{"eu-west-2", scan.LiveResource{Type: "aws_lb", Class: "network"}, 0.02646 * 730},
		{"us-east-1", scan.LiveResource{Type: "aws_elb"}, 0.025 * 730},
		{"us-east-1", scan.LiveResource{Type: "aws_ebs_snapshot", SizeGB: 100}, 5},
		{"xx-nowhere-1", scan.LiveResource{Type: "aws_ebs_snapshot", SizeGB: 100}, 5},
		{"us-east-1", scan.LiveResource{Type: "aws_cloudwatch_log_group", SizeGB: 100}, 3},
		{"sa-east-1", scan.LiveResource{Type: "aws_cloudwatch_log_group", SizeGB: 100}, 4.08},
		{"us-east-1", scan.LiveResource{Type: "aws_lambda_function"}, 0},
		{"eu-west-1", scan.LiveResource{Type: "aws_route53_zone"}, 0.50},
		// ENIs are free; an AMI's cost is its Derived snapshots.
		{"us-east-1", scan.LiveResource{Type: "aws_network_interface"}, 0},
		{"us-east-1", scan.LiveResource{Type: "aws_ami"}, 0},
	} {
		if got, approx := Monthly(tc.region, tc.r); math.Abs(got-tc.want) > 1e-9 || approx {
			t.Errorf("%s %s %s: got %v, want %v", tc.region, tc.r.Type, tc.r.Class, got, tc.want)
		}
	}
	// Every region seeded carries every flat rate.
	for region, rates := range flat {
		for _, k := range []string{"eip_hour", "nat_gateway_hour", "alb_hour", "nlb_hour", "clb_hour", "snapshot_gb_month", "log_storage_gb_month",
			"rds_gp2_gb_month", "rds_gp3_gb_month", "rds_io1_gb_month", "rds_io2_gb_month", "rds_standard_gb_month", "rds_backup_gb_month"} {
			if rates[k] <= 0 {
				t.Errorf("%s: no %s", region, k)
			}
		}
	}
}

func TestMath(t *testing.T) {
	for _, tc := range []struct {
		region string
		r      scan.LiveResource
		want   string
	}{
		{"us-east-1", scan.LiveResource{Type: "aws_ebs_volume", Class: "gp3", SizeGB: 100}, "gp3 100 GB × $0.08/GB-mo = $8.00/mo"},
		{"us-east-1", scan.LiveResource{Type: "aws_ebs_snapshot", SizeGB: 100}, "100 GB × $0.05/GB-mo = $5.00/mo"},
		{"us-east-1", scan.LiveResource{Type: "aws_eip"}, "$0.005/h × 730 h = $3.65/mo"},
		{"us-east-1", scan.LiveResource{Type: "aws_lb", Class: "application"}, "application $0.0225/h × 730 h = $16.43/mo"},
		{"us-east-1", scan.LiveResource{Type: "aws_instance", Class: "t3.large"}, "t3.large $0.0832/h × 730 h = $60.74/mo"},
		{"xx-nowhere-1", scan.LiveResource{Type: "aws_instance", Class: "t3.large"}, "t3.large $0.0832/h × 730 h = $60.74/mo (≈ us-east-1 price)"},
		{"us-east-1", scan.LiveResource{Type: "aws_instance", Class: "t3.large", Stopped: true}, "t3.large stopped, no compute charge = $0.00/mo"},
		{"us-east-1", scan.LiveResource{Type: "aws_security_group"}, "not priced = $0.00/mo"},
		{"us-east-1", scan.LiveResource{Type: "aws_db_instance", Class: "db.t3.micro", Nodes: 2}, "db.t3.micro $0.017/h × 730 h × 2 nodes = $24.82/mo"},
		{"af-south-1", scan.LiveResource{Type: "aws_elasticache_replication_group", Class: "cache.t3.micro", Nodes: 3}, "cache.t3.micro $0.017/h × 730 h × 3 nodes = $37.23/mo (≈ us-east-1 price)"},
		{"us-east-1", scan.LiveResource{Type: "aws_db_instance", Class: "db.t3.micro", Stopped: true}, "db.t3.micro stopped, no compute charge = $0.00/mo"},
		{"us-east-1", scan.LiveResource{Type: "aws_db_instance", Class: "db.t3.micro", Storage: "gp3", SizeGB: 20}, "db.t3.micro $0.017/h × 730 h + gp3 20 GB × $0.115/GB-mo = $14.71/mo"},
		{"us-east-1", scan.LiveResource{Type: "aws_db_instance", Class: "db.t3.micro", Storage: "gp3", SizeGB: 20, Nodes: 2}, "db.t3.micro $0.017/h × 730 h × 2 nodes + gp3 20 GB × $0.115/GB-mo × 2 = $29.42/mo"},
		{"us-east-1", scan.LiveResource{Type: "aws_db_instance", Class: "db.t3.micro", Storage: "gp3", SizeGB: 20, Stopped: true}, "db.t3.micro stopped, no compute charge + gp3 20 GB × $0.115/GB-mo = $2.30/mo"},
		{"us-east-1", scan.LiveResource{Type: "aws_db_snapshot", Class: "manual", SizeGB: 20}, "snapshot 20 GB × $0.095/GB-mo = $1.90/mo"},
		{"us-east-1", scan.LiveResource{Type: "aws_db_snapshot", Class: "automated", SizeGB: 20}, "automated backup, free up to DB size = $0.00/mo"},
		{"us-east-1", scan.LiveResource{Type: "aws_route53_zone"}, "$0.50/zone-mo = $0.50/mo"},
		{"us-east-1", scan.LiveResource{Type: "aws_s3_bucket"}, "size unknown = $0.00/mo"},
		{"us-east-1", scan.LiveResource{Type: "aws_cloudwatch_log_group", SizeGB: 4.657}, "4.657 GB × $0.03/GB-mo = $0.14/mo"},
		{"us-east-1", scan.LiveResource{Type: "aws_lambda_function"}, "requests and duration not estimated = $0.00/mo"},
	} {
		if got := Math(tc.region, tc.r); got != tc.want {
			t.Errorf("%s %s: got %q, want %q", tc.region, tc.r.Type, got, tc.want)
		}
	}
}
