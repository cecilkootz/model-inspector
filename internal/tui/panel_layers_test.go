package tui

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/cecilkootz/model-inspector/internal/fixture"
	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/safetensors"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

// layersProfile sets the colour profile the way renderPrepared does, for tests
// that call the panel directly.
func layersProfile(t *testing.T, profile string) {
	t.Helper()
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
	t.Setenv("MODEL_INSPECTOR_MOTION", "0")
	t.Setenv("MODEL_INSPECTOR_THEME", "dark")
	t.Setenv("LC_ALL", "C.UTF-8")
	t.Setenv("LANG", "C.UTF-8")
	SetNoColor(false)
	if profile == "colorless" {
		os.Setenv("NO_COLOR", "1")
		lipgloss.SetColorProfile(termenv.Ascii)
	} else {
		os.Unsetenv("NO_COLOR")
		lipgloss.SetColorProfile(termenv.TrueColor)
	}
	lipgloss.SetHasDarkBackground(true)
}

func layersPanelOf(x tea.Model) *layersPanel { return &layersPanel{t: ptrTo(x.(Model))} }

func ptrTo(m Model) *Model { return &m }

func layersRowNames(x tea.Model, w int) []string {
	var out []string
	for _, r := range layersPanelOf(x).rows(w) {
		out = append(out, strings.TrimSpace(ui.Strip(r)))
	}
	return out
}

func hasRow(rows []string, want string) bool {
	for _, r := range rows {
		if strings.Contains(r, want) {
			return true
		}
	}
	return false
}

func TestLayersPanelTree(t *testing.T) {
	layersProfile(t, "colorless")
	m := loadLayersV2(t)
	x := press(shellModel(t, m, 140, 40), "3")
	p := layersPanelOf(x)
	rows := p.rows(40)
	if len(rows) != 6 {
		t.Fatalf("collapsed rows %d, want 6: %v", len(rows), layersRowNames(x, 40))
	}
	for _, want := range []string{"embeddings", "n-gram table", "decoder ×2", "norms", "hyper_connection", "lm_head"} {
		if !hasRow(layersRowNames(x, 40), want) {
			t.Errorf("missing row %q in %v", want, layersRowNames(x, 40))
		}
	}
	if got := p.counter(); !strings.Contains(ui.Strip(got), "2 decoder") {
		t.Errorf("counter %q", got)
	}

	// Moving onto the decoder, → expands it and the layers follow the decoder.
	x = press(x, "down", "down")
	dec := layersPanelOf(x).cursor()
	if names := layersRowNames(x, 40); !strings.Contains(names[dec], "decoder") {
		t.Fatalf("cursor %d is %q", dec, names[dec])
	}
	x = press(x, "right")
	names := layersRowNames(x, 40)
	if len(names) != 8 || !strings.Contains(names[dec+1], "layer 0") || !strings.Contains(names[dec+2], "layer 1") {
		t.Fatalf("expanded rows %v", names)
	}
	if !strings.Contains(names[dec+1], ui.G().Linear) || !strings.Contains(names[dec+2], ui.G().Full) {
		t.Errorf("attention glyphs missing: %v", names[dec+1:dec+3])
	}
	if got := x.(Model).sh; got.inDetail {
		t.Fatal("→ on the decoder moved focus to the detail pane")
	}

	// → on an open decoder steps into the first layer; ← climbs back, then folds.
	x = press(x, "right")
	if got := layersPanelOf(x).cursor(); got != dec+1 {
		t.Fatalf("→ on open decoder: cursor %d, want %d", got, dec+1)
	}
	x = press(x, "down", "left")
	if got := layersPanelOf(x).cursor(); got != dec {
		t.Fatalf("← on a layer: cursor %d, want the decoder at %d", got, dec)
	}
	x = press(x, "left")
	if got := len(layersRowNames(x, 40)); got != 6 {
		t.Fatalf("← on an open decoder left %d rows, want 6", got)
	}

	// ⏎ and space fold the decoder; ⏎ elsewhere opens the detail pane.
	x = press(x, "enter")
	if len(layersRowNames(x, 40)) != 8 || x.(Model).sh.inDetail {
		t.Fatalf("⏎ on the decoder: rows %d, detail %v", len(layersRowNames(x, 40)), x.(Model).sh.inDetail)
	}
	x = press(x, "space")
	if len(layersRowNames(x, 40)) != 6 {
		t.Fatal("space did not fold the decoder")
	}
	x = press(x, "up", "enter")
	if !x.(Model).sh.inDetail {
		t.Fatal("⏎ on a plain group did not open the detail pane")
	}
	x = press(x, "esc", "right", "left", "h", "l")
	if len(layersRowNames(x, 40)) != 6 {
		t.Fatal("fold keys on a plain group changed the tree")
	}
}

