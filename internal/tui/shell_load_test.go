package tui

import (
	"errors"
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

const loadTestDir = "/models/hub/models--org--tiny-qwen/snapshots/0123456789ab"

func walkDone() model.Progress  { return model.Progress{Phase: model.PhaseWalk, Done: 35, Total: 35} }
func indexDone() model.Progress { return model.Progress{Phase: model.PhaseIndex, Done: 1, Total: 1} }
func headersDone() model.Progress {
	return model.Progress{Phase: model.PhaseHeaders, Done: 22, Total: 22, Ranges: 1790}
}
func layoutMid() model.Progress {
	return model.Progress{Phase: model.PhaseLayout, Done: 14, Total: 22, Ranges: 2361}
}

// midLoad is the state the goldens pin: headers read, offsets half checked.
var midLoad = []model.Progress{walkDone(), indexDone(), headersDone(), layoutMid()}

// loadingModel is a model that is still loading dir, fed synthetic progress.
func loadingModel(dir string, prog ...model.Progress) Model {
	n := NewLoading(dir, false, false, "org/tiny-qwen", "")
	n.apply(Options{Now: func() time.Time { return time.Unix(100, 0) }})
	var x tea.Model = n
	for _, p := range prog {
		x, _ = x.Update(scanProgMsg{p: p})
	}
	return x.(Model)
}

func loadingShell(w, h int, dir string, prog ...model.Progress) tea.Model {
	x, _ := loadingModel(dir, prog...).Update(tea.WindowSizeMsg{Width: w, Height: h})
	return x
}

func TestLoadPhaseStates(t *testing.T) {
	cases := []struct {
		name string
		prog []model.Progress
		want [5]phaseState
	}{
		{"nothing", nil, [5]phaseState{phaseActive}},
		{"walking", []model.Progress{{Phase: model.PhaseWalk, Done: 7, Total: 7}},
			[5]phaseState{phaseActive}},
		{"index", []model.Progress{walkDone(), indexDone()},
			[5]phaseState{phaseDone, phaseDone, phaseActive}},
		{"headers mid", []model.Progress{walkDone(), indexDone(), {Phase: model.PhaseHeaders, Done: 3, Total: 22}},
			[5]phaseState{phaseDone, phaseDone, phaseActive}},
		{"headers done", []model.Progress{walkDone(), indexDone(), headersDone()},
			[5]phaseState{phaseDone, phaseDone, phaseDone, phaseActive}},
		{"layout mid", midLoad,
			[5]phaseState{phaseDone, phaseDone, phaseDone, phaseActive}},
		{"layout done", append(append([]model.Progress{}, midLoad...), model.Progress{Phase: model.PhaseLayout, Done: 22, Total: 22}),
			[5]phaseState{phaseDone, phaseDone, phaseDone, phaseDone, phaseActive}},
		// A dropped event must not leave an earlier phase pending.
		{"index event lost", []model.Progress{headersDone()},
			[5]phaseState{phaseDone, phaseDone, phaseDone, phaseActive}},
		// No shards: the scan reports 0/0 and that finishes the layout pass.
		{"no shards", []model.Progress{walkDone(), indexDone(), {Phase: model.PhaseLayout}},
			[5]phaseState{phaseDone, phaseDone, phaseDone, phaseDone, phaseActive}},
	}
	for _, c := range cases {
		var l loadPhases
		for _, p := range c.prog {
			l.note(p)
		}
		got := l.states()
		// Pending is the zero value, so want lists only the leading states.
		if got != c.want {
			t.Errorf("%s: states %v, want %v", c.name, got, c.want)
		}
		if f := l.fraction(); f < 0 || f > 1 {
			t.Errorf("%s: fraction %v", c.name, f)
		}
	}
}

// The meter never moves backwards as events arrive.
func TestLoadFractionMonotone(t *testing.T) {
	var l loadPhases
	prev := -1.0
	seq := []model.Progress{
		{Phase: model.PhaseWalk, Done: 3, Total: 3}, walkDone(), indexDone(),
		{Phase: model.PhaseHeaders, Done: 1, Total: 22}, {Phase: model.PhaseHeaders, Done: 11, Total: 22}, headersDone(),
		{Phase: model.PhaseLayout, Done: 1, Total: 22}, layoutMid(), {Phase: model.PhaseLayout, Done: 22, Total: 22},
	}
	for i, p := range seq {
		l.note(p)
		if f := l.fraction(); f < prev {
			t.Fatalf("event %d (%+v): fraction %v fell from %v", i, p, f, prev)
		} else {
			prev = f
		}
	}
	if prev >= 1 {
		t.Fatalf("meter is full before the checks finish: %v", prev)
	}
}

// The order the shell lists is the order the real loader emits.
func TestLoadPhasesMatchRealLoader(t *testing.T) {
	files, err := fixture.ShardedNgram()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	ch := make(chan model.Progress, 4096)
	m, err := model.LoadWithProgress(dir, ch)
	if err != nil {
		t.Fatal(err)
	}
	scan.Run(m)
	m.DetachProgress()
	close(ch)
	var l loadPhases
	var order []string
	for p := range ch {
		if len(order) == 0 || order[len(order)-1] != p.Phase {
			order = append(order, p.Phase)
		}
		l.note(p)
	}
	want := []string{model.PhaseWalk, model.PhaseIndex, model.PhaseHeaders, model.PhaseLayout}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("loader order %v, want %v", order, want)
	}
	st := l.states()
	if st != [5]phaseState{phaseDone, phaseDone, phaseDone, phaseDone, phaseActive} {
		t.Fatalf("states after the scan: %v", st)
	}
}

