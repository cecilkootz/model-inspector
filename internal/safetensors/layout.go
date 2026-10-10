package safetensors

import (
	"fmt"
	"sort"
)

// CheckLayout reports integrity problems and warnings for one shard's data
// section. dataLen is the number of bytes after the header.
//
// Problems (fail the scan): reversed offsets, ranges past the data section,
// dtype×shape size mismatches, overlaps, gaps, and trailing bytes. The
// covered ranges must start at 0 and end exactly at dataLen.
// Warnings: a dtype this package does not know the size of.
func CheckLayout(shard string, h *Header, dataLen int64) (problems, warnings []string) {
	if h == nil {
		return []string{fmt.Sprintf("%s: missing header", shard)}, nil
	}
	if dataLen < 0 {
		fileSize := h.DataStart() + dataLen
		return []string{fmt.Sprintf("%s: header (%d bytes) runs past end of file (%d bytes)", shard, h.HeaderLen, fileSize)}, nil
	}
	if len(h.Tensors) == 0 {
		if dataLen != 0 {
			problems = append(problems, fmt.Sprintf("%s: data section is %d bytes but header has no tensors", shard, dataLen))
		}
		return problems, nil
	}
	ts := append([]TensorInfo(nil), h.Tensors...)
	sort.Slice(ts, func(i, j int) bool {
		if ts[i].DataOffsets[0] == ts[j].DataOffsets[0] {
			return ts[i].Name < ts[j].Name
		}
		return ts[i].DataOffsets[0] < ts[j].DataOffsets[0]
	})
	var prevEnd int64
	var prevName string
	covered := false
	for _, t := range ts {
		start, end := t.DataOffsets[0], t.DataOffsets[1]
		if start < 0 || end < start {
			problems = append(problems, fmt.Sprintf("%s/%s: invalid offsets [%d,%d]", shard, t.Name, start, end))
			continue
		}
		if end > dataLen {
			problems = append(problems, fmt.Sprintf("%s/%s: data ends at %d, beyond %d-byte data section", shard, t.Name, end, dataLen))
		}
		if size, ok := DTypeSize(t.DType); !ok {
			warnings = append(warnings, fmt.Sprintf("%s/%s: unknown dtype %s", shard, t.Name, t.DType))
		} else if elems, err := t.CheckedElements(); err != nil {
			problems = append(problems, fmt.Sprintf("%s/%s: %v", shard, t.Name, err))
		} else if need, err := mulInt64(elems, size); err != nil {
			problems = append(problems, fmt.Sprintf("%s/%s: byte size overflows", shard, t.Name))
		} else if need != end-start {
			problems = append(problems, fmt.Sprintf("%s/%s: %s%v needs %d bytes, range is %d", shard, t.Name, t.DType, t.Shape, need, end-start))
		}
		switch {
		case !covered && start != 0:
			problems = append(problems, fmt.Sprintf("%s/%s: data starts at %d, want 0", shard, t.Name, start))
		case covered && start > prevEnd:
			problems = append(problems, fmt.Sprintf("%s/%s: gap before [%d,%d]", shard, t.Name, start, end))
		case covered && start < prevEnd:
			problems = append(problems, fmt.Sprintf("%s/%s overlaps %s", shard, t.Name, prevName))
		}
		if !covered || end > prevEnd {
			prevEnd, prevName = end, t.Name
		}
		covered = true
	}
	if covered && prevEnd < dataLen {
		problems = append(problems, fmt.Sprintf("%s: data ranges end at %d but data section is %d bytes", shard, prevEnd, dataLen))
	}
	return problems, warnings
}
