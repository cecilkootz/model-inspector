package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/cecilkootz/model-inspector/internal/fixture"
	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/scan"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

func cfgPanel(x tea.Model) *configPanel {
	cur := x.(Model)
	return cur.sidePanels()[3].(*configPanel)
}

// detailText is the detail pane's text with borders dropped and runs of
// whitespace collapsed, so a phrase wrapped across rows still matches.
func detailText(screen []string, w int) string {
	var parts []string
	for _, ln := range screen[1 : len(screen)-1] {
		cell := ansi.Cut(ln, shellSideWidth(w), w)
		cell = strings.Trim(cell, "│╭╮╰╯─ ")
		parts = append(parts, cell)
	}
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

func cfgFrame(t *testing.T, m *model.Model, w, h int, profile string, keys ...string) string {
	t.Helper()
	view := renderPrepared(t, m, scan.Run(m), w, h, profile, keys, Options{Layout: LayoutPanels})
	assertFrame(t, view, w, h)
	return view
}

// Moving the field selection puts the tree cursor on that field's node, and
// the detail title is the node's path.
func TestPanelConfigSelectionDrivesTree(t *testing.T) {
	m := loadCfg(t, fixture.ConfigQwenLike())
	x := press(shellModel(t, m, 140, 40), "4")
	seen := 0
	for i := 0; i < 40; i++ {
		_ = x.View()
		p := cfgPanel(x)
		lines, at := p.listLines()
		f := lines[p.cursor()]
		if f.kind != fieldRow {
			t.Fatalf("cursor %d is on a %d line", p.cursor(), f.kind)
		}
		if f.jump >= 0 {
			seen++
			cur := x.(Model)
			if cur.cfg.cursor != f.jump {
				t.Fatalf("field %q: tree cursor %d, want %d", f.label, cur.cfg.cursor, f.jump)
			}
			path := strings.TrimPrefix(displayPath(cur.cfg, f.jump), ".")
			if title := screenOf(x)[1]; !strings.Contains(title, "[0] "+path) {
				t.Fatalf("field %q: title %q lacks %q", f.label, title, path)
			}
			key := cur.cfg.tree.Nodes[f.jump].Key
			view := cfgFrame(t, m, 140, 40, "colorless", append([]string{"4"}, repeat("down", i)...)...)
			found := false
			for _, ln := range strings.Split(view, "\n") {
				if c := ansi.Cut(ln, shellSideWidth(140), 140); strings.Contains(c, ">") && strings.Contains(c, key) {
					found = true
				}
			}
			if !found {
				t.Fatalf("field %q: no highlighted tree row for %q\n%s", f.label, key, view)
			}
		}
		_ = at
		before := cfgPanel(x).cursor()
		x = press(x, "down")
		if cfgPanel(x).cursor() == before {
			break
		}
	}
	if seen < 10 {
		t.Fatalf("walked only %d fields", seen)
	}
}

func repeat(k string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = k
	}
	return out
}

