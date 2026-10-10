package jsonx

import (
	"encoding/json"
	"strings"
	"testing"
	"unsafe"
)

func TestParseTreeAgreesWithUnmarshal(t *testing.T) {
	cases := []string{
		`{"s":"a","n":1,"b":true,"z":null,"a":[1,{"k":"v"}]}`,
		`{"a":1,"a":2}`,
		`{"outer":{"a":1,"a":2}}`,
		`[{"a":1,"a":2}]`,
		`{"a":1} {}`,
		strings.Repeat("[", MaxDepth+1) + strings.Repeat("]", MaxDepth+1),
		strings.Repeat("[", MaxDepth) + "1" + strings.Repeat("]", MaxDepth),
		`1e-06`,
		`9007199254740993`,
		`{"b":1,"a":2}`,
		`[]`,
		`{}`,
		`null`,
		`"x"`,
		`true`,
		`false`,
		`0.25`,
		`{"k":""," ":1,"a.b":2,"q\"u":3}`,
	}
	for _, js := range cases {
		v, e1 := Unmarshal([]byte(js))
		tree, e2 := ParseTree([]byte(js), Limits{})
		if (e1 == nil) != (e2 == nil) {
			t.Fatalf("%s:\nunmarshal %v\ntree %v", js, e1, e2)
		}
		if e1 != nil {
			if e1.Error() != e2.Error() {
				t.Fatalf("%s:\nunmarshal %q\ntree %q", js, e1.Error(), e2.Error())
			}
			continue
		}
		if !sameValue(v, tree, 0) {
			t.Fatalf("%s values differ\n%#v", js, tree.Nodes)
		}
	}
}

