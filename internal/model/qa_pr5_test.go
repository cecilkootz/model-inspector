package model

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"origin.cursor.com/cecilkootz/model-inspector/internal/safetensors"
)

func qt(name, dtype string, shape []int64, bytes int64, role string) *Tensor {
	key, kind, layer := Classify(name)
	t := &Tensor{TensorInfo: safetensors.TensorInfo{Name: name, DType: dtype, Shape: shape, DataOffsets: [2]int64{0, bytes}},
		Group: key, Kind: kind, Layer: layer, Role: role}
	n := int64(1)
	for _, d := range shape {
		n *= d
	}
	t.LogicalParams = n
	return t
}

func qm(ts ...*Tensor) *Model {
	m := &Model{Format: "safetensors", DTypes: map[string]*DTypeStat{}, BitsHistogram: map[int]int{}}
	m.Tensors = ts
	for _, t := range ts {
		m.acc(&m.TensorBytes, t.ByteLen())
	}
	m.buildGroups()
	return m
}

const qaStem = "model.layers.1.ple.ngram_embedding"

func TestQA5_TinyShards(t *testing.T) {
	var ts []*Tensor
	for i := 0; i < 64; i++ {
		ts = append(ts, qt(fmt.Sprintf("%s.shard_%d.weight", qaStem, i), "F32", []int64{1023, 4096}, 1023*4096*4, RolePlain))
		ts = append(ts, qt(fmt.Sprintf("%s.shard_%d_1d.weight", qaStem, i), "F32", []int64{1 << 20}, 4<<20, RolePlain))
	}
	m := qm(ts...)
	if got := m.HostCapable(); len(got) != 0 {
		t.Fatalf("tiny or 1-D shards flagged: %+v", got)
	}
	if mem := m.MemoryEstimate(); mem.HostCapableBytes != 0 {
		t.Fatalf("memory host %d", mem.HostCapableBytes)
	}
	ts = nil
	for i := 0; i < 1000; i++ {
		ts = append(ts, qt(fmt.Sprintf("%s.shard_%d.weight", qaStem, i), "F32", []int64{1024, 1}, 4096, RolePlain))
	}
	m = qm(ts...)
	host := m.HostCapable()
	if len(host) != 1 || host[0].Bytes != 1000*4096 {
		t.Fatalf("1000 consistent pieces = %+v", host)
	}
	if host[0].Bytes > m.TensorBytes {
		t.Fatalf("host %d > weights %d", host[0].Bytes, m.TensorBytes)
	}
}

func TestQA5_MixedDtypesColumns(t *testing.T) {
	m := qm(
		qt(qaStem+".shard_0.weight", "BF16", []int64{4096, 160}, 4096*160*2, RolePlain),
		qt(qaStem+".shard_1.weight", "F32", []int64{4096, 160}, 4096*160*4, RolePlain),
		qt(qaStem+".shard_2.weight", "F16", []int64{4096, 7}, 4096*7*2, RolePlain),
		qt(qaStem+".shard_3.weight", "I8", []int64{1 << 20, 3}, 3<<20, RolePlain),
	)
	if got := m.HostCapable(); len(got) != 0 {
		t.Fatalf("mixed dtype/column pieces merged: %+v", got)
	}
	var warned bool
	for _, w := range m.Warnings {
		if strings.Contains(w, "not merged") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("warnings %v", m.Warnings)
	}
}

func TestQA5_DuplicateIndices(t *testing.T) {
	one := int64(4096 * 160 * 2)
	m := qm(
		qt(qaStem+".shard_1.weight", "BF16", []int64{4096, 160}, one, RolePlain),
		qt(qaStem+".shard_01.weight", "BF16", []int64{4096, 160}, one, RolePlain),
		qt(qaStem+".shard_001.weight", "BF16", []int64{4096, 160}, one, RolePlain),
		qt(qaStem+".SHARD_1.weight", "BF16", []int64{4096, 160}, one, RolePlain),
		qt(qaStem+".shard_1", "BF16", []int64{4096, 160}, one, RolePlain),
		qt(qaStem+".shard_1.WEIGHT", "BF16", []int64{4096, 160}, one, RolePlain),
	)
	host := m.HostCapable()
	if len(host) != 1 || host[0].Bytes != one {
		t.Fatalf("duplicate spellings of index 1 = %+v, want %d bytes", host, one)
	}
	if host[0].Bytes > m.TensorBytes {
		t.Fatalf("host %d > weights %d", host[0].Bytes, m.TensorBytes)
	}
}