// Group labels are rows the cursor never rests on.
func TestPanelConfigGroupLabels(t *testing.T) {
	m := loadCfg(t, fixture.ConfigQwenLike())
	x := press(shellModel(t, m, 140, 40), "4")
	iw := shellSideWidth(140) - 4
	rows := cfgPanel(x).rows(iw)
	text := strings.Join(rows, "\n")
	for _, g := range []string{"TEXT", "ATTENTION", "MOE"} {
		if !strings.Contains(ui.Strip(text), g) {
			t.Errorf("missing group label %s", g)
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(ui.Strip(rows[0])), "TEXT") {
		t.Fatalf("first row %q", ui.Strip(rows[0]))
	}
	check := func(label string) {
		t.Helper()
		p := cfgPanel(x)
		lines, _ := p.listLines()
		if k := lines[p.cursor()].kind; k != fieldRow {
			t.Fatalf("%s: cursor %d rests on a non-field (kind %d)", label, p.cursor(), k)
		}
	}
	check("start")
	x = press(x, "k", "up")
	check("up at top")
	for i := 0; i < 60; i++ {
		x = press(x, "down")
		check(fmt.Sprintf("down %d", i))
	}
	x = press(x, "g")
	check("g")
	x = press(x, "G")
	check("G")

	// Clicking a label selects the next field.
	cur := press(shellModel(t, m, 140, 40), "4", "down", "down").(Model)
	_ = cur.View()
	var labelRow = -1
	lines, _ := cfgPanel(cur).listLines()
	for i, ln := range lines {
		if ln.kind == fieldHeader && ln.label == "ATTENTION" {
			labelRow = i
		}
	}
	p := cfgPanel(cur)
	p.move(labelRow - p.cursor())
	if lines[p.cursor()].kind != fieldRow || p.cursor() != labelRow+1 {
		t.Fatalf("label click landed on %d, want %d", p.cursor(), labelRow+1)
	}
}

func TestPanelConfigSummary(t *testing.T) {
	iw := shellSideWidth(120) - 4
	sum := func(raw []byte) []string {
		x := press(shellModel(t, loadCfg(t, raw), 120, 30), "4")
		var out []string
		for _, r := range cfgPanel(x).summary(iw) {
			out = append(out, strings.TrimSpace(ui.Strip(r)))
		}
		return out
	}
	got := sum(fixture.ConfigQwenLike())
	if len(got) != 3 || !strings.HasPrefix(got[0], "model_type") || !strings.HasPrefix(got[1], "layers") || !strings.HasPrefix(got[2], "context") {
		t.Fatalf("summary %q", got)
	}
	got = sum([]byte(`{"hidden_size":8,"vocab_size":2,"num_experts":4,"head_dim":4}`))
	if len(got) != 3 || !strings.HasPrefix(got[0], "hidden_size") {
		t.Fatalf("fallback summary %q", got)
	}
	if got := sum(fixture.ConfigNotObject()); len(got) != 1 || !strings.Contains(got[0], "config.json is not a JSON") {
		t.Fatalf("note summary %q", got)
	}
	if got := sum(nil); len(got) != 0 {
		t.Fatalf("no config summary %q", got)
	}
}

// Every problem state is the detail content, with today's wording and no
// legacy headings.
func TestPanelConfigCards(t *testing.T) {
	nan := loadCfg(t, []byte(`{"model_type":"x","text_config":{"hidden_size":NaN}}`))
	cases := []struct {
		name string
		m    *model.Model
		want []string
	}{
		{"none", loadCfg(t, nil), []string{"NO CONFIG", "No config.json in this snapshot.", "nothing to show here"}},
		{"unreadable", unreadableModel(t), []string{"FAIL", "UNREADABLE", "config.json is unreadable.", "could not be read"}},
		{"oversize", oversizeModel(t), []string{"TOO LARGE", "71.4 MiB", "64 MiB", "was not read"}},
		{"malformed", loadCfg(t, fixture.ConfigMalformed()), []string{"PARSE ERROR", "config.json did not parse.", "here", "need a parsed config"}},
		{"dupkey", loadCfg(t, fixture.ConfigDupKey()), []string{"config.json did not parse.", "duplicate key"}},
		{"deep", loadCfg(t, fixture.ConfigDeep()), []string{"nesting deeper than 32"}},
		{"nan", nan, []string{"model_type", "hidden_size: NaN !"}},
		{"nodes", loadCfg(t, fixture.ConfigHuge()), []string{"NODE LIMIT", "Tree disabled", "limit", "first 1 MiB"}},
	}
	for _, c := range cases {
		for _, sz := range [][2]int{{140, 40}, {100, 30}, {84, 24}} {
			t.Run(fmt.Sprintf("%s/%dx%d", c.name, sz[0], sz[1]), func(t *testing.T) {
				view := cfgFrame(t, c.m, sz[0], sz[1], "colorless", "4")
				if raw, bad := rawBad(view); bad {
					t.Fatalf("raw %s", raw)
				}
				det := detailText(strings.Split(view, "\n"), sz[0])
				for _, w := range c.want {
					if !strings.Contains(det, w) {
						t.Errorf("detail lacks %q:\n%s", w, view)
					}
				}
				for _, gone := range []string{"KEY FIELDS", "CONFIG.JSON", "Identity/Layers"} {
					if strings.Contains(det, gone) {
						t.Errorf("legacy heading %q in the detail pane", gone)
					}
				}
			})
		}
	}
}

func TestPanelConfigRaw(t *testing.T) {
	m := loadCfg(t, []byte("{\n  \"model_type\": \"x\",\n  \"esc\\u001bkey\": 1,\n  \"b\": 2,\n  \"c\": 3\n}\n"))
	x := press(shellModel(t, m, 120, 30), "4")
	x = press(x, "r")
	if !x.(Model).cfg.raw {
		t.Fatal("r did not turn raw on")
	}
	view := stripView(x.View())
	assertFrame(t, x.View(), 120, 30)
	if raw, bad := rawBad(x.View()); bad {
		t.Fatalf("raw %s", raw)
	}
	for _, want := range []string{"RAW", `u001bkey`, "[0] model_type"} {
		if !strings.Contains(view, want) {
			t.Errorf("raw view lacks %q\n%s", want, view)
		}
	}
	if !strings.Contains(screenOf(x)[1], "raw") {
		t.Errorf("title tag: %q", screenOf(x)[1])
	}
	// The detail pane scrolls the text, not the tree.
	tree := x.(Model).cfg.cursor
	x = press(x, "0", "down", "down")
	_ = x.View()
	cur := x.(Model)
	if cur.cfg.rawLine < 2 || cur.cfg.cursor != tree {
		t.Fatalf("raw line %d, tree cursor %d -> %d", cur.cfg.rawLine, tree, cur.cfg.cursor)
	}
	x = press(x, "r")
	if x.(Model).cfg.raw || !strings.Contains(strings.Join(screenOf(x), "\n"), "config.json {") {
		t.Fatal("second r did not return to the tree")
	}

	// No file, nothing to toggle.
	x = press(shellModel(t, loadCfg(t, nil), 120, 30), "4", "r")
	if x.(Model).cfg.raw {
		t.Fatal("raw turned on without a config")
	}
	// A config that does not parse still has raw text.
	x = press(shellModel(t, loadCfg(t, fixture.ConfigMalformed()), 120, 30), "4", "r")
	if !strings.Contains(strings.Join(screenOf(x), "\n"), "RAW") {
		t.Fatal("malformed config has no raw view")
	}
}

func TestPanelConfigTreeFolds(t *testing.T) {
	m := loadCfg(t, fixture.ConfigQwenLike())
	x := press(shellModel(t, m, 140, 40), "4", "0")
	_ = x.View()
	cur := x.(Model)
	id := cur.cfg.cursor
	x = press(x, "down", "down")
	_ = x.View()
	if x.(Model).cfg.cursor == id {
		t.Fatal("down did not move the tree cursor")
	}
	// Collapse the parent, then expand it again.
	x = press(x, "left")
	_ = x.View()
	if x.(Model).cfg.cursor == id {
		t.Fatal("left did not move to the parent")
	}
	x = press(x, "left", "right")
	_ = x.View()
}

// Hostile and oversized configs fill every frame without raw bytes, and the
// detail renderer itself never exceeds its box.
func TestPanelConfigHostile(t *testing.T) {
	files := map[string][]byte{
		"hostile": fixture.ConfigHostile(), "deep": fixture.ConfigDeep(), "huge": fixture.ConfigHuge(),
		"rawctl":  []byte("{\"a\":\"x\x1b]0;PWN\x07\u202e\n"),
		"nan":     []byte(`{"text_config":{"hidden_size":NaN,"vocab_size":-Infinity}}`),
		"qwen":    fixture.ConfigQwenLike(),
		"notobj":  fixture.ConfigNotObject(),
		"wideval": []byte(`{"model_type":"` + strings.Repeat("w", 400) + `","text_config":{"hidden_size":` + strings.Repeat("9", 300) + `}}`),
	}
	keysets := [][]string{
		{"4"}, {"4", "down", "down", "down"}, {"4", "0", "down", "right", "enter", "G", "g"},
		{"4", "r"}, {"4", "r", "0", "G", "up"}, {"4", "G", "0", "left", "left", "space"},
	}
	for name, raw := range files {
		m := loadCfg(t, raw)
		for _, sz := range [][2]int{{140, 40}, {100, 30}, {84, 24}} {
			x := shellModel(t, m, sz[0], sz[1])
			for _, keys := range keysets {
				label := fmt.Sprintf("%s %dx%d %v", name, sz[0], sz[1], keys)
				v := press(x, keys...)
				view := v.View()
				assertFrame(t, view, sz[0], sz[1])
				if raw, bad := rawBad(view); bad {
					t.Fatalf("%s: raw %s", label, raw)
				}
				p := cfgPanel(v)
				dw, dh := sz[0]-shellSideWidth(sz[0])-4, sz[1]-4
				lines := p.detail(dw, dh)
				if len(lines) > dh {
					t.Fatalf("%s: %d detail lines, box is %d", label, len(lines), dh)
				}
				for i, ln := range lines {
					if ui.Width(ln) > dw {
						t.Fatalf("%s: detail row %d is %d cells, box is %d: %q", label, i, ui.Width(ln), dw, ln)
					}
				}
			}
		}
	}
}

func TestPanelConfigBudgets(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []string
	}{{"tree", nil}, {"raw", []string{"r"}}} {
		m := loadCfg(t, fixture.ConfigHuge())
		x := press(shellModel(t, m, 140, 40), append([]string{"4"}, tc.keys...)...)
		_ = x.View()
		_ = x.View()
		view := median(9, func() { _ = x.View() })
		if view > budget(2*time.Millisecond) {
			t.Errorf("%s: cached View %s", tc.name, view)
		}
		key := median(9, func() { x, _ = x.Update(tea.KeyMsg{Type: tea.KeyDown}) })
		if key > budget(500*time.Microsecond) {
			t.Errorf("%s: Update key %s", tc.name, key)
		}
		resize := median(9, func() { x, _ = x.Update(tea.WindowSizeMsg{Width: 120, Height: 40}) })
		if resize > budget(5*time.Millisecond) {
			t.Errorf("%s: Resize %s", tc.name, resize)
		}
	}
}
