// Package fixture builds tiny, deterministic safetensors checkpoints for tests.
// Files are kilobytes: real header framing, Qwen-style names, no weight payload
// beyond the zero bytes the declared shapes require.
package fixture

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cecilkootz/model-inspector/internal/safetensors"
)

// File is one snapshot-relative file.
type File struct {
	Name string
	Data []byte
}

// Tensor is one safetensors entry. Data is zeros of dtype×shape bytes.
type Tensor struct {
	Name  string
	DType string
	Shape []int64
}

// HubRepoDir maps "org/name" to the cache folder "models--org--name",
// matching hfcache.RepoFolderName.
func HubRepoDir(id string) string { return "models--" + strings.ReplaceAll(id, "/", "--") }

// EncodeHeader builds a safetensors file: 8-byte little-endian header length,
// the header bytes, then data.
func EncodeHeader(header, data []byte) []byte {
	buf := make([]byte, 8+len(header)+len(data))
	binary.LittleEndian.PutUint64(buf[:8], uint64(len(header)))
	copy(buf[8:], header)
	copy(buf[8+len(header):], data)
	return buf
}

// WriteShard encodes tensors into a spec-shaped safetensors file.
// The JSON header is padded with spaces to a multiple of 8 bytes.
// Offsets are contiguous in the order tensors are given. Returns the file
// bytes and the data-section size (the sum of tensor byte ranges).
func WriteShard(format string, tensors []Tensor) ([]byte, int64, error) {
	return writeShard(format, tensors, true)
}

// WriteHeaderOnly encodes the same header and offsets as WriteShard but
// omits the tensor payload. The returned size is still the sum of the
// declared ranges, so a loader sees the real shapes without the bytes.
func WriteHeaderOnly(format string, tensors []Tensor) ([]byte, int64, error) {
	return writeShard(format, tensors, false)
}

func writeShard(format string, tensors []Tensor, withData bool) ([]byte, int64, error) {
	entries := map[string]any{}
	if format != "" {
		entries["__metadata__"] = map[string]string{"format": format}
	}
	var offset int64
	for _, t := range tensors {
		if _, dup := entries[t.Name]; dup {
			return nil, 0, fmt.Errorf("duplicate tensor %s", t.Name)
		}
		n, err := elements(t.Shape)
		if err != nil {
			return nil, 0, fmt.Errorf("%s: %w", t.Name, err)
		}
		sz, ok := safetensors.DTypeSize(t.DType)
		if !ok {
			return nil, 0, fmt.Errorf("%s: unknown dtype %s", t.Name, t.DType)
		}
		nbytes := n * sz
		entries[t.Name] = map[string]any{
			"dtype":        t.DType,
			"shape":        t.Shape,
			"data_offsets": []int64{offset, offset + nbytes},
		}
		offset += nbytes
	}
	js, err := json.Marshal(entries)
	if err != nil {
		return nil, 0, err
	}
	if pad := len(js) % 8; pad != 0 {
		js = append(js, bytesRepeat(' ', 8-pad)...)
	}
	var data []byte
	if withData {
		data = make([]byte, offset)
	}
	return EncodeHeader(js, data), offset, nil
}

func elements(shape []int64) (int64, error) {
	if len(shape) == 0 {
		return 0, fmt.Errorf("empty shape")
	}
	n := int64(1)
	for _, d := range shape {
		if d <= 0 {
			return 0, fmt.Errorf("non-positive dimension %d", d)
		}
		n *= d
	}
	return n, nil
}

func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

// IndexJSON is a model.safetensors.index.json document.
func IndexJSON(totalSize int64, weightMap map[string]string) ([]byte, error) {
	doc := struct {
		Metadata  map[string]int64  `json:"metadata"`
		WeightMap map[string]string `json:"weight_map"`
	}{
		Metadata:  map[string]int64{"total_size": totalSize},
		WeightMap: weightMap,
	}
	return json.MarshalIndent(doc, "", "  ")
}

