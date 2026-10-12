package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/rivo/uniseg"

	"github.com/cecilkootz/model-inspector/internal/jsonx"
	"github.com/cecilkootz/model-inspector/internal/sanitize"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

// configPanel is panel 4. The list is the key fields, grouped under muted
// non-selectable labels; the detail is the ordered JSON tree at the field's
// node, or raw sanitized text behind `r`.
type configPanel struct{ t *Model }

func (p *configPanel) num() int { return 4 }

func (p *configPanel) title() sanitize.Text { return ui.L("Config") }

func (p *configPanel) counter() string {
	if n := nodeCount(p.t.cfg); n > 0 {
		return muted(commaInt(n) + " nodes")
	}
	return ""
}

// listLines are the field lines without the blank spacers. at maps each one
// back to its index in fieldLines, which fieldCur and jumpField use.
func (p *configPanel) listLines() (lines []fieldLine, at []int) {
	p.t.prepareConfig()
	for i, ln := range p.t.fieldLines() {
		if ln.kind == fieldBlank {
			continue
		}
		lines = append(lines, ln)
		at = append(at, i)
	}
	return lines, at
}

func (p *configPanel) row(f fieldLine, width int, selected bool) string {
	switch f.kind {
	case fieldHeader:
		gutter := ""
		if ui.ColorOff() || ui.Profile() == "ansi16" {
			gutter = "  "
		}
		return gutter + ui.SectionLabel(sanitize.Program(f.label), max(1, width-len(gutter)))
	case fieldNote:
		return ui.ListRow(ui.RowSpec{Glyph: sanitize.Program(nrGlyph()), Name: sanitize.Program(f.label)}, width)
	}
	value := sanitize.From(f.value).String()
	if f.unit != "" {
		value += " " + f.unit
	}
	return ui.ListRow(ui.RowSpec{
		Name: sanitize.From(f.label), Value: sanitize.Program(value), Note: derivedNote(f), Selected: selected,
	}, width)
}

// derivedMax bounds derived text before it is wrapped or elided; it can carry
// a config string such as a rope type.
const derivedMax = 256

// derivedNote is the field's derivation as list-row text. A flagged field has
// no trustworthy derivation to show.
func derivedNote(f fieldLine) sanitize.Text {
	if f.bad || f.derived == "" {
		return sanitize.Text{}
	}
	return sanitize.From(clipRunes(f.derived, derivedMax))
}

func clipRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

func (p *configPanel) rows(width int) []string {
	lines, _ := p.listLines()
	cur := p.cursor()
	out := make([]string, 0, len(lines))
	for i, ln := range lines {
		out = append(out, p.row(ln, width, i == cur))
	}
	return out
}

// summary prefers model_type, layers and context, then the first fields in
// list order.
func (p *configPanel) summary(width int) []string {
	lines, _ := p.listLines()
	var pick []int
	taken := map[int]bool{}
	for _, want := range []string{"model_type", "layers", "context"} {
		for i, ln := range lines {
			if ln.kind == fieldRow && ln.label == want && !taken[i] {
				pick, taken[i] = append(pick, i), true
				break
			}
		}
	}
	for i, ln := range lines {
		if len(pick) == 3 {
			break
		}
		if ln.kind == fieldRow && !taken[i] {
			pick, taken[i] = append(pick, i), true
		}
	}
	if len(pick) == 0 {
		for i, ln := range lines {
			if ln.kind == fieldNote && len(pick) < 3 {
				pick = append(pick, i)
			}
		}
	}
	slices.Sort(pick)
	out := make([]string, 0, len(pick))
	for _, i := range pick {
		out = append(out, p.row(lines[i], width, false))
	}
	return out
}

// cursor is the list index of the selected field, counting group labels.
func (p *configPanel) cursor() int {
	_, at := p.listLines()
	cur := 0
	for i, idx := range at {
		if idx > p.t.cfg.fieldCur {
			break
		}
		cur = i
	}
	return cur
}

