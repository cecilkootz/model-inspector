package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cecilkootz/model-inspector/internal/fixture"
	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/scan"
)

func readyScan(tb testing.TB) tea.Model {
	tb.Helper()
	files, _, err := fixture.TinyQwen()
	if err != nil {
		tb.Fatal(err)
	}
	dir := tb.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		tb.Fatal(err)
	}
	m, err := model.Load(dir)
	if err != nil {
		tb.Fatal(err)
	}
	res := scan.Run(m)
	for i := range res.Checks {
		res.Checks[i].Duration = 0
	}
	ui := New(m, res)
	x, _ := ui.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	_ = x.View()
	_ = x.View()
	return x
}

func TestScanBudgets(t *testing.T) {
	x := readyScan(t)
	view := median(9, func() { _ = x.View() })
	if view > budget(2*time.Millisecond) {
		t.Errorf("cached View %s", view)
	}
	key := median(9, func() {
		x, _ = x.Update(tea.KeyMsg{Type: tea.KeyDown})
	})
	if key > budget(500*time.Microsecond) {
		t.Errorf("Update key %s", key)
	}
	resize := median(9, func() {
		x, _ = x.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	})
	if resize > budget(5*time.Millisecond) {
		t.Errorf("Resize %s", resize)
	}
}

func BenchmarkScanView(b *testing.B) {
	x := readyScan(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = x.View()
	}
}

func BenchmarkScanUpdate(b *testing.B) {
	x := readyScan(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		x, _ = x.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
}

func BenchmarkScanResize(b *testing.B) {
	x := readyScan(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := 140
		if i%2 == 0 {
			w = 120
		}
		x, _ = x.Update(tea.WindowSizeMsg{Width: w, Height: 40})
	}
}
