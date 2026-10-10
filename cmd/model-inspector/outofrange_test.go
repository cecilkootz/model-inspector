package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"origin.cursor.com/cecilkootz/model-inspector/internal/fixture"
	"origin.cursor.com/cecilkootz/model-inspector/internal/jsonx"
)

// The tree, the text summary, and --json must all keep 1e400 and a
// 100k-digit integer. Unmarshal used to reject the file, so --json reported
// an empty config while the tree showed the literals.
func TestOutOfRangeAgrees(t *testing.T) {
	digits := strings.Repeat("9", 100_000)
	raw := []byte(`{"model_type":"qwen4_exp","num_hidden_layers":1e400,"vocab_size":` + digits + `}`)

	v, e1 := jsonx.Unmarshal(raw)
	tree, e2 := jsonx.ParseTree(raw, jsonx.Limits{})
	if e1 != nil || e2 != nil {
		t.Fatalf("parse u=%v t=%v", e1, e2)
	}
	if !jsonxExportedSame(v, tree) {
		t.Fatal("tree and unmarshal disagree")
	}
	got := map[string]string{}
	for c := tree.Nodes[0].First; c >= 0; c = tree.Nodes[c].Next {
		n := tree.Nodes[c]
		if n.Kind == jsonx.KindNumber {
			got[n.Key] = n.Lit
		}
	}
	if got["num_hidden_layers"] != "1e400" || got["vocab_size"] != digits {
		t.Fatalf("tree lits %#v", got)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	shard, _, err := fixture.WriteHeaderOnly("", []fixture.Tensor{{Name: "w", DType: "BF16", Shape: []int64{2}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "model.safetensors"), shard, 0o644); err != nil {
		t.Fatal(err)
	}

	text, stderr, code := capture(t, func() int {
		return run([]string{"inspect", "--path", dir})
	})
	if code != 0 && code != 1 {
		t.Fatalf("text exit %d\n%s", code, stderr)
	}
	js, stderr, code := capture(t, func() int {
		return run([]string{"inspect", "--json", "--path", dir})
	})
	if code != 0 && code != 1 {
		t.Fatalf("json exit %d\n%s", code, stderr)
	}
	for _, name := range []string{"text", "json"} {
		body := text
		if name == "json" {
			body = js
		}
		if strings.Contains(body, "parsing config.json") || strings.Contains(body, "ParseFloat") {
			t.Fatalf("%s rejected the document:\n%s", name, body[:min(500, len(body))])
		}
		if !strings.Contains(body, "1e400") || !strings.Contains(body, "out of range") {
			t.Fatalf("%s missing the out-of-range mark", name)
		}
	}
	if !strings.Contains(js, digits) {
		t.Fatal("json dropped the 100k-digit literal")
	}
	if !strings.Contains(js, `"present": true`) {
		t.Fatalf("json config not present:\n%s", js[:min(800, len(js))])
	}
	if strings.Contains(js, `"partial": true`) {
		t.Fatalf("json marked the inspection partial:\n%s", js[:min(800, len(js))])
	}
	// Warnings are clipped (fileerr) and the text summary wraps at 80 columns.
	// The text keeps a prefix of the integer and says it was cut. The JSON
	// field value is the full literal.
	flat := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\t' {
			return -1
		}
		return r
	}, text)
	if !strings.Contains(flat, digits[:80]) || !strings.Contains(text, "… (") {
		t.Fatalf("text dropped the clipped integer\n%s", text[:min(1200, len(text))])
	}
	if strings.Contains(flat, digits) {
		t.Fatal("text printed the full 100k-digit literal")
	}
}

// jsonxExportedSame is the same comparison the fuzz target uses, inlined
// here so this package does not depend on an unexported helper.
func jsonxExportedSame(v any, tree *jsonx.Tree) bool {
	if tree == nil || len(tree.Nodes) == 0 {
		return false
	}
	return walkSame(v, tree, 0)
}

func walkSame(v any, tree *jsonx.Tree, id int32) bool {
	n := tree.Nodes[id]
	switch x := v.(type) {
	case string:
		return n.Kind == jsonx.KindString && n.Lit == x
	case float64:
		return n.Kind == jsonx.KindNumber
	case map[string]any:
		if n.Kind != jsonx.KindObject || int(n.Len) != len(x) {
			return false
		}
		for c := n.First; c >= 0; c = tree.Nodes[c].Next {
			k := tree.Nodes[c].Key
			child, ok := x[k]
			if !ok || !walkSame(child, tree, c) {
				return false
			}
		}
		return true
	default:
		num, ok := x.(interface{ String() string })
		if !ok {
			return false
		}
		return n.Kind == jsonx.KindNumber && n.Lit == num.String()
	}
}
