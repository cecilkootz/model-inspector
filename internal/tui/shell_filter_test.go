package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cecilkootz/model-inspector/internal/fixture"
	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/scan"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

// typeKeys presses `/` and then each rune of s as its own key.
func typeKeys(x tea.Model, s string) tea.Model {
	x = press(x, "/")
	return typeMore(x, s)
}

func typeMore(x tea.Model, s string) tea.Model {
	for _, r := range s {
		if r == ' ' {
			x = press(x, "space")
			continue
		}
		x = press(x, string(r))
	}
	return x
}

func modelPtr(x tea.Model) *Model {
	m := x.(Model)
	return &m
}

// drawn is the trimmed text of the body rows of side panel n as on screen.
func drawn(x tea.Model, n int) []string {
	lines := screenOf(x)
	r := x.(Model).sh.panelRect[n-1]
	var out []string
	for y := r.Y + 1; y < r.Y+r.H-1; y++ {
		cells := []rune(lines[y])
		if len(cells) < r.X+r.W {
			continue
		}
		out = append(out, strings.TrimSpace(string(cells[r.X+1:r.X+r.W-1])))
	}
	return out
}

// drawnRows drops the blank filler rows.
func drawnRows(x tea.Model, n int) []string {
	var out []string
	for _, l := range drawn(x, n) {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func containsAll(rows []string, wants ...string) bool {
	for _, w := range wants {
		found := false
		for _, r := range rows {
			if strings.Contains(r, w) {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func anyRow(rows []string, sub string) bool { return containsAll(rows, sub) }

func counterOf(x tea.Model, n int) string {
	lines := screenOf(x)
	r := x.(Model).sh.panelRect[n-1]
	return lines[r.Y]
}

func TestShellFilterChecks(t *testing.T) {
	layersProfile(t, "colorless")
	x := press(shellModel(t, loadTiny(t), 140, 40), "2")
	all := drawnRows(x, 2)
	if len(all) != 9 {
		t.Fatalf("unfiltered rows %d: %v", len(all), all)
	}

	x = typeKeys(x, "CODE")
	rows := drawnRows(x, 2)
	// Case-insensitive substring: two checks start with "Code"; the editing
	// row is the last body row.
	if !containsAll(rows, "Code-executing formats", "Code & pickle contents", "/ CODE") {
		t.Fatalf("rows: %v", rows)
	}
	if anyRow(rows, "Shard headers") || anyRow(rows, "Hashes") {
		t.Fatalf("non-matches drawn: %v", rows)
	}
	if !strings.Contains(counterOf(x, 2), "filter: CODE") {
		t.Fatalf("counter: %q", counterOf(x, 2))
	}
	// The cursor started on row 0, which does not match, so it snaps forward.
	if got := x.(Model).scanCursor; got != 4 {
		t.Fatalf("cursor %d, want the first match (4)", got)
	}
	if !strings.Contains(strings.Join(screenOf(x), "\n"), "[0] Code-executing formats") {
		t.Fatal("detail does not follow the match")
	}

	x = press(x, "down")
	if got := x.(Model).scanCursor; got != 5 {
		t.Fatalf("down: cursor %d, want 5", got)
	}
	x = press(x, "down")
	if got := x.(Model).scanCursor; got != 5 {
		t.Fatalf("down past the last match: cursor %d", got)
	}
	x = press(x, "up", "up")
	if got := x.(Model).scanCursor; got != 4 {
		t.Fatalf("up: cursor %d, want 4", got)
	}

	// ⏎ keeps the filter and returns to list navigation: j and k move again.
	x = press(x, "enter")
	if x.(Model).sh.filter[1].editing {
		t.Fatal("still editing after enter")
	}
	rows = drawnRows(x, 2)
	if anyRow(rows, "/ CODE") || anyRow(rows, "Shard headers") || !anyRow(rows, "Code & pickle") {
		t.Fatalf("kept filter rows: %v", rows)
	}
	if !strings.Contains(counterOf(x, 2), "filter: CODE") {
		t.Fatalf("counter after enter: %q", counterOf(x, 2))
	}
	x = press(x, "j")
	if got := x.(Model).scanCursor; got != 5 {
		t.Fatalf("j: cursor %d", got)
	}
	if x.(Model).sh.inDetail {
		t.Fatal("enter in the input moved focus to the detail")
	}

	// esc clears it and the whole list is back with the cursor where it was.
	x = press(x, "esc")
	if x.(Model).sh.filter[1].active() {
		t.Fatal("filter survived esc")
	}
	if got := len(drawnRows(x, 2)); got != 9 {
		t.Fatalf("rows after esc: %d", got)
	}
	if strings.Contains(counterOf(x, 2), "filter") {
		t.Fatalf("counter after esc: %q", counterOf(x, 2))
	}
	if got := x.(Model).scanCursor; got != 5 {
		t.Fatalf("cursor after esc: %d", got)
	}

	// esc while editing clears too.
	x = press(typeKeys(x, "hash"), "esc")
	if x.(Model).sh.filter[1].active() || x.(Model).sh.filter[1].editing {
		t.Fatal("esc in the input left state behind")
	}
}

func TestShellFilterNoMatches(t *testing.T) {
	layersProfile(t, "colorless")
	x := typeKeys(press(shellModel(t, loadTiny(t), 140, 40), "2"), "zzz")
	if rows := drawnRows(x, 2); !anyRow(rows, "no matches") {
		t.Fatalf("rows: %v", rows)
	}
	cur := x.(Model).scanCursor
	x = press(x, "down", "up", "pgdown", "pgup")
	if x.(Model).scanCursor != cur {
		t.Fatal("cursor moved with nothing to land on")
	}
	assertFrame(t, x.View(), 140, 40)
	x = press(x, "backspace", "backspace", "backspace")
	if got := len(drawnRows(x, 2)); got != 10 {
		t.Fatalf("empty filter shows %d rows, want all 9 plus the input", got)
	}
}

func TestShellFilterLayers(t *testing.T) {
	layersProfile(t, "colorless")
	m := loadFiles(t, mustShape(t))
	x := press(shellModel(t, m, 140, 40), "3")
	// Collapsed: only the rows drawn are searched.
	x = typeKeys(x, "layer")
	if rows := drawnRows(x, 3); !anyRow(rows, "no matches") {
		t.Fatalf("collapsed layers matched: %v", rows)
	}
	x = press(x, "esc", "right")
	x = typeKeys(x, "1")
	rows := drawnRows(x, 3)
	for _, want := range []string{"decoder", "layer 1 ", "layer 10", "layer 19", "layer 21", "layer 31", "layer 41"} {
		if !anyRow(rows, want) {
			t.Fatalf("missing %q: %v", want, rows)
		}
	}
	for _, not := range []string{"layer 0 ", "layer 2 ", "layer 3 ", "layer 20", "layer 47", "mtp"} {
		if anyRow(rows, not) {
			t.Fatalf("%q drawn: %v", not, rows)
		}
	}
	if !strings.Contains(counterOf(x, 3), "filter: 1") {
		t.Fatalf("counter %q", counterOf(x, 3))
	}

	// The cursor starts on the decoder row (a kept parent) and walks the matches.
	p := modelPtr(x).sidePanels()[2]
	sel := modelPtr(x).selectable(p)
	if len(sel) != 15 {
		t.Fatalf("selectable %d, want the decoder and 14 layers", len(sel))
	}
	x = press(x, "down", "down")
	if e, _ := layersPanelOf(x).selected(); !e.layer || layersPanelOf(x).label(e).String() != "layer 10" {
		t.Fatalf("selected %+v", e)
	}
	if !strings.Contains(strings.Join(screenOf(x), "\n"), "[0] layer 10") {
		t.Fatal("detail does not follow")
	}
	label := func() string { return layersPanelOf(x).label(mustSelected(t, x)).String() }

	// ⏎ keeps the filter; G and g then run to its last and first match.
	x = press(x, "enter", "G")
	if got := label(); got != "layer 41" {
		t.Fatalf("G landed on %q", got)
	}
	x = press(x, "g")
	if e := mustSelected(t, x); !e.dec {
		t.Fatalf("g landed on %+v", e)
	}

	// A narrower filter re-snaps the cursor onto a layer that still matches.
	x = press(x, "G", "/", "backspace")
	x = typeMore(x, "9")
	rows = drawnRows(x, 3)
	if !anyRow(rows, "layer 9 ") || !anyRow(rows, "layer 39") || anyRow(rows, "layer 10") {
		t.Fatalf("rows: %v", rows)
	}
	if got := label(); !strings.Contains(got, "9") {
		t.Fatalf("cursor is on %q after narrowing", got)
	}

	// Folding the decoder under a filter never leaves the cursor on a layer.
	x = press(x, "enter", "g", "enter")
	if mustSelected(t, x).layer {
		t.Fatal("cursor on a hidden layer")
	}
	assertFrame(t, x.View(), 140, 40)
}

func mustSelected(t *testing.T, x tea.Model) sideEntry {
	t.Helper()
	e, ok := layersPanelOf(x).selected()
	if !ok {
		t.Fatal("no selection")
	}
	return e
}

func TestShellFilterConfigLabels(t *testing.T) {
	layersProfile(t, "colorless")
	x := press(shellModel(t, loadTiny(t), 140, 40), "4")
	x = typeKeys(x, "head")
	rows := drawnRows(x, 4)
	if !containsAll(rows, "ATTENTION", "heads q / kv", "head_dim", "VISION", "heads · patch") {
		t.Fatalf("rows: %v", rows)
	}
	for _, not := range []string{"TEXT", "MOE", "QUANT", "model_type", "experts"} {
		if anyRow(rows, not) {
			t.Fatalf("%q drawn: %v", not, rows)
		}
	}
	// Labels match nothing themselves.
	x = press(x, "esc")
	x = typeKeys(x, "moe")
	if rows := drawnRows(x, 4); !anyRow(rows, "no matches") {
		t.Fatalf("a group label matched: %v", rows)
	}
	x = press(x, "esc")

	x = typeKeys(x, "head")
	p := modelPtr(x).sidePanels()[3]
	if got := p.rows(30)[p.cursor()]; !strings.Contains(ui.Strip(got), "heads q / kv") {
		t.Fatalf("cursor on %q, want the first match", ui.Strip(got))
	}
	x = press(x, "down", "down")
	if got := ui.Strip(p.rows(30)[p.cursor()]); !strings.Contains(got, "heads · patch") {
		t.Fatalf("cursor on %q", got)
	}
	x = press(x, "up")
	if rows := drawnRows(x, 4); !anyRow(rows, "ATTENTION") {
		t.Fatalf("rows: %v", rows)
	}
	if !strings.Contains(strings.Join(screenOf(x), "\n"), "[0] text_config.head_dim") {
		t.Fatal("detail does not follow the match")
	}
}

// #24: a label directly above the cursor row never scrolls out of view.
func TestShellConfigLabelStaysVisible(t *testing.T) {
	layersProfile(t, "colorless")
	for _, sz := range [][2]int{{100, 20}, {100, 24}, {84, 24}} {
		x := press(shellModel(t, loadTiny(t), sz[0], sz[1]), "4", "G")
		_ = x.View()
		for step := 0; step < 40; step++ {
			x = press(x, "up")
			rows := drawnRows(x, 4)
			p := modelPtr(x).sidePanels()[3].(*configPanel)
			lines, _ := p.listLines()
			cur := p.cursor()
			if cur > 0 && lines[cur-1].kind == fieldHeader {
				label := strings.ToUpper(lines[cur-1].label)
				if !anyRow(rows, label) {
					t.Fatalf("%dx%d step %d: label %q above the cursor is off screen: %v", sz[0], sz[1], step, label, rows)
				}
			}
		}
		x = press(x, "G", "g")
		if rows := drawnRows(x, 4); !anyRow(rows, "TEXT") {
			t.Fatalf("%dx%d: home leaves the first label off screen: %v", sz[0], sz[1], rows)
		}
	}
}

func TestShellFilterModel(t *testing.T) {
	layersProfile(t, "colorless")
	x := typeKeys(press(shellModel(t, loadTiny(t), 140, 40), "1"), "FILES")
	rows := drawnRows(x, 1)
	if !anyRow(rows, "Files") || anyRow(rows, "Overview") || anyRow(rows, "Memory") {
		t.Fatalf("rows: %v", rows)
	}
	if got := x.(Model).sh.modelCur; got != modelFiles {
		t.Fatalf("cursor %d, want Files", got)
	}
	x = press(x, "backspace", "backspace", "backspace", "backspace", "backspace", "e")
	if rows := drawnRows(x, 1); !anyRow(rows, "Memory") || !anyRow(rows, "Overview") {
		t.Fatalf("rows: %v", rows)
	}
}

// Keys that mean something in the list are text while the input has focus.
func TestShellFilterEditingSwallowsKeys(t *testing.T) {
	layersProfile(t, "colorless")
	x := typeKeys(press(shellModel(t, loadTiny(t), 140, 40), "2"), "q1?:y0")
	m := x.(Model)
	if m.help || m.palette.open || m.sh.side != 2 || m.sh.inDetail {
		t.Fatalf("a key leaked out of the input: %+v", m.sh)
	}
	if got := m.sh.filter[1].text; got != "q1?:y0" {
		t.Fatalf("text %q", got)
	}
	x = press(x, "tab")
	if x.(Model).sh.side != 2 || !x.(Model).sh.filter[1].editing {
		t.Fatal("tab left the input")
	}
	// Clicking elsewhere accepts the filter.
	_ = x.View()
	x = click(x, 100, 5)
	if x.(Model).sh.filter[1].editing || x.(Model).sh.filter[1].text != "q1?:y0" {
		t.Fatalf("click: %+v", x.(Model).sh.filter[1])
	}
}

func TestShellFilterCap(t *testing.T) {
	layersProfile(t, "colorless")
	long := strings.Repeat("ab", 100)
	x := typeKeys(press(shellModel(t, loadTiny(t), 140, 40), "2"), "")
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(long)})
	if n := len([]rune(x.(Model).sh.filter[1].text)); n != shellFilterMax {
		t.Fatalf("filter holds %d runes, want %d", n, shellFilterMax)
	}
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("zzz")})
	if n := len([]rune(x.(Model).sh.filter[1].text)); n != shellFilterMax {
		t.Fatalf("more typing grew the filter to %d", n)
	}
	assertFrame(t, x.View(), 140, 40)
	x = press(x, "enter")
	assertFrame(t, x.View(), 140, 40)
	// The counter fits the border at the narrowest side panel.
	x = shellModel(t, loadTiny(t), 84, 24)
	x, _ = press(x, "2", "/").Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(long)})
	x = press(x, "enter")
	assertFrame(t, x.View(), 84, 24)
	if !strings.Contains(counterOf(x, 2), "filter: ab") {
		t.Fatalf("counter %q", counterOf(x, 2))
	}
}

