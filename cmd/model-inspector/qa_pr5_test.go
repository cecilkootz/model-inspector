package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cecilkootz/model-inspector/internal/fixture"
)

func TestQA5_MissingShardIsPartial(t *testing.T) {
	shard, _, err := fixture.WriteShard("", []fixture.Tensor{
		{Name: "model.layers.1.ple.ngram_embedding.shard_0.weight", DType: "BF16", Shape: []int64{8192, 64}},
	})
	if err != nil {
		t.Fatal(err)
	}
	idx, err := fixture.IndexJSON(1, map[string]string{
		"model.layers.1.ple.ngram_embedding.shard_0.weight": "model-00001-of-00002.safetensors",
		"model.layers.1.ple.ngram_embedding.shard_1.weight": "model-00002-of-00002.safetensors",
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	files := []fixture.File{
		{Name: "config.json", Data: []byte(`{"model_type":"qwen3_next"}`)},
		{Name: "model.safetensors.index.json", Data: idx},
		{Name: "model-00001-of-00002.safetensors", Data: shard},
	}
	if err := fixture.Materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	out, stderr, code := capture(t, func() int { return run([]string{"inspect", "--path", dir}) })
	if code != 1 {
		t.Fatalf("exit %d, want 1\n%s\n%s", code, stderr, out)
	}
	if !strings.Contains(out, "(incomplete)") {
		t.Fatalf("text omitted incomplete:\n%s", out)
	}
	js, stderr, code := capture(t, func() int { return run([]string{"inspect", "--path", dir, "--json"}) })
	if code != 1 {
		t.Fatalf("json exit %d, want 1\n%s\n%s", code, stderr, js)
	}
	var doc struct {
		Partial bool `json:"partial"`
		Scan    struct {
			Overall string `json:"overall"`
		} `json:"scan"`
	}
	if err := json.Unmarshal([]byte(js), &doc); err != nil {
		t.Fatalf("json: %v\n%s", err, js)
	}
	if !doc.Partial || doc.Scan.Overall != "fail" {
		t.Fatalf("partial=%v overall=%s", doc.Partial, doc.Scan.Overall)
	}
}
