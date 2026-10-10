package model

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"origin.cursor.com/cecilkootz/model-inspector/internal/fixture"
)

func TestDuplicateConfigKeyRejected(t *testing.T) {
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "config.json"), []byte(`{"model_type":"a","model_type":"b"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(d)
	if err != nil {
		t.Fatal(err)
	}
	if m.Config != nil {
		t.Fatalf("duplicate config key accepted: %#v", m.Config)
	}
	if !strings.Contains(strings.Join(m.Warnings, "\n"), "duplicate") {
		t.Fatalf("warnings = %v", m.Warnings)
	}
}

func TestTotalSizeOutOfRangeIsNotWrapped(t *testing.T) {
	d := t.TempDir()
	idx := []byte(`{"metadata":{"total_size":1e20},"weight_map":{"w":"a.safetensors"}}`)
	if err := os.WriteFile(filepath.Join(d, "model.safetensors.index.json"), idx, 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(d)
	if err != nil {
		t.Fatal(err)
	}
	if m.Index == nil {
		t.Fatalf("index rejected: %v", m.IndexErr)
	}
	if m.Index.TotalSize != 0 {
		t.Fatalf("total_size wrapped to %d", m.Index.TotalSize)
	}
	if !strings.Contains(strings.Join(m.Warnings, "\n"), "total_size") {
		t.Fatalf("warnings = %v", m.Warnings)
	}
}

func TestDisplayTotalsDoNotWrap(t *testing.T) {
	hdr := `{"a":{"dtype":"U8","shape":[9223372036854775807],"data_offsets":[0,9223372036854775807]},"b":{"dtype":"U8","shape":[2],"data_offsets":[0,2]}}`
	d := t.TempDir()
	body := fixture.EncodeHeader([]byte(hdr), []byte{0, 0})
	if err := os.WriteFile(filepath.Join(d, "model.safetensors"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(d)
	if err != nil {
		t.Fatal(err)
	}
	if m.TotalElements < 0 || m.TensorBytes < 0 || m.TotalLogicalParams < 0 {
		t.Fatalf("wrapped totals: elements=%d bytes=%d logical=%d", m.TotalElements, m.TensorBytes, m.TotalLogicalParams)
	}
	if m.TotalElements != math.MaxInt64 || m.TensorBytes != math.MaxInt64 {
		t.Fatalf("elements=%d bytes=%d", m.TotalElements, m.TensorBytes)
	}
	if !m.Partial {
		t.Fatal("overflow did not mark the inspection partial")
	}
	if !strings.Contains(strings.Join(m.Warnings, "\n"), "overflowed") {
		t.Fatalf("warnings = %v", m.Warnings)
	}
}