func TestShellLoadingFrames(t *testing.T) {
	states := map[string][]model.Progress{
		"none": nil, "walk": {{Phase: model.PhaseWalk, Done: 9, Total: 9}},
		"headers": {walkDone(), indexDone(), {Phase: model.PhaseHeaders, Done: 4, Total: 22, Ranges: 300}},
		"layout":  midLoad,
		"checks":  {walkDone(), indexDone(), headersDone(), {Phase: model.PhaseLayout, Done: 22, Total: 22}},
	}
	sizes := [][2]int{{200, 50}, {140, 40}, {100, 30}, {84, 24}, {83, 24}, {80, 24}, {60, 20}, {40, 12}, {84, 12}, {100, 16}}
	for name, prog := range states {
		for _, sz := range sizes {
			for _, keys := range [][]string{nil, {"enter"}, {"?"}, {"enter", "esc"}, {"0", "?"}} {
				label := fmt.Sprintf("%s %dx%d %v", name, sz[0], sz[1], keys)
				x := press(loadingShell(sz[0], sz[1], loadTestDir, prog...), keys...)
				assertFrame(t, x.View(), sz[0], sz[1])
				v := stripView(x.View())
				help := x.(Model).help
				if !help && !strings.Contains(v, "[1] Model") && !strings.Contains(v, "[0] Loading") {
					t.Errorf("%s: neither panel title on screen:\n%s", label, v)
				}
			}
		}
	}
}

func TestShellLoadingContent(t *testing.T) {
	x := loadingShell(140, 40, loadTestDir, midLoad...)
	v := stripView(x.View())
	for _, want := range []string{
		"model-inspector", "org/tiny-qwen", "reading headers", "headers-only",
		"[1] Model", "[2] Checks", "[3] Layers", "[4] Config", "[0] Loading",
		"Walk snapshot", "35 files", "Index", "Shard headers", "22/22", "Offsets & layout", "14/22", "Checks",
		"Reading safetensors headers only; no weights are loaded.",
		"14 of 22 shards", "2,361 ranges", "…", "q cancel · ? keys",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q:\n%s", want, v)
		}
	}
	if n := strings.Count(v, "waiting for headers"); n != 3 {
		t.Errorf("waiting lines %d, want 3", n)
	}
	// Three finished phases, one active, one pending.
	if strings.Count(v, "✓") != 3 || strings.Count(v, "•") != 1 || strings.Count(v, "○") != 1 {
		t.Errorf("phase glyphs ✓%d •%d ○%d", strings.Count(v, "✓"), strings.Count(v, "•"), strings.Count(v, "○"))
	}
	if !strings.Contains(v, loadTestDir[:20]) || !strings.Contains(v, "0123456789ab") {
		t.Errorf("path head or tail missing:\n%s", v)
	}
	if f := x.(Model).scn.phases.fraction(); f < 0.7 || f > 0.8 {
		t.Errorf("overall fraction %v", f)
	}
}

