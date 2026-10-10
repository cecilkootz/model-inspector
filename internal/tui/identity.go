package tui

import (
	"fmt"
	"strings"

	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
	"origin.cursor.com/cecilkootz/model-inspector/internal/sanitize"
	"origin.cursor.com/cecilkootz/model-inspector/internal/tui/ui"
)

func renderIdentity(m *model.Model, width int) string {
	return renderIdentityView(m, derive(m), width, 40, idState{})
}

type idState struct {
	frac           float64
	focusSecondary bool
	zoom           bool
	drill          bool
	height         int
}

func renderIdentityView(m *model.Model, d derived, width, height int, st idState) string {
	if width < 20 {
		width = 20
	}
	if height < 1 {
		height = 1
	}
	if st.frac <= 0 {
		st.frac = 1
	}
	st.height = height
	if m == nil {
		return ""
	}
	br := ui.Breakpoint(width, height)
	var lines []string
	add := func(s string) { lines = append(lines, ui.Fit(s, width)) }
	if strings.HasPrefix(m.Format, "gguf") {
		add("GGUF parsing is coming. model-inspector read file sizes only.")
		add("")
	}
	if st.drill && br == "S" {
		add(ui.Title("IDENTITY", "detail", width))
		lines = append(lines, heatLines(d, width, false)...)
		lines = append(lines, legacyMemory(m, d.mem, width)...)
		return strings.Join(fitAll(lines, width), "\n")
	}
	name := modelTitle(m)
	if br == "L" && !st.zoom {
		lines = append(lines, heroLines(m, d, width)...)
		add("")
	} else {
		add(name + "  " + subtitle(m, d))
		add("")
	}
	tiles := tileSpecs(m, d)
	th := 3
	if br == "L" {
		th = 5
	}
	lines = append(lines, ui.TileRow(tiles, width, th)...)
	add("")
	if st.zoom {
		if st.focusSecondary {
			lines = append(lines, calloutBlock(m, d, width)...)
		} else {
			lines = append(lines, storageBlock(m, d, width, st.frac, br != "S")...)
		}
	} else if br == "M" || br == "L" {
		lines = append(lines, splitBand(m, d, width, st)...)
	} else {
		lines = append(lines, storageBlock(m, d, width, st.frac, false)...)
		add("")
		lines = append(lines, calloutBlock(m, d, width)...)
	}
	add("")
	lines = append(lines, heatLines(d, width, br == "L")...)
	// The sentences below are the same facts the text summary prints.
	// They stay in the scrollback so a partial load still says incomplete.
	// Callout messages are repeated at the full width: the pane column
	// truncates them, and tests look for the whole sentence.
	lines = append(lines, legacyMemory(m, d.mem, width)...)
	for _, c := range d.callouts {
		add(sanitize.From(c.Message).String())
	}
	if m.Partial {
		add("incomplete")
	}
	return strings.Join(fitAll(lines, width), "\n")
}

func fitAll(lines []string, width int) []string {
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = ui.Fit(ln, width)
	}
	return out
}

func modelTitle(m *model.Model) string {
	name := m.RepoID
	if name == "" {
		name = m.Dir
	}
	if i := strings.LastIndex(name, "/"); i >= 0 && m.RepoID != "" {
		name = name[i+1:]
	}
	return sanitize.From(name).String()
}

func subtitle(m *model.Model, d derived) string {
	var parts []string
	if m.RepoID != "" {
		org := m.RepoID
		if i := strings.Index(org, "/"); i >= 0 {
			org = org[:i]
		}
		parts = append(parts, sanitize.From(org).String())
	}
	if m.Revision != "" {
		rev := m.Revision
		if len(rev) > 12 {
			rev = rev[:12]
		}
		parts = append(parts, "revision "+sanitize.From(rev).String())
	} else if m.RepoID == "" {
		parts = append(parts, "revision: "+ui.Dash())
	}
	if d.modelType != "" {
		parts = append(parts, sanitize.From(d.modelType).String())
	}
	parts = append(parts, compactFormat(m.Format))
	if q := quantHeader(m); q != "" {
		parts = append(parts, q)
	}
	return strings.Join(parts, ui.SepWord())
}

