package model

import (
	"strings"
	"testing"

	"github.com/cecilkootz/model-inspector/internal/jsonx"
	"github.com/cecilkootz/model-inspector/internal/safetensors"
)

func FuzzConfigShapes(f *testing.F) {
	for _, s := range []string{
		`{"hidden_size":8,"vocab_size":32,"num_hidden_layers":2}`,
		`{"text_config":{"hidden_size":1e400,"vocab_size":-1},"num_hidden_layers":"2"}`,
		`{"llm_config":{"hidden_size":8.5},"language_config":{"vocab_size":[1]}}`,
		`{"num_hidden_layers":9223372036854775807,"num_nextn_predict_layers":9223372036854775807}`,
		`{"hidden_size":1e-400,"word_embed_proj_dim":{"a":1},"embedding_size":null}`,
		`{"text_config":[],"hidden_size":true}`,
	} {
		f.Add(s, int64(32), int64(8), "BF16", int64(3))
		f.Add(s, int64(-1), int64(1<<62), "U32", int64(0))
	}
	f.Fuzz(func(t *testing.T, cfg string, rows, width int64, dtype string, layers int64) {
		v, err := jsonx.Unmarshal([]byte(cfg))
		if err != nil {
			return
		}
		obj, ok := v.(map[string]any)
		if !ok {
			return
		}
		m := &Model{Config: obj}
		for _, name := range []string{"model.embed_tokens.weight", "lm_head.weight"} {
			_, kind, layer := Classify(name)
			ti := safetensors.TensorInfo{Name: name, DType: dtype, Shape: []int64{rows, width}}
			m.Tensors = append(m.Tensors, &Tensor{TensorInfo: ti, Kind: kind, Layer: layer, Role: RolePlain})
		}
		m.Tensors = append(m.Tensors, &Tensor{
			TensorInfo: safetensors.TensorInfo{Name: "x.embed_tokens.weight", DType: "U32", Shape: []int64{rows, 4}},
			Kind:       KindEmbeddings, Role: RoleQuantWeight, LogicalParams: width,
		})
		for i := int64(0); i < layers%8; i++ {
			m.Groups = append(m.Groups, &Group{Kind: KindLayer, Layer: int(i)})
		}
		r := m.ConfigShapes()
		for _, c := range r.Comparisons {
			if c.Want < 0 || c.Got < 0 {
				t.Fatalf("negative comparison %+v", c)
			}
		}
		for _, s := range append(r.Invalid, r.Skipped...) {
			if strings.ContainsAny(s, "\x1b\"{[") {
				t.Fatalf("file text in %q", s)
			}
		}
	})
}
