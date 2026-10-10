package tui

import (
	"fmt"
	"strings"

	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
	"origin.cursor.com/cecilkootz/model-inspector/internal/sanitize"
)

func renderIdentity(m *model.Model, width int) string {
	if width < 20 {
		width = 20
	}
	var lines []string
	add := func(s string) { lines = append(lines, truncWidth(s, width)) }

	if strings.HasPrefix(m.Format, "gguf") {
		add("GGUF parsing is coming. model-inspector read file sizes only.")
		add("")
	}
	add(kvLine("Repo", repoValue(m), width))
	add(kvLine("Arch", archValue(m), width))
	if moe := moeValue(m); moe != "" {
		add(kvLine("MoE", moe, width))
	}
	add(kvLine("Size", sizeValue(m), width))
	add(kvLine("Params", paramsValue(m), width))
	add(kvLine("Quant", quantValue(m), width))
	add("Storage (tensor bytes)")
	bar, legend := storageBar(m, width-2)
	add(" " + bar)
	add(" " + legend)

	mem := m.MemoryEstimate()
	add(kvLine("Memory", memoryValue(m, mem), width))
	if hasVisionOrMTP(m) {
		add(indent("text-only ≥ " + model.HumanBytes(mem.TextOnlyBytes)))
	}
	if mem.HostCapableBytes >= model.GiB {
		bytes := model.HumanBytes(mem.HostCapableBytes)
		line := g.host + " of which " + bytes + " are lookup tables that can stay in CPU RAM"
		if mem.HostCapablePartial {
			line = g.host + " of which ≥ " + bytes + " are lookup tables that can stay in CPU RAM (incomplete)"
		}
		add(indent(line))
		add(indent("  (heuristic) → ≥ " + model.HumanBytes(mem.AcceleratorMinBytes) + " on the GPU/accelerator"))
	}
	if mem.UnifiedMemory {
		add(indent("unified memory (MLX): CPU and GPU share RAM, total is unchanged"))
	}
	add(indent("weights only: no KV cache, activations or runtime overhead"))

	callouts := m.Outliers()
	if len(callouts) > 0 {
		add("Callouts")
		lines = append(lines, calloutLines(m, callouts, width)...)
	}
	add("")
	add("Dtypes")
	for _, dt := range model.SortedKeys(m.DTypes) {
		d := m.DTypes[dt]
		add(indent(sanitize.From(fmt.Sprintf("%s  %d tensors, %s elems, %s", dt, d.Tensors, model.HumanCount(d.Elements), model.HumanBytes(d.Bytes))).String()))
	}
	return strings.Join(lines, "\n")
}

func kvLine(label, value string, width int) string {
	lab := padLabel(label, 9)
	room := width - 9
	if room < 1 {
		room = 1
	}
	return lab + truncWidth(sanitize.From(value).String(), room)
}

func indent(s string) string { return padLabel("", 9) + s }

func repoValue(m *model.Model) string {
	rev := ""
	if m.Revision != "" {
		rev = m.Revision
		if len(rev) > 7 {
			rev = rev[:7]
		}
	}
	if m.RepoID != "" && rev != "" {
		return m.RepoID + " @ " + rev + " (cache ref)"
	}
	if m.RepoID != "" {
		return m.RepoID + "  revision: —"
	}
	if rev != "" {
		return m.Dir + " @ " + rev + " (cache ref)"
	}
	return m.Dir + "  revision: —"
}

func archValue(m *model.Model) string {
	mt := "—"
	for _, kv := range m.KeyConfig() {
		if kv.Key == "model_type" && fmt.Sprint(kv.Value) != "" {
			mt = fmt.Sprint(kv.Value)
			break
		}
	}
	layers, _ := m.LayerCount()
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
	s := mt
	if layers > 0 {
		if len(counts) == 0 {
			s += fmt.Sprintf(" · %d layers", layers)
		} else {
			s += fmt.Sprintf(" · %d layers: %s", layers, joinTypes(counts))
		}
	}
	if hasKind(m, model.KindMTP) {
		s += " · + MTP"
	}
	return s
}

