// Package pricing turns a live resource into a monthly USD Estimate from
// static tables embedded in the binary. Never queried live.
package pricing

import (
	_ "embed"
	"encoding/json"

	"github.com/wardbox/tofu-drift/internal/scan"
)

//go:embed ebs.json
var ebsJSON []byte

// ebs is USD per GB-month by region, then volume type. Refreshed at release
// time by hack/pricing/. Unlisted regions fall back to us-east-1.
// ponytail: hand-seeded for the common regions; #8 generates every region.
var ebs map[string]map[string]float64

func init() {
	if err := json.Unmarshal(ebsJSON, &ebs); err != nil {
		panic(err)
	}
}

// ebsRate is the USD per GB-month for a volume type in region.
func ebsRate(region, class string) float64 {
	rates, ok := ebs[region]
	if !ok {
		rates = ebs["us-east-1"]
	}
	return rates[class]
}

// Monthly is the USD/mo Estimate for r in region; 0 for unpriced types.
func Monthly(region string, r scan.LiveResource) float64 {
	switch r.Type {
	case "aws_ebs_volume":
		return r.SizeGB * ebsRate(region, r.Class)
	}
	return 0
}
