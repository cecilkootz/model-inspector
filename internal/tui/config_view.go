package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/rivo/uniseg"

	"github.com/cecilkootz/model-inspector/internal/jsonx"
	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/sanitize"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

func (t Model) configBody() string {
	if t.cfg == nil || t.m == nil {
		return ""
	}
	t.prepareConfig()
	if t.cfg.flash > 0 && !t.motion {
		t.cfg.flash = 0
	}
	t.cfg.hits = t.cfg.hits[:0]
	w, h := t.width, t.bodyHeight()
	body := t.configLines(w, h)
	if t.cfg.modal {
		return t.configModal(w, h)
	}
	return joinFit(body, w, h)
}

func (t Model) configLines(w, h int) []string {
	switch t.configProblem() {
	case "none":
		return t.filledCard(w, h, "NO CONFIG", ui.FillKindNR, []string{
			"No config.json in this snapshot.",
			"",
			"Architecture fields come from config.json, so nothing to show here.",
			"Layer counts, dtypes and sizes on Identity/Layers come from tensor",
			"headers and are still available.",
		})
	case "unreadable":
		// The loader warning names the absolute path. The card stays on a
		// stable sentence so a temp directory cannot leak into the frame.
		return t.filledCard(w, h, "UNREADABLE", ui.FillKindFail, []string{
			"config.json is unreadable.",
			"The file is there, but it could not be read.",
		})
	case "oversize":
		n, lim := oversizeAmounts(t.m)
		msg := fmt.Sprintf("config.json is %s; the limit is %s, so it was not read", mib1(n), mibLimit(lim))
		return t.filledCard(w, h, "TOO LARGE", ui.FillKindFail, []string{msg})
	}
	br := t.configBreak()
	if br == "S" || br == "XS" {
		switch t.configProblem() {
		case "badjson":
			return t.parseCard(w, h)
		case "nodes":
			return t.nodeCard(w, h)
		}
		if t.cfg.drill || t.cfg.raw {
			return t.treePaneLines(0, 2, w, h, true)
		}
		return t.fieldsPane(0, 2, w, h, true)
	}
	ratio := t.configRatio()
	rects := ui.Layout(w, t.height, ratio, t.zoom, t.focusSecondary)
	if !rects.Split {
		return t.fieldsPane(0, 2, w, h, !t.focusSecondary)
	}
	left := t.fieldsPane(rects.Primary.X, rects.Primary.Y, rects.Primary.W, rects.Primary.H, !t.focusSecondary)
	var right []string
	switch t.configProblem() {
	case "badjson":
		right = t.parseCard(rects.Secondary.W, rects.Secondary.H)
	case "nodes":
		right = t.nodeCard(rects.Secondary.W, rects.Secondary.H)
	default:
		right = t.treePaneLines(rects.Secondary.X, rects.Secondary.Y, rects.Secondary.W, rects.Secondary.H, t.focusSecondary)
	}
	return zipCols(left, right, rects.Primary.W, rects.Secondary.W, w)
}

func (t Model) configRatio() int {
	if t.cfg != nil && t.cfg.ratioSet {
		return t.cfg.ratio
	}
	if ui.Breakpoint(t.width, t.height) == "L" {
		return 42
	}
	return 55
}

func (t Model) configProblem() string {
	if t.m == nil || len(t.m.ConfigRaw) == 0 {
		if t.m != nil && hasOversize(t.m) {
			return "oversize"
		}
		if t.m != nil && configUnreadable(t.m) {
			return "unreadable"
		}
		return "none"
	}
	if t.cfg != nil && t.cfg.err != nil {
		if _, ok := t.cfg.err.(*jsonx.NodeLimitError); ok {
			return "nodes"
		}
		return "badjson"
	}
	return ""
}

func hasOversize(m *model.Model) bool {
	for _, w := range m.Warnings {
		if strings.Contains(w, "refusing to read more than") {
			return true
		}
	}
	return false
}

func oversizeAmounts(m *model.Model) (int64, int64) {
	for _, w := range m.Warnings {
		var n, lim int64
		if _, err := fmt.Sscanf(w, "config.json is %d bytes; refusing to read more than %d", &n, &lim); err == nil && lim > 0 {
			return n, lim
		}
	}
	return 0, 0
}

func mib1(n int64) string {
	if n <= 0 {
		return "0.0 MiB"
	}
	return fmt.Sprintf("%.1f MiB", float64(n)/(1024*1024))
}