func TestShellFilterHostileInput(t *testing.T) {
	layersProfile(t, "colorless")
	x := press(shellModel(t, loadTiny(t), 140, 40), "2", "/")
	evil := "a\x1b]0;PWN\x07\x1b[2Jb‮c\u0085d<U+202E>e\x00"
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(evil), Paste: true})
	if got := x.(Model).sh.filter[1].text; strings.ContainsAny(got, "\x1b\x07\x00‮\u0085") {
		t.Fatalf("control characters entered the filter: %q", got)
	}
	for _, keys := range [][]string{nil, {"enter"}} {
		x = press(x, keys...)
		view := x.View()
		if raw, bad := rawBad(view); bad {
			t.Fatalf("raw %s in\n%s", raw, view)
		}
		for _, raw := range []string{"\x1b]0;PWN", "\x1b[2J", "‮", "\u0085", "\x07"} {
			if strings.Contains(view, raw) {
				t.Fatalf("view contains %q", raw)
			}
		}
		if !strings.Contains(view, "<U+00") {
			t.Fatalf("typed < is not shown as its token:\n%s", view)
		}
	}
	assertFrame(t, x.View(), 140, 40)
}

// A filter over hostile config keys draws only sanitized names.
func TestShellFilterHostileConfig(t *testing.T) {
	layersProfile(t, "colorless")
	m := loadCfg(t, fixture.ConfigHostile())
	x := press(shellModel(t, m, 140, 40), "4")
	for _, q := range []string{"e", "<", "u+", "x"} {
		x = press(typeKeys(x, q), "enter")
		view := x.View()
		if raw, bad := rawBad(view); bad {
			t.Fatalf("filter %q: raw %s", q, raw)
		}
		assertFrame(t, view, 140, 40)
		x = press(x, "esc")
	}
}

