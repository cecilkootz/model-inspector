package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/rivo/uniseg"

	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
	"origin.cursor.com/cecilkootz/model-inspector/internal/sanitize"
	"origin.cursor.com/cecilkootz/model-inspector/internal/tui/ui"
)

func (t Model) layersBody() string {
	if t.lay == nil {
		return ""
	}
	t.lay.hits = t.lay.hits[:0]
	w, h := t.width, t.bodyHeight()
	return joinFit(t.layersLines(w, h), w, h)
}

func (t Model) layersLines(w, h int) []string {
	if msg, title := t.layersProblem(); msg.String() != "" {
		body := []string{"", msg.String()}
		return chrome(title, sanitize.Text{}, sanitize.Text{}, body, w, h, true)
	}
	br := ui.Breakpoint(t.width, t.height)
	if t.lay.drill && (br == "S" || br == "XS") {
		return t.layerDetailLines(0, 2, w, h, true)
	}
	heat := []string(nil)
	if !t.zoom {
		heat = t.layerHeatLines(w)
	}
	sumN := 0
	paneH := h
	if t.zoom && t.focusSecondary && (br == "M" || br == "L") {
		return t.layerDetailLines(0, 2, w, h, true)
	}
	single := br == "S" || br == "XS" || t.zoom
	if single {
		sumN = 1
	}
	room := h - sumN
	if room < 4 {
		room = 4
		sumN = 0
	}
	if len(heat) > room-4 {
		keep := room - 4
		if keep < 0 {
			keep = 0
		}
		heat = heat[:keep]
	}
	paneH = h - len(heat) - sumN
	if paneH < 3 {
		paneH = 3
	}
	t.addHeatHits(heat, w, br == "M" || br == "L")
	ratio := t.layersRatio()
	rects := ui.Layout(w, paneH+3, ratio, t.zoom, t.focusSecondary)
	treeY := 2 + len(heat)
	var panes []string
	if !rects.Split {
		panes = t.layerTreeLines(0, treeY, w, paneH, true)
	} else {
		left := t.layerTreeLines(rects.Primary.X, treeY, rects.Primary.W, paneH, !t.focusSecondary)
		right := t.layerDetailLines(rects.Secondary.X, treeY, rects.Secondary.W, paneH, t.focusSecondary)
		panes = zipCols(left, right, rects.Primary.W, rects.Secondary.W, w)
	}
	out := append(append([]string{}, heat...), panes...)
	if sumN > 0 {
		out = append(out, t.layerSummaryLine(w))
	}
	for len(out) < h {
		out = append(out, "")
	}
	if len(out) > h {
		out = out[:h]
	}
	return out
}

func (t Model) layersRatio() int {
	if t.lay != nil && t.lay.ratioSet {
		return t.lay.ratio
	}
	if ui.Breakpoint(t.width, t.height) == "L" {
		return 62
	}
	return 58
}

func (t Model) layersProblem() (sanitize.Text, sanitize.Text) {
	title := ui.L("LAYERS")
	if t.m == nil {
		return ui.L("No model is loaded."), title
	}
	if strings.HasPrefix(t.m.Format, "gguf") {
		return sanitize.Program(ui.G().NR + " No safetensors weights. GGUF support is planned."), title
	}
	if len(t.m.Tensors) == 0 {
		return sanitize.Program(ui.G().NR + " No weight files found."), title
	}
	if len(t.m.Groups) == 0 {
		return ui.L("No groups in this model."), title
	}
	return sanitize.Text{}, title
}

func (t Model) layerHeatLines(w int) []string {
	if t.derived == nil || t.m == nil {
		return nil
	}
	grid := t.derived.heat
	n := 0
	if len(grid.Rows) > 0 {
		n = len(grid.Rows[0].Cells)
	}
	if n < 1 {
		return nil
	}
	if t.lay != nil && t.lay.attn != attnAll {
		grid.Dim = t.heatDim(grid.Layers)
	}
	if col, ok := t.heatMark(); ok {
		grid.MarkOn = true
		grid.Mark = col
	}
	br := ui.Breakpoint(t.width, t.height)
	return ui.Heatmap(grid, w, br == "M" || br == "L")
}

func (t Model) heatDim(layers []int) []bool {
	if t.lay == nil || t.lay.attn == attnAll || len(layers) == 0 {
		return nil
	}
	dim := make([]bool, len(layers))
	for i, layer := range layers {
		short := ""
		if t.lay.info != nil {
			short = t.lay.info[layer].Short
		}
		switch t.lay.attn {
		case attnFull:
			dim[i] = short != "full"
		default:
			dim[i] = short == "full"
		}
	}
	return dim
}

func (t Model) heatMark() (int, bool) {
	g := t.layersGroup()
	if g == nil || g.Kind != model.KindLayer || t.derived == nil {
		return 0, false
	}
	for i, layer := range t.derived.heat.Layers {
		if layer == g.Layer {
			return i, true
		}
	}
	return 0, false
}

func (t Model) addHeatHits(lines []string, w int, large bool) {
	if t.lay == nil || t.derived == nil || len(lines) == 0 {
		return
	}
	grid := t.derived.heat
	n := len(grid.Layers)
	if n < 1 && len(grid.Rows) > 0 {
		n = len(grid.Rows[0].Cells)
	}
	if n < 1 {
		return
	}
	labelW, cw, pool, cols := ui.HeatGeom(w, n, large)
	if cw < 1 {
		cw = 1
	}
	for li := range lines {
		if li == 0 || li == len(lines)-1 {
			continue
		}
		for c := 0; c < cols; c++ {
			idx := c * pool
			if idx >= n {
				break
			}
			layer := idx
			if idx < len(grid.Layers) {
				layer = grid.Layers[idx]
			}
			t.lay.hits = append(t.lay.hits, hit{
				id: fmt.Sprintf("lay:strip:%d", layer),
				x:  labelW + c*cw, y: 2 + li, w: cw, h: 1,
			})
		}
	}
}

