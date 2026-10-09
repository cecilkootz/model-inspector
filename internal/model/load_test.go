package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mwelch/model-inspector/internal/fixture"
	"github.com/mwelch/model-inspector/internal/hfcache"
)

func loadTiny(t *testing.T) *Model {
	t.Helper()
	files, _, err := fixture.TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestLoadTinyQwen(t *testing.T) {
	m := loadTiny(t)
	assertTinyQwen(t, m)
	if len(m.Warnings) != 0 {
		t.Errorf("warnings: %v", m.Warnings)
	}
}

func TestLoadTinyQwenHFCache(t *testing.T) {
	files, _, err := fixture.TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	const (
		repo   = "Qwen/Tiny-Next"
		commit = "0123456789abcdef0123456789abcdef01234567"
	)
	hub := t.TempDir()
	snap, err := fixture.MaterializeHub(hub, repo, commit, files)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.HubRepoDir(repo) != hfcache.RepoFolderName(repo) {
		t.Fatalf("hub folder %s != %s", fixture.HubRepoDir(repo), hfcache.RepoFolderName(repo))
	}
	resolved, err := hfcache.Resolve(repo, hub)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Dir != snap || resolved.RepoID != repo || resolved.Revision != commit || !resolved.FromCache {
		t.Fatalf("resolve repo id: %+v", resolved)
	}
	byPath, err := hfcache.Resolve(snap, hub)
	if err != nil {
		t.Fatal(err)
	}
	if byPath.RepoID != repo || byPath.Revision != commit || byPath.FromCache {
		t.Fatalf("resolve snapshot path: %+v", byPath)
	}

	m, err := Load(resolved.Dir)
	if err != nil {
		t.Fatal(err)
	}
	m.RepoID, m.Revision = resolved.RepoID, resolved.Revision
	assertTinyQwen(t, m)

	byName := map[string]File{}
	var total int64
	for _, f := range m.Files {
		byName[f.Name] = f
		total += f.Size
		link, err := os.Readlink(filepath.Join(snap, f.Name))
		if err != nil {
			t.Fatalf("readlink %s: %v", f.Name, err)
		}
		if !strings.Contains(filepath.ToSlash(link), "blobs/") {
			t.Errorf("%s -> %s, want a blobs/ symlink", f.Name, link)
		}
		if !f.Symlink || f.Broken {
			t.Errorf("%s symlink=%v broken=%v", f.Name, f.Symlink, f.Broken)
		}
	}
	if total != m.TotalSize {
		t.Errorf("sum of file sizes %d != TotalSize %d", total, m.TotalSize)
	}
	for _, want := range files {
		got, ok := byName[want.Name]
		if !ok {
			t.Errorf("missing file %s", want.Name)
			continue
		}
		if got.Size != int64(len(want.Data)) {
			t.Errorf("%s size %d, blob is %d", want.Name, got.Size, len(want.Data))
		}
	}
	if len(m.Files) != len(files) {
		t.Errorf("files = %d, want %d", len(m.Files), len(files))
	}
}

func TestLoadRejectsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for a non-directory")
	}
}