func TestQA5_ShardNames(t *testing.T) {
	cases := map[string]bool{
		qaStem + ".shard_99999999999999999999999999999.weight": true,
		qaStem + ".shard_-1.weight":                            false,
		qaStem + ".shard_+1.weight":                            false,
		qaStem + ".shard_007.weight":                           true,
		qaStem + ".shard_\u0661.weight":                        false,
		qaStem + ".shard_\uff11.weight":                        false,
		qaStem + ".shard_\U0001D7CF.weight":                    false,
		qaStem + ".\u0455hard_1.weight":                        false,
		qaStem + ".shard_.weight":                              false,
		qaStem + ".shard_1 .weight":                            false,
		qaStem + "shard_1.weight":                              false,
	}
	for name, want := range cases {
		_, got := lookupStem(name)
		if got != want {
			t.Errorf("lookupStem(%q) sharded=%v want %v", name, got, want)
		}
		stem, _ := lookupStem(name)
		if !strings.Contains(name, stem) {
			t.Errorf("stem %q is not a substring of %q", stem, name)
		}
	}
	a, aok := shardOrdinal(qaStem + ".shard_007.weight")
	b, bok := shardOrdinal(qaStem + ".shard_7.weight")
	if !aok || !bok || a != b {
		t.Fatalf("ordinal 007 vs 7: %q %v / %q %v", a, aok, b, bok)
	}
}

func TestQA5_LowerLengthDrift(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{strings.Repeat("\u0130", 3) + ".ngram_embedding.shard_0.weight", strings.Repeat("\u0130", 3) + ".ngram_embedding"},
		{"\u212a.ngram_embedding.shard_0.weight", "\u212a.ngram_embedding"},
		{"\u023a\u023a.ngram_embedding.shard_0.weight", "\u023a\u023a.ngram_embedding"},
	} {
		got, ok := lookupStem(tc.name)
		if !ok || got != tc.want {
			t.Fatalf("lookupStem(%q) = %q,%v want %q", tc.name, got, ok, tc.want)
		}
		if !strings.Contains(tc.name, got) {
			t.Fatalf("stem %q is not a substring of %q", got, tc.name)
		}
	}
	name := strings.Repeat("\u0130", 40) + ".ngram_embedding.shard_0.weight"
	m := qm(qt(name, "BF16", []int64{4096, 160}, 4096*160*2, RolePlain))
	_ = m.HostCapable()
	_ = m.Outliers()
	_ = m.MemoryEstimate()
	proj := strings.Repeat("\u0130", 5) + ".ngram_embedding_proj.shard_0.weight"
	stem, ok := lookupStem(proj)
	if !ok || stem != strings.Repeat("\u0130", 5)+".ngram_embedding_proj" || hostName(stem) {
		t.Fatalf("proj stem %q ok=%v host=%v", stem, ok, hostName(stem))
	}
}

func TestQA5_LowerPanic(t *testing.T) {
	for _, r := range []string{"\u023a", "\u023e", "\u2c6f"} {
		name := strings.Repeat(r, 40) + ".ngram_embedding.shard_0.weight"
		stem, ok := lookupStem(name)
		if !ok || !strings.Contains(name, stem) || !strings.HasSuffix(stem, ".ngram_embedding") {
			t.Fatalf("%q -> %q %v", r, stem, ok)
		}
		m := qm(qt(name, "BF16", []int64{4096, 160}, 4096*160*2, RolePlain))
		_ = m.HostCapable()
		_ = m.Outliers()
		_ = m.MemoryEstimate()
	}
}