// move steps by list rows and lands on a field: a label or note is skipped in
// the direction of travel, or backwards at the end of the list. The tree
// follows the field.
func (p *configPanel) move(d int) {
	lines, at := p.listLines()
	if len(lines) == 0 {
		return
	}
	to := clampInt(p.cursor()+d, 0, len(lines)-1)
	step := 1
	if d < 0 {
		step = -1
	}
	for _, s := range []int{step, -step} {
		i := to
		for i >= 0 && i < len(lines) && lines[i].kind != fieldRow {
			i += s
		}
		if i >= 0 && i < len(lines) {
			to = i
			break
		}
	}
	if lines[to].kind != fieldRow {
		return
	}
	c := p.t.cfg
	c.fieldCur = at[to]
	p.t.clampFields()
	p.t.jumpField(c.fieldCur)
	if c.raw && len(p.t.m.ConfigRaw) > 0 {
		p.t.ensureRaw()
		p.t.syncRawLine()
	}
	p.t.touchConfig()
}

// fieldPath is the dotted path of the selected field's node, or its label
// when the field has no node (derived rows, or no tree).
func (p *configPanel) fieldPath() sanitize.Text {
	lines, _ := p.listLines()
	if len(lines) == 0 {
		return ui.L("Config")
	}
	f := lines[p.cursor()]
	if f.kind != fieldRow {
		return ui.L("Config")
	}
	c := p.t.cfg
	if f.jump >= 0 && c.tree != nil && int(f.jump) < len(c.tree.Nodes) {
		return sanitize.From(strings.TrimPrefix(displayPath(c, f.jump), "."))
	}
	return sanitize.From(f.label)
}

func (p *configPanel) selection() (sanitize.Text, ui.Safe) {
	if p.t.cfg == nil {
		return ui.L("Config"), ui.Safe{}
	}
	tag := ""
	switch {
	case p.t.cfg.raw && p.rawOK():
		tag = "raw"
	case p.t.cfg.tree != nil && p.t.cfg.err == nil:
		tag = "tree"
	}
	if tag != "" {
		return p.fieldPath(), ui.Chrome(muted(tag))
	}
	return p.fieldPath(), ui.Safe{}
}

func (p *configPanel) keys() []ui.KeyHint {
	return moveHints(hint("r", "raw"), hint("y", "copy"))
}

func (p *configPanel) problems() []int { return nil }

// rawOK is true when there is text to show raw.
func (p *configPanel) rawOK() bool {
	return p.t.m != nil && len(p.t.m.ConfigRaw) > 0
}

func (p *configPanel) toggleDetail() {
	c := p.t.cfg
	if c == nil || !p.rawOK() {
		return
	}
	p.t.prepareConfig()
	c.raw = !c.raw
	c.rawReady = false
	if c.raw {
		p.t.ensureRaw()
		p.t.syncRawLine()
	}
	p.t.touchConfig()
}

func (p *configPanel) scrollDetail(d int) {
	c := p.t.cfg
	if c == nil {
		return
	}
	if c.raw && p.rawOK() {
		p.t.ensureRaw()
		c.rawLine += d
		p.t.clampRaw()
		p.t.touchConfig()
		return
	}
	if c.tree != nil {
		p.t.configMove(d, true)
	}
}