func TestShellFilterKeyLineAndHelp(t *testing.T) {
	layersProfile(t, "colorless")
	x := shellModel(t, loadTiny(t), 140, 40)
	for n := 1; n <= 4; n++ {
		x = press(x, fmt.Sprint(n))
		lines := screenOf(x)
		if line := lines[len(lines)-1]; !strings.Contains(line, "/ filter") {
			t.Errorf("panel %d key line lacks the filter hint: %q", n, line)
		}
		if line := lines[len(lines)-1]; strings.Contains(line, "esc clear filter") {
			t.Errorf("panel %d: clear hint without a filter", n)
		}
		x = press(x, "?")
		help := strings.Join(screenOf(x), "\n")
		for _, want := range []string{"FILTER", "filter the list by name", "keep the filter", "clear the filter"} {
			if !strings.Contains(help, want) {
				t.Errorf("panel %d help lacks %q", n, want)
			}
		}
		x = press(x, "?")
	}
	x = press(typeKeys(x, "x"), "enter")
	if line := screenOf(x)[39]; !strings.Contains(line, "esc clear filter") {
		t.Errorf("key line with a filter: %q", line)
	}
	x = press(x, "0")
	if line := screenOf(x)[39]; strings.Contains(line, "/ filter") {
		t.Errorf("detail pane advertises the filter: %q", line)
	}
	// `/` in the detail pane is a no-op.
	x = press(x, "/")
	if x.(Model).sh.filter[3].editing {
		t.Error("/ opened the filter from the detail pane")
	}
	x = press(x, "esc")
	if !x.(Model).sh.filter[3].active() {
		t.Error("esc from the detail pane cleared the filter")
	}
	editing := press(x, "/")
	if line := screenOf(editing)[39]; !strings.Contains(line, "⏎ keep") || !strings.Contains(line, "esc clear") {
		t.Errorf("editing key line: %q", line)
	}
}

