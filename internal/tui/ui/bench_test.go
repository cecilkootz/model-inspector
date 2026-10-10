package ui

import "testing"

func BenchmarkHeatmap(b *testing.B) {
	var grid HeatGrid
	labels := []string{"attention", "router", "shared exp", "routed exp", "norms"}
	grid.Rows = make([]HeatRow, len(labels))
	for i, lab := range labels {
		cells := make([]int64, 49)
		for c := range cells {
			cells[c] = int64(1000 + c*17 + i*3)
		}
		grid.Rows[i] = HeatRow{Label: lab, Cells: cells, MTP: 800, HasMTP: true}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Heatmap(grid, 140, true)
	}
}