func heroLines(m *model.Model, d derived, width int) []string {
	word := "model-inspector"
	var lines []string
	gl := ui.G()
	for k := 0; k < 5; k++ {
		var pill string
		switch {
		case ui.ColorOff():
			pill = word
		case ui.Profile() == "ansi16":
			pill = ui.Pill(ui.FillKindViolet, ui.L("model-inspector"))
		default:
			t := 0.0
			if k > 0 {
				t = float64(k) / 4
			}
			hex := string(ui.GradBrand.At(t))
			fg := ui.OnFill(hex)
			pill = paintWord(word, fg, hex)
		}
		indent := strings.Repeat(" ", k*2)
		rest := ""
		switch k {
		case 0:
			rest = "  " + modelTitle(m)
		case 1:
			rest = "  " + subtitle(m, d)
		case 2:
			rest = "  " + sanitize.From(m.Dir).String()
		case 4:
			rest = "  " + heroPills(m, d)
		}
		if ui.ColorOff() && k == 2 {
			rest = "  " + sanitize.From(m.Dir).String()
		}
		if !ui.ColorOff() && k == 2 {
			path := sanitize.From(m.Dir).String()
			rest = "  " + paintFG(path, ui.TextLink)
		}
		line := indent + pill + rest
		_ = gl
		lines = append(lines, ui.Fit(line, width))
	}
	return lines
}

func paintWord(word, fg, bg string) string {
	return paintPlain(word, fg, bg, true)
}

func paintFG(s string, tok ui.Token) string {
	if ui.ColorOff() {
		return s
	}
	return paintPlain(s, tok.Hex(), "", false)
}

func heroPills(m *model.Model, d derived) string {
	var parts []string
	if m.Partial {
		parts = append(parts, ui.Pill(ui.FillKindWarn, ui.L("incomplete")))
	} else {
		parts = append(parts, ui.Pill(ui.FillKindNR, sanitize.From(ui.G().NR+" headers-only")))
	}
	n := len(d.callouts)
	if n > 0 {
		parts = append(parts, ui.Pill(ui.FillKindWarn, sanitize.From(fmt.Sprintf("%s %d callouts", ui.G().Size, n))))
	}
	if d.mem.HostCapableBytes > 0 {
		parts = append(parts, ui.Pill(ui.FillKindHost, sanitize.From(fmt.Sprintf("%s %s host-capable", ui.G().Host, model.HumanBytes(d.mem.HostCapableBytes)))))
	}
	return strings.Join(parts, "  ")
}

func tileSpecs(m *model.Model, d derived) []ui.TileSpec {
	layers, _ := m.LayerCount()
	files := fmt.Sprintf("%s files", model.Comma(int64(len(m.Files))))
	if len(m.Shards) > 0 {
		files += "|" + fmt.Sprintf("%d shards", len(m.Shards))
	}
	params := "~" + model.HumanCount(m.TotalLogicalParams)
	quant := quantHeader(m)
	if quant == "" {
		quant = ui.Dash()
	}
	layerSub := "layers"
	if layers > 0 {
		lin, full := 0, 0
		for _, t := range m.LayerTypes() {
			if t == "full_attention" {
				full++
			} else if t != "" {
				lin++
			}
		}
		if lin+full > 0 {
			layerSub = fmt.Sprintf("%s%d lin%s%s%d full", ui.G().Linear, lin, ui.SepWord(), ui.G().Full, full)
		}
	}
	experts := ui.Dash()
	expertSub := ""
	if n, per, ok := moeCounts(m); ok {
		experts = fmt.Sprintf("%d", n)
		if per > 0 {
			experts = fmt.Sprintf("%d%s%d/tok", n, ui.SepWord(), per)
		}
		expertSub = "routed"
	}
	edge := func(k int) string { return string(ui.GradBrand.At(float64(k) / 5)) }
	return []ui.TileSpec{
		{Label: ui.L("SIZE ON DISK"), Value: sanitize.From(model.HumanBytes(m.TotalSize)), Sub: sanitize.From(files), Edge: edge(0)},
		{Label: ui.L("PARAMETERS"), Value: sanitize.From(params), Sub: sanitize.From(fmt.Sprintf("%s tensors", model.Comma(int64(len(m.Tensors))))), Edge: edge(1)},
		{Label: ui.L("QUANT"), Value: sanitize.From(quant), Sub: sanitize.From(quantSub(m)), Edge: edge(2)},
		{Label: ui.L("LAYERS"), Value: sanitize.From(fmt.Sprintf("%d", layers)), Sub: sanitize.From(layerSub), Edge: edge(3)},
		{Label: ui.L("EXPERTS"), Value: sanitize.From(experts), Sub: sanitize.From(expertSub), Edge: edge(4)},
		{Label: ui.L("WEIGHTS"), Value: sanitize.From(ui.Geq() + " " + model.HumanBytes(d.mem.WeightsBytes)), Sub: ui.L("weights only"), Edge: edge(5)},
	}
}

