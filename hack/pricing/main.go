// Command pricing regenerates the embedded pricing tables in internal/pricing
// from the AWS Price List Bulk API. Run from the repo root at release time:
//
//	go run ./hack/pricing
//
// See README.md in this directory.
package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"maps"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// regions get instance-class tables; others fall back to us-east-1 (docs/spec.md §5).
var regions = []string{
	"us-east-1", "us-east-2", "us-west-2", "eu-west-1", "eu-west-2",
	"eu-central-1", "ap-southeast-1", "ap-southeast-2", "ap-northeast-1", "ca-central-1",
}

const offerURL = "https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/%s/current/%s/index.csv"

const outDir = "internal/pricing/"

func main() {
	t := newTables()
	for _, region := range regions {
		for offer, collect := range map[string]func(row) error{
			"AmazonEC2":         t.ec2(region),
			"AmazonRDS":         t.rds(region),
			"AmazonElastiCache": t.elasticache(region),
		} {
			log.Printf("%s %s", offer, region)
			if err := fetch(fmt.Sprintf(offerURL, offer, region), collect); err != nil {
				log.Fatalf("%s %s: %v", offer, region, err)
			}
		}
	}
	if err := writeJSON(outDir+"instances.json", t); err != nil {
		log.Fatal(err)
	}
	// ebs.json covers more regions and types than the offer files (io2 is
	// absent from them); refresh what we fetched, keep the rest.
	ebs := map[string]map[string]float64{}
	b, err := os.ReadFile(outDir + "ebs.json")
	if err != nil {
		log.Fatal(err)
	}
	if err := json.Unmarshal(b, &ebs); err != nil {
		log.Fatal(err)
	}
	for region, rates := range t.EBS {
		if ebs[region] == nil {
			ebs[region] = map[string]float64{}
		}
		maps.Copy(ebs[region], rates)
	}
	if err := writeJSON(outDir+"ebs.json", ebs); err != nil {
		log.Fatal(err)
	}
}

func fetch(url string, collect func(row) error) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return readOffer(resp.Body, collect)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// row is one price line keyed by CSV header.
type row map[string]string

// readOffer streams a Price List CSV, calling collect for every price row.
func readOffer(r io.Reader, collect func(row) error) error {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // metadata lines are two fields wide
	cr.ReuseRecord = true
	var header []string
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if header == nil {
			if len(rec) > 2 {
				header = append([]string(nil), rec...)
			}
			continue
		}
		rw := make(row, len(header))
		for i, h := range header {
			if i < len(rec) {
				rw[h] = rec[i]
			}
		}
		if err := collect(rw); err != nil {
			return err
		}
	}
}

// tables is instances.json; EBS goes to ebs.json.
type tables struct {
	// VCPU per instance class; classes are unique across services by prefix (db., cache.).
	VCPU map[string]int `json:"vcpu"`
	// Hourly is on-demand USD per hour by region, then instance class.
	Hourly map[string]map[string]float64 `json:"hourly"`
	EBS    map[string]map[string]float64 `json:"-"`
}

func newTables() *tables {
	return &tables{VCPU: map[string]int{}, Hourly: map[string]map[string]float64{}, EBS: map[string]map[string]float64{}}
}

// onDemand reports whether r is a plain on-demand price in an AWS Region
// (not a Local Zone, Wavelength zone, or Outpost), excluding extended-support
// surcharges for end-of-life engine versions.
func onDemand(r row) bool {
	return r["TermType"] == "OnDemand" && r["Location Type"] == "AWS Region" &&
		!strings.Contains(r["usageType"], "ExtendedSupport")
}

// ec2 collects Linux, shared-tenancy, on-demand instance hours and EBS
// storage rates.
func (t *tables) ec2(region string) func(row) error {
	return func(r row) error {
		switch {
		case !onDemand(r):
		case strings.HasPrefix(r["Product Family"], "Compute Instance") && r["Unit"] == "Hrs" &&
			r["Tenancy"] == "Shared" && r["operation"] == "RunInstances" && r["CapacityStatus"] == "Used":
			return t.instance(region, r)
		case r["Product Family"] == "Storage" && r["Unit"] == "GB-Mo" && r["Volume API Name"] != "":
			return set(t.EBS, region, r["Volume API Name"], r["PricePerUnit"])
		}
		return nil
	}
}

// rds collects MySQL Single-AZ on-demand instance hours.
// ponytail: one engine; add engine keys when commercial engines need pricing.
func (t *tables) rds(region string) func(row) error {
	return func(r row) error {
		if onDemand(r) && r["Product Family"] == "Database Instance" && r["Unit"] == "Hrs" &&
			r["Database Engine"] == "MySQL" && r["Deployment Option"] == "Single-AZ" {
			return t.instance(region, r)
		}
		return nil
	}
}

// elasticache collects Redis on-demand node hours.
func (t *tables) elasticache(region string) func(row) error {
	return func(r row) error {
		if onDemand(r) && r["Product Family"] == "Cache Instance" && r["Unit"] == "Hrs" && r["Cache Engine"] == "Redis" {
			return t.instance(region, r)
		}
		return nil
	}
}

func (t *tables) instance(region string, r row) error {
	class := r["Instance Type"]
	if n, err := strconv.Atoi(r["vCPU"]); err == nil {
		t.VCPU[class] = n
	}
	return set(t.Hourly, region, class, r["PricePerUnit"])
}

// set records a price, failing on a second, different price for the same
// key: that means the row filter is too loose.
func set(m map[string]map[string]float64, region, key, price string) error {
	p, err := strconv.ParseFloat(price, 64)
	if err != nil {
		return fmt.Errorf("%s %s: price %q: %w", region, key, price, err)
	}
	if m[region] == nil {
		m[region] = map[string]float64{}
	}
	if old, ok := m[region][key]; ok && old != p {
		return fmt.Errorf("%s %s: two prices %v and %v, filter too loose", region, key, old, p)
	}
	m[region][key] = p
	return nil
}
