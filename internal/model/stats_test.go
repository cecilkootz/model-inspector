package model

import (
	"math"
	"strconv"
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
		// A shard suffix on a non-table name is not a lookup table.
		tensor("model.layers.1.mlp.down.shard_0.weight", KindLayer, RolePlain, []int64{2048, 4}, 100),
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

func TestCodeExecSuffixes(t *testing.T) {
	yes := []string{"a.bin", "a.pt", "a.pth", "a.ckpt", "a.tar", "a.tgz", "a.bin.gz", "a.pt.gz", "a.pkl.gz", "a.tar.gz", "m.pt.zip", "m.ckpt.zip", "m.pth.tar", "m.pth.zip", "m.bin.zip", "dir/m.PTH.TAR", "dir/m.TAR.GZ"}
	no := []string{"m.onnx", "m.msgpack", "m.safetensors", "m.zip", "m.gz", "m.pt.onnx"}
	for _, name := range yes {
		if !codeExecName(name) {
			t.Errorf("codeExecName(%q) = false", name)
		}
	}
	for _, name := range no {
		if codeExecName(name) {
			t.Errorf("codeExecName(%q) = true", name)
		}
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

// vontraNgramRows is the leading dimension of each PLE n-gram shard in
// Vontra/Qwen3.8-Flash-Next-MLX-4bit-MTP. The packed U32 shape is
// [rows, 20]; 4-bit group-32 scales and biases are [rows, 5].
const vontraNgramRows int64 = 2500012

func vontraNgramTable() (params, bytes int64) {
	const shards = fixture.VontraNgramShards
	// 32/4 = 8 logical values per packed U32, so the last dim dequantizes
	// 20*8 = 160. Scales along that axis are 160/32 = 5.
	params = shards * vontraNgramRows * 160
	weight := vontraNgramRows * 20 * 4
	scale := vontraNgramRows * 5 * 2
	bytes = shards * (weight + scale + scale)
	return params, bytes
}

func TestShardedNgramAggregates(t *testing.T) {
	params, bytes := vontraNgramTable()
	if params != 51_200_245_760 || bytes != 32_000_153_600 {
		t.Fatalf("formula params=%d bytes=%d", params, bytes)
	}
	if HumanCount(params) != "51.20B" || HumanBytes(bytes) != "29.80 GiB" {
		t.Fatalf("display params %s bytes %s", HumanCount(params), HumanBytes(bytes))
	}
	perShard := bytes / fixture.VontraNgramShards
	if perShard >= GiB {
		t.Fatalf("one shard is %d bytes, already past 1 GiB", perShard)
	}

	m := syntheticVontraNgram(t)
	host := m.HostCapable()
	if len(host) != 1 {
		t.Fatalf("host tables = %d, want 1 logical table: %+v", len(host), host)
	}
	h := host[0]
	if h.Rule != RuleLookupTable || h.Group != "ngram" || !strings.Contains(h.Name, "ngram_embedding") || strings.Contains(h.Name, "shard_") {
		t.Fatalf("host = %+v", h)
	}
	if h.ParamsEst != params || h.Bytes != bytes {
		t.Fatalf("params %d bytes %d, want %d %d", h.ParamsEst, h.Bytes, params, bytes)
	}
	mem := m.MemoryEstimate()
	if mem.HostCapableBytes != bytes {
		t.Fatalf("memory host %d", mem.HostCapableBytes)
	}
	if mem.AcceleratorMinBytes != mem.WeightsBytes-bytes {
		t.Fatalf("accelerator %d weights %d", mem.AcceleratorMinBytes, mem.WeightsBytes)
	}
	var hostCalls, bigCalls int
	for _, c := range m.Outliers() {
		switch c.Kind {
		case "host_capable":
			hostCalls++
			if c.Message != "n-gram embedding table 29.80 GiB can stay in CPU RAM (heuristic)" {
				t.Errorf("host callout %q", c.Message)
			}
		case "big_tensor":
			bigCalls++
			if !strings.Contains(c.Message, "~51.20B-param n-gram embedding table") || !strings.Contains(c.Message, "29.80 GiB") {
				t.Errorf("big callout %q", c.Message)
			}
		}
	}
	if hostCalls != 1 || bigCalls != 1 {
		t.Fatalf("callouts host=%d big=%d\n%+v", hostCalls, bigCalls, m.Outliers())
	}

	// Rows under the lookup threshold stay out even when there are 128 of them.
	small := syntheticVontraNgram(t)
	for _, tensor := range small.Tensors {
		if tensor.Role == RoleQuantWeight {
			tensor.Shape = []int64{1023, 20}
		}
	}
	if got := small.HostCapable(); len(got) != 0 {
		t.Fatalf("short shards flagged: %+v", got)
	}

	files, err := fixture.ShardedNgram()
	loaded := loadFixture(t, files, err)
	if loaded.Quant == nil || loaded.Quant.Bits != 4 || loaded.Quant.GroupSize != 32 {
		t.Fatalf("quant %+v", loaded.Quant)
	}
	got := loaded.HostCapable()
	if len(got) != 1 || got[0].ParamsEst != params || got[0].Bytes != bytes {
		t.Fatalf("loaded host %+v", got)
	}
	if loaded.TotalLogicalParams != params {
		t.Fatalf("logical params %d", loaded.TotalLogicalParams)
	}
}

func syntheticVontraNgram(t *testing.T) *Model {
	t.Helper()
	const shards = fixture.VontraNgramShards
	m := &Model{
		Format:        "safetensors (mlx)",
		DTypes:        map[string]*DTypeStat{},
		BitsHistogram: map[int]int{},
		Quant:         &Quantization{Bits: 4, GroupSize: 32, Mode: "affine"},
	}
	for i := 0; i < shards; i++ {
		base := "language_model.model.layers.1.ple.ple_embedding.ngram_embedding.shard_" + strconv.Itoa(i)
		w := &Tensor{TensorInfo: safetensors.TensorInfo{
			Name: base + ".weight", DType: "U32", Shape: []int64{vontraNgramRows, 20},
		}}
		s := &Tensor{TensorInfo: safetensors.TensorInfo{
			Name: base + ".scales", DType: "BF16", Shape: []int64{vontraNgramRows, 5},
		}}
		b := &Tensor{TensorInfo: safetensors.TensorInfo{
			Name: base + ".biases", DType: "BF16", Shape: []int64{vontraNgramRows, 5},
		}}
		w.DataOffsets = [2]int64{0, vontraNgramRows * 20 * 4}
		s.DataOffsets = [2]int64{0, vontraNgramRows * 5 * 2}
		b.DataOffsets = [2]int64{0, vontraNgramRows * 5 * 2}
		bits, logical := inferQuant(w, s, 32)
		if bits != 4 || logical != vontraNgramRows*160 {
			t.Fatalf("shard %d inferQuant bits=%d logical=%d", i, bits, logical)
		}
		key, kind, layer := Classify(w.Name)
		for _, tensor := range []*Tensor{w, s, b} {
			tensor.Group, tensor.Kind, tensor.Layer = key, kind, layer
		}
		w.Role, w.Bits, w.LogicalParams = RoleQuantWeight, bits, logical
		s.Role, b.Role = RoleQuantScales, RoleQuantBiases
		m.Tensors = append(m.Tensors, w, s, b)
	}
	for _, tensor := range m.Tensors {
		m.TensorBytes += tensor.ByteLen()
		m.TotalLogicalParams += tensor.LogicalParams
	}
	return m
}

func TestStoragePctClamped(t *testing.T) {
	// The class still fits in int64 while the recorded total does not, which
	// is what a saturated TensorBytes looks like after an overflow.
	m := &Model{TensorBytes: 10, Tensors: []*Tensor{{
		Role: RolePlain,
		TensorInfo: safetensors.TensorInfo{
			Name: "model.layers.0.mlp.down_proj.weight", DType: "F32",
			Shape: []int64{1}, DataOffsets: [2]int64{0, 1000},
		},
	}}}
	for _, c := range m.StorageBreakdown() {
		if c.Bytes != 1000 || c.Pct != 100 {
			t.Fatalf("storage %+v", c)
		}
	}
	for _, c := range MergeStorage(m, m.StorageBreakdown(), 4) {
		if c.Pct < 0 || c.Pct > 100 {
			t.Fatalf("merged %+v", c)
		}
	}
	if got := pct(math.MaxInt64, math.MaxInt64-1); got < 0 || got > 100 {
		t.Fatalf("pct = %v", got)
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

func TestUnifiedMemoryFollowsHost(t *testing.T) {
	saved := AppleSilicon
	t.Cleanup(func() { AppleSilicon = saved })
	m := &Model{}
	for _, host := range []bool{false, true} {
		AppleSilicon = host
		if got := m.UnifiedMemory(); got != host {
			t.Errorf("AppleSilicon=%v: UnifiedMemory() = %v", host, got)
		}
	}
	m.Format = "safetensors (mlx)"
	AppleSilicon = false
	if !m.UnifiedMemory() {
		t.Error("an MLX checkpoint is unified memory on any host")
	}
}
