// Package tui implements the interactive Bubble Tea interface.
package tui

import (
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
	"origin.cursor.com/cecilkootz/model-inspector/internal/sanitize"
	"origin.cursor.com/cecilkootz/model-inspector/internal/scan"
)

type tab int

const (
	tabIdentity tab = iota
	tabConfig
	tabLayers
	tabScan
	numTabs
)

var tabNames = [numTabs]string{"1 Identity", "2 Config", "3 Layers", "4 Scan"}

var (
	titleStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212")).Padding(0, 1)
	tabStyle       = lipgloss.NewStyle().Padding(0, 2).Foreground(lipgloss.Color("245"))
	activeTabStyle = tabStyle.Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62")).Bold(true)
	labelStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Width(26)
	valueStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	headingStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99"))
	helpStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	warnStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	statusStyles   = map[scan.Status]lipgloss.Style{
		scan.StatusPass:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42")),
		scan.StatusWarn:    warnStyle,
		scan.StatusFail:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")),
		scan.StatusSkip:    lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		scan.StatusPlanned: lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
	}
)

// Model is the root Bubble Tea model.
type Model struct {
	m      *model.Model
	scan   scan.Result
	active tab
	width  int
	height int
	ready  bool

	identityVP viewport.Model
	configVP   viewport.Model
	scanVP     viewport.Model

	groupTable  table.Model
	tensorTable table.Model
	detail      *model.Group
}

// Run starts the TUI in the alternate screen.
func Run(m *model.Model, res scan.Result) error {
	_, err := tea.NewProgram(New(m, res), tea.WithAltScreen()).Run()
	return err
}

// New builds the UI for a loaded model.
func New(m *model.Model, res scan.Result) Model {
	t := Model{m: m, scan: res, width: 100, height: 30}
	t.identityVP = viewport.New(t.width, t.bodyHeight())
	t.configVP = viewport.New(t.width, t.bodyHeight())
	t.scanVP = viewport.New(t.width, t.bodyHeight())
	t.identityVP.SetContent(renderIdentity(m))
	t.configVP.SetContent(renderConfig(m))
	t.scanVP.SetContent(renderScan(res))

	styles := table.DefaultStyles()
	styles.Header = styles.Header.Bold(true).Foreground(lipgloss.Color("99"))
	styles.Selected = styles.Selected.Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62"))
	t.groupTable = table.New(table.WithColumns(groupColumns(t.width)), table.WithRows(groupRows(m)),
		table.WithFocused(true), table.WithHeight(t.bodyHeight()), table.WithStyles(styles))
	t.tensorTable = table.New(table.WithColumns(tensorColumns(t.width)),
		table.WithFocused(true), table.WithHeight(t.bodyHeight()-1), table.WithStyles(styles))
	return t
}

func (t Model) Init() tea.Cmd { return nil }

func (t Model) bodyHeight() int {
	if h := t.height - 4; h > 3 {
		return h
	}
	return 3
}

func (t *Model) resize() {
	h := t.bodyHeight()
	for _, vp := range []*viewport.Model{&t.identityVP, &t.configVP, &t.scanVP} {
		vp.Width, vp.Height = t.width, h
	}
	t.groupTable.SetColumns(groupColumns(t.width))
	t.groupTable.SetWidth(t.width)
	t.groupTable.SetHeight(h)
	t.tensorTable.SetColumns(tensorColumns(t.width))
	t.tensorTable.SetWidth(t.width)
	t.tensorTable.SetHeight(h - 1)
}

func (t Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		t.width, t.height, t.ready = msg.Width, msg.Height, true
		t.resize()
		return t, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return t, tea.Quit
		case "tab", "right", "l":
			t.active = (t.active + 1) % numTabs
			return t, nil
		case "shift+tab", "left", "h":
			t.active = (t.active + numTabs - 1) % numTabs
			return t, nil
		case "1", "2", "3", "4":
			t.active = tab(msg.String()[0] - '1')
			return t, nil
		}
		if t.active == tabLayers {
			switch msg.String() {
			case "enter":
				if t.detail == nil {
					if i := t.groupTable.Cursor(); i >= 0 && i < len(t.m.Groups) {
						t.detail = t.m.Groups[i]
						t.tensorTable.SetRows(tensorRows(t.detail))
						t.tensorTable.SetCursor(0)
					}
				}
				return t, nil
			case "esc", "backspace":
				t.detail = nil
				return t, nil
			}
		}
	}

	var cmd tea.Cmd
	switch t.active {
	case tabIdentity:
		t.identityVP, cmd = t.identityVP.Update(msg)
	case tabConfig:
		t.configVP, cmd = t.configVP.Update(msg)
	case tabLayers:
		if t.detail != nil {
			t.tensorTable, cmd = t.tensorTable.Update(msg)
		} else {
			t.groupTable, cmd = t.groupTable.Update(msg)
		}
	case tabScan:
		t.scanVP, cmd = t.scanVP.Update(msg)
	}
	return t, cmd
}

