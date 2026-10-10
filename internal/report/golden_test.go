package report

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"origin.cursor.com/cecilkootz/model-inspector/internal/fixture"
	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
	"origin.cursor.com/cecilkootz/model-inspector/internal/scan"
)

func TestGoldenJSON(t *testing.T) {
	cases := []struct {
		name  string
		files func() ([]fixture.File, error)
	}{
		{"tiny-qwen", func() ([]fixture.File, error) {
			files, _, err := fixture.TinyQwen()
			return files, err
		}},
		{"tiny-qwen-mlx-quant", func() ([]fixture.File, error) {
			files, _, err := fixture.TinyQwenMLXQuant()
			return files, err
		}},
		{"ngram-table", func() ([]fixture.File, error) { return fixture.NgramTable() }},
		{"tied-embed", func() ([]fixture.File, error) { return fixture.TiedEmbed() }},
		{"code-exec", func() ([]fixture.File, error) { return fixture.CodeExec() }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files, err := tc.files()
			if err != nil {
				t.Fatal(err)
			}
			got := goldenDoc(t, files)
			path := filepath.Join("testdata", tc.name+".json")
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("golden %s differs\n--- got ---\n%s", tc.name, got)
			}
		})
	}
}

func TestShardedNgramJSON(t *testing.T) {
	files, err := fixture.ShardedNgram()
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
	doc := Build(m, scan.Run(m), false, 0)
	if doc.SchemaVersion != 1 || doc.Partial {
		t.Fatalf("schema %d partial %v", doc.SchemaVersion, doc.Partial)
	}
	if doc.Memory.HostCapableBytes != 32_000_153_600 {
		t.Fatalf("host bytes %d", doc.Memory.HostCapableBytes)
	}
	if doc.Tensors.ParamsEst != 51_200_245_760 {
		t.Fatalf("params %d", doc.Tensors.ParamsEst)
	}
	if len(doc.HostCapableTensors) != 1 {
		t.Fatalf("host tensors %+v", doc.HostCapableTensors)
	}
	h := doc.HostCapableTensors[0]
	if h.Bytes != doc.Memory.HostCapableBytes || h.ParamsEst != doc.Tensors.ParamsEst || h.Rule != model.RuleLookupTable {
		t.Fatalf("host json %+v", h)
	}
	if strings.Contains(h.Name, "shard_") || h.Group != "ngram" {
		t.Fatalf("host name %q group %q", h.Name, h.Group)
	}
	var sawHost, sawParams bool
	for _, c := range doc.Callouts {
		if c.Kind == "host_capable" && strings.Contains(c.Message, "29.80 GiB") {
			sawHost = true
		}
		if strings.Contains(c.Message, "51.20B-param n-gram") {
			sawParams = true
		}
	}
	if !sawHost || !sawParams {
		t.Fatalf("callouts %+v", doc.Callouts)
	}
}

func TestPartialJSONField(t *testing.T) {
	m := &model.Model{Format: "safetensors", Partial: true}
	doc := Build(m, scan.Run(m), false, 0)
	if doc.SchemaVersion != 1 || !doc.Partial {
		t.Fatalf("schema %d partial %v", doc.SchemaVersion, doc.Partial)
	}
	b, err := Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"partial": true`)) {
		t.Fatalf("json missing partial\n%s", b)
	}
	m.Partial = false
	doc = Build(m, scan.Run(m), false, 0)
	b, err = Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"partial": false`)) {
		t.Fatalf("json missing partial false\n%s", b)
	}
}

func goldenDoc(t *testing.T, files []fixture.File) []byte {
	t.Helper()
	dir := t.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	doc := Build(m, scan.Run(m), false, 0)
	doc.ElapsedMS = 0
	doc.Source.Path = "DIR"
	b, err := Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	b = bytes.ReplaceAll(b, []byte(m.Dir), []byte("DIR"))
	return b
}
