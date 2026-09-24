package match

import (
	"testing"

	"github.com/wardbox/tofu-drift/internal/scan"
	"github.com/wardbox/tofu-drift/internal/state"
)

func TestRoots(t *testing.T) {
	roots := Roots([]scan.LiveResource{
		{Key: "asg-1", Derived: []string{"i-1"}},
		{Key: "i-1", Derived: []string{"vol-1"}},
		{Key: "vol-1"},
		{Key: "vol-2"},
	})
	if roots["vol-1"] != "asg-1" || roots["i-1"] != "asg-1" || len(roots) != 2 {
		t.Errorf("roots: %v", roots)
	}
}

func TestIndex(t *testing.T) {
	idx := Index([]state.Resource{
		{Address: "aws_ebs_volume.a", Type: "aws_ebs_volume", Attributes: map[string]any{"id": "vol-1"}},
		{Address: "aws_ebs_volume.noid", Type: "aws_ebs_volume", Attributes: map[string]any{}},
		{Address: "aws_widget.x", Type: "aws_widget", Attributes: map[string]any{"id": "vol-2"}},
	})
	if got := idx.Lookup("aws_ebs_volume", "vol-1"); got != "aws_ebs_volume.a" {
		t.Errorf("vol-1: got %q", got)
	}
	if got := idx.Lookup("aws_ebs_volume", "vol-2"); got != "" {
		t.Errorf("unknown state types must be ignored, got %q", got)
	}
	if got := idx.Lookup("aws_ebs_volume", ""); got != "" {
		t.Errorf("empty key must never match, got %q", got)
	}
}

func TestIndexPlumbing(t *testing.T) {
	idx := Index([]state.Resource{
		{Address: "aws_vpc.app", Type: "aws_vpc", Attributes: map[string]any{"id": "vpc-1"}},
		{Address: "aws_subnet.a", Type: "aws_subnet", Attributes: map[string]any{"id": "subnet-1"}},
		{Address: "aws_route_table.a", Type: "aws_route_table", Attributes: map[string]any{"id": "rtb-1"}},
		{Address: "aws_security_group.web", Type: "aws_security_group", Attributes: map[string]any{"id": "sg-1"}},
		{Address: "aws_default_security_group.d", Type: "aws_default_security_group", Attributes: map[string]any{"id": "sg-d"}},
		{Address: "aws_default_route_table.d", Type: "aws_default_route_table", Attributes: map[string]any{"id": "rtb-d"}},
	})
	for typ, want := range map[[2]string]string{
		{"aws_vpc", "vpc-1"}:           "aws_vpc.app",
		{"aws_subnet", "subnet-1"}:     "aws_subnet.a",
		{"aws_route_table", "rtb-1"}:   "aws_route_table.a",
		{"aws_security_group", "sg-1"}: "aws_security_group.web",
		{"aws_security_group", "sg-d"}: "aws_default_security_group.d",
		{"aws_route_table", "rtb-d"}:   "aws_default_route_table.d",
	} {
		if got := idx.Lookup(typ[0], typ[1]); got != want {
			t.Errorf("%v: got %q, want %q", typ, got, want)
		}
	}
}

func TestFurniture(t *testing.T) {
	for _, tc := range []struct {
		r    scan.LiveResource
		want bool
	}{
		{scan.LiveResource{Type: "aws_vpc", Default: true}, true},
		{scan.LiveResource{Type: "aws_subnet", Default: true}, true},
		{scan.LiveResource{Type: "aws_route_table", Default: true}, true},
		{scan.LiveResource{Type: "aws_security_group", Default: true}, true},
		{scan.LiveResource{Type: "aws_security_group"}, false},
		// Default only means furniture for types in the table.
		{scan.LiveResource{Type: "aws_ebs_volume", Default: true}, false},
	} {
		if got := Furniture(tc.r); got != tc.want {
			t.Errorf("%+v: got %v, want %v", tc.r, got, tc.want)
		}
	}
}