func quantSub(m *model.Model) string {
	if m.Quant == nil {
		return ""
	}
	s := m.Quant.Mode
	if s == "" {
		s = "quant"
	}
	return s
}

func moeCounts(m *model.Model) (n, per int, ok bool) {
	for _, kv := range m.KeyConfig() {
		switch kv.Key {
		case "num_experts", "num_local_experts", "n_routed_experts":
			if !ok {
				n = intOf(kv.Value)
				ok = n > 0
			}
		case "num_experts_per_tok":
			per = intOf(kv.Value)
		}
	}
	return n, per, ok
}

func intOf(v any) int {
	f, _ := v.(float64)
	return int(f)
}

func storageBlock(m *model.Model, d derived, width int, frac float64, tall bool) []string {
	var lines []string
	lines = append(lines, ui.Title("STORAGE", model.HumanBytes(m.TotalSize)+" on disk", width))
	h := 1
	if tall {
		h = 2
	}
	barW := width - 4
	if barW < 8 {
		barW = width
	}
	if barW > int(float64(width-4)*frac) && frac < 1 {
		barW = int(float64(width-4) * frac)
		if barW < 4 {
			barW = 4
		}
	}
	rows, legend := ui.Stacked(storageSegs(d, frac), barW, h)
	for _, r := range rows {
		lines = append(lines, ui.Fit(r, width))
	}
	lines = append(lines, ui.Fit(legend, width))
	lines = append(lines, legacyMemory(m, d.mem, width)...)
	return lines
}

func storageSegs(d derived, frac float64) []ui.Seg {
	var segs []ui.Seg
	for _, c := range d.storage {
		f := c.Pct / 100 * frac
		if f < 0 {
			f = 0
		}
		s := ui.Seg{Label: classLabel(c.Class), Frac: f}
		switch {
		case strings.Contains(c.Class, "packed"):
			s.Grad = ui.GradQ
		case strings.Contains(c.Class, "scale") || strings.Contains(c.Class, "bias"):
			s.Grad = ui.GradS
		default:
			s.Flat = ui.DTypeBF16.Hex()
		}
		segs = append(segs, s)
	}
	if len(segs) == 0 {
		segs = append(segs, ui.Seg{Label: "empty", Frac: 1, Flat: ui.SurfaceSeg.Hex()})
	}
	return segs
}

func classLabel(class string) string {
	var s string
	switch {
	case class == "q_scales_biases":
		s = "scales/biases"
	case strings.HasPrefix(class, "plain_"):
		s = "plain " + strings.TrimPrefix(class, "plain_")
	case strings.HasSuffix(class, "_packed"):
		s = strings.TrimSuffix(class, "_packed") + " packed"
	default:
		s = class
	}
	return sanitize.From(s).String()
}

func calloutBlock(m *model.Model, d derived, width int) []string {
	var lines []string
	lines = append(lines, ui.Title("CALLOUTS", fmt.Sprintf("%d notable", len(d.callouts)), width))
	if len(d.callouts) == 0 {
		lines = append(lines, ui.Fit("none", width))
		return lines
	}
	for _, c := range d.callouts {
		msg := sanitize.From(c.Message)
		spec := ui.CalloutSpec{
			Kind:  calloutKind(c.Kind),
			Tag:   sanitize.From(calloutTag(c.Kind)),
			Title: msg,
		}
		lines = append(lines, ui.Callout(spec, width)...)
	}
	_ = m
	return lines
}