func (t Model) View() string {
	if !t.ready {
		return "loading..."
	}
	name := t.m.RepoID
	if name == "" {
		name = filepath.Base(t.m.Dir)
	}
	name = sanitize.From(name).String()
	var tabs []string
	for i, n := range tabNames {
		if tab(i) == t.active {
			tabs = append(tabs, activeTabStyle.Render(n))
		} else {
			tabs = append(tabs, tabStyle.Render(n))
		}
	}
	header := titleStyle.Render("model-inspector · "+name) + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, tabs...)

	var body, help string
	switch t.active {
	case tabIdentity:
		body, help = t.identityVP.View(), "↑/↓ scroll"
	case tabConfig:
		body, help = t.configVP.View(), "↑/↓ scroll"
	case tabLayers:
		if t.detail != nil {
			body = headingStyle.Render(sanitize.From(fmt.Sprintf("%s · %d tensors · %s", t.detail.Key, len(t.detail.Tensors), model.HumanBytes(t.detail.Bytes))).String()) +
				"\n" + t.tensorTable.View()
			help = "↑/↓ select • esc back"
		} else {
			body, help = t.groupTable.View(), "↑/↓ select • enter drill in"
		}
	case tabScan:
		body, help = t.scanVP.View(), "↑/↓ scroll"
	}
	return header + "\n" + body + "\n" + helpStyle.Render(help+" • tab/←/→/1-4 switch • q quit")
}

func kv(label string, value sanitize.Text) string {
	return labelStyle.Render(sanitize.From(label).String()) + valueStyle.Render(value.String()) + "\n"
}

func text(s string) sanitize.Text { return sanitize.From(s) }

func textf(format string, args ...any) sanitize.Text {
	return sanitize.From(fmt.Sprintf(format, args...))
}

func renderIdentity(m *model.Model) string {
	var b strings.Builder
	b.WriteString(headingStyle.Render("Identity") + "\n")
	b.WriteString(kv("Path", text(m.Dir)))
	b.WriteString(kv("Repo id", text(orDash(m.RepoID))))
	b.WriteString(kv("Revision", text(orDash(m.Revision))))
	b.WriteString(kv("Format", text(m.Format)))
	b.WriteString(kv("Size on disk (stat)", textf("%s (%d bytes)", model.HumanBytes(m.TotalSize), m.TotalSize)))
	b.WriteString(kv("Files", textf("%d", len(m.Files))))
	b.WriteString(kv("Safetensors shards", textf("%d", len(m.Shards))))
	if m.Quant != nil {
		b.WriteString("\n" + headingStyle.Render("Quantization") + "\n")
		b.WriteString(kv("Source", text(m.Quant.Source)))
		b.WriteString(kv("Bits / group size / mode", textf("%d / %d / %s", m.Quant.Bits, m.Quant.GroupSize, orDash(m.Quant.Mode))))
		b.WriteString(kv("Per-module overrides", textf("%d", m.Quant.NumOverrides)))
		for _, bits := range sortedInts(m.BitsHistogram) {
			b.WriteString(kv(fmt.Sprintf("  %d-bit packed weights", bits), textf("%d", m.BitsHistogram[bits])))
		}
	}
	b.WriteString("\n" + headingStyle.Render("Tensors (from headers)") + "\n")
	b.WriteString(kv("Tensors", textf("%d", len(m.Tensors))))
	b.WriteString(kv("Stored elements", text(model.HumanCount(m.TotalElements))))
	b.WriteString(kv("Params (dequantized est.)", text(model.HumanCount(m.TotalLogicalParams))))
	b.WriteString(kv("Tensor bytes", text(model.HumanBytes(m.TensorBytes))))
	layers, experts := m.LayerCount()
	b.WriteString(kv("Decoder layers", textf("%d (%d with routed experts)", layers, experts)))
	for _, dt := range model.SortedKeys(m.DTypes) {
		d := m.DTypes[dt]
		b.WriteString(kv("  dtype "+dt, textf("%d tensors, %s elems, %s", d.Tensors, model.HumanCount(d.Elements), model.HumanBytes(d.Bytes))))
	}
	if len(m.Warnings) > 0 {
		b.WriteString("\n" + warnStyle.Render("Warnings") + "\n")
		for _, w := range m.Warnings {
			b.WriteString("  " + sanitize.From(w).String() + "\n")
		}
	}
	return b.String()
}