func (t Model) layerTreeLines(x, y, w, h int, focused bool) []string {
	searchN := 0
	if t.lay.search {
		searchN = 1
	}
	jumpN := 0
	if t.lay.jump {
		jumpN = 1
	}
	paneH := h - searchN - jumpN
	if paneH < 2 {
		paneH = 2
	}
	bodyH := paneH - 1
	if bodyH < 1 {
		bodyH = 1
	}
	header := 1
	rowH := bodyH - header
	if rowH < 1 {
		rowH = 1
		header = 0
	}
	t.lay.viewH = rowH
	t.lay.ensure(t.m)
	t.clampLayers()
	inner := innerWidth(w)
	var body []string
	if header > 0 {
		body = append(body, t.columnHeader(inner))
		t.addColHits(x, y+1, inner)
	}
	body = append(body, t.layerRows(inner, rowH, focused, x, y+header, w)...)
	for len(body) < bodyH {
		body = append(body, "")
	}
	if len(body) > bodyH {
		body = body[:bodyH]
	}
	out := chrome(ui.L("TREE"), t.treeRight(), sanitize.Text{}, body, w, paneH, focused)
	if jumpN > 0 {
		out = append(out, t.layersJumpRow(w))
	}
	if searchN > 0 {
		out = append(out, t.layersSearchRow(w))
	}
	for len(out) < h {
		out = append(out, "")
	}
	if len(out) > h {
		out = out[:h]
	}
	return out
}

func (t Model) treeRight() sanitize.Text {
	if t.lay == nil {
		return ui.L("sort: natural")
	}
	s := "sort: " + t.sortTitle()
	if t.lay.outliers {
		s += " outliers"
	}
	switch t.lay.attn {
	case attnLinear:
		s += " linear"
	case attnFull:
		s += " full"
	}
	if t.lay.parts {
		s += " parts"
	}
	return sanitize.Program(s)
}

func (t Model) columnHeader(width int) string {
	cols := t.layerColSpec()
	left := "NAME"
	parts := []string{padPlain(left, max(4, width-cols.fixed()))}
	if cols.dtype {
		parts = append(parts, padPlain("DTYPE", cols.dtypeW))
	}
	parts = append(parts, padPlain("TYPE", cols.typeW))
	parts = append(parts, padPlain("vs siblings", cols.bar))
	parts = append(parts, padPlain("BYTES", cols.bytesW))
	if cols.params {
		parts = append(parts, padPlain("PARAMS", cols.paramsW))
	}
	parts = append(parts, padPlain("%", cols.pctW))
	line := strings.Join(parts, " ")
	line = clipCells(line, width)
	if ui.ColorOff() {
		return line
	}
	bg := &ui.SurfaceCard
	return ui.PaintToken(ui.Fit(line, width), ui.TextMuted, bg, true)
}

func (t Model) addColHits(x, y, inner int) {
	if t.lay == nil {
		return
	}
	cols := t.layerColSpec()
	nameW := inner - cols.fixed()
	if nameW < 4 {
		nameW = 4
	}
	t.lay.hits = append(t.lay.hits, hit{id: "lay:col:name", x: x, y: y, w: nameW, h: 1})
	bx := x + nameW + 1
	if cols.dtype {
		bx += cols.dtypeW + 1
	}
	bx += cols.typeW + 1 + cols.bar + 1
	t.lay.hits = append(t.lay.hits, hit{id: "lay:col:bytes", x: bx, y: y, w: cols.bytesW, h: 1})
}

func (t Model) layersSubtitle() sanitize.Text {
	s := t.lay
	if s != nil && s.query != "" && !s.largeWait {
		if len(s.matches) == 0 {
			return ui.L("0 matches")
		}
		return sanitize.Program(fmt.Sprintf("%d/%d matches", s.matchIdx+1, len(s.matches)))
	}
	n := 0
	if t.m != nil {
		n = len(t.m.Tensors)
	}
	return sanitize.Program(commaInt(n) + " tensors")
}

func (t Model) layerRows(width, rows int, focused bool, x, y, paneW int) []string {
	s := t.lay
	if len(s.rows) == 0 {
		if s.filter && s.query != "" {
			return []string{ui.L("Nothing matches.").String()}
		}
		return nil
	}
	var out []string
	for i := s.scroll; i < len(s.rows) && len(out) < rows; i++ {
		row := s.rows[i]
		sel := i == s.row
		out = append(out, t.paintLayerRow(row, width, sel, focused))
		yy := y + len(out)
		s.hits = append(s.hits, hit{id: fmt.Sprintf("lay:exp:%d", i), x: x, y: yy, w: 3, h: 1})
		s.hits = append(s.hits, hit{id: fmt.Sprintf("lay:%d", i), x: x + 3, y: yy, w: max(1, paneW-3), h: 1})
	}
	return out
}

func (t Model) paintLayerRow(row lrow, width int, selected, focused bool) string {
	if ui.ColorOff() {
		plainW := width
		if plainW > 1 {
			plainW--
		}
		plain := t.layerPlain(row, plainW)
		g := " "
		if selected {
			g = ">"
		} else if row.match {
			g = "*"
		}
		return clipCells(g+plain, width)
	}
	if ui.Profile() == "ansi16" && selected && focused {
		return lipgloss.NewStyle().Reverse(true).Render(ui.Fit(t.layerPlain(row, width), width))
	}
	// A selected row is one fill so the cursor stays visible after the frame
	// cuts any line lipgloss measures wider than the pane.
	if selected {
		bg := &ui.SurfaceSeg
		if focused {
			bg = &ui.SurfaceSel
		}
		return ui.PaintToken(ui.Fit(t.layerPlain(row, width), width), ui.TextPrimary, bg, false)
	}
	line := t.composeRow(row, width, true, false, focused)
	if lipgloss.Width(line) > width {
		return ui.PaintToken(ui.Fit(t.layerPlain(row, width), width), ui.TextSecondary, &ui.SurfaceCard, false)
	}
	return line
}

