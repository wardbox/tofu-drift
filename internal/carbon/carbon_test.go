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
	hdd := Monthly("us-east-1", scan.LiveResource{Type: "aws_ebs_volume", Class: "sc1", SizeGB: 1000})
	if hdd >= got || hdd == 0 {
		t.Errorf("hdd should be below ssd and nonzero: %v vs %v", hdd, got)
	}
}

func TestFlights(t *testing.T) {
	if got := Flights(15); math.Abs(got-0.02) > 1e-9 {
		t.Errorf("got %v", got)
	}
}
