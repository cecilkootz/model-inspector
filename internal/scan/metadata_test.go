package scan

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cecilkootz/model-inspector/internal/fixture"
	"github.com/cecilkootz/model-inspector/internal/model"
)

func loadFiles(t *testing.T, files []fixture.File, err error) *model.Model {
	t.Helper()
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
	return m
}

func denseWith(t *testing.T, cfg string) *model.Model {
	t.Helper()
	files, err := fixture.DenseShapes([]byte(cfg))
	return loadFiles(t, files, err)
}

func TestMetadataExistingFixturesDoNotWarn(t *testing.T) {
	withBytes := func(f func() ([]fixture.File, int64, error)) func() ([]fixture.File, error) {
		return func() ([]fixture.File, error) {
			files, _, err := f()
			return files, err
		}
	}
	cases := []struct {
		name   string
		build  func() ([]fixture.File, error)
		status Status
	}{
		{"TinyQwen", withBytes(fixture.TinyQwen), StatusPass},
		{"TinyQwenMLXQuant", withBytes(fixture.TinyQwenMLXQuant), StatusPass},
		{"QwenShape", fixture.QwenShape, StatusPass},
		{"NgramTable", fixture.NgramTable, StatusPass},
		{"TiedEmbed", fixture.TiedEmbed, StatusSkip},
		{"UntiedEmbed", fixture.UntiedEmbed, StatusSkip},
		{"ShardedNgram", fixture.ShardedNgram, StatusSkip},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			files, err := c.build()
			m := loadFiles(t, files, err)
			got := checkID(t, m, CheckMetadata)
			if got.Status != c.status || len(got.Details) != 0 {
				t.Fatalf("%+v", got)
			}
		})
	}
}

func TestMetadataMismatchFixtureWarns(t *testing.T) {
	files, err := fixture.MetadataMismatch()
	m := loadFiles(t, files, err)
	res := Run(m)
	c := findCheck(t, res, CheckMetadata)
	if c.Status != StatusWarn || c.EffectiveStatus != StatusWarn || res.Overall != StatusWarn {
		t.Fatalf("%+v overall %s", c, res.Overall)
	}
	if c.Summary != "4 of 4 config comparisons disagree" {
		t.Fatalf("summary %q", c.Summary)
	}
	want := []string{
		"hidden_size is 16, token embedding width is 8",
		"num_hidden_layers is 3, decoder layers in tensor names is 2",
		"vocab_size is 64, lm_head rows is 32",
		"vocab_size is 64, token embedding rows is 32",
	}
	if !reflect.DeepEqual(c.Details, want) {
		t.Fatalf("details %q", c.Details)
	}
	strict := findCheck(t, RunOptions(m, Options{Strict: true}), CheckMetadata)
	if strict.Status != StatusWarn || strict.EffectiveStatus != StatusFail {
		t.Fatalf("strict %+v", strict)
	}
}

func TestMetadataDenseMatches(t *testing.T) {
	c := checkID(t, denseWith(t, `{"hidden_size":8,"vocab_size":32,"num_hidden_layers":2}`), CheckMetadata)
	if c.Status != StatusPass || c.Summary != "4 config comparisons match tensor shapes" || len(c.Details) != 0 {
		t.Fatalf("%+v", c)
	}
}

func TestMetadataNestedScopeWins(t *testing.T) {
	m := denseWith(t, `{"hidden_size":99,"vocab_size":99,"text_config":{"hidden_size":16,"vocab_size":32}}`)
	c := checkID(t, m, CheckMetadata)
	if c.Status != StatusWarn {
		t.Fatalf("%+v", c)
	}
	want := []string{"text_config.hidden_size is 16, token embedding width is 8"}
	if !reflect.DeepEqual(c.Details, want) {
		t.Fatalf("details %q", c.Details)
	}
}

func TestMetadataEmbeddingProjectionWidth(t *testing.T) {
	c := checkID(t, denseWith(t, `{"hidden_size":16,"word_embed_proj_dim":8}`), CheckMetadata)
	if c.Status != StatusPass {
		t.Fatalf("%+v", c)
	}
}