func TestLayersPanelCursorClamps(t *testing.T) {
	layersProfile(t, "colorless")
	x := press(shellModel(t, loadLayersV2(t), 120, 30), "3")
	x = press(x, "right", "up", "up", "up", "up", "up", "up", "up", "up", "up", "up")
	if got := layersPanelOf(x).cursor(); got != 0 {
		t.Fatalf("cursor above the first row: %d", got)
	}
	x = press(x, "G")
	last := len(layersRowNames(x, 40)) - 1
	if got := layersPanelOf(x).cursor(); got != last {
		t.Fatalf("G: cursor %d, want %d", got, last)
	}
	x = press(x, "down", "down", "j")
	if got := layersPanelOf(x).cursor(); got != last {
		t.Fatalf("cursor below the last row: %d", got)
	}
	// Folding while the cursor is on a layer must not strand it past the end.
	x = press(x, "g", "down", "down", "right", "down", "left", "left")
	if got, n := layersPanelOf(x).cursor(), len(layersRowNames(x, 40)); got >= n {
		t.Fatalf("cursor %d outside %d rows", got, n)
	}
	// Clicking a row selects it; the double click opens the detail pane.
	x = press(x, "g")
	_ = x.View()
	cur := x.(Model)
	for _, h := range cur.sh.hits {
		if h.id == "sh:row:3:1" {
			x = click(x, h.x+1, h.y)
		}
	}
	if got := layersPanelOf(x).cursor(); got != 1 {
		t.Fatalf("click selected row %d, want 1", got)
	}
}

func TestLayersPanelSummaryAndTitle(t *testing.T) {
	layersProfile(t, "colorless")
	m := loadLayersV2(t)
	x := shellModel(t, m, 140, 40)
	p := layersPanelOf(x)
	sum := p.summary(40)
	if len(sum) != 3 {
		t.Fatalf("summary rows %d, want 3", len(sum))
	}
	if !strings.Contains(ui.Strip(sum[0]), "n-gram table") {
		t.Errorf("largest group first, got %q", ui.Strip(sum[0]))
	}
	x = press(x, "3", "down", "down", "right", "down")
	if got := screenOf(x)[1]; !strings.Contains(got, "[0] layer 0") {
		t.Fatalf("detail title %q", got)
	}
	x = press(x, "up")
	if got := screenOf(x)[1]; !strings.Contains(got, "[0] decoder") {
		t.Fatalf("detail title %q", got)
	}
}