func (p *configPanel) foldDetail(key string) {
	c := p.t.cfg
	if c == nil || c.tree == nil || c.raw {
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

func (p *configPanel) detail(w, h int) []string {
	c := p.t.cfg
	if c == nil || w < 1 || h < 1 {
		return nil
	}
	p.t.prepareConfig()
	v := p.t.paneView(w, h)
	c.hits = c.hits[:0]
	defer func() { c.hits = c.hits[:0] }()

	var out []string
	switch v.configProblem() {
	case "none", "unreadable", "oversize":
		out = v.configCard(w)
	case "badjson", "nodes":
		if c.raw {
			out = v.rawLinesView(w, h)
		} else {
			out = v.configCard(w)
		}
	default:
		if c.raw {
			out = v.rawLinesView(w, h)
		} else {
			out = v.treeDetail(w, h)
		}
	}
	return fitLines(out, w, h)
}

func fitLines(lines []string, w, h int) []string {
	if len(lines) > h {
		lines = lines[:h]
	}
	for i, ln := range lines {
		if ui.Width(ln) > w {
			lines[i] = ui.Trunc(ln, w, ui.G().Ellipsis)
		}
	}
	return lines
}

// cardHead is `pill LABEL`. The label text is the one the legacy card used.
func cardHead(kind ui.PillKind, label string, w int) string {
	pill := ui.StatusPill(kind)
	return pill + " " + ui.SectionLabel(sanitize.Program(label), max(1, w-ui.Width(pill)-1))
}

func paras(w int, texts ...string) []string {
	var out []string
	for i, tx := range texts {
		if i > 0 {
			out = append(out, "")
		}
		for _, ln := range wrapWords(tx, w) {
			out = append(out, hardWrap(ln, w)...)
		}
	}
	return out
}

// hardWrap splits a line wider than w at cell boundaries. Only a single
// token wider than the pane gets here.
func hardWrap(s string, w int) []string {
	if w < 1 || uniseg.StringWidth(s) <= w {
		return []string{s}
	}
	var out []string
	var b strings.Builder
	got := 0
	state := -1
	for rest := s; len(rest) > 0; {
		cluster, next, _, st := uniseg.FirstGraphemeClusterInString(rest, state)
		cw := uniseg.StringWidth(cluster)
		if got+cw > w && got > 0 {
			out = append(out, b.String())
			b.Reset()
			got = 0
		}
		b.WriteString(cluster)
		got += cw
		rest, state = next, st
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

// configCard is the detail for a config that is missing, unreadable, too
// large, unparseable or over the node cap.
func (t Model) configCard(w int) []string {
	var out []string
	switch t.configProblem() {
	case "none":
		out = append(out, cardHead(ui.PillNR, "NO CONFIG", w), "")
		out = append(out, paras(w,
			"No config.json in this snapshot.",
			"Architecture fields come from config.json, so nothing to show here. Layer counts, dtypes and sizes on the Model and Layers panels come from tensor headers and are still available.")...)
	case "unreadable":
		// The loader warning names the absolute path; a stable sentence keeps
		// a temp directory out of the frame.
		out = append(out, cardHead(ui.PillFail, "UNREADABLE", w), "")
		out = append(out, paras(w,
			"config.json is unreadable.",
			"The file is there, but it could not be read.")...)
	case "oversize":
		n, lim := oversizeAmounts(t.m)
		msg := "config.json is " + mib1(n) + "; the limit is " + mibLimit(lim) + ", so it was not read"
		out = append(out, cardHead(ui.PillFail, "TOO LARGE", w), "")
		out = append(out, paras(w, sanitize.From(msg).String())...)
	case "badjson":
		out = append(out, cardHead(ui.PillFail, "PARSE ERROR", w), "")
		out = append(out, paras(w, t.parseMessage())...)
		out = append(out, "")
		out = append(out, t.excerptLines(w)...)
		out = append(out, "", ui.PaintToken("Key fields and the tree need a parsed config.", ui.TextMuted, nil, false))
	case "nodes":
		out = append(out, cardHead(ui.PillWarn, "NODE LIMIT", w), "")
		out = append(out, paras(w, nodeLimitText(t.cfg.err))...)
	}
	return out
}

// rawLinesView is the sanitized raw text with the line of the selection
// marked.
func (t Model) rawLinesView(w, h int) []string {
	c := t.cfg
	label := "RAW"
	if c.rawCut {
		label = "RAW (first " + mib(configRawMax) + ")"
	}
	rows := max(1, h-1)
	c.viewH = rows
	c.innerW = w
	out := []string{ui.SectionLabel(sanitize.Program(label), w)}
	return append(out, t.rawBody(w, rows)...)
}

// valueBlock is the VALUE section for the node under the tree cursor, at
// most n rows including a leading blank.
func (t Model) valueBlock(w, n int) []string {
	c := t.cfg
	id := c.cursor
	if c.tree == nil || id < 0 || int(id) >= len(c.tree.Nodes) {
		return nil
	}
	path := "config.json"
	if p := strings.TrimPrefix(displayPath(c, id), "."); p != "" {
		path = p
	}
	path = sanitize.From(path).String()
	nd := c.tree.Nodes[id]
	val := c.shownScalar(id)
	if nd.Kind == jsonx.KindObject || nd.Kind == jsonx.KindArray {
		val = c.preview(id, w*3)
	}
	// The value comes from litText and keyText, which sanitized it already.
	vals := paras(w, val)
	out := []string{"", ui.SectionLabel(ui.L("VALUE"), w), ui.PaintToken(path, ui.TextSecondary, nil, false)}
	room := n - len(out)
	if room < 1 {
		return out[:n]
	}
	if len(vals) > room {
		vals = vals[:room]
		vals[room-1] = clipCells(vals[room-1]+" "+ui.G().Ellipsis, w)
	}
	for _, ln := range vals {
		out = append(out, ui.PaintToken(ln, ui.TextPrimary, nil, false))
	}
	return out
}

func (t Model) treeDetail(w, h int) []string {
	c := t.cfg
	if c.tree == nil {
		msg := "reading config.json…"
		if c.parsing && c.spin {
			msg = "parsing config.json (" + mib(len(t.m.ConfigRaw)) + ")…"
		}
		return []string{ui.PaintToken(msg, ui.TextMuted, nil, false)}
	}
	var head []string
	if b := t.rootBanner(); b != "" {
		head = append(head, clipCells(b, w))
	}
	valueN := 0
	switch {
	case h >= 14:
		valueN = 6
	case h >= 9:
		valueN = 4
	}
	// The tree keeps at least treeMin rows; the derivation takes what is left.
	const treeMin = 6
	var derived []string
	if room := h - len(head) - valueN - treeMin; room >= 4 {
		derived = t.derivedBlock(w, min(room, 9))
	}
	rowN := max(1, h-len(head)-valueN-len(derived))
	c.viewH = rowN
	c.innerW = max(8, w)
	out := append(head, t.treeRows(w, rowN, t.sh != nil && t.sh.inDetail, 0, 0, w)...)
	for len(out) < len(head)+rowN {
		out = append(out, "")
	}
	if valueN > 0 {
		out = append(out, t.valueBlock(w, valueN)...)
	}
	return append(out, derived...)
}

// derivedBlock is the DERIVED section for the selected key field, at most n
// rows including a leading blank. It is empty when the field has no
// derivation or the tree cursor has left the field's node.
func (t Model) derivedBlock(w, n int) []string {
	c := t.cfg
	lines := t.fieldLines()
	if c.fieldCur < 0 || c.fieldCur >= len(lines) || n < 4 {
		return nil
	}
	f := lines[c.fieldCur]
	if f.kind != fieldRow || f.bad || f.derived == "" || (f.jump >= 0 && f.jump != c.cursor) {
		return nil
	}
	text := sanitize.From(f.value).String()
	if f.unit != "" {
		text += " " + f.unit
	}
	text += " " + sanitize.From(clipRunes(f.derived, derivedMax)).String()
	tone := ui.TextPrimary
	if strings.HasPrefix(f.derived, "!") {
		tone = ui.Warn
	}
	out := []string{"", ui.SectionLabel(ui.L("DERIVED"), w), ui.PaintToken(clipCells(sanitize.From(f.label).String(), w), ui.TextSecondary, nil, false)}
	room := n - len(out)
	meter := f.meterOn && room > 1
	if meter {
		room--
	}
	body := paras(w, text)
	if len(body) > room {
		body = body[:room]
		body[room-1] = clipCells(body[room-1]+" "+ui.G().Ellipsis, w)
	}
	for _, ln := range body {
		out = append(out, ui.PaintToken(ln, tone, nil, false))
	}
	if meter {
		out = append(out, meterLine(f.meter, f.meterBy, w))
	}
	return out
}

// meterLine is a bar on the green ramp and the percentage it shows.
func meterLine(frac float64, by string, w int) string {
	label := fmt.Sprintf("%.1f%% %s", frac*100, by)
	bar := min(16, max(1, w-ui.Width(label)-1))
	m, _ := ui.Meter(frac, bar, ui.GradMeter)
	return ui.Trunc(m+" "+ui.PaintToken(label, ui.TextMuted, nil, false), w, "")
}
