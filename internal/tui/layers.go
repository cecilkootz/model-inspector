package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/sanitize"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

// layersState holds the caches and test hooks behind the Layers panel. The
// panel's own list and cursor live in side.
type layersState struct {
	gen int

	san  map[string]sanitize.Text
	pack map[int][]tensorPack

	side layersSide
}

type tensorPack struct {
	rows []model.LogicalTensor
}

var subOrder = []string{"attn", "router", "shared", "experts", "norms", "hyper", "ple", "other"}

func newLayersState() *layersState {
	return &layersState{
		san:  map[string]sanitize.Text{},
		pack: map[int][]tensorPack{},
	}
}

func (s *layersState) framePart() string {
	if s == nil {
		return ""
	}
	return fmt.Sprintf("|ly %d", s.gen)
}

func (t *Model) touchLayers() {
	if t.lay != nil {
		t.lay.gen++
	}
	if t.cache != nil {
		t.cache.key = ""
	}
}

func (s *layersState) packs(m *model.Model, gidx int) []tensorPack {
	if s.pack != nil {
		if p, ok := s.pack[gidx]; ok {
			return p
		}
	}
	var out []tensorPack
	if m == nil || gidx < 0 || gidx >= len(m.Groups) || m.Groups[gidx] == nil {
		out = nil
	} else {
		g := m.Groups[gidx]
		logical := g.LogicalTensors(m)
		if g.Kind != model.KindLayer {
			out = []tensorPack{{rows: logical}}
		} else {
			buckets := map[string][]model.LogicalTensor{}
			var seenOrder []string
			for _, row := range logical {
				sub, _ := model.Sublayer(row.FullName)
				if _, ok := buckets[sub]; !ok {
					seenOrder = append(seenOrder, sub)
				}
				buckets[sub] = append(buckets[sub], row)
			}
			used := map[string]bool{}
			for _, name := range subOrder {
				if rows, ok := buckets[name]; ok {
					out = append(out, tensorPack{rows: rows})
					used[name] = true
				}
			}
			for _, name := range seenOrder {
				if !used[name] {
					out = append(out, tensorPack{rows: buckets[name]})
				}
			}
		}
	}
	if s.pack == nil {
		s.pack = map[int][]tensorPack{}
	}
	s.pack[gidx] = out
	return out
}

func (s *layersState) once(raw string) sanitize.Text {
	if s.san == nil {
		s.san = map[string]sanitize.Text{}
	}
	if t, ok := s.san[raw]; ok {
		return t
	}
	t := sanitize.From(raw)
	s.san[raw] = t
	return t
}

func (t *Model) copyLayerPayload(payload string) {
	if payload == "" {
		t.notice = "nothing to copy"
		t.touchLayers()
		return
	}
	if len(payload) > configCopyMax {
		t.notice = "too large to copy"
		t.touchLayers()
		return
	}
	if t.clipboardOff() {
		t.notice = "not copied (OSC 52 disabled); select it from the value strip"
		t.touchLayers()
		return
	}
	n := controlCount(payload)
	text := payload
	if n > 0 {
		text = sanitize.From(payload).String()
		t.notice = fmt.Sprintf("copied with %d control characters shown as tokens", n)
	} else {
		t.notice = "copied path" + cfgSep() + clipCells(payload, 80)
	}
	if !t.canCopy() {
		if n == 0 {
			t.notice = "not copied (terminal blocked OSC 52); select it from the value strip"
		}
		t.touchLayers()
		return
	}
	writeOSC(t.copyWriter(), text)
	t.touchLayers()
}

func (t Model) layerGroupLabel(g *model.Group) sanitize.Text {
	if g == nil {
		return sanitize.Program("")
	}
	if g.Key == "ngram" {
		label := "n-gram table (PLE)"
		if mark := t.ngramMark(g); mark != "" {
			label += " " + mark + " shards"
		}
		return sanitize.Program(label)
	}
	if g.Kind == model.KindLayer {
		return sanitize.Program(strconv.Itoa(g.Layer))
	}
	if t.lay == nil {
		return sanitize.From(g.Key)
	}
	return t.lay.once(g.Key)
}

func (t Model) layerDType(g *model.Group) sanitize.Text {
	if g == nil {
		return sanitize.Program("")
	}
	label := g.StorageLabel()
	if t.lay == nil {
		return sanitize.From(label)
	}
	return t.lay.once(label)
}

func (t Model) layerTensorLabel(g *model.Group, row model.LogicalTensor) sanitize.Text {
	name := row.Name
	if name == "" {
		name = row.FullName
	}
	if g != nil && g.Kind == model.KindLayer {
		name = model.ShortName(name)
	} else {
		name = tailName(name)
	}
	if t.lay == nil {
		return sanitize.From(name)
	}
	return t.lay.once(name)
}

func tailName(name string) string {
	parts := strings.Split(name, ".")
	if len(parts) <= 2 {
		return name
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

func shapeLimited(shape []int64) string {
	if len(shape) == 0 {
		return "[]"
	}
	n := len(shape)
	extra := 0
	if n > 8 {
		extra = n - 8
		n = 8
	}
	var b strings.Builder
	b.Grow(n * 4)
	b.WriteByte('[')
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatInt(shape[i], 10))
		if b.Len() > 48 {
			extra += len(shape) - i - 1
			break
		}
	}
	if extra > 0 {
		b.WriteString(",")
		b.WriteString(ui.G().Ellipsis)
	}
	b.WriteByte(']')
	return b.String()
}

func (t Model) ngramMark(g *model.Group) string {
	if g == nil || g.Key != "ngram" || t.lay == nil || t.m == nil {
		return ""
	}
	// gidx
	gi := -1
	for i, gg := range t.m.Groups {
		if gg == g {
			gi = i
			break
		}
	}
	if gi < 0 {
		return ""
	}
	packs := t.lay.packs(t.m, gi)
	shards := 0
	host := false
	for _, p := range packs {
		for _, row := range p.rows {
			if row.Shards > shards {
				shards = row.Shards
			}
			if row.Host {
				host = true
			}
		}
	}
	if shards < 1 && !host {
		return ""
	}
	gl := ui.G()
	var b strings.Builder
	if host {
		b.WriteString(gl.Host)
		b.WriteByte(' ')
	}
	if shards > 1 || host {
		b.WriteString(ui.Times())
		b.WriteString(strconv.Itoa(shards))
	}
	return b.String()
}
