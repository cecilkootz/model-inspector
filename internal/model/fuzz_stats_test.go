package model

import (
	"testing"

	"origin.cursor.com/cecilkootz/model-inspector/internal/safetensors"
)

func FuzzStats(f *testing.F) {
	f.Add(int64(4), int64(4), "F32", RolePlain)
	f.Add(int64(1)<<62+int64(1)<<61, int64(1)<<62+int64(1)<<61, "U8", RoleQuantWeight)
	f.Add(int64(0), int64(-1), "", "")
	f.Fuzz(func(t *testing.T, a, b int64, dtype, role string) {
		if len(dtype) > 16 {
			dtype = dtype[:16]
		}
		if len(role) > 16 {
			role = role[:16]
		}
		if a < 0 {
			a = -a
		}
		m := &Model{
			Format:      "safetensors",
			TensorBytes: a,
			Tensors: []*Tensor{
				{
					Role: role, Kind: KindLayer, Group: "model.layers.0",
					TensorInfo: tensorInfo("model.layers.0.mlp.down_proj.weight", dtype, a),
				},
				{
					Role: RolePlain, Kind: KindEmbeddings, Group: "embed",
					TensorInfo: tensorInfo("model.embed_tokens.weight", "F32", b),
				},
			},
			Groups: []*Group{{
				Key: "model.layers.0", Kind: KindLayer, Bytes: a,
			}},
		}
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("stats panicked: %v", r)
			}
		}()
		for _, c := range m.StorageBreakdown() {
			if c.Bytes < 0 || c.Pct < 0 {
				t.Fatalf("storage %+v", c)
			}
		}
		for _, c := range MergeStorage(m, m.StorageBreakdown(), 4) {
			if c.Bytes < 0 || c.Pct < 0 {
				t.Fatalf("merged %+v", c)
			}
		}
		mem := m.MemoryEstimate()
		if mem.WeightsBytes < 0 || mem.TextOnlyBytes < 0 || mem.HostCapableBytes < 0 || mem.AcceleratorMinBytes < 0 {
			t.Fatalf("memory %+v", mem)
		}
		for _, h := range m.HostCapable() {
			if h.Bytes < 0 {
				t.Fatalf("host %+v", h)
			}
		}
		_ = m.Outliers()
	})
}

func tensorInfo(name, dtype string, n int64) safetensors.TensorInfo {
	if n < 0 {
		n = 0
	}
	return safetensors.TensorInfo{Name: name, DType: dtype, Shape: []int64{1}, DataOffsets: [2]int64{0, n}}
}
