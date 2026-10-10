package tui

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"origin.cursor.com/cecilkootz/model-inspector/internal/fixture"
	"origin.cursor.com/cecilkootz/model-inspector/internal/jsonx"
	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
	"origin.cursor.com/cecilkootz/model-inspector/internal/scan"
)

func TestConfigDerived(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("LC_ALL", "C.UTF-8")
	m := loadCfg(t, fixture.ConfigQwenLike())
	ui := bootConfig(t, m, 140, 40)
	rows := map[string]fieldLine{}
	for _, ln := range ui.fieldLines() {
		if ln.kind == fieldRow {
			rows[ln.label] = ln
		}
	}
	if rows["heads q / kv"].derived != "= 12:1 GQA" {
		t.Fatalf("gqa %q", rows["heads q / kv"].derived)
	}
	if rows["heads q / kv"].jump == rows["model_type"].jump {
		t.Fatal("gqa jump landed on model_type")
	}
	if !strings.Contains(rows["context"].derived, "256K") || !strings.Contains(rows["context"].derived, "yarn") || !strings.Contains(rows["context"].derived, "1M") {
		t.Fatalf("context %q", rows["context"].derived)
	}
	textLayers := firstLabel(ui, "layers")
	if !strings.Contains(textLayers.derived, "36 linear") || !strings.Contains(textLayers.derived, "12 full") {
		t.Fatalf("layers %q", textLayers.derived)
	}
	if !strings.Contains(rows["per token"].derived, "11 of 512 active (2.1%)") || !rows["per token"].meterOn {
		t.Fatalf("experts %+v", rows["per token"])
	}
	if !strings.Contains(rows["effective"].value, "5.0") || !strings.Contains(rows["effective"].value, "2×16 / 32") {
		t.Fatalf("bits %q", rows["effective"].value)
	}
	if !strings.Contains(rows["full-attn every"].derived, "agrees") {
		t.Fatalf("agree %q", rows["full-attn every"].derived)
	}
	if rows["depth · hidden"].derived != "= head_dim 72" {
		t.Fatalf("vision %q", rows["depth · hidden"].derived)
	}
	if !strings.Contains(rowsMTP(ui), "speculative head") || strings.Contains(rowsMTP(ui), "GiB") {
		t.Fatalf("mtp %q", rowsMTP(ui))
	}
	if strings.Contains(rows["scheme"].derived, "mlx") {
		t.Fatalf("scheme invented mlx: %q", rows["scheme"].derived)
	}
	// Canonical paths.
	kv := rows["heads q / kv"].jump
	if got := displayPath(ui.cfg, kv); got != ".text_config.num_key_value_heads" {
		t.Fatalf("path %s", got)
	}
	lt := ui.cfg.findChild(ui.cfg.findChild(0, "text_config"), "layer_types")
	// 8th element, index 7.
	id := lt
	for i := 0; i < 8; i++ {
		if i == 0 {
			id = ui.cfg.tree.Nodes[lt].First
			continue
		}
		id = ui.cfg.tree.Nodes[id].Next
	}
	if got := displayPath(ui.cfg, id); got != ".text_config.layer_types[7]" {
		t.Fatalf("index path %s", got)
	}
	q := ui.cfg.findChild(0, "quantization")
	ov := ui.cfg.findChild(q, "synthetic.override.0")
	bits := ui.cfg.findChild(ov, "bits")
	if got := displayPath(ui.cfg, bits); got != `.quantization["synthetic.override.0"].bits` {
		t.Fatalf("quoted path %s", got)
	}
}

func rowsMTP(ui Model) string {
	for _, ln := range ui.fieldLines() {
		if ln.kind == fieldRow && strings.Contains(ln.derived, "speculative") {
			return ln.derived
		}
	}
	return ""
}

func firstLabel(ui Model, label string) fieldLine {
	for _, ln := range ui.fieldLines() {
		if ln.kind == fieldRow && ln.label == label {
			return ln
		}
	}
	return fieldLine{}
}

