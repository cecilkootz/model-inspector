package fixture

import "fmt"

// LayersV2 is a synthetic checkpoint for the v2 Layers tree.
// It is header-only. One shard holds a stacked-expert layer, a
// per-expert layer, a sharded PLE n-gram table, MLX q4 weights, a q8
// gate override, an unexpected q8 projection, and the hyper-connection
// mixer. Shapes are tiny except the n-gram rows, which are large enough
// for the host-capable heuristic.
func LayersV2() ([]File, error) {
	q4 := func(name string, rows int64) []Tensor {
		return []Tensor{
			{Name: name + ".weight", DType: "U32", Shape: []int64{rows, 4}},
			{Name: name + ".scales", DType: "BF16", Shape: []int64{rows, 1}},
		}
	}
	q8 := func(name string, rows int64) []Tensor {
		return []Tensor{
			{Name: name + ".weight", DType: "U32", Shape: []int64{rows, 8}},
			{Name: name + ".scales", DType: "BF16", Shape: []int64{rows, 1}},
		}
	}
	q2 := func(name string, rows int64) []Tensor {
		return []Tensor{
			{Name: name + ".weight", DType: "U32", Shape: []int64{rows, 2}},
			{Name: name + ".scales", DType: "BF16", Shape: []int64{rows, 1}},
		}
	}
	var tensors []Tensor
	tensors = append(tensors, q4("language_model.model.embed_tokens", 32)...)
	for i := 0; i < 3; i++ {
		base := fmt.Sprintf("language_model.model.layers.1.ple.ple_embedding.ngram_embedding.shard_%d", i)
		tensors = append(tensors,
			Tensor{Name: base + ".weight", DType: "U32", Shape: []int64{1024, 20}},
			Tensor{Name: base + ".scales", DType: "BF16", Shape: []int64{1024, 5}},
			Tensor{Name: base + ".biases", DType: "BF16", Shape: []int64{1024, 5}},
		)
	}
	tensors = append(tensors, q4("language_model.model.layers.0.self_attn.q_proj", 8)...)
	tensors = append(tensors, q4("language_model.model.layers.0.self_attn.o_proj", 8)...)
	tensors = append(tensors, q8("language_model.model.layers.0.mlp.gate", 8)...)
	tensors = append(tensors, q8("language_model.model.layers.0.mlp.up_proj", 8)...)
	tensors = append(tensors, q2("language_model.model.layers.0.mlp.down_proj", 8)...)
	tensors = append(tensors,
		Tensor{Name: "language_model.model.layers.0.mlp.switch_mlp.down_proj.weight", DType: "U32", Shape: []int64{4, 8, 4}},
		Tensor{Name: "language_model.model.layers.0.mlp.switch_mlp.down_proj.scales", DType: "BF16", Shape: []int64{4, 8, 1}},
		Tensor{Name: "language_model.model.layers.0.mlp.experts.0.down_proj.weight", DType: "U32", Shape: []int64{8, 4}},
		Tensor{Name: "language_model.model.layers.0.mlp.experts.0.down_proj.scales", DType: "BF16", Shape: []int64{8, 1}},
		Tensor{Name: "language_model.model.layers.0.mlp_hyper_connection.hc_norm.weight", DType: "BF16", Shape: []int64{8}},
	)
	tensors = append(tensors, q4("language_model.model.layers.1.full_attn.q_proj", 8)...)
	tensors = append(tensors, q4("language_model.model.layers.1.mlp.experts.0.down_proj", 8)...)
	tensors = append(tensors, q4("language_model.model.layers.1.mlp.experts.1.down_proj", 8)...)
	tensors = append(tensors, Tensor{Name: "language_model.model.norm.weight", DType: "BF16", Shape: []int64{8}})
	tensors = append(tensors,
		Tensor{Name: "language_model.model.hyper_connection_mixer.input_mix_weight_up.weight", DType: "BF16", Shape: []int64{2, 2}},
		Tensor{Name: "language_model.model.hyper_connection_mixer.input_mix_weight_down.weight", DType: "BF16", Shape: []int64{2, 2}},
		Tensor{Name: "language_model.model.hyper_connection_mixer.hc_norm.weight", DType: "BF16", Shape: []int64{8}},
	)
	tensors = append(tensors, Tensor{Name: "language_model.lm_head.weight", DType: "BF16", Shape: []int64{8, 8}})

	shard, _, err := WriteHeaderOnly("mlx", tensors)
	if err != nil {
		return nil, err
	}
	cfg := []byte(`{
  "model_type": "qwen3_next",
  "architectures": ["Qwen3NextForCausalLM"],
  "text_config": {
    "num_hidden_layers": 2,
    "hidden_size": 8,
    "vocab_size": 32,
    "num_experts": 4,
    "num_experts_per_tok": 2,
    "layer_types": ["linear_attention", "full_attention"],
    "tie_word_embeddings": false
  },
  "quantization": {
    "bits": 4,
    "group_size": 32,
    "mode": "affine",
    "language_model.model.layers.0.mlp.gate": {"bits": 8, "group_size": 32}
  }
}
`)
	return []File{
		{Name: "config.json", Data: cfg},
		{Name: "model.safetensors", Data: shard},
	}, nil
}
