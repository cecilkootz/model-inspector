package tui

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/cecilkootz/model-inspector/internal/fixture"
	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/safetensors"
	"github.com/cecilkootz/model-inspector/internal/scan"
)

func rawBad(s string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && ((s[j] >= '0' && s[j] <= '9') || s[j] == ';' || s[j] == ':') {
				j++
			}
			if j < len(s) && s[j] == 'm' {
				i = j
				continue
			}
		}
		b.WriteByte(s[i])
	}
	for _, r := range b.String() {
		switch {
		case r == '\n':
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
			return fmt.Sprintf("%U", r), true
		case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069, r >= 0x200b && r <= 0x200f, r == 0xfeff, r == 0x061c:
			return fmt.Sprintf("%U", r), true
		}
	}
	return "", false
}

func loadRepro(t *testing.T, name string) *model.Model {
	t.Helper()
	dir := filepath.Join("testdata", "repro", name)
	m, err := model.Load(dir)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return m
}

func TestEIBNoStackOverflow(t *testing.T) {
	m := loadRepro(t, "eib")
	var nbytes int64
	for _, tensor := range m.Tensors {
		if tensor.ByteLen() > nbytes {
			nbytes = tensor.ByteLen()
		}
	}
	if nbytes < 1<<62 {
		t.Fatalf("byte length %d, want at least 2^62", nbytes)
	}
	ui := New(m, scan.Result{})
	sized, _ := ui.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	view := sized.View()
	if view == "" {
		t.Fatal("empty view")
	}
	if r, bad := rawBad(view); bad {
		t.Fatalf("raw %s", r)
	}
	if strings.Contains(view, "-9223372036854775808") {
		t.Fatal("legend printed MinInt64")
	}
}

func TestIdxOnlyNoOOM(t *testing.T) {
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	m := loadRepro(t, "idxonly")
	if !m.Partial {
		t.Fatal("absurd layer index did not mark the load partial")
	}
	joined := strings.Join(m.Warnings, "\n")
	if !strings.Contains(joined, "layer index 99999999 exceeds 4096; dropped") {
		t.Fatalf("load warnings %q", joined)
	}
	res := scan.Run(m)
	var files, layer scan.Check
	var sawLayer bool
	for _, c := range res.Checks {
		switch c.ID {
		case scan.CheckFiles:
			files = c
		case scan.CheckMetadata:
			layer = c
			sawLayer = true
		}
	}
	if files.Name != "Files present" || files.Status != scan.StatusPass {
		t.Fatalf("files %+v", files)
	}
	if !sawLayer || layer.Name != "Layer index" || layer.Status != scan.StatusFail {
		t.Fatalf("layer check %+v", layer)
	}
	blob := layer.Summary + "\n" + strings.Join(layer.Details, "\n")
	if !strings.Contains(blob, "layer index 99999999 exceeds 4096; dropped") {
		t.Fatalf("layer check did not carry the warning: %s", blob)
	}
	ui := New(m, scan.Result{})
	if ui.derived == nil {
		t.Fatal("no derived grid")
	}
	for _, row := range ui.derived.heat.Rows {
		if len(row.Cells) > 1 {
			t.Fatalf("heatmap width %d for one absurd index", len(row.Cells))
		}
	}
	view := ui.View()
	_ = view
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if after.Alloc > before.Alloc+64<<20 && after.TotalAlloc > before.TotalAlloc+256<<20 {
		t.Fatalf("allocation grew by alloc %d total %d", after.Alloc-before.Alloc, after.TotalAlloc-before.TotalAlloc)
	}
}

func TestLayersCursorCache(t *testing.T) {
	t.Setenv("MODEL_INSPECTOR_MOTION", "0")
	t.Setenv("NO_COLOR", "")
	t.Setenv("LC_ALL", "C.UTF-8")
	SetNoColor(false)
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)
	t.Cleanup(func() {
		SetNoColor(false)
		lipgloss.SetColorProfile(termenv.Ascii)
	})
	m := loadFiles(t, mustFiles(t, fixture.TinyQwenMLXQuant))
	ui := New(m, scan.Run(m))
	var x tea.Model = ui
	x, _ = x.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	lipgloss.SetColorProfile(termenv.TrueColor)
	before := x.View()
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyDown})
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyDown})
	lipgloss.SetColorProfile(termenv.TrueColor)
	after := x.View()
	if before == after {
		got := x.(Model)
		t.Fatalf("layers highlight did not move after Down, Down (cursor %d groups %d esc %v)", got.lay.row, len(got.m.Groups), strings.Contains(after, "\x1b"))
	}
	got := x.(Model)
	if got.lay == nil || got.lay.row == 0 {
		t.Fatalf("cursor %d", got.lay.row)
	}
	beforeEnter := got.currentLayer()
	wasOpen := got.lay.isOpen(beforeEnter.path)
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got = x.(Model)
	if (beforeEnter.kind == rowGroup || beforeEnter.kind == rowSub) && got.lay.isOpen(beforeEnter.path) == wasOpen {
		t.Fatalf("enter did not toggle %s", beforeEnter.path)
	}
	tensorBefore := x.View()
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyDown})
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyDown})
	if x.View() == tensorBefore {
		t.Fatal("tensor highlight did not move after Down, Down")
	}
}

