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

// TileSpec is one KPI tile.
type TileSpec struct {
	Label sanitize.Text
	Value sanitize.Text
	Sub   sanitize.Text
	Edge  string // hex
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
