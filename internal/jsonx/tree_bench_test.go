package jsonx

import (
	"strings"
	"testing"
)

func BenchmarkParseTreeTypical(b *testing.B) {
	raw := typicalConfig()
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ParseTree(raw, Limits{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseTreeLarge(b *testing.B) {
	raw := largeConfig(b)
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree, err := ParseTree(raw, Limits{})
		if err != nil {
			b.Fatal(err)
		}
		if len(tree.Nodes) < 200_000 {
			b.Fatalf("nodes %d", len(tree.Nodes))
		}
	}
}

func typicalConfig() []byte {
	return []byte(`{
  "architectures": ["Qwen4ExpForConditionalGeneration"],
  "model_type": "qwen4_exp",
  "text_config": {
    "hidden_size": 2560,
    "num_hidden_layers": 48,
    "num_attention_heads": 24,
    "num_key_value_heads": 2,
    "head_dim": 256,
    "vocab_size": 248320,
    "max_position_embeddings": 262144,
    "num_experts": 512,
    "num_experts_per_tok": 10,
    "moe_intermediate_size": 640,
    "shared_expert_intermediate_size": 640,
    "full_attention_interval": 4,
    "tie_word_embeddings": false,
    "dtype": "bfloat16"
  },
  "vision_config": {"depth": 27, "hidden_size": 1152, "num_heads": 16, "patch_size": 16},
  "quantization": {"bits": 4, "group_size": 32, "mode": "affine"},
  "dtype": "bfloat16"
}`)
}

func largeConfig(b *testing.B) []byte {
	b.Helper()
	// One object plus 200_000 string values. Keys and values are padded so
	// the document is about 10 MiB. The padding length is a constant.
	const n = 200_000
	const pad = 40
	var buf strings.Builder
	buf.Grow(n * (pad + 16))
	buf.WriteByte('{')
	for i := 0; i < n; i++ {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.WriteByte('"')
		buf.WriteString("k")
		writeInt(&buf, i)
		buf.WriteString(`":"`)
		buf.WriteString(strings.Repeat("x", pad))
		buf.WriteByte('"')
	}
	buf.WriteByte('}')
	return []byte(buf.String())
}

func writeInt(b *strings.Builder, n int) {
	if n == 0 {
		b.WriteByte('0')
		return
	}
	var tmp [16]byte
	i := len(tmp)
	for n > 0 {
		i--
		tmp[i] = byte('0' + n%10)
		n /= 10
	}
	b.Write(tmp[i:])
}
