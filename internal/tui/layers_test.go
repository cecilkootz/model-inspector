package tui

import (
	"bytes"
	"math"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/cecilkootz/model-inspector/internal/fixture"
	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/safetensors"
	"github.com/cecilkootz/model-inspector/internal/scan"
)

func loadLayersV2(t *testing.T) *model.Model {
	t.Helper()
	files, err := fixture.LayersV2()
	if err != nil {
		t.Fatal(err)
	}
	return loadFiles(t, files)
}

func openLayers(t *testing.T, m *model.Model, w, h int) Model {
	t.Helper()
	t.Setenv("MODEL_INSPECTOR_MOTION", "0")
	ui := New(m, scan.Result{})
	var x tea.Model = ui
	x, _ = x.Update(tea.WindowSizeMsg{Width: w, Height: h})
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	return x.(Model)
}

func TestLayersGateKeepsBadgeWithoutDiamond(t *testing.T) {
	t.Setenv("LC_ALL", "C.UTF-8")
	t.Setenv("LANG", "C.UTF-8")
	m := loadLayersV2(t)
	ui := openLayers(t, m, 140, 40)
	ui.lay.ensure(m)
	var layer int
	for i, row := range ui.lay.rows {
		g := ui.groupAt(row.gidx)
		if row.kind == rowGroup && g != nil && g.Layer == 0 && g.Kind == model.KindLayer {
			layer = i
		}
	}
	ui.lay.row = layer
	ui.layersExpandAll()
	ui.lay.ensure(m)
	var gate, up, down string
	for _, row := range ui.lay.rows {
		if row.kind != rowTensor {
			continue
		}
		lt, ok := ui.layerTensor(row)
		if !ok {
			continue
		}
		plain := ui.layerPlain(row, 120)
		switch {
		case strings.Contains(lt.FullName, "mlp.gate"):
			gate = plain
		case strings.Contains(lt.FullName, "up_proj"):
			up = plain
		case strings.Contains(lt.FullName, "mlp.down_proj"):
			down = plain
		}
	}
	if gate == "" || up == "" || down == "" {
		t.Fatalf("missing rows gate=%q up=%q down=%q", gate, up, down)
	}
	if !strings.Contains(gate, "q8") || strings.Contains(gate, "◆") || strings.Contains(gate, "*") {
		t.Fatalf("gate row = %s", gate)
	}
	if !strings.Contains(up, "q8") || !strings.Contains(up, "◆") {
		t.Fatalf("up row = %s", up)
	}
	if !strings.Contains(down, "q2") || !strings.Contains(down, "◆") {
		t.Fatalf("down row = %s", down)
	}
}

func TestLayersExpertsAndPLE(t *testing.T) {
	t.Setenv("LC_ALL", "C.UTF-8")
	t.Setenv("LANG", "C.UTF-8")
	m := loadLayersV2(t)
	ui := openLayers(t, m, 140, 40)
	view := ui.View()
	for _, want := range []string{"n-gram table (PLE)", "⌂", "×3", "hyper_connection", "4·2", "2·2"} {
		if !strings.Contains(view, want) {
			t.Errorf("missing %q\n%s", want, view)
		}
	}
	var stacked, indexed bool
	for _, g := range m.Groups {
		if g == nil || g.Kind != model.KindLayer {
			continue
		}
		n, ok := g.ExpertCount()
		if !ok {
			continue
		}
		if g.Layer == 0 && n == 4 && g.ExpertStacked() {
			stacked = true
		}
		if g.Layer == 1 && n == 2 && !g.ExpertStacked() {
			indexed = true
		}
	}
	if !stacked || !indexed {
		t.Fatalf("stacked %v indexed %v", stacked, indexed)
	}
}

