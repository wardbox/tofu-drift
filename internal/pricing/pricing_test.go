package pricing

import (
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
		if got := Monthly(tc.region, vol); got < tc.want-1e-9 || got > tc.want+1e-9 {
			t.Errorf("%s: got %v, want %v", tc.region, got, tc.want)
		}
	}
	if got := Monthly("us-east-1", scan.LiveResource{Type: "aws_widget"}); got != 0 {
		t.Errorf("unpriced type: got %v", got)
	}
}
