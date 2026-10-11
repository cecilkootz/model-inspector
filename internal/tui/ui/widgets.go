package ui

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/cecilkootz/model-inspector/internal/sanitize"
)

func paint(s string, fg, bg lipgloss.TerminalColor, bold bool) string {
	if ColorOff() || s == "" {
		return s
	}
	st := lipgloss.NewStyle()
	if fg != nil {
		st = st.Foreground(fg)
	}
	if bg != nil && Profile() != "ansi16" {
		st = st.Background(bg)
	}
	if bold {
		st = st.Bold(true)
	}
	return st.Render(s)
}

// Paint colours s. Empty colours leave that channel alone.
func Paint(s, fg, bg string, bold bool) string { return paintHex(s, fg, bg, bold) }

// PaintToken colours s with semantic tokens. A nil background leaves the
// channel alone. ANSI-16 keeps the foreground and drops the fill.
func PaintToken(s string, fg Token, bg *Token, bold bool) string {
	if ColorOff() || s == "" {
		return s
	}
	if Profile() == "ansi16" {
		if fg.ANSI == "" {
			return s
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color(fg.ANSI)).Bold(bold).Render(s)
	}
	st := lipgloss.NewStyle().Bold(bold).Foreground(fg.Color())
	if bg != nil {
		st = st.Background(bg.Color())
	}
	return st.Render(s)
}

func paintHex(s, fg, bg string, bold bool) string {
	if ColorOff() || Profile() == "ansi16" {
		if Profile() == "ansi16" && fg != "" {
			return lipgloss.NewStyle().Foreground(lipgloss.Color(fg)).Bold(bold).Render(s)
		}
		return s
	}
	st := lipgloss.NewStyle().Bold(bold)
	if fg != "" {
		st = st.Foreground(lipgloss.Color(hexFor(fg)))
	}
	if bg != "" {
		st = st.Background(lipgloss.Color(hexFor(bg)))
	}
	return st.Render(s)
}

func hexFor(hex string) string {
	if Profile() == "ansi256" {
		if n, ok := ansi256Of(hex); ok {
			return n
		}
	}
	return hex
}

// Pill is a filled label. Colorless output is [label].
func Pill(kind FillKind, label sanitize.Text) string {
	text := label.String()
	if ColorOff() {
		return "[" + text + "]"
	}
	bg, fg := FillColors(kind)
	if Profile() == "ansi16" {
		st := lipgloss.NewStyle().Bold(true).Foreground(fg.Color())
		if bg.ANSI != "" {
			st = st.Background(bg.Color())
		}
		return st.Render(" " + text + " ")
	}
	return paint(" "+text+" ", fg.Color(), bg.Color(), true)
}

// Est marks an estimated figure. ASCII and colorless use (est).
func Est() string {
	g := G()
	if !UTF8() || ColorOff() {
		if !UTF8() {
			return g.Est
		}
	}
	if ColorOff() || !UTF8() {
		return "(est)"
	}
	return paint(g.Est, TextMuted.Color(), nil, false)
}

// Meter draws a gradient bar of width cells. frac outside [0,1] clamps,
// and values above 1 set overflow.
func Meter(frac float64, width int, g Gradient) (string, bool) {
	if width < 1 {
		return "", false
	}
	overflow := false
	if math.IsNaN(frac) || frac < 0 {
		frac = 0
	}
	if frac > 1 {
		overflow = true
		frac = 1
	}
	filled := frac * float64(width)
	n := int(math.Floor(filled))
	part := filled - float64(n)
	if n > width {
		n = width
		part = 0
	}
	gl := G()
	var b strings.Builder
	for i := 0; i < width; i++ {
		t := 0.0
		if width > 1 {
			t = float64(i) / float64(width-1)
		}
		col := string(g.At(t))
		switch {
		case i < n:
			b.WriteString(meterCell(gl.Bar[0], col, false))
		case i == n && part > 0:
			idx := int(part*8 + 0.5)
			if idx < 1 {
				idx = 1
			}
			if idx > 7 {
				idx = 7
			}
			b.WriteString(meterCell(gl.Eighth[idx], col, true))
		default:
			b.WriteString(meterCell(" ", "", true))
		}
	}
	s := b.String()
	if overflow {
		plain := Strip(s)
		if Width(plain) > width-1 {
			s = Trunc(plain, width-1, "") + ">"
		}
	}
	return Fit(s, width), overflow
}

