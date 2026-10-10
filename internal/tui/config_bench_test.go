package tui

import (
	"fmt"
	"strings"
	"testing"

	"origin.cursor.com/cecilkootz/model-inspector/internal/fixture"
	"origin.cursor.com/cecilkootz/model-inspector/internal/jsonx"
)

func BenchmarkConfigToggle(b *testing.B) {
	m := loadCfg(b, fixture.ConfigQwenLike())
	ui := bootConfig(b, m, 140, 40)
	id := ui.cfg.findChild(0, "text_config")
	if id < 0 {
		b.Fatal("text_config")
	}
	ui.cfg.cursor = id
	ui.cfg.ensureRows()
	ui.cfg.row = ui.cfg.indexOfCursor()
	ui.configToggle()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ui.configToggle()
	}
}

func BenchmarkConfigSearch(b *testing.B) {
	raw := wideObject(200_000)
	tree, err := jsonx.ParseTree(raw, jsonx.Limits{})
	if err != nil {
		b.Fatal(err)
	}
	m := loadCfg(b, []byte(`{"a":1}`))
	ui := bootConfig(b, m, 140, 40)
	ui.cfg.tree = tree
	ui.cfg.parsed = true
	ui.cfg.err = nil
	ui.cfg.query = "k000100"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ui.runSearch()
	}
}

func BenchmarkConfigView(b *testing.B) {
	m := loadCfg(b, fixture.ConfigQwenLike())
	ui := bootConfig(b, m, 140, 40)
	_ = ui.View()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ui.cache.key = ""
		_ = ui.View()
	}
}

func BenchmarkConfigHugeView(b *testing.B) {
	m := loadCfg(b, fixture.ConfigHuge())
	ui := bootConfig(b, m, 140, 40)
	_ = ui.View()
	if _, ok := ui.cfg.err.(*jsonx.NodeLimitError); !ok {
		b.Fatalf("err %v", ui.cfg.err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ui.cache.key = ""
		_ = ui.View()
	}
}

func wideObject(n int) []byte {
	var b strings.Builder
	b.Grow(n * 16)
	b.WriteByte('{')
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"k%06d":%d`, i, i)
	}
	b.WriteByte('}')
	return []byte(b.String())
}
