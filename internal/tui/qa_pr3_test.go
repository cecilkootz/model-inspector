package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/cecilkootz/model-inspector/internal/fixture"
	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/scan"
)

const hostileP3 = "h\u009b[31m\u202e\u2066\u200b\U000E0041\u2028\u00ad\x1b]0;PWN\x07\x1b[2J<x>"

func hostileDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	d := filepath.Join(root, "dir"+strings.ReplaceAll(hostileP3, "/", ""))
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	hb, err := json.Marshal(map[string]any{
		"__metadata__": map[string]string{"format": hostileP3, "k" + hostileP3: hostileP3},
		"model.layers.0.self_attn.q_proj.weight" + hostileP3: map[string]any{"dtype": "F32", "shape": []int{4}, "data_offsets": []int{0, 16}},
		"model.layers.0.mlp.weird" + hostileP3:               map[string]any{"dtype": "X" + hostileP3, "shape": []int{1}, "data_offsets": []int{16, 20}},
		"model.embed_tokens.weight":                          map[string]any{"dtype": "F32", "shape": []int{1}, "data_offsets": []int{20, 24}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "m"+hostileP3+".safetensors"), fixture.EncodeHeader(hb, make([]byte, 24)), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := json.Marshal(map[string]any{
		"model_type": hostileP3, "architectures": []string{hostileP3},
		"layer_types": []string{hostileP3, "full_attention"}, "hidden_size": 8, "num_hidden_layers": 1,
		hostileP3: hostileP3, "torch_dtype": hostileP3,
		"auto_map": map[string]any{"AutoModel" + hostileP3: "a/b--" + hostileP3},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "config.json"), cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "x"+hostileP3+".py"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(d, "locked"+hostileP3)
	if err := os.Mkdir(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	return d
}

// allViews drives every panel, its detail pane, help, the palette and the
// filter, and returns each frame.
func allViews(m *model.Model, w, h int) []string {
	var x tea.Model = New(m, scan.Run(m))
	x, _ = x.Update(tea.WindowSizeMsg{Width: w, Height: h})
	vs := []string{x.View()}
	key := func(s string) {
		x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
		vs = append(vs, x.View())
	}
	sp := func(k tea.KeyType) {
		x, _ = x.Update(tea.KeyMsg{Type: k})
		vs = append(vs, x.View())
	}
	key("?")
	sp(tea.KeyEsc)
	key(":")
	sp(tea.KeyEsc)
	for _, k := range []string{"1", "2", "3", "4"} {
		key(k)
		sp(tea.KeyDown)
		sp(tea.KeyEnter)
		sp(tea.KeyPgDown)
		sp(tea.KeyEnd)
		sp(tea.KeyEsc)
		key("/")
		key("e")
		sp(tea.KeyEsc)
		key("]")
	}
	sp(tea.KeyTab)
	sp(tea.KeyShiftTab)
	return vs
}

func checkViews(t *testing.T, label string, vs []string, noESC bool) {
	t.Helper()
	for i, v := range vs {
		for _, bad := range []string{"\x1b]0;", "\x1b[2J", "\x1b[31m", "\u009b", "\u202e", "\u2066", "\u200b", "\U000E0041", "\u2028", "\u00ad", "\x07"} {
			if strings.Contains(v, bad) {
				t.Errorf("%s view %d contains raw %q", label, i, bad)
			}
		}
		if noESC && strings.Contains(v, "\x1b") {
			t.Errorf("%s view %d contains ESC under NO_COLOR", label, i)
		}
		for _, r := range v {
			if (r < 0x20 && r != '\n' && r != 0x1b) || (r >= 0x7f && r <= 0x9f) {
				t.Errorf("%s view %d raw %U", label, i, r)
				break
			}
		}
	}
}

func TestP3_HostileViews(t *testing.T) {
	d := hostileDir(t)
	m, err := model.Load(d)
	if err != nil {
		t.Fatal(err)
	}
	prev, had := os.LookupEnv("NO_COLOR")
	t.Cleanup(func() {
		if had {
			os.Setenv("NO_COLOR", prev)
		} else {
			os.Unsetenv("NO_COLOR")
		}
		lipgloss.SetColorProfile(termenv.Ascii)
		SetNoColor(false)
	})
	for _, nc := range []bool{false, true} {
		SetNoColor(false)
		if nc {
			t.Setenv("NO_COLOR", "1")
		} else {
			os.Unsetenv("NO_COLOR")
		}
		for _, sz := range [][2]int{{80, 24}, {60, 16}, {120, 40}, {20, 5}, {1, 1}} {
			lipgloss.SetColorProfile(termenv.TrueColor)
			vs := allViews(m, sz[0], sz[1])
			if len(vs) == 0 {
				t.Fatalf("no views at %dx%d", sz[0], sz[1])
			}
			escs := 0
			for _, v := range vs {
				escs += strings.Count(v, "\x1b")
			}
			label := fmt.Sprintf("nocolor=%v %dx%d", nc, sz[0], sz[1])
			checkViews(t, label, vs, nc)
			// 80×24 is a real screen. With NO_COLOR unset and TrueColor
			// forced, the styles have to emit ESC. The too-small
			// screens are plain text.
			if !nc && sz == [2]int{80, 24} && escs == 0 {
				t.Errorf("%s: color path produced no ESC", label)
			}
			if nc && escs != 0 {
				t.Errorf("%s: %d ESC with NO_COLOR set", label, escs)
			}
		}
	}
}

func TestP3_StorageOverflowPanic(t *testing.T) {
	d := t.TempDir()
	big := int64(1)<<62 + int64(1)<<61
	hb := fmt.Sprintf(`{"model.layers.0.mlp.a.weight":{"dtype":"U8","shape":[%d],"data_offsets":[0,%d]},"model.layers.0.mlp.b.weight":{"dtype":"U8","shape":[%d],"data_offsets":[0,%d]},"model.embed_tokens.weight":{"dtype":"F32","shape":[1],"data_offsets":[0,4]}}`, big, big, big, big)
	if err := os.WriteFile(filepath.Join(d, "model.safetensors"), fixture.EncodeHeader([]byte(hb), make([]byte, 4)), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(d)
	if err != nil {
		t.Fatal(err)
	}
	if m.TensorBytes < 0 {
		t.Fatalf("TensorBytes %d", m.TensorBytes)
	}
	if !m.Partial {
		t.Fatal("overflow did not mark the inspection partial")
	}
	for _, c := range m.StorageBreakdown() {
		if c.Bytes < 0 || c.Pct < 0 {
			t.Fatalf("storage %s bytes=%d pct=%v", c.Class, c.Bytes, c.Pct)
		}
	}
	for _, c := range model.MergeStorage(m, m.StorageBreakdown(), 4) {
		if c.Bytes < 0 || c.Pct < 0 {
			t.Fatalf("merged %s bytes=%d pct=%v", c.Class, c.Bytes, c.Pct)
		}
	}
	mem := m.MemoryEstimate()
	if mem.WeightsBytes < 0 || mem.TextOnlyBytes < 0 || mem.HostCapableBytes < 0 || mem.AcceleratorMinBytes < 0 {
		t.Fatalf("memory %+v", mem)
	}
	for _, h := range m.HostCapable() {
		if h.Bytes < 0 {
			t.Fatalf("host %+v", h)
		}
	}
	vs := allViews(m, 80, 24)
	if len(vs) == 0 || vs[0] == "" {
		t.Fatal("first screen was empty")
	}
}
