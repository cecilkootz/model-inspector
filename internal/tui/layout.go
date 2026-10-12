package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// frame makes s exactly width by height. Over-long lines are cut.
// ANSI sequences are stripped before a cut so a truncated line cannot
// contain a broken escape.
func frame(s string, width, height int) string {
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
		lines[i] = fitLine(ln, width)
	}
	return strings.Join(lines, "\n")
}

func fitLine(s string, width int) string {
	if strings.Contains(s, "\x1b") && lipgloss.Width(s) > width {
		s = stripANSI(s)
	}
	w := lipgloss.Width(s)
	switch {
	case w == width:
		return s
	case w < width:
		n := width - w
		if n < 0 {
			n = 0
		}
		return s + strings.Repeat(" ", n)
	default:
		return truncWidth(s, width)
	}
}

func truncWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	ell := g.ellipsis
	if g.ellipsis == "" {
		ell = "..."
	}
	ew := lipgloss.Width(ell)
	if width <= ew {
		return truncRunes(ell, width)
	}
	budget := width - ew
	var b strings.Builder
	for _, r := range s {
		next := b.String() + string(r)
		if lipgloss.Width(next) > budget {
			break
		}
		b.WriteRune(r)
	}
	return b.String() + ell
}

func truncRunes(s string, width int) string {
	var b strings.Builder
	for _, r := range s {
		next := b.String() + string(r)
		if lipgloss.Width(next) > width {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

func clipLines(s string, n, width int) string {
	if n < 1 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	for len(lines) < n {
		lines = append(lines, "")
	}
	for i, ln := range lines {
		lines[i] = fitLine(ln, width)
	}
	return strings.Join(lines, "\n")
}

func stripANSI(s string) string { return ansi.Strip(s) }

func clampInt(v, low, high int) int {
	return min(max(v, low), high)
}
