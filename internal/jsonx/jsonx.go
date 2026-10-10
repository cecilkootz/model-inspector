// Package jsonx unmarshals JSON the way encoding/json does for a generic
// value, and rejects duplicate object keys and excessive nesting.
package jsonx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"

	"origin.cursor.com/cecilkootz/model-inspector/internal/fileerr"
)

// MaxDepth is the deepest array/object nesting Unmarshal accepts.
const MaxDepth = 32

// Unmarshal decodes one JSON value. Objects become map[string]any, arrays
// []any, and numbers float64 when they fit. A number whose literal does not
// fit in float64 is kept as json.Number so the document still parses; callers
// that need a float must treat that as out of range. A second occurrence of
// an object key is an error.
func Unmarshal(data []byte) (any, error) {
	v, err := unmarshal(data)
	return v, fileerr.Error(err)
}

func unmarshal(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := readValue(dec, 0)
	if err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("trailing data after JSON value")
		}
		return nil, err
	}
	return v, nil
}

func readValue(dec *json.Decoder, depth int) (any, error) {
	if depth > MaxDepth {
		return nil, fmt.Errorf("JSON nesting exceeds %d", MaxDepth)
	}
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			return readObject(dec, depth+1)
		case '[':
			return readArray(dec, depth+1)
		default:
			return nil, fmt.Errorf("unexpected delimiter %q", t)
		}
	case nil, string, bool:
		return t, nil
	case json.Number:
		f, err := t.Float64()
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			// Keep the literal. Rejecting it dropped the rest of the config
			// while the tree still showed the number.
			return t, nil
		}
		return f, nil
	default:
		return nil, fmt.Errorf("unexpected JSON token %T", tok)
	}
}

func readObject(dec *json.Decoder, depth int) (map[string]any, error) {
	if depth > MaxDepth {
		return nil, fmt.Errorf("JSON nesting exceeds %d", MaxDepth)
	}
	m := make(map[string]any)
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("expected string object key")
		}
		if _, exists := m[key]; exists {
			return nil, fmt.Errorf("duplicate key %q", key)
		}
		val, err := readValue(dec, depth)
		if err != nil {
			return nil, err
		}
		m[key] = val
	}
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '}' {
		return nil, fmt.Errorf("expected end of object")
	}
	return m, nil
}

func readArray(dec *json.Decoder, depth int) ([]any, error) {
	if depth > MaxDepth {
		return nil, fmt.Errorf("JSON nesting exceeds %d", MaxDepth)
	}
	var a []any
	for dec.More() {
		val, err := readValue(dec, depth)
		if err != nil {
			return nil, err
		}
		a = append(a, val)
	}
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != ']' {
		return nil, fmt.Errorf("expected end of array")
	}
	return a, nil
}

// FitFloat reports whether v is a finite float64. json.Number is accepted
// only when Float64 succeeds and the result is finite. A failure means the
// literal is out of range and must not be used as a magnitude.
func FitFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, false
		}
		return n, true
	case json.Number:
		f, err := n.Float64()
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return f, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
