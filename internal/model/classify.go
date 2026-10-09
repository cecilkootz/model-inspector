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
	KindLMHead     = "lm_head"
	KindMTP        = "mtp"
	KindVision     = "vision"
	KindOther      = "other"
)

var kindOrder = map[string]int{
	KindEmbeddings: 0, KindLayer: 1, KindNorms: 2, KindLMHead: 3,
	KindMTP: 4, KindVision: 5, KindOther: 6,
}

// layerRe matches decoder-layer style names: model.layers.N., transformer.h.N., blocks.N.
var layerRe = regexp.MustCompile(`(?:^|\.)(?:layers|h|blocks|block)\.(\d+)\.`)

// Classify maps a tensor name to a group key, kind, and decoder layer index
// (-1 when the tensor is not part of a numbered decoder layer).
// MTP and vision towers are grouped as a whole even though they contain
// their own numbered layers/blocks, so they never merge into decoder layer N.
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
	if m := layerRe.FindStringSubmatch(name); m != nil {
		n, _ := strconv.Atoi(m[1])
		return fmt.Sprintf("layer %d", n), KindLayer, n
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