// mibLimit prints a whole number of MiB without a trailing fraction so the
// oversized card can say "64 MiB". Sizes that are not an integer stay at
// one decimal.
func mibLimit(n int64) string {
	if n > 0 && n%(1024*1024) == 0 {
		return fmt.Sprintf("%d MiB", n/(1024*1024))
	}
	return mib1(n)
}

func mib(n int) string { return mib1(int64(n)) }

func joinFit(lines []string, w, h int) string {
	if h < 1 {
		h = 1
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	for i := range lines {
		lines[i] = ui.Fit(lines[i], w)
	}
	return strings.Join(lines, "\n")
}

func zipCols(left, right []string, lw, rw, total int) []string {
	n := len(left)
	if len(right) > n {
		n = len(right)
	}
	gutter := total - lw - rw
	if gutter < 1 {
		gutter = 1
	}
	gap := strings.Repeat(" ", gutter)
	out := make([]string, n)
	for i := 0; i < n; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out[i] = ui.Fit(l, lw) + gap + ui.Fit(r, rw)
	}
	return out
}

func (t Model) filledCard(w, h int, pill string, kind ui.FillKind, text []string) []string {
	inner := innerWidth(w)
	body := []string{"", ui.Pill(kind, sanitize.From(pill))}
	for _, ln := range text {
		body = append(body, clipCells(sanitize.From(ln).String(), inner))
	}
	return chrome(ui.L("KEY FIELDS"), sanitize.Text{}, sanitize.Text{}, body, w, h, true)
}

