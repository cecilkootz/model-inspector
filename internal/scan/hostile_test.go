package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"origin.cursor.com/cecilkootz/model-inspector/internal/fixture"
	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
)

func TestEmptyIndexDoesNotPass(t *testing.T) {
	d := t.TempDir()
	files := map[string][]byte{
		"model.safetensors":            fixture.EncodeHeader([]byte("not-json"), nil),
		"model.safetensors.index.json": []byte(`{}`),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(d, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m, err := model.Load(d)
	if err != nil {
		t.Fatal(err)
	}
	if m.Index != nil {
		t.Fatalf("index %+v", m.Index)
	}
	if m.IndexErr == nil || !strings.Contains(m.IndexErr.Error(), "missing or empty weight_map") {
		t.Fatalf("IndexErr = %v", m.IndexErr)
	}
	res := Run(m)
	idx := findCheck(t, res, CheckIndex)
	if res.Overall == StatusPass || idx.Status == StatusSkip {
		t.Fatalf("overall %s check %+v", res.Overall, idx)
	}
	if strings.Contains(idx.Summary, "unreadable") {
		t.Fatalf("empty index called unreadable: %s", idx.Summary)
	}
	if !strings.Contains(idx.Summary, "missing or empty weight_map") {
		t.Fatalf("summary %s", idx.Summary)
	}
	detailsContain(t, idx, "missing or empty weight_map")
	detailsContain(t, findCheck(t, res, CheckHeader), "model.safetensors: unreadable:")
}

func TestMultipleIndexesAreNotPicked(t *testing.T) {
	shard, n := mustShard(t, []fixture.Tensor{ten("model.norm.weight", "F32", 1)})
	idx := mustIndex(t, n, map[string]string{"model.norm.weight": "model.safetensors"})
	m := loadDir(t, map[string][]byte{
		"model.safetensors":            shard,
		"model.safetensors.index.json": idx,
		"other.safetensors.index.json": []byte(`{"weight_map":{"nope":"missing.safetensors"}}`),
	})
	if m.Index != nil {
		t.Fatalf("picked an index: %+v", m.Index)
	}
	if m.IndexErr == nil || !strings.Contains(m.IndexErr.Error(), "multiple") {
		t.Fatalf("IndexErr = %v", m.IndexErr)
	}
	if len(m.Shards) != 1 || m.Shards[0].Header == nil {
		t.Fatalf("on-disk shard not checked: %+v", m.Shards)
	}
	c := check(t, m)
	if c.Status != StatusFail {
		t.Fatalf("%+v", c)
	}
	if strings.Contains(c.Summary, "unreadable") {
		t.Fatalf("summary %s", c.Summary)
	}
}

func TestIndexDotDotShard(t *testing.T) {
	root := t.TempDir()
	d := filepath.Join(root, "model")
	outside, _, err := fixture.WriteShard("", []fixture.Tensor{{Name: "x", DType: "F32", Shape: []int64{1}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside.safetensors"), outside, 0o644); err != nil {
		t.Fatal(err)
	}
	inside := fixture.EncodeHeader([]byte(`{"evil":{"dtype":"F32","shape":[1],"data_offsets":[0,999]}}`), []byte{1, 2, 3, 4})
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "model-00001.safetensors"), inside, 0o644); err != nil {
		t.Fatal(err)
	}
	idx := []byte(`{"weight_map":{"x":"../outside.safetensors"}}`)
	if err := os.WriteFile(filepath.Join(d, "model.safetensors.index.json"), idx, 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, tensor := range m.Tensors {
		if tensor.Name == "x" {
			t.Fatalf("loaded tensor from outside the model dir")
		}
	}
	var sawOutside bool
	for _, s := range m.Shards {
		if s.Name == "../outside.safetensors" {
			sawOutside = true
			if s.Header != nil {
				t.Fatal("opened the escaped shard")
			}
		}
	}
	if !sawOutside {
		t.Fatal("non-local shard name was dropped")
	}
	res := Run(m)
	files := findCheck(t, res, CheckFiles)
	if res.Overall != StatusFail || files.Status != StatusFail {
		t.Fatalf("overall %s file-presence %+v", res.Overall, files)
	}
	detailsContain(t, files, "non-local", "../outside.safetensors", "model-00001.safetensors")
	indexCheck := findCheck(t, res, CheckIndex)
	if indexCheck.Status != StatusFail {
		t.Fatalf("index %+v", indexCheck)
	}
	detailsContain(t, indexCheck, "../outside.safetensors", "not a local file name")
}

func TestUnreferencedShardIsChecked(t *testing.T) {
	listed, n := mustShard(t, []fixture.Tensor{ten("model.norm.weight", "F32", 1)})
	extra, _ := mustShard(t, []fixture.Tensor{ten("model.extra.weight", "F32", 1)})
	idx := mustIndex(t, n, map[string]string{"model.norm.weight": "model-00001-of-00002.safetensors"})
	m := loadDir(t, map[string][]byte{
		"model-00001-of-00002.safetensors": listed,
		"model-00002-of-00002.safetensors": extra,
		"model.safetensors.index.json":     idx,
	})
	if len(m.Shards) != 2 {
		t.Fatalf("shards %+v", shardNames(m))
	}
	c := checkID(t, m, CheckFiles)
	if c.Status != StatusWarn && c.Status != StatusFail {
		t.Fatalf("unreferenced shard ignored: %+v", c)
	}
	detailsContain(t, c, "model-00002-of-00002.safetensors: safetensors file is not referenced by the index")
	bounds := checkID(t, m, CheckOffsets)
	if bounds.Status == StatusSkip {
		t.Fatal("extra shard was not examined")
	}
	if !strings.Contains(bounds.Summary, "2 ranges") {
		t.Fatalf("extra tensor was not examined: %s", bounds.Summary)
	}
	indexCheck := checkID(t, m, CheckIndex)
	if !strings.Contains(strings.Join(indexCheck.Details, "\n"), "model.extra.weight") {
		t.Fatalf("extra tensor was not examined: %+v", indexCheck)
	}
}

func TestEmptyAndGGUFDoNotPass(t *testing.T) {
	empty, err := model.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	res := Run(empty)
	if res.Overall == StatusPass {
		t.Fatalf("empty dir overall %s (%s)", res.Overall, res.Checks[0].Summary)
	}

	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "model.gguf"), []byte("GGUF\x00not-a-real-header"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(d)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.Format, "gguf") || !strings.Contains(m.Format, "not supported") {
		t.Fatalf("format %q", m.Format)
	}
	res = Run(m)
	if res.Overall == StatusPass {
		t.Fatalf("gguf-only scan passed: %+v", res.Checks[0])
	}
}

func shardNames(m *model.Model) []string {
	out := make([]string, len(m.Shards))
	for i, s := range m.Shards {
		out[i] = s.Name
	}
	return out
}
