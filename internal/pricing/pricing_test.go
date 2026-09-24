package pricing

import (
	"math"
	"testing"

	"github.com/wardbox/tofu-drift/internal/scan"
)

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
