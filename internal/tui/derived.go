package tui

import (
	"fmt"
	"math"
	"strings"

	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
	"origin.cursor.com/cecilkootz/model-inspector/internal/tui/ui"
)

// derived is everything View needs that used to be recomputed per frame.
type derived struct {
	mem           model.MemoryEstimate
	storage       []model.StorageClass
	callouts      []model.Callout
	notableName   string
	notableParams int64
	notableNgram  bool
	heat          ui.HeatGrid
	modelType     string
}

func derive(m *model.Model) derived {
	if m == nil {
		return derived{}
	}
	d := derived{
		mem:      m.MemoryEstimate(),
		storage:  model.MergeStorage(m, m.StorageBreakdown(), 4),
		callouts: m.Outliers(),
		heat:     buildHeat(m),
	}
	d.notableName, d.notableParams, d.notableNgram = m.NotableParams()
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
	n, _ := m.LayerCount()
	maxL := -1
	for _, t := range m.Tensors {
		if t != nil && t.Kind == model.KindLayer && t.Layer > maxL {
			maxL = t.Layer
		}
	}
	if maxL+1 > n {
		n = maxL + 1
	}
	rows := []struct {
		sub, label string
	}{
		{"attn", "attention"},
		{"router", "router"},
		{"shared", "shared exp"},
		{"experts", "routed exp"},
		{"norms", "norms"},
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
			if t.Layer >= 0 && t.Layer < n {
				addBytes(&cells[ri][t.Layer], nbytes)
			}
		case model.KindMTP:
			addBytes(&mtp[ri], nbytes)
			mtpAny[ri] = true
		}
	}
	out := ui.HeatGrid{Rows: make([]ui.HeatRow, len(rows))}
	for i, r := range rows {
		out.Rows[i] = ui.HeatRow{Label: r.label, Cells: cells[i], MTP: mtp[i], HasMTP: mtpAny[i]}
	}
	types := m.LayerTypes()
	out.Full = make([]bool, n)
	for i := 0; i < n && i < len(types); i++ {
		out.Full[i] = types[i] == "full_attention"
	}
	out.Note = totalsNote(cells)
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
	out.Caption = strings.Join(caps, ui.SepWord())
	return out
}

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
		return "per-layer totals differ by <1%"
	}
	pct := float64(max-min) / float64(med) * 100
	if pct < 1 {
		return "per-layer totals differ by <1%"
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
