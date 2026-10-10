// Package report builds the schema_version 1 document and the text summary.
package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
	"origin.cursor.com/cecilkootz/model-inspector/internal/sanitize"
	"origin.cursor.com/cecilkootz/model-inspector/internal/scan"
	"origin.cursor.com/cecilkootz/model-inspector/internal/version"
)

const paramsNote = "packed quantized weights counted at dequantized size; quantization scales/biases excluded"

// Document is the frozen v1 JSON object. Nil pointers encode as null.
// Slices are empty rather than null. ElapsedMS is not deterministic.
type Document struct {
	SchemaVersion      int          `json:"schema_version"`
	Tool               toolDoc      `json:"tool"`
	Options            optionsDoc   `json:"options"`
	Source             sourceDoc    `json:"source"`
	Format             formatDoc    `json:"format"`
	Files              filesDoc     `json:"files"`
	Architecture       archDoc      `json:"architecture"`
	Config             configDoc    `json:"config"`
	Quantization       *quantDoc    `json:"quantization"`
	Tensors            tensorsDoc   `json:"tensors"`
	DTypes             []dtypeDoc   `json:"dtypes"`
	Storage            []storageDoc `json:"storage"`
	Memory             memoryDoc    `json:"memory"`
	Groups             []groupDoc   `json:"groups"`
	HostCapableTensors []hostDoc    `json:"host_capable_tensors"`
	Callouts           []calloutDoc `json:"callouts"`
	Scan               scanDoc      `json:"scan"`
	Code               *struct{}    `json:"code"`
	LoadWarnings       []string     `json:"load_warnings"`
	Partial            bool         `json:"partial"`
	ElapsedMS          int64        `json:"elapsed_ms"`
}

type toolDoc struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type optionsDoc struct {
	Strict bool `json:"strict"`
	Code   bool `json:"code"`
}

type sourceDoc struct {
	Path           string  `json:"path"`
	RepoID         *string `json:"repo_id"`
	Revision       *string `json:"revision"`
	RevisionSource string  `json:"revision_source"`
}

type formatDoc struct {
	Name      string `json:"name"`
	Variant   string `json:"variant"`
	Supported bool   `json:"supported"`
}

type filesDoc struct {
	Count      int        `json:"count"`
	TotalBytes int64      `json:"total_bytes"`
	ShardCount int        `json:"shard_count"`
	IndexFile  *string    `json:"index_file"`
	Shards     []shardDoc `json:"shards"`
}

type shardDoc struct {
	Name     string  `json:"name"`
	Bytes    int64   `json:"bytes"`
	Readable bool    `json:"readable"`
	Error    *string `json:"error"`
}

type archDoc struct {
	ModelType               *string        `json:"model_type"`
	LayerCount              int            `json:"layer_count"`
	LayersWithRoutedExperts int            `json:"layers_with_routed_experts"`
	LayerTypeCounts         []layerTypeDoc `json:"layer_type_counts"`
	RoutedExperts           *int           `json:"routed_experts"`
	ExpertsPerToken         *int           `json:"experts_per_token"`
	HasMTP                  bool           `json:"has_mtp"`
	HasVision               bool           `json:"has_vision"`
}

type layerTypeDoc struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

type configDoc struct {
	Present bool       `json:"present"`
	Fields  []fieldDoc `json:"fields"`
}

type fieldDoc struct {
	Key   string `json:"key"`
	Path  string `json:"path"`
	Group string `json:"group"`
	Value any    `json:"value"`
}

type quantDoc struct {
	ConfigKey     string    `json:"config_key"`
	Bits          int       `json:"bits"`
	GroupSize     int       `json:"group_size"`
	Mode          string    `json:"mode"`
	OverrideCount int       `json:"override_count"`
	BitsHistogram []bitsDoc `json:"bits_histogram"`
}

type bitsDoc struct {
	Bits    int `json:"bits"`
	Tensors int `json:"tensors"`
}

type tensorsDoc struct {
	Count               int    `json:"count"`
	StoredElements      int64  `json:"stored_elements"`
	ParamsEst           int64  `json:"params_est"`
	ParamsNote          string `json:"params_note"`
	Bytes               int64  `json:"bytes"`
	IndexTotalSizeBytes *int64 `json:"index_total_size_bytes"`
}

type dtypeDoc struct {
	DType    string `json:"dtype"`
	Tensors  int    `json:"tensors"`
	Elements int64  `json:"elements"`
	Bytes    int64  `json:"bytes"`
}

type storageDoc struct {
	Class string  `json:"class"`
	Bytes int64   `json:"bytes"`
	Pct   float64 `json:"pct"`
}