func TestParseTreeOrderAndLiterals(t *testing.T) {
	raw := []byte(`{"b":1,"a":1e-06,"c":9007199254740993}`)
	tree, err := ParseTree(raw, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	keys := childKeys(tree, 0)
	if strings.Join(keys, ",") != "b,a,c" {
		t.Fatalf("order %v", keys)
	}
	lits := map[string]string{}
	for id := tree.Nodes[0].First; id >= 0; id = tree.Nodes[id].Next {
		lits[tree.Nodes[id].Key] = tree.Nodes[id].Lit
	}
	if lits["a"] != "1e-06" || lits["c"] != "9007199254740993" || lits["b"] != "1" {
		t.Fatalf("lits %#v", lits)
	}
	if tree.Nodes[0].Depth != 0 || tree.Nodes[tree.Nodes[0].First].Depth != 1 {
		t.Fatalf("depth root %d child %d", tree.Nodes[0].Depth, tree.Nodes[tree.Nodes[0].First].Depth)
	}
}

func TestParseTreeOffsets(t *testing.T) {
	raw := []byte("{\n  \"n\": 10,\n  \"s\": \"hi\"\n}\n")
	tree, err := ParseTree(raw, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	for id := tree.Nodes[0].First; id >= 0; id = tree.Nodes[id].Next {
		n := tree.Nodes[id]
		off := int(n.Off)
		if off < 0 || off >= len(raw) {
			t.Fatalf("%s off %d", n.Key, off)
		}
		switch n.Kind {
		case KindNumber:
			if !strings.HasPrefix(string(raw[off:]), n.Lit) {
				t.Fatalf("number at %d %q", off, raw[off:])
			}
		case KindString:
			if raw[off] != '"' {
				t.Fatalf("string at %d %q", off, raw[off:])
			}
		}
	}
	_, err = ParseTree([]byte("{\n\"a\":1,\n}\n"), Limits{})
	loc, ok := err.(*LocatedError)
	if !ok {
		t.Fatalf("%T %v", err, err)
	}
	if loc.Off <= 0 {
		t.Fatalf("offset %d", loc.Off)
	}
}

func TestParseTreeStringCap(t *testing.T) {
	body := strings.Repeat("x", 100)
	raw := []byte(`{"s":"` + body + `"}`)
	tree, err := ParseTree(raw, Limits{MaxString: 16})
	if err != nil {
		t.Fatal(err)
	}
	var s Node
	for id := tree.Nodes[0].First; id >= 0; id = tree.Nodes[id].Next {
		s = tree.Nodes[id]
	}
	if s.Lit != strings.Repeat("x", 16) || s.Len != 100 {
		t.Fatalf("lit %q len %d", s.Lit, s.Len)
	}
	if tree.FullBytes[tree.Nodes[0].First] != 100 {
		t.Fatalf("full %v", tree.FullBytes)
	}
}

func TestParseTreeNodeLimit(t *testing.T) {
	raw := []byte("[" + strings.Repeat("1,", 9) + "1]")
	_, err := ParseTree(raw, Limits{MaxNodes: 5})
	lim, ok := err.(*NodeLimitError)
	if !ok {
		t.Fatalf("%T %v", err, err)
	}
	if lim.Nodes != 11 || lim.Limit != 5 {
		t.Fatalf("%+v", lim)
	}
	if _, err := Unmarshal(raw); err != nil {
		t.Fatal(err)
	}
}

func TestNodeStructBudget(t *testing.T) {
	if unsafe.Sizeof(Node{}) > 80 {
		t.Fatalf("node is %d bytes", unsafe.Sizeof(Node{}))
	}
}

func TestFuzzAgreesSmall(t *testing.T) {
	// A few hand-picked fuzz seeds the fuzzer also starts from.
	for _, s := range []string{"", " ", "{", "[", "]", "1 2", `{"a":1,"b":[true,false,null]}`} {
		v, e1 := Unmarshal([]byte(s))
		tree, e2 := ParseTree([]byte(s), Limits{})
		if (e1 == nil) != (e2 == nil) {
			t.Fatalf("%q u=%v t=%v", s, e1, e2)
		}
		if e1 == nil && !sameValue(v, tree, 0) {
			t.Fatalf("%q values", s)
		}
	}
}

func FuzzParseTree(f *testing.F) {
	f.Add([]byte(`{"a":1}`))
	f.Add([]byte(`{"a":1,"a":2}`))
	f.Add([]byte(`[1,2,3]`))
	f.Add([]byte(`"hi"`))
	f.Add([]byte(`1e-06`))
	f.Add([]byte(``))
	f.Add([]byte(`{"b":1,"a":2}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			data = data[:1<<16]
		}
		v, e1 := Unmarshal(data)
		tree, e2 := ParseTree(data, Limits{MaxNodes: 10_000, MaxString: 4096})
		if e2 != nil {
			if _, ok := e2.(*NodeLimitError); ok && e1 == nil {
				return
			}
		}
		if (e1 == nil) != (e2 == nil) {
			t.Fatalf("accept u=%v t=%v", e1, e2)
		}
		if e1 != nil {
			return
		}
		if !sameValue(v, tree, 0) {
			t.Fatal("values differ")
		}
	})
}

func childKeys(tree *Tree, id int32) []string {
	var out []string
	for c := tree.Nodes[id].First; c >= 0; c = tree.Nodes[c].Next {
		out = append(out, tree.Nodes[c].Key)
	}
	return out
}

func sameValue(v any, tree *Tree, id int32) bool {
	if tree == nil || id < 0 || int(id) >= len(tree.Nodes) {
		return false
	}
	n := tree.Nodes[id]
	switch x := v.(type) {
	case nil:
		return n.Kind == KindNull
	case bool:
		return n.Kind == KindBool && ((x && n.Lit == "true") || (!x && n.Lit == "false"))
	case string:
		if n.Kind != KindString {
			return false
		}
		if full, ok := tree.FullBytes[id]; ok && full > len(n.Lit) {
			return strings.HasPrefix(x, n.Lit)
		}
		return x == n.Lit
	case float64:
		if n.Kind != KindNumber {
			return false
		}
		f, err := parseFloat(n.Lit)
		return err == nil && f == x
	case map[string]any:
		if n.Kind != KindObject || int(n.Len) != len(x) {
			return false
		}
		seen := map[string]bool{}
		for c := n.First; c >= 0; c = tree.Nodes[c].Next {
			k := tree.Nodes[c].Key
			if seen[k] {
				return false
			}
			seen[k] = true
			child, ok := x[k]
			if !ok || !sameValue(child, tree, c) {
				return false
			}
		}
		return len(seen) == len(x)
	case []any:
		if n.Kind != KindArray || int(n.Len) != len(x) {
			return false
		}
		i := 0
		for c := n.First; c >= 0; c = tree.Nodes[c].Next {
			if i >= len(x) || !sameValue(x[i], tree, c) {
				return false
			}
			i++
		}
		return i == len(x)
	default:
		return false
	}
}

func parseFloat(s string) (float64, error) {
	return json.Number(s).Float64()
}