func (t Model) layerPlain(row lrow, width int) string {
	return t.composeRow(row, width, false, false, false)
}

type layerColSpec struct {
	dtype         bool
	dtypeW, typeW int
	bar, bytesW   int
	pctW, paramsW int
	params        bool
}

func (c layerColSpec) fixed() int {
	// gaps between the columns that follow the name
	n := 3
	w := c.typeW + c.bar + c.bytesW + c.pctW
	if c.dtype {
		w += c.dtypeW
		n++
	}
	if c.params {
		w += c.paramsW
		n++
	}
	return w + n
}

func (t Model) layerColSpec() layerColSpec {
	br := ui.Breakpoint(t.width, t.height)
	c := layerColSpec{typeW: 12, bar: 8, bytesW: 11, pctW: 6, paramsW: 8}
	if br == "L" {
		c.bar = 10
	}
	if t.width >= 110 && br != "S" && br != "XS" {
		c.dtype = true
		c.dtypeW = 10
	}
	if t.width >= 160 {
		c.params = true
	}
	return c
}

func (t Model) composeRow(row lrow, width int, color, selected, focused bool) string {
	if width < 1 {
		return ""
	}
	if row.more || row.hidden || row.kind == rowMore {
		text := clipCells(t.moreText(crow{
			more: row.more, lead: row.lead, hidden: row.hidden, hideN: row.hideN, last: row.last,
		}), width)
		return t.fillRow(text, width, color, selected, focused)
	}
	cols := t.layerColSpec()
	leftW := width - cols.fixed()
	if leftW < 8 {
		cols.bar = 4
		cols.typeW = 8
		cols.bytesW = 8
		if cols.dtype {
			cols.dtypeW = 8
		}
		leftW = width - cols.fixed()
	}
	if leftW < 4 {
		leftW = 4
	}
	left, dtype, typ, bytes, pct, params, frac := t.rowFields(row)
	bg := (*ui.Token)(nil)
	if color && !ui.ColorOff() {
		bg = &ui.SurfaceCard
		if selected {
			if focused {
				bg = &ui.SurfaceSel
			} else {
				bg = &ui.SurfaceSeg
			}
		}
	}
	var parts []string
	parts = append(parts, t.fillRow(clipLabel(left, leftW), leftW, color, selected, focused))
	if cols.dtype {
		parts = append(parts, t.dtypeCell(dtype, cols.dtypeW, color, bg))
	}
	parts = append(parts, t.fillRow(clipCells(typ, cols.typeW), cols.typeW, color, selected, focused))
	parts = append(parts, t.barCell(frac, cols.bar, color))
	parts = append(parts, t.fillRow(clipCells(bytes, cols.bytesW), cols.bytesW, color, selected, focused))
	if cols.params {
		parts = append(parts, t.fillRow(clipCells(params, cols.paramsW), cols.paramsW, color, selected, focused))
	}
	parts = append(parts, t.fillRow(clipCells(pct, cols.pctW), cols.pctW, color, selected, focused))
	line := strings.Join(parts, " ")
	if ui.Width(line) > width {
		return clipANSI(line, width)
	}
	if color && !ui.ColorOff() && ui.Profile() != "ansi16" {
		gap := width - ui.Width(line)
		if gap > 0 {
			line += ui.PaintToken(strings.Repeat(" ", gap), ui.TextSecondary, bg, false)
		}
	}
	return ui.Fit(line, width)
}

func (t Model) fillRow(text string, width int, color, selected, focused bool) string {
	text = padPlain(text, width)
	if !color || ui.ColorOff() || ui.Profile() == "ansi16" {
		return text
	}
	bg := &ui.SurfaceCard
	if selected {
		if focused {
			bg = &ui.SurfaceSel
		} else {
			bg = &ui.SurfaceSeg
		}
	}
	return ui.PaintToken(text, ui.TextSecondary, bg, false)
}

func (t Model) dtypeCell(dtype string, width int, color bool, bg *ui.Token) string {
	if dtype == "" {
		return t.fillRow("", width, color, bg == &ui.SurfaceSel, bg == &ui.SurfaceSel)
	}
	if !color || ui.ColorOff() {
		return padPlain(clipCells("["+dtype+"]", width), width)
	}
	pill := ui.Pill(dtypeKind(dtype), t.lay.once(dtype))
	if ui.Width(pill) > width {
		return clipANSI(pill, width)
	}
	gap := width - ui.Width(pill)
	if gap > 0 && ui.Profile() != "ansi16" {
		pill += ui.PaintToken(strings.Repeat(" ", gap), ui.TextSecondary, bg, false)
	}
	return pill
}

func dtypeKind(dtype string) ui.FillKind {
	low := strings.ToLower(dtype)
	switch {
	case strings.Contains(low, "bf16"), strings.Contains(low, "f16"), strings.Contains(low, "f32"):
		return ui.FillKindInfo
	case strings.HasPrefix(low, "q"):
		return ui.FillKindViolet
	default:
		return ui.FillKindInfo
	}
}

func (t Model) barCell(frac float64, width int, color bool) string {
	if width < 1 {
		return ""
	}
	bar, _ := ui.Meter(frac, width, ui.GradMeter)
	if !color || ui.ColorOff() {
		return clipCells(ui.Strip(bar), width)
	}
	return bar
}

