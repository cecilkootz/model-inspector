package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/report"
	"github.com/cecilkootz/model-inspector/internal/scan"
)

// loadModel loads the directory. Tests replace it.
var loadModel = model.LoadWithProgress

type scanProgMsg struct{ p model.Progress }
type scanLoadedMsg struct {
	m   *model.Model
	res scan.Result
	err error
}

// RunLoading opens the TUI immediately and loads dir inside it.
// Quitting from the load-error card returns 3. Every other quit returns 0.
func RunLoading(dir string, strict, fromCache bool, repo, rev string, opt ...Options) (code int, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			restoreTerminal()
			err = fmt.Errorf("tui panic: %v", rec)
			code = 1
		}
	}()
	t := NewLoading(dir, strict, fromCache, repo, rev)
	if len(opt) > 0 {
		t.apply(opt[0])
	}
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if t.mouse {
		opts = append(opts, tea.WithMouseCellMotion())
	}
	final, err := tea.NewProgram(t, opts...).Run()
	if err != nil && strings.Contains(err.Error(), "panic") {
		restoreTerminal()
		return 1, err
	}
	if err != nil {
		return 1, err
	}
	if m, ok := final.(Model); ok {
		return m.exitCode(), nil
	}
	return 0, nil
}

// NewLoading builds a Model that loads dir on Init.
func NewLoading(dir string, strict, fromCache bool, repo, rev string) Model {
	t := New(nil, scan.Result{})
	t.scn.loading = true
	t.scn.dir = dir
	t.scn.strict = strict
	t.scn.fromCache = fromCache
	t.scn.repo = repo
	t.scn.rev = rev
	return t
}

func (t Model) exitCode() int {
	if t.scn != nil && t.scn.loadErr != "" {
		return 3
	}
	return 0
}

func (t Model) loadCmds() tea.Cmd {
	if t.scn == nil || !t.scn.loading {
		return nil
	}
	ch := make(chan model.Progress, 64)
	t.scn.progCh = ch
	return tea.Batch(
		waitScanProg(ch),
		runScanLoad(t),
	)
}

func waitScanProg(ch <-chan model.Progress) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return nil
		}
		return scanProgMsg{p: p}
	}
}

func runScanLoad(t Model) tea.Cmd {
	return func() tea.Msg {
		if t.scn == nil {
			return scanLoadedMsg{err: fmt.Errorf("load state was not initialised")}
		}
		m, err := loadModel(t.scn.dir, t.scn.progCh)
		// The scan reports its layout pass on the same channel, so it stays
		// open until the checks finish. Detaching first means a later
		// re-scan of this model cannot send on the closed channel.
		defer func() {
			m.DetachProgress()
			if t.scn.progCh != nil {
				close(t.scn.progCh)
			}
		}()
		if err != nil {
			return scanLoadedMsg{err: err}
		}
		if t.scn.repo != "" || t.scn.rev != "" {
			m.RepoID, m.Revision = t.scn.repo, t.scn.rev
		}
		res := finishScan(m, t.scn.strict, t.scn.fromCache)
		return scanLoadedMsg{m: m, res: res}
	}
}

// finishScan runs the checks and the same partial refresh the text and JSON
// paths use, so the TUI verdict matches report.ExitCode.
func finishScan(m *model.Model, strict, fromCache bool) scan.Result {
	opt := scan.Options{Strict: strict}
	res := scan.RunOptions(m, opt)
	doc := report.Build(m, res, fromCache, 0)
	if m != nil && m.Partial && res.Overall != scan.StatusFail {
		res = scan.RunOptions(m, opt)
	}
	if doc.Scan.Overall == string(scan.StatusFail) && res.Overall != scan.StatusFail {
		res = scan.RunOptions(m, opt)
	}
	return res
}

func (t Model) onScanLoaded(msg scanLoadedMsg) (tea.Model, tea.Cmd) {
	if t.scn == nil {
		t.scn = &scanView{}
	}
	t.scn.loading = false
	if msg.err != nil {
		t.scn.loadErr = clipScan(msg.err.Error())
		t.touchScan()
		return t, nil
	}
	t.m = msg.m
	t.scan = msg.res
	if t.sh != nil {
		// A detail pane opened over the loading stack is not the user's pick
		// of a loaded panel.
		t.sh.inDetail = false
	}
	if t.m != nil {
		d := derive(t.m)
		t.derived = &d
	}
	t.touchScan()
	return t, nil
}

func (t *Model) touchScan() {
	if t.cache != nil {
		t.cache.key = ""
	}
}

// copyScan copies the selected check as plain text.
func (t *Model) copyScan() {
	var payload string
	if t.scanCursor >= 0 && t.scanCursor < len(t.scan.Checks) {
		payload = t.cardPlain(t.scan.Checks[t.scanCursor])
	}
	if payload == "" {
		t.notice = "nothing to copy"
		return
	}
	if t.clipboardOff() {
		t.notice = "not copied (OSC 52 disabled); select it from the value strip"
		return
	}
	if !t.canCopy() {
		t.notice = "not copied (terminal blocked OSC 52); select it from the value strip"
		return
	}
	writeOSC(t.copyWriter(), payload)
	t.notice = "copied check"
}

func (t Model) cardPlain(c scan.Check) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n%s\n", c.Name, stripANSI(statusPill(c)), clipScan(c.Summary))
	if why := scan.Why(c.ID); why != "" {
		fmt.Fprintf(&b, "Why it matters: %s\n", why)
	}
	if rule := scan.Rule(c.ID); rule != "" {
		fmt.Fprintf(&b, "Rule: %s\n", rule)
	}
	if np := scan.NotProven(c.ID); np != "" {
		fmt.Fprintf(&b, "Does not prove: %s\n", np)
	}
	for _, d := range c.Details {
		fmt.Fprintf(&b, "  %s\n", clipScan(d))
	}
	return b.String()
}
