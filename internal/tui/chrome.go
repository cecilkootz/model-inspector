package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
	"origin.cursor.com/cecilkootz/model-inspector/internal/sanitize"
	"origin.cursor.com/cecilkootz/model-inspector/internal/scan"
)

func (t Model) headerLine(width int) string {
	m := t.m
	name := m.RepoID
	if name == "" {
		name = filepath.Base(m.Dir)
	}
	name = sanitize.From(name).String()
	if width < 100 {
		if i := strings.Index(name, "/"); i >= 0 {
			name = name[i+1:]
		}
	}
	size := model.HumanBytes(m.TotalSize)
	if incomplete(m) {
		size += " (incomplete)"
	}
	var parts []string
	if strings.HasPrefix(m.Format, "gguf") {
		parts = []string{size, "gguf · not supported yet"}
	} else {
		parts = []string{compactFormat(m.Format), size, "~" + model.HumanCount(m.TotalLogicalParams), quantHeader(m)}
	}
	// Drop lower-priority fields once the name has been squeezed away.
	// Keep order: format, size, params, quant. Drop format, then quant, then params.
	drop := []int{0, 3, 2}
	for {
		right := strings.Join(nonEmpty(parts), "  ")
		room := width - lipgloss.Width(right)
		if right != "" {
			room -= 2
		}
		if room >= 1 || len(parts) == 0 {
			if room < 1 {
				return truncWidth(right, width)
			}
			name = middleTrunc(name, room)
			gap := width - lipgloss.Width(name) - lipgloss.Width(right)
			if gap < 1 {
				gap = 1
			}
			if right == "" {
				return fitLine(name, width)
			}
			return name + strings.Repeat(" ", gap) + right
		}
		if len(drop) == 0 {
			return truncWidth(right, width)
		}
		idx := drop[0]
		drop = drop[1:]
		if idx < len(parts) {
			parts[idx] = ""
		}
	}
}

