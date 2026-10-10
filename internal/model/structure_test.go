package model

import "testing"

func TestSublayer(t *testing.T) {
	cases := []struct{ name, sub, part string }{
		{"language_model.model.layers.0.self_attn.q_proj.weight", "attn", "q_proj"},
		{"model.layers.3.mlp.gate.weight", "router", "gate"},
		{"model.layers.0.mlp.shared_expert.up_proj.weight", "shared", "up_proj"},
		{"model.layers.0.mlp.experts.0.down_proj.weight", "experts", "down_proj"},
		{"model.layers.0.mlp.switch_mlp.up_proj.weight", "experts", "up_proj"},
		{"model.layers.0.input_layernorm.weight", "norms", "input_layernorm"},
		{"model.layers.0.mlp_hyper_connection.hc_norm.weight", "hyper", "hc_norm"},
		{"model.layers.1.ple.ple_embedding.ngram_embedding.shard_0.weight", "ple", "shard_0"},
		{"model.embed_tokens.weight", "other", "embed_tokens"},
		{"vision_tower.blocks.0.attn.qkv.weight", "attn", "qkv"},
	}
	for _, tc := range cases {
		sub, part := Sublayer(tc.name)
		if sub != tc.sub || part != tc.part {
			t.Errorf("%s → %s/%s, want %s/%s", tc.name, sub, part, tc.sub, tc.part)
		}
	}
}