// The detail pane never draws a legacy heading and always fits its pane.
func TestLayersPanelDetailFits(t *testing.T) {
	models := map[string]*model.Model{"layersv2": loadLayersV2(t), "qwen": loadFiles(t, mustShape(t))}
	for _, profile := range []string{"colorless", "truecolor"} {
		layersProfile(t, profile)
		for name, m := range models {
			for _, sz := range [][2]int{{140, 40}, {100, 30}, {84, 24}} {
				x := press(shellModel(t, m, sz[0], sz[1]), "3", "right")
				_ = x.View()
				p := layersPanelOf(x)
				w := sz[0] - shellSideWidth(sz[0]) - 4
				h := sz[1] - 4
				n := len(p.rows(40))
				for i := 0; i < n; i++ {
					p.move(i - p.cursor())
					lines := p.detail(w, h)
					label := fmt.Sprintf("%s %s %dx%d row %d", profile, name, sz[0], sz[1], i)
					if len(lines) > h {
						t.Errorf("%s: %d detail lines, pane has %d", label, len(lines), h)
					}
					for _, l := range lines {
						if ui.Width(l) > w {
							t.Errorf("%s: line is %d cells in a %d-cell pane: %q", label, ui.Width(l), w, ui.Strip(l))
						}
						for _, legacy := range []string{"LAYERS", "TREE", "DETAIL"} {
							if strings.Contains(ui.Strip(l), legacy) {
								t.Errorf("%s: legacy heading %q: %q", label, legacy, ui.Strip(l))
							}
						}
					}
					plain := ui.Strip(strings.Join(lines, "\n"))
					for _, l := range strings.Split(plain, "\n") {
						if strings.Contains(l, "linear") && !strings.Contains(l, ui.G().Linear+" linear") {
							t.Errorf("%s: legend cut mid-word: %q", label, l)
						}
						if strings.Contains(l, ui.G().Full+" ful") && !strings.Contains(l, ui.G().Full+" full") {
							t.Errorf("%s: legend cut mid-word: %q", label, l)
						}
					}
				}
			}
		}
	}
}

func TestLayersPanelHeatmapSections(t *testing.T) {
	layersProfile(t, "colorless")
	x := press(shellModel(t, loadFiles(t, mustShape(t)), 140, 40), "3", "right", "down")
	_ = x.View()
	detail := strings.Join(layersPanelOf(x).detail(90, 36), "\n")
	for _, want := range []string{"BYTES PER LAYER", "TENSORS", "self_attn.q_proj.weight", ui.G().Size} {
		if !strings.Contains(detail, want) {
			t.Errorf("layer detail missing %q\n%s", want, detail)
		}
	}
	x = press(x, "G")
	if mtp := strings.Join(layersPanelOf(x).detail(90, 36), "\n"); strings.Contains(mtp, "BYTES PER LAYER") || !strings.Contains(mtp, "TENSORS") {
		t.Errorf("a plain group shows the heatmap or lacks TENSORS\n%s", mtp)
	}
}

// Scrolling the detail pane moves the tensor window, not the tree cursor.
func TestLayersPanelDetailScroll(t *testing.T) {
	layersProfile(t, "colorless")
	x := press(shellModel(t, benchModel(t), 120, 30), "3", "0")
	_ = x.View()
	cur := layersPanelOf(x).cursor()
	first := strings.Join(screenOf(x), "\n")
	x = press(x, "down", "down", "down")
	_ = x.View()
	if got := x.(Model).lay.side.top; got != 3 {
		t.Fatalf("tensor window starts at %d, want 3", got)
	}
	if layersPanelOf(x).cursor() != cur {
		t.Fatal("scrolling the detail pane moved the tree cursor")
	}
	if strings.Join(screenOf(x), "\n") == first {
		t.Fatal("scrolling did not change the pane")
	}
	x = press(x, "G")
	_ = x.View()
	top := x.(Model).lay.side.top
	if top <= 3 || top > sideKindsMax {
		t.Fatalf("G: window starts at %d", top)
	}
	x = press(x, "g")
	_ = x.View()
	if got := x.(Model).lay.side.top; got != 0 {
		t.Fatalf("g: window starts at %d", got)
	}
	x = press(x, "G", "esc", "down")
	if got := x.(Model).lay.side.top; got != 0 {
		t.Fatalf("moving the selection kept the scroll at %d", got)
	}
}