func TestMetadataHostileConfigValues(t *testing.T) {
	cases := []struct {
		lit     string
		status  Status
		detail  string
		summary string
	}{
		{lit: `8`, status: StatusPass},
		{lit: `8.0`, status: StatusPass},
		{lit: `8e0`, status: StatusPass},
		{lit: `80e-1`, status: StatusPass},
		{lit: `null`, status: StatusPass, summary: "3 config comparisons match tensor shapes"},
		{lit: `0`, status: StatusWarn, detail: "hidden_size is 0, token embedding width is 8"},
		{lit: `-0`, status: StatusWarn, detail: "hidden_size is 0, token embedding width is 8"},
		{lit: `-8`, status: StatusWarn, detail: "hidden_size is negative"},
		{lit: `-8.0`, status: StatusWarn, detail: "hidden_size is negative"},
		{lit: `8.5`, status: StatusWarn, detail: "hidden_size is not a whole number"},
		{lit: `1e400`, status: StatusWarn, detail: "hidden_size is out of range"},
		{lit: `-1e400`, status: StatusWarn, detail: "hidden_size is out of range"},
		{lit: `1e-400`, status: StatusWarn, detail: "hidden_size is out of range"},
		{lit: `9007199254740993.0`, status: StatusWarn, detail: "hidden_size is out of range"},
		{lit: `99999999999999999999999`, status: StatusWarn, detail: "hidden_size is out of range"},
		{lit: `9223372036854775807`, status: StatusWarn, detail: "hidden_size is out of range"},
		{lit: `9007199254740991`, status: StatusWarn, detail: "hidden_size is 9007199254740991, token embedding width is 8"},
		{lit: `"8"`, status: StatusWarn, detail: "hidden_size is not a number"},
		{lit: `"\u001b[31m8"`, status: StatusWarn, detail: "hidden_size is not a number"},
		{lit: `true`, status: StatusWarn, detail: "hidden_size is not a number"},
		{lit: `[8]`, status: StatusWarn, detail: "hidden_size is not a number"},
		{lit: `{"v":8}`, status: StatusWarn, detail: "hidden_size is not a number"},
	}
	for _, c := range cases {
		t.Run(c.lit, func(t *testing.T) {
			m := denseWith(t, `{"vocab_size":32,"num_hidden_layers":2,"hidden_size":`+c.lit+`}`)
			got := checkID(t, m, CheckMetadata)
			if got.Status != c.status {
				t.Fatalf("%+v", got)
			}
			if c.detail != "" && !reflect.DeepEqual(got.Details, []string{c.detail}) {
				t.Fatalf("details %q", got.Details)
			}
			if c.summary != "" && got.Summary != c.summary {
				t.Fatalf("summary %q", got.Summary)
			}
			for _, d := range got.Details {
				if strings.ContainsAny(d, "\x1b\"[{") {
					t.Fatalf("detail echoed the config value: %q", d)
				}
			}
		})
	}
}

func TestMetadataUnparseableConfigSkips(t *testing.T) {
	for _, cfg := range []string{`{"hidden_size": NaN}`, `{"hidden_size": Infinity}`, `[8]`, `{`} {
		c := checkID(t, denseWith(t, cfg), CheckMetadata)
		if c.Status != StatusSkip || c.Summary != "no usable config.json" {
			t.Fatalf("%s: %+v", cfg, c)
		}
	}
}

func TestMetadataNoComparableField(t *testing.T) {
	c := checkID(t, denseWith(t, `{"model_type":"llama","intermediate_size":8}`), CheckMetadata)
	if c.Status != StatusSkip || c.Summary != "no config field to compare with tensors" {
		t.Fatalf("%+v", c)
	}
}

func mlxEmbed(t *testing.T, cfg string) *model.Model {
	t.Helper()
	shard, _ := mustShard(t, []fixture.Tensor{
		// 4-bit, group 32: 4 U32 words per row are 32 logical columns.
		ten("model.embed_tokens.weight", "U32", 64, 4),
		ten("model.embed_tokens.scales", "BF16", 64, 1),
		ten("model.embed_tokens.biases", "BF16", 64, 1),
		ten("model.layers.0.self_attn.q_proj.weight", "BF16", 2),
	})
	return loadDir(t, map[string][]byte{"config.json": []byte(cfg), "model.safetensors": shard})
}

func TestMetadataMLXPackedEmbeddingIsDequantized(t *testing.T) {
	quant := `"quantization":{"bits":4,"group_size":32}`
	ok := checkID(t, mlxEmbed(t, `{"hidden_size":32,"vocab_size":64,"num_hidden_layers":1,`+quant+`}`), CheckMetadata)
	if ok.Status != StatusPass || len(ok.Details) != 0 {
		t.Fatalf("%+v", ok)
	}
	bad := checkID(t, mlxEmbed(t, `{"hidden_size":4,"vocab_size":64,`+quant+`}`), CheckMetadata)
	want := []string{"hidden_size is 4, token embedding width (dequantized) is 32"}
	if bad.Status != StatusWarn || !reflect.DeepEqual(bad.Details, want) {
		t.Fatalf("%+v", bad)
	}
}

