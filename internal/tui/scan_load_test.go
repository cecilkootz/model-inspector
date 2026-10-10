package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
	"origin.cursor.com/cecilkootz/model-inspector/internal/scan"
)

func TestLoadErrorCardExits3(t *testing.T) {
	prev := loadModel
	t.Cleanup(func() { loadModel = prev })
	loadModel = func(dir string, ch chan<- model.Progress) (*model.Model, error) {
		return nil, errors.New("disk\x1b failed " + strings.Repeat("x", 400))
	}
	ui := NewLoading(t.TempDir(), false, false, "", "")
	msg := runScanLoad(ui)()
	next, _ := ui.Update(msg.(tea.Msg))
	m := next.(Model)
	if m.exitCode() != 3 {
		t.Fatalf("exit %d", m.exitCode())
	}
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	view := sized.View()
	if strings.Contains(view, "\x1b") {
		t.Fatal("error card contains a raw ESC")
	}
	if !strings.Contains(view, "<U+001B>") || !strings.Contains(view, "could not inspect") {
		t.Fatalf("card:\n%s", view)
	}
	if strings.Count(view, "x") > 300 {
		t.Fatal("error text was not clipped")
	}
	quit, cmd := sized.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("q did not quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q was not a quit")
	}
	if quit.(Model).exitCode() != 3 {
		t.Fatal("quit dropped the exit code")
	}
}

func TestHostileScanDetailsDoNotPanic(t *testing.T) {
	m := loadTiny(t)
	res := scan.Run(m)
	hostile := "e\u009b/\u202e/\x1b]0;T\x07/" + strings.Repeat("p", 400)
	res.Checks[0].Summary = hostile
	res.Checks[0].Details = []string{hostile, "\xff\xfe tail"}
	ui := New(m, res)
	sized, _ := ui.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	on, _ := sized.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	view := on.View()
	if r, bad := rawBad(view); bad {
		t.Fatalf("raw %s", r)
	}
	if !strings.Contains(view, "<U+001B>") || !strings.Contains(view, "<U+202E>") {
		t.Fatalf("hostile detail was dropped:\n%s", view)
	}
	plain := clipScan(hostile)
	if len([]rune(plain)) > scanClip {
		t.Fatalf("clip %d", len([]rune(plain)))
	}
}
