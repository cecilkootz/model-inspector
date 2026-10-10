package jsonx

import (
	"strings"
	"testing"
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
