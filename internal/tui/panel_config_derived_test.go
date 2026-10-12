package tui

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cecilkootz/model-inspector/internal/fixture"
	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/sanitize"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

func derivedModels(t *testing.T) map[string]*model.Model {
	t.Helper()
	return map[string]*model.Model{
		"tiny": loadTiny(t),
		"qwen": loadCfg(t, fixture.ConfigQwenLike()),
		"sym":  loadCfg(t, []byte(`{"quantization":{"bits":4,"group_size":32,"mode":"symmetric"}}`)),
		"disagree": loadCfg(t, []byte(`{"num_hidden_layers":8,"full_attention_interval":4,"layer_types":["full_attention",`+
			`"linear_attention","linear_attention","linear_attention","linear_attention","linear_attention","linear_attention","linear_attention"]}`)),
		"noshared": loadCfg(t, []byte(`{"num_experts":512,"num_experts_per_tok":10,"hidden_size":256,"num_attention_heads":2}`)),
	}
}

// derivedFields are the key fields that carry a derivation, with the number
// of "down" presses that select each.
func derivedFields(t *testing.T, m *model.Model) (rows []fieldLine, downs []int) {
	t.Helper()
	x := press(shellModel(t, m, 140, 40), "4")
	_ = x.View()
	p := cfgPanel(x)
	lines, _ := p.listLines()
	n := 0
	for _, ln := range lines {
		if ln.kind != fieldRow {
			continue
		}
		if ln.derived != "" && !ln.bad {
			rows, downs = append(rows, ln), append(downs, n)
		}
		n++
	}
	return rows, downs
}

func fieldFull(f fieldLine) string {
	s := sanitize.From(f.value).String()
	if f.unit != "" {
		s += " " + f.unit
	}
	return s
}

// Every derived field shows its full derivation under the tree's VALUE
// section, beside a meter where the field has one, and a field without a
// derivation has no DERIVED section at all.
func TestPanelConfigDerivedDetail(t *testing.T) {
	seen := 0
	for name, m := range derivedModels(t) {
		rows, downs := derivedFields(t, m)
		for i, f := range rows {
			seen++
			keys := append([]string{"4"}, repeat("down", downs[i])...)
			for _, sz := range [][2]int{{140, 40}, {100, 30}} {
				view := cfgFrame(t, m, sz[0], sz[1], "colorless", keys...)
				det := detailText(strings.Split(view, "\n"), sz[0])
				want := strings.Join(strings.Fields(fieldFull(f)+" "+sanitize.From(f.derived).String()), " ")
				label := fmt.Sprintf("%s/%s/%dx%d", name, f.label, sz[0], sz[1])
				if !strings.Contains(det, "DERIVED "+f.label+" "+want) {
					t.Errorf("%s: detail lacks the derivation %q\n%s", label, want, view)
				}
				if meter := fmt.Sprintf("%.1f%% %s", f.meter*100, f.meterBy); f.meterOn && !strings.Contains(det, meter) {
					t.Errorf("%s: no meter label %q\n%s", label, meter, view)
				}
			}
		}
		// Fields with nothing derived stay quiet.
		x := press(shellModel(t, m, 140, 40), "4")
		for i := 0; i < 40; i++ {
			_ = x.View()
			p := cfgPanel(x)
			lines, _ := p.listLines()
			f := lines[p.cursor()]
			tree := detailText(screenOf(x), 140)
			if has := strings.Contains(tree, "DERIVED"); has != (f.derived != "" && !f.bad && (f.jump < 0 || f.jump == x.(Model).cfg.cursor)) {
				t.Errorf("%s/%s: DERIVED present=%v for derived %q", name, f.label, has, f.derived)
			}
			before := p.cursor()
			x = press(x, "down")
			if cfgPanel(x).cursor() == before {
				break
			}
		}
	}
	if seen < 12 {
		t.Fatalf("only %d derived fields exercised", seen)
	}
}

// The tree cursor leaving the field's node drops the section: it would
// describe a value the pane no longer shows.
func TestPanelConfigDerivedFollowsTree(t *testing.T) {
	m := loadCfg(t, fixture.ConfigQwenLike())
	rows, downs := derivedFields(t, m)
	var k int
	for i, f := range rows {
		if f.label == "heads q / kv" {
			k = downs[i]
		}
	}
	x := press(shellModel(t, m, 140, 40), append([]string{"4"}, repeat("down", k)...)...)
	if !strings.Contains(detailText(screenOf(x), 140), "12:1 GQA") {
		t.Fatalf("no derivation on the field\n%s", strings.Join(screenOf(x), "\n"))
	}
	x = press(x, "0", "up")
	_ = x.View()
	if strings.Contains(detailText(screenOf(x), 140), "DERIVED") {
		t.Fatalf("derivation stayed after the tree cursor moved\n%s", strings.Join(screenOf(x), "\n"))
	}
}

