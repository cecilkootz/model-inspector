package model

import "testing"

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
		{"language_model.model.hyper_connection_mixer.input_mix_weight_up.weight", "other", KindOther, -1},
	}
	for _, c := range cases {
		key, kind, layer := Classify(c.name)
		if key != c.key || kind != c.kind || layer != c.layer {
			t.Errorf("Classify(%q) = (%q,%q,%d), want (%q,%q,%d)", c.name, key, kind, layer, c.key, c.kind, c.layer)
		}
	}
}

func TestIsExpertTensor(t *testing.T) {
	yes := []string{
		"model.layers.3.mlp.experts.17.down_proj.weight",
		"language_model.model.layers.3.mlp.switch_mlp.gate_proj.weight",
		"blk.3.ffn_up_exps.weight",
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