func (t Model) warnLines(limit int) []string {
	var out []string
	for _, w := range t.m.Warnings {
		if !strings.Contains(w, "config.json") {
			continue
		}
		out = append(out, sanitize.From(w).String())
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (t Model) fieldsPane(x, y, w, h int, focused bool) []string {
	browse := t.configBreak() == "S" || t.configBreak() == "XS"
	paneH := h
	if browse {
		paneH = h - 1
		if paneH < 1 {
			paneH = 1
		}
	}
	inner := innerWidth(w)
	t.cfg.fieldH = paneH - 2
	if t.cfg.fieldH < 1 {
		t.cfg.fieldH = 1
	}
	t.clampFields()
	lines := t.fieldLines()
	var body []string
	if t.configProblem() == "nodes" {
		body = append(body, clipCells("Tree disabled. Fields below come from the parsed config.", inner))
	}
	shown := 0
	for i := t.cfg.fieldTop; i < len(lines) && shown < t.cfg.fieldH; i++ {
		body = append(body, t.paintField(lines[i], inner, i == t.cfg.fieldCur, focused))
		if lines[i].kind == fieldRow {
			t.cfg.hits = append(t.cfg.hits, hit{
				id: fmt.Sprintf("cfg:field:%d", i),
				x:  x, y: y + 1 + len(body) - 1, w: w, h: 1,
			})
		}
		shown++
	}
	sub := t.fieldSubtitle()
	out := chrome(ui.L("KEY FIELDS"), sanitize.Text{}, sub, body, w, paneH, focused)
	if browse {
		out = append(out, t.browseRow(w))
	}
	for len(out) < h {
		out = append(out, "")
	}
	if len(out) > h {
		out = out[:h]
	}
	return out
}

func (t Model) fieldSubtitle() sanitize.Text {
	n := 0
	for _, ln := range t.cfg.lines {
		if ln.kind == fieldHeader {
			n++
		}
	}
	verb := "jumps to the tree"
	if t.configBreak() == "S" || t.configBreak() == "XS" {
		verb = "opens the tree"
	}
	return sanitize.From(fmt.Sprintf("%d groups%s%s %s", n, cfgSep(), cfgEnter(), verb))
}

func (t Model) paintField(f fieldLine, width int, selected, focused bool) string {
	switch f.kind {
	case fieldHeader:
		s := strings.ToUpper(f.label)
		if ui.ColorOff() {
			return s
		}
		return ui.PaintToken(s, ui.Accent, nil, true)
	case fieldBlank:
		return ""
	case fieldNote:
		mark := ui.G().NR + " "
		return clipCells(mark+sanitize.From(f.label).String(), width)
	}
	plain := t.fieldPlain(f, width)
	gutter := " "
	if selected && (ui.ColorOff() || ui.Profile() == "ansi16") {
		gutter = ">"
	}
	if ui.ColorOff() {
		return clipCells(gutter+plain, width)
	}
	bg := (*ui.Token)(nil)
	if selected {
		if focused {
			bg = &ui.SurfaceSel
		} else {
			bg = &ui.SurfaceSeg
		}
	}
	if ui.Profile() == "ansi16" && selected && focused {
		return lipgloss.NewStyle().Reverse(true).Render(ui.Fit(plain, width))
	}
	fg := ui.TextPrimary
	return ui.PaintToken(ui.Fit(plain, width), fg, bg, false)
}

func (t Model) fieldPlain(f fieldLine, width int) string {
	label := sanitize.From(f.label).String()
	value := sanitize.From(f.value).String()
	gap := 16 - uniseg.StringWidth(label)
	if gap < 2 {
		gap = 2
	}
	var b strings.Builder
	b.WriteString(label)
	b.WriteString(strings.Repeat(" ", gap))
	b.WriteString(value)
	if f.unit != "" {
		b.WriteByte(' ')
		b.WriteString(f.unit)
	}
	if f.derived != "" {
		b.WriteString("  ")
		b.WriteString(sanitize.From(f.derived).String())
	}
	if f.meterOn {
		m, _ := ui.Meter(f.meter, 10, ui.GradMeter)
		b.WriteByte(' ')
		b.WriteString(ui.Strip(m))
		fmt.Fprintf(&b, "  %.1f%% active", f.meter*100)
	}
	return clipCells(b.String(), width)
}

func (t Model) browseRow(w int) string {
	gl := ui.G()
	arrow := gl.Arrow + " "
	if ui.UTF8() {
		arrow = "▸ "
	}
	left := fmt.Sprintf("%sBrowse config.json (%s nodes)", arrow, commaInt(nodeCount(t.cfg)))
	right := cfgEnter() + " on any row"
	gap := w - 2 - uniseg.StringWidth(left) - uniseg.StringWidth(right)
	if gap < 1 {
		return clipCells(" "+left, w)
	}
	line := " " + left + strings.Repeat(" ", gap) + right
	if ui.ColorOff() {
		return clipCells(line, w)
	}
	return ui.PaintToken(ui.Fit(clipCells(line, w), w), ui.TextPrimary, &ui.SurfaceSeg, false)
}

func (t Model) treePaneLines(x, y, w, h int, focused bool) []string {
	stripN := 0
	if !t.cfg.raw {
		if ui.Breakpoint(t.width, t.height) == "L" {
			stripN = 4
		} else if t.cfg.drill || ui.Breakpoint(t.width, t.height) == "M" {
			stripN = 1
		}
	}
	searchN := 0
	if t.cfg.search {
		searchN = 1
	}
	bodyH := h - 1 - stripN - searchN
	if bodyH < 1 {
		bodyH = 1
	}
	t.cfg.viewH = bodyH
	t.cfg.innerW = innerWidth(w)
	if t.cfg.innerW < 8 {
		t.cfg.innerW = 8
	}
	inner := innerWidth(w)
	var body []string
	switch {
	case t.cfg.parsing && len(t.m.ConfigRaw) > configSyncMax:
		msg := "reading config.json…"
		if t.cfg.spin {
			msg = fmt.Sprintf("parsing config.json (%s)…", mib(len(t.m.ConfigRaw)))
		}
		body = []string{msg}
	case t.cfg.raw:
		body = t.rawBody(inner, bodyH)
	default:
		body = append(body, t.crumbLine(inner, x, y+1, w))
		if b := t.rootBanner(); b != "" {
			body = append(body, clipCells(b, inner))
		}
		rowN := bodyH - len(body)
		if rowN < 1 {
			rowN = 1
		}
		t.cfg.viewH = rowN
		body = append(body, t.treeRows(inner, rowN, focused, x, y+1+len(body), w)...)
	}
	for len(body) < bodyH {
		body = append(body, "")
	}
	if len(body) > bodyH {
		body = body[:bodyH]
	}
	out := chrome(ui.L("CONFIG.JSON"), ui.L("r raw"), t.treeSubtitle(), body, w, h-stripN-searchN, focused)
	if stripN > 0 {
		out = append(out, t.valueStrip(w, stripN)...)
	}
	if searchN > 0 {
		out = append(out, t.searchRow(w))
	}
	for len(out) < h {
		out = append(out, "")
	}
	if len(out) > h {
		out = out[:h]
	}
	return out
}

func (t Model) crumbIDs() []int32 {
	c := t.cfg
	if c == nil || c.tree == nil || len(c.tree.Nodes) == 0 {
		return nil
	}
	id := c.cursor
	if id < 0 || int(id) >= len(c.tree.Nodes) {
		id = 0
	}
	var ids []int32
	for id >= 0 {
		ids = append(ids, id)
		p := c.tree.Nodes[id].Parent
		if p < 0 {
			break
		}
		id = p
	}
	for i, j := 0, len(ids)-1; i < j; i, j = i+1, j-1 {
		ids[i], ids[j] = ids[j], ids[i]
	}
	return ids
}

func (t Model) crumbLine(width, x, y, paneW int) string {
	ids := t.crumbIDs()
	if len(ids) == 0 {
		return "config.json"
	}
	gl := ui.G()
	var b strings.Builder
	b.WriteString("config.json")
	segW := uniseg.StringWidth("config.json")
	t.cfg.hits = append(t.cfg.hits, hit{id: "cfg:crumb:0", x: x, y: y, w: segW + 2, h: 1})
	for i := 1; i < len(ids); i++ {
		b.WriteByte(' ')
		b.WriteString(gl.Arrow)
		b.WriteByte(' ')
		n := t.cfg.tree.Nodes[ids[i]]
		name := t.cfg.displayKey(ids[i])
		if n.Key == "" && n.Index >= 0 {
			name = fmt.Sprintf("[%d]", n.Index)
		}
		name = sanitize.From(name).String()
		start := uniseg.StringWidth(b.String())
		b.WriteString(name)
		t.cfg.hits = append(t.cfg.hits, hit{
			id: fmt.Sprintf("cfg:crumb:%d", i),
			x:  x + start, y: y, w: uniseg.StringWidth(name) + 1, h: 1,
		})
	}
	_ = paneW
	return clipCells(b.String(), width)
}

func (t Model) crumbClick(depth int) {
	ids := t.crumbIDs()
	if depth < 0 || depth >= len(ids) || t.cfg == nil {
		return
	}
	t.cfg.cursor = ids[depth]
	t.cfg.ensureRows()
	t.cfg.row = t.cfg.indexOfCursor()
	t.clampTree()
	t.focusSecondary = true
	t.touchConfig()
}

func (t Model) treeSubtitle() sanitize.Text {
	c := t.cfg
	if c.raw {
		return ui.L("raw")
	}
	if c.query != "" && !c.largeWait {
		if len(c.matches) == 0 {
			return ui.L("0 matches")
		}
		return sanitize.From(fmt.Sprintf("%d/%d matches", c.matchIdx+1, len(c.matches)))
	}
	if c.largeWait && c.search {
		return sanitize.From(fmt.Sprintf("%s to search %s nodes", cfgEnter(), commaInt(nodeCount(c))))
	}
	return sanitize.From("tree" + cfgSep() + commaInt(nodeCount(c)) + " nodes")
}

func (t Model) rootBanner() string {
	c := t.cfg
	if c.tree == nil || len(c.tree.Nodes) == 0 || c.err != nil {
		return ""
	}
	if c.tree.Nodes[0].Kind != jsonx.KindObject {
		return ui.G().Warn + " config.json is not a JSON object; Key fields unavailable"
	}
	return ""
}

func (t Model) treeRows(width, rows int, focused bool, x, y, paneW int) []string {
	c := t.cfg
	c.ensureRows()
	t.clampTree()
	if len(c.rows) == 0 {
		return nil
	}
	cur := c.indexOfCursor()
	var out []string
	for i := c.scroll; i < len(c.rows) && len(out) < rows; i++ {
		row := c.rows[i]
		sel := i == cur
		out = append(out, t.paintTreeRow(row, width, sel, focused))
		yy := y + len(out) - 1
		if row.more {
			c.hits = append(c.hits, hit{id: fmt.Sprintf("cfg:more:%d", row.id), x: x, y: yy, w: paneW, h: 1})
		} else if !row.hidden {
			c.hits = append(c.hits, hit{id: fmt.Sprintf("cfg:node:%d", row.id), x: x, y: yy, w: paneW, h: 1})
			c.hits = append(c.hits, hit{id: fmt.Sprintf("cfg:exp:%d", row.id), x: x, y: yy, w: 6, h: 1})
		}
	}
	return out
}

func (t Model) paintTreeRow(row crow, width int, selected, focused bool) string {
	plain := t.treePlain(row, width)
	if ui.ColorOff() {
		g := " "
		if selected {
			g = ">"
		} else if row.match {
			g = "*"
		}
		return clipCells(g+plain, width)
	}
	bg := (*ui.Token)(nil)
	if selected {
		if focused || (t.cfg.flash > 0 && t.motion) {
			bg = &ui.SurfaceSel
		} else {
			bg = &ui.SurfaceSeg
		}
	}
	if ui.Profile() == "ansi16" && selected && focused {
		return lipgloss.NewStyle().Reverse(true).Render(ui.Fit(plain, width))
	}
	return ui.PaintToken(ui.Fit(plain, width), ui.TextSecondary, bg, false)
}

func (t Model) treePlain(row crow, width int) string {
	c := t.cfg
	gl := ui.G()
	if row.more || row.hidden {
		return clipCells(t.moreText(row), width)
	}
	if c.tree == nil || row.id < 0 || int(row.id) >= len(c.tree.Nodes) {
		return ""
	}
	n := c.tree.Nodes[row.id]
	container := n.Kind == jsonx.KindObject || n.Kind == jsonx.KindArray
	open := container && c.isOpen(row.id)
	var b strings.Builder
	b.WriteString(guideText(row, container, open))
	if n.Parent < 0 {
		fmt.Fprintf(&b, "config.json {%d}", n.Len)
		return clipCells(b.String(), width)
	}
	key := c.displayKey(row.id)
	kw := c.keyWidth(n.Parent)
	keyW := uniseg.StringWidth(key)
	b.WriteString(key)
	if keyW <= kw {
		b.WriteString(strings.Repeat(" ", kw-keyW))
	}
	b.WriteString(": ")
	if container && c.inline(row.id) {
		b.WriteString(c.inlineJSON(row.id, width))
	} else if container {
		b.WriteString(c.preview(row.id, width))
	} else {
		b.WriteString(c.scalarText(row.id))
		if n.Kind == jsonx.KindNumber {
			if h := magHint(n.Lit); h != "" {
				b.WriteString(" ")
				b.WriteString(gl.Dot)
				if gl.Dot == " " {
					b.WriteString(".")
				}
				b.WriteString(h)
			}
		}
		if n.Kind == jsonx.KindString {
			extra := int(n.Len) - utfLen(c.litText(row.id).String())
			if extra > 0 {
				fmt.Fprintf(&b, " (+%s chars)", commaInt(extra))
			}
		}
	}
	return clipCells(b.String(), width)
}

func utfLen(s string) int { return uniseg.StringWidth(s) }

func guideText(row crow, container, open bool) string {
	var b strings.Builder
	d := int(row.depth)
	for i := 1; i < d; i++ {
		if row.anc&(1<<uint(i)) != 0 {
			b.WriteString("   ")
		} else if ui.UTF8() {
			b.WriteString("│  ")
		} else {
			b.WriteString("|  ")
		}
	}
	if d == 0 {
		if !container {
			return ""
		}
		if ui.UTF8() {
			if open {
				return "▾ "
			}
			return "▸ "
		}
		if open {
			return "- "
		}
		return "+ "
	}
	if ui.UTF8() {
		br := "├"
		if row.last {
			br = "└"
		}
		switch {
		case container && open:
			b.WriteString(br + "▾ ")
		case container:
			b.WriteString(br + "▸ ")
		default:
			b.WriteString(br + "─ ")
		}
		return b.String()
	}
	br := "|"
	if row.last {
		br = "`"
	}
	if container && !open {
		b.WriteString(br + "+ ")
	} else {
		b.WriteString(br + "- ")
	}
	return b.String()
}

func (t Model) moreText(row crow) string {
	gl := ui.G()
	sep := " · "
	if !ui.UTF8() {
		sep = " . "
	}
	if row.hidden {
		return fmt.Sprintf("%s %s hidden by filter", gl.Ellipsis, commaInt(row.hideN))
	}
	enter := cfgEnter()
	shift := "⇧" + enter
	if !ui.UTF8() {
		shift = "shift+" + enter
	}
	if row.lead {
		return fmt.Sprintf("%s %s more%s%s show %d", gl.Ellipsis, commaInt(row.hideN), sep, enter, pageSize)
	}
	return fmt.Sprintf("%s %s more%s%s show %d%s%s show all (≤ %s)",
		gl.Ellipsis, commaInt(row.hideN), sep, enter, pageSize, sep, shift, commaInt(pageAllMax))
}

func (t Model) valueStrip(w, n int) []string {
	c := t.cfg
	if c.tree == nil || len(c.rows) == 0 {
		return padN(nil, n)
	}
	id := c.cursor
	if id < 0 || int(id) >= len(c.tree.Nodes) {
		return padN(nil, n)
	}
	path := sanitize.From(displayPath(c, id)).String()
	val := sanitize.From(c.scalarText(id)).String()
	if c.tree.Nodes[id].Kind == jsonx.KindObject || c.tree.Nodes[id].Kind == jsonx.KindArray {
		val = sanitize.From(c.preview(id, w)).String()
	}
	pos := fmt.Sprintf("%d/%d", int(id)+1, len(c.tree.Nodes))
	if n == 1 {
		line := path + " = " + val
		gap := w - uniseg.StringWidth(line) - uniseg.StringWidth(pos) - 2
		if gap < 1 {
			line = clipCells(line, w-uniseg.StringWidth(pos)-2)
			gap = 1
		}
		text := " " + line + strings.Repeat(" ", gap) + pos + " "
		if ui.ColorOff() {
			return []string{clipCells(text, w)}
		}
		return []string{ui.PaintToken(ui.Fit(text, w), ui.TextPrimary, &ui.SurfaceSeg, false)}
	}
	var out []string
	out = append(out, ui.Title(ui.S(ui.L("VALUE")), ui.Safe{}, w))
	out = append(out, clipCells(path, w))
	from := ""
	if c.fromField != "" {
		from = "from Key fields " + ui.G().Arrow + " " + c.fromField
	}
	out = append(out, clipCells(val+"   "+from+"   y path"+cfgSep()+"Y value", w))
	out = append(out, "")
	return padN(out, n)
}

func padN(in []string, n int) []string {
	for len(in) < n {
		in = append(in, "")
	}
	if len(in) > n {
		return in[:n]
	}
	return in
}

func (t Model) searchRow(w int) string {
	c := t.cfg
	shown := sanitize.From(c.query).String()
	mode := "search"
	if c.filter {
		mode = "filter"
	}
	hint := mode + cfgSep() + cfgEnter() + " keep" + cfgSep() + "tab switch"
	if c.largeWait {
		hint = fmt.Sprintf("%s to search %s nodes", cfgEnter(), commaInt(nodeCount(c)))
	}
	left := "/ " + shown
	gap := w - 2 - uniseg.StringWidth(left) - uniseg.StringWidth(hint)
	if gap < 1 {
		return clipCells(" "+left, w)
	}
	text := " " + left + strings.Repeat(" ", gap) + hint
	if ui.ColorOff() {
		return clipCells(text, w)
	}
	return ui.PaintToken(ui.Fit(clipCells(text, w), w), ui.TextPrimary, &ui.SurfaceSeg, false)
}

func (t Model) rawBody(width, rows int) []string {
	t.ensureRaw()
	t.clampRaw()
	c := t.cfg
	var out []string
	for i := c.rawScroll; i < len(c.rawLines) && len(out) < rows; i++ {
		mark := " "
		if i == c.rawLine {
			mark = ">"
		}
		num := fmt.Sprintf("%4d ", i+1)
		out = append(out, clipCells(mark+num+c.rawLines[i].String(), width))
	}
	if c.rawCut && len(out) < rows {
		out = append(out, fmt.Sprintf("showing %s of %s", mib(configRawMax), mib(len(t.m.ConfigRaw))))
	}
	return out
}

func (t Model) parseCard(w, h int) []string {
	inner := innerWidth(w)
	body := []string{"", ui.Pill(ui.FillKindFail, ui.L("PARSE ERROR")), ""}
	body = append(body, wrapPlain(t.parseMessage(), inner)...)
	body = append(body, "")
	body = append(body, t.excerptLines(inner)...)
	body = append(body, "", "Key fields and the tree need a parsed config.")
	return chrome(ui.L("CONFIG.JSON"), sanitize.Text{}, sanitize.Text{}, body, w, h, true)
}

func (t Model) parseMessage() string {
	err := t.cfg.err
	if err == nil {
		return "config.json did not parse."
	}
	msg := err.Error()
	loc, _ := err.(*jsonx.LocatedError)
	line, col := 1, 1
	if loc != nil && t.m != nil {
		line, col = lineCol(t.m.ConfigRaw, loc.Off)
	}
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "duplicate key"):
		return "config.json did not parse. " + sanitize.From(msg).String() + fmt.Sprintf(" at line %d", line)
	case strings.Contains(lower, "nesting exceeds"):
		return fmt.Sprintf("config.json did not parse. nesting deeper than %d at line %d", jsonx.MaxDepth, line)
	default:
		return fmt.Sprintf("config.json did not parse. %s at line %d, col %d", sanitize.From(msg).String(), line, col)
	}
}

