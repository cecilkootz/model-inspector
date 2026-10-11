package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/cecilkootz/model-inspector/internal/fixture"
	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/scan"
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
	if !strings.Contains(view, "hyper_connection") || !strings.Contains(view, "n-gram") {
		t.Fatalf("layers tree missing hyper_connection or n-gram:\n%s", view)
	}
	var sawHyper bool
	for _, g := range m.Groups {
		if g == nil {
			continue
		}
		if g.Key == "other" || g.Kind == model.KindOther {
			t.Fatalf("layers has an other group: %s %s", g.Key, g.Kind)
		}
		if g.Key == "hyper_connection" && g.Kind == model.KindHyper {
			sawHyper = true
		}
	}
	if !sawHyper {
		t.Fatal("hyper_connection group missing")
	}

	hyper := drill(t, layers.(Model), m, "hyper_connection")
	for _, name := range []string{
		"input_mix_weight_up.weight",
		"input_mix_weight_down.weight",
		"hc_norm.weight",
	} {
		if !strings.Contains(hyper, name) {
			t.Errorf("hyper group view missing %s\n%s", name, hyper)
		}
	}
	norms := drill(t, layers.(Model), m, "norms")
	if !strings.Contains(norms, "norm.weight") {
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
	if ui.lay == nil {
		t.Fatal("layers state missing")
	}
	ui.layersCollapseAll()
	ui.lay.ensure(m)
	found := false
	for i, row := range ui.lay.rows {
		if row.kind == rowGroup && row.path == "g:"+key {
			ui.lay.row = i
			ui.lay.cursorPath = row.path
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no tree row for %s", key)
	}
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

	// Both states are explicit. An inherited NO_COLOR must not make the
	// color-on case a no-op, and TrueColor is forced so the color path
	// actually emits ESC before NO_COLOR strips it.
	t.Setenv("NO_COLOR", "")
	os.Unsetenv("NO_COLOR")
	t.Cleanup(func() {
		lipgloss.SetColorProfile(termenv.Ascii)
		SetNoColor(false)
	})
	SetNoColor(false)
	lipgloss.SetColorProfile(termenv.TrueColor)
	ui = New(m, scan.Run(m))
	sized, _ := ui.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	colorView := sized.View()
	assertFrame(t, colorView, 80, 24)
	if !strings.Contains(colorView, "\x1b") {
		t.Fatal("TrueColor with NO_COLOR unset produced no ESC")
	}

	t.Setenv("NO_COLOR", "1")
	lipgloss.SetColorProfile(termenv.TrueColor)
	ui = New(m, scan.Run(m))
	sized, _ = ui.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
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
	if !strings.Contains(view, "Code & pickles") || !strings.Contains(view, "Nothing flagged") {
		t.Fatalf("c key did not open the code list:\n%s", view)
	}
	cleared, _ := noticed.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if !strings.Contains(cleared.View(), "Code & pickles") {
		t.Fatalf("code list closed on j:\n%s", cleared.View())
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

func TestShardedNgramIdentity(t *testing.T) {
	files, err := fixture.ShardedNgram()
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
	oldLC, oldLang := os.Getenv("LC_ALL"), os.Getenv("LANG")
	if err := os.Setenv("LC_ALL", "C.UTF-8"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Setenv("LC_ALL", oldLC)
		_ = os.Setenv("LANG", oldLang)
		configure()
	})
	configure()
	view := renderIdentity(m, 160)
	for _, want := range []string{
		"⌂ of which 29.80 GiB are lookup tables that can stay in CPU RAM",
		"(heuristic) → ≥",
		"on the GPU/accelerator",
		"n-gram embedding table 29.80 GiB can stay in CPU RAM (heuristic)",
		"~51.20B-param n-gram embedding table",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("identity missing %q\n%s", want, view)
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