func TestConfigDerivedVariants(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("LC_ALL", "C.UTF-8")
	cases := []struct {
		name, raw, label, want string
		meter                  bool
	}{
		{"mha", `{"num_attention_heads":8,"num_key_value_heads":8}`, "heads q / kv", "= MHA", false},
		{"mqa", `{"num_attention_heads":8,"num_key_value_heads":1}`, "heads q / kv", "= MQA", false},
		{"headdim", `{"hidden_size":256,"num_attention_heads":2}`, "head_dim", "= 128 (hidden ÷ heads)", false},
		{"noshared", `{"num_experts":512,"num_experts_per_tok":10}`, "per token", "= 10 of 512 (2.0%)", false},
		{"sym", `{"quantization":{"bits":4,"group_size":32,"mode":"symmetric"}}`, "effective", "4.5", false},
		{"disagree", `{"num_hidden_layers":8,"full_attention_interval":4,"layer_types":["full_attention","linear_attention","linear_attention","linear_attention","linear_attention","linear_attention","linear_attention","linear_attention"]}`, "full-attn every", "! layer_types says 1", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ui := bootConfig(t, loadCfg(t, []byte(c.raw)), 140, 40)
			var got fieldLine
			for _, ln := range ui.fieldLines() {
				if ln.label == c.label {
					got = ln
				}
			}
			text := got.derived + " " + got.value
			if !strings.Contains(text, c.want) {
				t.Fatalf("got %q want %q", text, c.want)
			}
			if c.name == "noshared" && got.meterOn {
				t.Fatal("meter without shared experts")
			}
		})
	}
}

func TestConfigSearchCapAndCursor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("LC_ALL", "C.UTF-8")
	t.Setenv("MODEL_INSPECTOR_MOTION", "0")
	m := loadCfg(t, fixture.ConfigQwenLike())
	ui := bootConfig(t, m, 140, 40)
	var x tea.Model = ui
	before := x.View()
	x, _ = x.Update(keyMsg("w"))
	x, _ = x.Update(keyMsg("down"))
	after := x.View()
	if before == after {
		t.Fatal("tree cursor move did not change the frame")
	}
	x, _ = x.Update(keyMsg("/"))
	paste := strings.Repeat("é", 200)
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(paste)})
	mm := x.(Model)
	if n := len([]rune(mm.cfg.query)); n != configQueryMax {
		t.Fatalf("query runes %d", n)
	}
	// A second paste appends then truncates; it is not dropped whole.
	mm.cfg.query = strings.Repeat("a", 120)
	x = mm
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(strings.Repeat("b", 20))})
	mm = x.(Model)
	if mm.cfg.query != strings.Repeat("a", 120)+strings.Repeat("b", 8) {
		t.Fatalf("cap %q", mm.cfg.query)
	}
	if strings.Contains(x.View(), strings.Repeat("a", 120)) && strings.Contains(mm.cfg.query, "\x1b") {
		t.Fatal("raw query leaked")
	}
}

func TestConfigSearchWrapAndCancel(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("LC_ALL", "C.UTF-8")
	ui := bootConfig(t, loadCfg(t, []byte(`{"num_experts":1,"other":2}`)), 140, 40)
	var x tea.Model = ui
	x, _ = x.Update(keyMsg("w"))
	mm := x.(Model)
	before := len(mm.cfg.expanded)
	x, _ = x.Update(keyMsg("/"))
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("expert")})
	x, _ = x.Update(keyMsg("enter"))
	x, _ = x.Update(keyMsg("n"))
	if !strings.Contains(x.(Model).notice, "wrapped") {
		t.Fatalf("notice %q", x.(Model).notice)
	}
	// A fresh input: esc restores the expansion from before this search.
	ui = bootConfig(t, loadCfg(t, []byte(`{"num_experts":1,"kept":{"hidden":1}}`)), 140, 40)
	x = ui
	x, _ = x.Update(keyMsg("w"))
	before = len(x.(Model).cfg.expanded)
	x, _ = x.Update(keyMsg("/"))
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hidden")})
	x, _ = x.Update(keyMsg("tab"))
	view := x.View()
	if !strings.Contains(view, "hidden by filter") && !strings.Contains(view, "FILTER") {
		t.Fatalf("filter view missing marker\n%s", view)
	}
	x, _ = x.Update(keyMsg("esc"))
	mm = x.(Model)
	if mm.cfg.query != "" || len(mm.cfg.expanded) != before {
		t.Fatalf("cancel query %q expanded %d want %d", mm.cfg.query, len(mm.cfg.expanded), before)
	}
}

