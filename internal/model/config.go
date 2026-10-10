package model

import (
	"encoding/json"
	"fmt"
	"sort"

	"origin.cursor.com/cecilkootz/model-inspector/internal/jsonx"
)

// KV is one extracted config field.
type KV struct {
	Key   string `json:"key"`
	Path  string `json:"path"`
	Value any    `json:"value"`
}

var keyFields = []string{
	"model_type", "architectures", "num_hidden_layers", "hidden_size", "intermediate_size",
	"num_attention_heads", "num_key_value_heads", "head_dim", "vocab_size",
	"max_position_embeddings", "tie_word_embeddings", "torch_dtype", "dtype",
	"num_experts", "num_local_experts", "n_routed_experts", "num_experts_per_tok",
	"moe_intermediate_size", "shared_expert_intermediate_size", "n_shared_experts",
	"full_attention_interval", "mtp_num_hidden_layers", "num_nextn_predict_layers",
}

// fieldGroup is the v1 JSON group for each keyFields entry. The Config
// screen layout that uses these groups is a later change; the names are
// stable for schema_version 1.
var fieldGroup = map[string]string{
	"model_type": "architecture", "architectures": "architecture",
	"num_hidden_layers": "architecture", "hidden_size": "architecture",
	"intermediate_size": "architecture", "vocab_size": "architecture",
	"max_position_embeddings": "architecture", "tie_word_embeddings": "architecture",
	"torch_dtype": "architecture", "dtype": "architecture",
	"num_attention_heads": "attention", "num_key_value_heads": "attention",
	"head_dim": "attention", "full_attention_interval": "attention",
	"num_experts": "moe", "num_local_experts": "moe", "n_routed_experts": "moe",
	"num_experts_per_tok": "moe", "moe_intermediate_size": "moe",
	"shared_expert_intermediate_size": "moe", "n_shared_experts": "moe",
	"mtp_num_hidden_layers": "mtp_vision", "num_nextn_predict_layers": "mtp_vision",
	"vision.depth": "mtp_vision", "vision.hidden_size": "mtp_vision",
	"vision.num_heads": "mtp_vision", "vision.patch_size": "mtp_vision",
}

// FieldGroup returns the schema group for a key from KeyConfig.
// Unknown keys return "other".
func FieldGroup(key string) string {
	if g, ok := fieldGroup[key]; ok {
		return g
	}
	return "other"
}

// KeyConfig extracts architecture fields, looking at the root first and then
// nested text/LLM configs used by multimodal models.
func (m *Model) KeyConfig() []KV {
	if m.Config == nil {
		return nil
	}
	scopes := []struct {
		prefix string
		m      map[string]any
	}{
		{"", m.Config},
		{"text_config.", subMap(m.Config, "text_config")},
		{"llm_config.", subMap(m.Config, "llm_config")},
		{"language_config.", subMap(m.Config, "language_config")},
	}
	var out []KV
	for _, k := range keyFields {
		for _, s := range scopes {
			if s.m == nil {
				continue
			}
			if v, ok := s.m[k]; ok && v != nil {
				out = append(out, KV{Key: k, Path: s.prefix + k, Value: v})
				break
			}
		}
	}
	if vc := subMap(m.Config, "vision_config"); vc != nil {
		for _, k := range []string{"depth", "hidden_size", "num_heads", "patch_size"} {
			if v, ok := vc[k]; ok {
				out = append(out, KV{Key: "vision." + k, Path: "vision_config." + k, Value: v})
			}
		}
	}
	return out
}

// LayerTypeCounts summarizes text_config.layer_types (hybrid attention models).
func (m *Model) LayerTypeCounts() map[string]int {
	for _, scope := range []map[string]any{m.Config, subMap(m.Config, "text_config")} {
		if scope == nil {
			continue
		}
		if lt, ok := scope["layer_types"].([]any); ok {
			out := map[string]int{}
			for _, v := range lt {
				out[strOf(v)]++
			}
			return out
		}
	}
	return nil
}

func subMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	v, _ := m[key].(map[string]any)
	return v
}

func floatOf(v any) float64 {
	f, ok := jsonx.FitFloat(v)
	if !ok {
		return 0
	}
	return f
}

// noteOutOfRangeNumbers records each JSON number that does not fit in
// float64. The literal is kept on the value; this is the mark numeric
// callers use instead of failing the document.
func (m *Model) noteOutOfRangeNumbers(v any, path string) {
	switch t := v.(type) {
	case map[string]any:
		for _, k := range SortedKeys(t) {
			next := k
			if path != "" {
				next = path + "." + k
			}
			m.noteOutOfRangeNumbers(t[k], next)
		}
	case []any:
		for i, e := range t {
			m.noteOutOfRangeNumbers(e, fmt.Sprintf("%s[%d]", path, i))
		}
	case json.Number:
		if _, ok := jsonx.FitFloat(t); !ok {
			m.warnf("%s is out of range: %s", path, t.String())
		}
	}
}

func intOf(v any) int { return int(floatOf(v)) }

func strOf(v any) string {
	s, _ := v.(string)
	return s
}

// SortedKeys returns map keys in order.
func SortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