func (t Model) excerptLines(width int) []string {
	raw := t.m.ConfigRaw
	loc, _ := t.cfg.err.(*jsonx.LocatedError)
	off := 0
	if loc != nil {
		off = int(loc.Off)
	}
	if off < 0 {
		off = 0
	}
	if off > len(raw) {
		off = len(raw)
	}
	line, _ := lineCol(raw, int64(off))
	lines := splitLines(raw)
	var out []string
	for n := line - 2; n <= line+2; n++ {
		if n < 1 || n > len(lines) {
			continue
		}
		text := sanitize.From(lines[n-1]).String()
		row := fmt.Sprintf("%5d %s", n, text)
		if n == line && !ui.ColorOff() {
			row = ui.PaintToken(ui.Fit(clipCells(row, width), width), ui.TextPrimary, &ui.SurfaceSeg, false)
		} else {
			row = clipCells(row, width)
		}
		out = append(out, row)
		if n == line {
			caret := strings.Repeat(" ", 6) + ui.G().Size + " here"
			if hint := heuristic(raw, off); hint != "" {
				caret += " (" + hint + ")"
			}
			out = append(out, clipCells(caret, width))
		}
	}
	return out
}

func heuristic(raw []byte, off int) string {
	if off > len(raw) {
		off = len(raw)
	}
	i := off - 1
	for i >= 0 {
		c := raw[i]
		if c == ' ' || c == '\n' || c == '\t' || c == '\r' {
			i--
			continue
		}
		break
	}
	if i >= 0 && raw[i] == ',' {
		return "trailing comma?"
	}
	window := raw
	if off > 0 && off <= len(raw) {
		a := off - 8
		if a < 0 {
			a = 0
		}
		b := off + 8
		if b > len(raw) {
			b = len(raw)
		}
		window = raw[a:b]
	}
	s := string(window)
	if strings.Contains(s, "NaN") || strings.Contains(s, "Infinity") {
		return "NaN or Infinity is not JSON"
	}
	return ""
}