func assertTinyQwen(t *testing.T, m *Model) {
	t.Helper()
	want := map[string]string{
		"language_model.model.embed_tokens.weight":                                       "embeddings",
		"language_model.model.embed_tokens.scales":                                       "embeddings",
		"language_model.model.embed_tokens.biases":                                       "embeddings",
		"language_model.model.layers.0.self_attn.q_proj.weight":                          "layer 0",
		"language_model.model.layers.0.mlp.switch_mlp.up_proj.weight":                    "layer 0",
		"language_model.model.layers.0.mlp.experts.0.down_proj.weight":                   "layer 0",
		"language_model.model.layers.0.mlp.shared_expert.up_proj.weight":                 "layer 0",
		"language_model.model.layers.0.mlp.gate.weight":                                  "layer 0",
		"language_model.model.layers.0.mlp_hyper_connection.hc_norm.weight":              "layer 0",
		"language_model.model.layers.1.self_attn.q_proj.weight":                          "layer 1",
		"language_model.model.layers.1.ple.ple_embedding.ngram_embedding.shard_0.weight": "ngram",
		"language_model.model.layers.1.ple.ple_embedding.ngram_heads_offsets":            "ngram",
		"language_model.model.layers.1.ple.ple_embedding.ngram_heads_vocab_sizes":        "ngram",
		"language_model.model.layers.1.ple.key_proj.weight":                              "layer 1",
		"language_model.model.norm.weight":                                               "norms",
		"language_model.model.hyper_connection_mixer.input_mix_weight_up.weight":         "hyper_connection",
		"language_model.model.hyper_connection_mixer.input_mix_weight_down.weight":       "hyper_connection",
		"language_model.model.hyper_connection_mixer.hc_norm.weight":                     "hyper_connection",
		"language_model.lm_head.weight":                                                  "lm_head",
		"language_model.mtp.layers.0.self_attn.q_proj.weight":                            "mtp",
		"language_model.mtp.fc_hidden.scales":                                            "mtp",
		"language_model.mtp.hyper_connection_mixer.hc_norm.weight":                       "mtp",
		"language_model.mtp.pre_fc_norm_embedding.weight":                                "mtp",
		"vision_tower.blocks.0.attn.qkv.weight":                                          "vision",
		"model.visual.blocks.0.norm1.weight":                                             "vision",
	}
	if len(m.Tensors) != len(want) {
		t.Fatalf("got %d tensors, want %d", len(m.Tensors), len(want))
	}
	for _, tensor := range m.Tensors {
		group, ok := want[tensor.Name]
		if !ok {
			t.Errorf("unexpected tensor %s in %s", tensor.Name, tensor.Group)
			continue
		}
		if tensor.Group != group {
			t.Errorf("%s group %q, want %q", tensor.Name, tensor.Group, group)
		}
	}

	groups := map[string]*Group{}
	for _, g := range m.Groups {
		groups[g.Key] = g
		if g.Kind == KindOther {
			t.Errorf("unexpected other group %q", g.Key)
		}
	}
	layer0 := groups["layer 0"]
	if layer0 == nil || !layer0.HasExperts || layer0.Kind != KindLayer {
		t.Fatalf("layer 0 = %+v", layer0)
	}
	var sawExpert, sawShared bool
	for _, tensor := range layer0.Tensors {
		switch {
		case strings.Contains(tensor.Name, ".experts.") || strings.Contains(tensor.Name, ".switch_mlp."):
			sawExpert = true
			if !IsExpertTensor(tensor.Name) {
				t.Errorf("IsExpertTensor(%s) = false", tensor.Name)
			}
		case strings.Contains(tensor.Name, "shared_expert") || strings.HasSuffix(tensor.Name, "mlp.gate.weight"):
			sawShared = true
			if IsExpertTensor(tensor.Name) {
				t.Errorf("IsExpertTensor(%s) = true", tensor.Name)
			}
		}
	}
	if !sawExpert || !sawShared {
		t.Errorf("layer 0 expert=%v shared=%v", sawExpert, sawShared)
	}
	layer1 := groups["layer 1"]
	if layer1 == nil || layer1.HasExperts {
		t.Fatalf("layer 1 = %+v", layer1)
	}
	for _, tensor := range layer1.Tensors {
		if strings.Contains(tensor.Name, "ngram_") || strings.Contains(tensor.Name, "ple_embedding") {
			t.Errorf("n-gram tensor %s grouped into %s", tensor.Name, layer1.Key)
		}
	}
	ngram := groups["ngram"]
	if ngram == nil || ngram.Kind != KindEmbeddings || len(ngram.Tensors) != 3 {
		t.Fatalf("ngram group = %+v", ngram)
	}
	hyper := groups["hyper_connection"]
	if hyper == nil || hyper.Kind != KindHyper || len(hyper.Tensors) != 3 {
		t.Fatalf("hyper group = %+v", hyper)
	}
	norms := groups["norms"]
	if norms == nil || len(norms.Tensors) != 1 || norms.Tensors[0].Name != "language_model.model.norm.weight" {
		t.Fatalf("norms = %+v", norms)
	}
	for _, key := range []string{"embeddings", "lm_head", "mtp", "vision"} {
		if groups[key] == nil {
			t.Errorf("missing group %s", key)
		}
	}
	if groups["mtp"].Kind != KindMTP || groups["vision"].Kind != KindVision {
		t.Fatalf("mtp/vision kinds: %+v %+v", groups["mtp"], groups["vision"])
	}
	layers, experts := m.LayerCount()
	if layers != 2 || experts != 1 {
		t.Errorf("LayerCount = (%d, %d), want (2, 1)", layers, experts)
	}

	var weight *Tensor
	for _, tensor := range groups["embeddings"].Tensors {
		switch tensor.Name {
		case "language_model.model.embed_tokens.weight":
			weight = tensor
			if tensor.Role != RoleQuantWeight || tensor.Bits != 4 || tensor.LogicalParams != 32 {
				t.Errorf("embed weight role=%s bits=%d logical=%d", tensor.Role, tensor.Bits, tensor.LogicalParams)
			}
		case "language_model.model.embed_tokens.scales":
			if tensor.Role != RoleQuantScales || tensor.LogicalParams != 0 {
				t.Errorf("scales role=%s logical=%d", tensor.Role, tensor.LogicalParams)
			}
		case "language_model.model.embed_tokens.biases":
			if tensor.Role != RoleQuantBiases || tensor.LogicalParams != 0 {
				t.Errorf("biases role=%s logical=%d", tensor.Role, tensor.LogicalParams)
			}
		}
	}
	if weight == nil {
		t.Fatal("missing embed weight")
	}
	if groups["embeddings"].QuantizedTensors != 1 {
		t.Errorf("quantized tensors = %d", groups["embeddings"].QuantizedTensors)
	}

	if m.Format != "safetensors (mlx)" {
		t.Errorf("format = %q", m.Format)
	}
	if m.Quant == nil || m.Quant.Bits != 4 || m.Quant.GroupSize != 32 || m.Quant.Mode != "mlx" || m.Quant.Source != "text_config.quantization_config" {
		t.Fatalf("quant = %+v", m.Quant)
	}
	if m.BitsHistogram[4] != 1 {
		t.Errorf("bits histogram = %v", m.BitsHistogram)
	}
	if m.Index == nil || m.Index.TotalSize != m.TensorBytes || m.Index.File != "model.safetensors.index.json" {
		t.Fatalf("index = %+v tensorBytes=%d", m.Index, m.TensorBytes)
	}
	if got := kv(m, "model_type"); got != "qwen3_5" {
		t.Errorf("model_type = %#v", got)
	}
	if got := kv(m, "text_config.num_experts"); got != float64(4) {
		t.Errorf("num_experts = %#v", got)
	}
	if got := kv(m, "text_config.mtp_num_hidden_layers"); got != float64(1) {
		t.Errorf("mtp layers = %#v", got)
	}
	if got := kv(m, "vision_config.depth"); got != float64(1) {
		t.Errorf("vision depth = %#v", got)
	}
	lt := m.LayerTypeCounts()
	if lt["linear_attention"] != 1 || lt["full_attention"] != 1 {
		t.Errorf("layer types = %v", lt)
	}
	if len(m.Shards) != 2 {
		t.Errorf("shards = %d", len(m.Shards))
	}
}

func kv(m *Model, path string) any {
	for _, field := range m.KeyConfig() {
		if field.Path == path {
			return field.Value
		}
	}
	return nil
}