func meterCell(ch, col string, track bool) string {
	if ch == "" {
		ch = " "
	}
	if ColorOff() {
		if track && ch == " " {
			return " "
		}
		return ch
	}
	if Profile() == "ansi16" {
		if track && (ch == " " || col == "") {
			return " "
		}
		return paintHex(ch, col, "", false)
	}
	if track && col == "" {
		return paintHex(" ", "", SurfaceSeg.Hex(), false)
	}
	if track {
		return paintHex(ch, col, SurfaceSeg.Hex(), false)
	}
	return paintHex(ch, col, "", false)
}

// Seg is one stacked-bar segment.
type Seg struct {
	Label Safe
	Frac  float64
	Grad  Gradient
	Flat  string // hex, used when Grad is empty
}

// Stacked draws a bar of height rows and a legend.
func Stacked(segs []Seg, width, height int) (rows []string, legend string) {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	cells := make([]int, len(segs))
	used := 0
	var largest int
	for i, s := range segs {
		if s.Frac > segs[largest].Frac {
			largest = i
		}
		n := 0
		if s.Frac > 0 {
			n = int(math.Round(s.Frac * float64(width)))
			if n < 1 {
				n = 1
			}
		}
		cells[i] = n
		used += n
	}
	for used > width {
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
	for used < width && len(cells) > 0 {
		cells[largest]++
		used++
	}
	gl := G()
	lines := make([]string, height)
	var b strings.Builder
	for i, s := range segs {
		n := cells[i]
		ch := gl.Bar[i%len(gl.Bar)]
		for c := 0; c < n; c++ {
			t := 0.0
			if n > 1 {
				t = float64(c) / float64(n-1)
			}
			col := s.Flat
			if s.Grad.ok {
				col = string(s.Grad.At(t))
			}
			if ColorOff() || Profile() == "ansi16" {
				if Profile() == "ansi16" && col != "" {
					b.WriteString(paintHex(ch, col, "", false))
				} else {
					b.WriteString(ch)
				}
			} else if ch != "" {
				b.WriteString(paintHex(ch, col, "", false))
			} else {
				b.WriteString(paintHex(" ", "", col, false))
			}
		}
	}
	row := Fit(b.String(), width)
	for i := range lines {
		lines[i] = row
	}
	var legendParts []string
	for i, s := range segs {
		sw := gl.Bar[i%len(gl.Bar)]
		if sw == "" {
			sw = "#"
		}
		pct := s.Frac * 100
		legendParts = append(legendParts, sw+" "+s.Label.String()+" "+formatPct(pct))
	}
	return lines, strings.Join(legendParts, "   ")
}

func formatPct(p float64) string {
	return strings.TrimRight(strings.TrimRight(sprintf1(p), "0"), ".") + "%"
}

func sprintf1(p float64) string {
	s := lipgloss.PlaceHorizontal(0, lipgloss.Left, "")
	_ = s
	return strings.TrimSpace(strings.ReplaceAll(
		strings.ReplaceAll(
			func() string {
				whole := int(math.Round(p * 10))
				return itoa(whole/10) + "." + itoa(whole%10)
			}(), "", ""), "", ""))
}

// itoa formats n without recursing. Negating MinInt does not terminate,
// so the absolute value is taken in unsigned two's complement.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	u := uint(n)
	if neg {
		u = ^u + 1
	}
	var buf [32]byte
	i := len(buf)
	for u > 0 {
		i--
		buf[i] = byte('0' + u%10)
		u /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// Title is a bold uppercase label plus a muted subtitle.
func Title(label, sub Safe, width int) string {
	lab := strings.ToUpper(label.String())
	line := lab
	if !sub.Empty() {
		line += "  " + sub.String()
	}
	if ColorOff() {
		return Fit(line, width)
	}
	left := paint(lab, TextPrimary.Color(), nil, true)
	if !sub.Empty() {
		left += "  " + paint(sub.String(), TextMuted.Color(), nil, false)
	}
	return Fit(left, width)
}

// KVRow is one label/value pair.
type KVRow struct {
	Label sanitize.Text
	Value sanitize.Text
}

// KV renders rows with a shared label column (cap 28) and at least two spaces.
func KV(rows []KVRow, width int) string {
	max := 0
	for _, r := range rows {
		w := Width(r.Label.String())
		if w > max {
			max = w
		}
	}
	if max > 28 {
		max = 28
	}
	var lines []string
	for _, r := range rows {
		lab := Trunc(r.Label.String(), max, "")
		lab = PadLabel(lab, max)
		val := r.Value.String()
		line := lab + "  " + val
		if ColorOff() {
			lines = append(lines, Fit(line, width))
			continue
		}
		styled := paint(lab, TextMuted.Color(), nil, false) + "  " + paint(val, TextPrimary.Color(), nil, true)
		lines = append(lines, Fit(styled, width))
	}
	return strings.Join(lines, "\n")
}

// Button is a modal button. The label includes its key.
func Button(keyName, label Safe, primary, enabled bool) string {
	text := strings.TrimSpace(keyName.String() + "  " + label.String())
	if ColorOff() {
		return "[ " + text + " ]"
	}
	if !enabled {
		return paint(" "+text+" ", TextMuted.Color(), SurfaceSeg.Color(), false)
	}
	if primary {
		return paint(" "+text+" ", TextInk.Color(), FillPink.Color(), true)
	}
	return paint(" "+text+" ", TextPrimary.Color(), SurfaceSeg2.Color(), false)
}

// StatusSeg is one status-bar segment. Higher Pri drops first.
type StatusSeg struct {
	Text   Safe
	Pri    int
	FG, BG Token
	Bold   bool
	Min    int
	Shrink bool
	Keep   bool
}

// Segments lays left and right groups into one line of width cells.
// The flexible gap sits between them. Higher priority numbers drop first.
// A Shrink segment middle-truncates down to Min before it drops.
func Segments(left, right []StatusSeg, width int) string {
	if width < 1 {
		return ""
	}
	sep := ""
	if ColorOff() {
		sep = " " + G().Sep + " "
	}
	type item struct {
		s     StatusSeg
		right bool
		dead  bool
	}
	items := make([]item, 0, len(left)+len(right))
	for _, s := range left {
		items = append(items, item{s: s})
	}
	for _, s := range right {
		items = append(items, item{s: s, right: true})
	}
	render := func(s StatusSeg, text string) string {
		if text == "" {
			return ""
		}
		if ColorOff() {
			return text
		}
		return paint(" "+text+" ", s.FG.Color(), s.BG.Color(), s.Bold)
	}
	measure := func() (int, []string) {
		out := make([]string, len(items))
		total := 0
		nLeft, nRight := 0, 0
		for i, it := range items {
			if it.dead || it.s.Text.Empty() {
				continue
			}
			out[i] = render(it.s, it.s.Text.String())
			w := Width(out[i])
			total += w
			if it.right {
				nRight++
			} else {
				nLeft++
			}
		}
		if ColorOff() {
			if nLeft > 1 {
				total += (nLeft - 1) * Width(sep)
			}
			if nRight > 1 {
				total += (nRight - 1) * Width(sep)
			}
		}
		return total, out
	}
	for {
		total, _ := measure()
		if total <= width {
			break
		}
		// Shrink the model-name segment before dropping it.
		shrunk := false
		for i := range items {
			it := &items[i]
			if it.dead || !it.s.Shrink {
				continue
			}
			min := it.s.Min
			if min < 4 {
				min = 4
			}
			if Width(it.s.Text.String()) > min {
				it.s.Text = Chrome(MiddleTrunc(it.s.Text.String(), Width(it.s.Text.String())-1, G().Ellipsis))
				shrunk = true
				break
			}
		}
		if shrunk {
			continue
		}
		best := -1
		for i, it := range items {
			if it.dead || it.s.Keep {
				continue
			}
			if best < 0 || it.s.Pri > items[best].s.Pri {
				best = i
			}
		}
		if best < 0 {
			break
		}
		items[best].dead = true
	}
	_, rendered := measure()
	var lparts, rparts []string
	for i, it := range items {
		if rendered[i] == "" {
			continue
		}
		if it.right {
			rparts = append(rparts, rendered[i])
		} else {
			lparts = append(lparts, rendered[i])
		}
	}
	join := func(parts []string) string {
		if len(parts) == 0 {
			return ""
		}
		if ColorOff() {
			return strings.Join(parts, sep)
		}
		return strings.Join(parts, "")
	}
	ls, rs := join(lparts), join(rparts)
	gap := width - Width(ls) - Width(rs)
	if gap < 1 {
		line := ls
		if rs != "" {
			if ls != "" && ColorOff() {
				line += sep
			}
			line += rs
		}
		return Fit(line, width)
	}
	gapStr := strings.Repeat(" ", gap)
	if !ColorOff() && Profile() != "ansi16" && SurfaceBar.ANSI != "" || (!ColorOff() && Profile() != "ansi16") {
		if Profile() != "ansi16" && !ColorOff() {
			gapStr = paint(gapStr, nil, SurfaceBar.Color(), false)
		}
	}
	if ls == "" {
		return Fit(gapStr+rs, width)
	}
	if rs == "" {
		return Fit(ls+gapStr, width)
	}
	return Fit(ls+gapStr+rs, width)
}

// CalloutSpec is one callout row.
type CalloutSpec struct {
	Kind   FillKind
	Tag    sanitize.Text
	Title  sanitize.Text
	Right  sanitize.Text
	Detail sanitize.Text
	// Wrap is the most lines the title may take. 0 or 1 truncates it.
	Wrap int
}

// Callout renders a pill, title, figure, and a muted detail line.
func Callout(c CalloutSpec, width int) []string {
	pill := Pill(c.Kind, c.Tag)
	title := c.Title.String()
	right := c.Right.String()
	var more []string
	if c.Wrap > 1 && right == "" {
		// One cell stays free after the title, as on an unwrapped row.
		if room := width - Width(pill) - 2; room >= 8 && Width(title) > room {
			parts := wrapWords(title, room)
			if len(parts) > c.Wrap {
				parts = parts[:c.Wrap]
				parts[c.Wrap-1] = Trunc(parts[c.Wrap-1]+" "+G().Ellipsis, room, G().Ellipsis)
			}
			title, more = parts[0], parts[1:]
		}
	}
	gap := width - Width(pill) - 1 - Width(title) - 1 - Width(right)
	if gap < 1 {
		title = Trunc(title, max(1, width-Width(pill)-Width(right)-3), G().Ellipsis)
		gap = width - Width(pill) - 1 - Width(title) - 1 - Width(right)
		if gap < 1 {
			gap = 1
		}
	}
	row := pill + " " + title + strings.Repeat(" ", gap) + right
	if !ColorOff() {
		row = pill + " " + paint(title, TextPrimary.Color(), nil, true) + strings.Repeat(" ", gap)
		if right != "" {
			row += paint(right, TextMuted.Color(), nil, false)
		}
	}
	out := []string{Fit(row, width)}
	indent := strings.Repeat(" ", Width(pill)+1)
	for _, m := range more {
		if !ColorOff() {
			m = paint(m, TextPrimary.Color(), nil, true)
		}
		out = append(out, Fit(indent+m, width))
	}
	if d := c.Detail.String(); d != "" {
		line := indent + d
		if !ColorOff() {
			line = indent + paint(d, TextMuted.Color(), nil, false)
		}
		out = append(out, Fit(line, width))
	}
	return out
}

// TileSpec is one KPI tile.
type TileSpec struct {
	Label sanitize.Text
	Value sanitize.Text
	Sub   sanitize.Text
	Edge  string // hex
}

// Tile renders one filled tile of width w and rows h (3 or 5).
func Tile(spec TileSpec, w, h int) []string {
	if w < 1 {
		w = 1
	}
	if h < 3 {
		h = 3
	}
	gl := G()
	edge := gl.Edge
	inner := w
	if edge != "" && !ColorOff() {
		inner = w - Width(edge)
		if inner < 1 {
			inner = 1
		}
	}
	label := spec.Label.String()
	value := spec.Value.String()
	sub := spec.Sub.String()
	lines := []string{label, value, sub}
	if h >= 5 {
		lines = []string{"", label, value, sub, ""}
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	var out []string
	for i, ln := range lines {
		body := Trunc(ln, inner, "")
		pad := inner - Width(body)
		if pad < 0 {
			pad = 0
		}
		styled := body + strings.Repeat(" ", pad)
		if !ColorOff() && Profile() != "ansi16" {
			fg := TextSecondary.Color()
			bold := false
			if (h >= 5 && i == 2) || (h < 5 && i == 1) {
				fg = TextPrimary.Color()
				bold = true
			} else if (h >= 5 && i == 1) || (h < 5 && i == 0) {
				fg = TextMuted.Color()
			}
			styled = paint(styled, fg, SurfaceCard.Color(), bold)
		}
		if edge != "" && Profile() != "colorless" && Profile() != "ansi16" {
			styled = paintHex(edge, spec.Edge, SurfaceCard.Hex(), false) + styled
		} else if edge != "" && !ColorOff() && Profile() == "ansi16" {
			styled = edge + styled
		}
		out = append(out, Fit(styled, w))
	}
	return out
}

// TileRow distributes tiles across width with one cell between them.
// Tiles narrower than 13 cells are dropped from the right.
func TileRow(tiles []TileSpec, width, height int) []string {
	if len(tiles) == 0 || width < 1 {
		return nil
	}
	n := len(tiles)
	for n > 1 {
		gap := n - 1
		tw := (width - gap) / n
		if tw >= 13 {
			break
		}
		n--
	}
	tiles = tiles[:n]
	gap := n - 1
	if gap < 0 {
		gap = 0
	}
	tw := (width - gap) / n
	if tw < 1 {
		tw = 1
	}
	extra := (width - gap) - tw*n
	rows := make([]string, height)
	for i, spec := range tiles {
		w := tw
		if i == n-1 {
			w += extra
		}
		col := Tile(spec, w, height)
		for r := 0; r < height && r < len(col); r++ {
			if i > 0 {
				rows[r] += " "
			}
			rows[r] += col[r]
		}
	}
	for i := range rows {
		rows[i] = Fit(rows[i], width)
	}
	return rows
}

// Panel renders a filled panel. Focus uses a violet title pill and an accent edge.
func Panel(title, right sanitize.Text, body []string, w, h int, focused bool) []string {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	gl := G()
	pad := 2
	if w < 40 {
		pad = 1
	}
	edge := gl.Edge
	inner := w - pad*2
	if edge != "" && !ColorOff() {
		inner = w - Width(edge) - pad*2
	}
	if inner < 1 {
		inner = 1
	}
	name := strings.ToUpper(title.String())
	var titleCell string
	switch {
	case ColorOff() && focused:
		titleCell = "[ " + name + " ]"
	case ColorOff():
		titleCell = name
	case focused && Profile() == "ansi16":
		titleCell = lipgloss.NewStyle().Reverse(true).Bold(true).Render(" " + name + " ")
	case focused:
		titleCell = paint(" "+name+" ", TextOnFill.Color(), FillViolet.Color(), true)
	default:
		titleCell = paint(name, TextPrimary.Color(), nil, true)
	}
	rightStr := right.String()
	gap := inner - Width(titleCell) - Width(rightStr)
	if gap < 1 {
		gap = 1
		rightStr = Trunc(rightStr, max(0, inner-Width(titleCell)-1), "")
		gap = inner - Width(titleCell) - Width(rightStr)
		if gap < 1 {
			gap = 1
		}
	}
	titleLine := titleCell + strings.Repeat(" ", gap) + rightStr
	if !ColorOff() && rightStr != "" {
		titleLine = titleCell + strings.Repeat(" ", gap) + paint(rightStr, TextMuted.Color(), nil, false)
	}
	var content []string
	content = append(content, titleLine)
	content = append(content, body...)
	var out []string
	for i := 0; i < h; i++ {
		var raw string
		if i < len(content) {
			raw = content[i]
		}
		raw = Trunc(Strip(raw), inner, "")
		// Keep styled title line as-is when it already fits.
		cell := contentLine(i, content, inner, pad)
		if edge != "" && !ColorOff() && Profile() != "ansi16" {
			col := SurfaceSeg.Hex()
			if focused {
				col = Accent.Hex()
			}
			cell = paintHex(edge, col, SurfaceCard.Hex(), false) + cell
		}
		out = append(out, Fit(cell, w))
	}
	return out
}

func contentLine(i int, content []string, inner, pad int) string {
	var raw string
	if i < len(content) {
		raw = content[i]
	}
	// Title line is already styled; other lines get card fill.
	if i == 0 {
		plain := Strip(raw)
		if Width(plain) > inner {
			raw = Trunc(plain, inner, "")
		}
		padL := strings.Repeat(" ", pad)
		padR := inner - Width(Strip(raw))
		if padR < 0 {
			padR = 0
		}
		line := padL + raw + strings.Repeat(" ", padR) + strings.Repeat(" ", pad)
		if !ColorOff() && Profile() != "ansi16" {
			// pad spaces take the card colour; the title keeps its own fill
			return paint(strings.Repeat(" ", pad), nil, SurfaceCard.Color(), false) + raw + paint(strings.Repeat(" ", padR+pad), nil, SurfaceCard.Color(), false)
		}
		return line
	}
	text := Trunc(Strip(raw), inner, G().Ellipsis)
	if !ColorOff() && Profile() != "ansi16" {
		body := PadLabel(text, inner)
		return paint(strings.Repeat(" ", pad)+body+strings.Repeat(" ", pad), TextSecondary.Color(), SurfaceCard.Color(), false)
	}
	return strings.Repeat(" ", pad) + PadLabel(text, inner) + strings.Repeat(" ", pad)
}

// ModalSpec is an overlay card.
type ModalSpec struct {
	Title   Safe
	Body    Safe
	Buttons Safe
	Width   int
}

// ModalLayout is where Modal placed the card inside the bgW by bgH scrim.
// ContentTop is the box-relative row of the first content line.
type ModalLayout struct {
	X, Y, W, H int
	ContentTop int
}

// Modal places a rounded card on a dot scrim of bgW by bgH.
func Modal(spec ModalSpec, bgW, bgH int) string {
	s, _ := modalRender(spec, bgW, bgH)
	return s
}

// ModalLayoutOf reports the card rectangle for the same inputs as Modal.
func ModalLayoutOf(spec ModalSpec, bgW, bgH int) ModalLayout {
	_, lay := modalRender(spec, bgW, bgH)
	return lay
}

func modalRender(spec ModalSpec, bgW, bgH int) (string, ModalLayout) {
	if bgW < 1 {
		bgW = 1
	}
	if bgH < 1 {
		bgH = 1
	}
	gl := G()
	boxW := spec.Width
	if boxW < 20 {
		boxW = 20
	}
	if boxW > bgW-2 {
		boxW = bgW - 2
	}
	if boxW < 10 {
		boxW = bgW
	}
	inner := boxW - 2
	if inner < 1 {
		inner = 1
	}
	// Padding(1,3) is inside the border, so the text column is inner-6.
	textW := inner - 6
	if textW < 8 {
		textW = inner
	}
	var lines []string
	lines = append(lines, spec.Title.String())
	lines = append(lines, "")
	for _, ln := range strings.Split(spec.Body.String(), "\n") {
		for Width(ln) > textW {
			lines = append(lines, Trunc(ln, textW, ""))
			ln = ""
			break
		}
		lines = append(lines, ln)
	}
	if !spec.Buttons.Empty() {
		lines = append(lines, "")
		btn := spec.Buttons.String()
		if Width(btn) < textW {
			btn = strings.Repeat(" ", textW-Width(btn)) + btn
		}
		lines = append(lines, btn)
	}
	contentTop := 1
	var boxed string
	if ColorOff() || !UTF8() {
		var b strings.Builder
		top := gl.TL + strings.Repeat(gl.H, inner) + gl.TR
		b.WriteString(Fit(top, boxW))
		for _, ln := range lines {
			b.WriteByte('\n')
			cell := gl.V + " " + PadLabel(Strip(ln), inner-2) + gl.V
			b.WriteString(Fit(cell, boxW))
		}
		b.WriteByte('\n')
		bot := gl.BL + strings.Repeat(gl.H, inner) + gl.BR
		b.WriteString(Fit(bot, boxW))
		boxed = b.String()
	} else {
		body := strings.Join(padModal(lines, textW), "\n")
		st := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Accent.Color()).
			BorderBackground(SurfaceCard.Color()).
			Background(SurfaceCard.Color()).
			Foreground(TextSecondary.Color()).
			Padding(1, 3).
			Width(textW)
		boxed = st.Render(body)
		contentTop = 2
	}
	linesN := strings.Count(boxed, "\n") + 1
	boxH := linesN
	if boxW < 1 {
		boxW = 1
	}
	x := (bgW - boxW) / 2
	y := (bgH - boxH) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	lay := ModalLayout{X: x, Y: y, W: boxW, H: boxH, ContentTop: contentTop}
	if ColorOff() || !UTF8() {
		return lipgloss.Place(bgW, bgH, lipgloss.Center, lipgloss.Center, boxed), lay
	}
	return lipgloss.Place(bgW, bgH, lipgloss.Center, lipgloss.Center, boxed,
		lipgloss.WithWhitespaceChars(gl.Dot+" "),
		lipgloss.WithWhitespaceForeground(SurfaceSeg.Color())), lay
}

func padModal(lines []string, w int) []string {
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = PadLabel(ln, w)
	}
	return out
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// wrapWords splits s at spaces into lines of at most width cells. A word
// wider than width is cut.
func wrapWords(s string, width int) []string {
	var lines []string
	cur := ""
	for _, w := range strings.Fields(s) {
		for Width(w) > width {
			if cur != "" {
				lines = append(lines, cur)
				cur = ""
			}
			cut := Trunc(w, width, "")
			lines = append(lines, cut)
			w = string([]rune(w)[len([]rune(cut)):])
		}
		switch {
		case cur == "":
			cur = w
		case Width(cur)+1+Width(w) > width:
			lines = append(lines, cur)
			cur = w
		default:
			cur += " " + w
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}
