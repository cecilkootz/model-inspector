package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestTableOffsetMatchesBubbles(t *testing.T) {
	rows := make([]table.Row, 40)
	for i := range rows {
		rows[i] = table.Row{fmt.Sprintf("row-%02d", i), "k"}
	}
	tbl := table.New(
		table.WithColumns([]table.Column{{Title: "Name", Width: 12}, {Title: "K", Width: 4}}),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(8),
	)
	y := 0
	check := func(step string) {
		t.Helper()
		start, yoff := bubbleScroll(t, tbl)
		if y != yoff {
			t.Fatalf("after %s offset %d, bubbles %d (cursor %d)", step, y, yoff, tbl.Cursor())
		}
		gotStart, _ := tableWindow(tbl.Cursor(), tbl.Height(), len(tbl.Rows()))
		if gotStart != start {
			t.Fatalf("after %s start %d, bubbles %d", step, gotStart, start)
		}
		if got, want := tableCursorLine(tbl, y), 1+tbl.Cursor()-start-yoff; got != want {
			t.Fatalf("after %s line %d, bubbles %d", step, got, want)
		}
	}
	check("init")
	keys := []string{
		"j", "down", "j", "j", "j", "j", "j", "j", "j", "j", "j",
		"f", "pgdown", " ", "d", "ctrl+d", "G", "end",
		"k", "up", "b", "pgup", "u", "ctrl+u", "g", "home",
		"j", "G", "k", "g",
	}
	for _, k := range keys {
		msg := scrollKey(k)
		y = stepTableY(tbl, y, msg)
		var cmd tea.Cmd
		tbl, cmd = tbl.Update(msg)
		if cmd != nil {
			t.Fatalf("%s returned a command", k)
		}
		check(k)
	}
	for _, h := range []int{4, 12, 1, 30} {
		tbl.SetHeight(h)
		y = clampTableY(tbl, y)
		check(fmt.Sprintf("height %d", h))
	}
	tbl.SetCursor(35)
	y = clampTableY(tbl, y)
	check("cursor 35")
	tbl.SetRows(rows[:3])
	y = clampTableY(tbl, y)
	check("3 rows")
	tbl.SetCursor(0)
	y = clampTableY(tbl, y)
	check("cursor 0")
	// A key the table does not bind must not move the offset.
	y = stepTableY(tbl, y, scrollKey("x"))
	tbl, _ = tbl.Update(scrollKey("x"))
	check("ignored")
}

func TestMarkTableCursorUsesTrackedOffset(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	rows := make([]table.Row, 30)
	for i := range rows {
		rows[i] = table.Row{fmt.Sprintf("id-%02d", i)}
	}
	tbl := table.New(
		table.WithColumns([]table.Column{{Title: "ID", Width: 8}}),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(6),
	)
	y := 0
	for _, k := range []string{"j", "j", "j", "j", "j", "f", "d", "G", "b", "u", "g"} {
		msg := scrollKey(k)
		y = stepTableY(tbl, y, msg)
		tbl, _ = tbl.Update(msg)
		view := markTableCursor(tbl.View(), tbl, y)
		_, yoff := bubbleScroll(t, tbl)
		want := 1 + tbl.Cursor() - mustStart(t, tbl) - yoff
		lines := strings.Split(view, "\n")
		marked := -1
		for i, ln := range lines {
			if strings.HasPrefix(ln, ">") {
				if marked >= 0 {
					t.Fatalf("after %s two markers\n%s", k, view)
				}
				marked = i
			}
		}
		if want < 1 || want >= len(lines) {
			if marked >= 0 {
				t.Fatalf("after %s marked line %d, cursor row not visible (want %d)", k, marked, want)
			}
			continue
		}
		if marked != want {
			t.Fatalf("after %s marker on %d, want %d\n%s", k, marked, want, view)
		}
		if !strings.Contains(lines[want], fmt.Sprintf("id-%02d", tbl.Cursor())) {
			t.Fatalf("after %s line %q is not the cursor row", k, lines[want])
		}
	}
}

func bubbleScroll(t *testing.T, tbl table.Model) (start, yoff int) {
	t.Helper()
	v := reflect.ValueOf(tbl)
	startField := v.FieldByName("start")
	vp := v.FieldByName("viewport")
	if !startField.IsValid() || !vp.IsValid() {
		t.Fatal("bubbles table no longer has start and viewport fields")
	}
	yField := vp.FieldByName("YOffset")
	if !yField.IsValid() {
		t.Fatal("bubbles viewport no longer has YOffset")
	}
	return int(startField.Int()), int(yField.Int())
}

func mustStart(t *testing.T, tbl table.Model) int {
	t.Helper()
	start, _ := bubbleScroll(t, tbl)
	return start
}

func scrollKey(s string) tea.KeyMsg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	case "ctrl+u":
		return tea.KeyMsg{Type: tea.KeyCtrlU}
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}
