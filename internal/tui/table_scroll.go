package tui

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
)

// stepTableY applies the same offset change bubbles' table.Update applies
// for msg. The table itself is updated by the caller; this only tracks the
// unexported viewport offset. Keys that the table ignores leave y unchanged.
func stepTableY(tbl table.Model, y int, msg tea.Msg) int {
	if !tbl.Focused() {
		return y
	}
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return y
	}
	km := tbl.KeyMap
	h := tbl.Height()
	switch {
	case key.Matches(keyMsg, km.LineUp):
		return moveUpY(tbl, y, 1)
	case key.Matches(keyMsg, km.LineDown):
		return moveDownY(tbl, y, 1)
	case key.Matches(keyMsg, km.PageUp):
		return moveUpY(tbl, y, h)
	case key.Matches(keyMsg, km.PageDown):
		return moveDownY(tbl, y, h)
	case key.Matches(keyMsg, km.HalfPageUp):
		return moveUpY(tbl, y, h/2)
	case key.Matches(keyMsg, km.HalfPageDown):
		return moveDownY(tbl, y, h/2)
	case key.Matches(keyMsg, km.GotoTop):
		return moveUpY(tbl, y, tbl.Cursor())
	case key.Matches(keyMsg, km.GotoBottom):
		return moveDownY(tbl, y, len(tbl.Rows()))
	default:
		return y
	}
}

// clampTableY applies the offset clamp bubbles' SetContent runs after
// SetHeight, SetRows, or SetCursor. Those paths do not otherwise change the
// offset.
func clampTableY(tbl table.Model, y int) int {
	start, end := tableWindow(tbl.Cursor(), tbl.Height(), len(tbl.Rows()))
	return clampContentY(y, start, end, tbl.Height())
}

// moveUpY mirrors table.MoveUp. The switch reads the window from before the
// cursor moves, and SetYOffset uses that same content. UpdateViewport then
// clamps against the new window.
func moveUpY(tbl table.Model, y, n int) int {
	height := tbl.Height()
	nrows := len(tbl.Rows())
	oldStart, oldEnd := tableWindow(tbl.Cursor(), height, nrows)
	cursor := clampInt(tbl.Cursor()-n, 0, nrows-1)
	switch {
	case oldStart == 0:
		y = setTableY(clampInt(y, 0, cursor), contentLines(oldStart, oldEnd), height)
	case oldStart < height:
		y = clampInt(clampInt(y+n, 0, cursor), 0, height)
	case y >= 1:
		y = clampInt(y+n, 1, height)
	}
	start, end := tableWindow(cursor, height, nrows)
	return clampContentY(y, start, end, height)
}

// moveDownY mirrors table.MoveDown. UpdateViewport runs before the switch,
// so the offset is clamped to the new window first.
func moveDownY(tbl table.Model, y, n int) int {
	height := tbl.Height()
	nrows := len(tbl.Rows())
	cursor := clampInt(tbl.Cursor()+n, 0, nrows-1)
	start, end := tableWindow(cursor, height, nrows)
	y = clampContentY(y, start, end, height)
	nlines := contentLines(start, end)
	switch {
	case end == nrows && y > 0:
		y = setTableY(clampInt(y-n, 1, height), nlines, height)
	case cursor > (end-start)/2 && y > 0:
		y = setTableY(clampInt(y-n, 1, cursor), nlines, height)
	case y > 1:
	case cursor > y+height-1:
		y = setTableY(clampInt(y+1, 0, 1), nlines, height)
	}
	return y
}

// tableWindow is bubbles' UpdateViewport range: rows from cursor-height
// through cursor+height, clamped to the table.
func tableWindow(cursor, height, nrows int) (start, end int) {
	if cursor >= 0 {
		start = clampInt(cursor-height, 0, cursor)
	}
	end = clampInt(cursor+height, cursor, nrows)
	return start, end
}

// contentLines is the line count SetContent stores. An empty render is one
// empty line; each rendered row is one line with these table styles.
func contentLines(start, end int) int {
	n := end - start
	if n <= 0 {
		return 1
	}
	return n
}

// clampContentY mirrors viewport.SetContent: an offset past the last line
// jumps to the bottom.
func clampContentY(y, start, end, height int) int {
	nlines := contentLines(start, end)
	if y > nlines-1 {
		return max(0, nlines-height)
	}
	return y
}

// setTableY mirrors viewport.SetYOffset. The default viewport style has no
// vertical frame, so the maximum offset is lines-height.
func setTableY(y, nlines, height int) int {
	return clampInt(y, 0, max(0, nlines-height))
}

func clampInt(v, low, high int) int {
	return min(max(v, low), high)
}
