package model

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cecilkootz/model-inspector/internal/fixture"
)

func writeFile(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestConfigDevZeroDoesNotHang(t *testing.T) {
	if _, err := os.Stat("/dev/zero"); err != nil {
		t.Skip("/dev/zero not available")
	}
	d := t.TempDir()
	if err := os.Symlink("/dev/zero", filepath.Join(d, "config.json")); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var m *Model
	var err error
	go func() {
		defer close(done)
		m, err = Load(d)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Load hung on config.json -> /dev/zero")
	}
	if err != nil {
		t.Fatal(err)
	}
	if m.Config != nil {
		t.Fatalf("config accepted from a device: %#v", m.Config)
	}
	if len(m.Warnings) == 0 {
		t.Fatal("expected a warning")
	}
}

func TestShardFIFODoesNotHang(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fifo")
	}
	d := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(d, "model.safetensors"), 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	done := make(chan struct{})
	var err error
	go func() {
		defer close(done)
		_, err = Load(d)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Load hung on a FIFO shard")
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestSymlinkedRoot(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	shard, _, err := fixture.WriteShard("", []fixture.Tensor{{Name: "model.embed.weight", DType: "F32", Shape: []int64{1}}})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(real, "model.safetensors"), shard)
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	m, err := Load(link)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Shards) != 1 || m.Shards[0].Header == nil {
		t.Fatalf("shards = %+v", m.Shards)
	}
	resolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	if m.Dir != resolved {
		t.Fatalf("dir %q resolved %q", m.Dir, resolved)
	}
}

func TestEmptyDirectory(t *testing.T) {
	m, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if m.Format != "unknown" || len(m.Files) != 0 || len(m.Shards) != 0 {
		t.Fatalf("format %q files %d shards %d", m.Format, len(m.Files), len(m.Shards))
	}
}

func TestUnparseableConfig(t *testing.T) {
	d := t.TempDir()
	writeFile(t, filepath.Join(d, "config.json"), []byte(`{`))
	m, err := Load(d)
	if err != nil {
		t.Fatal(err)
	}
	if m.Config != nil {
		t.Fatalf("config %#v", m.Config)
	}
	if len(m.Warnings) == 0 || !strings.Contains(strings.Join(m.Warnings, "\n"), "config.json") {
		t.Fatalf("warnings %v", m.Warnings)
	}
}

func TestGGUFOnly(t *testing.T) {
	d := t.TempDir()
	writeFile(t, filepath.Join(d, "model.gguf"), []byte("GGUF\x00not-a-real-header"))
	m, err := Load(d)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.Format, "gguf") || !strings.Contains(m.Format, "not supported") {
		t.Fatalf("format %q", m.Format)
	}
	if len(m.Shards) != 0 {
		t.Fatalf("shards %+v", m.Shards)
	}
}

func TestTiedEmbeddingsWithoutLMHead(t *testing.T) {
	shard, _, err := fixture.WriteShard("", []fixture.Tensor{
		{Name: "model.embed_tokens.weight", DType: "F32", Shape: []int64{4, 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	d := t.TempDir()
	writeFile(t, filepath.Join(d, "model.safetensors"), shard)
	writeFile(t, filepath.Join(d, "config.json"), []byte(`{"model_type":"gpt2","tie_word_embeddings":true}`))
	m, err := Load(d)
	if err != nil {
		t.Fatal(err)
	}
	var tied any
	for _, kv := range m.KeyConfig() {
		if kv.Key == "tie_word_embeddings" {
			tied = kv.Value
		}
	}
	if tied != true {
		t.Fatalf("tie_word_embeddings = %#v", tied)
	}
	for _, g := range m.Groups {
		if g.Kind == KindLMHead {
			t.Fatalf("invented lm_head group %+v", g)
		}
	}
}

func TestStackedExpertTensor(t *testing.T) {
	shard, _, err := fixture.WriteShard("", []fixture.Tensor{
		{Name: "model.layers.0.mlp.experts.weight", DType: "BF16", Shape: []int64{4, 2, 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	d := t.TempDir()
	writeFile(t, filepath.Join(d, "model.safetensors"), shard)
	m, err := Load(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Tensors) != 1 || len(m.Tensors[0].Shape) != 3 {
		t.Fatalf("tensors %+v", m.Tensors)
	}
	if !IsExpertTensor(m.Tensors[0].Name) {
		t.Fatal("stacked experts tensor was not classified as experts")
	}
	var layer *Group
	for _, g := range m.Groups {
		if g.Key == "layer 0" {
			layer = g
		}
	}
	if layer == nil || !layer.HasExperts || layer.Kind != KindLayer {
		t.Fatalf("layer 0 = %+v", layer)
	}
}

func TestSymlinkEscapeIsNotRead(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "outside.safetensors")
	shard, _, err := fixture.WriteShard("", []fixture.Tensor{{Name: "secret.weight", DType: "F32", Shape: []int64{1}}})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, outside, shard)
	d := filepath.Join(root, "model")
	if err := os.Mkdir(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(d, "model.safetensors")); err != nil {
		t.Fatal(err)
	}
	m, err := Load(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, tensor := range m.Tensors {
		if strings.Contains(tensor.Name, "secret") {
			t.Fatalf("read escaped shard: %+v", tensor)
		}
	}
	if len(m.Shards) != 1 || m.Shards[0].Header != nil {
		t.Fatalf("escaped shard was parsed: %+v", m.Shards)
	}
	if m.Shards[0].Err == nil {
		t.Fatal("escaped symlink had no error")
	}
}
