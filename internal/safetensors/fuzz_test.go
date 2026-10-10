package safetensors

import (
	"bytes"
	"testing"
)

func FuzzParse(f *testing.F) {
	f.Add([]byte(`{"x":{"dtype":"F32","shape":[1],"data_offsets":[0,4]}}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"x":{"dtype":"U8","shape":[4],"data_offsets":[0,4]},"x":{"dtype":"U8","shape":[1],"data_offsets":[0,1]}}`))
	f.Add([]byte(`{"x":{"dtype":"F32","shape":[-1],"data_offsets":[0,4]}}`))
	// Go's minimizer tries every subset of a new input and does not publish
	// execs until that pass returns. The cost is quadratic in the length, so
	// a multi-kilobyte interesting input stalls the target at 0 execs/s.
	// Inputs past this cap are shortened to their prefix. The cap sits above
	// every seed. The assertions below are unchanged for every byte that is
	// actually parsed.
	const fuzzParseCap = 512
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > fuzzParseCap {
			data = data[:fuzzParseCap]
		}
		h, err := Parse(data)
		if err != nil || h == nil {
			return
		}
		if len(h.Tensors) > MaxTensors {
			t.Fatalf("accepted %d tensors", len(h.Tensors))
		}
		seen := map[string]struct{}{}
		for _, tensor := range h.Tensors {
			if _, ok := seen[tensor.Name]; ok {
				t.Fatalf("duplicate tensor %q", tensor.Name)
			}
			seen[tensor.Name] = struct{}{}
			if tensor.DType == "" {
				t.Fatalf("empty dtype on %q", tensor.Name)
			}
			if len(tensor.Shape) > MaxDims {
				t.Fatalf("%q rank %d", tensor.Name, len(tensor.Shape))
			}
			if _, err := tensor.CheckedElements(); err != nil {
				t.Fatalf("%q: %v", tensor.Name, err)
			}
		}
	})
}

func FuzzRead(f *testing.F) {
	f.Add([]byte{2, 0, 0, 0, 0, 0, 0, 0, '{', '}'})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			data = data[:1<<20]
		}
		h, err := Read(bytes.NewReader(data))
		if err != nil || h == nil {
			return
		}
		if h.HeaderLen > MaxHeaderLen {
			t.Fatalf("header len %d", h.HeaderLen)
		}
		if int64(len(data)) < 8 || h.HeaderLen > uint64(len(data)-8) {
			t.Fatalf("header len %d file %d", h.HeaderLen, len(data))
		}
	})
}

func FuzzDataOffsets(f *testing.F) {
	f.Add([]byte(`{"a":{"dtype":"U8","shape":[4],"data_offsets":[0,4]}}`), int64(4))
	f.Add([]byte(`{"a":{"dtype":"U8","shape":[4],"data_offsets":[100,104]}}`), int64(100000))
	f.Add([]byte(`{"a":{"dtype":"F32","shape":[1],"data_offsets":[0,4]},"b":{"dtype":"F32","shape":[1],"data_offsets":[2,6]}}`), int64(6))
	// The previous cap returned immediately past 64KiB. That branch is new
	// coverage, so the minimizer kept a 64KiB+1 input and then tried every
	// subset of it. Execs froze at 0/s for the rest of the run. 512 bytes
	// still covers the seeds (the longest is about 100 bytes) and every
	// header that is parsed still goes through Parse and CheckLayout.
	const fuzzOffsetsCap = 512
	f.Fuzz(func(t *testing.T, hdr []byte, dataLen int64) {
		if len(hdr) > fuzzOffsetsCap {
			hdr = hdr[:fuzzOffsetsCap]
		}
		h, err := Parse(hdr)
		if err != nil || h == nil {
			return
		}
		problems, warnings := CheckLayout("shard", h, dataLen)
		if problems == nil && warnings == nil {
			return
		}
	})
}