func TestPalettePasteTruncates(t *testing.T) {
	t.Setenv("MODEL_INSPECTOR_MOTION", "0")
	m := loadFiles(t, mustFiles(t, fixture.TinyQwenMLXQuant))
	ui := New(m, scan.Result{})
	var x tea.Model = ui
	x, _ = x.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	big := make([]rune, 2_000_000)
	for i := range big {
		big[i] = 'a'
	}
	big[0] = '\x1b'
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: big, Paste: true})
	got := x.(Model)
	if n := len([]rune(got.palette.query)); n > 64 {
		t.Fatalf("palette query %d runes", n)
	}
	view := x.View()
	if r, bad := rawBad(view); bad {
		t.Fatalf("raw %s", r)
	}
}

func TestPaletteRowZones(t *testing.T) {
	t.Setenv("MODEL_INSPECTOR_MOTION", "0")
	t.Setenv("MODEL_INSPECTOR_MOUSE", "1")
	m := loadFiles(t, mustFiles(t, fixture.TinyQwenMLXQuant))
	ui := New(m, scan.Result{})
	var x tea.Model = ui
	x, _ = x.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	_ = x.View()
	got := x.(Model)
	z, ok := got.zones.Hit(0, 0)
	found := false
	var row uiRect
	if got.zones != nil {
		// Walk by probing the card. The first row zone is enough.
		for y := 0; y < got.height; y++ {
			for x0 := 0; x0 < got.width; x0 += 4 {
				hit, okHit := got.zones.Hit(x0, y)
				if okHit && strings.HasPrefix(hit.ID, "palette:row:") {
					found = true
					row = uiRect{x0, y}
					z = hit
					ok = true
					break
				}
			}
			if found {
				break
			}
		}
	}
	if !found || !ok || !strings.HasPrefix(z.ID, "palette:row:") {
		t.Fatal("palette row zones were not created")
	}
	x, _ = x.Update(tea.MouseMsg{X: row.x, Y: row.y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if x.(Model).palette.cursor < 0 {
		t.Fatal("click did not keep a cursor")
	}
}

type uiRect struct{ x, y int }

func TestTerminalRestoreOnPanic(t *testing.T) {
	var buf bytes.Buffer
	prev := termOut
	termOut = &buf
	defer func() { termOut = prev }()
	func() {
		defer func() { _ = recover() }()
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					restoreTerminal()
					panic(rec)
				}
			}()
			panic("boom")
		}()
	}()
	if !strings.Contains(buf.String(), "\x1b[?1049l") || !strings.Contains(buf.String(), "\x1b[?1000l") || !strings.Contains(buf.String(), "\x1b[?25h") {
		t.Fatalf("restore %q", buf.String())
	}
}

func TestView50kBudget(t *testing.T) {
	t.Setenv("MODEL_INSPECTOR_MOTION", "0")
	t.Setenv("NO_COLOR", "1")
	lipgloss.SetColorProfile(termenv.Ascii)
	m := syntheticLayers(50_000)
	start := time.Now()
	ui := New(m, scan.Result{})
	if time.Since(start) > 2*time.Second {
		t.Fatalf("New of 50k layers took %s", time.Since(start))
	}
	sized, _ := ui.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	t0 := time.Now()
	view := sized.View()
	if d := time.Since(t0); d > raceScale*50*time.Millisecond {
		t.Fatalf("50k View took %s", d)
	}
	if view == "" {
		t.Fatal("empty")
	}
	got := sized.(Model)
	if !got.m.Partial {
		t.Fatal("50k layer indices were not marked partial")
	}
	for _, row := range got.derived.heat.Rows {
		if len(row.Cells) != model.MaxLayerIndex+1 {
			t.Fatalf("columns %d, want %d", len(row.Cells), model.MaxLayerIndex+1)
		}
		if !row.Cached() {
			t.Fatal("spread was not cached at load")
		}
	}
}