func TestLayersShardedNgramMark(t *testing.T) {
	files, err := fixture.ShardedNgram()
	if err != nil {
		t.Fatal(err)
	}
	m := loadFiles(t, files)
	ui := openLayers(t, m, 100, 30)
	t.Setenv("LC_ALL", "C.UTF-8")
	t.Setenv("NO_COLOR", "1")
	lipgloss.SetColorProfile(termenv.Ascii)
	view := ui.View()
	if strings.Contains(view, "\x1b") {
		t.Fatal("colorless frame contains ESC")
	}
	for _, want := range []string{"n-gram table (PLE)", "⌂", "×" + "128"} {
		if !strings.Contains(view, want) {
			t.Errorf("missing %q\n%s", want, view)
		}
	}
}

func TestLayersHostileDtype(t *testing.T) {
	dtype := "BF16\x1b\u0085\r\u202e"
	name := "model.layers.0.self_attn.q_proj.weight"
	m := &model.Model{
		Format: "safetensors", Dir: "/model",
		Groups: []*model.Group{{
			Key: "model.layers.0", Kind: model.KindLayer, Layer: 0,
			Tensors: []*model.Tensor{{
				TensorInfo: safetensors.TensorInfo{Name: name, DType: dtype, Shape: []int64{4}},
				Kind:       model.KindLayer, Layer: 0, Group: "model.layers.0", Role: model.RolePlain,
			}},
		}},
	}
	m.Tensors = m.Groups[0].Tensors
	ui := openLayers(t, m, 200, 50)
	ui.lay.setOpen("dec", true)
	ui.lay.setOpen("g:model.layers.0", true)
	ui.lay.ensure(m)
	found := false
	for i, row := range ui.lay.rows {
		if row.kind == rowTensor {
			ui.lay.row = i
			ui.lay.cursorPath = row.path
			found = true
			break
		}
	}
	if !found {
		t.Fatal("tensor row missing")
	}
	tokens := []string{"<U+001B>", "<U+0085>", "<U+000D>", "<U+202E>"}
	check := func(color bool) {
		t.Helper()
		if ui.cache != nil {
			ui.cache.key = ""
		}
		if color {
			t.Setenv("NO_COLOR", "")
			SetNoColor(false)
			lipgloss.SetColorProfile(termenv.TrueColor)
		} else {
			t.Setenv("NO_COLOR", "1")
			SetNoColor(true)
		}
		view := ui.View()
		for _, raw := range []string{"\u0085", "\r", "\u202e"} {
			if strings.Contains(view, raw) {
				t.Fatalf("raw dtype leaked color=%v", color)
			}
		}
		if !color && strings.Contains(view, "\x1b") {
			t.Fatal("raw ESC in colorless view")
		}
		for _, tok := range tokens {
			if !strings.Contains(view, tok) {
				t.Fatalf("missing %s color=%v\n%s", tok, color, view)
			}
		}
	}
	check(true)
	check(false)
}

func TestLayersHostileName(t *testing.T) {
	name := "model.layers.0.self_attn.q_proj.weight\x1b\u202e"
	m := &model.Model{
		Format: "safetensors", Dir: "/model",
		Groups: []*model.Group{{
			Key: "layer 0", Kind: model.KindLayer, Layer: 0,
			Tensors: []*model.Tensor{{
				TensorInfo: safetensors.TensorInfo{Name: name, DType: "U8", Shape: []int64{4}},
				Kind:       model.KindLayer, Layer: 0, Group: "layer 0", Role: model.RolePlain,
			}},
		}},
	}
	m.Tensors = m.Groups[0].Tensors
	ui := openLayers(t, m, 80, 24)
	t.Setenv("NO_COLOR", "1")
	t.Setenv("LC_ALL", "C.UTF-8")
	lipgloss.SetColorProfile(termenv.Ascii)
	ui.layersExpand()
	ui.layersExpand()
	view := ui.View()
	if strings.Contains(view, "\x1b") {
		t.Fatal("raw ESC in colorless layers view")
	}
	if !strings.Contains(view, "<U+001B>") {
		t.Fatalf("token missing\n%s", view)
	}
}

