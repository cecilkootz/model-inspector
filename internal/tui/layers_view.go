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
	ratio := t.layersRatio()
	rects := ui.Layout(w, t.height, ratio, t.zoom, t.focusSecondary)
	if !rects.Split {
		if t.zoom && t.focusSecondary && (br == "M" || br == "L") {
			return t.layerDetailLines(0, 2, w, h, true)
		}
		return t.layerTreeLines(0, 2, w, h, true)
	}
	left := t.layerTreeLines(rects.Primary.X, rects.Primary.Y, rects.Primary.W, rects.Primary.H, !t.focusSecondary)
	right := t.layerDetailLines(rects.Secondary.X, rects.Secondary.Y, rects.Secondary.W, rects.Secondary.H, t.focusSecondary)
	return zipCols(left, right, rects.Primary.W, rects.Secondary.W, w)
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
		return ui.L("GGUF parsing is coming. model-inspector read file sizes only."), title
	}
	if len(t.m.Tensors) == 0 {
		return ui.L("No tensors: this directory has no safetensors shards."), title
	}
	if len(t.m.Groups) == 0 {
		return ui.L("No groups in this model."), title
	}
	return sanitize.Text{}, title
}

func (t Model) layerTreeLines(x, y, w, h int, focused bool) []string {
	searchN := 0
	if t.lay.search {
		searchN = 1
	}
	paneH := h - searchN
	if paneH < 2 {
		paneH = 2
	}
	bodyH := paneH - 1
	if bodyH < 1 {
		bodyH = 1
	}
	t.lay.viewH = bodyH
	t.lay.ensure(t.m)
	t.clampLayers()
	inner := innerWidth(w)
	body := t.layerRows(inner, bodyH, focused, x, y, w)
	for len(body) < bodyH {
		body = append(body, "")
	}
	if len(body) > bodyH {
		body = body[:bodyH]
	}
	out := chrome(ui.L("LAYERS"), ui.L("s sort"), t.layersSubtitle(), body, w, paneH, focused)
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
		n = len(t.m.Groups)
	}
	return sanitize.Program(commaInt(n) + " groups")
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
		yy := y + len(out) // chrome title occupies y; first body line is y+1
		s.hits = append(s.hits, hit{id: fmt.Sprintf("lay:%d", i), x: x, y: yy, w: paneW, h: 1})
	}
	return out
}