func syntheticLayers(n int) *model.Model {
	m := &model.Model{Dir: "/model", Format: "safetensors", DTypes: map[string]*model.DTypeStat{}}
	m.Tensors = make([]*model.Tensor, n)
	for i := 0; i < n; i++ {
		m.Tensors[i] = &model.Tensor{
			TensorInfo: safetensors.TensorInfo{
				Name:        fmt.Sprintf("model.layers.%d.self_attn.q_proj.weight", i),
				DType:       "U8",
				Shape:       []int64{8},
				DataOffsets: [2]int64{0, 64},
			},
			Kind:  model.KindLayer,
			Layer: i,
			Role:  model.RolePlain,
			Group: fmt.Sprintf("layer %d", i),
		}
	}
	return m
}

func TestHeroOmitsDuplicateDir(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("LC_ALL", "C.UTF-8")
	m := &model.Model{Dir: "/models/demo", Format: "safetensors"}
	view := renderIdentity(m, 160)
	if strings.Count(view, "/models/demo") != 1 {
		t.Fatalf("dir count %d\n%s", strings.Count(view, "/models/demo"), view)
	}
}

func TestRestoreSequence(t *testing.T) {
	if !strings.Contains(termRestore, "\x1b[?1049l") || !strings.Contains(termRestore, "\x1b[?1002l") || !strings.Contains(termRestore, "\x1b[?1003l") || !strings.Contains(termRestore, "\x1b[?1006l") {
		t.Fatal(termRestore)
	}
}

var (
	fzOnce   sync.Once
	fzModels []*model.Model
	fzRes    []scan.Result
)

var fzKeys = []tea.KeyType{tea.KeyEnter, tea.KeyEsc, tea.KeyBackspace, tea.KeyTab, tea.KeyShiftTab, tea.KeyUp, tea.KeyDown, tea.KeyLeft, tea.KeyRight, tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd, tea.KeyCtrlK, tea.KeySpace, tea.KeyDelete}
var fzRunes = []rune("1234qwzm?:<>hjklgGcse/ \x1b\x00\u202e\u0301\U0001F600\x9b<")

func FuzzUpdate(f *testing.F) {
	f.Add([]byte{0, 3, 1, 2, 3, 4, 5, 6, 7, 8, 9})
	f.Add([]byte{2, 255, 255, 3, 0, 0, 4, 9, 9, 1, 1})
	f.Fuzz(func(t *testing.T, data []byte) {
		fzOnce.Do(func() {
			os.Setenv("MODEL_INSPECTOR_MOTION", "0")
			if m := loadDir(filepath.Join("testdata", "repro", "eib")); m != nil {
				fzModels = append(fzModels, m)
				fzRes = append(fzRes, scan.Result{})
			}
			if m := loadDir(filepath.Join("testdata", "repro", "idxonly")); m != nil {
				fzModels = append(fzModels, m)
				fzRes = append(fzRes, scan.Result{})
			}
			fzModels = append(fzModels, syntheticLayers(4), &model.Model{})
			fzRes = append(fzRes, scan.Result{}, scan.Result{})
		})
		if len(data) < 2 || len(fzModels) == 0 {
			return
		}
		mi := int(data[0]) % len(fzModels)
		var x tea.Model = New(fzModels[mi], fzRes[mi])
		base := time.Unix(0, 0)
		i := 1
		next := func() int {
			if i >= len(data) {
				return 0
			}
			v := int(data[i])
			i++
			return v
		}
		steps := 0
		for i < len(data) && steps < 200 {
			steps++
			switch next() % 8 {
			case 0:
				w, h := next()*3-20, next()*2-10
				x, _ = x.Update(tea.WindowSizeMsg{Width: w, Height: h})
			case 1:
				x, _ = x.Update(tea.KeyMsg{Type: fzKeys[next()%len(fzKeys)]})
			case 2:
				n := next()%5 + 1
				rs := make([]rune, n)
				for k := range rs {
					rs[k] = fzRunes[next()%len(fzRunes)]
				}
				x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: rs, Paste: next()%2 == 0})
			case 3, 4:
				x, _ = x.Update(tea.MouseMsg{X: int(int8(next())) * (next()%40 + 1), Y: int(int8(next())) * (next()%20 + 1),
					Button: tea.MouseButton(next() % 12), Action: tea.MouseAction(next() % 3)})
			case 5:
				x, _ = x.Update(tickMsg(base.Add(time.Duration(next()) * time.Millisecond)))
			case 6:
				if next()%2 == 0 {
					x, _ = x.Update(tea.FocusMsg{})
				} else {
					x, _ = x.Update(tea.BlurMsg{})
				}
			case 7:
				x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{rune(next())}})
			}
			v := x.View()
			if r, bad := rawBad(v); bad {
				t.Fatalf("raw %s in view", r)
			}
		}
	})
}

func loadDir(dir string) *model.Model {
	m, err := model.Load(dir)
	if err != nil {
		return nil
	}
	return m
}