func (t Model) rowFields(row lrow) (left, dtype, typ, bytes, pct, params string, frac float64) {
	container := row.kind == rowGroup || row.kind == rowSub
	open := container && t.lay != nil && t.lay.isOpen(row.path)
	guide := guideText(crow{depth: uint8(row.depth), anc: row.anc, last: row.last}, container, open)
	label := t.rowLabel(row)
	left = guide + label
	n := t.rowBytes(row)
	bytes = model.HumanBytes(n)
	total := int64(0)
	if t.m != nil {
		total = t.m.TensorBytes
		if total <= 0 {
			total = t.m.TotalSize
		}
	}
	pct = shareText(n, total)
	frac = t.rowFrac(row, n)
	params = humanCount(t.rowParams(row))
	dtype, typ = t.rowDtypeType(row)
	return left, dtype, typ, bytes, pct, params, frac
}

func (t Model) rowLabel(row lrow) string {
	switch {
	case row.path == "dec":
		return "decoder layers " + strconv.Itoa(t.layerCount())
	case row.kind == rowGroup:
		g := t.groupAt(row.gidx)
		return t.layerGroupLabel(g).String()
	case row.kind == rowSub:
		return t.subRowLabel(row)
	case row.kind == rowTensor:
		g := t.groupAt(row.gidx)
		lt, ok := t.layerTensor(row)
		if !ok {
			return ""
		}
		return t.layerTensorLabel(g, lt).String()
	default:
		return ""
	}
}

func (t Model) subRowLabel(row lrow) string {
	base := subLabel(row.sub).String()
	gl := ui.G()
	switch row.sub {
	case "attn":
		kind := "linear"
		if t.attnGlyph(t.groupAt(row.gidx)) == gl.Full {
			kind = "full"
		}
		return base + " " + gl.Dot + " " + kind
	case "moe":
		n := t.expertN(t.groupAt(row.gidx))
		if n > 0 {
			return base + " " + gl.Dot + " " + strconv.Itoa(n) + " experts"
		}
		return base
	case "experts":
		n := t.expertN(t.groupAt(row.gidx))
		if n > 1 {
			return base + " " + ui.Times() + strconv.Itoa(n)
		}
		return base
	default:
		return base
	}
}

func (t Model) rowDtypeType(row lrow) (string, string) {
	gl := ui.G()
	switch {
	case row.path == "dec":
		lin, full := t.attnCounts()
		return t.decoderDtype(), strconv.Itoa(lin) + gl.Linear + " " + strconv.Itoa(full) + gl.Full
	case row.kind == rowSub:
		return t.packDtype(row), t.subType(row)
	case row.kind == rowTensor:
		g := t.groupAt(row.gidx)
		lt, ok := t.layerTensor(row)
		if !ok {
			return "", ""
		}
		typ := ""
		if lt.Host {
			typ = gl.Host + " host"
		}
		if g != nil && g.ExpertStacked() {
			if n, ok := g.ExpertCount(); ok && n > 0 && len(lt.Shape) > 0 && lt.Shape[0] == int64(n) {
				typ = "stacked"
			}
		}
		if t.logicalDiamond(g, lt) {
			typ = joinMark(typ, precisionMark())
		}
		return t.tensorDtypeText(lt), typ
	default:
		g := t.groupAt(row.gidx)
		if g == nil {
			return "", ""
		}
		typ := ""
		if g.Kind == model.KindLayer {
			typ = t.attnGlyph(g)
			if n := t.expertN(g); n > 0 {
				per := 0
				if t.lay != nil {
					per = t.lay.perTok
				}
				if per > 0 {
					typ += " " + expertJoin(strconv.Itoa(n), strconv.Itoa(per))
				} else {
					typ += " " + strconv.Itoa(n)
				}
			}
			if t.lay != nil && t.lay.info != nil && t.lay.info[g.Layer].Short == "?" {
				typ = joinMark(typ, "?")
			}
		}
		if g.Key == "ngram" {
			typ = gl.Host + " host"
			if mark := t.ngramMark(g); mark != "" {
				typ = mark
			}
		}
		if t.groupDiamond(g) {
			typ = joinMark(typ, precisionMark())
		}
		if t.lay != nil && t.lay.big[g.Key] {
			typ = joinMark(typ, gl.Size)
		}
		return t.groupDtypeText(g), typ
	}
}

func (t Model) subType(row lrow) string {
	g := t.groupAt(row.gidx)
	n := t.expertN(g)
	per := 0
	if t.lay != nil {
		per = t.lay.perTok
	}
	var typ string
	switch row.sub {
	case "attn":
		typ = t.attnGlyph(g)
	case "moe":
		if n > 0 && per > 0 {
			typ = expertJoin(strconv.Itoa(n), strconv.Itoa(per)) + "/tok"
		} else if n > 0 {
			typ = strconv.Itoa(n)
		}
	case "shared":
		typ = "+1/tok"
	case "experts":
		if per > 0 {
			typ = strconv.Itoa(per) + "/tok"
		}
	}
	if t.subDiamond(row) {
		typ = joinMark(typ, precisionMark())
	}
	return typ
}

func joinMark(typ, mark string) string {
	if mark == "" {
		return typ
	}
	if typ == "" {
		return mark
	}
	return typ + " " + mark
}

func expertJoin(a, b string) string {
	if ui.UTF8() {
		return a + "·" + b
	}
	return a + "x" + b
}

func (t Model) attnGlyph(g *model.Group) string {
	gl := ui.G()
	if g == nil || t.lay == nil || t.lay.info == nil {
		return gl.Linear
	}
	if t.lay.info[g.Layer].Short == "full" {
		return gl.Full
	}
	return gl.Linear
}

func (t Model) expertN(g *model.Group) int {
	if g == nil {
		return 0
	}
	n, ok := g.ExpertCount()
	if !ok {
		return 0
	}
	return n
}

func (t Model) layerCount() int {
	n := 0
	if t.m == nil {
		return 0
	}
	for _, g := range t.m.Groups {
		if g != nil && g.Kind == model.KindLayer {
			n++
		}
	}
	return n
}