func TestConfigCopyOSC(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("LC_ALL", "C.UTF-8")
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "xterm-256color")
	ui := bootConfig(t, loadCfg(t, fixture.ConfigQwenLike()), 80, 24)
	var buf bytes.Buffer
	yes := true
	ui.cfg.copyTTY = &yes
	ui.cfg.copyTo = &buf
	var x tea.Model = ui
	x, _ = x.Update(keyMsg("y"))
	mm := x.(Model)
	if !strings.Contains(mm.notice, "copied path") || !strings.Contains(mm.notice, ".model_type") {
		t.Fatalf("notice %q active %d search %v cur %d", mm.notice, mm.active, mm.cfg.search, mm.cfg.fieldCur)
	}
	if !bytes.Contains(buf.Bytes(), []byte("\x1b]52;c;")) {
		t.Fatalf("osc %q", buf.Bytes())
	}
	plain := buf.Bytes()

	buf.Reset()
	t.Setenv("TMUX", "1")
	x, _ = x.Update(keyMsg("y"))
	if bytes.Equal(buf.Bytes(), plain) || !bytes.Contains(buf.Bytes(), []byte("tmux")) {
		t.Fatalf("tmux wrap %q", buf.Bytes()[:min(40, buf.Len())])
	}

	buf.Reset()
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "screen")
	x, _ = x.Update(keyMsg("y"))
	if bytes.Contains(buf.Bytes(), []byte("tmux")) || !bytes.Contains(buf.Bytes(), []byte("\x1b")) {
		t.Fatalf("screen wrap %q", buf.Bytes()[:min(48, buf.Len())])
	}

	t.Setenv("TERM", "dumb")
	buf.Reset()
	x, _ = x.Update(keyMsg("y"))
	if buf.Len() != 0 || !strings.Contains(x.(Model).notice, "not copied") {
		t.Fatalf("dumb %q buf %d", x.(Model).notice, buf.Len())
	}

	t.Setenv("TERM", "xterm-256color")
	no := false
	mm = x.(Model)
	mm.cfg.copyTTY = &no
	x = mm
	buf.Reset()
	x, _ = x.Update(keyMsg("y"))
	if buf.Len() != 0 || !strings.Contains(x.(Model).notice, "not copied") {
		t.Fatalf("notty %q", x.(Model).notice)
	}
}

func TestConfigCopySafetyAndCap(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("LC_ALL", "C.UTF-8")
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "xterm-256color")
	ui := bootConfig(t, loadCfg(t, fixture.ConfigHostile()), 140, 40)
	var buf bytes.Buffer
	yes := true
	ui.cfg.copyTTY = &yes
	ui.cfg.copyTo = &buf
	var x tea.Model = ui
	x, _ = x.Update(keyMsg("w"))
	// Land on the value that contains ESC by searching.
	x, _ = x.Update(keyMsg("/"))
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("has")})
	x, _ = x.Update(keyMsg("enter"))
	x, _ = x.Update(keyMsg("Y"))
	mm := x.(Model)
	if !strings.Contains(mm.notice, "control characters") {
		t.Fatalf("notice %q", mm.notice)
	}
	if bytes.Contains(buf.Bytes(), []byte{0x1b, 'h'}) || bytes.Contains(buf.Bytes(), []byte("has\x1b")) {
		t.Fatal("raw ESC in OSC payload")
	}
	if !strings.Contains(oscPayload(buf.Bytes()), "<U+001B>") {
		t.Fatalf("payload missing token: %q", buf.Bytes())
	}

	buf.Reset()
	ui = bootConfig(t, loadCfg(t, fixture.ConfigQwenLike()), 140, 40)
	ui.cfg.copyTTY = &yes
	ui.cfg.copyTo = &buf
	x = ui
	x, _ = x.Update(keyMsg("w"))
	x, _ = x.Update(keyMsg("/"))
	for _, r := range "synthetic_long" {
		x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	x, _ = x.Update(keyMsg("enter"))
	x, _ = x.Update(keyMsg("Y"))
	mm = x.(Model)
	if !strings.Contains(mm.notice, "too large to copy") || buf.Len() != 0 {
		t.Fatalf("notice %q buf %d", mm.notice, buf.Len())
	}
}

func TestConfigLargeSearchWaits(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	ui := bootConfig(t, loadCfg(t, []byte(`{"a":1}`)), 140, 40)
	ui.cfg.tree.Nodes = make([]jsonx.Node, 50_001)
	ui.cfg.tree.Nodes[0] = jsonx.Node{Kind: jsonx.KindObject, First: -1, Next: -1, Parent: -1, Index: -1}
	var x tea.Model = ui
	x, _ = x.Update(keyMsg("/"))
	mm := x.(Model)
	if !mm.cfg.largeWait {
		t.Fatal("expected enter-to-search above 50k")
	}
	if !strings.Contains(x.View(), "to search") {
		t.Fatal("missing large-search hint")
	}
}

