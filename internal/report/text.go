package report

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/rivo/uniseg"

	"origin.cursor.com/cecilkootz/model-inspector/internal/fileerr"
	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
)

// Text is the 80-column inspect summary. File-derived strings are sanitized.
// Sentences this program composed are not.
func Text(d Document) string {
	var b strings.Builder
	writeIdentity(&b, d)
	writeOwn(&b, "format", formatLine(d))
	disk := fmt.Sprintf("%s in %s files (%d shards) · %s tensors",
		model.HumanBytes(d.Files.TotalBytes), model.Comma(int64(d.Files.Count)), d.Files.ShardCount, model.Comma(int64(d.Tensors.Count)))
	if d.Partial {
		disk += " (incomplete)"
	}
	writeOwn(&b, "on disk", disk)
	writeOwn(&b, "params", fmt.Sprintf("~%s (dequantized est.; quant scales/biases excluded)", model.HumanCount(d.Tensors.ParamsEst)))
	if line := layerLine(d); line != "" {
		writeOwn(&b, "layers", line)
	}
	if line := otherLine(d); line != "" {
		writeOwn(&b, "other", line)
	}
	writeOwn(&b, "memory", memoryLine(d))
	if line := calloutLine(d); line != "" {
		writeOwn(&b, "callouts", line)
	}
	writeOwn(&b, "checks", checksLine(d))
	if problems := problemLines(d); len(problems) > 0 {
		writeOwn(&b, "problems", strings.Join(problems, "; "))
	}
	if len(d.LoadWarnings) > 0 {
		warned := make([]string, len(d.LoadWarnings))
		for i, w := range d.LoadWarnings {
			warned[i] = san(w)
		}
		writeOwn(&b, "warnings", strings.Join(warned, "; "))
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
		fmt.Fprintf(b, "%s\n", trunc(san(*d.Source.RepoID)+" @ "+san(rev)+" (cache ref, not hash-verified)", 80))
	case d.Source.RepoID != nil:
		fmt.Fprintf(b, "%s\n", trunc(san(*d.Source.RepoID), 80))
		fmt.Fprintf(b, "  %-10s%s\n", "revision", "—")
	default:
		fmt.Fprintf(b, "%s\n", trunc(san(d.Source.Path), 80))
		if rev != "" {
			fmt.Fprintf(b, "  %-10s%s\n", "revision", san(rev)+" (cache ref, not hash-verified)")
		} else {
			fmt.Fprintf(b, "  %-10s%s\n", "revision", "—")
		}
	}
}

func formatLine(d Document) string {
	s := d.Format.Name
	if d.Format.Variant != "" {
		// Name is safetensors/gguf/unknown. The variant is file metadata.
		s += " (" + san(d.Format.Variant) + ")"
	}
	if q := d.Quantization; q != nil && q.Bits > 0 {
		mode := q.Mode
		head := ""
		if d.Format.Variant == "mlx" || strings.EqualFold(mode, "mlx") {
			head = "MLX "
		}
		if mode != "" && !strings.EqualFold(mode, "mlx") {
			head += san(mode) + " "
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
				bits[i] = fmt.Sprintf("%d %s", lt.Count, san(lt.Type))
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
		label := san(g.Key)
		if g.Storage != "" && g.Storage != "—" && (g.Kind == "vision" || strings.HasPrefix(g.Storage, "bf") || strings.HasPrefix(g.Storage, "fp")) {
			parts = append(parts, fmt.Sprintf("%s %s (%s)", label, model.HumanBytes(g.Bytes), san(g.Storage)))
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
		label := model.HumanBytes(d.Memory.HostCapableBytes)
		line := fmt.Sprintf("of which %s lookup tables can stay in CPU RAM (heuristic)", label)
		if d.Memory.HostPartial {
			line = fmt.Sprintf("of which ≥ %s lookup tables can stay in CPU RAM (heuristic, incomplete)", label)
		}
		parts = append(parts, line)
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
		lines = append(lines, glyph(c.Kind)+" "+san(c.Message))
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
			lines = append(lines, c.ID+": "+san(detail))
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
	writeRow(b, label, san(value))
}

// writeOwn wraps a sentence this program composed. File bytes in it must
// already have been passed through san.
func writeOwn(b *strings.Builder, label, value string) {
	writeRow(b, label, value)
}

func writeRow(b *strings.Builder, label, value string) {
	// A cut is only on a grapheme boundary or between atoms, and never
	// inside a <U+XXXX> token.
	const width = 80
	prefix := fmt.Sprintf("  %-10s", label)
	rest := value
	first := true
	for rest != "" {
		pad := prefix
		if !first {
			pad = strings.Repeat(" ", len(prefix))
		}
		room := width - uniseg.StringWidth(pad)
		if room < 8 {
			room = 8
		}
		if uniseg.StringWidth(rest) <= room {
			fmt.Fprintf(b, "%s%s\n", pad, rest)
			return
		}
		line, next := takeLine(rest, room)
		if line == "" || next == rest {
			atom, after, _, _ := nextAtom(rest, -1)
			line = atom
			next = strings.TrimLeft(after, " ")
		}
		fmt.Fprintf(b, "%s%s\n", pad, line)
		rest = next
		first = false
	}
}

// takeLine returns the next display row of s, at most room columns wide.
// It breaks on a space when one sits inside the row, and otherwise on a
// grapheme boundary. Sanitizer tokens are one atom.
func takeLine(s string, room int) (line, rest string) {
	if room < 1 {
		room = 1
	}
	var b strings.Builder
	width := 0
	breakAt := -1
	state := -1
	remaining := s
	for remaining != "" {
		atom, next, w, st := nextAtom(remaining, state)
		if atom == "" {
			break
		}
		if w < 0 {
			w = 0
		}
		if width > 0 && width+w > room {
			break
		}
		b.WriteString(atom)
		width += w
		state = st
		if atom == " " && width > 16 {
			breakAt = b.Len()
		}
		remaining = next
		if width >= room {
			break
		}
	}
	full := b.String()
	if remaining != "" && breakAt > 0 && breakAt < len(full) {
		return strings.TrimRight(full[:breakAt], " "), strings.TrimLeft(full[breakAt:]+remaining, " ")
	}
	return strings.TrimRight(full, " "), strings.TrimLeft(remaining, " ")
}

// nextAtom returns one wrap unit: a <U+XXXX> token, or one grapheme cluster.
func nextAtom(s string, state int) (atom, rest string, width, newState int) {
	if n := tokenLen(s); n > 0 {
		return s[:n], s[n:], n, -1
	}
	atom, rest, width, newState = uniseg.FirstGraphemeClusterInString(s, state)
	if atom == "" && s != "" {
		// A lone invalid byte is still one unit, so the loop makes progress
		// and the following line cannot start with a continuation byte.
		r, size := utf8.DecodeRuneInString(s)
		if size < 1 {
			size = 1
		}
		_ = r
		return s[:size], s[size:], 1, -1
	}
	if width < 0 {
		width = 0
	}
	return atom, rest, width, newState
}

// tokenLen is the byte length of a leading sanitizer token, or 0.
// Tokens look like <U+009B> (4 to 6 hex digits).
func tokenLen(s string) int {
	if !strings.HasPrefix(s, "<U+") {
		return 0
	}
	i := strings.IndexByte(s, '>')
	if i < 7 || i > 9 {
		return 0
	}
	for _, c := range s[3:i] {
		if (c < '0' || c > '9') && (c < 'A' || c > 'F') {
			return 0
		}
	}
	return i + 1
}

func san(s string) string { return fileerr.Present(s) }

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