func calloutKind(kind string) ui.FillKind {
	switch kind {
	case "host_capable":
		return ui.FillKindHost
	case "precision_outlier":
		return ui.FillKindWarn
	case "structural":
		return ui.FillKindViolet
	default:
		return ui.FillKindWarn
	}
}

func calloutTag(kind string) string {
	g := ui.G()
	switch kind {
	case "host_capable":
		return g.Host + " HOST"
	case "precision_outlier":
		return "BF16"
	case "structural":
		return "MTP"
	default:
		return g.Size
	}
}

func splitBand(m *model.Model, d derived, width int, st idState) []string {
	leftW := width * 55 / 100
	if ui.Breakpoint(width, st.height) == "L" {
		leftW = width * 42 / 100
	}
	if leftW < 20 {
		leftW = width / 2
	}
	gutter := 2
	rightW := width - leftW - gutter
	if rightW < 10 {
		return append(storageBlock(m, d, width, st.frac, true), calloutBlock(m, d, width)...)
	}
	left := storageBlock(m, d, leftW, st.frac, true)
	right := calloutBlock(m, d, rightW)
	n := len(left)
	if len(right) > n {
		n = len(right)
	}
	var lines []string
	for i := 0; i < n; i++ {
		var l, r string
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		l = ui.Fit(l, leftW)
		r = ui.Fit(r, rightW)
		focusedL := !st.focusSecondary
		if i == 0 {
			if focusedL {
				l = ui.Fit(ui.Pill(ui.FillKindViolet, ui.L("STORAGE"))+" "+model.HumanBytes(m.TotalSize), leftW)
			}
			if !focusedL {
				r = ui.Fit(ui.Pill(ui.FillKindViolet, ui.L("CALLOUTS")), rightW)
			}
		}
		lines = append(lines, ui.Fit(l+strings.Repeat(" ", gutter)+r, width))
	}
	return lines
}

func heatLines(d derived, width int, large bool) []string {
	lines := ui.Heatmap(d.heat, width, large)
	for i, ln := range lines {
		lines[i] = ui.Fit(ln, width)
	}
	return lines
}

func legacyMemory(m *model.Model, mem model.MemoryEstimate, width int) []string {
	var lines []string
	add := func(s string) { lines = append(lines, ui.Fit(s, width)) }
	add("Memory  weights " + ui.Geq() + " " + model.HumanBytes(mem.WeightsBytes) + " (" + model.HumanDecimalGB(mem.WeightsBytes) + ")")
	if hasKind(m, model.KindVision) || hasKind(m, model.KindMTP) {
		add("  text-only " + ui.Geq() + " " + model.HumanBytes(mem.TextOnlyBytes))
	}
	if mem.HostCapableBytes >= model.GiB {
		bytes := model.HumanBytes(mem.HostCapableBytes)
		line := ui.G().Host + " of which " + bytes + " are lookup tables that can stay in CPU RAM"
		if mem.HostCapablePartial {
			line = ui.G().Host + " of which " + ui.Geq() + " " + bytes + " are lookup tables that can stay in CPU RAM (incomplete)"
		}
		add("  " + line)
		// The subtraction is a lower bound only when the host total is exact.
		// A partial host figure is itself a lower bound; subtracting it can
		// overstate the accelerator remainder, so the line is omitted.
		if !mem.HostCapablePartial {
			add("    (heuristic) " + ui.RArrow() + " " + ui.Geq() + " " + model.HumanBytes(mem.AcceleratorMinBytes) + " on the GPU/accelerator")
		}
	}
	if mem.UnifiedMemory {
		add("  unified memory (MLX): CPU and GPU share RAM, total is unchanged")
	}
	add("  weights only: no KV cache, activations or runtime overhead")
	add("  " + mem.Note)
	if mem.HostCapableBytes > 0 {
		add("  " + mem.HostCapableNote)
	}
	return lines
}

func hasKind(m *model.Model, kind string) bool {
	for _, g := range m.Groups {
		if g.Kind == kind {
			return true
		}
	}
	return false
}

func paintPlain(s, fg, bg string, bold bool) string {
	if ui.ColorOff() || s == "" {
		return s
	}
	return ui.Paint(s, fg, bg, bold)
}