func (t Model) attnCounts() (lin, full int) {
	if t.m == nil {
		return 0, 0
	}
	for _, g := range t.m.Groups {
		if g == nil || g.Kind != model.KindLayer {
			continue
		}
		if t.attnGlyph(g) == ui.G().Full {
			full++
		} else {
			lin++
		}
	}
	return lin, full
}

func (t Model) decoderDtype() string {
	var best *model.Group
	if t.m == nil {
		return ""
	}
	for _, g := range t.m.Groups {
		if g == nil || g.Kind != model.KindLayer {
			continue
		}
		if best == nil || g.Bytes > best.Bytes {
			best = g
		}
	}
	return t.groupDtypeText(best)
}

func (t Model) groupDtypeText(g *model.Group) string {
	if g == nil {
		return ""
	}
	label := g.StorageLabel()
	if label == "" || label == "—" {
		return ""
	}
	if t.m != nil && t.m.Quant != nil && t.m.Quant.GroupSize > 0 && strings.HasPrefix(label, "q") && !strings.Contains(label, "+") {
		return label + " g" + strconv.Itoa(t.m.Quant.GroupSize)
	}
	return label
}

func (t Model) tensorDtypeText(lt model.LogicalTensor) string {
	if b := quantBadge(t.m, lt.Role); b != "" {
		return b
	}
	if lt.DType != "" {
		return strings.ToLower(lt.DType)
	}
	return lt.Role
}

func quantBadge(m *model.Model, role string) string {
	if role == "" || role == "plain" || role == "qscal" || role == "qbias" {
		return ""
	}
	if !strings.HasPrefix(role, "q") {
		return ""
	}
	if m != nil && m.Quant != nil && m.Quant.GroupSize > 0 && !strings.Contains(role, "+") {
		return role + " g" + strconv.Itoa(m.Quant.GroupSize)
	}
	return role
}

func (t Model) packDtype(row lrow) string {
	var rows []model.LogicalTensor
	if t.lay != nil {
		for _, p := range t.lay.packs(t.m, row.gidx) {
			if p.sub == row.sub {
				rows = p.rows
				break
			}
		}
	}
	if len(rows) == 0 {
		return t.groupDtypeText(t.groupAt(row.gidx))
	}
	var best string
	var bestN int64
	for _, lt := range rows {
		d := t.tensorDtypeText(lt)
		if lt.Bytes >= bestN && d != "" {
			bestN = lt.Bytes
			best = d
		}
	}
	return best
}

func (t Model) rowBytes(row lrow) int64 {
	if row.path == "dec" {
		var n int64
		if t.m != nil {
			for _, g := range t.m.Groups {
				if g != nil && g.Kind == model.KindLayer {
					n += g.Bytes
				}
			}
		}
		return n
	}
	if row.kind == rowTensor {
		lt, ok := t.layerTensor(row)
		if ok {
			return lt.Bytes
		}
		return 0
	}
	if row.kind == rowSub && t.lay != nil {
		var n int64
		for _, p := range t.lay.packs(t.m, row.gidx) {
			if p.sub != row.sub {
				continue
			}
			for _, lt := range p.rows {
				n += lt.Bytes
			}
		}
		return n
	}
	if g := t.groupAt(row.gidx); g != nil {
		return g.Bytes
	}
	return 0
}

func (t Model) rowParams(row lrow) int64 {
	if row.path == "dec" {
		var n int64
		if t.m != nil {
			for _, g := range t.m.Groups {
				if g != nil && g.Kind == model.KindLayer {
					n += g.LogicalParams
				}
			}
		}
		return n
	}
	if row.kind == rowTensor {
		lt, ok := t.layerTensor(row)
		if ok {
			return lt.Params
		}
		return 0
	}
	if g := t.groupAt(row.gidx); g != nil {
		return g.LogicalParams
	}
	return 0
}

func (t Model) rowFrac(row lrow, bytes int64) float64 {
	if t.lay == nil || bytes <= 0 {
		return 0
	}
	var maxB int64
	for _, r := range t.lay.rows {
		if r.parent != row.parent || r.kind == rowMore {
			continue
		}
		if b := t.rowBytes(r); b > maxB {
			maxB = b
		}
	}
	if maxB <= 0 {
		return 0
	}
	return float64(bytes) / float64(maxB)
}

func shareText(part, total int64) string {
	if total <= 0 || part <= 0 {
		return "0%"
	}
	p := float64(part) / float64(total) * 100
	if p < 0.1 {
		return "<0.1%"
	}
	if p >= 99.95 {
		return "100%"
	}
	return fmt.Sprintf("%.1f%%", p)
}

func humanCount(n int64) string {
	if n < 0 {
		n = 0
	}
	if n < 1000 {
		return strconv.FormatInt(n, 10)
	}
	units := []string{"K", "M", "B", "T"}
	v := float64(n)
	u := -1
	for v >= 1000 && u < len(units)-1 {
		v /= 1000
		u++
	}
	return fmt.Sprintf("%.1f%s", v, units[u])
}

func clipLabel(s string, width int) string {
	if strings.Contains(s, "<U+") {
		return clipKeepTail(s, width)
	}
	return clipCells(s, width)
}

func clipKeepTail(s string, width int) string {
	if width < 1 {
		return ""
	}
	if uniseg.StringWidth(s) <= width {
		return s
	}
	ell := ui.G().Ellipsis
	if ell == "" {
		ell = "..."
	}
	budget := width - uniseg.StringWidth(ell)
	if budget < 1 {
		return clipCells(s, width)
	}
	gr := uniseg.NewGraphemes(s)
	var gs []string
	for gr.Next() {
		gs = append(gs, gr.Str())
	}
	w := 0
	i := len(gs)
	for i > 0 {
		gw := uniseg.StringWidth(gs[i-1])
		if w+gw > budget {
			break
		}
		w += gw
		i--
	}
	return ell + strings.Join(gs[i:], "")
}

