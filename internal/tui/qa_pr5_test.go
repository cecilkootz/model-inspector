package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cecilkootz/model-inspector/internal/fixture"
	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/safetensors"
	"github.com/cecilkootz/model-inspector/internal/scan"
)

func qa5Bad(s string) []rune {
	var out []rune
	for _, r := range s {
		if (r < 0x20 && r != '\n') || (r >= 0x7f && r <= 0x9f) || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) || r == 0x200b || r == 0x200e || r == 0x200f || r == 0xfeff || r == 0xfffd {
			out = append(out, r)
		}
	}
	return out
}

func TestQA5_TUI(t *testing.T) {
	SetNoColor(true)
	cfg := fixture.File{Name: "config.json", Data: []byte(`{"model_type":"qwen3_next","num_hidden_layers":2}`)}
	stem := "model.layers.1.ple.ngram_embedding"
	other := fixture.Tensor{Name: "model.layers.0.mlp.down_proj.weight", DType: "BF16", Shape: []int64{64, 64}}
	must := func(b []byte, _ int64, err error) []byte {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	loadDir := func(files ...fixture.File) *model.Model {
		t.Helper()
		dir := t.TempDir()
		if err := fixture.Materialize(dir, files); err != nil {
			t.Fatal(err)
		}
		m, err := model.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}

	t.Run("panic", func(t *testing.T) {
		shard := must(fixture.WriteShard("", []fixture.Tensor{
			other,
			{Name: strings.Repeat("\u023a", 12) + ".ngram_embedding.shard_0.weight", DType: "BF16", Shape: []int64{4, 4}},
		}))
		m := loadDir(cfg, fixture.File{Name: "model.safetensors", Data: shard})
		_ = m.HostCapable()
		_ = m.Outliers()
		for i, view := range qaViews(t, m) {
			if bad := qa5Bad(view); len(bad) > 0 {
				t.Fatalf("view %d: hostile runes %U", i, bad)
			}
		}
		if incomplete(m) != m.Partial {
			t.Fatalf("incomplete %v partial %v", incomplete(m), m.Partial)
		}
	})

	t.Run("doublecount", func(t *testing.T) {
		var dc []fixture.Tensor
		for i := 0; i < 4; i++ {
			b := stem + ".shard_" + string(rune('0'+i))
			dc = append(dc,
				fixture.Tensor{Name: b + ".weight", DType: "BF16", Shape: []int64{4096, 64}},
				fixture.Tensor{Name: b + ".scales", DType: "BF16", Shape: []int64{4096, 64}},
				fixture.Tensor{Name: b + ".biases", DType: "BF16", Shape: []int64{4096, 64}})
		}
		dc = append(dc, other)
		m := loadDir(cfg, fixture.File{Name: "model.safetensors", Data: must(fixture.WriteShard("", dc))})
		mem := m.MemoryEstimate()
		if mem.HostCapableBytes > mem.WeightsBytes {
			t.Fatalf("host %d > weights %d", mem.HostCapableBytes, mem.WeightsBytes)
		}
		want := int64(4 * 3 * 4096 * 64 * 2)
		if mem.HostCapableBytes != want {
			t.Fatalf("host %d want %d (each tensor once)", mem.HostCapableBytes, want)
		}
	})

	t.Run("missing-shard", func(t *testing.T) {
		var a []fixture.Tensor
		wm := map[string]string{}
		for i := 0; i < 8; i++ {
			tt := fixture.Tensor{Name: stem + ".shard_" + string(rune('0'+i)) + ".weight", DType: "BF16", Shape: []int64{8192, 64}}
			file := "model-00002-of-00002.safetensors"
			if i < 4 {
				a = append(a, tt)
				file = "model-00001-of-00002.safetensors"
			}
			wm[tt.Name] = file
		}
		a = append(a, other)
		wm[other.Name] = "model-00001-of-00002.safetensors"
		idx, err := fixture.IndexJSON(1, wm)
		if err != nil {
			t.Fatal(err)
		}
		fa := must(fixture.WriteShard("", a))
		m := loadDir(cfg,
			fixture.File{Name: "model.safetensors.index.json", Data: idx},
			fixture.File{Name: "model-00001-of-00002.safetensors", Data: fa},
		)
		if !m.Partial || incomplete(m) != m.Partial {
			t.Fatalf("partial %v incomplete %v", m.Partial, incomplete(m))
		}
		res := scan.Run(m)
		if res.Overall != scan.StatusFail {
			t.Fatalf("overall %s", res.Overall)
		}
		if ov := strings.Join(modelDetails(t, m, 140, 40)["Overview"], "\n"); !strings.Contains(ov, "incomplete") {
			t.Fatalf("overview omitted incomplete:\n%s", ov)
		}
		hdr := Model{m: m, scan: res}.shellHeader(160)
		if !strings.Contains(hdr, "(incomplete)") {
			t.Fatalf("header %q", hdr)
		}
	})

	t.Run("hostile-stem", func(t *testing.T) {
		hs := "model.layers.1.\x1b[31m\u202e\u200bevil\u009b2J.ngram_embedding"
		var ht []fixture.Tensor
		for i := 0; i < 4; i++ {
			ht = append(ht, fixture.Tensor{Name: hs + ".shard_" + string(rune('0'+i)) + ".weight", DType: "BF16", Shape: []int64{8192, 64}})
		}
		m := loadDir(cfg, fixture.File{Name: "model.safetensors", Data: must(fixture.WriteShard("", append(ht, other)))})
		res := scan.Run(m)
		hdr := Model{m: m, scan: res}.shellHeader(160)
		views := append(qaViews(t, m), hdr)
		for i, view := range views {
			if bad := qa5Bad(view); len(bad) > 0 {
				t.Fatalf("view %d: raw hostile runes %U\n%s", i, bad, view)
			}
		}
	})

	_ = filepath.Separator
}

func TestQA5_PartialOmitsGPUFloor(t *testing.T) {
	SetNoColor(true)
	n := int64(model.GiB)
	m := &model.Model{
		Format:      "safetensors",
		Partial:     true,
		TensorBytes: n + 4096,
		Tensors: []*model.Tensor{{
			TensorInfo: safetensors.TensorInfo{
				Name: "model.ngram_embedding.weight", DType: "BF16", Shape: []int64{4096, 8},
				DataOffsets: [2]int64{0, n},
			},
			Role: model.RolePlain, Group: "ngram", Kind: model.KindEmbeddings,
		}},
	}
	all := ""
	for _, lines := range modelDetails(t, m, 140, 40) {
		all += strings.Join(lines, "\n") + "\n"
	}
	if strings.Contains(all, "on the GPU/accelerator") {
		t.Fatalf("partial model still claims a GPU lower bound:\n%s", all)
	}
	if !strings.Contains(all, "incomplete") {
		t.Fatalf("partial model omitted incomplete:\n%s", all)
	}
}
