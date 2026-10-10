package report

import (
	"fmt"
	"strings"

	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
	"origin.cursor.com/cecilkootz/model-inspector/internal/sanitize"
)

// Text is the 80-column inspect summary. Every file-derived string is sanitized.
func Text(d Document) string {
	var b strings.Builder
	writeIdentity(&b, d)
	writeWrapped(&b, "format", formatLine(d))
	writeWrapped(&b, "on disk", fmt.Sprintf("%s in %s files (%d shards) · %s tensors",
		model.HumanBytes(d.Files.TotalBytes), model.Comma(int64(d.Files.Count)), d.Files.ShardCount, model.Comma(int64(d.Tensors.Count))))
	writeWrapped(&b, "params", fmt.Sprintf("~%s (dequantized est.; quant scales/biases excluded)", model.HumanCount(d.Tensors.ParamsEst)))
	if line := layerLine(d); line != "" {
		writeWrapped(&b, "layers", line)
	}
	if line := otherLine(d); line != "" {
		writeWrapped(&b, "other", line)
	}
	writeWrapped(&b, "memory", memoryLine(d))
	if line := calloutLine(d); line != "" {
		writeWrapped(&b, "callouts", line)
	}
	writeWrapped(&b, "checks", checksLine(d))
	if problems := problemLines(d); len(problems) > 0 {
		writeWrapped(&b, "problems", strings.Join(problems, "; "))
	}
	if len(d.LoadWarnings) > 0 {
		writeWrapped(&b, "warnings", strings.Join(d.LoadWarnings, "; "))
	}
	return b.String()
}

func writeIdentity(b *strings.Builder, d Document) {
	rev := ""
	if d.Source.Revision != nil {
		rev = ShortRevision(*d.Source.Revision)
	}
	switch {
	case d.Source.RepoID != nil && rev != "":
		fmt.Fprintf(b, "%s\n", trunc(san(fmt.Sprintf("%s @ %s (cache ref, not hash-verified)", *d.Source.RepoID, rev)), 80))
	case d.Source.RepoID != nil:
		fmt.Fprintf(b, "%s\n", trunc(san(*d.Source.RepoID), 80))
		fmt.Fprintf(b, "  %-10s%s\n", "revision", "—")
	default:
		fmt.Fprintf(b, "%s\n", trunc(san(d.Source.Path), 80))
		if rev != "" {
			fmt.Fprintf(b, "  %-10s%s\n", "revision", san(rev+" (cache ref, not hash-verified)"))
		} else {
			fmt.Fprintf(b, "  %-10s%s\n", "revision", "—")
		}
	}
}

func formatLine(d Document) string {
	s := d.Format.Name
	if d.Format.Variant != "" {
		s += " (" + d.Format.Variant + ")"
	}
	if q := d.Quantization; q != nil && q.Bits > 0 {
		mode := q.Mode
		head := ""
		if d.Format.Variant == "mlx" || strings.EqualFold(mode, "mlx") {
			head = "MLX "
		}
		if mode != "" && !strings.EqualFold(mode, "mlx") {
			head += mode + " "
		}
		s += fmt.Sprintf(" · %s%d-bit", head, q.Bits)
		if q.GroupSize > 0 {
			s += fmt.Sprintf(", group %d", q.GroupSize)
		}
		if q.OverrideCount > 0 {
			s += fmt.Sprintf(", %d overrides", q.OverrideCount)
		}
	}
	if !d.Format.Supported {
		s += " · not supported yet"
	}
	return s
}

