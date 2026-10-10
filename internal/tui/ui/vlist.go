package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// VList is a window over a row slice. Only the visible rows are rendered.
type VList struct {
	Rows   []string
	Cursor int
	Offset int
	Height int
}

// Clamp keeps the cursor and offset inside the list.
func (v *VList) Clamp() {
	if v.Height < 1 {
		v.Height = 1
	}
	if v.Cursor < 0 {
		v.Cursor = 0
	}
	if n := len(v.Rows); v.Cursor >= n && n > 0 {
		v.Cursor = n - 1
	}
	if v.Offset < 0 {
		v.Offset = 0
	}
	if v.Cursor < v.Offset {
		v.Offset = v.Cursor
	}
	if v.Cursor >= v.Offset+v.Height {
		v.Offset = v.Cursor - v.Height + 1
	}
}

// Move shifts the cursor by d rows.
func (v *VList) Move(d int) {
	v.Cursor += d
	v.Clamp()
}

// Page moves by the window height.
func (v *VList) Page(d int) {
	step := v.Height
	if step < 1 {
		step = 1
	}
	v.Move(d * step)
}

// View renders Height lines. focused selects with the violet fill.
func (v *VList) View(width int, focused bool) string {
	v.Clamp()
	gl := G()
	var lines []string
	for i := 0; i < v.Height; i++ {
		idx := v.Offset + i
		var text string
		if idx >= 0 && idx < len(v.Rows) {
			text = v.Rows[idx]
		}
		sel := idx == v.Cursor && len(v.Rows) > 0
		lines = append(lines, rowLine(text, width, sel, focused, gl))
	}
	return strings.Join(lines, "\n")
}

func rowLine(text string, width int, selected, focused bool, gl Glyphs) string {
	gutter := " "
	if gl.Edge != "" {
		gutter = " "
	}
	switch {
	case selected && focused && ColorOff():
		gutter = gl.Selected
	case selected && !focused && ColorOff():
		gutter = "."
		if !UTF8() {
			gutter = "."
		}
	case selected && focused && Profile() == "ansi16":
		gutter = gl.Edge
		if gutter == "" {
			gutter = ">"
		}
	case selected && focused:
		gutter = gl.Edge
		if gutter == "" {
			gutter = " "
		}
	}
	inner := width - Width(gutter)
	if inner < 1 {
		inner = 1
	}
	body := PadLabel(Strip(text), inner)
	if ColorOff() {
		return Fit(gutter+body, width)
	}
	if Profile() == "ansi16" && selected && focused {
		return Fit(lipgloss.NewStyle().Reverse(true).Render(gutter+body), width)
	}
	bg := ""
	fg := TextPrimary.Hex()
	if selected && focused {
		bg = SurfaceSel.Hex()
	} else if selected {
		bg = SurfaceSeg.Hex()
	}
	if bg == "" {
		return Fit(body, width)
	}
	edge := ""
	if selected && focused && gl.Edge != "" {
		edge = paintHex(gl.Edge, Accent.Hex(), bg, false)
		body = PadLabel(Strip(text), inner)
	}
	return Fit(edge+paintHex(body, fg, bg, selected && focused), width)
}

// TreeRow draws an indented guide and a label.
func TreeRow(depth int, last, expanded bool, label string, width int) string {
	gl := G()
	var b strings.Builder
	for i := 0; i < depth; i++ {
		if UTF8() {
			b.WriteString("│ ")
		} else {
			b.WriteString("| ")
		}
	}
	if depth > 0 || last || expanded {
		switch {
		case !UTF8() && last:
			b.WriteString("+- ")
		case !UTF8():
			b.WriteString("|- ")
		case last:
			b.WriteString("└ ")
		default:
			b.WriteString("├ ")
		}
	}
	if expanded {
		if UTF8() {
			b.WriteString("▾ ")
		} else {
			b.WriteString("- ")
		}
	} else if depth >= 0 {
		if UTF8() {
			b.WriteString("▸ ")
		} else {
			b.WriteString("> ")
		}
	}
	b.WriteString(label)
	_ = gl
	return Fit(b.String(), width)
}

func lipglossPaint(ch, ansi string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(ansi)).Render(ch)
}
