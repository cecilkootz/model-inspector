package tui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/sanitize"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

const (
	modelOverview = iota
	modelStorage
	modelMemory
	modelCallouts
	modelFiles
)

const (
	overviewCallouts = 3
	calloutWrap      = 3
	maxFileRows      = 1000
	tileMinWidth     = 13
	tileGap          = 2
)

type modelPanel struct{ t *Model }

func (p *modelPanel) num() int { return 1 }

func (p *modelPanel) title() sanitize.Text { return ui.L("Model") }

func (p *modelPanel) counter() string {
	if p.t.derived == nil || len(p.t.derived.callouts) == 0 {
		return ""
	}
	n := len(p.t.derived.callouts)
	if n == 1 {
		return muted("1 note")
	}
	return muted(strconv.Itoa(n) + " notes")
}

func (p *modelPanel) d() derived {
	if p.t.derived != nil {
		return *p.t.derived
	}
	return derived{}
}

func (p *modelPanel) items() []struct{ name, value string } {
	m := p.t.m
	out := []struct{ name, value string }{
		{"Overview", ""},
		{"Storage", model.HumanBytes(m.TotalSize)},
		{"Memory", ""},
		{"Callouts", ""},
		{"Files", strconv.Itoa(len(m.Files))},
	}
	if d := p.t.derived; d != nil {
		out[modelMemory].value = ui.Geq() + " " + model.HumanBytes(d.mem.WeightsBytes)
		out[modelCallouts].value = strconv.Itoa(len(d.callouts))
	}
	return out
}

func (p *modelPanel) rows(width int) []string {
	cur := p.cursor()
	var out []string
	for i, it := range p.items() {
		out = append(out, ui.ListRow(ui.RowSpec{
			Name: sanitize.Program(it.name), Value: sanitize.Program(it.value), Selected: i == cur,
		}, width))
	}
	return out
}

// summary is Overview, Storage and Callouts: the sections worth a glance
// while another panel has focus.
func (p *modelPanel) summary(width int) []string {
	items := p.items()
	var out []string
	for _, i := range []int{modelOverview, modelStorage, modelCallouts} {
		out = append(out, plainRow(sanitize.Program(items[i].name), sanitize.Program(items[i].value), width))
	}
	return out
}

func (p *modelPanel) cursor() int { return clampInt(p.t.sh.modelCur, 0, len(p.items())-1) }

func (p *modelPanel) move(d int) { p.t.sh.modelCur = clampInt(p.t.sh.modelCur+d, 0, len(p.items())-1) }

func (p *modelPanel) selection() (sanitize.Text, ui.Safe) {
	it := p.items()[p.cursor()]
	var tag string
	switch p.cursor() {
	case modelStorage, modelCallouts, modelFiles:
		tag = it.value
	}
	return sanitize.Program(it.name), ui.Chrome(muted(tag))
}

func (p *modelPanel) keys() []ui.KeyHint { return moveHints() }

func (p *modelPanel) problems() []int { return nil }

func (p *modelPanel) detail(w, _ int) []string {
	m, d := p.t.m, p.d()
	w = max(w, 1)
	switch p.cursor() {
	case modelStorage:
		return storageDetail(m, d, w)
	case modelMemory:
		return memoryDetail(m, d, w)
	case modelCallouts:
		return calloutsDetail(d, w)
	case modelFiles:
		return filesDetail(m, w)
	default:
		return overviewDetail(m, d, w)
	}
}

// Text helpers. Everything here takes plain strings that are already safe:
// sanitized file text or program text.

func paint(s string, tok ui.Token, bold bool) string { return ui.PaintToken(s, tok, nil, bold) }

