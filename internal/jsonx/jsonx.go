// Package jsonx unmarshals JSON the way encoding/json does for a generic
// value, and rejects duplicate object keys and excessive nesting.
package jsonx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// MaxDepth is the deepest array/object nesting Unmarshal accepts.
const MaxDepth = 32

// Unmarshal decodes one JSON value. Objects become map[string]any, arrays
// []any, and numbers float64. A second occurrence of an object key is an error.
func Unmarshal(data []byte) (any, error) {
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
		if err != nil {
			return nil, fmt.Errorf("number %s: %w", t, err)
		}
		return f, nil
	default:
		return nil, fmt.Errorf("unexpected JSON token %T", tok)
	}
}

func readObject(dec *json.Decoder, depth int) (map[string]any, error) {
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