func nonEmpty(ss []string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func compactFormat(format string) string {
	name, variant, _ := model.FormatName(format)
	if variant == "" {
		return sanitize.From(name).String()
	}
	return sanitize.From(name + "·" + variant).String()
}

func quantHeader(m *model.Model) string {
	if m.Quant == nil || m.Quant.Bits <= 0 {
		d := model.DisplayDType(dominantDType(m))
		if d == "" {
			return "—"
		}
		return sanitize.From(d).String()
	}
	s := fmt.Sprintf("q%d", m.Quant.Bits)
	if m.Quant.GroupSize > 0 {
		s += fmt.Sprintf(" g%d", m.Quant.GroupSize)
	}
	if m.Quant.NumOverrides > 0 {
		s += fmt.Sprintf(" +%d", m.Quant.NumOverrides)
	}
	return sanitize.From(s).String()
}

func dominantDType(m *model.Model) string {
	var best string
	var n int64 = -1
	for _, k := range model.SortedKeys(m.DTypes) {
		if m.DTypes[k].Bytes > n {
			n = m.DTypes[k].Bytes
			best = k
		}
	}
	return best
}

// incomplete is the header marker. It follows Model.Partial, which the
// loader sets for a missing, unreadable, or non-local shard as well as
// for a walk cap, a bad config, and a counter overflow.
func incomplete(m *model.Model) bool {
	return m != nil && m.Partial
}

func (t Model) tabLine(width int) string {
	var parts []string
	for i, n := range tabNames {
		label := n
		if tab(i) == tabLayers && t.detail != nil {
			label = "3 Layers › " + sanitize.From(t.detail.Key).String()
		}
		if tab(i) == tabScan {
			label += " " + scanBadge(t.scan)
		}
		if tab(i) == t.active {
			text := "[" + label + "]"
			if colorOff() {
				parts = append(parts, text)
			} else {
				parts = append(parts, activeTabStyle.Render(text))
			}
			continue
		}
		if colorOff() {
			parts = append(parts, label)
		} else {
			parts = append(parts, tabStyle.Render(label))
		}
	}
	return strings.Join(parts, "  ")
}

func scanBadge(res scan.Result) string {
	switch res.Overall {
	case scan.StatusFail:
		return g.fail + fmt.Sprint(effectiveCount(res, scan.StatusFail))
	case scan.StatusWarn:
		return g.warnMark + fmt.Sprint(effectiveCount(res, scan.StatusWarn))
	case scan.StatusPass:
		return g.pass
	default:
		return g.nr
	}
}

func effectiveCount(res scan.Result, want scan.Status) int {
	n := 0
	for _, c := range res.Checks {
		if c.EffectiveStatus == want {
			n++
		}
	}
	if n == 0 {
		return 1
	}
	return n
}

func (t Model) footerLine(width int) string {
	keys := t.footerKeys()
	status := t.footerStatus()
	// A notice is the thing the user just asked to see. Shrink the keys
	// before cutting the message. Other status text yields first.
	if width < 70 || (t.notice != "" && lipgloss.Width(keys)+1+lipgloss.Width(status) > width) {
		keys = dropHelpQuit(keys)
	}
	if lipgloss.Width(keys)+1+lipgloss.Width(status) > width {
		if t.notice != "" {
			room := width - lipgloss.Width(status) - 1
			if room < 1 {
				return truncWidth(status, width)
			}
			keys = truncWidth(keys, room)
		} else {
			status = truncWidth(status, max(1, width/3))
		}
	}
	gap := width - lipgloss.Width(keys) - lipgloss.Width(status)
	if gap < 0 {
		gap = 0
	}
	if gap < 1 {
		if t.notice != "" && lipgloss.Width(status) <= width {
			return truncWidth(status, width)
		}
		return truncWidth(keys, width)
	}
	if keys == "" {
		return fitLine(status, width)
	}
	return keys + strings.Repeat(" ", gap) + status
}

func (t Model) footerKeys() string {
	if t.help {
		return "? close  esc back  q quit"
	}
	switch t.active {
	case tabScan:
		return "↑↓ select  ⏎ details  c code  ? help  q quit"
	case tabLayers:
		if t.detail != nil {
			return "↑↓ select  esc back  ? help  q quit"
		}
		return "↑↓ select  ⏎ open  ? help  q quit"
	default:
		return "↑↓ scroll  ? help  q quit"
	}
}

func dropHelpQuit(keys string) string {
	keys = strings.ReplaceAll(keys, "  ? help", "")
	keys = strings.ReplaceAll(keys, "  q quit", "")
	keys = strings.ReplaceAll(keys, "? help  ", "")
	keys = strings.ReplaceAll(keys, "q quit", "")
	return strings.TrimSpace(keys)
}

func (t Model) footerStatus() string {
	if t.notice != "" {
		return t.notice
	}
	switch t.active {
	case tabScan:
		return fmt.Sprintf("%d checks", len(t.scan.Checks))
	case tabLayers:
		if t.detail != nil {
			return fmt.Sprintf("%d tensors", len(t.detail.Tensors))
		}
		return fmt.Sprintf("%d groups", len(t.m.Groups))
	case tabIdentity:
		n := len(t.m.Outliers())
		if n == 0 {
			return "identity"
		}
		return fmt.Sprintf("%d callouts", n)
	default:
		return "config"
	}
}

func (t Model) helpText(width, height int) string {
	rows := []string{
		"Keys",
		"1-4 tab/S-tab ←/→  switch screen",
		"↑↓ j/k pgup/pgdn   move",
		"⏎ open   esc back",
		"/ filter  s/S sort  o outliers   (later)",
		"r raw JSON (Config)   (later)",
		"c code & pickles (Scan)",
		"? close   q quit",
		"",
		"Terms",
		"~ / est.   estimated, not measured",
		"Params     packed q-weights counted at dequantized size",
		"weights only   no KV cache, activations, or overhead",
		g.host + "  lookup table that can stay in CPU RAM (heuristic)",
		"GiB = 2^30 B    GB = 10^9 B",
	}
	inner := width - 4
	if inner < 10 {
		inner = 10
	}
	var b strings.Builder
	top := g.tl + strings.Repeat(g.hbar, min(inner, 20)) + " help " + strings.Repeat(g.hbar, max(0, inner-27)) + g.tr
	b.WriteString(truncWidth(top, width))
	b.WriteByte('\n')
	maxBody := height - 2
	if maxBody < 1 {
		maxBody = 1
	}
	shown := 0
	for _, row := range rows {
		if shown >= maxBody {
			break
		}
		line := g.vbar + " " + row
		b.WriteString(truncWidth(line, width-1) + g.vbar)
		b.WriteByte('\n')
		shown++
	}
	bot := g.bl + strings.Repeat(g.hbar, max(0, inner)) + g.br
	b.WriteString(truncWidth(bot, width))
	return b.String()
}

func (t Model) tooSmall() string {
	msg := fmt.Sprintf("model-inspector needs at least 60×16 (now %d×%d).", t.width, t.height)
	msg2 := "Enlarge the window or run: model-inspector inspect <path>"
	return msg + "\n" + msg2 + "\n\nq quit"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