func TestLayersAbsurdShape(t *testing.T) {
	shape := make([]int64, 40)
	for i := range shape {
		shape[i] = math.MaxInt64
	}
	got := shapeLimited(shape)
	if strings.Contains(got, "panic") || len(got) > 80 {
		t.Fatalf("shape %s", got)
	}
	m := &model.Model{
		Format: "safetensors", Dir: "/model",
		Groups: []*model.Group{{
			Key: "embeddings", Kind: model.KindEmbeddings,
			Tensors: []*model.Tensor{{
				TensorInfo: safetensors.TensorInfo{Name: "model.embed_tokens.weight", DType: "F16", Shape: shape},
				Kind:       model.KindEmbeddings, Group: "embeddings",
			}},
		}},
	}
	m.Tensors = m.Groups[0].Tensors
	ui := openLayers(t, m, 80, 24)
	ui.layersEnter(false)
	view := ui.View()
	if !strings.Contains(view, "embed") {
		t.Fatalf("view\n%s", view)
	}
}

func TestLayersPageAndSearch(t *testing.T) {
	g := &model.Group{Key: "embeddings", Kind: model.KindEmbeddings}
	m := &model.Model{Format: "safetensors", Dir: "/model", Groups: []*model.Group{g}}
	for i := 0; i < 120; i++ {
		name := "model.embed_tokens.weight"
		if i > 0 {
			name = "model.embed_extra." + itoa(i)
		}
		if i == 77 {
			name = "model.embed_needle.weight"
		}
		tensor := &model.Tensor{
			TensorInfo: safetensors.TensorInfo{Name: name, DType: "F16", Shape: []int64{2}},
			Kind:       model.KindEmbeddings, Group: g.Key,
		}
		g.Tensors = append(g.Tensors, tensor)
		m.Tensors = append(m.Tensors, tensor)
	}
	ui := openLayers(t, m, 80, 24)
	ui.layersEnter(false)
	ui.lay.ensure(m)
	tensors := 0
	more := false
	for _, row := range ui.lay.rows {
		if row.kind == rowTensor {
			tensors++
		}
		if row.kind == rowMore && row.more && !row.lead {
			more = true
		}
	}
	if tensors > pageSize || !more {
		t.Fatalf("tensors %d more %v rows %d", tensors, more, len(ui.lay.rows))
	}
	ui.openLayerSearch()
	ui.lay.query = "needle"
	ui.lay.filter = true
	ui.lay.largeWait = false
	ui.runLayerSearch()
	found := false
	for _, row := range ui.lay.rows {
		if row.kind == rowTensor && row.match {
			lt, ok := ui.layerTensor(row)
			if ok && strings.Contains(lt.FullName, "needle") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("filter did not reveal the needle")
	}
}

func TestLayersCopyName(t *testing.T) {
	m := loadLayersV2(t)
	t.Setenv("TERM", "xterm-256color")
	ui := openLayers(t, m, 80, 24)
	on := true
	var buf bytes.Buffer
	ui.lay.copyTTY = &on
	ui.lay.copyTo = &buf
	ui.active = tabLayers
	ui.lay.ensure(m)
	for i, row := range ui.lay.rows {
		if row.path == "g:embeddings" {
			ui.lay.row = i
			ui.lay.cursorPath = row.path
		}
	}
	key := ui.layersGroup().Key
	ui.copyLayers(false)
	if !strings.Contains(ui.notice, key) || !strings.Contains(buf.String(), "\x1b]52;") {
		t.Fatalf("notice %q osc %q", ui.notice, buf.String())
	}
}

func TestLayersNoGroups(t *testing.T) {
	m := syntheticLayers(8)
	ui := openLayers(t, m, 80, 24)
	view := ui.View()
	if !strings.Contains(view, "No groups") {
		t.Fatalf("view\n%s", view)
	}
	ui.layersEnter(false)
	ui.layersExpand()
	ui.layersMove(3)
	_ = ui.View()
}
