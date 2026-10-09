// Command model-inspector inspects local model weights (headers and small
// JSON files only) in a terminal UI or as JSON.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mwelch/model-inspector/internal/hfcache"
	"github.com/mwelch/model-inspector/internal/model"
	"github.com/mwelch/model-inspector/internal/scan"
	"github.com/mwelch/model-inspector/internal/tui"
)

const usage = `model-inspector: inspect local model weights without loading them.

Usage:
  model-inspector [tui] <path-or-repo>            interactive TUI
  model-inspector inspect <path-or-repo> [--json]  headless summary

<path-or-repo> is a model/snapshot directory, or an HF repo id (org/name[@ref])
resolved through the Hugging Face hub cache.

Flags:
  --json            print a JSON summary (inspect only)
  --hub-dir <dir>   HF hub cache (default: $HF_HUB_CACHE, $HF_HOME/hub, ~/.cache/huggingface/hub)
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "model-inspector:", err)
		os.Exit(1)
	}
}

type options struct {
	json   bool
	hubDir string
	target string
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return errors.New("missing model path or repo id")
	}
	cmd := "tui"
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	case "inspect", "tui":
		cmd, args = args[0], args[1:]
	}
	opts, err := parseArgs(args)
	if err != nil {
		return err
	}
	start := time.Now()
	m, err := load(opts)
	if err != nil {
		return err
	}
	res := scan.Run(m)
	if cmd == "tui" {
		return tui.Run(m, res)
	}
	s := buildSummary(m, res, time.Since(start))
	if opts.json {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(s)
	}
	printText(s)
	return nil
}

func parseArgs(args []string) (options, error) {
	o := options{hubDir: hfcache.DefaultHubDir()}
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json" || a == "-json":
			o.json = true
		case a == "--hub-dir" || a == "-hub-dir":
			if i+1 >= len(args) {
				return o, errors.New("--hub-dir needs a value")
			}
			i++
			o.hubDir = args[i]
		case strings.HasPrefix(a, "--hub-dir="):
			o.hubDir = strings.TrimPrefix(a, "--hub-dir=")
		case strings.HasPrefix(a, "-"):
			return o, fmt.Errorf("unknown flag %s", a)
		default:
			pos = append(pos, a)
		}
	}
	if len(pos) != 1 {
		return o, fmt.Errorf("expected exactly one model path or repo id, got %d", len(pos))
	}
	o.target = pos[0]
	return o, nil
}

func load(o options) (*model.Model, error) {
	r, err := hfcache.Resolve(o.target, o.hubDir)
	if err != nil {
		return nil, err
	}
	m, err := model.Load(r.Dir)
	if err != nil {
		return nil, err
	}
	m.RepoID, m.Revision = r.RepoID, r.Revision
	return m, nil
}

type summary struct {
	Identity     identity                    `json:"identity"`
	Config       map[string]any              `json:"config"`
	LayerTypes   map[string]int              `json:"layer_types,omitempty"`
	Quantization *model.Quantization         `json:"quantization,omitempty"`
	Layers       layerSummary                `json:"layers"`
	Tensors      tensorSummary               `json:"tensors"`
	DTypes       map[string]*model.DTypeStat `json:"dtypes"`
	QuantBits    map[string]int              `json:"quant_bits_histogram,omitempty"`
	Scan         scan.Result                 `json:"scan"`
	Warnings     []string                    `json:"warnings,omitempty"`
	ElapsedMS    int64                       `json:"elapsed_ms"`
}

type identity struct {
	Path              string `json:"path"`
	RepoID            string `json:"repo_id,omitempty"`
	Revision          string `json:"revision,omitempty"`
	Format            string `json:"format"`
	TotalSizeBytes    int64  `json:"total_size_bytes"`
	TotalSizeHuman    string `json:"total_size_human"`
	FileCount         int    `json:"file_count"`
	SafetensorsShards int    `json:"safetensors_shards"`
}

type groupSummary struct {
	Name    string `json:"name"`
	Tensors int    `json:"tensors"`
	Params  int64  `json:"params_est"`
	Bytes   int64  `json:"bytes"`
}

type layerSummary struct {
	Count       int            `json:"count"`
	WithExperts int            `json:"with_routed_experts"`
	OtherGroups []groupSummary `json:"other_groups"`
}

type tensorSummary struct {
	Count          int    `json:"count"`
	StoredElements int64  `json:"stored_elements"`
	ParamsEst      int64  `json:"params_est"`
	ParamsEstHuman string `json:"params_est_human"`
	ParamsNote     string `json:"params_note"`
	Bytes          int64  `json:"bytes"`
	BytesHuman     string `json:"bytes_human"`
	IndexTotalSize int64  `json:"index_total_size,omitempty"`
}

func buildSummary(m *model.Model, res scan.Result, elapsed time.Duration) summary {
	s := summary{
		Identity: identity{
			Path: m.Dir, RepoID: m.RepoID, Revision: m.Revision, Format: m.Format,
			TotalSizeBytes: m.TotalSize, TotalSizeHuman: model.HumanBytes(m.TotalSize),
			FileCount: len(m.Files), SafetensorsShards: len(m.Shards),
		},
		Config:       map[string]any{},
		LayerTypes:   m.LayerTypeCounts(),
		Quantization: m.Quant,
		Tensors: tensorSummary{
			Count: len(m.Tensors), StoredElements: m.TotalElements,
			ParamsEst: m.TotalLogicalParams, ParamsEstHuman: model.HumanCount(m.TotalLogicalParams),
			ParamsNote: "packed quantized weights counted at dequantized size; quantization scales/biases excluded",
			Bytes:      m.TensorBytes, BytesHuman: model.HumanBytes(m.TensorBytes),
		},
		DTypes:    m.DTypes,
		Scan:      res,
		Warnings:  m.Warnings,
		ElapsedMS: elapsed.Milliseconds(),
	}
	if m.Index != nil {
		s.Tensors.IndexTotalSize = m.Index.TotalSize
	}
	for _, kv := range m.KeyConfig() {
		s.Config[kv.Path] = kv.Value
	}
	s.Layers.Count, s.Layers.WithExperts = m.LayerCount()
	for _, g := range m.Groups {
		if g.Kind != model.KindLayer {
			s.Layers.OtherGroups = append(s.Layers.OtherGroups, groupSummary{g.Key, len(g.Tensors), g.LogicalParams, g.Bytes})
		}
	}
	if len(m.BitsHistogram) > 0 {
		s.QuantBits = map[string]int{}
		for b, n := range m.BitsHistogram {
			s.QuantBits[strconv.Itoa(b)] = n
		}
	}
	return s
}

func printText(s summary) {
	name := s.Identity.RepoID
	if name == "" {
		name = s.Identity.Path
	}
	fmt.Printf("%s @ %s\n", name, s.Identity.Revision)
	fmt.Printf("  format:   %s\n", s.Identity.Format)
	fmt.Printf("  on disk:  %s in %d files (%d shards)\n", s.Identity.TotalSizeHuman, s.Identity.FileCount, s.Identity.SafetensorsShards)
	fmt.Printf("  tensors:  %d, %s params (est), %s\n", s.Tensors.Count, s.Tensors.ParamsEstHuman, s.Tensors.BytesHuman)
	fmt.Printf("  layers:   %d (%d with routed experts)\n", s.Layers.Count, s.Layers.WithExperts)
	for _, g := range s.Layers.OtherGroups {
		fmt.Printf("  %-10s %d tensors, %s\n", g.Name+":", g.Tensors, model.HumanBytes(g.Bytes))
	}
	fmt.Printf("  scan:     %s (%s)\n", s.Scan.Overall, s.Scan.Disclaimer)
}
