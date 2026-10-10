package jsonx

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

func TestParseTreeStringLifetime(t *testing.T) {
	src := []byte(`{"keep":"alive-please-xxxxx","n":1}`)
	buf := append([]byte(nil), src...)
	tree, err := ParseTree(buf, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	buf = nil
	src = nil
	runtime.GC()
	runtime.GC()
	n := tree.Nodes[tree.Nodes[0].First]
	if n.Key != "keep" || n.Lit != "alive-please-xxxxx" {
		t.Fatalf("aliased text did not survive GC: %#v", n)
	}
}

func TestParseTreeHashDuplicate(t *testing.T) {
	var b strings.Builder
	b.WriteByte('{')
	for i := 0; i < 30; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"k%02d":%d`, i, i)
	}
	b.WriteString(`,"k25":1}`)
	_, err := ParseTree([]byte(b.String()), Limits{})
	if err == nil || !strings.Contains(err.Error(), `duplicate key "k25"`) {
		t.Fatalf("got %v", err)
	}
	// The same text from Unmarshal, so the hash-table path does not drift.
	_, uerr := Unmarshal([]byte(b.String()))
	if uerr == nil || uerr.Error() != err.Error() {
		t.Fatalf("unmarshal %v\nscan %v", uerr, err)
	}
}