func lineCol(raw []byte, off int64) (int, int) {
	line, col := 1, 1
	if off < 0 {
		off = 0
	}
	if int(off) > len(raw) {
		off = int64(len(raw))
	}
	for i := int64(0); i < off; i++ {
		if raw[i] == '\n' {
			line++
			col = 1
			continue
		}
		col++
	}
	return line, col
}

func splitLines(raw []byte) []string {
	if len(raw) > configRawMax {
		raw = raw[:configRawMax]
	}
	s := string(raw)
	parts := strings.Split(s, "\n")
	if len(parts) > 100_000 {
		parts = parts[:100_000]
	}
	return parts
}

func (t Model) nodeCard(w, h int) []string {
	return chrome(ui.L("CONFIG.JSON"), ui.L("r raw"), sanitize.Text{}, wrapPlain(nodeLimitText(t.cfg.err), innerWidth(w)), w, h, true)
}

func nodeLimitText(err error) string {
	lim, _ := err.(*jsonx.NodeLimitError)
	if lim == nil {
		return "Tree disabled."
	}
	return fmt.Sprintf("Tree disabled: %s nodes (limit %s). Key fields are still read from the parsed config. r shows the first 1 MiB of the raw file (sanitized).",
		commaInt(lim.Nodes), commaInt(lim.Limit))
}