// A panel that cannot filter ignores `/` and does not advertise it.
type plainPanel struct{ sidePanel }

func TestShellFilterOptional(t *testing.T) {
	var p sidePanel = plainPanel{}
	if _, ok := p.(filterable); ok {
		t.Fatal("plainPanel filters")
	}
	x := shellModel(t, loadTiny(t), 140, 40).(Model)
	if (&x).filterOpen(p) {
		t.Fatal("filterOpen accepted a panel without filterRows")
	}
	if x.sh.filter[0].editing {
		t.Fatal("filterOpen left a filter open")
	}
}

func TestShellFilterProblemJump(t *testing.T) {
	layersProfile(t, "colorless")
	m := loadFiles(t, mustCode(t))
	x := press(shellModel(t, m, 140, 40), "2")
	probs := modelPtr(x).sidePanels()[1].problems()
	if len(probs) == 0 {
		t.Skip("no problems")
	}
	name := x.(Model).scan.Checks[probs[0]].Name
	x = press(typeKeys(x, "zzz"), "enter", "]")
	if x.(Model).notice == "" {
		t.Fatal("jump ignored the filter")
	}
	x = press(x, "esc")
	x = press(typeKeys(x, name), "enter", "]")
	if got := x.(Model).scan.Checks[x.(Model).scanCursor].Name; got != name {
		t.Fatalf("jumped to %q, want %q", got, name)
	}
}