func renderConfig(m *model.Model) string {
	if m.Config == nil {
		if len(m.ConfigRaw) == 0 {
			return "no config.json found"
		}
		return "config.json could not be parsed\n" + sanitize.From(string(m.ConfigRaw)).String()
	}
	var b strings.Builder
	b.WriteString(headingStyle.Render("Key fields") + "\n")
	for _, f := range m.KeyConfig() {
		b.WriteString(kv(f.Path, text(compact(f.Value))))
	}
	if lt := m.LayerTypeCounts(); lt != nil {
		for _, k := range model.SortedKeys(lt) {
			b.WriteString(kv("layer_types: "+k, textf("%d", lt[k])))
		}
	}
	if m.Quant != nil {
		b.WriteString(kv(m.Quant.Source, textf("bits=%d group_size=%d mode=%s", m.Quant.Bits, m.Quant.GroupSize, m.Quant.Mode)))
	}
	b.WriteString("\n" + headingStyle.Render("config.json (raw)") + "\n")
	pretty, err := sanitize.JSON(m.Config)
	if err != nil {
		b.WriteString(sanitize.From(string(m.ConfigRaw)).String())
	} else {
		b.WriteString(pretty)
	}
	return b.String()
}

func renderScan(r scan.Result) string {
	var b strings.Builder
	b.WriteString(warnStyle.Render("⚠ "+r.Disclaimer) + "\n\n")
	b.WriteString(labelStyle.Render("Overall") + statusStyles[r.Overall].Render(strings.ToUpper(string(r.Overall))) + "\n")
	for _, c := range r.Checks {
		b.WriteString("\n" + statusStyles[c.Status].Render(fmt.Sprintf("[%s]", strings.ToUpper(string(c.Status)))) +
			" " + headingStyle.Render(c.Name) + "\n  " + sanitize.From(c.Summary).String() + "\n")
		for _, d := range c.Details {
			b.WriteString("    - " + sanitize.From(d).String() + "\n")
		}
	}
	return b.String()
}

func groupColumns(width int) []table.Column {
	cols := []table.Column{
		{Title: "Group", Width: 0}, {Title: "Kind", Width: 10}, {Title: "Tensors", Width: 8},
		{Title: "Params*", Width: 9}, {Title: "Bytes", Width: 11}, {Title: "Experts", Width: 7}, {Title: "QWeights", Width: 8},
	}
	return flex(cols, width, 12)
}

func tensorColumns(width int) []table.Column {
	cols := []table.Column{
		{Title: "Tensor", Width: 0}, {Title: "DType", Width: 6}, {Title: "Shape", Width: 20},
		{Title: "Bytes", Width: 11}, {Title: "Role", Width: 8},
	}
	return flex(cols, width, 20)
}

// flex gives column 0 whatever width remains (cells have 1 char padding each side).
func flex(cols []table.Column, width, minW int) []table.Column {
	used := 0
	for _, c := range cols[1:] {
		used += c.Width
	}
	w := width - used - 2*len(cols)
	if w < minW {
		w = minW
	}
	cols[0].Width = w
	return cols
}

func groupRows(m *model.Model) []table.Row {
	rows := make([]table.Row, 0, len(m.Groups))
	for _, g := range m.Groups {
		exp := ""
		if g.HasExperts {
			exp = "yes"
		}
		rows = append(rows, table.Row{
			sanitize.From(g.Key).String(), sanitize.From(g.Kind).String(), fmt.Sprint(len(g.Tensors)), model.HumanCount(g.LogicalParams),
			model.HumanBytes(g.Bytes), exp, fmt.Sprint(g.QuantizedTensors)})
	}
	return rows
}

func tensorRows(g *model.Group) []table.Row {
	rows := make([]table.Row, 0, len(g.Tensors))
	for _, t := range g.Tensors {
		name := t.Name
		if g.Kind == model.KindLayer {
			name = model.ShortName(name)
		}
		role := t.Role
		if t.Role == model.RoleQuantWeight {
			role = fmt.Sprintf("q%d", t.Bits)
		}
		rows = append(rows, table.Row{
			sanitize.From(name).String(), sanitize.From(t.DType).String(), sanitize.From(fmt.Sprint(t.Shape)).String(),
			model.HumanBytes(t.ByteLen()), sanitize.From(role).String()})
	}
	return rows
}

func compact(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func sortedInts(m map[int]int) []int {
	return slices.Sorted(maps.Keys(m))
}
