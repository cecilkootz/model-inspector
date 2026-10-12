package tui

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/sanitize"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

// derived is everything View needs that used to be recomputed per frame.
type derived struct {
	mem       model.MemoryEstimate
	storage   []model.StorageClass
	callouts  []model.Callout
	heat      ui.HeatGrid
	modelType string
}

func derive(m *model.Model) derived {
	if m == nil {
		return derived{}
	}
	// An absurd layer index has to be partial before the memory estimate,
	// so the accelerator line is omitted for the same reason as any other
	// incomplete host figure.
	m.NoteAbsurdLayers()
	d := derived{
		mem:      m.MemoryEstimate(),
		storage:  model.MergeStorage(m, m.StorageBreakdown(), 4),
		callouts: m.Outliers(),
		heat:     buildHeat(m),
	}
	for _, kv := range m.KeyConfig() {
		if kv.Key == "model_type" {
			s := strings.TrimSpace(fmt.Sprint(kv.Value))
			if s != "" && s != "<nil>" {
				d.modelType = s
			}
		}
	}
	return d
}

func buildHeat(m *model.Model) ui.HeatGrid {
	// Columns follow the distinct in-range layer indices, not the largest
	// index. model.layers.99999999 must not allocate a multi-gigabyte row.
	seen := map[int]struct{}{}
	var skipped int
	for _, t := range m.Tensors {
		if t == nil || t.Kind != model.KindLayer || t.Layer < 0 {
			continue
		}
		if t.Layer > model.MaxLayerIndex {
			skipped++
			continue
		}
		seen[t.Layer] = struct{}{}
	}
	layers := make([]int, 0, len(seen))
	for layer := range seen {
		layers = append(layers, layer)
	}
	sort.Ints(layers)
	n := len(layers)
	col := make(map[int]int, n)
	for i, layer := range layers {
		col[layer] = i
	}
	rows := []struct {
		sub   string
		label sanitize.Text
	}{
		{"attn", ui.L("attention")},
		{"router", ui.L("router")},
		{"shared", ui.L("shared exp")},
		{"experts", ui.L("routed exp")},
		{"norms", ui.L("norms")},
	}
	cells := make([][]int64, len(rows))
	for i := range cells {
		cells[i] = make([]int64, n)
	}
	var mtp [5]int64
	var mtpAny [5]bool
	index := map[string]int{}
	for i, r := range rows {
		index[r.sub] = i
	}
	var ple, hyper, other int64
	for _, t := range m.Tensors {
		if t == nil {
			continue
		}
		sub, _ := model.Sublayer(t.Name)
		nbytes := t.ByteLen()
		if nbytes < 0 {
			continue
		}
		switch sub {
		case "ple":
			addBytes(&ple, nbytes)
			continue
		case "hyper":
			addBytes(&hyper, nbytes)
			continue
		case "other":
			if t.Kind == model.KindLayer || t.Kind == model.KindMTP {
				addBytes(&other, nbytes)
			}
			continue
		}
		ri, ok := index[sub]
		if !ok {
			continue
		}
		switch t.Kind {
		case model.KindLayer:
			if c, ok := col[t.Layer]; ok {
				addBytes(&cells[ri][c], nbytes)
			}
		case model.KindMTP:
			addBytes(&mtp[ri], nbytes)
			mtpAny[ri] = true
		}
	}
	out := ui.HeatGrid{Rows: make([]ui.HeatRow, len(rows)), Layers: layers}
	for i, r := range rows {
		out.Rows[i] = ui.HeatRow{Label: ui.S(r.label), Cells: cells[i], MTP: mtp[i], HasMTP: mtpAny[i]}
	}
	types := m.LayerTypes()
	out.Full = make([]bool, n)
	for i, layer := range layers {
		if layer >= 0 && layer < len(types) && types[layer] == "full_attention" {
			out.Full[i] = true
		}
	}
	// The note and caption are program text (a fixed phrase plus counts).
	// sanitize.From would turn the "<" in "<1%" into a <U+003C> token.
	out.Note = ui.Chrome(totalsNote(cells))
	var caps []string
	if ple > 0 {
		caps = append(caps, "excludes ple "+model.HumanBytes(ple))
	}
	if hyper > 0 {
		caps = append(caps, "excludes hyper "+model.HumanBytes(hyper))
	}
	if other > 0 {
		caps = append(caps, "other "+model.HumanBytes(other)+" not in the grid")
	}
	if skipped > 0 {
		caps = append(caps, fmt.Sprintf("%d layer indices above %d left out", skipped, model.MaxLayerIndex))
	}
	out.Caption = ui.Chrome(strings.Join(caps, ui.SepWord()))
	out.Prepare()
	return out
}

// noteTotalsFlat is program text. It stays a real "<" so the heatmap
// reads "differ by <1%" instead of a sanitizer token.
const noteTotalsFlat = "per-layer totals differ by <1%"

func totalsNote(cells [][]int64) string {
	if len(cells) == 0 || len(cells[0]) == 0 {
		return ""
	}
	n := len(cells[0])
	tot := make([]int64, n)
	for _, row := range cells {
		for i, v := range row {
			if i < n {
				addBytes(&tot[i], v)
			}
		}
	}
	min, max := int64(0), int64(0)
	var pos int
	for _, v := range tot {
		if v <= 0 {
			continue
		}
		pos++
		if min == 0 || v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	if pos == 0 || min <= 0 {
		return ""
	}
	med := min
	if max == min {
		return noteTotalsFlat
	}
	pct := float64(max-min) / float64(med) * 100
	if pct < 1 {
		return noteTotalsFlat
	}
	return fmt.Sprintf("per-layer totals differ by %.1f%%", pct)
}

func addBytes(dst *int64, n int64) {
	if dst == nil || n <= 0 || *dst < 0 {
		return
	}
	if *dst > math.MaxInt64-n {
		*dst = math.MaxInt64
		return
	}
	*dst += n
}
