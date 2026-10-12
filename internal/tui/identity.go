package tui

import (
	"fmt"
	"strings"

	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/sanitize"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

func tileSpecs(m *model.Model, d derived) []ui.TileSpec {
	layers, _ := m.LayerCount()
	files := fmt.Sprintf("%s files", model.Comma(int64(len(m.Files))))
	if len(m.Shards) > 0 {
		files += ui.SepWord() + fmt.Sprintf("%d shards", len(m.Shards))
	}
	params := "~" + model.HumanCount(m.TotalLogicalParams)
	quant := quantHeader(m)
	if quant == "" {
		quant = ui.Dash()
	}
	layerSub := "layers"
	if layers > 0 {
		lin, full := 0, 0
		for _, t := range m.LayerTypes() {
			if t == "full_attention" {
				full++
			} else if t != "" {
				lin++
			}
		}
		if lin+full > 0 {
			layerSub = fmt.Sprintf("%s%d lin%s%s%d full", ui.G().Linear, lin, ui.SepWord(), ui.G().Full, full)
		}
	}
	experts := ui.Dash()
	expertSub := ""
	if n, per, ok := moeCounts(m); ok {
		experts = fmt.Sprintf("%d", n)
		if per > 0 {
			experts = fmt.Sprintf("%d%s%d/tok", n, ui.SepWord(), per)
		}
		expertSub = "routed"
	}
	edge := func(int) string { return ui.RuleDim.Hex() }
	return []ui.TileSpec{
		{Label: ui.L("SIZE ON DISK"), Value: sanitize.Program(model.HumanBytes(m.TotalSize)), Sub: sanitize.Program(files), Edge: edge(0)},
		{Label: ui.L("PARAMETERS"), Value: sanitize.Program(params), Sub: sanitize.Program(fmt.Sprintf("%s tensors", model.Comma(int64(len(m.Tensors))))), Edge: edge(1)},
		{Label: ui.L("QUANT"), Value: sanitize.Program(quant), Sub: quantSub(m), Edge: edge(2)},
		{Label: ui.L("LAYERS"), Value: sanitize.Program(fmt.Sprintf("%d", layers)), Sub: sanitize.Program(layerSub), Edge: edge(3)},
		{Label: ui.L("EXPERTS"), Value: sanitize.Program(experts), Sub: sanitize.Program(expertSub), Edge: edge(4)},
		{Label: ui.L("WEIGHTS"), Value: sanitize.Program(ui.Geq() + " " + model.HumanBytes(d.mem.WeightsBytes)), Sub: ui.L("weights only"), Edge: edge(5)},
	}
}

func quantSub(m *model.Model) sanitize.Text {
	if m.Quant == nil {
		return sanitize.Program("")
	}
	// Mode is a config string. The fallback word is ours.
	if m.Quant.Mode == "" {
		return sanitize.Program("quant")
	}
	return sanitize.From(m.Quant.Mode)
}

func moeCounts(m *model.Model) (n, per int, ok bool) {
	for _, kv := range m.KeyConfig() {
		switch kv.Key {
		case "num_experts", "num_local_experts", "n_routed_experts":
			if !ok {
				n = intOf(kv.Value)
				ok = n > 0
			}
		case "num_experts_per_tok":
			per = intOf(kv.Value)
		}
	}
	return n, per, ok
}

func intOf(v any) int {
	f, _ := v.(float64)
	return int(f)
}

func classLabel(class string) sanitize.Text {
	// The patterns below are disjoint, so the order of the checks does not
	// decide whether a file dtype is sanitized.
	switch class {
	case "q_scales_biases":
		return sanitize.Program("scales/biases")
	case "other":
		return sanitize.Program("other")
	}
	if label, ok := packedQuantLabel(class); ok {
		return sanitize.Program(label)
	}
	if dt, ok := strings.CutPrefix(class, "plain_"); ok {
		return sanitize.Program("plain " + sanitize.From(dt).String())
	}
	return sanitize.From(class)
}

// packedQuantLabel matches only q<digits>_packed, the class this program
// builds from a bit width. plain_<dtype>_packed is not this shape.
func packedQuantLabel(class string) (string, bool) {
	rest, ok := strings.CutPrefix(class, "q")
	if !ok {
		return "", false
	}
	digits, ok := strings.CutSuffix(rest, "_packed")
	if !ok || digits == "" {
		return "", false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	return "q" + digits + " packed", true
}

func hasKind(m *model.Model, kind string) bool {
	for _, g := range m.Groups {
		if g.Kind == kind {
			return true
		}
	}
	return false
}