func layerLine(d Document) string {
	if d.Architecture.LayerCount == 0 && len(d.Architecture.LayerTypeCounts) == 0 && d.Architecture.RoutedExperts == nil {
		return ""
	}
	var parts []string
	if d.Architecture.LayerCount > 0 {
		if len(d.Architecture.LayerTypeCounts) == 0 {
			parts = append(parts, fmt.Sprintf("%d decoder", d.Architecture.LayerCount))
		} else {
			bits := make([]string, len(d.Architecture.LayerTypeCounts))
			for i, lt := range d.Architecture.LayerTypeCounts {
				bits[i] = fmt.Sprintf("%d %s", lt.Count, lt.Type)
			}
			parts = append(parts, fmt.Sprintf("%d decoder: %s", d.Architecture.LayerCount, strings.Join(bits, ", ")))
		}
	}
	if d.Architecture.RoutedExperts != nil {
		s := fmt.Sprintf("routed experts in %d/%d layers · %d experts", d.Architecture.LayersWithRoutedExperts, d.Architecture.LayerCount, *d.Architecture.RoutedExperts)
		if d.Architecture.ExpertsPerToken != nil {
			s += fmt.Sprintf(", %d per token", *d.Architecture.ExpertsPerToken)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "; ")
}

func otherLine(d Document) string {
	var parts []string
	for _, g := range d.Groups {
		if g.Kind == "layer" {
			continue
		}
		label := g.Key
		if g.Storage != "" && g.Storage != "—" && (g.Kind == "vision" || strings.HasPrefix(g.Storage, "bf") || strings.HasPrefix(g.Storage, "fp")) {
			parts = append(parts, fmt.Sprintf("%s %s (%s)", label, model.HumanBytes(g.Bytes), g.Storage))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s", label, model.HumanBytes(g.Bytes)))
	}
	return strings.Join(parts, " · ")
}

func memoryLine(d Document) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("weights alone ≥ %s (%s)", model.HumanBytes(d.Memory.WeightsBytes), model.HumanDecimalGB(d.Memory.WeightsBytes)))
	if d.Architecture.HasMTP || d.Architecture.HasVision {
		parts = append(parts, fmt.Sprintf("text-only ≥ %s", model.HumanBytes(d.Memory.TextOnlyBytes)))
	}
	if d.Memory.HostCapableBytes >= model.GiB {
		parts = append(parts, fmt.Sprintf("of which %s lookup tables can stay in CPU RAM (heuristic)", model.HumanBytes(d.Memory.HostCapableBytes)))
	}
	if d.Memory.UnifiedMemory {
		parts = append(parts, "unified memory (MLX): total unchanged")
	}
	parts = append(parts, "excludes KV cache, activations, runtime overhead")
	return strings.Join(parts, "; ")
}

func calloutLine(d Document) string {
	if len(d.Callouts) == 0 {
		return ""
	}
	lines := make([]string, 0, len(d.Callouts))
	for _, c := range d.Callouts {
		lines = append(lines, glyph(c.Kind)+" "+c.Message)
	}
	return strings.Join(lines, "; ")
}

func checksLine(d Document) string {
	c := d.Scan.Counts
	notRun := c.Skip + c.Planned
	s := fmt.Sprintf("%d passed, %d warn, %d fail, %d not run (structure only; not a malware or behavior scan)",
		c.Pass, c.Warn, c.Fail, notRun)
	if d.Scan.Strict {
		s += " (strict: warnings count as failures)"
	}
	return s
}

func problemLines(d Document) []string {
	if d.Scan.Overall != "fail" {
		return nil
	}
	var lines []string
	for _, c := range d.Scan.Checks {
		if c.EffectiveStatus != "fail" {
			continue
		}
		for _, detail := range c.Details {
			lines = append(lines, c.ID+": "+detail)
		}
	}
	const max = 5
	if len(lines) > max {
		extra := len(lines) - max
		lines = append(lines[:max], fmt.Sprintf("…%d more (use --json)", extra))
	}
	return lines
}

func glyph(kind string) string {
	switch kind {
	case "size_outlier", "big_tensor":
		return "▲"
	case "host_capable":
		return "⌂"
	case "precision_outlier":
		return "◆"
	case "structural":
		return "●"
	case "load_warnings":
		return "!"
	default:
		return "·"
	}
}

func writeWrapped(b *strings.Builder, label, value string) {
	value = san(value)
	const width = 80
	prefix := fmt.Sprintf("  %-10s", label)
	rest := value
	first := true
	for rest != "" {
		pad := prefix
		if !first {
			pad = strings.Repeat(" ", len(prefix))
		}
		room := width - len(pad)
		if room < 8 {
			room = 8
		}
		if len(rest) <= room {
			fmt.Fprintf(b, "%s%s\n", pad, rest)
			return
		}
		cut := room
		if i := strings.LastIndex(rest[:room], " "); i > 16 {
			cut = i
		}
		fmt.Fprintf(b, "%s%s\n", pad, strings.TrimRight(rest[:cut], " "))
		rest = strings.TrimLeft(rest[cut:], " ")
		first = false
	}
}

func san(s string) string { return sanitize.From(s).String() }

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}
