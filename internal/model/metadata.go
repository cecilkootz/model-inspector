package model

import (
	"encoding/json"
	"math"

	"github.com/cecilkootz/model-inspector/internal/jsonx"
)

// ShapeComparison is one config field compared with a tensor-derived value.
// Path and What are program text; Want comes from the config.
type ShapeComparison struct {
	Path  string
	Want  int64
	What  string
	Got   int64
	Match bool
}

// ShapeReport is the metadata-sanity input. Invalid lists config fields
// whose values cannot be dimensions. Skipped lists comparisons that were
// possible in principle but not made, with the reason.
type ShapeReport struct {
	Comparisons []ShapeComparison
	Invalid     []string
	Skipped     []string
}

// Nested text scopes come first: in a multimodal config they describe the
// decoder whose tensors are compared, and root fields may belong elsewhere.
var textScopes = []string{"text_config", "llm_config", "language_config"}

// ConfigShapes compares a few config fields with tensor shapes:
// hidden_size with the token embedding width, vocab_size with the token
// embedding and lm_head rows, and num_hidden_layers with the distinct
// decoder layer indices. MLX packed weights are compared at their
// dequantized width. An integer-dtype tensor with no scales is packed in a
// way this tool cannot undo, so it is not compared. It runs in O(tensors)
// and does not change the model.
func (m *Model) ConfigShapes() ShapeReport {
	var r ShapeReport
	if m == nil || m.Config == nil {
		return r
	}
	embed := m.pickTable(func(t *Tensor) bool {
		return t.Kind == KindEmbeddings && tokenEmbedSegment(t.Name)
	}, "token embedding", &r)
	head := m.pickTable(func(t *Tensor) bool {
		if t.Kind != KindLMHead {
			return false
		}
		seg := foldASCII(lastSegment(stripWeightSuffix(t.Name)))
		return seg == "lm_head" || seg == "output"
	}, "lm_head", &r)

	if embed.ok && embed.widthOK {
		// OPT-style models project a narrower embedding up to hidden_size.
		key := "hidden_size"
		for _, alt := range []string{"word_embed_proj_dim", "embedding_size"} {
			if _, _, ok := m.textField(alt); ok {
				key = alt
				break
			}
		}
		what := "token embedding width"
		if embed.dequant {
			what += " (dequantized)"
		}
		m.compare(&r, key, what, embed.width, nil)
	}
	if embed.ok {
		m.compare(&r, "vocab_size", "token embedding rows", embed.rows, nil)
	}
	if head.ok {
		m.compare(&r, "vocab_size", "lm_head rows", head.rows, nil)
	}
	if layers := m.decoderLayers(); layers > 0 {
		if _, _, ok := m.textField("num_hidden_layers"); ok {
			if m.unreadShards() {
				r.Skipped = append(r.Skipped, "num_hidden_layers not compared: some shards were not read")
			} else {
				m.compare(&r, "num_hidden_layers", "decoder layers in tensor names", int64(layers), m.mtpInStack)
			}
		}
	}
	return r
}

type table struct {
	rows, width      int64
	ok, widthOK      bool
	dequant          bool
	disagree, packed bool
}

// pickTable finds the rank-2 tensors that match and agree on rows and width.
// A tensor with one row is not a table (tiny fixtures and 1-row packs).
func (m *Model) pickTable(match func(*Tensor) bool, label string, r *ShapeReport) table {
	var out table
	for _, t := range m.Tensors {
		if t == nil || (t.Role != RolePlain && t.Role != RoleQuantWeight) || !match(t) {
			continue
		}
		if len(t.Shape) != 2 || t.Shape[0] <= 1 {
			continue
		}
		if t.Role == RolePlain && storageBits(t.DType) != 0 {
			out.packed = true
			continue
		}
		rows, width, widthOK := t.Shape[0], t.Shape[1], true
		dequant := t.Role == RoleQuantWeight
		if dequant {
			widthOK = t.LogicalParams > 0 && t.LogicalParams%rows == 0
			width = 0
			if widthOK {
				width = t.LogicalParams / rows
			}
		}
		if !out.ok {
			out = table{rows: rows, width: width, ok: true, widthOK: widthOK, dequant: dequant, packed: out.packed}
			continue
		}
		if rows != out.rows || widthOK != out.widthOK || width != out.width {
			out.disagree = true
		}
	}
	switch {
	case out.disagree:
		r.Skipped = append(r.Skipped, label+" not compared: tensors disagree on shape")
		return table{}
	case out.packed && !out.ok:
		r.Skipped = append(r.Skipped, label+" not compared: integer dtype with no scales (packed)")
	}
	return out
}

// compare records one comparison. accept may widen what counts as a match.
func (m *Model) compare(r *ShapeReport, key, what string, got int64, accept func(want, got int64) bool) {
	path, v, ok := m.textField(key)
	if !ok {
		return
	}
	want, problem := wholeNumber(v)
	if problem != "" {
		for _, s := range r.Invalid {
			if s == path+problem {
				return
			}
		}
		r.Invalid = append(r.Invalid, path+problem)
		return
	}
	match := want == got || (accept != nil && accept(want, got))
	r.Comparisons = append(r.Comparisons, ShapeComparison{Path: path, Want: want, What: what, Got: got, Match: match})
}

// mtpInStack accepts DeepSeek-style checkpoints that store the MTP module
// as decoder layer N, after the num_hidden_layers it extends.
func (m *Model) mtpInStack(want, got int64) bool {
	_, v, ok := m.textField("num_nextn_predict_layers")
	if !ok {
		return false
	}
	n, problem := wholeNumber(v)
	if problem != "" || n <= 0 || want > math.MaxInt64-n {
		return false
	}
	return got == want+n
}

func (m *Model) textField(key string) (path string, v any, ok bool) {
	for _, s := range textScopes {
		if sub := subMap(m.Config, s); sub != nil {
			if v, ok := sub[key]; ok && v != nil {
				return s + "." + key, v, true
			}
		}
	}
	if v, ok := m.Config[key]; ok && v != nil {
		return key, v, true
	}
	return "", nil, false
}

// decoderLayers counts distinct decoder indices. Groups are keyed by index,
// and the overflow group has no index.
func (m *Model) decoderLayers() int {
	n := 0
	for _, g := range m.Groups {
		if g != nil && g.Kind == KindLayer && g.Layer >= 0 {
			n++
		}
	}
	return n
}

func (m *Model) unreadShards() bool {
	for _, s := range m.Shards {
		if s == nil || s.Header == nil {
			return true
		}
	}
	return false
}

// Config numbers arrive as float64; at 2^53 and above, integers stop being exact.
const maxWholeFloat = 1 << 53

// wholeNumber reads a config value as a non-negative dimension. problem is
// program text starting with a space, or "" when n is usable.
func wholeNumber(v any) (n int64, problem string) {
	switch x := v.(type) {
	case json.Number, float64:
		// The loader keeps a literal as json.Number only when it does not
		// fit in float64.
		f, ok := jsonx.FitFloat(x)
		if !ok {
			return 0, " is out of range"
		}
		return wholeFloat(f)
	case int:
		return nonNegative(int64(x))
	case int64:
		return nonNegative(x)
	default:
		return 0, " is not a number"
	}
}

func wholeFloat(f float64) (int64, string) {
	switch {
	case f != math.Trunc(f):
		return 0, " is not a whole number"
	case f < 0:
		return 0, " is negative"
	case f >= maxWholeFloat:
		return 0, " is out of range"
	}
	return int64(f), ""
}

func nonNegative(i int64) (int64, string) {
	if i < 0 {
		return 0, " is negative"
	}
	return i, ""
}