func padTo(s string, w int) string {
	if n := ui.Width(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

func padLeft(s string, w int) string {
	if n := ui.Width(s); n < w {
		return strings.Repeat(" ", w-n) + s
	}
	return s
}

// cut ends s with an ellipsis when it is wider than w.
func cut(s string, w int) string { return ui.Trunc(s, w, ui.G().Ellipsis) }

// wrapHard wraps on spaces and splits a token wider than w, so no line
// exceeds w even for a path or hash with no breaks.
func wrapHard(s string, w int) []string {
	w = max(w, 1)
	var out []string
	var line string
	flush := func() {
		if line != "" {
			out = append(out, line)
			line = ""
		}
	}
	for _, word := range strings.Fields(s) {
		for ui.Width(word) > w {
			flush()
			head, n := "", 0
			for _, r := range word {
				rw := ui.Width(string(r))
				if n+rw > w && n > 0 {
					break
				}
				head += string(r)
				n += rw
			}
			out = append(out, head)
			word = word[len(head):]
		}
		switch {
		case word == "":
		case line == "":
			line = word
		case ui.Width(line)+1+ui.Width(word) <= w:
			line += " " + word
		default:
			flush()
			line = word
		}
	}
	flush()
	return out
}

// capLines keeps the first n lines and marks the cut on the last one.
func capLines(lines []string, n, w int) []string {
	if len(lines) <= n {
		return lines
	}
	lines = lines[:n]
	lines[n-1] = ui.Trunc(lines[n-1]+ui.G().Ellipsis, w, ui.G().Ellipsis)
	return lines
}

// midElide keeps the start and the end of a path, which are the parts that
// tell snapshots apart.
func midElide(s string, w int) string {
	if ui.Width(s) <= w {
		return s
	}
	ell := ui.G().Ellipsis
	budget := w - ui.Width(ell)
	if budget < 2 {
		return ui.Trunc(s, w, "")
	}
	head := budget / 3
	var h, t []rune
	n := 0
	for _, r := range s {
		rw := ui.Width(string(r))
		if n+rw > head {
			break
		}
		h = append(h, r)
		n += rw
	}
	rs := []rune(s)
	tailBudget := budget - n
	n = 0
	for i := len(rs) - 1; i >= 0; i-- {
		rw := ui.Width(string(rs[i]))
		if n+rw > tailBudget {
			break
		}
		t = append([]rune{rs[i]}, t...)
		n += rw
	}
	return string(h) + ell + string(t)
}

func blankIf(lines []string) []string {
	if len(lines) > 0 {
		return append(lines, "")
	}
	return lines
}

func quiet(s string, w int) []string {
	var out []string
	for _, ln := range wrapHard(s, w) {
		out = append(out, paint(ln, ui.TextMuted, false))
	}
	return out
}

func body(s string, w int) []string {
	var out []string
	for _, ln := range wrapHard(s, w) {
		out = append(out, paint(ln, ui.TextSecondary, false))
	}
	return out
}

func isGGUF(m *model.Model) bool { return strings.HasPrefix(m.Format, "gguf") }

const ggufNote = "GGUF parsing is coming. model-inspector read file sizes only."

func incompleteLine(w int) []string {
	g := ui.G().Warn
	var out []string
	for i, ln := range wrapHard("incomplete: some values are lower bounds", max(1, w-ui.Width(g)-1)) {
		if i == 0 {
			out = append(out, paint(g, ui.Warn, false)+" "+paint(ln, ui.TextPrimary, false))
		} else {
			out = append(out, strings.Repeat(" ", ui.Width(g)+1)+paint(ln, ui.TextPrimary, false))
		}
	}
	return out
}

// Overview

func provenance(m *model.Model, d derived) string {
	var parts []string
	if m.RepoID != "" {
		org := m.RepoID
		if i := strings.Index(org, "/"); i >= 0 {
			org = org[:i]
		}
		parts = append(parts, sanitize.From(org).String())
	}
	if r := []rune(m.Revision); len(r) > 0 {
		parts = append(parts, "revision "+sanitize.From(string(r[:min(len(r), 12)])).String())
	} else {
		parts = append(parts, "revision "+ui.Dash())
	}
	if d.modelType != "" {
		parts = append(parts, sanitize.From(d.modelType).String())
	}
	parts = append(parts, compactFormat(m.Format))
	return strings.Join(parts, ui.SepWord())
}

func overviewDetail(m *model.Model, d derived, w int) []string {
	var out []string
	for _, ln := range capLines(wrapHard(provenance(m, d), w), 2, w) {
		out = append(out, paint(ln, ui.TextSecondary, false))
	}
	out = append(out, paint(midElide(sanitize.From(m.Dir).String(), w), ui.TextMuted, false), "")
	if isGGUF(m) {
		out = blankIf(append(out, body(ggufNote, w)...))
	}
	if m.Partial {
		out = blankIf(append(out, incompleteLine(w)...))
	}
	out = blankIf(append(out, tileRows(overviewTiles(m, d), w)...))

	out = append(out, ui.SectionLabel(ui.L("Storage"), w))
	out = blankIf(append(out, storageBar(d, w)...))

	out = append(out, ui.SectionLabel(ui.L("Callouts"), w))
	if len(d.callouts) == 0 {
		return append(out, paint("none", ui.TextMuted, false))
	}
	for i, c := range d.callouts {
		if i == overviewCallouts {
			out = append(out, paint(fmt.Sprintf("+%d more", len(d.callouts)-i), ui.TextMuted, false))
			break
		}
		out = append(out, calloutLines(c, w, 2)...)
	}
	return out
}

// overviewTiles drops the memory tile (it has its own section) and the
// experts tile on dense models. GGUF has no tensor headers to count.
func overviewTiles(m *model.Model, d derived) []ui.TileSpec {
	all := tileSpecs(m, d)
	if isGGUF(m) {
		return all[:1]
	}
	out := all[:4]
	if _, _, ok := moeCounts(m); ok {
		out = append(out, all[4])
	}
	return out
}

// tileRows lays tiles out in equal columns that fit w, balancing the rows
// when they do not all fit on one.
func tileRows(tiles []ui.TileSpec, w int) []string {
	if len(tiles) == 0 {
		return nil
	}
	maxCols := max(1, min(len(tiles), (w+tileGap)/(tileMinWidth+tileGap)))
	rows := (len(tiles) + maxCols - 1) / maxCols
	cols := (len(tiles) + rows - 1) / rows
	cw := max(1, (w-tileGap*(cols-1))/cols)
	cell := func(s sanitize.Text, tok ui.Token, bold bool) string {
		e := ui.Trunc(s.String(), cw, ui.G().Ellipsis)
		return paint(e, tok, bold) + strings.Repeat(" ", cw-ui.Width(e))
	}
	var out []string
	for start := 0; start < len(tiles); start += cols {
		chunk := tiles[start:min(len(tiles), start+cols)]
		var label, value, sub []string
		for _, t := range chunk {
			label = append(label, ui.SectionLabel(t.Label, cw))
			value = append(value, cell(t.Value, ui.TextPrimary, true))
			sub = append(sub, cell(t.Sub, ui.TextMuted, false))
		}
		gap := strings.Repeat(" ", tileGap)
		if start > 0 {
			out = append(out, "")
		}
		out = append(out, strings.Join(label, gap), strings.Join(value, gap), strings.Join(sub, gap))
	}
	return out
}

// Storage

type storageSeg struct {
	label string
	bytes int64
	pct   float64
	tok   ui.Token
	glyph string
}

// storageShades go from the largest class to the smallest. Shade and glyph
// both differ, so the bar reads without color.
var storageShades = []ui.Token{ui.Ramp5, ui.Ramp4, ui.Ramp3, ui.Ramp2}

func storageSegments(d derived) []storageSeg {
	bar := ui.G().Bar
	var out []storageSeg
	for i, c := range d.storage {
		if c.Bytes <= 0 {
			continue
		}
		out = append(out, storageSeg{
			label: classLabel(c.Class).String(), bytes: c.Bytes, pct: c.Pct,
			tok: storageShades[i%len(storageShades)], glyph: bar[i%len(bar)],
		})
	}
	return out
}

func barCells(segs []storageSeg, w int) []int {
	var total int64
	for _, s := range segs {
		total += s.bytes
	}
	cells := make([]int, len(segs))
	if total <= 0 || w < 1 {
		return cells
	}
	used, largest := 0, 0
	for i, s := range segs {
		if s.bytes > segs[largest].bytes {
			largest = i
		}
		n := int(float64(s.bytes)/float64(total)*float64(w) + 0.5)
		cells[i] = max(n, 1)
		used += cells[i]
	}
	for used > w {
		best := -1
		for i, n := range cells {
			if n > 1 && (best < 0 || n > cells[best]) {
				best = i
			}
		}
		if best < 0 {
			break
		}
		cells[best]--
		used--
	}
	if used < w {
		cells[largest] += w - used
	}
	return cells
}

// storageBar is the bar and its legend wrapped to w.
func storageBar(d derived, w int) []string {
	segs := storageSegments(d)
	if len(segs) == 0 {
		return []string{paint("no tensor data", ui.TextMuted, false)}
	}
	var bar strings.Builder
	for i, n := range barCells(segs, w) {
		if n > 0 {
			bar.WriteString(paint(strings.Repeat(segs[i].glyph, n), segs[i].tok, false))
		}
	}
	out := []string{bar.String()}
	var line string
	lineW := 0
	for _, s := range segs {
		txt := s.glyph + " " + s.label + " " + pctText(s.pct)
		txt = cut(txt, w)
		item := paint(s.glyph, s.tok, false) + paint(strings.TrimPrefix(txt, s.glyph), ui.TextSecondary, false)
		iw := ui.Width(txt)
		if line != "" && lineW+3+iw > w {
			out = append(out, line)
			line, lineW = "", 0
		}
		if line != "" {
			line += "   "
			lineW += 3
		}
		line += item
		lineW += iw
	}
	return append(out, line)
}

func pctText(p float64) string {
	return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.1f", p), "0"), ".") + "%"
}

func storageDetail(m *model.Model, d derived, w int) []string {
	var out []string
	if isGGUF(m) {
		out = blankIf(body(ggufNote, w))
	}
	out = blankIf(append(out, storageBar(d, w)...))
	segs := storageSegments(d)
	if len(segs) == 0 {
		return append(out, paint(model.HumanBytes(m.TotalSize)+" on disk", ui.TextSecondary, false))
	}
	const bytesW, pctW = 10, 6
	room := w - 2 - 1 - bytesW - 1 - pctW
	head := func(label, bytes, pct string) string {
		return paint(padTo(label, 2+max(room, 0)+1)+padLeft(bytes, bytesW)+" "+padLeft(pct, pctW), ui.TextMuted, true)
	}
	if room < 6 {
		for _, s := range segs {
			out = append(out, paint(s.glyph, s.tok, false)+" "+paint(cut(s.label, w-2), ui.TextSecondary, false))
		}
		return out
	}
	out = append(out, head("CLASS", "BYTES", "SHARE"))
	var sum int64
	for _, s := range segs {
		sum += s.bytes
		label := cut(s.label, room)
		out = append(out, paint(s.glyph, s.tok, false)+" "+
			paint(padTo(label, room), ui.TextPrimary, false)+" "+
			paint(padLeft(model.HumanBytes(s.bytes), bytesW)+" "+padLeft(pctText(s.pct), pctW), ui.TextMuted, false))
	}
	out = append(out, paint(padTo("  total tensors", 2+room+1)+padLeft(model.HumanBytes(sum), bytesW), ui.TextMuted, false))
	if m.TotalSize > 0 {
		out = append(out, paint(padTo("  on disk", 2+room+1)+padLeft(model.HumanBytes(m.TotalSize), bytesW), ui.TextMuted, false))
	}
	return out
}

// Memory

func kvLines(label, value string, w int, tok ui.Token) []string {
	lw := min(16, w/2)
	vw := max(1, w-lw)
	var out []string
	for i, ln := range wrapHard(value, vw) {
		key := strings.Repeat(" ", lw)
		if i == 0 {
			key = padTo(cut(strings.ToUpper(label), lw-1), lw)
		}
		out = append(out, paint(key, ui.TextMuted, true)+paint(ln, tok, false))
	}
	return out
}

func memoryDetail(m *model.Model, d derived, w int) []string {
	mem := d.mem
	if isGGUF(m) {
		return append(body(ggufNote, w), body("There is no memory estimate until tensor headers are read.", w)...)
	}
	geq := ui.Geq()
	var out []string
	if m.Partial {
		out = blankIf(incompleteLine(w))
	}
	out = append(out, kvLines("weights", geq+" "+model.HumanBytes(mem.WeightsBytes)+" ("+model.HumanDecimalGB(mem.WeightsBytes)+")", w, ui.TextPrimary)...)
	if hasKind(m, model.KindVision) || hasKind(m, model.KindMTP) {
		out = append(out, kvLines("text-only", geq+" "+model.HumanBytes(mem.TextOnlyBytes), w, ui.TextPrimary)...)
	}
	if mem.HostCapableBytes >= model.GiB {
		bytes := model.HumanBytes(mem.HostCapableBytes)
		v := ui.G().Host + " " + bytes + " of lookup tables can stay in CPU RAM"
		if mem.HostCapablePartial {
			v = ui.G().Host + " " + geq + " " + bytes + " of lookup tables can stay in CPU RAM (incomplete)"
		}
		out = append(out, kvLines("host-capable", v, w, ui.TextPrimary)...)
		// A partial host figure is a lower bound, so subtracting it could
		// overstate what the accelerator needs.
		if !mem.HostCapablePartial {
			out = append(out, kvLines("accelerator", geq+" "+model.HumanBytes(mem.AcceleratorMinBytes)+" on the GPU (heuristic)", w, ui.TextPrimary)...)
		}
	}
	if mem.UnifiedMemory {
		out = append(out, kvLines("unified memory", "MLX: CPU and GPU share RAM, total is unchanged", w, ui.TextPrimary)...)
	}
	seen := map[string]bool{}
	var notes []string
	for _, n := range []string{mem.Note, hostNote(mem)} {
		if n != "" && !seen[n] {
			seen[n] = true
			notes = append(notes, n)
		}
	}
	if len(notes) > 0 {
		out = append(out, "", ui.SectionLabel(ui.L("Notes"), w))
		for _, n := range notes {
			out = append(out, body(n, w)...)
		}
	}
	return out
}

func hostNote(mem model.MemoryEstimate) string {
	if mem.HostCapableBytes > 0 {
		return mem.HostCapableNote
	}
	return ""
}

// Callouts

func calloutGlyph(kind string) (string, ui.Token) {
	switch kind {
	case "host_capable":
		return ui.G().Host, ui.TextSecondary
	case "size_outlier", "big_tensor":
		return ui.G().Warn, ui.Warn
	default:
		return nrGlyph(), ui.TextMuted
	}
}

func calloutLines(c model.Callout, w, maxLines int) []string {
	glyph, tok := calloutGlyph(c.Kind)
	ind := ui.Width(glyph) + 1
	tw := max(1, w-ind)
	lines := capLines(wrapHard(sanitize.From(c.Message).String(), tw), maxLines, tw)
	out := make([]string, 0, len(lines))
	for i, ln := range lines {
		lead := strings.Repeat(" ", ind)
		if i == 0 {
			lead = paint(glyph, tok, false) + " "
		}
		out = append(out, lead+paint(ln, ui.TextPrimary, false))
	}
	return out
}

func calloutsDetail(d derived, w int) []string {
	if len(d.callouts) == 0 {
		return []string{paint("none", ui.TextMuted, false)}
	}
	var out []string
	for _, c := range d.callouts {
		out = append(out, calloutLines(c, w, calloutWrap)...)
	}
	return out
}

// Files

func fileRole(m *model.Model, f model.File, shards, code map[string]bool) string {
	base := filepath.Base(f.Name)
	lower := strings.ToLower(base)
	switch {
	case f.Broken:
		return "broken"
	case shards[f.Name]:
		return "shard"
	case m.Index != nil && m.Index.File != "" && filepath.Base(m.Index.File) == base,
		strings.HasSuffix(lower, ".safetensors.index.json"):
		return "index"
	case strings.HasSuffix(lower, ".safetensors"):
		return "shard"
	case code[f.Name]:
		return "code"
	case strings.HasSuffix(lower, ".json"), strings.HasSuffix(lower, ".jinja"), strings.HasSuffix(lower, ".txt"),
		strings.HasSuffix(lower, ".model"), strings.HasSuffix(lower, ".yaml"), strings.HasSuffix(lower, ".yml"):
		return "config"
	}
	return "other"
}

func filesDetail(m *model.Model, w int) []string {
	if len(m.Files) == 0 {
		return []string{paint("no files", ui.TextMuted, false)}
	}
	shards := map[string]bool{}
	for _, s := range m.Shards {
		shards[s.Name] = true
	}
	code := map[string]bool{}
	for _, f := range m.CodeExecFiles() {
		code[f.Name] = true
	}
	const sizeW, roleW = 10, 6
	room := w - sizeW - 1 - roleW - 1
	if room < 4 {
		room = max(1, w)
	}
	var out []string
	for i, f := range m.Files {
		if i == maxFileRows {
			out = append(out, paint(fmt.Sprintf("+%d more files", len(m.Files)-i), ui.TextMuted, false))
			break
		}
		name := cut(sanitize.From(f.Name).String(), room)
		row := paint(padTo(name, room), ui.TextPrimary, false)
		if w >= room+sizeW+roleW+2 {
			role := fileRole(m, f, shards, code)
			row += " " + paint(padLeft(model.HumanBytes(f.Size), sizeW), ui.TextMuted, false) +
				" " + paint(role, ui.TextMuted, false)
		}
		out = append(out, row)
	}
	return out
}