func TestQA5_SpoofedPrefix(t *testing.T) {
	m := qm(
		qt("a.ngram_embedding.shard_0.weight", "BF16", []int64{4096, 160}, 4096*160*2, RolePlain),
		qt("b.ngram_embedding.shard_1.weight", "BF16", []int64{4096, 160}, 4096*160*2, RolePlain),
		qt("model.layers.3.mlp.down_proj.shard_0.weight", "BF16", []int64{4096, 160}, 4096*160*2, RolePlain),
		qt("model.layers.3.self_attn.embed_table.shard_0.weight", "BF16", []int64{4096, 160}, 4096*160*2, RolePlain),
	)
	host := m.HostCapable()
	if len(host) != 3 {
		t.Fatalf("want 3 entries, got %+v", host)
	}
	for _, x := range host {
		if strings.Contains(x.Name, "down_proj") {
			t.Fatalf("projection counted: %+v", x)
		}
	}
	m = qm(
		qt(qaStem, "BF16", []int64{4096, 160}, 4096*160*2, RolePlain),
		qt(qaStem+".shard_0.weight", "BF16", []int64{4096, 160}, 4096*160*2, RolePlain),
	)
	host = m.HostCapable()
	if len(host) != 2 {
		t.Fatalf("bare stem collided with shards: %+v", host)
	}
	seen := map[string]bool{}
	var sharded, bare bool
	for _, h := range host {
		if h.Bytes != 4096*160*2 {
			t.Fatalf("bytes summed across the collision: %+v", h)
		}
		if seen[h.Name] {
			t.Fatalf("duplicate host name %q in %+v", h.Name, host)
		}
		seen[h.Name] = true
		if h.sharded {
			sharded = true
			if h.Name == qaStem || !strings.Contains(h.Name, qaStem) || !strings.Contains(h.Name, "sharded") {
				t.Fatalf("sharded name %q is not distinct from the stem", h.Name)
			}
			if h.stem != qaStem {
				t.Fatalf("stem = %q", h.stem)
			}
		} else if h.Name == qaStem {
			bare = true
		}
	}
	if !sharded || !bare {
		t.Fatalf("entries %+v", host)
	}
}

func TestQA5_ScalesBiases(t *testing.T) {
	weight := int64(4096 * 160 * 2)
	m := qm(
		qt(qaStem+".shard_0.weight", "BF16", []int64{4096, 160}, weight, RolePlain),
		qt(qaStem+".shard_5.scales", "BF16", []int64{4096, 5}, 4096*5*2, RolePlain),
		qt(qaStem+".shard_6.biases", "BF16", []int64{4096, 5}, 4096*5*2, RolePlain),
	)
	host := m.HostCapable()
	if len(host) != 1 || host[0].Bytes != weight {
		t.Fatalf("orphan scales/biases counted: %+v", host)
	}
	w := qt(qaStem+".shard_0.weight", "BF16", []int64{4096, 160}, weight, RolePlain)
	s := qt(qaStem+".shard_0.scales", "BF16", []int64{4096, 160}, weight, RolePlain)
	b := qt(qaStem+".shard_0.biases", "BF16", []int64{4096, 160}, weight, RolePlain)
	m = qm(w, s, b)
	mem := m.MemoryEstimate()
	if mem.HostCapableBytes != 3*weight || mem.HostCapableBytes > mem.WeightsBytes {
		t.Fatalf("double count: host %d weights %d", mem.HostCapableBytes, mem.WeightsBytes)
	}
	w2 := qt("model.ngram_embedding.weight", "BF16", []int64{4096, 160}, weight, RolePlain)
	s2 := qt("model.ngram_embedding.scales", "BF16", []int64{4096, 160}, weight, RolePlain)
	m = qm(w2, s2)
	mem = m.MemoryEstimate()
	if mem.HostCapableBytes != 2*weight || mem.HostCapableBytes > mem.WeightsBytes {
		t.Fatalf("unsharded double count: host %d weights %d", mem.HostCapableBytes, mem.WeightsBytes)
	}
	m = qm(
		qt(qaStem+".shard_0.weight", "U32", []int64{4096, 20}, 4096*20*4, RoleQuantWeight),
		qt(qaStem+".shard_1.scales", "BF16", []int64{4096, 5}, 4096*5*2, RoleQuantScales),
	)
	if host = m.HostCapable(); len(host) != 1 || host[0].Bytes != 4096*20*4 {
		t.Fatalf("cross-shard scales attributed: %+v", host)
	}
}

