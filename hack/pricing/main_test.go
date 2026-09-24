package main

import (
	"strings"
	"testing"
)

// A trimmed Price List CSV: five metadata lines, the header, then rows.
const offerCSV = `"FormatVersion","v1.0"
"Disclaimer","x"
"Publication Date","2026-09-21T19:47:12Z"
"Version","20260921194712"
"OfferCode","AmazonEC2"
"TermType","Unit","PricePerUnit","Product Family","Instance Type","vCPU","Tenancy","operation","CapacityStatus","Volume API Name","Location Type"
"OnDemand","Hrs","0.0832","Compute Instance","t3.large","2","Shared","RunInstances","Used","","AWS Region"
"OnDemand","Hrs","0.0832","Compute Instance","t3.large","2","Dedicated","RunInstances","Used","","AWS Region"
"OnDemand","Hrs","0.1832","Compute Instance","t3.large","2","Shared","RunInstances:0002","Used","","AWS Region"
"Reserved","Hrs","0.05","Compute Instance","t3.large","2","Shared","RunInstances","Used","","AWS Region"
"OnDemand","Hrs","4.2","Compute Instance (bare metal)","m5.metal","96","Shared","RunInstances","Used","","AWS Region"
"OnDemand","GB-Mo","0.08","Storage","","","","","","gp3","AWS Region"
"OnDemand","GB-Mo","0.1","Storage","","","","","","gp3","AWS Local Zone"
`

func TestReadOffer(t *testing.T) {
	var got []row
	err := readOffer(strings.NewReader(offerCSV), func(r row) error {
		got = append(got, r)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 7 || got[0]["Instance Type"] != "t3.large" || got[6]["Location Type"] != "AWS Local Zone" {
		t.Fatalf("rows: %v", got)
	}
}

func TestCollect(t *testing.T) {
	tables := newTables()
	if err := readOffer(strings.NewReader(offerCSV), tables.ec2("us-east-1")); err != nil {
		t.Fatal(err)
	}
	if got := tables.Hourly["us-east-1"]["t3.large"]; got != 0.0832 {
		t.Errorf("t3.large hourly: %v (want Linux shared on-demand only)", got)
	}
	if got := tables.Hourly["us-east-1"]["m5.metal"]; got != 4.2 {
		t.Errorf("bare metal hourly: %v", got)
	}
	if tables.VCPU["t3.large"] != 2 || tables.VCPU["m5.metal"] != 96 {
		t.Errorf("vcpu: %v", tables.VCPU)
	}
	if got := tables.EBS["us-east-1"]["gp3"]; got != 0.08 {
		t.Errorf("gp3 rate: %v (want region, not local zone)", got)
	}
}

func TestCollectConflict(t *testing.T) {
	tables := newTables()
	conflicting := offerCSV + `"OnDemand","Hrs","0.09","Compute Instance","t3.large","2","Shared","RunInstances","Used","","AWS Region"` + "\n"
	if err := readOffer(strings.NewReader(conflicting), tables.ec2("us-east-1")); err == nil {
		t.Error("two prices for one class must fail, not silently pick one")
	}
}
