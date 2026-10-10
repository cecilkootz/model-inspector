package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Breakpoint is XS, S, M, or L.
func Breakpoint(w, h int) string {
	if w < 60 || h < 16 {
		return "XS"
	}
	if w >= 100 && h >= 30 {
		if w >= 140 && h >= 36 {
			return "L"
		}
		return "M"
	}
	return "S"
}

// Rect is an inclusive-origin cell rectangle.
type Rect struct {
	X, Y, W, H int
}

// Zone is a mouse hit target.
type Zone struct {
	ID string
	R  Rect
}

// Zones is the hit map for one frame.
type Zones struct {
	items []Zone
}

// Add records a zone. Later zones win on overlap.
func (z *Zones) Add(id string, r Rect) {
	if z == nil || r.W <= 0 || r.H <= 0 {
		return
	}
	z.items = append(z.items, Zone{ID: id, R: r})
}

// Hit returns the top-most zone containing x, y.
func (z Zones) Hit(x, y int) (Zone, bool) {
	for i := len(z.items) - 1; i >= 0; i-- {
		r := z.items[i].R
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			return z.items[i], true
		}
	}
	return Zone{}, false
}

// Reset drops every zone.
func (z *Zones) Reset() {
	if z != nil {
		z.items = z.items[:0]
	}
}

// Rects is the chrome and pane geometry for one frame.
type Rects struct {
	Break     string
	Body      Rect
	Primary   Rect
	Secondary Rect
	Split     bool
	Gutter    int
}

// Layout computes pane rectangles. ratio is the primary share in percent.
// zoom gives the focused pane the whole body. The 2-cell gutter is not in either pane.
func Layout(w, h, ratio int, zoom, focusSecondary bool) Rects {
	br := Breakpoint(w, h)
	bodyH := h - 3
	if bodyH < 1 {
		bodyH = 1
	}
	body := Rect{X: 0, Y: 2, W: w, H: bodyH}
	out := Rects{Break: br, Body: body, Primary: body}
	if br != "M" && br != "L" || zoom {
		return out
	}
	if ratio < 30 {
		ratio = 30
	}
	if ratio > 70 {
		ratio = 70
	}
	gutter := 2
	inner := w - gutter
	if inner < 2 {
		return out
	}
	pw := inner * ratio / 100
	if pw < 1 {
		pw = 1
	}
	if pw > inner-1 {
		pw = inner - 1
	}
	out.Split = true
	out.Gutter = gutter
	out.Primary = Rect{X: 0, Y: 2, W: pw, H: bodyH}
	out.Secondary = Rect{X: pw + gutter, Y: 2, W: w - pw - gutter, H: bodyH}
	if focusSecondary && zoom {
		out.Primary = body
		out.Secondary = Rect{}
		out.Split = false
	}
	return out
}

// Width is the display width of s.
func Width(s string) int { return ansi.StringWidth(s) }

// Strip removes ANSI sequences.
func Strip(s string) string { return ansi.Strip(s) }

// Trunc cuts s to width cells and appends tail when it cuts.
func Trunc(s string, width int, tail string) string {
	if width <= 0 {
		return ""
	}
	if Width(s) <= width {
		return s
	}
	return ansi.Truncate(Strip(s), width, tail)
}

// MiddleTrunc keeps the tail of s.
func MiddleTrunc(s string, width int, ell string) string {
	if width <= 0 {
		return ""
	}
	if Width(s) <= width {
		return s
	}
	if ell == "" {
		ell = "..."
	}
	ew := Width(ell)
	if width <= ew {
		return Trunc(s, width, "")
	}
	plain := Strip(s)
	budget := width - ew
	runes := []rune(plain)
	lo := 0
	for lo < len(runes) && Width(string(runes[lo:])) > budget {
		lo++
	}
	return ell + string(runes[lo:])
}

// Fit pads or cuts s to exactly width cells.
func Fit(s string, width int) string {
	if width < 1 {
		return ""
	}
	if strings.Contains(s, "\x1b") && lipgloss.Width(s) > width {
		s = Strip(s)
	}
	w := lipgloss.Width(s)
	switch {
	case w == width:
		return s
	case w < width:
		return s + strings.Repeat(" ", width-w)
	default:
		ell := G().Ellipsis
		if ell == "" {
			ell = "..."
		}
		cut := Trunc(s, width, "")
		if Width(cut) > width {
			cut = Trunc(cut, width, "")
		}
		// Prefer a clean cut with no dangling escape.
		if lipgloss.Width(cut) > width {
			cut = ansi.Truncate(Strip(s), width, "")
		}
		if lipgloss.Width(cut) < width {
			cut += strings.Repeat(" ", width-lipgloss.Width(cut))
		}
		return cut
	}
}

// Frame makes s exactly width by height lines.
func Frame(s string, width, height int) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	lines := strings.Split(s, "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, ln := range lines {
		lines[i] = Fit(ln, width)
	}
	return strings.Join(lines, "\n")
}

// PadLabel pads or cuts label to n cells.
func PadLabel(label string, n int) string {
	if Width(label) >= n {
		return Trunc(label, n, "")
	}
	return label + strings.Repeat(" ", n-Width(label))
}
