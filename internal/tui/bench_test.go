package tui

import (
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/cecilkootz/model-inspector/internal/fixture"
	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/scan"
)

func TestViewUnder50ms(t *testing.T) {
	t.Setenv("MODEL_INSPECTOR_MOTION", "0")
	t.Setenv("NO_COLOR", "1")
	lipgloss.SetColorProfile(termenv.Ascii)
	m := benchModel(t)
	ui := New(m, scan.Run(m))
	sized, _ := ui.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	start := time.Now()
	view := sized.View()
	if d := time.Since(start); d > raceScale*50*time.Millisecond {
		t.Fatalf("View took %s", d)
	}
	if view == "" {
		t.Fatal("empty view")
	}
}

func BenchmarkView(b *testing.B) {
	b.Setenv("MODEL_INSPECTOR_MOTION", "0")
	b.Setenv("NO_COLOR", "1")
	lipgloss.SetColorProfile(termenv.Ascii)
	m := benchModel(b)
	ui := New(m, scan.Run(m))
	sized, _ := ui.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	sized.View()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sized.View()
	}
}

func BenchmarkUpdateKey(b *testing.B) {
	b.Setenv("MODEL_INSPECTOR_MOTION", "0")
	m := benchModel(b)
	ui := New(m, scan.Run(m))
	var x tea.Model = ui
	x, _ = x.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		x, _ = x.Update(msg)
	}
}

func BenchmarkUpdateMouse(b *testing.B) {
	b.Setenv("MODEL_INSPECTOR_MOTION", "0")
	m := benchModel(b)
	ui := New(m, scan.Run(m))
	var x tea.Model = ui
	x, _ = x.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	msg := tea.MouseMsg{X: 2, Y: 4, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		x, _ = x.Update(msg)
	}
}

func BenchmarkResize(b *testing.B) {
	b.Setenv("MODEL_INSPECTOR_MOTION", "0")
	m := benchModel(b)
	ui := New(m, scan.Run(m))
	var x tea.Model = ui
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

func benchModel(tb testing.TB) *model.Model {
	tb.Helper()
	var tensors []fixture.Tensor
	for i := 0; i < 4000; i++ {
		layer := i % 49
		tensors = append(tensors, fixture.Tensor{
			Name:  fmt.Sprintf("model.layers.%d.self_attn.q_proj.weight.extra%d", layer, i),
			DType: "U8",
			Shape: []int64{8},
		})
	}
	shard, _, err := fixture.WriteHeaderOnly("", tensors)
	if err != nil {
		tb.Fatal(err)
	}
	dir := tb.TempDir()
	files := []fixture.File{
		{Name: "config.json", Data: []byte(`{"model_type":"bench","text_config":{"num_hidden_layers":49}}`)},
		{Name: "model.safetensors", Data: shard},
	}
	if err := fixture.Materialize(dir, files); err != nil {
		tb.Fatal(err)
	}
	m, err := model.Load(dir)
	if err != nil {
		tb.Fatal(err)
	}
	return m
}
