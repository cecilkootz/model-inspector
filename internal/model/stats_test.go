package model

import (
	"strings"
	"testing"

	"origin.cursor.com/cecilkootz/model-inspector/internal/fixture"
	"origin.cursor.com/cecilkootz/model-inspector/internal/safetensors"
)

func loadFixture(t *testing.T, files []fixture.File, err error) *Model {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestStorageBreakdownTinyQwen(t *testing.T) {
	files, _, err := fixture.TinyQwen()
	m := loadFixture(t, files, err)
	classes := m.StorageBreakdown()
	got := map[string]int64{}
	for _, c := range classes {
		got[c.Class] = c.Bytes
		if c.Pct < 0 || c.Pct > 100 {
			t.Errorf("%s pct %v", c.Class, c.Pct)
		}
	}
	if got["q4_packed"] <= 0 {
		t.Fatalf("classes = %+v", classes)
	}
	if got["q_scales_biases"] <= 0 {
		t.Fatalf("missing scales/biases: %+v", classes)
	}
	if got["plain_bf16"] <= 0 {
		t.Fatalf("missing plain bf16: %+v", classes)
	}
}

func TestHostCapableNgramAndTied(t *testing.T) {
	files, err := fixture.NgramTable()
	ng := loadFixture(t, files, err)
	host := ng.HostCapable()
	if len(host) != 1 || !strings.Contains(host[0].Name, "ngram_embed") {
		t.Fatalf("host = %+v", host)
	}
	if host[0].Rule != RuleLookupTable || host[0].Group != "layer 1" {
		t.Fatalf("host = %+v", host[0])
	}
	var sawSize, sawHost bool
	for _, c := range ng.Outliers() {
		switch c.Kind {
		case "size_outlier":
			sawSize = true
			if c.Group != "layer 1" || !strings.Contains(c.Message, "n-gram") {
				t.Errorf("size callout = %+v", c)
			}
		case "host_capable":
			sawHost = true
			if !strings.Contains(c.Message, "heuristic") {
				t.Errorf("host callout = %+v", c)
			}
		}
	}
	if !sawSize || !sawHost {
		t.Fatalf("callouts = %+v", ng.Outliers())
	}
	mem := ng.MemoryEstimate()
	if mem.HostCapableBytes < GiB {
		if mem.HostCapableBytes != host[0].Bytes {
			t.Fatalf("memory host %d table %d", mem.HostCapableBytes, host[0].Bytes)
		}
	}
	types := ng.LayerTypes()
	if len(types) != 3 || types[0] != "linear_attention" || types[2] != "full_attention" {
		t.Fatalf("layer types = %v", types)
	}

	tiedFiles, err := fixture.TiedEmbed()
	tied := loadFixture(t, tiedFiles, err)
	if got := tied.HostCapable(); len(got) != 0 {
		t.Fatalf("tied embedding was flagged: %+v", got)
	}
	untiedFiles, err := fixture.UntiedEmbed()
	untied := loadFixture(t, untiedFiles, err)
	if got := untied.HostCapable(); len(got) != 1 || !strings.Contains(got[0].Name, "embed_tokens") {
		t.Fatalf("untied = %+v", got)
	}
}

func TestHostCapableRules(t *testing.T) {
	tensor := func(name, kind, role string, shape []int64, bytes int64) *Tensor {
		return &Tensor{
			TensorInfo: safetensors.TensorInfo{
				Name: name, DType: "BF16", Shape: shape,
				DataOffsets: [2]int64{0, bytes},
			},
			Role: role, Kind: kind, Group: "g", LogicalParams: 1024 * 4,
		}
	}
	m := &Model{TensorBytes: 2*GiB + 4096, Format: "safetensors (mlx)", Tensors: []*Tensor{
		tensor("model.layers.1.ngram_embed.weight", KindLayer, RolePlain, []int64{1024, 4}, 2*GiB),
		tensor("model.layers.1.ngram_embed.scales", KindLayer, RoleQuantScales, []int64{1024, 1}, 2048),
		tensor("vision.embed_tokens.weight", KindVision, RolePlain, []int64{2048, 4}, 100),
		tensor("model.patch_embed.weight", KindOther, RolePlain, []int64{2048, 4}, 100),
		tensor("model.embed_tokens.weight", KindEmbeddings, RolePlain, []int64{1023, 4}, 100),
		tensor("model.layers.1.ngram_embedding.shard_0.weight", KindEmbeddings, RolePlain, []int64{2048, 4}, 100),
	}}
	// The scales sibling is found by name. Give the primary a .weight suffix
	// and point scales at the same base. Bytes on the primary are 2 GiB.
	got := m.HostCapable()
	if len(got) != 1 || got[0].Name != "model.layers.1.ngram_embed.weight" {
		t.Fatalf("got %+v", got)
	}
	if got[0].Bytes != 2*GiB+2048 {
		t.Fatalf("bytes = %d, want weight+scales", got[0].Bytes)
	}
	mem := m.MemoryEstimate()
	if mem.HostCapableBytes < GiB || !mem.UnifiedMemory {
		t.Fatalf("memory = %+v", mem)
	}
	if mem.AcceleratorMinBytes != mem.WeightsBytes-mem.HostCapableBytes {
		t.Fatalf("accelerator = %d", mem.AcceleratorMinBytes)
	}
}

func TestLayerTypesIntervalFallback(t *testing.T) {
	m := &Model{Config: map[string]any{
		"num_hidden_layers":       float64(4),
		"full_attention_interval": float64(4),
	}}
	got := m.LayerTypes()
	want := []string{"linear_attention", "linear_attention", "linear_attention", "full_attention"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v", got)
	}
}

func TestStorageLabel(t *testing.T) {
	g := &Group{Bytes: 100, Tensors: []*Tensor{
		{Role: RoleQuantWeight, Bits: 4, TensorInfo: safetensors.TensorInfo{DataOffsets: [2]int64{0, 90}}},
		{Role: RolePlain, TensorInfo: safetensors.TensorInfo{DType: "BF16", DataOffsets: [2]int64{0, 10}}},
	}}
	if g.StorageLabel() != "q4+bf16" {
		t.Fatalf("label %s", g.StorageLabel())
	}
	plain := &Group{Bytes: 4, Tensors: []*Tensor{
		{Role: RolePlain, TensorInfo: safetensors.TensorInfo{DType: "F32", DataOffsets: [2]int64{0, 4}}},
	}}
	if plain.StorageLabel() != "fp32" {
		t.Fatalf("plain label %s", plain.StorageLabel())
	}
}

func TestAutoMapAndCodeFiles(t *testing.T) {
	files, err := fixture.CodeExec()
	m := loadFixture(t, files, err)
	codeFiles := m.CodeExecFiles()
	names := map[string]bool{}
	for _, f := range codeFiles {
		names[f.Name] = true
	}
	if !names["modeling_demo.py"] || !names["weights.bin"] {
		t.Fatalf("files = %+v", codeFiles)
	}
	entries := m.AutoMapEntries()
	if len(entries) != 1 || entries[0].File != "config.json" || entries[0].Target != "modeling_demo.DemoForCausalLM" {
		t.Fatalf("auto_map = %+v", entries)
	}
}

func TestMergeStorageCapsAtFour(t *testing.T) {
	in := []StorageClass{
		{Class: "a", Bytes: 50},
		{Class: "b", Bytes: 40},
		{Class: "c", Bytes: 5},
		{Class: "d", Bytes: 3},
		{Class: "e", Bytes: 2},
	}
	out := MergeStorage(nil, in, 4)
	if len(out) != 4 {
		t.Fatalf("got %d: %+v", len(out), out)
	}
	var other int64
	for _, c := range out {
		if c.Class == "other" {
			other = c.Bytes
		}
	}
	if other != 5 {
		t.Fatalf("other = %d, classes %+v", other, out)
	}
}