func padPlain(s string, width int) string {
	if width < 1 {
		return ""
	}
	s = clipCells(s, width)
	gap := width - uniseg.StringWidth(s)
	if gap > 0 {
		s += strings.Repeat(" ", gap)
	}
	return s
}

func fitRight(left, right string, width int) string {
	if right == "" {
		return clipCells(left, width)
	}
	lw := uniseg.StringWidth(left)
	rw := uniseg.StringWidth(right)
	if lw+1+rw <= width {
		return left + strings.Repeat(" ", width-lw-rw) + right
	}
	room := width - rw - 1
	if room < 4 {
		return clipCells(left, width)
	}
	return clipCells(left, room) + " " + right
}

func (t Model) groupAt(gidx int) *model.Group {
	if t.m == nil || gidx < 0 || gidx >= len(t.m.Groups) {
		return nil
	}
	return t.m.Groups[gidx]
}

func (t Model) attnShort(g *model.Group) string {
	if g == nil || g.Kind != model.KindLayer || t.lay == nil || t.lay.info == nil {
		return ""
	}
	info, ok := t.lay.info[g.Layer]
	if !ok || info.Short == "" || info.Short == "-" {
		return ""
	}
	return t.lay.once(info.Short).String()
}

func (t Model) logicalDiamond(g *model.Group, lt model.LogicalTensor) bool {
	if t.lay == nil || len(t.lay.diamT) == 0 {
		return false
	}
	if lt.FullName != "" && t.lay.diamT[lt.FullName] {
		return true
	}
	if g == nil {
		return false
	}
	for _, tensor := range g.Tensors {
		if tensor == nil || !t.lay.diamT[tensor.Name] {
			continue
		}
		if tensor.Name == lt.FullName || strings.HasPrefix(tensor.Name, lt.FullName+".") {
			return true
		}
	}
	return false
}

func (t Model) subDiamond(row lrow) bool {
	g := t.groupAt(row.gidx)
	if t.lay == nil {
		return false
	}
	for _, p := range t.lay.packs(t.m, row.gidx) {
		if p.sub != row.sub {
			continue
		}
		for _, lt := range p.rows {
			if t.logicalDiamond(g, lt) {
				return true
			}
		}
	}
	return false
}

func precisionMark() string {
	if ui.UTF8() {
		return "◆"
	}
	return "*"
}

