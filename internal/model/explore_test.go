package model

import (
	"strings"
	"testing"

	"origin.cursor.com/cecilkootz/model-inspector/internal/fixture"
	"origin.cursor.com/cecilkootz/model-inspector/internal/safetensors"
)

func TestExpertCountStackedAndIndexed(t *testing.T) {
	stacked := &Group{HasExperts: true, Tensors: []*Tensor{
		{TensorInfo: safetensors.TensorInfo{Name: "model.layers.0.mlp.switch_mlp.down_proj.weight", Shape: []int64{512, 8, 4}}},
		{TensorInfo: safetensors.TensorInfo{Name: "model.layers.0.mlp.switch_mlp.up_proj.weight", Shape: []int64{512, 4, 8}}},
		{TensorInfo: safetensors.TensorInfo{Name: "model.layers.0.mlp.experts.0.down_proj.weight", Shape: []int64{4}}},
	}}
	n, ok := stacked.ExpertCount()
	if !ok || n != 512 {
		t.Fatalf("stacked count %d ok %v", n, ok)
	}
	indexed := &Group{HasExperts: true, Tensors: []*Tensor{
		{TensorInfo: safetensors.TensorInfo{Name: "model.layers.0.mlp.experts.0.down_proj.weight", Shape: []int64{2}}},
		{TensorInfo: safetensors.TensorInfo{Name: "model.layers.0.mlp.experts.3.down_proj.weight", Shape: []int64{2}}},
		{TensorInfo: safetensors.TensorInfo{Name: "model.layers.0.mlp.switch_mlp.up_proj.weight", Shape: []int64{2}}},
	}}
	n, ok = indexed.ExpertCount()
	if !ok || n != 2 {
		t.Fatalf("indexed count %d ok %v", n, ok)
	}
	if _, ok := (&Group{}).ExpertCount(); ok {
		t.Fatal("empty group reported a count")
	}
}

func TestLayerInfoDisagreementSkipsSelfAttn(t *testing.T) {
	m := &Model{
		Config: map[string]any{
			"layer_types": []any{"linear_attention", "full_attention"},
		},
		Groups: []*Group{
			{Key: "layer 0", Kind: KindLayer, Layer: 0, Tensors: []*Tensor{
				{TensorInfo: safetensors.TensorInfo{Name: "model.layers.0.self_attn.q_proj.weight"}},
			}},
			{Key: "layer 1", Kind: KindLayer, Layer: 1, Tensors: []*Tensor{
				{TensorInfo: safetensors.TensorInfo{Name: "model.layers.1.full_attn.q_proj.weight"}},
			}},
		},
	}
	info := m.LayerInfo()
	if len(info) != 2 || info[0].Disagree || info[0].Short != "lin" {
		t.Fatalf("layer 0 = %+v", info)
	}
	if info[1].Disagree || info[1].Short != "full" || !info[1].Compared {
		t.Fatalf("layer 1 = %+v", info[1])
	}
	if got := m.MetadataMismatches(); len(got) != 0 {
		t.Fatalf("self_attn disagreed: %v", got)
	}

	m.Groups[0].Tensors[0].Name = "model.layers.0.full_attn.q_proj.weight"
	info = m.LayerInfo()
	if !info[0].Disagree || info[0].Short != "?" {
		t.Fatalf("expected disagreement, got %+v", info[0])
	}
	miss := m.MetadataMismatches()
	if len(miss) != 1 || !strings.Contains(miss[0], "layer 0") {
		t.Fatalf("mismatches %v", miss)
	}
}

func TestMetadataMismatchesTinyAndSharded(t *testing.T) {
	files, _, err := fixture.TinyQwen()
	m := loadFixture(t, files, err)
	if !m.MetadataCompared() {
		t.Fatal("tiny qwen compared nothing")
	}
	if got := m.MetadataMismatches(); len(got) != 0 {
		t.Fatalf("tiny mismatches %v", got)
	}
	n, ok := expertCountOf(m, "layer 0")
	if !ok || n != 1 {
		t.Fatalf("layer 0 experts %d ok %v", n, ok)
	}

	sharded, err := fixture.ShardedNgram()
	s := loadFixture(t, sharded, err)
	if s.MetadataCompared() {
		t.Fatal("n-gram-only fixture was compared against num_hidden_layers")
	}
	if got := s.MetadataMismatches(); len(got) != 0 {
		t.Fatalf("sharded mismatches %v", got)
	}
}