// Layer indices never size anything: only distinct groups get rows.
func TestLayersPanelSparseIndices(t *testing.T) {
	layersProfile(t, "colorless")
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	m := loadRepro(t, "idxonly")
	for _, sz := range [][2]int{{140, 40}, {84, 24}} {
		x := press(shellModel(t, m, sz[0], sz[1]), "3", "right", "down", "0", "down", "G")
		assertFrame(t, x.View(), sz[0], sz[1])
		s := layersPanelOf(x).side()
		if len(s.layers) > 2 {
			t.Fatalf("%d layer rows for one absurd index", len(s.layers))
		}
	}

	m = syntheticLayers(0)
	for _, idx := range []int{0, 7, 4096, 4097, 99999999, 1 << 40} {
		key := fmt.Sprintf("layer %d", idx)
		g := &model.Group{Key: key, Kind: model.KindLayer, Layer: idx}
		tensor := &model.Tensor{
			TensorInfo: safetensors.TensorInfo{
				Name: fmt.Sprintf("model.layers.%d.self_attn.q_proj.weight", idx), DType: "U8",
				Shape: []int64{8}, DataOffsets: [2]int64{0, 8},
			},
			Kind: model.KindLayer, Layer: idx, Group: key, Role: model.RolePlain,
		}
		g.Tensors, g.Bytes = []*model.Tensor{tensor}, 8
		m.Groups = append(m.Groups, g)
		m.Tensors = append(m.Tensors, tensor)
	}
	x := press(shellModel(t, m, 100, 30), "3", "right", "down", "down", "0", "down")
	assertFrame(t, x.View(), 100, 30)
	if got := len(layersRowNames(press(x, "esc"), 40)); got != 7 {
		t.Fatalf("rows %d, want the decoder plus 6 layers", got)
	}

	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if after.TotalAlloc > before.TotalAlloc+256<<20 {
		t.Fatalf("allocated %d MiB", (after.TotalAlloc-before.TotalAlloc)>>20)
	}
}

// More layers than the cap draw the first rows and one overflow row, and the
// cursor never reaches the overflow row.
func TestLayersPanelLayerCap(t *testing.T) {
	layersProfile(t, "colorless")
	m := layersScaleModel(sideLayersMax+50, 1)
	x := press(shellModel(t, m, 120, 30), "3", "right", "G")
	names := layersRowNames(x, 40)
	if len(names) != 1+sideLayersMax+1 || !strings.Contains(names[len(names)-1], "+50 more layers") {
		t.Fatalf("rows %d, last %q", len(names), names[len(names)-1])
	}
	if got := layersPanelOf(x).cursor(); got != len(names)-2 {
		t.Fatalf("G: cursor %d, want the last layer at %d", got, len(names)-2)
	}
	assertFrame(t, x.View(), 120, 30)
}

func TestLayersPanelHostileNames(t *testing.T) {
	layersProfile(t, "colorless")
	name := "model.layers.0.self_attn.a<b\x1b‮.weight"
	dtype := "BF16\x1b\u0085"
	m := &model.Model{
		Format: "safetensors", Dir: "/model",
		Groups: []*model.Group{{
			Key: "layer 0", Kind: model.KindLayer, Layer: 0, Bytes: 8,
			Tensors: []*model.Tensor{{
				TensorInfo: safetensors.TensorInfo{Name: name, DType: dtype, Shape: []int64{4}, DataOffsets: [2]int64{0, 8}},
				Kind:       model.KindLayer, Layer: 0, Group: "layer 0", Role: model.RolePlain,
			}},
		}, {
			Key: "other", Kind: model.KindOther, Bytes: 8,
			Tensors: []*model.Tensor{{
				TensorInfo: safetensors.TensorInfo{Name: "x.<y\x1b[2J", DType: dtype, Shape: []int64{4}, DataOffsets: [2]int64{0, 8}},
				Kind:       model.KindOther, Group: "other", Role: model.RolePlain,
			}},
		}},
	}
	m.Tensors = []*model.Tensor{m.Groups[0].Tensors[0], m.Groups[1].Tensors[0]}
	var views []string
	for _, keys := range [][]string{{"3"}, {"3", "right"}, {"3", "right", "down"}, {"3", "right", "down", "0"}, {"3", "down"}} {
		views = append(views, press(shellModel(t, m, 140, 40), keys...).View())
	}
	all := strings.Join(views, "\n")
	for _, raw := range []string{"\x1b", "‮", "\u0085"} {
		if strings.Contains(all, raw) {
			t.Fatalf("raw %q reached the screen", raw)
		}
	}
	for _, tok := range []string{"a<U+003C>b<U+001B><U+202E>", "<U+0085>", "x.<U+003C>y<U+001B>"} {
		if !strings.Contains(all, tok) {
			t.Errorf("missing %q", tok)
		}
	}
	// Once through the sanitizer: an escaped token is not escaped again.
	if strings.Contains(all, "<U+003C>U+") {
		t.Fatal("a name was sanitized twice")
	}
}

