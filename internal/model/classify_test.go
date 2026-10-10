package model

import (
	"math"
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name, key, kind string
		layer           int
	}{
		{"language_model.model.layers.12.mlp.switch_mlp.up_proj.weight", "layer 12", KindLayer, 12},
		{"model.layers.0.self_attn.q_proj.weight", "layer 0", KindLayer, 0},
		{"transformer.h.5.attn.c_attn.weight", "layer 5", KindLayer, 5},
		{"language_model.mtp.layers.0.self_attn.q_proj.weight", "mtp", KindMTP, -1},
		{"language_model.mtp.fc_hidden.scales", "mtp", KindMTP, -1},
		{"vision_tower.blocks.3.attn.qkv.weight", "vision", KindVision, -1},
		{"model.visual.blocks.0.norm1.weight", "vision", KindVision, -1},
		{"language_model.lm_head.weight", "lm_head", KindLMHead, -1},
		{"language_model.model.embed_tokens.biases", "embeddings", KindEmbeddings, -1},
		{"model.norm.weight", "norms", KindNorms, -1},
		// Model-level mixer, including hc_norm, stays in one group. The norm
		// substring must not peel hc_norm off into norms.
		{"language_model.model.hyper_connection_mixer.input_mix_weight_up.weight", "hyper_connection", KindHyper, -1},
		{"language_model.model.hyper_connection_mixer.input_mix_weight_down.weight", "hyper_connection", KindHyper, -1},
		{"language_model.model.hyper_connection_mixer.hc_norm.weight", "hyper_connection", KindHyper, -1},
		// Same mixer nested under mtp or a decoder layer keeps that group.
		{"language_model.mtp.hyper_connection_mixer.hc_norm.weight", "mtp", KindMTP, -1},
		{"language_model.model.layers.3.mlp_hyper_connection.hc_norm.weight", "layer 3", KindLayer, 3},
		{"language_model.model.layers.3.attn_hyper_connection.input_mix_weight_up.weight", "layer 3", KindLayer, 3},
		// N-gram table lives under layers.1 but is an embedding table, not layer 1.
		{"language_model.model.layers.1.ple.ple_embedding.ngram_embedding.shard_0.weight", "ngram", KindEmbeddings, -1},
		{"language_model.model.layers.1.ple.ple_embedding.ngram_heads_offsets", "ngram", KindEmbeddings, -1},
		{"language_model.model.layers.1.ple.ple_embedding.ngram_heads_vocab_sizes", "ngram", KindEmbeddings, -1},
		{"model.language_model.layers.1.ple.ple_embedding.ngram_embedding.shard_3.weight", "ngram", KindEmbeddings, -1},
		// PLE projections that are not the table stay with the decoder layer.
		{"language_model.model.layers.1.ple.key_proj.weight", "layer 1", KindLayer, 1},
		{"some.other.tensor.weight", "other", KindOther, -1},
		{"model.layers.99999999.self_attn.q_proj.weight", "layer 99999999", KindLayer, 99999999},
	}
	for _, c := range cases {
		key, kind, layer := Classify(c.name)
		if key != c.key || kind != c.kind || layer != c.layer {
			t.Errorf("Classify(%q) = (%q,%q,%d), want (%q,%q,%d)", c.name, key, kind, layer, c.key, c.kind, c.layer)
		}
	}
}

func TestClassifyOverflowIndex(t *testing.T) {
	name := "model.layers." + strings.Repeat("9", 40) + ".self_attn.q_proj.weight"
	key, kind, layer := Classify(name)
	if key != "layer overflow" || kind != KindLayer || layer != -1 {
		t.Fatalf("Classify overflow = (%q,%q,%d)", key, kind, layer)
	}
	// layers.4097 is a real index. It must not share the overflow sentinel.
	key, kind, layer = Classify("model.layers.4097.self_attn.q_proj.weight")
	if key != "layer 4097" || kind != KindLayer || layer != 4097 {
		t.Fatalf("Classify 4097 = (%q,%q,%d)", key, kind, layer)
	}
}

func TestNoteAbsurdLayersWarns(t *testing.T) {
	m := &Model{
		Tensors: []*Tensor{
			{Group: "layer 99999999", Kind: KindLayer, Layer: 99999999},
			{Group: "layer overflow", Kind: KindLayer, Layer: -1},
			{Group: "layer 4097", Kind: KindLayer, Layer: 4097},
		},
	}
	m.NoteAbsurdLayers()
	if !m.Partial {
		t.Fatal("absurd indices did not mark partial")
	}
	got := strings.Join(m.Warnings, "\n")
	for _, want := range []string{
		"layer index 99999999 exceeds 4096; dropped",
		"layer index exceeds 4096; dropped",
		"layer index 4097 exceeds 4096; dropped",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "4097; dropped") && strings.Count(got, "layer index exceeds 4096; dropped") != 1 {
		t.Fatalf("overflow warning collided with a numbered layer: %q", got)
	}
	n := len(m.Warnings)
	m.NoteAbsurdLayers()
	if len(m.Warnings) != n {
		t.Fatalf("second pass appended warnings: %v", m.Warnings)
	}
}

func TestIsExpertTensor(t *testing.T) {
	yes := []string{
		"model.layers.3.mlp.experts.17.down_proj.weight",
		"language_model.model.layers.3.mlp.switch_mlp.gate_proj.weight",
		"blk.3.ffn_up_exps.weight",
		"model.layers.1.block_sparse_moe.experts.0.w1.weight",
	}
	no := []string{
		"language_model.model.layers.3.mlp.shared_expert.up_proj.weight",
		"language_model.model.layers.3.mlp.gate.weight",
	}
	for _, n := range yes {
		if !IsExpertTensor(n) {
			t.Errorf("IsExpertTensor(%q) = false", n)
		}
	}
	for _, n := range no {
		if IsExpertTensor(n) {
			t.Errorf("IsExpertTensor(%q) = true", n)
		}
	}
}

func TestShortName(t *testing.T) {
	if got := ShortName("language_model.model.layers.7.self_attn.k_proj.weight"); got != "self_attn.k_proj.weight" {
		t.Errorf("ShortName = %q", got)
	}
}

func TestInferQuant(t *testing.T) {
	// MLX 4-bit, group 32: [248320, 2560] packed into U32 [248320, 320], scales [248320, 80].
	w := &Tensor{}
	w.DType, w.Shape = "U32", []int64{248320, 320}
	s := &Tensor{}
	s.DType, s.Shape = "BF16", []int64{248320, 80}
	bits, logical := inferQuant(w, s, 32)
	if bits != 4 || logical != 248320*2560 {
		t.Errorf("inferQuant = (%d, %d)", bits, logical)
	}
}

func TestInferQuantOverflow(t *testing.T) {
	w := &Tensor{}
	w.DType, w.Shape = "U32", []int64{math.MaxInt64}
	s := &Tensor{}
	s.DType, s.Shape = "BF16", []int64{math.MaxInt64}
	if bits, logical := inferQuant(w, s, 32); bits != 0 || logical != 0 {
		t.Fatalf("inferQuant = (%d, %d)", bits, logical)
	}
}