type memoryDoc struct {
	WeightsBytes        int64  `json:"weights_bytes"`
	TextOnlyBytes       int64  `json:"text_only_bytes"`
	HostCapableBytes    int64  `json:"host_capable_bytes"`
	AcceleratorMinBytes int64  `json:"accelerator_min_bytes"`
	UnifiedMemory       bool   `json:"unified_memory"`
	Note                string `json:"note"`
	HostCapableNote     string `json:"host_capable_note"`
	// HostPartial qualifies the text memory line. It is not a JSON field;
	// partial on the document is the inspection flag.
	HostPartial bool `json:"-"`
}

type groupDoc struct {
	Key              string   `json:"key"`
	Kind             string   `json:"kind"`
	Layer            *int     `json:"layer"`
	AttentionType    *string  `json:"attention_type"`
	Tensors          int      `json:"tensors"`
	ParamsEst        int64    `json:"params_est"`
	Bytes            int64    `json:"bytes"`
	PctBytes         float64  `json:"pct_bytes"`
	Storage          string   `json:"storage"`
	HasRoutedExperts bool     `json:"has_routed_experts"`
	RoutedExperts    *int     `json:"routed_experts"`
	Outliers         []string `json:"outliers"`
	HostCapableBytes int64    `json:"host_capable_bytes"`
}

type hostDoc struct {
	Name      string `json:"name"`
	Group     string `json:"group"`
	Bytes     int64  `json:"bytes"`
	ParamsEst int64  `json:"params_est"`
	Rule      string `json:"rule"`
}

type calloutDoc struct {
	Kind    string  `json:"kind"`
	Group   *string `json:"group"`
	Tensor  *string `json:"tensor"`
	Message string  `json:"message"`
}

type scanDoc struct {
	Strict     bool        `json:"strict"`
	Overall    string      `json:"overall"`
	Disclaimer string      `json:"disclaimer"`
	Counts     scan.Counts `json:"counts"`
	Checks     []checkDoc  `json:"checks"`
}

type checkDoc struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Status          string   `json:"status"`
	EffectiveStatus string   `json:"effective_status"`
	Summary         string   `json:"summary"`
	Details         []string `json:"details"`
}