func (t Model) paintLayerRow(row lrow, width int, selected, focused bool) string {
	plainW := width
	if ui.ColorOff() && plainW > 1 {
		// The colorless cursor is a leading cell, so the row itself is one shorter.
		plainW--
	}
	plain := t.layerPlain(row, plainW)
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
		if focused {
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

func (t Model) layerPlain(row lrow, width int) string {
	if row.more || row.hidden || row.kind == rowMore {
		return clipCells(t.moreText(crow{
			more: row.more, lead: row.lead, hidden: row.hidden, hideN: row.hideN, last: row.last,
		}), width)
	}
	container := row.kind == rowGroup || row.kind == rowSub
	open := container && t.lay.isOpen(row.path)
	guide := guideText(crow{depth: uint8(row.depth), anc: row.anc, last: row.last}, container, open)
	label, meta := t.layerLabelMeta(row)
	return fitRight(guide+label, meta, width)
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
	// Keep the marker column when the label is long.
	room := width - rw - 1
	if room < 4 {
		return clipCells(left, width)
	}
	return clipCells(left, room) + " " + right
}

func (t Model) layerLabelMeta(row lrow) (string, string) {
	switch row.kind {
	case rowGroup:
		return t.groupLabelMeta(row)
	case rowSub:
		return t.subLabelMeta(row)
	case rowTensor:
		return t.tensorLabelMeta(row)
	default:
		return "", ""
	}
}

func (t Model) groupLabelMeta(row lrow) (string, string) {
	g := t.groupAt(row.gidx)
	label := t.layerGroupLabel(g).String()
	if g == nil {
		return label, ""
	}
	var meta []string
	if s := t.attnShort(g); s != "" {
		meta = append(meta, s)
	}
	if n, ok := g.ExpertCount(); ok && n > 0 {
		meta = append(meta, strconv.Itoa(n)+" exp")
	}
	if d := t.layerDType(g).String(); d != "" {
		meta = append(meta, d)
	}
	if mark := t.ngramMark(g); mark != "" {
		meta = append(meta, mark)
	}
	if t.groupDiamond(g) {
		meta = append(meta, precisionMark())
	}
	meta = append(meta, model.HumanBytes(g.Bytes))
	return label, strings.Join(meta, cfgSep())
}

func (t Model) subLabelMeta(row lrow) (string, string) {
	label := subLabel(row.sub).String()
	packs := t.lay.packs(t.m, row.gidx)
	var rows []model.LogicalTensor
	for _, p := range packs {
		if p.sub == row.sub {
			rows = p.rows
			break
		}
	}
	var bytes int64
	diamond := false
	g := t.groupAt(row.gidx)
	for _, lt := range rows {
		bytes += lt.Bytes
		if t.logicalDiamond(g, lt) {
			diamond = true
		}
	}
	meta := []string{strconv.Itoa(len(rows))}
	if diamond {
		meta = append(meta, precisionMark())
	}
	meta = append(meta, model.HumanBytes(bytes))
	return label, strings.Join(meta, cfgSep())
}

func (t Model) tensorLabelMeta(row lrow) (string, string) {
	g := t.groupAt(row.gidx)
	lt, ok := t.layerTensor(row)
	if !ok {
		return "", ""
	}
	label := t.layerTensorLabel(g, lt).String()
	var meta []string
	if b := tensorBadge(lt); b != "" {
		meta = append(meta, t.lay.once(b).String())
	}
	if lt.Host {
		meta = append(meta, ui.G().Host)
	}
	if t.logicalDiamond(g, lt) {
		meta = append(meta, precisionMark())
	}
	meta = append(meta, model.HumanBytes(lt.Bytes))
	shape := shapeLimited(lt.Shape)
	meta = append(meta, shape)
	return label, strings.Join(meta, cfgSep())
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
	right := t.detailRight()
	return chrome(ui.L("DETAIL"), right, sanitize.Text{}, body, w, h, focused)
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
	g := t.layersGroup()
	var lines []string
	title := t.detailTitle(row, g)
	lines = append(lines, clipCells(title, width))
	if pills := t.detailPills(row, g); pills != "" {
		lines = append(lines, clipANSI(pills, width))
	}
	lines = append(lines, "")
	kv := ui.KV(t.detailKV(row, g), width)
	if kv != "" {
		lines = append(lines, strings.Split(kv, "\n")...)
	}
	if bar := t.detailMeter(g, width); bar != "" {
		lines = append(lines, "", clipANSI(bar, width))
	}
	if t.detailDiamond(row, g) {
		lines = append(lines, "", ui.L("Precision differs from the checkpoint quant.").String())
	}
	if prev := t.detailPreview(row, g, width); len(prev) > 0 {
		lines = append(lines, "", ui.L("contains").String())
		lines = append(lines, prev...)
	}
	for i := range lines {
		if !strings.Contains(lines[i], "\x1b") {
			lines[i] = clipCells(lines[i], width)
		} else {
			lines[i] = clipANSI(lines[i], width)
		}
	}
	return lines
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
		return t.layerGroupLabel(g).String()
	}
	return ""
}

func (t Model) detailPills(row lrow, g *model.Group) string {
	var parts []string
	switch row.kind {
	case rowTensor:
		lt, ok := t.layerTensor(row)
		if !ok {
			return ""
		}
		if b := tensorBadge(lt); b != "" {
			parts = append(parts, ui.Pill(ui.FillKindInfo, t.lay.once(b)))
		}
		if lt.Host {
			parts = append(parts, ui.Pill(ui.FillKindHost, ui.L("host")))
		}
		if t.logicalDiamond(g, lt) {
			parts = append(parts, ui.Pill(ui.FillKindWarn, sanitize.Program(precisionMark())))
		}
	default:
		if g == nil {
			return ""
		}
		if d := t.layerDType(g).String(); d != "" {
			parts = append(parts, ui.Pill(ui.FillKindInfo, t.layerDType(g)))
		}
		if t.ngramMark(g) != "" {
			parts = append(parts, ui.Pill(ui.FillKindHost, ui.L("host")))
		}
		if t.groupDiamond(g) && row.kind == rowGroup {
			parts = append(parts, ui.Pill(ui.FillKindWarn, sanitize.Program(precisionMark())))
		}
	}
	return strings.Join(parts, " ")
}

func (t Model) detailKV(row lrow, g *model.Group) []ui.KVRow {
	var rows []ui.KVRow
	switch row.kind {
	case rowTensor:
		lt, ok := t.layerTensor(row)
		if !ok {
			return nil
		}
		rows = append(rows,
			ui.KVRow{Label: ui.L("name"), Value: t.lay.once(lt.FullName)},
			ui.KVRow{Label: ui.L("shape"), Value: sanitize.Program(shapeLimited(lt.Shape))},
			ui.KVRow{Label: ui.L("bytes"), Value: sanitize.Program(model.HumanBytes(lt.Bytes))},
		)
		if lt.Shards > 1 {
			rows = append(rows, ui.KVRow{Label: ui.L("shards"), Value: sanitize.Program(ui.Times() + strconv.Itoa(lt.Shards))})
		}
		return rows
	case rowSub:
		rows = append(rows, ui.KVRow{Label: ui.L("sublayer"), Value: subLabel(row.sub)})
	}
	if g == nil {
		return rows
	}
	rows = append(rows,
		ui.KVRow{Label: ui.L("group"), Value: t.layerGroupLabel(g)},
		ui.KVRow{Label: ui.L("kind"), Value: t.lay.once(g.Kind)},
		ui.KVRow{Label: ui.L("tensors"), Value: sanitize.Program(strconv.Itoa(len(g.Tensors)))},
		ui.KVRow{Label: ui.L("bytes"), Value: sanitize.Program(model.HumanBytes(g.Bytes))},
	)
	if g.Kind == model.KindLayer {
		if s := t.attnShort(g); s != "" {
			rows = append(rows, ui.KVRow{Label: ui.L("attention"), Value: sanitize.Program(s)})
		}
		if n, ok := g.ExpertCount(); ok && n > 0 {
			how := "indexed"
			if g.ExpertStacked() {
				how = "stacked"
			}
			rows = append(rows, ui.KVRow{Label: ui.L("experts"), Value: sanitize.Program(strconv.Itoa(n) + " " + how)})
		}
	}
	return rows
}

func (t Model) detailMeter(g *model.Group, width int) string {
	if g == nil || t.m == nil || t.m.TotalSize <= 0 || width < 8 {
		return ""
	}
	frac := float64(g.Bytes) / float64(t.m.TotalSize)
	barW := width
	if barW > 24 {
		barW = 24
	}
	bar, _ := ui.Meter(frac, barW, ui.GradMeter)
	share := sanitize.Program(fmt.Sprintf("%.0f%%", frac*100)).String()
	return bar + "  " + share
}

func (t Model) detailDiamond(row lrow, g *model.Group) bool {
	if row.kind == rowTensor {
		lt, ok := t.layerTensor(row)
		return ok && t.logicalDiamond(g, lt)
	}
	if row.kind == rowSub {
		_, meta := t.subLabelMeta(row)
		return strings.Contains(meta, precisionMark())
	}
	return t.groupDiamond(g)
}

func (t Model) detailPreview(row lrow, g *model.Group, width int) []string {
	if g == nil || row.kind == rowTensor || t.lay == nil {
		return nil
	}
	var rows []model.LogicalTensor
	if row.kind == rowSub {
		for _, p := range t.lay.packs(t.m, row.gidx) {
			if p.sub == row.sub {
				rows = p.rows
				break
			}
		}
	} else if _, ok := t.lay.pack[row.gidx]; ok || len(g.Tensors) <= 64 {
		for _, p := range t.lay.packs(t.m, row.gidx) {
			rows = append(rows, p.rows...)
		}
	} else {
		return []string{sanitize.Program(commaInt(len(g.Tensors)) + " tensors").String()}
	}
	n := len(rows)
	if n > 6 {
		n = 6
	}
	var out []string
	for i := 0; i < n; i++ {
		name := t.layerTensorLabel(g, rows[i]).String()
		out = append(out, clipCells(name, width))
	}
	if len(rows) > n {
		out = append(out, sanitize.Program(commaInt(len(rows)-n)+" more").String())
	}
	return out
}
