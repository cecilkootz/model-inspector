package report

import (
	"math"
	"strings"
	"testing"

	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
	"origin.cursor.com/cecilkootz/model-inspector/internal/safetensors"
	"origin.cursor.com/cecilkootz/model-inspector/internal/scan"
)

func TestQA5_GroupPctClamped(t *testing.T) {
	if got := pct(100, 10); got != 100 {
		t.Fatalf("pct(100, 10) = %v", got)
	}
	got := pct(math.MaxInt64, math.MaxInt64-1)
	if got < 0 || got > 100 {
		t.Fatalf("pct overflow = %v", got)
	}
}

func TestQA5_OverflowRefreshesScan(t *testing.T) {
	big := int64(math.MaxInt64/2) + 1
	var tensors []*model.Tensor
	for i := 0; i < 3; i++ {
		name := "x.ngram_embedding.shard_" + string(rune('0'+i)) + ".weight"
		tensors = append(tensors, &model.Tensor{
			TensorInfo: safetensors.TensorInfo{
				Name: name, DType: "BF16", Shape: []int64{4096, 64},
				DataOffsets: [2]int64{0, big},
			},
			Role: model.RolePlain, Group: "ngram", Kind: model.KindEmbeddings,
		})
	}
	m := &model.Model{
		Format: "safetensors", Tensors: tensors, TensorBytes: math.MaxInt64,
		DTypes: map[string]*model.DTypeStat{}, BitsHistogram: map[int]int{},
	}
	res := scan.Run(m)
	if m.Partial || res.Overall == scan.StatusFail {
		t.Fatalf("precondition partial=%v overall=%s", m.Partial, res.Overall)
	}
	doc := Build(m, res, false, 0)
	if !doc.Partial || doc.Scan.Overall != string(scan.StatusFail) {
		t.Fatalf("partial=%v overall=%s", doc.Partial, doc.Scan.Overall)
	}
	if doc.Memory.HostCapableBytes > doc.Memory.WeightsBytes {
		t.Fatalf("host %d > weights %d", doc.Memory.HostCapableBytes, doc.Memory.WeightsBytes)
	}
	out := Text(doc)
	if !strings.Contains(out, "(incomplete)") {
		t.Fatalf("text omitted incomplete:\n%s", out)
	}
}
