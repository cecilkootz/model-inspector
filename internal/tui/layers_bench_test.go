package tui

import (
	"fmt"
	"sort"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/safetensors"
	"github.com/cecilkootz/model-inspector/internal/scan"
)

func layersScaleModel(groups, per int) *model.Model {
	m := &model.Model{Format: "safetensors", Dir: "/model", TotalSize: 1}
	for i := 0; i < groups; i++ {
		key := fmt.Sprintf("layer %d", i)
		g := &model.Group{Key: key, Kind: model.KindLayer, Layer: i}
		for j := 0; j < per; j++ {
			name := fmt.Sprintf("model.layers.%d.self_attn.q_proj.weight", i)
			if per > 1 {
				name = fmt.Sprintf("model.layers.%d.mlp.experts.%d.down_proj.weight", i, j)
			}
			tensor := &model.Tensor{
				TensorInfo: safetensors.TensorInfo{Name: name, DType: "U8", Shape: []int64{8}, DataOffsets: [2]int64{0, 8}},
				Kind:       model.KindLayer, Layer: i, Group: key, Role: model.RolePlain,
			}
			g.Tensors = append(g.Tensors, tensor)
			m.Tensors = append(m.Tensors, tensor)
		}
		m.Groups = append(m.Groups, g)
	}
	return m
}

func readyLayers(tb testing.TB, m *model.Model) tea.Model {
	tb.Helper()
	tb.Setenv("MODEL_INSPECTOR_MOTION", "0")
	tb.Setenv("MODEL_INSPECTOR_MOUSE", "1")
	tb.Setenv("NO_COLOR", "1")
	lipgloss.SetColorProfile(termenv.Ascii)
	ui := New(m, scan.Result{})
	var x tea.Model = ui
	x, _ = x.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	_ = x.View()
	return x
}

func BenchmarkLayersViewCached(b *testing.B) {
	x := readyLayers(b, layersScaleModel(50_000, 1))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = x.View()
	}
}

func BenchmarkLayersViewCached200k(b *testing.B) {
	x := readyLayers(b, layersScaleModel(1, 200_000))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = x.View()
	}
}

func BenchmarkLayersUpdateKey(b *testing.B) {
	x := readyLayers(b, layersScaleModel(50_000, 1))
	msg := tea.KeyMsg{Type: tea.KeyDown}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		x, _ = x.Update(msg)
	}
}

func BenchmarkLayersUpdateMouse(b *testing.B) {
	x := readyLayers(b, layersScaleModel(50_000, 1))
	msg := tea.MouseMsg{X: 4, Y: 6, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		x, _ = x.Update(msg)
	}
}

func BenchmarkLayersResize(b *testing.B) {
	x := readyLayers(b, layersScaleModel(50_000, 1))
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

func TestLayersBudgets(t *testing.T) {
	models := []*model.Model{layersScaleModel(50_000, 1), layersScaleModel(1, 200_000)}
	names := []string{"50k-groups", "200k-tensors"}
	for i, m := range models {
		t.Run(names[i], func(t *testing.T) {
			x := readyLayers(t, m)
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
			mouse := median(9, func() {
				x, _ = x.Update(tea.MouseMsg{X: 4, Y: 6, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
			})
			if mouse > budget(500*time.Microsecond) {
				t.Errorf("Update mouse %s", mouse)
			}
			resize := median(9, func() {
				x, _ = x.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
			})
			if resize > budget(5*time.Millisecond) {
				t.Errorf("Resize %s", resize)
			}
		})
	}
}

func median(n int, fn func()) time.Duration {
	samples := make([]time.Duration, n)
	for i := 0; i < n; i++ {
		start := time.Now()
		fn()
		samples[i] = time.Since(start)
	}
	sort.Slice(samples, func(a, b int) bool { return samples[a] < samples[b] })
	return samples[n/2]
}