// rowCutOK reports whether a list row ends cleanly: with the whole
// derivation, with the value alone, or with an end-elided derivation.
func rowCutOK(f fieldLine, row string) bool {
	row = strings.TrimRight(ui.Strip(row), " ")
	full := fieldFull(f)
	note := sanitize.From(f.derived).String()
	switch {
	case strings.HasSuffix(row, full+" "+note), strings.HasSuffix(row, full):
		return true
	}
	i := strings.LastIndex(row, full+" ")
	if i < 0 || !strings.HasSuffix(row, "…") {
		return false
	}
	cut := strings.TrimSuffix(row[i+len(full)+1:], "…")
	return strings.HasPrefix(note, cut) && ui.Width(cut) >= 7
}

// Rows show the derivation when the side column has room, cut it cleanly
// when it does not, and never exceed the column, in the wide, narrow and
// portrait layouts.
func TestPanelConfigDerivedRows(t *testing.T) {
	m := loadCfg(t, fixture.ConfigQwenLike())
	wantFull := map[string]string{
		"layers": "= 36 linear · 12 full", "heads q / kv": "= 12:1 GQA",
		"scheme": "= affine", "effective": "= 4 + 2×16 / 32",
	}
	for _, sz := range [][2]int{{140, 40}, {100, 30}, {84, 24}, {80, 24}, {60, 20}} {
		x := press(shellModel(t, m, sz[0], sz[1]), "4")
		_ = x.View()
		iw := shellSideWidth(sz[0]) - 4
		if x.(Model).shellPortrait() {
			iw = sz[0] - 4
		}
		p := cfgPanel(x)
		lines, _ := p.listLines()
		rows := p.rows(iw)
		if len(rows) != len(lines) {
			t.Fatalf("%v: %d rows for %d lines", sz, len(rows), len(lines))
		}
		shown, cut := 0, 0
		for i, f := range lines {
			if w := ui.Width(rows[i]); w != iw {
				t.Fatalf("%v %s: row is %d cells, want %d: %q", sz, f.label, w, iw, ui.Strip(rows[i]))
			}
			if f.kind != fieldRow || f.derived == "" {
				continue
			}
			if !rowCutOK(f, rows[i]) {
				t.Errorf("%v %s: ragged cut %q", sz, f.label, ui.Strip(rows[i]))
			}
			note := sanitize.From(f.derived).String()
			switch row := strings.TrimRight(ui.Strip(rows[i]), " "); {
			case strings.HasSuffix(row, note):
				shown++
				if sz[0] == 140 && wantFull[f.label] == note {
					delete(wantFull, f.label)
				}
			case strings.HasSuffix(row, "…"):
				cut++
			}
		}
		if sz[0] == 140 {
			if len(wantFull) > 0 {
				t.Errorf("140x40 rows lack %v", wantFull)
			}
			if shown < 4 {
				t.Errorf("only %d full derivations at 140x40", shown)
			}
		}
		if sz[0] <= 84 && cut+shown == 0 && sz[0] != 60 {
			t.Errorf("%v: no derivations at all", sz)
		}
		cfgFrame(t, m, sz[0], sz[1], "colorless", "4", "down", "down", "down")
	}
}

// A flagged field, or one without a derivation, shows its value alone.
func TestPanelConfigDerivedRowQuiet(t *testing.T) {
	m := loadCfg(t, []byte(`{"num_hidden_layers":1e-400,"hidden_size":8,"layer_types":["full_attention"]}`))
	x := press(shellModel(t, m, 140, 40), "4")
	_ = x.View()
	rows := strings.Join(cfgPanel(x).rows(shellSideWidth(140)-4), "\n")
	rows = ui.Strip(rows)
	if strings.Contains(rows, "= ") {
		t.Fatalf("derivation beside a flagged or plain field:\n%s", rows)
	}
	if !strings.Contains(rows, "1e-400 ! out of") {
		t.Fatalf("the warn mark is gone:\n%s", rows)
	}
}

