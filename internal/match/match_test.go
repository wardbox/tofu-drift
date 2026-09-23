package match

import (
	"testing"

	"github.com/wardbox/tofu-drift/internal/state"
)

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
