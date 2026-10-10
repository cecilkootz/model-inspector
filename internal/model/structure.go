package model

import "strings"

// Sublayer classifies a tensor name into a heatmap row and a finer part.
// sub is one of attn, router, shared, experts, norms, ple, hyper, other.
// part is the last path segment with a storage suffix removed.
// The function is pure: it does not read the checkpoint.
func Sublayer(name string) (sub, part string) {
	part = sublayerPart(name)
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "hyper_connection") || strings.Contains(lower, "hyper_conn"):
		return "hyper", part
	case strings.Contains(lower, "ple") || strings.Contains(lower, "ngram"):
		return "ple", part
	case strings.Contains(lower, "shared_expert"):
		return "shared", part
	case strings.Contains(lower, "switch_mlp") || strings.Contains(lower, ".experts.") ||
		strings.Contains(lower, "_exps") || strings.Contains(lower, "block_sparse_moe"):
		return "experts", part
	case strings.Contains(lower, "self_attn") || strings.Contains(lower, "attention") || strings.Contains(lower, ".attn."):
		return "attn", part
	case strings.Contains(lower, "router") || strings.Contains(lower, ".gate."):
		return "router", part
	case strings.Contains(lower, "norm"):
		return "norms", part
	default:
		return "other", part
	}
}

func sublayerPart(name string) string {
	part := name
	if i := strings.LastIndex(part, "."); i >= 0 {
		part = part[i+1:]
	}
	switch strings.ToLower(part) {
	case "weight", "bias", "scales", "biases":
		base := name
		if i := strings.LastIndex(base, "."); i >= 0 {
			base = base[:i]
		}
		if j := strings.LastIndex(base, "."); j >= 0 {
			return base[j+1:]
		}
		return base
	}
	return part
}
