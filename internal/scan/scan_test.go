package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cecilkootz/model-inspector/internal/fixture"
	"github.com/cecilkootz/model-inspector/internal/hfcache"
	"github.com/cecilkootz/model-inspector/internal/model"
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

func checkID(t *testing.T, m *model.Model, id string) Check {
	t.Helper()
	return findCheck(t, Run(m), id)
}

func check(t *testing.T, m *model.Model) Check {
	t.Helper()
	return checkID(t, m, CheckIndex)
}

func findCheck(t *testing.T, res Result, id string) Check {
	t.Helper()
	for _, c := range res.Checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no check %s in %+v", id, res.Checks)
	return Check{}
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
	if c.Status != StatusSkip || !strings.Contains(c.Summary, "no index file") {
		t.Fatalf("%+v", c)
	}
	bounds := checkID(t, m, CheckOffsets)
	if bounds.Status != StatusPass {
		t.Fatalf("bounds %+v", bounds)
	}
	if Run(m).Overall != StatusPass {
		t.Fatal("single-file model with a valid header did not pass")
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
	files := checkID(t, m, CheckFiles)
	if files.Status != StatusFail {
		t.Fatalf("files %+v", files)
	}
	detailsContain(t, files, "missing.safetensors: unreadable:")
	c := check(t, m)
	if c.Status != StatusFail {
		t.Fatalf("status %s details %v", c.Status, c.Details)
	}
	detailsContain(t, c, "model.layers.0.self_attn.q_proj.weight -> missing.safetensors: shard missing or unreadable")
}

func TestTruncatedHeader(t *testing.T) {
	shard, _ := mustShard(t, []fixture.Tensor{
		ten("model.layers.0.self_attn.q_proj.weight", "BF16", 2),
	})
	m := loadDir(t, map[string][]byte{"model.safetensors": shard[:10]})
	c := checkID(t, m, CheckHeader)
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
	c := checkID(t, m, CheckHeader)
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
	c := checkID(t, m, CheckOffsets)
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
	c := checkID(t, m, CheckOffsets)
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
	c := checkID(t, m, CheckOffsets)
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
	c := checkID(t, m, CheckOffsets)
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
		t.Fatalf("overall %s; checks %+v", res.Overall, res.Checks)
	}
	idx := findCheck(t, res, CheckIndex)
	if idx.Status != StatusFail {
		t.Fatalf("index %+v", idx)
	}
	detailsContain(t, idx, "weight_map")
	hdr := findCheck(t, res, CheckHeader)
	detailsContain(t, hdr, "model.safetensors: unreadable:", "parsing header JSON")
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
	files := checkID(t, m, CheckFiles)
	if files.Status != StatusFail {
		t.Fatalf("files %+v", files)
	}
	detailsContain(t, files, "model-00002-of-00002.safetensors: unreadable:")
	c := check(t, m)
	if c.Status != StatusFail {
		t.Fatalf("status %s details %v", c.Status, c.Details)
	}
	detailsContain(t, c, "model.norm.weight -> model-00002-of-00002.safetensors: shard missing or unreadable")
}

func TestCodeExecWarnsAndStrictPromotes(t *testing.T) {
	files, err := fixture.CodeExec()
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
	res := Run(m)
	c := findCheck(t, res, CheckCodeExec)
	if c.Status != StatusWarn || c.EffectiveStatus != StatusWarn {
		t.Fatalf("%+v", c)
	}
	if res.Overall != StatusWarn {
		t.Fatalf("overall %s", res.Overall)
	}
	blob := strings.Join(c.Details, "\n")
	for _, want := range []string{"modeling_demo.py", "weights.bin", "auto_map", "modeling_demo.DemoForCausalLM"} {
		if !strings.Contains(blob, want) {
			t.Errorf("details missing %s\n%s", want, blob)
		}
	}
	content := findCheck(t, res, CheckCode)
	if content.Status != StatusSkip || !strings.Contains(content.Summary, "not run") {
		t.Fatalf("code-content %+v", content)
	}
	for _, ch := range res.Checks {
		if ch.ID == "behavior-probes" {
			t.Fatal("behavior-probes is still a check")
		}
	}
	strict := RunOptions(m, Options{Strict: true})
	promoted := findCheck(t, strict, CheckCodeExec)
	if promoted.Status != StatusWarn || promoted.EffectiveStatus != StatusFail {
		t.Fatalf("strict %+v", promoted)
	}
	if strict.Overall != StatusFail || !strict.Strict {
		t.Fatalf("strict overall %s", strict.Overall)
	}
}

func TestTotalSizeStrictPromotesWarn(t *testing.T) {
	shard, n := mustShard(t, []fixture.Tensor{
		ten("model.layers.0.self_attn.q_proj.weight", "BF16", 2),
	})
	if n == 999 {
		t.Fatal("payload unexpectedly 999")
	}
	idx := mustIndex(t, 999, map[string]string{
		"model.layers.0.self_attn.q_proj.weight": "model.safetensors",
	})
	m := loadDir(t, map[string][]byte{
		"model.safetensors":            shard,
		"model.safetensors.index.json": idx,
	})
	res := RunOptions(m, Options{Strict: true})
	c := findCheck(t, res, CheckIndex)
	if c.Status != StatusWarn || c.EffectiveStatus != StatusFail || res.Overall != StatusFail {
		t.Fatalf("status %s effective %s overall %s", c.Status, c.EffectiveStatus, res.Overall)
	}
}

func TestNoFlaggedCodeContentSkipsQuietly(t *testing.T) {
	files, _, err := fixture.TinyQwen()
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
	res := Run(m)
	c := findCheck(t, res, CheckCode)
	if c.Status != StatusSkip || !strings.Contains(c.Summary, "nothing to read") {
		t.Fatalf("%+v", c)
	}
	if res.Overall != StatusPass {
		t.Fatalf("overall %s", res.Overall)
	}
}

func TestPartialSaysIncompleteNotSafetyCap(t *testing.T) {
	good, _ := mustShard(t, []fixture.Tensor{ten("w", "F32", 1)})
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "a.safetensors"), good, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-config.json", filepath.Join(d, "config.json")); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(d)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Partial {
		t.Fatal("dangling config.json was not partial")
	}
	res := Run(m)
	if res.Overall == StatusPass {
		t.Fatal("dangling config.json overall pass")
	}
	c := findCheck(t, res, CheckFiles)
	blob := c.Summary + "\n" + strings.Join(c.Details, "\n")
	if !strings.Contains(blob, incompleteInspection) {
		t.Fatalf("file-presence = %s", blob)
	}
	if strings.Contains(blob, "safety cap") {
		t.Fatalf("partial wording still says safety cap: %s", blob)
	}
}