func TestLayersPanelCopy(t *testing.T) {
	layersProfile(t, "colorless")
	t.Setenv("TERM", "xterm-256color")
	m := loadLayersV2(t)
	x := press(shellModel(t, m, 120, 30), "3", "down", "down", "right", "down")
	key := ""
	for _, g := range m.Groups {
		if g.Kind == model.KindLayer && g.Layer == 0 {
			key = g.Key
		}
	}
	cur := x.(Model)
	on := true
	var buf bytes.Buffer
	cur.cfg.copyTTY, cur.cfg.copyTo = &on, &buf
	y, _ := cur.Update(keyMsg("y"))
	got := y.(Model)
	if key == "" || !strings.Contains(got.notice, key) || !strings.Contains(buf.String(), "\x1b]52;") {
		t.Fatalf("notice %q osc %q", got.notice, buf.String())
	}
	buf.Reset()
	z, _ := press(x, "up").(Model).Update(keyMsg("y"))
	if strings.Contains(buf.String(), "\x1b]52;") {
		t.Fatalf("copied from the decoder row: %q (%q)", buf.String(), z.(Model).notice)
	}
}

func TestLayersPanelBudgets(t *testing.T) {
	layersProfile(t, "truecolor")
	for name, files := range map[string]func(*testing.T) []fixture.File{
		"sharded-ngram": func(t *testing.T) []fixture.File {
			f, err := fixture.ShardedNgram()
			if err != nil {
				t.Fatal(err)
			}
			return f
		},
		"qwen-shape": mustShape,
	} {
		t.Run(name, func(t *testing.T) {
			x := press(shellModel(t, loadFiles(t, files(t)), 140, 40), "3", "right")
			_ = x.View()
			_ = x.View()
			if view := median(9, func() { _ = x.View() }); view > budget(2*time.Millisecond) {
				t.Errorf("cached View %s", view)
			}
			if key := median(9, func() { x, _ = x.Update(tea.KeyMsg{Type: tea.KeyDown}) }); key > budget(500*time.Microsecond) {
				t.Errorf("Update %s", key)
			}
			if resize := median(9, func() { x, _ = x.Update(tea.WindowSizeMsg{Width: 120, Height: 40}) }); resize > budget(5*time.Millisecond) {
				t.Errorf("Resize %s", resize)
			}
		})
	}
}

// BenchmarkLayersPanel moves through a decoder with 48 expanded layers; every
// key changes the frame, so each View is cold.
func BenchmarkLayersPanel(b *testing.B) {
	files, err := fixture.QwenShape()
	if err != nil {
		b.Fatal(err)
	}
	dir := b.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		b.Fatal(err)
	}
	m, err := model.Load(dir)
	if err != nil {
		b.Fatal(err)
	}
	x := press(shellModel(b, m, 140, 40), "3", "right")
	_ = x.View()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k := tea.KeyDown
		if i%2 == 1 {
			k = tea.KeyUp
		}
		x, _ = x.Update(tea.KeyMsg{Type: k})
		_ = x.View()
	}
}

// BenchmarkLayersPanelCap is the worst case the cap allows: every layer row
// is drawn on each cold frame.
func BenchmarkLayersPanelCap(b *testing.B) {
	x := press(shellModel(b, layersScaleModel(sideLayersMax, 1), 140, 40), "3", "right")
	_ = x.View()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		x, _ = x.Update(tea.KeyMsg{Type: tea.KeyDown})
		_ = x.View()
	}
}

func BenchmarkLayersPanelUpdate(b *testing.B) {
	x := press(shellModel(b, layersScaleModel(sideLayersMax, 1), 140, 40), "3", "right")
	_ = x.View()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		x, _ = x.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
}