func (t Model) configModal(w, h int) string {
	c := t.cfg
	title := "VALUE"
	body := ""
	if c.tree != nil && c.modalID >= 0 && int(c.modalID) < len(c.tree.Nodes) {
		n := c.tree.Nodes[c.modalID]
		title = displayPath(c, c.modalID)
		body = c.scalarText(c.modalID)
		if n.Kind == jsonx.KindObject || n.Kind == jsonx.KindArray {
			body = compactJSON(c, c.modalID, 8192)
		}
		if full, ok := c.tree.FullBytes[c.modalID]; ok && full > len(n.Lit) {
			note := fmt.Sprintf("… truncated at 64 KiB of %s", mib1(int64(full)))
			// The note is the first line so it stays on screen, and the last
			// line so the scrollable text still ends with it.
			body = note + "\n" + body + "\n" + note
		}
	}
	wrapped := wrapWords(sanitize.From(body).String(), min(w-8, 68))
	start := c.modalScroll
	// Keep the card inside the screen. A tall body is centered and the
	// truncation line, which is the last row, would be clipped off.
	vis := h - 14
	if vis < 3 {
		vis = 3
	}
	if vis > 12 {
		vis = 12
	}
	if start < 0 {
		start = 0
	}
	if start > len(wrapped)-vis {
		start = len(wrapped) - vis
	}
	if start < 0 {
		start = 0
	}
	end := start + vis
	if end > len(wrapped) {
		end = len(wrapped)
	}
	text := strings.Join(wrapped[start:end], "\n")
	return ui.Modal(ui.ModalSpec{
		Title: ui.S(sanitize.From(title)),
		Body:  ui.S(sanitize.From(text)),
		Width: min(w-4, 72),
	}, w, h)
}

