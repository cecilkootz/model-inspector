package jsonx

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"origin.cursor.com/cecilkootz/model-inspector/internal/fileerr"
)

func TestUnmarshalValues(t *testing.T) {
	v, err := Unmarshal([]byte(`{"s":"a","n":1,"b":true,"z":null,"a":[1,{"k":"v"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%T", v)
	}
	if obj["s"] != "a" || obj["n"] != float64(1) || obj["b"] != true || obj["z"] != nil {
		t.Fatalf("%#v", obj)
	}
	arr, ok := obj["a"].([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("%#v", obj["a"])
	}
}

func TestUnmarshalRejectsDuplicateKeys(t *testing.T) {
	for _, js := range []string{
		`{"a":1,"a":2}`,
		`{"outer":{"a":1,"a":2}}`,
		`[{"a":1,"a":2}]`,
	} {
		if _, err := Unmarshal([]byte(js)); err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("%s: %v", js, err)
		}
	}
}

func TestUnderflowToZero(t *testing.T) {
	keep := []string{"1e-400", "-1e-400", "10e-400", "1e-324", "1e400"}
	zero := []string{"0e-400", "-0e-400", "0", "-0", "0.0", "0.00e-10"}
	finite := []string{"1.5", "5e-324", "1e-323", "1"}
	for _, lit := range keep {
		v, err := Unmarshal([]byte(lit))
		if err != nil {
			t.Fatalf("%s: %v", lit, err)
		}
		n, ok := v.(json.Number)
		if !ok || n.String() != lit {
			t.Fatalf("%s: got %#v, want json.Number", lit, v)
		}
		if _, ok := FitFloat(v); ok {
			t.Fatalf("%s fits float64", lit)
		}
		if _, ok := NumberFits(lit); ok {
			t.Fatalf("%s NumberFits", lit)
		}
		tree, err := ParseTree([]byte(lit), Limits{})
		if err != nil || tree.Nodes[0].Kind != KindNumber || tree.Nodes[0].Lit != lit {
			t.Fatalf("%s tree %v", lit, err)
		}
		if !sameValue(v, tree, 0) {
			t.Fatalf("%s disagree", lit)
		}
	}
	for _, lit := range zero {
		v, err := Unmarshal([]byte(lit))
		if err != nil {
			t.Fatalf("%s: %v", lit, err)
		}
		f, ok := v.(float64)
		if !ok || f != 0 {
			t.Fatalf("%s: got %#v, want 0", lit, v)
		}
		if _, ok := FitFloat(v); !ok {
			t.Fatalf("%s rejected", lit)
		}
		if g, ok := FitFloat(json.Number(lit)); !ok || g != 0 {
			t.Fatalf("%s number fit %#v %v", lit, g, ok)
		}
		if g, ok := NumberFits(lit); !ok || g != 0 {
			t.Fatalf("%s NumberFits %#v %v", lit, g, ok)
		}
	}
	for _, lit := range finite {
		v, err := Unmarshal([]byte(lit))
		if err != nil {
			t.Fatalf("%s: %v", lit, err)
		}
		f, ok := v.(float64)
		if !ok || f == 0 {
			t.Fatalf("%s: got %#v, want a non-zero float", lit, v)
		}
		if _, ok := FitFloat(v); !ok {
			t.Fatalf("%s rejected", lit)
		}
	}
}

func TestUnmarshalRejectsTrailingDataAndDepth(t *testing.T) {
	if _, err := Unmarshal([]byte(`{"a":1} {}`)); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("trailing: %v", err)
	}
	nested := strings.Repeat("[", MaxDepth+1) + strings.Repeat("]", MaxDepth+1)
	if _, err := Unmarshal([]byte(nested)); err == nil || !strings.Contains(err.Error(), "nesting") {
		t.Fatalf("depth: %v", err)
	}
	ok := strings.Repeat("[", MaxDepth) + "1" + strings.Repeat("]", MaxDepth)
	if _, err := Unmarshal([]byte(ok)); err != nil {
		t.Fatalf("depth %d rejected: %v", MaxDepth, err)
	}
}

func TestUnmarshalKeepsHugeNumber(t *testing.T) {
	const tail = "9876543210"
	digits := strings.Repeat("1", 8000) + tail
	v, err := Unmarshal([]byte(digits))
	if err != nil {
		t.Fatal(err)
	}
	n, ok := v.(json.Number)
	if !ok || n.String() != digits {
		t.Fatalf("got %#v", v)
	}
	if _, ok := FitFloat(v); ok {
		t.Fatal("huge integer fit in float64")
	}
	// The value keeps the literal. Text that prints it is clipped, so the
	// tail of an untrusted number cannot land in a warning.
	msg := fileerr.String("out of range: " + digits)
	if utf8.RuneCountInString(msg) > fileerr.MaxRunes {
		t.Fatalf("runes %d", utf8.RuneCountInString(msg))
	}
	if strings.Contains(msg, tail) {
		t.Fatalf("literal survived: %q", msg)
	}
	if !strings.Contains(msg, "… (") || !strings.HasSuffix(msg, " runes)") {
		t.Fatal(msg)
	}
}
