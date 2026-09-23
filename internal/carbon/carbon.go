// Package carbon turns a live resource into a monthly kgCO₂ Estimate using
// the Cloud Carbon Footprint coefficients embedded in carbon.json.
package carbon

import (
	_ "embed"
	"encoding/json"

	"github.com/wardbox/tofu-drift/internal/scan"
)

//go:embed carbon.json
var coefficientsJSON []byte

var coef struct {
	PUE            float64            `json:"pue"`
	StorageWh      map[string]float64 `json:"storage_wh_per_tb_hour"`
	EBSReplication float64            `json:"ebs_replication"`
	Grid           map[string]float64 `json:"grid_kg_per_kwh"`
}

func init() {
	if err := json.Unmarshal(coefficientsJSON, &coef); err != nil {
		panic(err)
	}
}

// kgPerFlight is one passenger's one-way trans-Atlantic flight.
const kgPerFlight = 750

const hoursPerMonth = 730

// Flights expresses kg of CO₂ as trans-Atlantic flights.
func Flights(kg float64) float64 { return kg / kgPerFlight }

// grid is the kg CO₂ per kWh for region; unlisted regions use us-east-1.
func grid(region string) float64 {
	if g, ok := coef.Grid[region]; ok {
		return g
	}
	return coef.Grid["us-east-1"]
}

// Monthly is the kgCO₂/mo Estimate for r in region; 0 for plumbing types.
func Monthly(region string, r scan.LiveResource) float64 {
	switch r.Type {
	case "aws_ebs_volume":
		disk := "ssd"
		switch r.Class {
		case "st1", "sc1", "standard":
			disk = "hdd"
		}
		kwh := r.SizeGB / 1000 * hoursPerMonth * coef.StorageWh[disk] * coef.EBSReplication * coef.PUE / 1000
		return kwh * grid(region)
	}
	return 0
}