// Build assembles a document. fromCache is true when the directory came
// from a hub lookup rather than a path the user named.
func Build(m *model.Model, res scan.Result, fromCache bool, elapsed time.Duration) Document {
	name, variant, supported := model.FormatName(m.Format)
	if variant == "" {
		variant = ""
	}
	layers, withExperts := m.LayerCount()
	mem := m.MemoryEstimate()
	host := m.HostCapable()
	callouts := m.Outliers()
	types := m.LayerTypes()

	d := Document{
		SchemaVersion: 1,
		Tool:          toolDoc{Name: version.Name, Version: version.Version},
		Options:       optionsDoc{Strict: res.Strict, Code: false},
		Source: sourceDoc{
			Path:           m.Dir,
			RepoID:         strPtr(m.RepoID),
			Revision:       strPtr(m.Revision),
			RevisionSource: revisionSource(m.Revision, fromCache),
		},
		Format: formatDoc{Name: name, Variant: variant, Supported: supported},
		Files: filesDoc{
			Count: len(m.Files), TotalBytes: m.TotalSize, ShardCount: len(m.Shards),
			Shards: []shardDoc{},
		},
		Architecture: archDoc{
			ModelType:  strPtr(configString(m, "model_type")),
			LayerCount: layers, LayersWithRoutedExperts: withExperts,
			LayerTypeCounts: layerTypeDocs(m),
			RoutedExperts:   configIntPtr(m, "num_experts", "num_local_experts", "n_routed_experts"),
			ExpertsPerToken: configIntPtr(m, "num_experts_per_tok"),
			HasMTP:          hasKind(m, model.KindMTP),
			HasVision:       hasKind(m, model.KindVision),
		},
		Config: configDoc{Present: m.Config != nil, Fields: []fieldDoc{}},
		Tensors: tensorsDoc{
			Count: len(m.Tensors), StoredElements: m.TotalElements,
			ParamsEst: m.TotalLogicalParams, ParamsNote: paramsNote, Bytes: m.TensorBytes,
		},
		DTypes: []dtypeDoc{}, Storage: []storageDoc{},
		Memory: memoryDoc{
			WeightsBytes: mem.WeightsBytes, TextOnlyBytes: mem.TextOnlyBytes,
			HostCapableBytes: mem.HostCapableBytes, AcceleratorMinBytes: mem.AcceleratorMinBytes,
			UnifiedMemory: mem.UnifiedMemory, Note: mem.Note, HostCapableNote: mem.HostCapableNote,
			HostPartial: mem.HostCapablePartial,
		},
		Groups: []groupDoc{}, HostCapableTensors: []hostDoc{}, Callouts: []calloutDoc{},
		Scan: scanDoc{
			Strict: res.Strict, Overall: string(res.Overall), Disclaimer: res.Disclaimer,
			Counts: res.Count(), Checks: []checkDoc{},
		},
		LoadWarnings: append([]string{}, m.Warnings...),
		Partial:      m.Partial,
		ElapsedMS:    elapsed.Milliseconds(),
	}
	if d.LoadWarnings == nil {
		d.LoadWarnings = []string{}
	}
	if m.Index != nil {
		d.Files.IndexFile = strPtr(m.Index.File)
		d.Tensors.IndexTotalSizeBytes = int64Ptr(m.Index.TotalSize)
	}
	for _, s := range m.Shards {
		sd := shardDoc{Name: s.Name, Bytes: s.Size, Readable: s.Header != nil}
		if s.Err != nil {
			sd.Error = strPtr(s.Err.Error())
		}
		d.Files.Shards = append(d.Files.Shards, sd)
	}
	for _, kv := range m.KeyConfig() {
		d.Config.Fields = append(d.Config.Fields, fieldDoc{
			Key: kv.Key, Path: kv.Path, Group: model.FieldGroup(kv.Key), Value: kv.Value,
		})
	}
	if m.Quant != nil {
		q := &quantDoc{
			ConfigKey: m.Quant.Source, Bits: m.Quant.Bits, GroupSize: m.Quant.GroupSize,
			Mode: m.Quant.Mode, OverrideCount: m.Quant.NumOverrides, BitsHistogram: []bitsDoc{},
		}
		bits := make([]int, 0, len(m.BitsHistogram))
		for b := range m.BitsHistogram {
			bits = append(bits, b)
		}
		sort.Ints(bits)
		for _, b := range bits {
			q.BitsHistogram = append(q.BitsHistogram, bitsDoc{Bits: b, Tensors: m.BitsHistogram[b]})
		}
		d.Quantization = q
	}
	for _, dt := range model.SortedKeys(m.DTypes) {
		st := m.DTypes[dt]
		d.DTypes = append(d.DTypes, dtypeDoc{DType: dt, Tensors: st.Tensors, Elements: st.Elements, Bytes: st.Bytes})
	}
	for _, c := range m.StorageBreakdown() {
		d.Storage = append(d.Storage, storageDoc{Class: c.Class, Bytes: c.Bytes, Pct: c.Pct})
	}
	hostByGroup := map[string]int64{}
	for _, h := range host {
		addGroupHost(m, hostByGroup, h.Group, h.Bytes)
		d.HostCapableTensors = append(d.HostCapableTensors, hostDoc{
			Name: h.Name, Group: h.Group, Bytes: h.Bytes, ParamsEst: h.ParamsEst, Rule: h.Rule,
		})
	}
	outliersByGroup := map[string][]string{}
	for _, c := range callouts {
		var tag string
		switch c.Kind {
		case "size_outlier":
			tag = "size"
		case "precision_outlier":
			tag = "precision"
		case "structural":
			tag = "structural"
		}
		if tag != "" && c.Group != "" {
			outliersByGroup[c.Group] = appendUnique(outliersByGroup[c.Group], tag)
		}
		d.Callouts = append(d.Callouts, calloutDoc{
			Kind: c.Kind, Group: strPtr(c.Group), Tensor: strPtr(c.Tensor), Message: c.Message,
		})
	}
	for _, g := range m.Groups {
		gd := groupDoc{
			Key: g.Key, Kind: g.Kind, Tensors: len(g.Tensors), ParamsEst: g.LogicalParams,
			Bytes: g.Bytes, PctBytes: pct(g.Bytes, m.TensorBytes), Storage: g.StorageLabel(),
			HasRoutedExperts: g.HasExperts, Outliers: outliersByGroup[g.Key],
			HostCapableBytes: hostByGroup[g.Key],
		}
		if gd.Outliers == nil {
			gd.Outliers = []string{}
		}
		if g.Layer >= 0 {
			gd.Layer = intPtr(g.Layer)
			if g.Layer < len(types) && types[g.Layer] != "" {
				gd.AttentionType = strPtr(types[g.Layer])
			}
		}
		d.Groups = append(d.Groups, gd)
	}
	for _, c := range res.Checks {
		details := c.Details
		if details == nil {
			details = []string{}
		}
		d.Scan.Checks = append(d.Scan.Checks, checkDoc{
			ID: c.ID, Name: c.Name, Status: string(c.Status),
			EffectiveStatus: string(c.EffectiveStatus), Summary: c.Summary, Details: details,
		})
	}
	// Summing host bytes can overflow after the caller already scanned.
	// Re-run the checks so partial, the printed scan, and the exit code
	// describe the same inspection.
	if m.Partial && d.Scan.Overall != string(scan.StatusFail) {
		d.Scan = scanDocument(scan.RunOptions(m, scan.Options{Strict: res.Strict}))
	}
	return d
}

