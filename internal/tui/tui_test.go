package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"origin.cursor.com/cecilkootz/model-inspector/internal/fixture"
	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
	"origin.cursor.com/cecilkootz/model-inspector/internal/scan"
)

func TestLayersViewKeepsHyperConnectionTogether(t *testing.T) {
	files, _, err := fixture.TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	ui := New(m, scan.Run(m))
	sized, _ := ui.Update(tea.WindowSizeMsg{Width: 180, Height: 40})
	layers, _ := sized.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	view := layers.(Model).View()
	if !strings.Contains(view, "hyper_connection") || !strings.Contains(view, "ngram") {
		t.Fatalf("group table missing hyper_connection or ngram:\n%s", view)
	}
	rows := groupRows(m)
	var sawHyper bool
	for _, row := range rows {
		if row[0] == "other" || row[1] == model.KindOther {
			t.Fatalf("layers table has an other group row: %v", row)
		}
		if row[0] == "hyper_connection" && row[1] == model.KindHyper {
			sawHyper = true
		}
	}
	if !sawHyper {
		t.Fatalf("layers table rows = %v", rows)
	}

	hyper := drill(t, layers.(Model), m, "hyper_connection")
	for _, name := range []string{
		"hyper_connection_mixer.input_mix_weight_up.weight",
		"hyper_connection_mixer.input_mix_weight_down.weight",
		"hyper_connection_mixer.hc_norm.weight",
	} {
		if !strings.Contains(hyper, name) {
			t.Errorf("hyper group view missing %s\n%s", name, hyper)
		}
	}
	norms := drill(t, layers.(Model), m, "norms")
	if !strings.Contains(norms, "model.norm.weight") {
		t.Fatalf("norms view = %s", norms)
	}
	if strings.Contains(norms, "hyper_connection_mixer") {
		t.Fatalf("hc_norm still rendered under norms:\n%s", norms)
	}
}

func drill(t *testing.T, ui Model, m *model.Model, key string) string {
	t.Helper()
	idx := -1
	for i, g := range m.Groups {
		if g.Key == key {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("no group %s", key)
	}
	ui.groupTable.SetCursor(idx)
	drilled, _ := ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return drilled.(Model).View()
}

func TestViewSizesAndNoColor(t *testing.T) {
	m := loadTiny(t)
	ui := New(m, scan.Run(m))
	before := ui.View()
	if strings.Contains(before, "loading") {
		t.Fatalf("before a window size the view says loading:\n%s", before)
	}
	if !strings.Contains(before, "Identity") || !strings.Contains(before, "Scan") {
		t.Fatalf("before a window size the header is missing:\n%s", before)
	}

	for _, sz := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 60, Height: 16}, {Width: 120, Height: 40}} {
		sized, _ := ui.Update(sz)
		view := sized.View()
		assertFrame(t, view, sz.Width, sz.Height)
		if strings.Contains(view, "needs at least") {
			t.Fatalf("%dx%d rendered the too-small screen:\n%s", sz.Width, sz.Height, view)
		}
	}

	small, _ := ui.Update(tea.WindowSizeMsg{Width: 59, Height: 15})
	smallView := small.View()
	assertFrame(t, smallView, 59, 15)
	if !strings.Contains(smallView, "needs at least 60×16") || !strings.Contains(smallView, "59×15") {
		t.Fatalf("too-small screen:\n%s", smallView)
	}

	t.Setenv("NO_COLOR", "1")
	ui = New(m, scan.Run(m))
	sized, _ := ui.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	view := sized.View()
	assertFrame(t, view, 80, 24)
	if strings.Contains(view, "\x1b") {
		t.Fatalf("NO_COLOR view contains ESC:\n%s", view)
	}
	for _, r := range view {
		if r < 0x20 && r != '\n' {
			t.Fatalf("NO_COLOR view contains %#U", r)
		}
	}
}

func TestScanCodeNoticeAndInputFocus(t *testing.T) {
	m := loadTiny(t)
	ui := New(m, scan.Run(m))
	sized, _ := ui.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	scanTab, _ := sized.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	noticed, _ := scanTab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	view := noticed.View()
	if !strings.Contains(view, "Code & pickles arrives in a later version") {
		t.Fatalf("c key did not set the footer notice:\n%s", view)
	}
	cleared, _ := noticed.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if strings.Contains(cleared.View(), "Code & pickles arrives") {
		t.Fatalf("notice survived the next key:\n%s", cleared.View())
	}

	focused := sized.(Model)
	focused.inputFocused = true
	next, cmd := focused.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd != nil {
		if msg := cmd(); msg != nil {
			if _, quit := msg.(tea.QuitMsg); quit {
				t.Fatal("q quit while a text input was focused")
			}
		}
	}
	if !next.(Model).inputFocused {
		t.Fatal("q cleared input focus")
	}
}

func loadTiny(t *testing.T) *model.Model {
	t.Helper()
	files, _, err := fixture.TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func assertFrame(t *testing.T, view string, width, height int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) != height {
		t.Fatalf("got %d lines, want %d\n%s", len(lines), height, view)
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w != width {
			t.Fatalf("line %d width %d, want %d: %q", i, w, width, ln)
		}
	}
}

func TestASCIILocaleHasNoUnicodeGlyphs(t *testing.T) {
	t.Setenv("LC_ALL", "C")
	t.Setenv("LANG", "C")
	m := loadTiny(t)
	ui := New(m, scan.Run(m))
	sized, _ := ui.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	scanTab, _ := sized.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	view := scanTab.View()
	for _, glyph := range []string{"✓", "✗", "–", "▶", "▲", "◆", "●", "⌂", "█", "▓", "▒", "░"} {
		if strings.Contains(view, glyph) {
			t.Fatalf("ASCII locale view contains %q\n%s", glyph, view)
		}
	}
	if !strings.Contains(view, "+ PASS") {
		t.Fatalf("ASCII locale did not use the + pass glyph:\n%s", view)
	}
}