func wrapPlain(s string, width int) []string {
	if width < 4 {
		width = 4
	}
	return wrapWords(s, width)
}

func wrapWords(s string, width int) []string {
	if width < 4 {
		width = 4
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		if para == "" {
			out = append(out, "")
			continue
		}
		var line strings.Builder
		for _, word := range strings.Fields(para) {
			if line.Len() == 0 {
				line.WriteString(word)
				continue
			}
			if uniseg.StringWidth(line.String())+1+uniseg.StringWidth(word) > width {
				out = append(out, line.String())
				line.Reset()
				line.WriteString(word)
				continue
			}
			line.WriteByte(' ')
			line.WriteString(word)
		}
		if line.Len() > 0 {
			out = append(out, line.String())
		}
	}
	return out
}

func chrome(title, right, sub sanitize.Text, body []string, w, h int, focused bool) []string {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	pad := 2
	if w < 40 {
		pad = 1
	}
	edge := ""
	if !ui.ColorOff() && ui.Profile() != "ansi16" && ui.UTF8() {
		edge = ui.G().Edge
	}
	inner := w - pad*2 - uniseg.StringWidth(edge)
	if inner < 1 {
		inner = 1
	}
	name := strings.ToUpper(title.String())
	var titleCell string
	switch {
	case ui.ColorOff() && focused:
		titleCell = "[ " + name + " ]"
	case ui.ColorOff():
		titleCell = name
	case focused && ui.Profile() == "ansi16":
		titleCell = lipgloss.NewStyle().Reverse(true).Bold(true).Render(" " + name + " ")
	case focused:
		titleCell = ui.PaintToken(" "+name+" ", ui.TextOnFill, &ui.FillViolet, true)
	default:
		titleCell = ui.PaintToken(name, ui.TextPrimary, nil, true)
	}
	subStr := sub.String()
	if subStr != "" {
		subStr = "  " + subStr
	}
	rightStr := right.String()
	left := titleCell + subStr
	gap := inner - ui.Width(left) - ui.Width(rightStr)
	if gap < 1 {
		gap = 1
		rightStr = clipCells(rightStr, max(0, inner-ui.Width(left)-1))
		gap = inner - ui.Width(left) - ui.Width(rightStr)
		if gap < 1 {
			gap = 1
		}
	}
	titleLine := left + strings.Repeat(" ", gap) + rightStr
	if !ui.ColorOff() && rightStr != "" {
		titleLine = left + strings.Repeat(" ", gap) + ui.PaintToken(rightStr, ui.TextMuted, nil, false)
	}
	var out []string
	out = append(out, fitChrome(edge, pad, titleLine, inner, w, true, focused))
	for i := 0; i < h-1; i++ {
		raw := ""
		if i < len(body) {
			raw = body[i]
		}
		out = append(out, fitChrome(edge, pad, raw, inner, w, false, focused))
	}
	return out
}

