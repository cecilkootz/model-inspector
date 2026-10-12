package tui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cecilkootz/model-inspector/internal/fixture"
	"github.com/cecilkootz/model-inspector/internal/jsonx"
	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/scan"
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

func TestUnderflowFieldMark(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("LC_ALL", "C.UTF-8")
	raw := []byte(`{"model_type":"qwen4_exp","num_hidden_layers":1e-400,"vocab_size":-1e-400,"hidden_size":0e-400}`)
	m := loadCfg(t, raw)
	warn := strings.Join(m.Warnings, "\n")
	for _, want := range []string{
		"num_hidden_layers is out of range: 1e-400",
		"vocab_size is out of range: -1e-400",
	} {
		if !strings.Contains(warn, want) {
			t.Fatalf("warnings missing %q\n%s", want, warn)
		}
	}
	if strings.Contains(warn, "0e-400") || strings.Contains(warn, "hidden_size is out of range") {
		t.Fatalf("warnings flagged a real zero\n%s", warn)
	}
	ui := bootConfig(t, m, 140, 40)
	rows := map[string]fieldLine{}
	for _, ln := range ui.fieldLines() {
		if ln.kind == fieldRow {
			rows[ln.label] = ln
		}
	}
	if rows["layers"].value != "1e-400 ! out of range" || !rows["layers"].bad {
		t.Fatalf("layers %+v", rows["layers"])
	}
	if rows["vocab_size"].value != "-1e-400 ! out of range" || !rows["vocab_size"].bad {
		t.Fatalf("vocab %+v", rows["vocab_size"])
	}
	if rows["hidden_size"].value != "0e-400" || rows["hidden_size"].bad {
		t.Fatalf("hidden %+v", rows["hidden_size"])
	}
	lits := map[string]string{}
	for c := ui.cfg.tree.Nodes[0].First; c >= 0; c = ui.cfg.tree.Nodes[c].Next {
		n := ui.cfg.tree.Nodes[c]
		if n.Kind == jsonx.KindNumber {
			lits[n.Key] = n.Lit
		}
	}
	if lits["num_hidden_layers"] != "1e-400" || lits["vocab_size"] != "-1e-400" || lits["hidden_size"] != "0e-400" {
		t.Fatalf("tree lits %#v", lits)
	}
	cur := ui
	list := stripView(strings.Join(cur.sidePanels()[3].rows(100), "\n"))
	if !strings.Contains(list, "1e-400 ! out of range") || !strings.Contains(list, "-1e-400 ! out of range") {
		t.Fatalf("the list did not flag underflow:\n%s", list)
	}
	if strings.Contains(list, "0e-400 !") {
		t.Fatalf("the list flagged 0e-400:\n%s", list)
	}
	view := stripView(ui.View())
	for _, lit := range []string{"1e-400", "-1e-400", "0e-400"} {
		if !strings.Contains(view, lit) {
			t.Fatalf("the tree dropped %s:\n%s", lit, view)
		}
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
		t.Fatalf("notice %q cur %d", mm.notice, mm.cfg.fieldCur)
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

func TestMagLabel(t *testing.T) {
	if magLabel(262144) != "256K" || magLabel(10_000_000) != "10M" || magLabel(1048576) != "1M" {
		t.Fatalf("%s %s %s", magLabel(262144), magLabel(10_000_000), magLabel(1048576))
	}
}

func TestControlCountGrapheme(t *testing.T) {
	if controlCount("\x1b\x1b") != 2 {
		t.Fatalf("two ESC graphemes: %d", controlCount("\x1b\x1b"))
	}
	if controlCount("\u202e") != 1 {
		t.Fatalf("one bidi rune: %d", controlCount("\u202e"))
	}
	// Base + variation selector is one cluster, not two runes.
	if got := controlCount("A\uFE0F"); got != 1 {
		t.Fatalf("variation selector cluster: %d", got)
	}
	// Two controls inside one ZWJ sequence count once.
	seq := "\u200D\uFE0F"
	if got := controlCount(seq); got != 1 {
		t.Fatalf("zwj cluster: %d", got)
	}
	if controlCount("plain") != 0 {
		t.Fatal("plain text counted")
	}
}

func TestNoClipboardFlag(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("LC_ALL", "C.UTF-8")
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("MODEL_INSPECTOR_NO_OSC52", "1")
	ui := bootConfig(t, loadCfg(t, fixture.ConfigQwenLike()), 140, 40)
	var buf bytes.Buffer
	yes := true
	ui.cfg.copyTTY = &yes
	ui.cfg.copyTo = &buf
	var x tea.Model = ui
	x, _ = x.Update(keyMsg("y"))
	mm := x.(Model)
	if buf.Len() != 0 || !strings.Contains(mm.notice, "OSC 52 disabled") {
		t.Fatalf("env notice %q buf %d", mm.notice, buf.Len())
	}

	t.Setenv("MODEL_INSPECTOR_NO_OSC52", "")
	ui = bootConfig(t, loadCfg(t, fixture.ConfigQwenLike()), 140, 40)
	ui.noClipboard = true
	ui.cfg.copyTTY = &yes
	ui.cfg.copyTo = &buf
	buf.Reset()
	x = ui
	x, _ = x.Update(keyMsg("y"))
	mm = x.(Model)
	if buf.Len() != 0 || !strings.Contains(mm.notice, "OSC 52 disabled") {
		t.Fatalf("flag notice %q buf %d", mm.notice, buf.Len())
	}
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
	x, _ = x.Update(keyMsg("4"))
	mm, ok := x.(Model)
	if !ok {
		t.Fatalf("%T", x)
	}
	return mm
}
