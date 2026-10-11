package ui

import (
	"testing"

	"github.com/cecilkootz/model-inspector/internal/sanitize"
)

func heatBench(n int) HeatGrid {
	labels := []string{"attention", "router", "shared exp", "routed exp", "norms"}
	var grid HeatGrid
	grid.Rows = make([]HeatRow, len(labels))
	for i, lab := range labels {
		cells := make([]int64, n)
		for c := range cells {
			cells[c] = int64(1000 + (c%200)*17 + i*3)
		}
		grid.Rows[i] = HeatRow{Label: S(sanitize.From(lab)), Cells: cells, MTP: 800, HasMTP: true}
	}
	grid.Prepare()
	return grid
}

func BenchmarkHeatmap(b *testing.B) {
	grid := heatBench(49)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Heatmap(grid, 140, true)
	}
}

func BenchmarkHeatmap50k(b *testing.B) {
	grid := heatBench(50_000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Heatmap(grid, 140, true)
	}
}
