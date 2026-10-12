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

func nodeLimitText(err error) string {
	lim, _ := err.(*jsonx.NodeLimitError)
	if lim == nil {
		return "Tree disabled."
	}
	return fmt.Sprintf("Tree disabled: %s nodes (limit %s). Key fields are still read from the parsed config. r shows the first 1 MiB of the raw file (sanitized).",
		commaInt(lim.Nodes), commaInt(lim.Limit))
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