// Every drawn row of every panel is hit where it is drawn while a filter is
// active, and the cursor stays on a drawn row.
func TestShellFilterRowsHitWhereDrawn(t *testing.T) {
	for name, m := range shellFixtures(t) {
		for _, sz := range shellSizes {
			for n := 1; n <= 4; n++ {
				for _, q := range []string{"e", "a", "layer", "1"} {
					label := fmt.Sprintf("%s %dx%d panel %d /%s", name, sz[0], sz[1], n, q)
					x := press(shellModel(t, m, sz[0], sz[1]), fmt.Sprint(n), "right")
					x = press(typeKeys(x, q), "enter")
					cur := x.(Model)
					p := cur.sidePanels()[n-1]
					iw := shellSideWidth(sz[0]) - 4
					sel := (&cur).selectable(p)
					for _, i := range sel {
						p.move(i - p.cursor())
						rows := p.rows(iw)
						screen := strings.Split(stripView(cur.View()), "\n")
						found := false
						for _, h := range cur.sh.hits {
							var hn, hr int
							if _, err := fmt.Sscanf(h.id, "sh:row:%d:%d", &hn, &hr); err != nil || hn != n {
								t.Errorf("%s: stray hit %q", label, h.id)
								continue
							}
							if h.y < 1 || h.y >= sz[1]-1 {
								t.Errorf("%s: row %d hit at %d leaves the body", label, hr, h.y)
								continue
							}
							if !strings.Contains(screen[h.y], strings.TrimRight(ui.Strip(rows[hr]), " ")) {
								t.Errorf("%s cursor %d: hit for row %d at screen row %d reads %q", label, i, hr, h.y, screen[h.y])
							}
							if z, ok := cur.zones.Hit(h.x+1, h.y); !ok || z.ID != h.id {
								t.Errorf("%s: zone at (%d,%d) is %v, want %s", label, h.x+1, h.y, z.ID, h.id)
							}
							if hr == i {
								found = true
							}
						}
						if !found {
							t.Errorf("%s: selected row %d is not on screen", label, i)
						}
					}
				}
			}
		}
	}
}

