package report

import (
	"bytes"
	"os"
	"path/filepath"
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
