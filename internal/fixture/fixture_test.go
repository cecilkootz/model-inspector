package fixture

import (
	"bytes"
	"testing"

	"github.com/mwelch/model-inspector/internal/safetensors"
)

func TestWriteShardRoundTrip(t *testing.T) {
	tensors := []Tensor{
		{Name: "language_model.model.norm.weight", DType: "BF16", Shape: []int64{4}},
		{Name: "language_model.model.embed_tokens.weight", DType: "U32", Shape: []int64{1, 4}},
	}
	a, n, err := WriteShard("mlx", tensors)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := WriteShard("mlx", tensors)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("WriteShard is not deterministic")
	}
	h, err := safetensors.Read(bytes.NewReader(a))
	if err != nil {
		t.Fatal(err)
	}
	if h.HeaderLen%8 != 0 {
		t.Errorf("header length %d is not a multiple of 8", h.HeaderLen)
	}
	if got, want := int64(len(a)), h.DataStart()+n; got != want {
		t.Errorf("file size %d, data starts at %d and spans %d", got, h.DataStart(), n)
	}
	if h.Metadata["format"] != "mlx" {
		t.Errorf("format = %q", h.Metadata["format"])
	}
	if len(h.Tensors) != 2 || h.Tensors[0].Name != "language_model.model.embed_tokens.weight" {
		t.Fatalf("tensors = %+v", h.Tensors)
	}
	emb := h.Tensors[0]
	if emb.DType != "U32" || emb.NumElements() != 4 || emb.ByteLen() != 16 || emb.DataOffsets != [2]int64{8, 24} {
		t.Errorf("embed = %+v bytes=%d", emb, emb.ByteLen())
	}
	// norm is listed first in the writer, so its range precedes the embed.
	norm := h.Tensors[1]
	if norm.ByteLen() != 8 || norm.DataOffsets != [2]int64{0, 8} {
		t.Errorf("norm = %+v bytes=%d", norm, norm.ByteLen())
	}
}

func TestTinyQwenIsSmallAndStable(t *testing.T) {
	files, tensorBytes, err := TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	again, againBytes, err := TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	if tensorBytes != againBytes {
		t.Fatalf("tensor bytes %d vs %d", tensorBytes, againBytes)
	}
	if tensorBytes == 0 {
		t.Fatal("tensor payload is empty")
	}
	var total int
	for i, f := range files {
		if !bytes.Equal(f.Data, again[i].Data) || f.Name != again[i].Name {
			t.Fatalf("file %s is not stable", f.Name)
		}
		if len(f.Data) > 16<<10 {
			t.Errorf("%s is %d bytes; fixtures must stay in the kilobytes", f.Name, len(f.Data))
		}
		total += len(f.Data)
		t.Logf("fixture %s: %d bytes", f.Name, len(f.Data))
	}
	t.Logf("fixture total: %d bytes; tensor payload: %d bytes", total, tensorBytes)
}
