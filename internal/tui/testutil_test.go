package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/cecilkootz/model-inspector/internal/fixture"
	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/safetensors"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

func stripView(s string) string { return ui.Strip(s) }

func dim(sz [2]int) string { return itoa(sz[0]) + "x" + itoa(sz[1]) }

func median(n int, fn func()) time.Duration {
	samples := make([]time.Duration, n)
	for i := 0; i < n; i++ {
		start := time.Now()
		fn()
		samples[i] = time.Since(start)
	}
	sort.Slice(samples, func(a, b int) bool { return samples[a] < samples[b] })
	return samples[n/2]
}

func loadLayersV2(t *testing.T) *model.Model {
	t.Helper()
	files, err := fixture.LayersV2()
	if err != nil {
		t.Fatal(err)
	}
	return loadFiles(t, files)
}

func layersScaleModel(groups, per int) *model.Model {
	m := &model.Model{Format: "safetensors", Dir: "/model", TotalSize: 1}
	for i := 0; i < groups; i++ {
		key := fmt.Sprintf("layer %d", i)
		g := &model.Group{Key: key, Kind: model.KindLayer, Layer: i}
		for j := 0; j < per; j++ {
			name := fmt.Sprintf("model.layers.%d.self_attn.q_proj.weight", i)
			if per > 1 {
				name = fmt.Sprintf("model.layers.%d.mlp.experts.%d.down_proj.weight", i, j)
			}
			tensor := &model.Tensor{
				TensorInfo: safetensors.TensorInfo{Name: name, DType: "U8", Shape: []int64{8}, DataOffsets: [2]int64{0, 8}},
				Kind:       model.KindLayer, Layer: i, Group: key, Role: model.RolePlain,
			}
			g.Tensors = append(g.Tensors, tensor)
			m.Tensors = append(m.Tensors, tensor)
		}
		m.Groups = append(m.Groups, g)
	}
	return m
}

func unreadableModel(t *testing.T) *model.Model {
	t.Helper()
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "config.json")); err != nil {
		t.Fatal(err)
	}
	writeShard(t, dir)
	m, err := model.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.Dir = "/model"
	return m
}

func oversizeModel(t *testing.T) *model.Model {
	t.Helper()
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	// 71.4 MiB logical size. The reader refuses it from the stat, so the
	// file stays sparse.
	const size = int64(74868327)
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	f.Close()
	writeShard(t, dir)
	m, err := model.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.Dir = "/model"
	return m
}
