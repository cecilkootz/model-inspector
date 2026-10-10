package jsonx

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
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
	f.Add([]byte(`1e400`))
	f.Add([]byte(`1e700`))
	f.Add([]byte(`{"huge":1e400,"tiny":1e-400}`))
	// A few hundred digits still overflow float64. The 100k-digit case is a
	// unit test; keeping it in the corpus makes every mutation parse it.
	f.Add([]byte(strings.Repeat("9", 400)))
	f.Add([]byte("\"0000000\x9d\""))
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
		// NaN and Infinity stay in the tree as error nodes. Unmarshal still
		// rejects those tokens, so the accept bits differ on purpose.
		if e2 == nil && hasKind(tree, KindError) {
			if e1 == nil {
				t.Fatal("unmarshal accepted a non-finite token")
			}
			return
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
	case json.Number:
		return n.Kind == KindNumber && n.Lit == x.String()
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

func hasKind(tree *Tree, k Kind) bool {
	if tree == nil {
		return false
	}
	for i := range tree.Nodes {
		if tree.Nodes[i].Kind == k {
			return true
		}
	}
	return false
}

func TestInvalidUTF8StringAgrees(t *testing.T) {
	js := []byte("\"0000000\x9d\"")
	v, e1 := Unmarshal(js)
	tree, e2 := ParseTree(js, Limits{})
	if e1 != nil || e2 != nil {
		t.Fatalf("u=%v t=%v", e1, e2)
	}
	if !sameValue(v, tree, 0) {
		t.Fatalf("unmarshal %q tree %q", v, tree.Nodes[0].Lit)
	}
}

func TestOutOfRangeNumberKept(t *testing.T) {
	digits := strings.Repeat("9", 100_000)
	for _, js := range []string{
		`{"huge":1e400}`,
		`1e700`,
		`{"digits":` + digits + `}`,
		`{"huge":1e400,"tiny":1e-400,"neg0":-0}`,
	} {
		v, e1 := Unmarshal([]byte(js))
		tree, e2 := ParseTree([]byte(js), Limits{})
		if e1 != nil || e2 != nil {
			t.Fatalf("%s… u=%v t=%v", js[:min(24, len(js))], e1, e2)
		}
		if !sameValue(v, tree, 0) {
			t.Fatalf("values differ for %s…", js[:min(24, len(js))])
		}
	}
}

func TestNaNIsErrorNode(t *testing.T) {
	cases := []string{
		`{"a":1,"b":NaN,"c":Infinity,"d":-Infinity,"e":2}`,
		`{"s":"a\nb","n":NaN}`,
	}
	for _, js := range cases {
		tree, err := ParseTree([]byte(js), Limits{})
		if err != nil {
			t.Fatalf("%s: %v", js, err)
		}
		if !hasKind(tree, KindError) {
			t.Fatalf("%s: no error node", js)
		}
		if _, err := Unmarshal([]byte(js)); err == nil {
			t.Fatalf("%s: unmarshal accepted NaN", js)
		}
		var lits []string
		for _, n := range tree.Nodes {
			if n.Kind == KindError {
				lits = append(lits, n.Lit)
			}
		}
		if js == cases[0] && strings.Join(lits, ",") != "NaN,Infinity,-Infinity" {
			t.Fatalf("lits %v", lits)
		}
		// The neighbours of the bad token are still in the tree.
		if js == cases[0] {
			if tree.Nodes[0].Kind != KindObject || tree.Nodes[0].Len != 5 {
				t.Fatalf("root %+v", tree.Nodes[0])
			}
		}
	}
	// A digit glued to NaN is still a syntax error, not a rewritten number.
	if _, err := ParseTree([]byte(`1NaN`), Limits{}); err == nil {
		t.Fatal("1NaN accepted")
	}
}

func TestHtabFullDoesNotSpin(t *testing.T) {
	h := &htab{max: 4}
	h.slot = make([]slot, 4)
	h.mask = 3
	// n is below the length so add enters the probe. Every slot is occupied,
	// which is the state that used to spin.
	h.n = 3
	for i := range h.slot {
		h.slot[i] = slot{hash: uint64(i + 1), off: uint32(i), n: 1}
	}
	raw := []byte("abcd")
	done := make(chan struct{})
	go func() {
		_, _ = h.add(raw, 0, 2)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("htab probe did not finish")
	}
	if _, err := h.add(raw, 0, 2); err == nil {
		t.Fatal("full table returned no error")
	}
}