func TestConfigProblems(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("LC_ALL", "C.UTF-8")
	cases := []struct {
		name string
		raw  []byte
		want string
	}{
		{"malformed", fixture.ConfigMalformed(), "config.json did not parse."},
		{"dup", fixture.ConfigDupKey(), `duplicate key "num_experts"`},
		{"deep", fixture.ConfigDeep(), "nesting deeper than 32"},
		{"array", fixture.ConfigNotObject(), "not a JSON object"},
		{"empty", fixture.ConfigEmpty(), "no fields"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			view := bootConfig(t, loadCfg(t, c.raw), 80, 24).View()
			if !strings.Contains(view, c.want) {
				t.Fatalf("missing %q\n%s", c.want, view)
			}
		})
	}
	view := bootConfig(t, loadCfg(t, nil), 80, 24).View()
	if !strings.Contains(view, "No config.json") {
		t.Fatalf("missing no-config\n%s", view)
	}
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "config.json")); err != nil {
		t.Fatal(err)
	}
	writeShard(t, dir)
	m, err := model.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.Dir = "/model"
	view = bootConfig(t, m, 80, 24).View()
	if !strings.Contains(view, "unreadable") {
		t.Fatalf("unreadable\n%s", view)
	}
}

func TestPageEnd(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("LC_ALL", "C.UTF-8")
	ui := bootConfig(t, loadCfg(t, fixture.ConfigQwenLike()), 100, 30)
	var x tea.Model = ui
	for _, k := range append(append([]string{"w", "/"}, runes("synthetic_items")...), "enter", "enter", "end") {
		x, _ = x.Update(keyMsg(k))
	}
	mm := x.(Model)
	view := mm.View()
	if !strings.Contains(view, "show 100") {
		t.Fatalf("row %d scroll %d rows %d\n%s", mm.cfg.row, mm.cfg.scroll, len(mm.cfg.rows), view)
	}
}

func TestModalEndScrolls(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("LC_ALL", "C.UTF-8")
	ui := bootConfig(t, loadCfg(t, fixture.ConfigQwenLike()), 80, 24)
	var x tea.Model = ui
	x, _ = x.Update(keyMsg("/"))
	for _, r := range "synthetic_long" {
		x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	x, _ = x.Update(keyMsg("enter"))
	x, _ = x.Update(keyMsg("enter"))
	x, _ = x.Update(keyMsg("end"))
	mm := x.(Model)
	if !mm.cfg.modal || mm.cfg.modalScroll == 0 {
		t.Fatalf("modal %v scroll %d", mm.cfg.modal, mm.cfg.modalScroll)
	}
	if !strings.Contains(mm.View(), "truncated") {
		t.Fatalf("view missing truncation\n%s", mm.View())
	}
}

func TestMagLabel(t *testing.T) {
	if magLabel(262144) != "256K" || magLabel(10_000_000) != "10M" || magLabel(1048576) != "1M" {
		t.Fatalf("%s %s %s", magLabel(262144), magLabel(10_000_000), magLabel(1048576))
	}
}

func oscPayload(b []byte) string {
	s := string(b)
	i := strings.LastIndex(s, "]52;c;")
	if i < 0 {
		return s
	}
	s = s[i+6:]
	if j := strings.IndexAny(s, "\a\x1b"); j >= 0 {
		s = s[:j]
	}
	dec, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return string(b)
	}
	return string(dec)
}

func loadCfg(t testing.TB, raw []byte) *model.Model {
	t.Helper()
	dir := t.TempDir()
	if raw != nil {
		if err := os.WriteFile(filepath.Join(dir, "config.json"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeShard(t, dir)
	m, err := model.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.Dir = "/model"
	return m
}

func writeShard(t testing.TB, dir string) {
	t.Helper()
	shard, _, err := fixture.WriteHeaderOnly("", []fixture.Tensor{{Name: "w", DType: "BF16", Shape: []int64{2}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "model.safetensors"), shard, 0o644); err != nil {
		t.Fatal(err)
	}
}

func bootConfig(t testing.TB, m *model.Model, w, h int) Model {
	t.Helper()
	t.Setenv("MODEL_INSPECTOR_MOTION", "0")
	t.Setenv("MODEL_INSPECTOR_MOUSE", "1")
	ui := New(m, scan.Run(m))
	x, _ := ui.Update(tea.WindowSizeMsg{Width: w, Height: h})
	x, _ = x.Update(keyMsg("2"))
	mm, ok := x.(Model)
	if !ok {
		t.Fatalf("%T", x)
	}
	return mm
}