func fitChrome(edge string, pad int, content string, inner, w int, title, focused bool) string {
	content = clipANSI(content, inner)
	if ui.ColorOff() || ui.Profile() == "ansi16" {
		line := strings.Repeat(" ", pad) + ui.Fit(ui.Strip(content), inner) + strings.Repeat(" ", pad)
		return ui.Fit(line, w)
	}
	padS := strings.Repeat(" ", pad)
	gap := inner - ui.Width(content)
	if gap < 0 {
		gap = 0
	}
	body := ui.PaintToken(padS, ui.TextSecondary, &ui.SurfaceCard, false) + content + ui.PaintToken(strings.Repeat(" ", gap+pad), ui.TextSecondary, &ui.SurfaceCard, false)
	if edge == "" {
		return ui.Fit(body, w)
	}
	col := ui.SurfaceSeg
	if focused {
		col = ui.Accent
	}
	return ui.Fit(ui.PaintToken(edge, col, &ui.SurfaceCard, false)+body, w)
}

func clipANSI(s string, width int) string {
	if ui.Width(s) <= width {
		return s
	}
	return ui.Trunc(s, width, ui.G().Ellipsis)
}

func innerWidth(w int) int {
	pad := 2
	if w < 40 {
		pad = 1
	}
	edge := 0
	if !ui.ColorOff() && ui.Profile() != "ansi16" && ui.UTF8() {
		edge = 1
	}
	inner := w - pad*2 - edge
	if inner < 1 {
		return 1
	}
	return inner
}

func fitPlain(s string, w int) string { return clipCells(s, w) }
