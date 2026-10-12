package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

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

// PadLabel pads or cuts label to n cells.
func PadLabel(label string, n int) string {
	if Width(label) >= n {
		return Trunc(label, n, "")
	}
	return label + strings.Repeat(" ", n-Width(label))
}
