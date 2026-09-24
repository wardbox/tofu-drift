// Package carbon turns a live resource into a monthly kgCO₂ Estimate using
// the Cloud Carbon Footprint coefficients embedded in carbon.json.
package carbon

import (
	_ "embed"
	"encoding/json"

	"github.com/wardbox/tofu-drift/internal/pricing"
	"github.com/wardbox/tofu-drift/internal/scan"
)

//go:embed carbon.json
var coefficientsJSON []byte

var coef struct {
	PUE            float64                    `json:"pue"`
	ComputeWatts   struct{ Min, Max float64 } `json:"compute_watts_per_vcpu"`
	StorageWh      map[string]float64         `json:"storage_wh_per_tb_hour"`
	EBSReplication float64                    `json:"ebs_replication"`
	Grid           map[string]float64         `json:"grid_kg_per_kwh"`
}

func init() {
	if err := json.Unmarshal(coefficientsJSON, &coef); err != nil {
		panic(err)
	}
}

// kgPerFlight is one passenger's one-way trans-Atlantic flight.
const kgPerFlight = 750

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
	case "aws_db_snapshot":
		if r.Class == "automated" { // folded into its instance, free up to DB size
			return 0
		}
		return storage(region, "hdd", r.SizeGB)
	case "aws_instance", "aws_db_instance", "aws_elasticache_cluster", "aws_elasticache_replication_group":
		var disk float64
		if r.Type == "aws_db_instance" {
			// RDS storage is SSD like EBS, stopped or not, once per Multi-AZ copy.
			disk = storage(region, "ssd", r.SizeGB*float64(pricing.Nodes(r)))
		}
		if r.Stopped {
			return disk
		}
		watts := float64(pricing.VCPU(r.Class)*pricing.Nodes(r)) * (coef.ComputeWatts.Min + coef.ComputeWatts.Max) / 2
		return disk + watts*pricing.HoursPerMonth*coef.PUE/1000*grid(region)
	case "aws_ebs_volume":
		disk := "ssd"
		switch r.Class {
		case "st1", "sc1", "standard":
			disk = "hdd"
		}
		return storage(region, disk, r.SizeGB)
	case "aws_ebs_snapshot", "aws_cloudwatch_log_group":
		return storage(region, "hdd", r.SizeGB)
	}
	return 0
}

// storage is the kgCO₂/mo of gb of replicated EBS-class storage on disk.
func storage(region, disk string, gb float64) float64 {
	kwh := gb / 1000 * pricing.HoursPerMonth * coef.StorageWh[disk] * coef.EBSReplication * coef.PUE / 1000
	return kwh * grid(region)
}
