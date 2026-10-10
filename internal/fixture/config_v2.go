package fixture

import (
	"fmt"
	"strings"
)

// ConfigQwenLike is a synthetic config.json. The real Vontra checkpoint is
// not in this tree; use this fixture until that file is supplied. The
// architecture numbers follow the v2 screen spec so the derived rows have a
// known shape. rope_theta, partial_rotary_factor, the linear head counts,
// sliding_window, the long string, and synthetic_items exist so paging,
// yarn, and the value modal have something to draw. Quantization overrides
// are named synthetic.override.N.
func ConfigQwenLike() []byte {
	var b strings.Builder
	b.WriteString(`{"model_type":"qwen4_exp","architectures":["Qwen4ExpForConditionalGeneration"],"text_config":{`)
	b.WriteString(`"hidden_size":2560,"num_attention_heads":24,"num_key_value_heads":2,"head_dim":256,`)
	b.WriteString(`"vocab_size":248320,"max_position_embeddings":262144,`)
	b.WriteString(`"moe_intermediate_size":640,"shared_expert_intermediate_size":640,`)
	b.WriteString(`"num_experts":512,"num_experts_per_tok":10,"full_attention_interval":4,`)
	b.WriteString(`"mtp_num_hidden_layers":1,"tie_word_embeddings":false,"dtype":"bfloat16",`)
	b.WriteString(`"num_hidden_layers":48,"rope_theta":10000000,"partial_rotary_factor":0.25,`)
	b.WriteString(`"linear_num_key_heads":16,"linear_num_value_heads":32,"sliding_window":4096,`)
	b.WriteString(`"rope_scaling":{"factor":4,"type":"yarn"},"layer_types":[`)
	for i := 0; i < 48; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		if i%4 == 0 {
			b.WriteString(`"full_attention"`)
		} else {
			b.WriteString(`"linear_attention"`)
		}
	}
	b.WriteString(`]},"vision_config":{"depth":27,"hidden_size":1152,"num_heads":16,"patch_size":16},`)
	b.WriteString(`"quantization":{"bits":4,"group_size":32,"mode":"affine"`)
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&b, `,"synthetic.override.%d":{"bits":4,"group_size":32}`, i)
	}
	b.WriteString(`},"synthetic_long":"`)
	b.WriteString(strings.Repeat("Z", 70_000))
	b.WriteString(`","synthetic_items":[`)
	for i := 0; i < 5000; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('0')
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

// ConfigHostile puts C0, bidi, and zero-width characters in keys and values,
// plus a dotted key and a quote, so the tree has to sanitize before drawing.
func ConfigHostile() []byte {
	var b strings.Builder
	// Escapes, not raw C0, so the file is legal JSON. The parser decodes
	// them and the screen has to draw tokens instead of the characters.
	b.WriteString(`{"model_type":"qwen4_exp",`)
	b.WriteString(`"esc\u001bkey":"has\u001besc",`)
	b.WriteString(`"bidi":"a\u202eb",`)
	b.WriteString(`"zw":"a\u200bb",`)
	b.WriteString(`"dotted.key":1,`)
	b.WriteString(`"quote\"k":"x<y"}`)
	return []byte(b.String())
}

// ConfigMalformed is a trailing comma.
func ConfigMalformed() []byte { return []byte("{\n  \"a\": 1,\n}\n") }

// ConfigDupKey repeats num_experts.
func ConfigDupKey() []byte { return []byte("{\n  \"num_experts\": 1,\n  \"num_experts\": 2\n}\n") }

// ConfigDeep nests 33 arrays, one past the decoder limit.
func ConfigDeep() []byte {
	return []byte(strings.Repeat("[", 33) + "1" + strings.Repeat("]", 33))
}

// ConfigNotObject is a JSON array at the root.
func ConfigNotObject() []byte { return []byte("[1, 2, 3]\n") }

// ConfigEmpty is an object with no fields.
func ConfigEmpty() []byte { return []byte("{}\n") }

// ConfigHuge is a root object whose array has enough zeros to pass the
// 250,000 node limit while staying under 1 MiB, so the screen parses it
// synchronously. Generated, not stored.
func ConfigHuge() []byte {
	const n = 260_000
	var b strings.Builder
	b.Grow(5 + n*2 + 2)
	b.WriteString(`{"a":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('0')
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}
