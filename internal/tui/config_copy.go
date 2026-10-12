package tui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/rivo/uniseg"

	"github.com/aymanbagabas/go-osc52/v2"
	"github.com/muesli/termenv"

	"github.com/cecilkootz/model-inspector/internal/sanitize"
)

// copyConfig copies the dotted path of the selected field, or of the tree
// cursor when the detail pane has focus.
func (t *Model) copyConfig() {
	if t.cfg == nil {
		return
	}
	payload := t.copyPayload()
	if payload == "" {
		t.notice = "nothing to copy"
		return
	}
	if len(payload) > configCopyMax {
		kb := (len(payload) + 512) / 1024
		t.notice = fmt.Sprintf("too large to copy (%d KiB); press r for the raw view", kb)
		return
	}
	if t.clipboardOff() {
		t.notice = "not copied (OSC 52 disabled); select it from the value strip"
		return
	}
	n := controlCount(payload)
	text := payload
	if n > 0 {
		text = sanitize.From(payload).String()
		t.notice = fmt.Sprintf("copied with %d control characters shown as tokens", n)
	} else {
		t.notice = "copied path" + cfgSep() + clipCells(payload, 80) + " (if your terminal blocks OSC 52, select it from the value strip)"
	}
	if !t.canCopy() {
		if n == 0 {
			t.notice = "not copied (terminal blocked OSC 52); select it from the value strip"
		}
		return
	}
	writeOSC(t.copyWriter(), text)
}

func (t Model) clipboardOff() bool {
	if t.noClipboard {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MODEL_INSPECTOR_NO_OSC52"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (t Model) canCopy() bool {
	if t.clipboardOff() {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	if t.cfg != nil && t.cfg.copyTTY != nil {
		return *t.cfg.copyTTY
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func (t Model) copyWriter() io.Writer {
	if t.cfg != nil && t.cfg.copyTo != nil {
		return t.cfg.copyTo
	}
	return os.Stdout
}

func writeOSC(w io.Writer, payload string) {
	if os.Getenv("TMUX") != "" {
		_, _ = osc52.New(payload).Tmux().WriteTo(w)
		return
	}
	if strings.HasPrefix(os.Getenv("TERM"), "screen") {
		_, _ = osc52.New(payload).Screen().WriteTo(w)
		return
	}
	termenv.NewOutput(w).Copy(payload)
}

// controlCount counts grapheme clusters whose sanitized form differs from
// the cluster. A multi-rune cluster (base + variation selector, ZWJ
// sequence) is one count, and a lone invalid byte is one count. Counting
// runes inflated a single cluster that held more than one control.
func controlCount(s string) int {
	n := 0
	state := -1
	rest := s
	for rest != "" {
		cluster, next, _, st := uniseg.FirstGraphemeClusterInString(rest, state)
		if cluster == "" {
			_, size := utf8.DecodeRuneInString(rest)
			if size < 1 {
				size = 1
			}
			cluster = rest[:size]
			next = rest[size:]
			st = -1
		}
		if sanitize.From(cluster).String() != cluster {
			n++
		}
		rest = next
		state = st
	}
	return n
}

func (t Model) copyPayload() string {
	c := t.cfg
	if !t.sh.inDetail && !c.raw {
		lines := t.fieldLines()
		if c.fieldCur < 0 || c.fieldCur >= len(lines) || lines[c.fieldCur].kind != fieldRow {
			return ""
		}
		f := lines[c.fieldCur]
		if f.jump < 0 || c.tree == nil {
			return f.label
		}
		return displayPath(c, f.jump)
	}
	id := c.cursor
	if c.tree == nil || id < 0 || int(id) >= len(c.tree.Nodes) {
		return ""
	}
	return displayPath(c, id)
}

func displayPath(c *configState, id int32) string {
	p := c.path(id)
	if p == "" {
		return "."
	}
	return p
}
