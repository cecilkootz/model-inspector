package tui

import (
	"fmt"

	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/sanitize"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

func compactFormat(format string) string {
	name, variant, _ := model.FormatName(format)
	if variant == "" {
		return name
	}
	sep := "·"
	if !ui.UTF8() {
		sep = "|"
	}
	// name is safetensors/gguf/unknown. The parenthetical variant is file metadata.
	return name + sep + sanitize.From(variant).String()
}

func quantHeader(m *model.Model) string {
	if m.Quant == nil || m.Quant.Bits <= 0 {
		d := model.DisplayDType(dominantDType(m))
		if d == "" {
			return ui.Dash()
		}
		// The dtype token comes from the file. The "qN gN" form below is ours.
		return sanitize.From(d).String()
	}
	s := fmt.Sprintf("q%d", m.Quant.Bits)
	if m.Quant.GroupSize > 0 {
		s += fmt.Sprintf(" g%d", m.Quant.GroupSize)
	}
	if m.Quant.NumOverrides > 0 {
		s += fmt.Sprintf(" +%d", m.Quant.NumOverrides)
	}
	return s
}

func dominantDType(m *model.Model) string {
	var best string
	var n int64 = -1
	for _, k := range model.SortedKeys(m.DTypes) {
		if m.DTypes[k].Bytes > n {
			n = m.DTypes[k].Bytes
			best = k
		}
	}
	return best
}

// incomplete is the header marker. It follows Model.Partial, which the
// loader sets for a missing, unreadable, or non-local shard as well as
// for a walk cap, a bad config, and a counter overflow.
func incomplete(m *model.Model) bool {
	return m != nil && m.Partial
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