// Clicking a drawn row selects that row, not the one at the same position in
// the unfiltered list.
func TestShellFilterClickSelectsDrawnRow(t *testing.T) {
	layersProfile(t, "colorless")
	x := press(shellModel(t, loadTiny(t), 140, 40), "2")
	x = press(typeKeys(x, "hashes"), "enter")
	_ = x.View()
	m := x.(Model)
	var h hit
	for _, c := range m.sh.hits {
		h = c
	}
	x = click(x, h.x+1, h.y)
	if got := x.(Model).scan.Checks[x.(Model).scanCursor].Name; got != "Hashes vs revision" {
		t.Fatalf("clicked row selects %q", got)
	}
}

func TestShellFilterGolden(t *testing.T) {
	tiny := loadFiles(t, mustFiles(t, fixture.TinyQwen))
	tiny.RepoID, tiny.Revision = "org/tiny-qwen", "0123456789abcdef0123456789abcdef01234567"
	qwen := loadFiles(t, mustShape(t))
	qwen.RepoID, qwen.Revision = "org/qwen-shape", "fedcba9876543210fedcba9876543210fedcba98"
	type spec struct {
		name string
		m    *model.Model
		keys []string
	}
	cases := []spec{
		{"config", tiny, []string{"4", "/", "h", "e", "a", "d", "enter"}},
		{"config-editing", tiny, []string{"4", "/", "h", "e", "a", "d"}},
		{"layers", qwen, []string{"3", "right", "/", "1", "enter"}},
		{"layers-editing", qwen, []string{"3", "right", "/", "1", "down", "down"}},
	}
	for _, c := range cases {
		for _, p := range []string{"colorless", "truecolor"} {
			name := "shell.filter." + c.name + ".140x40." + p
			t.Run(name, func(t *testing.T) {
				res := scan.Run(c.m)
				view := renderPrepared(t, c.m, res, 140, 40, p, c.keys, Options{Layout: LayoutPanels})
				assertFrame(t, view, 140, 40)
				if p == "colorless" && strings.Contains(view, "\x1b") {
					t.Fatal("colorless frame contains ESC")
				}
				path := filepath.Join("testdata", "golden", name+".golden")
				if *updateGolden {
					if err := os.WriteFile(path, []byte(view), 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read golden: %v (go test -update)", err)
				}
				if string(got) != view {
					t.Errorf("golden mismatch\n%s", firstDiff(string(got), view))
				}
			})
		}
	}
}

func filterBudget(t *testing.T, name string, x tea.Model) {
	t.Helper()
	_ = x.View()
	_ = x.View()
	if v := median(9, func() { _ = x.View() }); v > budget(2*time.Millisecond) {
		t.Errorf("%s: cached View %s", name, v)
	}
	if v := median(9, func() { x, _ = x.Update(tea.KeyMsg{Type: tea.KeyDown}) }); v > budget(500*time.Microsecond) {
		t.Errorf("%s: Update %s", name, v)
	}
	if v := median(9, func() { x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")}) }); v > budget(500*time.Microsecond) {
		t.Errorf("%s: Update typing %s", name, v)
	}
	if v := median(9, func() { x, _ = x.Update(tea.WindowSizeMsg{Width: 120, Height: 40}) }); v > budget(5*time.Millisecond) {
		t.Errorf("%s: Resize %s", name, v)
	}
}

func TestShellFilterBudgets(t *testing.T) {
	huge := loadCfg(t, fixture.ConfigHuge())
	filterBudget(t, "config huge", press(shellModel(t, huge, 140, 40), "4", "/"))
	big := layersScaleModel(4097, 1)
	filterBudget(t, "layers 4097", press(shellModel(t, big, 140, 40), "3", "right", "/"))
	filterBudget(t, "layers 4097 accepted", press(shellModel(t, big, 140, 40), "3", "right", "/", "1", "enter"))
}