// The active phase is accent and bold, finished and pending phases are
// text-3, per the spec.
func TestShellLoadingColors(t *testing.T) {
	view := renderUI(t, 140, 40, "truecolor", nil, func() Model { return loadingModel(loadTestDir, midLoad...) })
	var bullet, check, pend string
	for _, ln := range strings.Split(view, "\n") {
		switch {
		case strings.Contains(ln, "•"):
			bullet = ln
		case strings.Contains(ln, "Walk snapshot"):
			check = ln
		case strings.Contains(ln, "○"):
			pend = ln
		}
	}
	accent := "\x1b[1;38;2;143;209;79m"
	if !strings.Contains(bullet, accent+"•") {
		t.Errorf("active bullet is not accent bold: %q", bullet)
	}
	muted := "38;2;131;139;153m"
	if !strings.Contains(check, muted+"✓") {
		t.Errorf("done check is not text-3: %q", check)
	}
	if !strings.Contains(pend, muted+"○") {
		t.Errorf("pending circle is not text-3: %q", pend)
	}
}

func TestShellLoadingKeys(t *testing.T) {
	x := loadingShell(100, 30, loadTestDir, midLoad...)
	// Panel keys do nothing before there is a model to point at.
	for _, k := range []string{"1", "2", "3", "4", "tab", "shift+tab", "]", "[", "y", "r", "down", "up", "enter", "esc", ":", "m"} {
		x = press(x, k)
		assertFrame(t, x.View(), 100, 30)
	}
	if !x.(Model).shellLoad() {
		t.Fatal("a key left the loading frame")
	}
	x = press(x, "?")
	if v := stripView(x.View()); !strings.Contains(v, "KEYS") || !strings.Contains(v, "LOADING") {
		t.Fatalf("help:\n%s", v)
	}
	assertFrame(t, x.View(), 100, 30)
	x = press(x, "q") // closes help
	if _, cmd := x.Update(keyMsg("q")); cmd == nil {
		t.Fatal("q did not quit while loading")
	} else if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q was not a quit")
	}
	if _, cmd := press(x, "?").Update(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Fatal("ctrl+c did not quit")
	}
	// Mouse and wheel are inert, and old zones do not linger.
	x, _ = x.Update(tea.MouseMsg{X: 5, Y: 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	x, _ = x.Update(tea.MouseMsg{X: 90, Y: 10, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	assertFrame(t, x.View(), 100, 30)
	if _, ok := x.(Model).zones.Hit(5, 3); ok {
		t.Fatal("loading frame left a hit zone")
	}
}

func TestShellLoadingPortraitDetail(t *testing.T) {
	x := loadingShell(60, 20, loadTestDir, midLoad...)
	v := stripView(x.View())
	if !strings.Contains(v, "[1] Model") || strings.Contains(v, "[0]") || !strings.Contains(v, "⏎ detail") {
		t.Fatalf("portrait loading stack:\n%s", v)
	}
	x = press(x, "enter")
	v = stripView(x.View())
	if !strings.Contains(v, "[0] Loading") || strings.Contains(v, "[2] Checks") || !strings.Contains(v, "esc back") {
		t.Fatalf("portrait loading detail:\n%s", v)
	}
	x = press(x, "esc")
	if v := stripView(x.View()); !strings.Contains(v, "[1] Model") {
		t.Fatalf("esc did not return:\n%s", v)
	}
}

// A detail pane opened over the loading stack does not carry into the loaded
// shell, which starts on Model.
func TestShellLoadedAfterDetail(t *testing.T) {
	m := loadTiny(t)
	x := press(loadingShell(60, 20, loadTestDir, midLoad...), "0")
	x, _ = x.Update(scanLoadedMsg{m: m, res: scan.Run(m)})
	cur := x.(Model)
	if !cur.shellOn() || cur.sh.inDetail || cur.sh.side != 1 {
		t.Fatalf("shell %v detail %v side %d", cur.shellOn(), cur.sh.inDetail, cur.sh.side)
	}
	if v := stripView(x.View()); !strings.Contains(v, "[1] Model") || strings.Contains(v, "[0]") {
		t.Fatalf("loaded portrait stack:\n%s", v)
	}
}

// A failed load puts the card in the detail pane, even in portrait where the
// stack would hide it.
func TestShellLoadErrorCard(t *testing.T) {
	prev := loadModel
	t.Cleanup(func() { loadModel = prev })
	loadModel = func(string, chan<- model.Progress) (*model.Model, error) {
		return nil, errors.New("disk\x1b[31m failed \u202e " + strings.Repeat("x", 400))
	}
	for _, sz := range [][2]int{{140, 40}, {100, 30}, {80, 24}, {60, 20}, {40, 12}} {
		n := NewLoading(loadTestDir, false, false, "", "")
		var x tea.Model = n
		x, _ = x.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		x, _ = x.Update(scanProgMsg{p: walkDone()})
		x, _ = x.Update(runScanLoad(n)())
		view := x.View()
		assertFrame(t, view, sz[0], sz[1])
		if r, bad := rawBad(view); bad {
			t.Fatalf("%v: raw %s", sz, r)
		}
		v := stripView(view)
		for _, want := range []string{"could not inspect", "<U+001B>", "<U+202E>", "q quit", "FAIL"} {
			if sz[0] >= 60 && !strings.Contains(v, want) {
				t.Errorf("%v: card lacks %q:\n%s", sz, want, v)
			}
		}
		if strings.Count(v, "x") > 300 {
			t.Errorf("%v: error text was not clipped", sz)
		}
		if x.(Model).exitCode() != 3 {
			t.Fatalf("%v: exit code", sz)
		}
		_, cmd := x.Update(keyMsg("q"))
		if cmd == nil {
			t.Fatalf("%v: q did not quit", sz)
		}
	}
}

// File-derived names in the header and the detail path are sanitized once.
func TestShellLoadingHostileNames(t *testing.T) {
	hostile := "/m/\x1b]0;pwn\x07/\u202e/evil\u009b/" + strings.Repeat("d", 300)
	for _, sz := range [][2]int{{140, 40}, {60, 20}} {
		n := NewLoading(hostile, false, false, "org/\x1b[2Jrepo\u202e", "")
		var x tea.Model = n
		x, _ = x.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		x = press(x, "enter")
		for _, k := range []string{"", "esc"} {
			x = press(x, k)
			view := x.View()
			assertFrame(t, view, sz[0], sz[1])
			if r, bad := rawBad(view); bad {
				t.Fatalf("%v: raw %s\n%s", sz, r, view)
			}
		}
	}
	x := loadingShell(140, 40, hostile, midLoad...)
	if v := stripView(x.View()); !strings.Contains(v, "<U+001B>") {
		t.Fatalf("hostile path was dropped:\n%s", v)
	}
}

// Portrait: the side panels stack; enter or 0 swaps in the detail pane; esc
// and the panel keys swap the stack back.
func TestShellPortrait(t *testing.T) {
	m := shellFixtures(t)["code"]
	for _, sz := range [][2]int{{83, 24}, {80, 24}, {60, 20}, {40, 12}, {50, 30}} {
		// The shell's state hangs off the Model by pointer, so every
		// scenario starts from a fresh one.
		fresh := func() tea.Model { return shellModel(t, m, sz[0], sz[1]) }
		x := fresh()
		assertFrame(t, x.View(), sz[0], sz[1])
		v := stripView(x.View())
		for _, want := range []string{"[1] Model", "[2] Checks", "[3] Layers", "[4] Config"} {
			if !strings.Contains(v, want) {
				t.Fatalf("%v stack lacks %s:\n%s", sz, want, v)
			}
		}
		if strings.Contains(v, "[0]") {
			t.Fatalf("%v: detail drawn beside the stack:\n%s", sz, v)
		}
		for _, k := range []string{"enter", "0"} {
			d := press(fresh(), k)
			assertFrame(t, d.View(), sz[0], sz[1])
			dv := stripView(d.View())
			if !strings.Contains(dv, "[0]") || strings.Contains(dv, "[2] Checks") || strings.Contains(dv, "[1] Model") {
				t.Fatalf("%v %s: detail did not replace the stack:\n%s", sz, k, dv)
			}
			if !d.(Model).sh.inDetail {
				t.Fatalf("%v %s: not in detail", sz, k)
			}
			// Full width: the pane's border spans the screen.
			if lines := strings.Split(dv, "\n"); !strings.HasPrefix(lines[1], "╭") || !strings.HasSuffix(lines[1], "╮") || strings.Count(lines[1], "╭") != 1 {
				t.Fatalf("%v: detail is not one full-width pane: %q", sz, lines[1])
			}
			back := press(d, "esc")
			if bv := stripView(back.View()); !strings.Contains(bv, "[2] Checks") || strings.Contains(bv, "[0]") {
				t.Fatalf("%v: esc did not restore the stack:\n%s", sz, bv)
			}
		}
		// Panel keys leave the detail pane too, and land on that panel.
		for n := 1; n <= 4; n++ {
			back := press(fresh(), "enter", fmt.Sprint(n))
			cur := back.(Model)
			if cur.sh.side != n || cur.sh.inDetail {
				t.Fatalf("%v: key %d: side %d detail %v", sz, n, cur.sh.side, cur.sh.inDetail)
			}
			assertFrame(t, back.View(), sz[0], sz[1])
		}
		// tab walks 1-4 then the detail pane, and wraps.
		y := fresh()
		var seen []string
		for i := 0; i < 6; i++ {
			y = press(y, "tab")
			assertFrame(t, y.View(), sz[0], sz[1])
			c := y.(Model)
			seen = append(seen, fmt.Sprintf("%d%v", c.sh.side, c.sh.inDetail))
		}
		if got := strings.Join(seen, " "); got != "2false 3false 4false 4true 1false 2false" {
			t.Fatalf("%v: tab order %s", sz, got)
		}
	}
}

// A focused panel takes the spare rows; the others keep their summaries.
func TestShellPortraitStackHeights(t *testing.T) {
	m := shellFixtures(t)["code"]
	x := press(shellModel(t, m, 80, 24), "2")
	sh := x.(Model).sh
	_ = x.View()
	total := 0
	for i, r := range sh.panelRect {
		if r.W != 80 || r.X != 0 {
			t.Errorf("panel %d rect %+v is not full width", i+1, r)
		}
		total += r.H
	}
	if total != 22 {
		t.Errorf("stack covers %d rows, want 22", total)
	}
	if sh.panelRect[1].H <= sh.panelRect[0].H {
		t.Errorf("focused Checks %d rows is not taller than Model %d", sh.panelRect[1].H, sh.panelRect[0].H)
	}
	if sh.detailRect != (ui.Rect{}) {
		t.Errorf("detail rect %+v while hidden", sh.detailRect)
	}
	// Short terminals shrink the others to their title row.
	y := press(shellModel(t, m, 60, 20), "3")
	_ = y.View()
	for i, r := range y.(Model).sh.panelRect {
		if i != 2 && r.H != 1 {
			t.Errorf("60x20 panel %d is %d rows, want the title row", i+1, r.H)
		}
	}
}

func TestShellPortraitMouse(t *testing.T) {
	x := shellModel(t, shellFixtures(t)["code"], 80, 24)
	_ = x.View()
	sh := x.(Model).sh
	r3 := sh.panelRect[2]
	x = click(x, r3.X+3, r3.Y)
	if c := x.(Model); c.sh.side != 3 || c.sh.inDetail {
		t.Fatalf("title click: side %d detail %v", c.sh.side, c.sh.inDetail)
	}
	x = press(x, "2")
	_ = x.View()
	var row hit
	for _, h := range x.(Model).sh.hits {
		if h.id == "sh:row:2:3" {
			row = h
		}
	}
	if row.id == "" {
		t.Fatal("no zone for Checks row 3")
	}
	x = click(x, row.x+4, row.y)
	if c := x.(Model); c.scanCursor != 3 || c.sh.inDetail {
		t.Fatalf("row click: cursor %d detail %v", c.scanCursor, c.sh.inDetail)
	}
	_ = x.View()
	x = click(x, row.x+4, row.y)
	if !x.(Model).sh.inDetail {
		t.Fatal("double click did not open the detail pane")
	}
	_ = x.View()
	// The detail pane fills the width: no stale panel or row zones remain.
	for _, p := range []struct{ x, y int }{{3, 3}, {70, 3}} {
		z, ok := x.(Model).zones.Hit(p.x, p.y)
		if !ok || z.ID != "sh:detail" {
			t.Fatalf("zone at %v in detail view: %v %v", p, z, ok)
		}
	}
	// Wheel scrolls the detail pane in portrait.
	x, _ = x.Update(tea.MouseMsg{X: 70, Y: 8, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	cur := x.(Model)
	if cur.sh.top == 0 && len(cur.sidePanels()[1].detail(76, 20)) > 20 {
		t.Fatal("wheel did not scroll the portrait detail")
	}
	// In the stack, the wheel moves the focused list, not a hidden detail.
	x = press(x, "esc")
	_ = x.View()
	before := x.(Model).scanCursor
	x, _ = x.Update(tea.MouseMsg{X: 10, Y: x.(Model).sh.panelRect[1].Y + 2, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if x.(Model).scanCursor <= before {
		t.Fatalf("wheel in the stack: cursor %d -> %d", before, x.(Model).scanCursor)
	}
}

func TestShellTooSmall(t *testing.T) {
	m := shellFixtures(t)["tiny"]
	for _, sz := range [][2]int{{30, 10}, {39, 30}, {100, 11}, {20, 5}, {10, 3}, {1, 1}} {
		x := shellModel(t, m, sz[0], sz[1])
		view := x.View()
		assertFrame(t, view, sz[0], sz[1])
		v := stripView(view)
		if sz[0] >= 26 {
			if !strings.Contains(v, fmt.Sprintf("terminal too small (%dx%d)", sz[0], sz[1])) {
				t.Errorf("%v: message missing:\n%s", sz, v)
			}
			if sz[1] >= 2 && !strings.Contains(v, "q quit") {
				t.Errorf("%v: q quit missing:\n%s", sz, v)
			}
		}
		for _, k := range []string{"2", "enter", "?", ":"} {
			x = press(x, k)
		}
		if c := x.(Model); c.help || c.palette.open || c.sh.side != 1 {
			t.Errorf("%v: keys reached the shell", sz)
		}
		if _, cmd := x.Update(keyMsg("q")); cmd == nil {
			t.Errorf("%v: q did not quit", sz)
		}
	}
	// 30x10 is centered: message and q quit sit on the middle rows.
	lines := screenOf(shellModel(t, m, 30, 10))
	if got := strings.TrimSpace(lines[4]); got != "terminal too small (30x10)" {
		t.Errorf("row 4 %q", got)
	}
	if got := strings.TrimSpace(lines[5]); got != "q quit" {
		t.Errorf("row 5 %q", got)
	}
	if pad := len(lines[4]) - len(strings.TrimLeft(lines[4], " ")); pad != 2 {
		t.Errorf("message left pad %d", pad)
	}
	// Growing back restores the shell, with state intact.
	x := press(shellModel(t, m, 30, 10), "q")
	_ = x
	y := press(shellModel(t, m, 100, 30), "3")
	y, _ = y.Update(tea.WindowSizeMsg{Width: 30, Height: 10})
	y, _ = y.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if c := y.(Model); !c.shellOn() || c.sh.side != 3 {
		t.Fatal("shell state lost across a too-small resize")
	}
	// Loading too: the loading frame also gives way to the notice.
	l := loadingShell(30, 10, loadTestDir, midLoad...)
	if v := stripView(l.View()); !strings.Contains(v, "terminal too small (30x10)") {
		t.Fatalf("loading at 30x10:\n%s", v)
	}
}

func TestShellPortraitRowsHitWhereDrawn(t *testing.T) {
	for name, m := range shellFixtures(t) {
		for _, sz := range [][2]int{{80, 24}, {60, 20}, {40, 12}} {
			for n := 1; n <= 4; n++ {
				label := fmt.Sprintf("%s %dx%d panel %d", name, sz[0], sz[1], n)
				cur := press(shellModel(t, m, sz[0], sz[1]), fmt.Sprint(n)).(Model)
				p := cur.sidePanels()[n-1]
				count := len(p.rows(sz[0] - 4))
				for i := 0; i < count; i++ {
					p.move(i - p.cursor())
					screen := strings.Split(stripView(cur.View()), "\n")
					rows := p.rows(sz[0] - 4)
					found := false
					for _, h := range cur.sh.hits {
						var hn, hr int
						if _, err := fmt.Sscanf(h.id, "sh:row:%d:%d", &hn, &hr); err != nil || hn != n {
							t.Errorf("%s: stray hit %q", label, h.id)
							continue
						}
						if h.y < 1 || h.y >= sz[1]-1 || h.x+h.w > sz[0] {
							t.Errorf("%s: row %d hit %+v leaves the body", label, hr, h)
							continue
						}
						if !strings.Contains(screen[h.y], strings.TrimRight(stripView(rows[hr]), " ")) {
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

func shellLoadGoldenCheck(t *testing.T, name, view string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".golden")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
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
}

func TestShellLoadGoldens(t *testing.T) {
	prev := loadModel
	t.Cleanup(func() { loadModel = prev })
	loadModel = func(string, chan<- model.Progress) (*model.Model, error) {
		return nil, errors.New("open " + loadTestDir + "/config.json: permission denied")
	}
	loading := func() Model { return loadingModel(loadTestDir, midLoad...) }
	failed := func() Model {
		n := NewLoading(loadTestDir, false, false, "org/tiny-qwen", "")
		x, _ := n.Update(runScanLoad(n)())
		return x.(Model)
	}
	type spec struct {
		name    string
		w, h    int
		profile string
		build   func() Model
		keys    []string
	}
	var cases []spec
	for _, p := range []string{"colorless", "truecolor"} {
		for _, sz := range [][2]int{{140, 40}, {84, 24}} {
			cases = append(cases, spec{"shell.loading." + dim(sz) + "." + p, sz[0], sz[1], p, loading, nil})
		}
		cases = append(cases, spec{"shell.loaderr.100x30." + p, 100, 30, p, failed, nil})
	}
	cases = append(cases,
		spec{"shell.loading.80x24.colorless.portrait", 80, 24, "colorless", loading, nil},
		spec{"shell.loading.60x20.colorless.portrait-detail", 60, 20, "colorless", loading, []string{"enter"}},
		spec{"shell.loaderr.60x20.colorless", 60, 20, "colorless", failed, nil},
		spec{"shell.loading.140x40.colorless.first", 140, 40, "colorless", func() Model { return loadingModel(loadTestDir) }, nil},
	)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			view := renderUI(t, c.w, c.h, c.profile, c.keys, c.build)
			assertFrame(t, view, c.w, c.h)
			if c.profile == "colorless" && strings.Contains(view, "\x1b") {
				t.Fatal("colorless frame contains ESC")
			}
			shellLoadGoldenCheck(t, c.name, view)
		})
	}
}

func TestShellPortraitGoldens(t *testing.T) {
	tiny := loadFiles(t, mustFiles(t, fixture.TinyQwen))
	tiny.RepoID, tiny.Revision = "org/tiny-qwen", "0123456789abcdef0123456789abcdef01234567"
	code := loadFiles(t, mustCode(t))
	code.RepoID, code.Revision = "org/code-exec", "89abcdef0123456789abcdef0123456789abcdef"
	type spec struct {
		name    string
		m       *model.Model
		w, h    int
		profile string
		keys    []string
	}
	var cases []spec
	for _, sz := range [][2]int{{80, 24}, {60, 20}} {
		for _, p := range []string{"colorless", "truecolor"} {
			if sz[0] == 60 && p == "truecolor" {
				continue
			}
			cases = append(cases,
				spec{"shell.portrait.code." + dim(sz) + "." + p + ".list", code, sz[0], sz[1], p, []string{"2"}},
				spec{"shell.portrait.code." + dim(sz) + "." + p + ".detail", code, sz[0], sz[1], p, []string{"]", "enter"}},
			)
		}
		cases = append(cases, spec{"shell.portrait.tiny." + dim(sz) + ".colorless.model", tiny, sz[0], sz[1], "colorless", nil})
	}
	cases = append(cases,
		spec{"shell.portrait.tiny.80x24.colorless.layers", tiny, 80, 24, "colorless", []string{"3"}},
		spec{"shell.portrait.tiny.80x24.colorless.config-derived", tiny, 80, 24, "colorless", append([]string{"4"}, append(repeatKey("down", 10), "enter")...)},
		spec{"shell.toosmall.30x10.colorless", tiny, 30, 10, "colorless", nil},
		spec{"shell.toosmall.30x10.truecolor", tiny, 30, 10, "truecolor", nil},
	)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			view := renderPrepared(t, c.m, scan.Run(c.m), c.w, c.h, c.profile, c.keys)
			assertFrame(t, view, c.w, c.h)
			if c.profile == "colorless" && strings.Contains(view, "\x1b") {
				t.Fatal("colorless frame contains ESC")
			}
			shellLoadGoldenCheck(t, c.name, view)
		})
	}
}

func TestShellPortraitProfiles(t *testing.T) {
	m := loadFiles(t, mustCode(t))
	m.RepoID = "org/code-exec"
	for _, p := range []string{"truecolor", "ansi256", "ansi16", "colorless", "ascii"} {
		for _, keys := range [][]string{nil, {"2"}, {"3", "enter"}, {"4"}, {"?"}} {
			view := renderPrepared(t, m, scan.Run(m), 60, 20, p, keys)
			assertFrame(t, view, 60, 20)
			if p == "ascii" {
				for i := 0; i < len(view); i++ {
					if view[i] >= 0x80 {
						t.Fatalf("ascii frame has byte %#x at %d (keys %v)", view[i], i, keys)
					}
				}
			}
		}
	}
	for _, p := range []string{"truecolor", "ansi16", "colorless", "ascii"} {
		view := renderUI(t, 100, 30, p, nil, func() Model { return loadingShell(100, 30, loadTestDir, midLoad...).(Model) })
		assertFrame(t, view, 100, 30)
		if p == "ascii" {
			for i := 0; i < len(view); i++ {
				if view[i] >= 0x80 {
					t.Fatalf("ascii loading frame has byte %#x at %d", view[i], i)
				}
			}
		}
	}
}

func TestShellLoadAndPortraitBudgets(t *testing.T) {
	for _, sz := range [][2]int{{140, 40}, {80, 24}} {
		x := loadingShell(sz[0], sz[1], loadTestDir, midLoad...)
		_ = x.View()
		_ = x.View()
		if d := median(9, func() { _ = x.View() }); d > budget(2*time.Millisecond) {
			t.Errorf("%v loading cached View %s", sz, d)
		}
		i := 0
		if d := median(9, func() {
			i++
			x, _ = x.Update(scanProgMsg{p: model.Progress{Phase: model.PhaseLayout, Done: i, Total: 22}})
		}); d > budget(500*time.Microsecond) {
			t.Errorf("%v loading Update %s", sz, d)
		}
		if d := median(9, func() {
			i++
			x, _ = x.Update(scanProgMsg{p: model.Progress{Phase: model.PhaseLayout, Done: i, Total: 1 << 20}})
			_ = x.View()
		}); d > budget(2*time.Millisecond) {
			t.Errorf("%v loading cold View %s", sz, d)
		}
		if d := median(9, func() { x, _ = x.Update(tea.WindowSizeMsg{Width: sz[0] - 1, Height: sz[1]}) }); d > budget(5*time.Millisecond) {
			t.Errorf("%v loading Resize %s", sz, d)
		}
	}
	files, _, err := fixture.TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	x := press(shellModel(t, m, 80, 24), "2")
	_ = x.View()
	_ = x.View()
	if d := median(9, func() { _ = x.View() }); d > budget(2*time.Millisecond) {
		t.Errorf("portrait cached View %s", d)
	}
	if d := median(9, func() { x, _ = x.Update(tea.KeyMsg{Type: tea.KeyDown}) }); d > budget(500*time.Microsecond) {
		t.Errorf("portrait Update %s", d)
	}
	if d := median(9, func() { x, _ = x.Update(tea.KeyMsg{Type: tea.KeyDown}); _ = x.View() }); d > budget(2*time.Millisecond) {
		t.Errorf("portrait cold View %s", d)
	}
	w := 80
	if d := median(9, func() {
		w = 140 - w + 60 // 80 <-> 60
		x, _ = x.Update(tea.WindowSizeMsg{Width: w, Height: 24})
	}); d > budget(5*time.Millisecond) {
		t.Errorf("portrait Resize %s", d)
	}
}

// FuzzUpdate found a panic in the too-small notice for a zero or negative
// terminal size (testdata/fuzz/FuzzUpdate/285acbaf8430bedf).
func TestShellTooSmallNonPositiveSize(t *testing.T) {
	m := shellFixtures(t)["tiny"]
	for _, sz := range [][2]int{{0, 0}, {-8, -4}, {20, -4}, {-20, 7}, {30, 0}} {
		x := shellModel(t, m, sz[0], sz[1])
		view := x.View()
		if want := max(1, sz[1]); strings.Count(view, "\n")+1 != want {
			t.Errorf("%v: %d lines, want %d", sz, strings.Count(view, "\n")+1, want)
		}
		if _, cmd := x.Update(keyMsg("q")); cmd == nil {
			t.Errorf("%v: q did not quit", sz)
		}
	}
}