// TinyQwenConfig is a small config.json with the architecture keys the loader reads.
// Quantization sits on text_config.quantization_config, the HF-style location.
// vocab_size equals the lm_head rows so metadata-sanity passes.
func TinyQwenConfig() []byte {
	return []byte(`{
  "model_type": "qwen3_5",
  "architectures": ["Qwen3_5ForConditionalGeneration"],
  "torch_dtype": "bfloat16",
  "text_config": {
    "num_hidden_layers": 2,
    "hidden_size": 8,
    "intermediate_size": 16,
    "num_attention_heads": 2,
    "num_key_value_heads": 1,
    "head_dim": 4,
    "vocab_size": 2,
    "max_position_embeddings": 128,
    "tie_word_embeddings": false,
    "num_experts": 4,
    "num_experts_per_tok": 2,
    "moe_intermediate_size": 8,
    "shared_expert_intermediate_size": 8,
    "mtp_num_hidden_layers": 1,
    "layer_types": ["linear_attention", "full_attention"],
    "quantization_config": {"bits": 4, "group_size": 32, "quant_method": "mlx"}
  },
  "vision_config": {"depth": 1, "hidden_size": 8, "num_heads": 2, "patch_size": 14}
}
`)
}

// TinyQwenMLXQuantConfig is TinyQwenConfig with 4-bit quantization recorded
// the way mlx-lm writes it: a top-level "quantization" object
// ({"bits","group_size","mode"}), and no text_config.quantization_config.
// group_size 32 matches the tiny U32 embed pack in TinyQwen.
func TinyQwenMLXQuantConfig() ([]byte, error) {
	var cfg map[string]any
	if err := json.Unmarshal(TinyQwenConfig(), &cfg); err != nil {
		return nil, err
	}
	text, _ := cfg["text_config"].(map[string]any)
	if text == nil {
		return nil, fmt.Errorf("tiny config has no text_config")
	}
	delete(text, "quantization_config")
	cfg["quantization"] = map[string]any{
		"bits":       4,
		"group_size": 32,
		"mode":       "affine",
	}
	return json.MarshalIndent(cfg, "", "  ")
}

const (
	shard1Name = "model-00001-of-00002.safetensors"
	shard2Name = "model-00002-of-00002.safetensors"
)

// TinyQwen returns a two-shard checkpoint: config, index, and safetensors
// whose names mirror the Qwen3.8-Flash-Next / MLX layout the classifier handles.
// Shapes are tiny on purpose so the whole tree stays a few kilobytes.
// tensorBytes is the sum of header data ranges (index metadata.total_size).
func TinyQwen() (files []File, tensorBytes int64, err error) {
	s1, n1, err := WriteShard("mlx", []Tensor{
		// 4-bit MLX pack: U32 [1, 4] with BF16 scales [1, 1] and group 32 → 4 bits.
		{"language_model.model.embed_tokens.weight", "U32", []int64{1, 4}},
		{"language_model.model.embed_tokens.scales", "BF16", []int64{1, 1}},
		{"language_model.model.embed_tokens.biases", "BF16", []int64{1, 1}},
		{"language_model.model.layers.0.self_attn.q_proj.weight", "BF16", []int64{2}},
		{"language_model.model.layers.0.mlp.switch_mlp.up_proj.weight", "BF16", []int64{2}},
		{"language_model.model.layers.0.mlp.experts.0.down_proj.weight", "BF16", []int64{2}},
		{"language_model.model.layers.0.mlp.shared_expert.up_proj.weight", "BF16", []int64{2}},
		{"language_model.model.layers.0.mlp.gate.weight", "BF16", []int64{2}},
		{"language_model.model.layers.0.mlp_hyper_connection.hc_norm.weight", "BF16", []int64{2}},
		{"language_model.model.layers.1.self_attn.q_proj.weight", "BF16", []int64{2}},
		{"language_model.model.layers.1.ple.ple_embedding.ngram_embedding.shard_0.weight", "BF16", []int64{2}},
		{"language_model.model.layers.1.ple.ple_embedding.ngram_heads_offsets", "I64", []int64{2}},
		{"language_model.model.layers.1.ple.ple_embedding.ngram_heads_vocab_sizes", "I64", []int64{2}},
		{"language_model.model.layers.1.ple.key_proj.weight", "BF16", []int64{2}},
		{"language_model.model.norm.weight", "BF16", []int64{4}},
		{"language_model.model.hyper_connection_mixer.input_mix_weight_up.weight", "BF16", []int64{2, 2}},
		{"language_model.model.hyper_connection_mixer.input_mix_weight_down.weight", "BF16", []int64{2, 2}},
		{"language_model.model.hyper_connection_mixer.hc_norm.weight", "BF16", []int64{4}},
	})
	if err != nil {
		return nil, 0, err
	}
	s2, n2, err := WriteShard("mlx", []Tensor{
		{"language_model.lm_head.weight", "BF16", []int64{2, 2}},
		{"language_model.mtp.layers.0.self_attn.q_proj.weight", "BF16", []int64{2}},
		{"language_model.mtp.fc_hidden.scales", "BF16", []int64{2}},
		{"language_model.mtp.hyper_connection_mixer.hc_norm.weight", "BF16", []int64{2}},
		{"language_model.mtp.pre_fc_norm_embedding.weight", "BF16", []int64{2}},
		{"vision_tower.blocks.0.attn.qkv.weight", "BF16", []int64{2}},
		{"model.visual.blocks.0.norm1.weight", "BF16", []int64{2}},
	})
	if err != nil {
		return nil, 0, err
	}
	tensorBytes = n1 + n2
	wm := map[string]string{}
	h1, err := safetensors.Read(bytes.NewReader(s1))
	if err != nil {
		return nil, 0, err
	}
	h2, err := safetensors.Read(bytes.NewReader(s2))
	if err != nil {
		return nil, 0, err
	}
	for _, t := range h1.Tensors {
		wm[t.Name] = shard1Name
	}
	for _, t := range h2.Tensors {
		wm[t.Name] = shard2Name
	}
	idx, err := IndexJSON(tensorBytes, wm)
	if err != nil {
		return nil, 0, err
	}
	files = []File{
		{Name: "config.json", Data: TinyQwenConfig()},
		{Name: "model.safetensors.index.json", Data: idx},
		{Name: shard1Name, Data: s1},
		{Name: shard2Name, Data: s2},
	}
	return files, tensorBytes, nil
}

