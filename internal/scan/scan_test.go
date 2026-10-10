package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"origin.cursor.com/cecilkootz/model-inspector/internal/fixture"
	"origin.cursor.com/cecilkootz/model-inspector/internal/hfcache"
	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
)

func loadDir(t *testing.T, files map[string][]byte) *model.Model {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m, err := model.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func ten(name, dtype string, shape ...int64) fixture.Tensor {
	return fixture.Tensor{Name: name, DType: dtype, Shape: shape}
}

func mustShard(t *testing.T, tensors []fixture.Tensor) ([]byte, int64) {
	t.Helper()
	b, n, err := fixture.WriteShard("mlx", tensors)
	if err != nil {
		t.Fatal(err)
	}
	return b, n
}

func mustIndex(t *testing.T, total int64, wm map[string]string) []byte {
	t.Helper()
	b, err := fixture.IndexJSON(total, wm)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func check(t *testing.T, m *model.Model) Check {
	t.Helper()
	res := Run(m)
	if len(res.Checks) == 0 || res.Checks[0].ID != "index-shard-consistency" {
		t.Fatalf("checks = %+v", res.Checks)
	}
	if res.Overall == StatusPass && res.Checks[0].Status != StatusPass {
		t.Fatalf("overall PASS with check %s", res.Checks[0].Status)
	}
	if res.Overall != res.Checks[0].Status && res.Checks[0].Status != StatusSkip {
		t.Fatalf("overall %s, check %s", res.Overall, res.Checks[0].Status)
	}
	return res.Checks[0]
}

func detailsContain(t *testing.T, c Check, subs ...string) {
	t.Helper()
	blob := strings.Join(c.Details, "\n")
	for _, s := range subs {
		if !strings.Contains(blob, s) {
			t.Errorf("details missing %q\n%s", s, blob)
		}
	}
}

func TestTinyQwenScanPasses(t *testing.T) {
	files, tensorBytes, err := fixture.TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.TensorBytes != tensorBytes {
		t.Fatalf("tensor bytes %d, index payload %d", m.TensorBytes, tensorBytes)
	}
	c := check(t, m)
	if c.Status != StatusPass || len(c.Details) != 0 {
		t.Fatalf("%+v", c)
	}
	if !strings.Contains(c.Summary, "25/25") {
		t.Errorf("summary = %s", c.Summary)
	}
}

func TestNoShardsSkips(t *testing.T) {
	m := loadDir(t, map[string][]byte{"config.json": fixture.TinyQwenConfig()})
	c := check(t, m)
	if c.Status != StatusSkip {
		t.Fatalf("status %s (%s)", c.Status, c.Summary)
	}
}

func TestNoIndexPassesBounds(t *testing.T) {
	shard, _ := mustShard(t, []fixture.Tensor{
		ten("model.layers.0.self_attn.q_proj.weight", "BF16", 2),
	})
	m := loadDir(t, map[string][]byte{"model.safetensors": shard})
	c := check(t, m)
	if c.Status != StatusPass || !strings.Contains(c.Summary, "no index file") {
		t.Fatalf("%+v", c)
	}
}

func TestIndexTensorMissingFromShard(t *testing.T) {
	shard, n := mustShard(t, []fixture.Tensor{
		ten("model.layers.0.self_attn.q_proj.weight", "BF16", 2),
	})
	idx := mustIndex(t, n, map[string]string{
		"model.layers.0.self_attn.q_proj.weight":        "model-00001-of-00002.safetensors",
		"model.layers.0.mlp.experts.0.down_proj.weight": "model-00001-of-00002.safetensors",
	})
	m := loadDir(t, map[string][]byte{
		"model-00001-of-00002.safetensors": shard,
		"model.safetensors.index.json":     idx,
	})
	c := check(t, m)
	if c.Status != StatusFail {
		t.Fatalf("status %s details %v", c.Status, c.Details)
	}
	detailsContain(t, c, "model.layers.0.mlp.experts.0.down_proj.weight: not present in model-00001-of-00002.safetensors header")
}

func TestShardTensorMissingFromIndex(t *testing.T) {
	shard, n := mustShard(t, []fixture.Tensor{
		ten("model.layers.0.self_attn.q_proj.weight", "BF16", 2),
		ten("model.layers.0.mlp.gate.weight", "BF16", 2),
	})
	idx := mustIndex(t, n, map[string]string{
		"model.layers.0.self_attn.q_proj.weight": "model-00001-of-00002.safetensors",
	})
	m := loadDir(t, map[string][]byte{
		"model-00001-of-00002.safetensors": shard,
		"model.safetensors.index.json":     idx,
	})
	c := check(t, m)
	if c.Status != StatusWarn {
		t.Fatalf("status %s details %v", c.Status, c.Details)
	}
	detailsContain(t, c, "1 tensors in shard headers are not mapped to that shard by the index")
}

func TestIndexPointsAtMissingShard(t *testing.T) {
	idx := mustIndex(t, 4, map[string]string{
		"model.layers.0.self_attn.q_proj.weight": "missing.safetensors",
	})
	m := loadDir(t, map[string][]byte{"model.safetensors.index.json": idx})
	c := check(t, m)
	if c.Status != StatusFail {
		t.Fatalf("status %s details %v", c.Status, c.Details)
	}
	detailsContain(t, c,
		"missing.safetensors: unreadable:",
		"model.layers.0.self_attn.q_proj.weight -> missing.safetensors: shard missing or unreadable",
	)
}

func TestTruncatedHeader(t *testing.T) {
	shard, _ := mustShard(t, []fixture.Tensor{
		ten("model.layers.0.self_attn.q_proj.weight", "BF16", 2),
	})
	m := loadDir(t, map[string][]byte{"model.safetensors": shard[:10]})
	c := check(t, m)
	if c.Status != StatusFail {
		t.Fatalf("status %s details %v", c.Status, c.Details)
	}
	detailsContain(t, c, "model.safetensors: unreadable:")
}

func TestCorruptHeader(t *testing.T) {
	// 8-byte declared header that is not JSON.
	m := loadDir(t, map[string][]byte{
		"model.safetensors": fixture.EncodeHeader([]byte("not-json"), nil),
	})
	c := check(t, m)
	if c.Status != StatusFail {
		t.Fatalf("status %s details %v", c.Status, c.Details)
	}
	detailsContain(t, c, "model.safetensors: unreadable:", "parsing header JSON")
}

func TestDataRangePastEOF(t *testing.T) {
	hdr := `{"model.layers.0.self_attn.q_proj.weight":{"dtype":"BF16","shape":[2],"data_offsets":[0,4]}}`
	m := loadDir(t, map[string][]byte{
		"model.safetensors": fixture.EncodeHeader([]byte(hdr), []byte{0x00}),
	})
	c := check(t, m)
	if c.Status != StatusFail {
		t.Fatalf("status %s details %v", c.Status, c.Details)
	}
	detailsContain(t, c, "beyond 1-byte data section")
}

func TestDTypeSizeMismatch(t *testing.T) {
	hdr := `{"model.layers.0.self_attn.q_proj.weight":{"dtype":"F32","shape":[4],"data_offsets":[0,4]}}`
	m := loadDir(t, map[string][]byte{
		"model.safetensors": fixture.EncodeHeader([]byte(hdr), []byte{0, 0, 0, 0}),
	})
	c := check(t, m)
	if c.Status != StatusFail {
		t.Fatalf("status %s details %v", c.Status, c.Details)
	}
	detailsContain(t, c, "needs 16 bytes, range is 4")
}

func TestOverlappingRanges(t *testing.T) {
	hdr := `{"model.layers.0.self_attn.k_proj.weight":{"dtype":"BF16","shape":[2],"data_offsets":[2,6]},` +
		`"model.layers.0.self_attn.q_proj.weight":{"dtype":"BF16","shape":[2],"data_offsets":[0,4]}}`
	m := loadDir(t, map[string][]byte{
		"model.safetensors": fixture.EncodeHeader([]byte(hdr), make([]byte, 6)),
	})
	c := check(t, m)
	if c.Status != StatusFail {
		t.Fatalf("status %s details %v", c.Status, c.Details)
	}
	detailsContain(t, c, "model.layers.0.self_attn.k_proj.weight overlaps model.layers.0.self_attn.q_proj.weight")
}

func TestInvalidOffsets(t *testing.T) {
	hdr := `{"model.layers.0.self_attn.q_proj.weight":{"dtype":"BF16","shape":[2],"data_offsets":[4,2]}}`
	m := loadDir(t, map[string][]byte{
		"model.safetensors": fixture.EncodeHeader([]byte(hdr), make([]byte, 4)),
	})
	c := check(t, m)
	if c.Status != StatusFail {
		t.Fatalf("status %s details %v", c.Status, c.Details)
	}
	detailsContain(t, c, "invalid offsets [4,2]")
}

func TestTotalSizeMismatch(t *testing.T) {
	shard, n := mustShard(t, []fixture.Tensor{
		ten("model.layers.0.self_attn.q_proj.weight", "BF16", 2),
	})
	if n == 999 {
		t.Fatal("fixture payload unexpectedly 999")
	}
	idx := mustIndex(t, 999, map[string]string{
		"model.layers.0.self_attn.q_proj.weight": "model.safetensors",
	})
	m := loadDir(t, map[string][]byte{
		"model.safetensors":            shard,
		"model.safetensors.index.json": idx,
	})
	c := check(t, m)
	if c.Status != StatusWarn {
		t.Fatalf("status %s details %v", c.Status, c.Details)
	}
	detailsContain(t, c, "index metadata.total_size=999 but header byte ranges sum to")
}

func TestCorruptIndexFailsScan(t *testing.T) {
	shard, _ := mustShard(t, []fixture.Tensor{
		ten("model.layers.0.self_attn.q_proj.weight", "BF16", 2),
	})
	m := loadDir(t, map[string][]byte{
		"model.safetensors":            shard,
		"model.safetensors.index.json": []byte(`{"weight_map":`),
	})
	if m.Index != nil || m.IndexErr == nil {
		t.Fatalf("index=%v err=%v", m.Index, m.IndexErr)
	}
	if len(m.Tensors) != 1 {
		t.Fatalf("shard fallback loaded %d tensors", len(m.Tensors))
	}
	c := check(t, m)
	if c.Status != StatusFail {
		t.Fatalf("status %s summary %s details %v", c.Status, c.Summary, c.Details)
	}
	detailsContain(t, c, "index file unreadable:")
	if !strings.Contains(c.Summary, "unreadable") {
		t.Errorf("summary = %s", c.Summary)
	}
}

func TestEmptyWeightMapWithCorruptShard(t *testing.T) {
	// {"weight_map":{}} is an empty map, not a missing one. It must not count
	// as a usable index: a corrupt shard beside it has to fail the scan.
	m := loadDir(t, map[string][]byte{
		"model.safetensors":            fixture.EncodeHeader([]byte("not-json"), nil),
		"model.safetensors.index.json": []byte(`{"weight_map":{}}`),
	})
	if m.Index != nil {
		t.Fatalf("empty weight_map was accepted: %+v", m.Index)
	}
	if m.IndexErr == nil || !strings.Contains(m.IndexErr.Error(), "weight_map") {
		t.Fatalf("IndexErr = %v", m.IndexErr)
	}
	if len(m.Shards) != 1 || m.Shards[0].Header != nil {
		t.Fatalf("corrupt shard was not read: %+v", m.Shards)
	}
	res := Run(m)
	if res.Overall != StatusFail {
		t.Fatalf("overall %s; check %+v", res.Overall, res.Checks[0])
	}
	if res.Checks[0].Status != StatusFail {
		t.Fatalf("check %+v", res.Checks[0])
	}
	detailsContain(t, res.Checks[0], "weight_map", "model.safetensors: unreadable:", "parsing header JSON")
}

func TestIndexMissingWeightMap(t *testing.T) {
	shard, _ := mustShard(t, []fixture.Tensor{
		ten("model.layers.0.self_attn.q_proj.weight", "BF16", 2),
	})
	m := loadDir(t, map[string][]byte{
		"model.safetensors":            shard,
		"model.safetensors.index.json": []byte(`{"metadata":{"total_size":4}}`),
	})
	if m.IndexErr == nil || !strings.Contains(m.IndexErr.Error(), "missing or empty weight_map") {
		t.Fatalf("IndexErr = %v", m.IndexErr)
	}
	c := check(t, m)
	if c.Status != StatusFail {
		t.Fatalf("status %s details %v", c.Status, c.Details)
	}
	detailsContain(t, c, "missing or empty weight_map")
	if strings.Contains(c.Summary, "unreadable") {
		t.Fatalf("parsed index called unreadable: %s", c.Summary)
	}
	if !strings.Contains(c.Summary, "missing or empty weight_map") {
		t.Fatalf("summary = %s", c.Summary)
	}
	if len(m.Shards) != 1 || m.Shards[0].Header == nil {
		t.Fatal("expected the on-disk shard to still be read")
	}
}

func TestDanglingSnapshotSymlink(t *testing.T) {
	shard, n := mustShard(t, []fixture.Tensor{
		ten("model.layers.0.self_attn.q_proj.weight", "BF16", 2),
	})
	idx := mustIndex(t, n, map[string]string{
		"model.layers.0.self_attn.q_proj.weight": "model-00001-of-00002.safetensors",
		"model.norm.weight":                      "model-00002-of-00002.safetensors",
	})
	const (
		repo   = "Qwen/Tiny-Next"
		commit = "0123456789abcdef0123456789abcdef01234567"
	)
	hub := t.TempDir()
	snap, err := fixture.MaterializeHub(hub, repo, commit, []fixture.File{
		{Name: "model.safetensors.index.json", Data: idx},
		{Name: "model-00001-of-00002.safetensors", Data: shard},
	})
	if err != nil {
		t.Fatal(err)
	}
	dangling := filepath.Join(snap, "model-00002-of-00002.safetensors")
	if err := os.Symlink(filepath.Join("..", "..", "blobs", "missing-blob"), dangling); err != nil {
		t.Fatal(err)
	}
	resolved, err := hfcache.Resolve(repo, hub)
	if err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(resolved.Dir)
	if err != nil {
		t.Fatal(err)
	}
	var broken model.File
	var found bool
	for _, f := range m.Files {
		if f.Name == "model-00002-of-00002.safetensors" {
			broken, found = f, true
		}
	}
	if !found || !broken.Symlink || !broken.Broken || broken.Size != 0 {
		t.Fatalf("dangling file entry = %+v found=%v", broken, found)
	}
	var warned bool
	for _, w := range m.Warnings {
		if strings.Contains(w, "model-00002-of-00002.safetensors") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("warnings = %v", m.Warnings)
	}
	c := check(t, m)
	if c.Status != StatusFail {
		t.Fatalf("status %s details %v", c.Status, c.Details)
	}
	detailsContain(t, c,
		"model-00002-of-00002.safetensors: unreadable:",
		"model.norm.weight -> model-00002-of-00002.safetensors: shard missing or unreadable",
	)
}
