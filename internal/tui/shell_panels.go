package tui

import (
	"strconv"

	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/sanitize"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

// sidePanel is one of the four stacked panels of the panels layout. Content
// PRs replace one adapter each; the shell only talks to this interface.
type sidePanel interface {
	num() int                // 1..4
	title() sanitize.Text    //
	counter() string         // pre-styled
	rows(width int) []string // full list, pre-rendered rows
	summary(width int) []string
	cursor() int
	move(delta int) // clamps
	detail(width, height int) []string
	keys() []ui.KeyHint
	problems() []int // row indices, for ] and [
}

// detailTitled names the detail pane after the selection.
type detailTitled interface {
	selection() (title sanitize.Text, tag ui.Safe)
}

// detailDriver is for panels whose detail is a tree with its own cursor.
// The shell scrolls the rendered lines of every other panel.
type detailDriver interface {
	scrollDetail(delta int)
	foldDetail(key string)
}

// sidePanels builds the adapters over t. They hold the pointer so a move
// lands in the Model the caller returns.
func (t *Model) sidePanels() [4]sidePanel {
	return [4]sidePanel{&modelPanel{t}, &checksPanel{t}, &layersPanel{t}, &configPanel{t}}
}

// paneView is t sized to a detail pane, so the screen renderers lay out for
// the pane rather than the terminal. Their state is shared through pointers.
func (t Model) paneView(w, h int) Model {
	c := t
	c.width, c.height = w, h+3
	c.zoom, c.focusSecondary = false, false
	return c
}

func upDown() string {
	if ui.UTF8() {
		return "↑↓"
	}
	return "up/down"
}

func enterKey() string {
	if ui.UTF8() {
		return "⏎"
	}
	return "enter"
}

func hint(key, desc string) ui.KeyHint {
	return ui.KeyHint{Key: sanitize.Program(key), Desc: sanitize.Program(desc)}
}

func moveHints(extra ...ui.KeyHint) []ui.KeyHint {
	out := []ui.KeyHint{hint(upDown(), "move"), hint(enterKey(), "detail"), hint("?", "keys")}
	out = append(out, extra...)
	return append(out, hint("1-4", "panels"), hint("tab", "next panel"))
}

func plainRow(name, value sanitize.Text, width int) string {
	return ui.ListRow(ui.RowSpec{Name: name, Value: value}, width)
}

func muted(s string) string { return ui.PaintToken(s, ui.TextMuted, nil, false) }

// Layers

type layersPanel struct{ t *Model }

func (p *layersPanel) num() int { return 3 }

func (p *layersPanel) title() sanitize.Text { return ui.L("Layers") }

func (p *layersPanel) counter() string {
	if n := p.t.layerCount(); n > 0 {
		return muted(strconv.Itoa(n) + " decoder")
	}
	return ""
}

// tops are the indices into lay.rows of the top-level groups.
func (p *layersPanel) tops() []int {
	l := p.t.lay
	if l == nil || p.t.m == nil {
		return nil
	}
	l.ensure(p.t.m)
	var out []int
	for i, r := range l.rows {
		if r.depth == 0 && r.kind != rowMore {
			out = append(out, i)
		}
	}
	return out
}

func (p *layersPanel) label(r lrow) sanitize.Text {
	if r.path == "dec" {
		return sanitize.Program(p.t.rowLabel(r))
	}
	return p.t.layerGroupLabel(p.t.groupAt(r.gidx))
}

func (p *layersPanel) row(r lrow, width int, selected bool) string {
	return ui.ListRow(ui.RowSpec{
		Name: p.label(r), Value: sanitize.Program(model.HumanBytes(p.t.rowBytes(r))), Selected: selected,
	}, width)
}

func (p *layersPanel) rows(width int) []string {
	cur := p.cursor()
	var out []string
	for i, at := range p.tops() {
		out = append(out, p.row(p.t.lay.rows[at], width, i == cur))
	}
	return out
}

func (p *layersPanel) summary(width int) []string {
	var out []string
	for _, at := range p.tops() {
		if len(out) == 3 {
			break
		}
		out = append(out, p.row(p.t.lay.rows[at], width, false))
	}
	return out
}

// cursor is the top-level group that holds the tree cursor.
func (p *layersPanel) cursor() int {
	tops := p.tops()
	if len(tops) == 0 {
		return 0
	}
	row := p.t.lay.row
	cur := 0
	for i, at := range tops {
		if at > row {
			break
		}
		cur = i
	}
	return cur
}

func (p *layersPanel) move(d int) {
	tops := p.tops()
	if len(tops) == 0 {
		return
	}
	p.t.lay.row = tops[clampInt(p.cursor()+d, 0, len(tops)-1)]
	p.t.clampLayers()
	p.t.touchLayers()
}

func (p *layersPanel) detail(w, h int) []string {
	if p.t.lay == nil {
		return nil
	}
	v := p.t.paneView(w, h)
	v.lay.hits = v.lay.hits[:0]
	return v.layersLines(w, h)
}

func (p *layersPanel) selection() (sanitize.Text, ui.Safe) {
	tops := p.tops()
	if len(tops) == 0 {
		return ui.L("Layers"), ui.Safe{}
	}
	r := p.t.lay.rows[tops[p.cursor()]]
	return p.label(r), ui.Chrome(muted(model.HumanBytes(p.t.rowBytes(r))))
}

func (p *layersPanel) keys() []ui.KeyHint { return moveHints(hint("y", "copy")) }

func (p *layersPanel) problems() []int { return nil }

func (p *layersPanel) scrollDetail(d int) { p.t.layersMove(d) }

func (p *layersPanel) foldDetail(key string) {
	if p.t.lay == nil {
		return
	}
	p.t.lay.ensure(p.t.m)
	switch key {
	case "right", "l":
		p.t.layersExpand()
	case "left", "h":
		p.t.layersCollapse()
	default:
		p.t.layersToggle()
	}
}

// Config

type configPanel struct{ t *Model }

func (p *configPanel) num() int { return 4 }

func (p *configPanel) title() sanitize.Text { return ui.L("Config") }

func (p *configPanel) counter() string {
	if n := nodeCount(p.t.cfg); n > 0 {
		return muted(commaInt(n) + " nodes")
	}
	return ""
}

// fields are the indices into fieldLines of the key-field rows.
func (p *configPanel) fields() ([]fieldLine, []int) {
	p.t.prepareConfig()
	lines := p.t.fieldLines()
	var at []int
	for i, ln := range lines {
		if ln.kind == fieldRow {
			at = append(at, i)
		}
	}
	return lines, at
}

func (p *configPanel) row(f fieldLine, width int, selected bool) string {
	value := sanitize.From(f.value).String()
	if f.unit != "" {
		value += " " + f.unit
	}
	return ui.ListRow(ui.RowSpec{
		Name: sanitize.From(f.label), Value: sanitize.Program(value), Selected: selected,
	}, width)
}

func (p *configPanel) rows(width int) []string {
	lines, at := p.fields()
	cur := p.cursor()
	var out []string
	for i, idx := range at {
		out = append(out, p.row(lines[idx], width, i == cur))
	}
	return out
}

func (p *configPanel) summary(width int) []string {
	lines, at := p.fields()
	var out []string
	for _, idx := range at {
		if len(out) == 3 {
			break
		}
		out = append(out, p.row(lines[idx], width, false))
	}
	return out
}

func (p *configPanel) cursor() int {
	_, at := p.fields()
	cur := 0
	for i, idx := range at {
		if idx > p.t.cfg.fieldCur {
			break
		}
		cur = i
	}
	return cur
}

// move steps the field cursor and points the tree at that field's node.
func (p *configPanel) move(d int) {
	_, at := p.fields()
	if len(at) == 0 {
		return
	}
	p.t.cfg.fieldCur = at[clampInt(p.cursor()+d, 0, len(at)-1)]
	p.t.clampFields()
	p.t.jumpField(p.t.cfg.fieldCur)
	p.t.touchConfig()
}

func (p *configPanel) detail(w, h int) []string {
	if p.t.cfg == nil {
		return nil
	}
	p.t.prepareConfig()
	v := p.t.paneView(w, h)
	v.cfg.hits = v.cfg.hits[:0]
	var lines []string
	if v.configProblem() != "" {
		lines = v.configLines(w, h)
	} else {
		lines = v.treePaneLines(0, 0, w, h, p.t.sh.inDetail)
	}
	return lines
}

func (p *configPanel) selection() (sanitize.Text, ui.Safe) {
	lines, at := p.fields()
	if len(at) == 0 {
		return ui.L("Config"), ui.Safe{}
	}
	return sanitize.From(lines[at[p.cursor()]].label), ui.Safe{}
}

func (p *configPanel) keys() []ui.KeyHint { return moveHints(hint("y", "copy")) }

func (p *configPanel) problems() []int { return nil }

func (p *configPanel) scrollDetail(d int) {
	if p.t.cfg != nil && p.t.cfg.tree != nil {
		p.t.configMove(d, true)
	}
}

func (p *configPanel) foldDetail(key string) {
	if p.t.cfg == nil || p.t.cfg.tree == nil {
		return
	}
	switch key {
	case "right", "l":
		p.t.configExpand()
	case "left", "h":
		p.t.configCollapse()
	default:
		p.t.configToggle()
	}
}