func TestQA5_Overflow(t *testing.T) {
	big := int64(math.MaxInt64/4) + 12345
	var ts []*Tensor
	for i := 0; i < 8; i++ {
		x := qt(fmt.Sprintf("%s.shard_%d.weight", qaStem, i), "U8", []int64{1024, 1}, big, RolePlain)
		x.LogicalParams = big
		ts = append(ts, x)
	}
	m := &Model{Format: "safetensors", Tensors: ts, TensorBytes: math.MaxInt64, TotalLogicalParams: math.MaxInt64}
	m.buildGroups()
	host := m.HostCapable()
	if !m.Partial {
		t.Fatal("overflow did not mark Partial")
	}
	for _, x := range host {
		if x.Bytes < 0 || x.ParamsEst < 0 {
			t.Fatalf("negative after overflow %+v", x)
		}
	}
	mem := m.MemoryEstimate()
	if mem.HostCapableBytes < 0 || mem.AcceleratorMinBytes < 0 || mem.HostCapableBytes > mem.WeightsBytes {
		t.Fatalf("memory %+v", mem)
	}
	for _, c := range m.Outliers() {
		if strings.Contains(c.Message, " -") {
			t.Fatalf("negative in %q", c.Message)
		}
	}
}

func TestQA5_PartialAcceleratorIsNotAFloor(t *testing.T) {
	// Host bytes are a lower bound on a partial inspection. Subtracting them
	// from weights can overstate the accelerator remainder, so the estimate
	// does not claim that subtraction as a minimum.
	n := int64(2 * GiB)
	proj := int64(4096)
	m := qm(
		qt(qaStem+".weight", "BF16", []int64{4096, 160}, n, RolePlain),
		qt("model.layers.0.mlp.down_proj.weight", "BF16", []int64{64, 64}, proj, RolePlain),
	)
	m.Partial = true
	mem := m.MemoryEstimate()
	if !mem.HostCapablePartial || mem.HostCapableBytes != n {
		t.Fatalf("memory %+v", mem)
	}
	if mem.AcceleratorMinBytes != 0 {
		t.Fatalf("partial accelerator %d, want 0", mem.AcceleratorMinBytes)
	}
	// A complete model still subtracts. The partial model above is cached
	// with Partial set, so this is a separate value.
	m = qm(
		qt(qaStem+".weight", "BF16", []int64{4096, 160}, n, RolePlain),
		qt("model.layers.0.mlp.down_proj.weight", "BF16", []int64{64, 64}, proj, RolePlain),
	)
	mem = m.MemoryEstimate()
	if mem.HostCapablePartial || mem.AcceleratorMinBytes != proj {
		t.Fatalf("complete accelerator %+v", mem)
	}
}

func TestQA5_PartialStillConfident(t *testing.T) {
	var ts []*Tensor
	for i := 0; i < 64; i++ {
		ts = append(ts, qt(fmt.Sprintf("%s.shard_%d.weight", qaStem, i), "BF16", []int64{1 << 16, 160}, (1<<16)*160*2, RolePlain))
	}
	m := qm(ts...)
	m.Partial = true
	var host int
	for _, c := range m.Outliers() {
		if c.Kind != "host_capable" && c.Kind != "big_tensor" {
			continue
		}
		host++
		if !strings.Contains(c.Message, "incomplete") {
			t.Errorf("unqualified partial callout: %s", c.Message)
		}
	}
	if host == 0 {
		t.Fatal("partial model produced no n-gram callout to qualify")
	}
	mem := m.MemoryEstimate()
	if !mem.HostCapablePartial {
		t.Fatal("memory line was not marked partial")
	}
}