func (t Model) layersSearchRow(w int) string {
	s := t.lay
	shown := sanitize.From(s.query).String()
	mode := "search"
	if s.filter {
		mode = "filter"
	}
	hint := mode + cfgSep() + cfgEnter() + " keep" + cfgSep() + "tab switch"
	if s.largeWait {
		n := 0
		if t.m != nil {
			n = len(t.m.Tensors)
		}
		hint = fmt.Sprintf("%s to search %s tensors", cfgEnter(), commaInt(n))
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

func (t Model) layersJumpRow(w int) string {
	q := ""
	if t.lay != nil {
		q = t.lay.jumpQ
	}
	text := " jump: #" + q
	if ui.ColorOff() {
		return clipCells(text, w)
	}
	return ui.PaintToken(ui.Fit(text, w), ui.TextPrimary, &ui.SurfaceSeg, false)
}

func (t Model) layerSummaryLine(w int) string {
	text := t.layerSummaryText()
	if ui.ColorOff() {
		return clipCells(" "+text, w)
	}
	return ui.PaintToken(ui.Fit(" "+text, w), ui.TextPrimary, &ui.SurfaceSeg, false)
}

func (t Model) layerSummaryText() string {
	if t.lay == nil || len(t.lay.rows) == 0 {
		return "nothing selected"
	}
	row := t.currentLayer()
	gl := ui.G()
	label := t.rowLabel(row)
	bytes := model.HumanBytes(t.rowBytes(row))
	dtype, typ := t.rowDtypeType(row)
	var parts []string
	parts = append(parts, label)
	if typ != "" {
		parts = append(parts, typ)
	}
	parts = append(parts, bytes)
	if dtype != "" {
		parts = append(parts, "["+dtype+"]")
	}
	return strings.Join(parts, " "+gl.Dot+" ")
}

func (t Model) layerDetailLines(x, y, w, h int, focused bool) []string {
	_ = x
	_ = y
	bodyH := h - 1
	if bodyH < 1 {
		bodyH = 1
	}
	inner := innerWidth(w)
	body := t.detailBody(inner)
	for len(body) < bodyH {
		body = append(body, "")
	}
	if len(body) > bodyH {
		body = body[:bodyH]
	}
	return chrome(ui.L("DETAIL"), t.detailRight(), sanitize.Text{}, body, w, h, focused)
}

func (t Model) selectedLayerKey() string {
	g := t.layersGroup()
	if g == nil || t.lay == nil {
		return ""
	}
	row := t.currentLayer()
	if row.kind == rowGroup && !t.lay.isOpen(row.path) {
		return ""
	}
	return t.layerGroupLabel(g).String()
}

func (t Model) detailRight() sanitize.Text {
	row := t.currentLayer()
	if row.kind == rowTensor {
		return ui.L("tensor")
	}
	if row.kind == rowSub {
		return subLabel(row.sub)
	}
	if row.path == "dec" {
		return ui.L("layers")
	}
	g := t.layersGroup()
	if g == nil {
		return sanitize.Text{}
	}
	return sanitize.Program(model.HumanBytes(g.Bytes))
}

func (t Model) detailBody(width int) []string {
	if t.lay == nil || t.m == nil || len(t.lay.rows) == 0 {
		return []string{ui.L("Select a group.").String()}
	}
	row := t.currentLayer()
	if row.kind == rowMore {
		return []string{t.moreText(crow{more: row.more, lead: row.lead, hidden: row.hidden, hideN: row.hideN})}
	}
	var lines []string
	crumb := t.layerCrumb(row)
	if crumb != "" {
		lines = append(lines, clipCells(crumb, width))
		lines = append(lines, "")
	}
	switch {
	case row.kind == rowTensor:
		lines = append(lines, t.tensorDetail(row, width)...)
	case row.path == "dec" || row.kind == rowGroup:
		lines = append(lines, t.groupDetail(row, width)...)
	default:
		lines = append(lines, t.subDetail(row, width)...)
	}
	for i := range lines {
		if strings.Contains(lines[i], "\x1b") {
			lines[i] = clipANSI(lines[i], width)
		} else {
			lines[i] = clipCells(lines[i], width)
		}
	}
	return lines
}

func (t Model) layerCrumb(row lrow) string {
	if t.lay == nil {
		return ""
	}
	var chain []lrow
	cur := row
	seen := map[string]bool{}
	for cur.path != "" && !seen[cur.path] {
		seen[cur.path] = true
		chain = append(chain, cur)
		if cur.parent == "" {
			break
		}
		next := lrow{}
		found := false
		for _, r := range t.lay.rows {
			if r.path == cur.parent {
				next = r
				found = true
				break
			}
		}
		if !found {
			break
		}
		cur = next
	}
	gl := ui.G()
	var parts []string
	for i := len(chain) - 1; i >= 0; i-- {
		label := t.rowLabel(chain[i])
		if label != "" {
			parts = append(parts, label)
		}
	}
	return strings.Join(parts, " "+gl.Arrow+" ")
}

func (t Model) tensorDetail(row lrow, width int) []string {
	g := t.groupAt(row.gidx)
	lt, ok := t.layerTensor(row)
	if !ok {
		return []string{ui.L("Select a tensor.").String()}
	}
	var lines []string
	lines = append(lines, t.lay.once(lt.FullName).String())
	lines = append(lines, "")
	role := t.tensorDtypeText(lt)
	if role == "" {
		role = "plain"
	}
	mode := ""
	if t.m.Quant != nil && t.m.Quant.Mode != "" && strings.HasPrefix(lt.Role, "q") && lt.Role != "qscal" && lt.Role != "qbias" {
		mode = " " + t.lay.once(t.m.Quant.Mode).String()
	}
	lines = append(lines, ui.Title(ui.S(ui.L("ROLE")), ui.S(sanitize.Program(role+mode)), width))
	lines = append(lines, ui.Title(ui.S(ui.L("LOGICAL")), ui.S(sanitize.Program(shapeLimited(lt.Shape)+"  "+humanCount(lt.Params)+" params")), width))
	packed := strings.ToUpper(lt.DType) + " " + shapeLimited(lt.Shape)
	lines = append(lines, ui.Title(ui.S(ui.L("PACKED")), ui.S(sanitize.Program(packed)), width))
	if g != nil && g.ExpertStacked() {
		if n, ok := g.ExpertCount(); ok && n > 0 && lt.Params > 0 {
			each := lt.Params / int64(n)
			lines = append(lines, ui.Title(ui.S(ui.L("EXPERTS")), ui.S(sanitize.Program(strconv.Itoa(n)+"  "+humanCount(each)+" params")), width))
		}
	}
	parts := t.storedParts(g, lt)
	if len(parts) > 0 {
		lines = append(lines, "", ui.Title(ui.S(ui.L("STORED PARTS")), ui.S(sanitize.Text{}), width))
		var sum int64
		for _, p := range parts {
			n := p.ByteLen()
			if n < 0 {
				n = 0
			}
			sum += n
		}
		for _, p := range parts {
			n := p.ByteLen()
			if n < 0 {
				n = 0
			}
			frac := 0.0
			if sum > 0 {
				frac = float64(n) / float64(sum)
			}
			bar, _ := ui.Meter(frac, 6, ui.GradMeter)
			name := p.Name
			if i := strings.LastIndex(name, "."); i >= 0 {
				name = name[i:]
			}
			lines = append(lines, clipANSI(t.lay.once(name).String()+"  "+strings.ToUpper(p.DType)+"  "+bar+"  "+model.HumanBytes(n), width))
		}
	}
	if lt.Shard != "" || lt.AbsOK {
		lines = append(lines, "", ui.Title(ui.S(ui.L("LOCATION")), ui.S(sanitize.Text{}), width))
		if lt.Shard != "" {
			lines = append(lines, t.lay.once(lt.Shard).String())
		}
		if lt.AbsOK {
			start := lt.DataStart + lt.OffStart
			end := lt.DataStart + lt.OffEnd
			lines = append(lines, sanitize.Program(strconv.FormatInt(start, 10)+" – "+strconv.FormatInt(end, 10)).String())
		}
	}
	lines = append(lines, "")
	lines = append(lines, t.shareBlock(row, lt.Bytes, width)...)
	return lines
}

func (t Model) storedParts(g *model.Group, lt model.LogicalTensor) []*model.Tensor {
	if g == nil {
		return nil
	}
	var out []*model.Tensor
	for _, tensor := range g.Tensors {
		if tensor == nil {
			continue
		}
		if tensor.Name == lt.FullName || tensor.Name == lt.Name || strings.HasPrefix(tensor.Name, lt.FullName+".") {
			out = append(out, tensor)
		}
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func (t Model) shareBlock(row lrow, bytes int64, width int) []string {
	parent := int64(0)
	if row.parent != "" && t.lay != nil {
		for _, r := range t.lay.rows {
			if r.path == row.parent {
				parent = t.rowBytes(r)
				break
			}
		}
	}
	modelBytes := int64(0)
	if t.m != nil {
		modelBytes = t.m.TensorBytes
	}
	barW := 14
	if width < 40 {
		barW = 8
	}
	var lines []string
	lines = append(lines, ui.Title(ui.S(ui.L("SHARE")), ui.S(sanitize.Text{}), width))
	lines = append(lines, t.shareLine(bytes, parent, "parent", barW))
	lines = append(lines, t.shareLine(bytes, modelBytes, "model", barW))
	return lines
}

func (t Model) shareLine(part, total int64, which string, barW int) string {
	frac := 0.0
	if total > 0 && part > 0 {
		frac = float64(part) / float64(total)
	}
	bar, _ := ui.Meter(frac, barW, ui.GradMeter)
	return bar + "  " + shareText(part, total) + " of " + which
}

func (t Model) groupDetail(row lrow, width int) []string {
	var lines []string
	if row.path == "dec" {
		lin, full := t.attnCounts()
		lines = append(lines, ui.Title(ui.S(ui.L("LAYERS")), ui.S(sanitize.Program(strconv.Itoa(t.layerCount())+"  "+strconv.Itoa(lin)+" linear  "+strconv.Itoa(full)+" full")), width))
		lines = append(lines, ui.Title(ui.S(ui.L("BYTES")), ui.S(sanitize.Program(model.HumanBytes(t.rowBytes(row)))), width))
		lines = append(lines, ui.Title(ui.S(ui.L("PARAMS")), ui.S(sanitize.Program(humanCount(t.rowParams(row)))), width))
		lines = append(lines, "", ui.L("top layers").String())
		lines = append(lines, t.topChildren(row, width)...)
		return lines
	}
	g := t.groupAt(row.gidx)
	if g == nil {
		return []string{ui.L("Select a group.").String()}
	}
	if g.Kind == model.KindLayer {
		attn := t.attnShort(g)
		if attn == "" {
			attn = "unknown"
		}
		lines = append(lines, ui.Title(ui.S(ui.L("ATTENTION")), ui.S(sanitize.Program(attn)), width))
		if n := t.expertN(g); n > 0 {
			per := 0
			if t.lay != nil {
				per = t.lay.perTok
			}
			text := strconv.Itoa(n)
			if per > 0 {
				text = expertJoin(strconv.Itoa(n), strconv.Itoa(per)) + "/tok"
			}
			lines = append(lines, ui.Title(ui.S(ui.L("EXPERTS")), ui.S(sanitize.Program(text)), width))
		}
	}
	lines = append(lines,
		ui.Title(ui.S(ui.L("TENSORS")), ui.S(sanitize.Program(strconv.Itoa(len(g.Tensors)))), width),
		ui.Title(ui.S(ui.L("BYTES")), ui.S(sanitize.Program(model.HumanBytes(g.Bytes))), width),
		ui.Title(ui.S(ui.L("PARAMS")), ui.S(sanitize.Program(humanCount(g.LogicalParams))), width),
	)
	if dtype := t.groupDtypeText(g); dtype != "" {
		lines = append(lines, ui.Title(ui.S(ui.L("DTYPE")), ui.S(t.lay.once(dtype)), width))
	}
	if t.groupDiamond(g) {
		lines = append(lines, "", ui.L("Precision differs from the checkpoint quant.").String())
	}
	lines = append(lines, "", ui.L("contains").String())
	lines = append(lines, t.topChildren(row, width)...)
	return lines
}

func (t Model) subDetail(row lrow, width int) []string {
	var lines []string
	lines = append(lines, ui.Title(ui.S(ui.L("SUBLAYER")), ui.S(subLabel(row.sub)), width))
	lines = append(lines, ui.Title(ui.S(ui.L("BYTES")), ui.S(sanitize.Program(model.HumanBytes(t.rowBytes(row)))), width))
	if t.subDiamond(row) {
		lines = append(lines, "", ui.L("Precision differs from the checkpoint quant.").String())
	}
	lines = append(lines, "", ui.L("contains").String())
	lines = append(lines, t.topChildren(row, width)...)
	return lines
}

func (t Model) topChildren(row lrow, width int) []string {
	var out []string
	if t.lay == nil {
		return out
	}
	n := 0
	for _, r := range t.lay.rows {
		if r.parent != row.path || r.kind == rowMore {
			continue
		}
		if !t.lay.isOpen(row.path) {
			break
		}
		out = append(out, clipCells(t.rowLabel(r)+"  "+model.HumanBytes(t.rowBytes(r)), width))
		n++
		if n >= 3 {
			break
		}
	}
	if n == 0 {
		g := t.groupAt(row.gidx)
		if g != nil && len(g.Tensors) > 64 && row.kind == rowGroup && !t.lay.isOpen(row.path) {
			return []string{sanitize.Program(commaInt(len(g.Tensors)) + " tensors").String()}
		}
		if g != nil && (row.kind != rowGroup || len(g.Tensors) <= 64 || t.lay.isOpen(row.path)) {
			shown := 0
			for _, p := range t.lay.packs(t.m, row.gidx) {
				if row.kind == rowSub && p.sub != row.sub {
					continue
				}
				for _, lt := range p.rows {
					out = append(out, clipCells(t.layerTensorLabel(g, lt).String(), width))
					shown++
					if shown >= 3 {
						return out
					}
				}
			}
		}
	}
	if len(out) == 0 {
		return []string{ui.L("Open this row to list its children.").String()}
	}
	return out
}

func (t Model) detailTitle(row lrow, g *model.Group) string {
	switch row.kind {
	case rowTensor:
		if lt, ok := t.layerTensor(row); ok {
			return t.layerTensorLabel(g, lt).String()
		}
	case rowSub:
		return subLabel(row.sub).String()
	default:
		if row.path == "dec" {
			return "decoder layers " + strconv.Itoa(t.layerCount())
		}
		return t.layerGroupLabel(g).String()
	}
	return ""
}