func TestPanelConfigDerivedSummary(t *testing.T) {
	m := loadCfg(t, fixture.ConfigQwenLike())
	x := press(shellModel(t, m, 140, 40), "1")
	view := stripView(x.View())
	if !strings.Contains(view, "layers   48 layers = 36 linear · 12 full") {
		t.Fatalf("summary lacks the layer mix\n%s", view)
	}
	narrow := stripView(press(shellModel(t, m, 84, 24), "1").View())
	for _, ln := range strings.Split(narrow, "\n") {
		if strings.Contains(ln, "context") && !strings.Contains(ln, "262,144 tok") {
			t.Fatalf("context row lost its value: %q", ln)
		}
	}
}

// Hostile config text reaches derivations only through sanitize, and numbers
// that would wrap or go non-finite derive nothing instead of something wrong.
func TestPanelConfigDerivedHostile(t *testing.T) {
	const max = "9223372036854775807"
	cfgs := map[string]string{
		"ropetype":   `{"max_position_embeddings":262144,"rope_scaling":{"factor":4,"type":"ya\u001b]0;PWN\u0007r‮n<x>` + strings.Repeat("w", 400) + `"}}`,
		"mode":       `{"quantization":{"bits":4,"group_size":32,"mode":"sy\u001bm‮<"}}`,
		"partial":    `{"rope_theta":1,"partial_rotary_factor":"\u001b[31m<x>"}`,
		"headoverfl": `{"head_dim":` + max + `,"num_attention_heads":4,"num_key_value_heads":2}`,
		"headneg":    `{"head_dim":-8,"num_attention_heads":4,"num_key_value_heads":2}`,
		"factorbig":  `{"max_position_embeddings":4096,"rope_scaling":{"factor":1e308,"type":"yarn"}}`,
		"factorneg":  `{"max_position_embeddings":4096,"rope_scaling":{"factor":-2,"type":"yarn"}}`,
		"factornan":  `{"max_position_embeddings":4096,"rope_scaling":{"factor":NaN,"type":"yarn"}}`,
		"factortiny": `{"max_position_embeddings":4096,"rope_scaling":{"factor":1e-400,"type":"yarn"}}`,
		"ctxmax":     `{"max_position_embeddings":` + max + `,"rope_scaling":{"factor":2,"type":"yarn"}}`,
		"sharedovfl": `{"num_experts":8,"num_experts_per_tok":` + max + `,"n_shared_experts":5}`,
		"pertokneg":  `{"num_experts":8,"num_experts_per_tok":-3,"n_shared_experts":1}`,
		"nanheads":   `{"num_attention_heads":NaN,"num_key_value_heads":Infinity,"hidden_size":8}`,
		"hugeheads":  `{"num_attention_heads":1e999,"num_key_value_heads":-1e-999,"hidden_size":8}`,
		"layersneg":  `{"num_hidden_layers":-4,"full_attention_interval":-2,"layer_types":["full_attention"]}`,
		"visionzero": `{"vision_config":{"depth":3,"hidden_size":8,"num_heads":0}}`,
		"visionmin":  `{"vision_config":{"depth":3,"hidden_size":-9223372036854775808,"num_heads":-1}}`,
	}
	for name, raw := range cfgs {
		m := loadCfg(t, []byte(raw))
		x := press(shellModel(t, m, 140, 40), "4")
		_ = x.View()
		lines, _ := cfgPanel(x).listLines()
		for _, ln := range lines {
			if ln.kind != fieldRow {
				continue
			}
			switch name {
			case "headoverfl", "headneg":
				if ln.label == "head_dim" && ln.derived != "" {
					t.Errorf("%s: head_dim derived %q", name, ln.derived)
				}
			case "factorbig", "factorneg", "factornan", "factortiny", "ctxmax":
				if ln.label == "context" && strings.Contains(ln.derived, "×") {
					t.Errorf("%s: context scaled to %q", name, ln.derived)
				}
			case "sharedovfl", "pertokneg":
				if ln.label == "per token" && ln.derived != "" {
					t.Errorf("%s: per token derived %q", name, ln.derived)
				}
			case "nanheads", "hugeheads", "visionzero", "visionmin", "layersneg":
				if ln.derived != "" && ln.label != "layers" && !strings.Contains(ln.derived, "layer_types") {
					t.Errorf("%s: %s derived %q", name, ln.label, ln.derived)
				}
			}
			if strings.Contains(ln.derived, "-") && strings.Contains(ln.derived, "wide") {
				t.Errorf("%s: negative width %q", name, ln.derived)
			}
		}
		p := cfgPanel(x)
		for n := 0; n < len(lines); n++ {
			for _, sz := range [][2]int{{140, 40}, {84, 24}, {80, 24}} {
				v := press(shellModel(t, m, sz[0], sz[1]), append([]string{"4"}, repeat("down", n)...)...)
				view := v.View()
				assertFrame(t, view, sz[0], sz[1])
				if raw, bad := rawBad(view); bad {
					t.Fatalf("%s %v row %d: raw %s", name, sz, n, raw)
				}
				iw := shellSideWidth(sz[0]) - 4
				for _, r := range cfgPanel(v).rows(iw) {
					if w := ui.Width(r); w != iw {
						t.Fatalf("%s: row is %d cells, want %d", name, w, iw)
					}
				}
			}
		}
		for _, s := range p.rows(400) {
			if raw, bad := rawBad(s); bad {
				t.Fatalf("%s: raw %s", name, raw)
			}
		}
	}
}

