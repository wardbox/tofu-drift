package state

import (
	"os"
	"strings"
	"testing"
)

func TestParseV4(t *testing.T) {
	f, err := os.Open("testdata/v4.tfstate")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	rs, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}

	want := []struct {
		addr, typ, id, name string
	}{
		{`aws_instance.web`, "aws_instance", "i-0123456789abcdef0", "web"},
		{`module.storage.aws_ebs_volume.data["a"]`, "aws_ebs_volume", "vol-0aaa", ""},
		{`module.storage.aws_ebs_volume.data["b"]`, "aws_ebs_volume", "vol-0bbb", ""},
		{`module.net.module.subnets.aws_subnet.private[0]`, "aws_subnet", "subnet-0", ""},
		{`module.net.module.subnets.aws_subnet.private[1]`, "aws_subnet", "subnet-1", ""},
	}
	if len(rs) != len(want) {
		t.Fatalf("got %d resources, want %d (data resources must be skipped)", len(rs), len(want))
	}
	for i, w := range want {
		r := rs[i]
		if r.Address != w.addr || r.Type != w.typ || r.ID != w.id || r.Tags["Name"] != w.name {
			t.Errorf("resource %d: got %+v, want %+v", i, r, w)
		}
	}
	if rs[0].Attributes["instance_type"] != "t3.micro" {
		t.Errorf("attributes not preserved: %v", rs[0].Attributes)
	}
}

func TestParseRejectsOtherVersions(t *testing.T) {
	_, err := Parse(strings.NewReader(`{"version": 3, "resources": []}`))
	if err == nil || !strings.Contains(err.Error(), "version 3") {
		t.Fatalf("want version error, got %v", err)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse(strings.NewReader(`not json`)); err == nil {
		t.Fatal("want error")
	}
}