func joinTypes(counts map[string]int) string {
	order := []string{}
	pref := []string{"linear_attention", "full_attention"}
	seen := map[string]bool{}
	for _, p := range pref {
		if counts[p] > 0 {
			order = append(order, p)
			seen[p] = true
		}
	}
	rest := model.SortedKeys(counts)
	for _, k := range rest {
		if !seen[k] {
			order = append(order, k)
		}
	}
	parts := make([]string, len(order))
	for i, k := range order {
		parts[i] = fmt.Sprintf("%d %s", counts[k], shortType(k))
	}
	return strings.Join(parts, ", ")
}

func shortType(s string) string {
	switch s {
	case "linear_attention":
		return "linear-attn"
	case "full_attention":
		return "full-attn"
	default:
		return s
	}
}

func moeValue(m *model.Model) string {
	var n, per int
	var haveN, havePer bool
	for _, kv := range m.KeyConfig() {
		switch kv.Key {
		case "num_experts", "num_local_experts", "n_routed_experts":
			if !haveN {
				n = intOf(kv.Value)
				haveN = n > 0
			}
		case "num_experts_per_tok":
			per = intOf(kv.Value)
			havePer = per > 0
		}
	}
	if !haveN {
		return ""
	}
	layers, with := m.LayerCount()
	s := fmt.Sprintf("%d routed experts", n)
	if havePer {
		s += fmt.Sprintf(", %d/token", per)
	}
	if layers > 0 {
		s += fmt.Sprintf(" · experts in %d/%d layers", with, layers)
	}
	return s
}

func intOf(v any) int {
	f, _ := v.(float64)
	return int(f)
}

func sizeValue(m *model.Model) string {
	return fmt.Sprintf("%s on disk · %s files · %d shards · %s tensors",
		model.HumanBytes(m.TotalSize), model.Comma(int64(len(m.Files))), len(m.Shards), model.Comma(int64(len(m.Tensors))))
}

func paramsValue(m *model.Model) string {
	s := fmt.Sprintf("~%s dequantized est.", model.HumanCount(m.TotalLogicalParams))
	if m.TotalLogicalParams <= 0 {
		return s
	}
	// Shard pieces of one lookup table are summed before the 10% check.
	name, params, ngram := m.NotableParams()
	if params*10 < m.TotalLogicalParams {
		return s
	}
	if ngram {
		return s + fmt.Sprintf(" (~%s in one n-gram table)", model.HumanCount(params))
	}
	if name != "" {
		return s + " (" + model.ShortName(name) + ")"
	}
	return s
}

func quantValue(m *model.Model) string {
	if m.Quant == nil {
		d := model.DisplayDType(dominantDType(m))
		if d == "" {
			return "—"
		}
		return d
	}
	if len(m.BitsHistogram) > 1 {
		bits := make([]int, 0, len(m.BitsHistogram))
		for b := range m.BitsHistogram {
			bits = append(bits, b)
		}
		sortInts(bits)
		parts := make([]string, len(bits))
		for i, b := range bits {
			parts[i] = fmt.Sprintf("%d-bit %s", b, model.Comma(int64(m.BitsHistogram[b])))
		}
		return "mixed: " + strings.Join(parts, " · ")
	}
	head := ""
	mode := m.Quant.Mode
	if _, variant, _ := model.FormatName(m.Format); variant == "mlx" || strings.EqualFold(mode, "mlx") {
		head = "MLX "
	}
	if mode != "" && !strings.EqualFold(mode, "mlx") {
		head += mode + " "
	}
	s := fmt.Sprintf("%s%d-bit", head, m.Quant.Bits)
	if m.Quant.GroupSize > 0 {
		s += fmt.Sprintf(", group %d", m.Quant.GroupSize)
	}
	if m.Quant.NumOverrides > 0 {
		s += fmt.Sprintf(" · %d per-module overrides", m.Quant.NumOverrides)
	}
	return s
}