func TestDerivedArithmetic(t *testing.T) {
	for _, c := range []struct {
		a, b int64
		ok   bool
	}{
		{3, 4, true}, {0, math.MaxInt64, true}, {math.MaxInt64, 1, true}, {math.MaxInt64, 2, false},
		{math.MinInt64, -1, false}, {-1, math.MinInt64, false}, {1 << 32, 1 << 31, false}, {1 << 31, 1 << 31, true},
		{-3, 4, true}, {math.MinInt64, 1, true},
	} {
		if _, ok := mulInt(c.a, c.b); ok != c.ok {
			t.Errorf("mulInt(%d, %d) ok=%v", c.a, c.b, ok)
		}
	}
	for _, c := range []struct {
		a, b int64
		ok   bool
	}{
		{1, 2, true}, {math.MaxInt64, 1, false}, {math.MaxInt64, 0, true}, {math.MinInt64, -1, false}, {math.MaxInt64, -1, true},
	} {
		if _, ok := addInt(c.a, c.b); ok != c.ok {
			t.Errorf("addInt(%d, %d) ok=%v", c.a, c.b, ok)
		}
	}
	for _, c := range []struct {
		n  int64
		f  float64
		ok bool
	}{
		{4096, 4, true}, {4096, 0, false}, {4096, -1, false}, {4096, math.Inf(1), false}, {4096, math.NaN(), false},
		{4096, 1e308, false}, {math.MaxInt64, 2, false}, {0, 2, false}, {-5, 2, false}, {1 << 40, 0.5, true},
	} {
		if _, ok := scaleInt(c.n, c.f); ok != c.ok {
			t.Errorf("scaleInt(%d, %v) ok=%v", c.n, c.f, ok)
		}
	}
	if got := widthText(1<<40, 1<<30, 2); got != "" {
		t.Errorf("widthText overflow: %q", got)
	}
	if got := widthText(256, 24, 2); !strings.Contains(got, "6,144") {
		t.Errorf("widthText: %q", got)
	}
}

// The derivation path is on the hot path of every frame with Config
// visible; it must stay inside the same budgets as the rest of the panel.
func TestPanelConfigDerivedBudgets(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  []byte
		keys []string
	}{
		{"huge", fixture.ConfigHuge(), nil},
		{"qwen", fixture.ConfigQwenLike(), append([]string{"4"}, repeat("down", 10)...)},
	} {
		m := loadCfg(t, tc.raw)
		keys := tc.keys
		if keys == nil {
			keys = []string{"4"}
		}
		x := press(shellModel(t, m, 140, 40), keys...)
		_ = x.View()
		_ = x.View()
		if view := median(9, func() { _ = x.View() }); view > budget(2*time.Millisecond) {
			t.Errorf("%s: cached View %s", tc.name, view)
		}
		if key := median(9, func() { x, _ = x.Update(tea.KeyMsg{Type: tea.KeyDown}) }); key > budget(500*time.Microsecond) {
			t.Errorf("%s: Update key %s", tc.name, key)
		}
		if rs := median(9, func() { x, _ = x.Update(tea.WindowSizeMsg{Width: 120, Height: 40}) }); rs > budget(5*time.Millisecond) {
			t.Errorf("%s: Resize %s", tc.name, rs)
		}
	}
}