func TestMetadataPackedWithoutScalesIsNotCompared(t *testing.T) {
	shard, _ := mustShard(t, []fixture.Tensor{
		ten("model.embed_tokens.weight", "U8", 64, 4),
		ten("model.layers.0.self_attn.q_proj.weight", "BF16", 2),
	})
	m := loadDir(t, map[string][]byte{
		"config.json":       []byte(`{"hidden_size":32,"vocab_size":64,"num_hidden_layers":1}`),
		"model.safetensors": shard,
	})
	c := checkID(t, m, CheckMetadata)
	want := []string{"token embedding not compared: integer dtype with no scales (packed)"}
	if c.Status != StatusPass || !reflect.DeepEqual(c.Details, want) {
		t.Fatalf("%+v", c)
	}
}

func TestMetadataEmbeddingsDisagree(t *testing.T) {
	shard, _ := mustShard(t, []fixture.Tensor{
		ten("model.embed_tokens.weight", "BF16", 64, 8),
		ten("language_model.embed_tokens.weight", "BF16", 32, 8),
	})
	m := loadDir(t, map[string][]byte{
		"config.json":       []byte(`{"hidden_size":8,"vocab_size":64}`),
		"model.safetensors": shard,
	})
	c := checkID(t, m, CheckMetadata)
	want := []string{"token embedding not compared: tensors disagree on shape"}
	if c.Status != StatusSkip || !reflect.DeepEqual(c.Details, want) {
		t.Fatalf("%+v", c)
	}
}

func TestMetadataMTPStoredAsLastLayer(t *testing.T) {
	shard, _ := mustShard(t, []fixture.Tensor{
		ten("model.layers.0.self_attn.q_proj.weight", "BF16", 2),
		ten("model.layers.1.self_attn.q_proj.weight", "BF16", 2),
		ten("model.layers.2.eh_proj.weight", "BF16", 2),
	})
	load := func(cfg string) Check {
		return checkID(t, loadDir(t, map[string][]byte{"config.json": []byte(cfg), "model.safetensors": shard}), CheckMetadata)
	}
	if c := load(`{"num_hidden_layers":2,"num_nextn_predict_layers":1}`); c.Status != StatusPass {
		t.Fatalf("%+v", c)
	}
	if c := load(`{"num_hidden_layers":2}`); c.Status != StatusWarn {
		t.Fatalf("%+v", c)
	}
	if c := load(`{"num_hidden_layers":9223372036854775807,"num_nextn_predict_layers":9223372036854775807}`); c.Status != StatusWarn {
		t.Fatalf("overflow %+v", c)
	}
}

func TestMetadataUnreadShardSkipsLayerCount(t *testing.T) {
	a, _ := mustShard(t, []fixture.Tensor{ten("model.layers.0.self_attn.q_proj.weight", "BF16", 2)})
	idx := mustIndex(t, 0, map[string]string{
		"model.layers.0.self_attn.q_proj.weight": "model-00001-of-00002.safetensors",
		"model.layers.1.self_attn.q_proj.weight": "model-00002-of-00002.safetensors",
	})
	m := loadDir(t, map[string][]byte{
		"config.json":                      []byte(`{"num_hidden_layers":2}`),
		"model.safetensors.index.json":     idx,
		"model-00001-of-00002.safetensors": a,
	})
	c := checkID(t, m, CheckMetadata)
	want := []string{"num_hidden_layers not compared: some shards were not read"}
	if c.Status != StatusSkip || !reflect.DeepEqual(c.Details, want) {
		t.Fatalf("%+v", c)
	}
}

// Dropped decoder indices keep the v1 fail exactly, even when the config
// would also disagree.
func TestMetadataDroppedLayersUnchanged(t *testing.T) {
	shard, _ := mustShard(t, []fixture.Tensor{
		ten("model.embed_tokens.weight", "BF16", 32, 8),
		ten("model.layers.0.self_attn.q_proj.weight", "BF16", 2),
		ten("model.layers.5000.self_attn.q_proj.weight", "BF16", 2),
	})
	m := loadDir(t, map[string][]byte{
		"config.json":       []byte(`{"hidden_size":16,"num_hidden_layers":1}`),
		"model.safetensors": shard,
	})
	c := checkID(t, m, CheckMetadata)
	want := model.DroppedLayerWarning(5000)
	if c.Name != "Layer index" || c.Status != StatusFail || c.Summary != want || !reflect.DeepEqual(c.Details, []string{want}) {
		t.Fatalf("%+v", c)
	}
}