func TestLogicalTensorsCollapseShards(t *testing.T) {
	files, err := fixture.ShardedNgram()
	m := loadFixture(t, files, err)
	var g *Group
	for _, group := range m.Groups {
		if group.Key == "ngram" {
			g = group
		}
	}
	if g == nil {
		t.Fatal("no ngram group")
	}
	if len(g.Tensors) != fixture.VontraNgramShards*3 {
		t.Fatalf("raw tensors %d", len(g.Tensors))
	}
	rows := g.LogicalTensors(m)
	if len(rows) != 3 {
		t.Fatalf("logical rows %d, want weight+scales+biases", len(rows))
	}
	var weight LogicalTensor
	for _, row := range rows {
		if strings.Contains(row.Name, "shard_") {
			t.Fatalf("logical name still has a shard suffix: %s", row.Name)
		}
		if row.Shards != fixture.VontraNgramShards {
			t.Fatalf("shards %d on %s", row.Shards, row.Name)
		}
		if !row.Host {
			t.Fatalf("row not host: %+v", row)
		}
		if strings.HasSuffix(row.FullName, ".weight") {
			weight = row
		}
	}
	_, bytes := vontraNgramTable()
	weightBytes := int64(fixture.VontraNgramShards) * vontraNgramRows * 20 * 4
	if weight.Bytes != weightBytes {
		t.Fatalf("weight bytes %d, want %d", weight.Bytes, weightBytes)
	}
	var sum int64
	for _, row := range rows {
		if !addChecked(&sum, row.Bytes) {
			t.Fatal("logical bytes overflow")
		}
	}
	if sum != bytes {
		t.Fatalf("logical sum %d, host table %d", sum, bytes)
	}
	if weight.Outlier {
		// The weight alone is most of the group, so the marker is expected.
	}
	if !weight.Outlier {
		t.Fatalf("weight row is %.0f%% of the group and was not marked", float64(weight.Bytes)/float64(g.Bytes)*100)
	}
}

func TestLogicalTensorsKeepDisagreeingPieces(t *testing.T) {
	g := &Group{Key: "ngram", Kind: KindEmbeddings, Tensors: []*Tensor{
		tensorNamed("model.layers.1.ngram_embed.shard_0.weight", []int64{8, 2}),
		tensorNamed("model.layers.1.ngram_embed.shard_1.weight", []int64{4, 2}),
	}}
	rows := g.LogicalTensors(nil)
	if len(rows) != 2 {
		t.Fatalf("disagreeing shards merged into %d rows", len(rows))
	}
}

func TestLayerInfoSkipsSparseHighIndex(t *testing.T) {
	m := &Model{Groups: []*Group{
		{Key: "layer 0", Kind: KindLayer, Layer: 0, Tensors: []*Tensor{
			{TensorInfo: safetensors.TensorInfo{Name: "model.layers.0.self_attn.q_proj.weight"}},
		}},
		{Key: "layer 9000000", Kind: KindLayer, Layer: 9_000_000},
		{Key: "layer 4096", Kind: KindLayer, Layer: MaxLayerIndex},
	}}
	info := m.LayerInfo()
	if len(info) > 2 {
		t.Fatalf("dense slice len %d, want a short prefix", len(info))
	}
	by := m.LayerInfoBy()
	if _, ok := by[9_000_000]; ok {
		t.Fatal("absurd layer index was stored")
	}
	if _, ok := by[MaxLayerIndex]; !ok {
		t.Fatal("in-range high layer missing")
	}
	if len(by) > 8 {
		t.Fatalf("layer info entries %d", len(by))
	}
}

func tensorNamed(name string, shape []int64) *Tensor {
	return &Tensor{TensorInfo: safetensors.TensorInfo{Name: name, DType: "BF16", Shape: shape, DataOffsets: [2]int64{0, 4}}}
}

func expertCountOf(m *Model, key string) (int, bool) {
	for _, g := range m.Groups {
		if g.Key == key {
			return g.ExpertCount()
		}
	}
	return 0, false
}