// Materialize writes files into dir as regular files.
func Materialize(dir string, files []File) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, f := range files {
		path := filepath.Join(dir, f.Name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, f.Data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// MaterializeHub writes an HF hub repo: blobs/<sha256>, snapshots/<commit>
// symlinks into those blobs, and refs/main containing the commit hash.
func MaterializeHub(hubDir, repoID, commit string, files []File) (snapshot string, err error) {
	repo := filepath.Join(hubDir, HubRepoDir(repoID))
	blobDir := filepath.Join(repo, "blobs")
	snap := filepath.Join(repo, "snapshots", commit)
	if err := os.MkdirAll(blobDir, 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(snap, 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(repo, "refs"), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(repo, "refs", "main"), []byte(commit+"\n"), 0o644); err != nil {
		return "", err
	}
	for _, f := range files {
		if strings.Contains(f.Name, "/") {
			return "", fmt.Errorf("nested snapshot name %q", f.Name)
		}
		sum := sha256.Sum256(f.Data)
		blobName := hex.EncodeToString(sum[:])
		if err := os.WriteFile(filepath.Join(blobDir, blobName), f.Data, 0o644); err != nil {
			return "", err
		}
		link := filepath.Join(snap, f.Name)
		if err := os.Symlink(filepath.Join("..", "..", "blobs", blobName), link); err != nil {
			return "", err
		}
	}
	return snap, nil
}

// NgramTable is a three-layer checkpoint whose layer 1 holds a 2-D
// ngram_embed table large enough to be a size outlier and a
// lookup_table_v1 hit, while staying well under 1 MiB.
func NgramTable() ([]File, error) {
	shard, n, err := WriteShard("", []Tensor{
		{"model.layers.0.mlp.down_proj.weight", "BF16", []int64{8}},
		{"model.layers.1.ngram_embed.weight", "BF16", []int64{1024, 16}},
		{"model.layers.1.self_attn.q_proj.weight", "BF16", []int64{8}},
		{"model.layers.2.mlp.down_proj.weight", "BF16", []int64{8}},
		{"model.embed_tokens.weight", "BF16", []int64{8, 4}},
		{"lm_head.weight", "BF16", []int64{8, 4}},
	})
	if err != nil {
		return nil, err
	}
	wm := map[string]string{}
	for _, name := range []string{
		"model.layers.0.mlp.down_proj.weight",
		"model.layers.1.ngram_embed.weight",
		"model.layers.1.self_attn.q_proj.weight",
		"model.layers.2.mlp.down_proj.weight",
		"model.embed_tokens.weight",
		"lm_head.weight",
	} {
		wm[name] = "model.safetensors"
	}
	idx, err := IndexJSON(n, wm)
	if err != nil {
		return nil, err
	}
	cfg := []byte(`{
  "model_type": "qwen3_next",
  "num_hidden_layers": 3,
  "tie_word_embeddings": false,
  "layer_types": ["linear_attention", "linear_attention", "full_attention"]
}
`)
	return []File{
		{Name: "config.json", Data: cfg},
		{Name: "model.safetensors.index.json", Data: idx},
		{Name: "model.safetensors", Data: shard},
	}, nil
}

// VontraNgramShards is the piece count of the PLE n-gram table in
// Vontra/Qwen3.8-Flash-Next-MLX-4bit-MTP.
const VontraNgramShards = 128

// ShardedNgram is that table's on-disk layout: shard_0 through shard_127,
// each a U32 4-bit pack of shape [2500012, 20] with BF16 scales and biases
// of shape [2500012, 5] (group size 32). The payload is omitted; the header
// offsets still describe the full ranges so stats see the real sizes.
func ShardedNgram() ([]File, error) {
	tensors := make([]Tensor, 0, VontraNgramShards*3)
	for i := 0; i < VontraNgramShards; i++ {
		base := fmt.Sprintf("language_model.model.layers.1.ple.ple_embedding.ngram_embedding.shard_%d", i)
		tensors = append(tensors,
			Tensor{Name: base + ".weight", DType: "U32", Shape: []int64{2500012, 20}},
			Tensor{Name: base + ".scales", DType: "BF16", Shape: []int64{2500012, 5}},
			Tensor{Name: base + ".biases", DType: "BF16", Shape: []int64{2500012, 5}},
		)
	}
	shard, _, err := WriteHeaderOnly("mlx", tensors)
	if err != nil {
		return nil, err
	}
	cfg := []byte(`{
  "model_type": "qwen3_next",
  "num_hidden_layers": 48,
  "tie_word_embeddings": false,
  "quantization": {"bits": 4, "group_size": 32, "mode": "affine"}
}
`)
	return []File{
		{Name: "config.json", Data: cfg},
		{Name: "model.safetensors", Data: shard},
	}, nil
}

// TiedEmbed is a negative lookup_table_v1 case: tie_word_embeddings is
// true, there is no lm_head, and the only table is a token embedding.
func TiedEmbed() ([]File, error) {
	shard, _, err := WriteShard("", []Tensor{
		{"model.embed_tokens.weight", "BF16", []int64{1024, 4}},
	})
	if err != nil {
		return nil, err
	}
	cfg := []byte(`{"model_type":"dense","tie_word_embeddings":true,"num_hidden_layers":0}`)
	return []File{
		{Name: "config.json", Data: cfg},
		{Name: "model.safetensors", Data: shard},
	}, nil
}

// UntiedEmbed is the matching positive case: the token embedding is not
// tied, and an lm_head is present.
func UntiedEmbed() ([]File, error) {
	shard, _, err := WriteShard("", []Tensor{
		{"model.embed_tokens.weight", "BF16", []int64{1024, 4}},
		{"lm_head.weight", "BF16", []int64{8, 4}},
	})
	if err != nil {
		return nil, err
	}
	cfg := []byte(`{"model_type":"dense","tie_word_embeddings":false}`)
	return []File{
		{Name: "config.json", Data: cfg},
		{Name: "model.safetensors", Data: shard},
	}, nil
}

// CodeExec is a tiny safetensors checkpoint plus the names the
// code-exec-formats check looks for: a .py, a .bin, and auto_map.
// The code file is not executed. The .bin is not a pickle.
func CodeExec() ([]File, error) {
	shard, n, err := WriteShard("", []Tensor{
		{"model.layers.0.self_attn.q_proj.weight", "BF16", []int64{2}},
	})
	if err != nil {
		return nil, err
	}
	idx, err := IndexJSON(n, map[string]string{
		"model.layers.0.self_attn.q_proj.weight": "model.safetensors",
	})
	if err != nil {
		return nil, err
	}
	cfg := []byte(`{
  "model_type": "demo",
  "auto_map": {"AutoModelForCausalLM": "modeling_demo.DemoForCausalLM"}
}
`)
	py := []byte("# fixture only\nclass DemoForCausalLM:\n    pass\n")
	bin := []byte("not-a-pickle\n")
	return []File{
		{Name: "config.json", Data: cfg},
		{Name: "model.safetensors.index.json", Data: idx},
		{Name: "model.safetensors", Data: shard},
		{Name: "modeling_demo.py", Data: py},
		{Name: "weights.bin", Data: bin},
	}, nil
}

// QwenShape is a 48-layer header-only checkpoint. Attention bytes spread
// (linear 1000, full 1180), norms alternate 10 and 11, and the router,
// shared, and routed rows are constant. The UI derives every figure from
// these shapes.
func QwenShape() ([]File, error) {
	var tensors []Tensor
	for i := 0; i < 48; i++ {
		attn := int64(1000)
		if (i+1)%4 == 0 {
			attn = 1180
		}
		norm := int64(10)
		if i%2 == 1 {
			norm = 11
		}
		p := fmt.Sprintf("model.layers.%d.", i)
		tensors = append(tensors,
			Tensor{Name: p + "self_attn.q_proj.weight", DType: "U8", Shape: []int64{attn}},
			Tensor{Name: p + "mlp.gate.weight", DType: "U8", Shape: []int64{64}},
			Tensor{Name: p + "mlp.shared_expert.up_proj.weight", DType: "U8", Shape: []int64{64}},
			Tensor{Name: p + "mlp.experts.0.down_proj.weight", DType: "U8", Shape: []int64{64}},
			Tensor{Name: p + "input_layernorm.weight", DType: "U8", Shape: []int64{norm}},
		)
	}
	tensors = append(tensors,
		Tensor{Name: "model.mtp.layers.0.self_attn.q_proj.weight", DType: "U8", Shape: []int64{1000}},
		Tensor{Name: "model.mtp.layers.0.mlp.gate.weight", DType: "U8", Shape: []int64{64}},
		Tensor{Name: "model.mtp.layers.0.mlp.shared_expert.up_proj.weight", DType: "U8", Shape: []int64{64}},
		Tensor{Name: "model.mtp.layers.0.mlp.experts.0.down_proj.weight", DType: "U8", Shape: []int64{64}},
		Tensor{Name: "model.mtp.layers.0.input_layernorm.weight", DType: "U8", Shape: []int64{10}},
	)
	shard, _, err := WriteHeaderOnly("", tensors)
	if err != nil {
		return nil, err
	}
	cfg := []byte(`{
  "model_type": "qwen3_next",
  "text_config": {
    "num_hidden_layers": 48,
    "hidden_size": 8,
    "full_attention_interval": 4,
    "num_experts": 8,
    "num_experts_per_tok": 2
  }
}
`)
	return []File{
		{Name: "config.json", Data: cfg},
		{Name: "model.safetensors", Data: shard},
	}, nil
}

// TinyQwenMLXQuant is TinyQwen with quantization recorded as a top-level
// "quantization" object (mlx-lm style) instead of text_config.quantization_config.
func TinyQwenMLXQuant() ([]File, int64, error) {
	files, n, err := TinyQwen()
	if err != nil {
		return nil, 0, err
	}
	cfg, err := TinyQwenMLXQuantConfig()
	if err != nil {
		return nil, 0, err
	}
	for i := range files {
		if files[i].Name == "config.json" {
			files[i].Data = cfg
		}
	}
	return files, n, nil
}

// DenseShapes is a two-layer dense checkpoint whose tensors describe
// hidden_size 8, vocab_size 32, and an untied lm_head. cfg is written
// as config.json unchanged, so tests can pair it with any config.
func DenseShapes(cfg []byte) ([]File, error) {
	shard, _, err := WriteShard("", []Tensor{
		{"model.embed_tokens.weight", "BF16", []int64{32, 8}},
		{"model.layers.0.self_attn.q_proj.weight", "BF16", []int64{8, 8}},
		{"model.layers.1.self_attn.q_proj.weight", "BF16", []int64{8, 8}},
		{"model.norm.weight", "BF16", []int64{8}},
		{"lm_head.weight", "BF16", []int64{32, 8}},
	})
	if err != nil {
		return nil, err
	}
	return []File{
		{Name: "config.json", Data: cfg},
		{Name: "model.safetensors", Data: shard},
	}, nil
}

// MetadataMismatch is DenseShapes with a config that disagrees on every
// field metadata-sanity compares.
func MetadataMismatch() ([]File, error) {
	return DenseShapes([]byte(`{
  "model_type": "llama",
  "hidden_size": 16,
  "vocab_size": 64,
  "num_hidden_layers": 3,
  "tie_word_embeddings": false
}
`))
}
