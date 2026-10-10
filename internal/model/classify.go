package model

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Group kinds.
const (
	KindEmbeddings = "embeddings"
	KindLayer      = "layer"
	KindNorms      = "norms"
	KindHyper      = "hyper"
	KindLMHead     = "lm_head"
	KindMTP        = "mtp"
	KindVision     = "vision"
	KindOther      = "other"
)

var kindOrder = map[string]int{
	KindEmbeddings: 0, KindLayer: 1, KindNorms: 2, KindHyper: 3,
	KindLMHead: 4, KindMTP: 5, KindVision: 6, KindOther: 7,
}

// MaxLayerIndex is the highest decoder index that receives its own column.
// A larger index is rejected and the inspection is marked partial, so a
// name cannot size an allocation.
const MaxLayerIndex = 4096

// layerRe matches decoder-layer style names: model.layers.N., transformer.h.N., blocks.N.
var layerRe = regexp.MustCompile(`(?:^|\.)(?:layers|h|blocks|block)\.(\d+)\.`)

// Classify maps a tensor name to a group key, kind, and decoder layer index
// (-1 when the tensor is not part of a numbered decoder layer).
// MTP, vision, and the n-gram embedding table are grouped as a whole even
// though their names contain numbered layers/blocks, so they never merge
// into decoder layer N.
func Classify(name string) (key, kind string, layer int) {
	lower := strings.ToLower(name)
	segs := strings.Split(lower, ".")
	if hasSeg(segs, func(s string) bool {
		return s == "mtp" || strings.HasPrefix(s, "mtp_") || strings.HasPrefix(s, "nextn")
	}) {
		return "mtp", KindMTP, -1
	}
	if hasSeg(segs, func(s string) bool {
		return strings.Contains(s, "vision") || s == "visual" || s == "multi_modal_projector" || s == "mm_projector"
	}) {
		return "vision", KindVision, -1
	}
	// The PLE n-gram table is stored under decoder layer 1
	// (...layers.N.ple.ple_embedding.ngram_embedding.shard_N.weight, plus
	// ngram_heads_offsets / ngram_heads_vocab_sizes). The layer regex would
	// swallow that whole table into layer N before the "embed" rule runs.
	if hasSeg(segs, func(s string) bool { return s == "ple_embedding" || s == "ngram_embedding" }) {
		return "ngram", KindEmbeddings, -1
	}
	if m := layerRe.FindStringSubmatch(name); m != nil {
		n, err := strconv.Atoi(m[1])
		// A value that does not fit in int must not collapse to layer 0.
		if err != nil || n < 0 {
			return "layer overflow", KindLayer, MaxLayerIndex + 1
		}
		return fmt.Sprintf("layer %d", n), KindLayer, n
	}
	// The model-level mixer sits next to the stack, not inside layers.N or
	// mtp. One parameter is hc_norm; the "norm" substring rule below would
	// file that tensor under norms and leave input_mix_weight_* in other.
	// Per-layer attn_/mlp_hyper_connection tensors already returned above.
	if hasSeg(segs, func(s string) bool { return s == "hyper_connection_mixer" }) {
		return "hyper_connection", KindHyper, -1
	}
	switch {
	case strings.Contains(lower, "lm_head") || strings.HasPrefix(lower, "output."):
		return "lm_head", KindLMHead, -1
	case strings.Contains(lower, "embed") || hasSeg(segs, func(s string) bool { return s == "wte" || s == "wpe" }):
		return "embeddings", KindEmbeddings, -1
	case strings.Contains(lower, "norm"):
		return "norms", KindNorms, -1
	}
	return "other", KindOther, -1
}

// NoteAbsurdLayers marks the inspection partial when a tensor or group
// carries a decoder index above MaxLayerIndex. The heatmap then omits it.
func (m *Model) NoteAbsurdLayers() {
	if m == nil {
		return
	}
	for _, t := range m.Tensors {
		if t != nil && t.Layer > MaxLayerIndex {
			m.Partial = true
			return
		}
	}
	for _, g := range m.Groups {
		if g != nil && g.Layer > MaxLayerIndex {
			m.Partial = true
			return
		}
	}
}

func hasSeg(segs []string, f func(string) bool) bool {
	for _, s := range segs {
		if f(s) {
			return true
		}
	}
	return false
}

// IsExpertTensor reports whether a tensor holds routed MoE expert weights
// (HF ".experts.N.", MLX stacked ".switch_mlp.", GGUF-style "_exps").
// Shared experts are not counted.
func IsExpertTensor(name string) bool {
	return strings.Contains(name, ".experts.") || strings.Contains(name, ".switch_mlp.") ||
		strings.Contains(name, "_exps.") || strings.Contains(name, ".block_sparse_moe.experts")
}

// ShortName strips the "...layers.N." prefix for display inside a layer group.
func ShortName(name string) string {
	if loc := layerRe.FindStringIndex(name); loc != nil {
		return name[loc[1]:]
	}
	return name
}
