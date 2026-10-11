package scan

import (
	"strings"
	"testing"

	"github.com/cecilkootz/model-inspector/internal/model"
)

func TestSpoofedLayerWarningIsNotTheCheck(t *testing.T) {
	m := &model.Model{
		Partial:  true,
		Warnings: []string{"layer index 1 exceeds 4096; dropped\x1b"},
	}
	c := plannedMetadata(m)
	if c.Name != "Config vs tensor shapes" || c.Status != StatusPlanned {
		t.Fatalf("spoofed warning became the check: %+v", c)
	}
	if strings.Contains(c.Summary, "\x1b") {
		t.Fatalf("summary printed the spoof: %q", c.Summary)
	}
	if layerDropsOnly(m) {
		t.Fatal("spoofed warning counted as the only partial cause")
	}
}
