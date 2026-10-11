package model

import (
	"math"
	"strings"
	"testing"

	"github.com/cecilkootz/model-inspector/internal/safetensors"
)

func FuzzStats(f *testing.F) {
	f.Add(int64(4), int64(4), "F32", RolePlain, "model.layers.0.mlp.down_proj.weight")
	f.Add(int64(1)<<62+int64(1)<<61, int64(1)<<62+int64(1)<<61, "U8", RoleQuantWeight, "model.embed_tokens.weight")
	f.Add(int64(0), int64(-1), "", "", "")
	f.Add(int64(4096), int64(4096), "BF16", RolePlain, "model.layers.1.ple.ngram_embedding.shard_0.weight")
	f.Add(int64(4096), int64(4096), "BF16", RolePlain, strings.Repeat("\u023a", 12)+".ngram_embedding.shard_0.weight")
	f.Add(int64(4096), int64(4096), "BF16", RolePlain, strings.Repeat("\u0130", 8)+".ngram_embedding_proj.shard_0.weight")
	f.Add(int64(4096), int64(4096), "BF16", RolePlain, "\u212a.ngram_embedding.shard_0.weight")
	f.Fuzz(func(t *testing.T, a, b int64, dtype, role, name string) {
		if len(dtype) > 16 {
			dtype = dtype[:16]
		}
		if len(role) > 16 {
			role = role[:16]
		}
		if len(name) > 200 {
			name = name[:200]
		}
		if a < 0 {
			a = -a
		}
		if b < 0 {
			b = -b
		}
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("stats panicked: %v", r)
			}
		}()
		stem, _ := lookupStem(name)
		if !strings.Contains(name, stem) {
			t.Fatalf("stem %q is not a substring of %q", stem, name)
		}
		named := tensorInfo(name, "BF16", b)
		named.Shape = []int64{4096, 4}
		tensors := []*Tensor{
			{
				Role: role, Kind: KindLayer, Group: "model.layers.0",
				TensorInfo: tensorInfo("model.layers.0.mlp.down_proj.weight", dtype, a),
			},
			{
				Role: RolePlain, Kind: KindEmbeddings, Group: "embed",
				TensorInfo: tensorInfo("model.embed_tokens.weight", "F32", b),
			},
			{
				Role: RolePlain, Kind: KindEmbeddings, Group: "ngram",
				TensorInfo: named,
			},
		}
		if hasSuffixFold(name, ".weight") {
			base := stripWeightSuffix(name)
			scales := tensorInfo(base+".scales", "BF16", b)
			scales.Shape = []int64{4096, 4}
			tensors = append(tensors, &Tensor{
				Role: RolePlain, Kind: KindEmbeddings, Group: "ngram",
				TensorInfo: scales,
			})
		}
		m := &Model{
			Format:      "safetensors",
			TensorBytes: a,
			Tensors:     tensors,
			Groups: []*Group{{
				Key: "model.layers.0", Kind: KindLayer, Bytes: a,
			}},
		}
		for _, c := range m.StorageBreakdown() {
			if c.Bytes < 0 || c.Pct < 0 || c.Pct > 100 {
				t.Fatalf("storage %+v", c)
			}
		}
		for _, c := range MergeStorage(m, m.StorageBreakdown(), 4) {
			if c.Bytes < 0 || c.Pct < 0 || c.Pct > 100 {
				t.Fatalf("merged %+v", c)
			}
		}
		var weight int64
		overflow := false
		for _, tensor := range m.Tensors {
			n := tensor.ByteLen()
			if n < 0 || weight > math.MaxInt64-n {
				overflow = true
				break
			}
			weight += n
		}
		mem := m.MemoryEstimate()
		if mem.WeightsBytes < 0 || mem.TextOnlyBytes < 0 || mem.HostCapableBytes < 0 || mem.AcceleratorMinBytes < 0 {
			t.Fatalf("memory %+v", mem)
		}
		// Compare with the tensor byte sum, not the clamped accelerator line.
		if !overflow && mem.HostCapableBytes > weight {
			t.Fatalf("host-capable %d > weight bytes %d", mem.HostCapableBytes, weight)
		}
		for _, h := range m.HostCapable() {
			if h.Bytes < 0 {
				t.Fatalf("host %+v", h)
			}
			if !strings.Contains(h.Name, lookupMustSub(h.Name)) {
				t.Fatalf("host name %q", h.Name)
			}
		}
		_ = m.Outliers()
	})
}

func lookupMustSub(name string) string {
	stem, _ := lookupStem(name)
	if stem == "" {
		return name
	}
	return stem
}

func tensorInfo(name, dtype string, n int64) safetensors.TensorInfo {
	if n < 0 {
		n = 0
	}
	return safetensors.TensorInfo{Name: name, DType: dtype, Shape: []int64{1}, DataOffsets: [2]int64{0, n}}
}