func scanDocument(res scan.Result) scanDoc {
	d := scanDoc{
		Strict: res.Strict, Overall: string(res.Overall), Disclaimer: res.Disclaimer,
		Counts: res.Count(), Checks: []checkDoc{},
	}
	for _, c := range res.Checks {
		details := c.Details
		if details == nil {
			details = []string{}
		}
		d.Checks = append(d.Checks, checkDoc{
			ID: c.ID, Name: c.Name, Status: string(c.Status),
			EffectiveStatus: string(c.EffectiveStatus), Summary: c.Summary, Details: details,
		})
	}
	return d
}

// addGroupHost adds n into the per-group host total without wrapping.
// A sum that does not fit marks the inspection partial.
func addGroupHost(m *model.Model, acc map[string]int64, key string, n int64) {
	v := acc[key]
	if n < 0 || v < 0 || v > math.MaxInt64-n {
		if m != nil {
			m.Partial = true
			const msg = "displayed size overflowed int64 and was not accumulated"
			for _, w := range m.Warnings {
				if w == msg {
					return
				}
			}
			m.Warnings = append(m.Warnings, msg)
		}
		return
	}
	acc[key] = v + n
}

// Marshal encodes the document with stable indentation.
// HTML-sensitive characters are not escaped. encoding/json writes C0 as
// \uXXXX. sanitize.EscapeJSON does the same for C1, bidi, and other
// invisible characters inside strings.
func Marshal(d Document) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(d); err != nil {
		return nil, err
	}
	return sanitize.EscapeJSON(buf.Bytes()), nil
}

// Blocked reports whether the CLI should exit 3 after printing the summary.
func Blocked(d Document) (string, bool) {
	if d.Format.Name == "gguf" {
		return "format gguf: not supported yet (planned)", true
	}
	if !d.Format.Supported || d.Format.Name == "unknown" {
		return "no safetensors or gguf files found", true
	}
	return "", false
}

func revisionSource(rev string, fromCache bool) string {
	if rev == "" {
		return "none"
	}
	if fromCache {
		return "hf_cache_ref"
	}
	return "path"
}

func configString(m *model.Model, key string) string {
	for _, kv := range m.KeyConfig() {
		if kv.Key == key {
			return fmt.Sprint(kv.Value)
		}
	}
	return ""
}

func configIntPtr(m *model.Model, keys ...string) *int {
	for _, key := range keys {
		for _, kv := range m.KeyConfig() {
			if kv.Key != key {
				continue
			}
			n := int(asFloat(kv.Value))
			if n == 0 {
				continue
			}
			return &n
		}
	}
	return nil
}

func asFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		f, _ := strconv.ParseFloat(fmt.Sprint(v), 64)
		return f
	}
}

func layerTypeDocs(m *model.Model) []layerTypeDoc {
	counts := m.LayerTypeCounts()
	if len(counts) == 0 {
		for _, t := range m.LayerTypes() {
			if t == "" {
				continue
			}
			if counts == nil {
				counts = map[string]int{}
			}
			counts[t]++
		}
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]layerTypeDoc, 0, len(keys))
	for _, k := range keys {
		out = append(out, layerTypeDoc{Type: k, Count: counts[k]})
	}
	return out
}

func hasKind(m *model.Model, kind string) bool {
	for _, g := range m.Groups {
		if g.Kind == kind {
			return true
		}
	}
	return false
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func intPtr(n int) *int { return &n }

func int64Ptr(n int64) *int64 { return &n }

func pct(part, total int64) float64 {
	if total <= 0 || part <= 0 {
		return 0
	}
	if part >= total {
		return 100
	}
	v := math.Round(float64(part)/float64(total)*1000) / 10
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func appendUnique(ss []string, s string) []string {
	for _, e := range ss {
		if e == s {
			return ss
		}
	}
	return append(ss, s)
}

// ShortRevision is the first 7 characters, for display. JSON keeps the full value.
func ShortRevision(rev string) string {
	if len(rev) <= 7 {
		return rev
	}
	return rev[:7]
}

// DisplayName is the repo id, or the directory base when there is no repo id.
func DisplayName(d Document) string {
	if d.Source.RepoID != nil {
		return *d.Source.RepoID
	}
	if d.Source.Path == "" {
		return ""
	}
	path := d.Source.Path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}