func sortInts(s []int) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func memoryValue(m *model.Model, mem model.MemoryEstimate) string {
	return fmt.Sprintf("weights alone ≥ %s (%s)", model.HumanBytes(mem.WeightsBytes), model.HumanDecimalGB(mem.WeightsBytes))
}

func hasVisionOrMTP(m *model.Model) bool {
	return hasKind(m, model.KindVision) || hasKind(m, model.KindMTP)
}

func hasKind(m *model.Model, kind string) bool {
	for _, g := range m.Groups {
		if g.Kind == kind {
			return true
		}
	}
	return false
}

func storageBar(m *model.Model, width int) (string, string) {
	if width < 4 {
		width = 4
	}
	classes := model.MergeStorage(m, m.StorageBreakdown(), 4)
	if len(classes) == 0 || m.TensorBytes <= 0 {
		return strings.Repeat(g.bar[3], width), "no tensor bytes"
	}
	for _, c := range classes {
		if c.Bytes < 0 || c.Pct < 0 {
			return strings.Repeat(g.bar[3], width), "size overflow"
		}
	}
	cells := make([]int, len(classes))
	used := 0
	for i, c := range classes {
		n := 0
		if c.Bytes > 0 && m.TensorBytes > 0 {
			n = int(float64(c.Bytes)/float64(m.TensorBytes)*float64(width) + 0.5)
		}
		if n < 0 {
			n = 0
		}
		if n > width {
			n = width
		}
		if c.Bytes > 0 && n < 1 {
			n = 1
		}
		cells[i] = n
		used += n
	}
	for used > width {
		// Shrink the largest segment that can give a cell back.
		best := -1
		for i, n := range cells {
			if n > 1 && (best < 0 || n > cells[best]) {
				best = i
			}
		}
		if best < 0 {
			break
		}
		cells[best]--
		used--
	}
	for used < width {
		best := 0
		for i, c := range classes {
			if c.Bytes > classes[best].Bytes {
				best = i
			}
		}
		cells[best]++
		used++
	}
	var bar strings.Builder
	var legend []string
	for i, c := range classes {
		fill := g.bar[i%len(g.bar)]
		n := cells[i]
		if n < 0 {
			n = 0
		}
		if n > width {
			n = width
		}
		bar.WriteString(strings.Repeat(fill, n))
		legend = append(legend, sanitize.From(fmt.Sprintf("%s %s %.1f%%", fill, classLabel(c.Class), c.Pct)).String())
	}
	return bar.String(), strings.Join(legend, "  ")
}

func classLabel(class string) string {
	switch {
	case class == "q_scales_biases":
		return "q-scales/biases"
	case strings.HasPrefix(class, "plain_"):
		return "plain " + strings.TrimPrefix(class, "plain_")
	case strings.HasSuffix(class, "_packed"):
		return strings.TrimSuffix(class, "_packed") + " packed"
	default:
		return class
	}
}

func calloutLines(m *model.Model, cs []model.Callout, width int) []string {
	var lines []string
	prevSizeGroup := ""
	used := 0
	const maxLines = 4
	for _, c := range cs {
		if used >= maxLines {
			break
		}
		glyph := calloutGlyph(c.Kind)
		text := glyph + " " + sanitize.From(c.Message).String()
		if c.Kind == "host_capable" && c.Group != "" && c.Group == prevSizeGroup {
			text = "  " + text
		}
		lines = append(lines, truncWidth(text, width))
		used++
		if c.Kind == "size_outlier" {
			prevSizeGroup = c.Group
		}
		if c.Kind == "load_warnings" {
			for i, w := range m.Warnings {
				if used >= maxLines || i >= 3 {
					break
				}
				lines = append(lines, truncWidth("  "+sanitize.From(w).String(), width))
				used++
			}
		}
	}
	return lines
}

func calloutGlyph(kind string) string {
	switch kind {
	case "size_outlier", "big_tensor":
		return g.size
	case "host_capable":
		return g.host
	case "precision_outlier":
		return g.precision
	case "structural":
		return g.note
	case "load_warnings":
		return g.warnMark
	default:
		return "·"
	}
}
