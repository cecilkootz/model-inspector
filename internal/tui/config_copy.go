package tui

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aymanbagabas/go-osc52/v2"
	"github.com/muesli/termenv"

	"origin.cursor.com/cecilkootz/model-inspector/internal/jsonx"
	"origin.cursor.com/cecilkootz/model-inspector/internal/sanitize"
)

func (t *Model) copyConfig(value bool) {
	c := t.cfg
	if c == nil {
		return
	}
	payload, kind := t.copyPayload(value)
	if payload == "" && kind == "" {
		t.notice = "nothing to copy"
		return
	}
	if value && c.tree != nil {
		id := t.copyNode()
		if id >= 0 {
			if full, ok := c.tree.FullBytes[id]; ok && full > configCopyMax {
				t.notice = fmt.Sprintf("too large to copy (%d KiB); press r for the raw view", (full+512)/1024)
				return
			}
		}
	}
	if len(payload) > configCopyMax {
		kb := (len(payload) + 512) / 1024
		t.notice = fmt.Sprintf("too large to copy (%d KiB); press r for the raw view", kb)
		return
	}
	n := controlCount(payload)
	text := payload
	if n > 0 {
		text = sanitize.From(payload).String()
		t.notice = fmt.Sprintf("copied with %d control characters shown as tokens", n)
	} else if kind == "path" {
		t.notice = "copied path" + cfgSep() + clipCells(payload, 80) + " (if your terminal blocks OSC 52, select it from the value strip)"
	} else {
		t.notice = "copied value" + cfgSep() + clipCells(payload, 80) + " (if your terminal blocks OSC 52, select it from the value strip)"
	}
	if !t.canCopy() {
		if n == 0 {
			t.notice = "not copied (terminal blocked OSC 52); select it from the value strip"
		}
		return
	}
	writeOSC(t.copyWriter(), text)
}

func (t Model) canCopy() bool {
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

func controlCount(s string) int {
	n := 0
	for _, r := range s {
		if sanitize.From(string(r)).String() != string(r) {
			n++
		}
	}
	return n
}

func (t Model) copyPayload(value bool) (string, string) {
	c := t.cfg
	if !t.treePane() && !c.raw {
		lines := t.fieldLines()
		if c.fieldCur < 0 || c.fieldCur >= len(lines) || lines[c.fieldCur].kind != fieldRow {
			return "", ""
		}
		f := lines[c.fieldCur]
		if f.jump < 0 || c.tree == nil {
			if value {
				return f.value, "value"
			}
			return f.label, "path"
		}
		if value {
			return nodeValue(c, f.jump), "value"
		}
		return displayPath(c, f.jump), "path"
	}
	id := c.cursor
	if c.tree == nil || id < 0 || int(id) >= len(c.tree.Nodes) {
		return "", ""
	}
	if value {
		return nodeValue(c, id), "value"
	}
	return displayPath(c, id), "path"
}

func (t Model) copyNode() int32 {
	c := t.cfg
	if c == nil || c.tree == nil {
		return -1
	}
	if !t.treePane() && !c.raw {
		lines := t.fieldLines()
		if c.fieldCur >= 0 && c.fieldCur < len(lines) {
			return lines[c.fieldCur].jump
		}
		return -1
	}
	return c.cursor
}

func displayPath(c *configState, id int32) string {
	p := c.path(id)
	if p == "" {
		return "."
	}
	return p
}

func nodeValue(c *configState, id int32) string {
	if c.tree == nil || id < 0 || int(id) >= len(c.tree.Nodes) {
		return ""
	}
	n := c.tree.Nodes[id]
	switch n.Kind {
	case jsonx.KindString:
		return n.Lit
	case jsonx.KindNumber, jsonx.KindBool, jsonx.KindNull:
		return n.Lit
	default:
		return compactJSON(c, id, configCopyMax+1)
	}
}

func compactJSON(c *configState, id int32, limit int) string {
	var b strings.Builder
	writeCompact(&b, c, id, limit)
	return b.String()
}

func writeCompact(b *strings.Builder, c *configState, id int32, limit int) {
	if b.Len() > limit {
		return
	}
	n := c.tree.Nodes[id]
	switch n.Kind {
	case jsonx.KindString:
		b.WriteString(strconvQuote(n.Lit))
	case jsonx.KindNumber, jsonx.KindBool, jsonx.KindNull:
		if n.Lit == "" {
			b.WriteString("null")
		} else {
			b.WriteString(n.Lit)
		}
	case jsonx.KindArray:
		b.WriteByte('[')
		first := true
		for ch := n.First; ch >= 0; ch = c.tree.Nodes[ch].Next {
			if b.Len() > limit {
				return
			}
			if !first {
				b.WriteByte(',')
			}
			first = false
			writeCompact(b, c, ch, limit)
		}
		b.WriteByte(']')
	case jsonx.KindObject:
		b.WriteByte('{')
		first := true
		for ch := n.First; ch >= 0; ch = c.tree.Nodes[ch].Next {
			if b.Len() > limit {
				return
			}
			if !first {
				b.WriteByte(',')
			}
			first = false
			b.WriteString(strconvQuote(c.tree.Nodes[ch].Key))
			b.WriteByte(':')
			writeCompact(b, c, ch, limit)
		}
		b.WriteByte('}')
	}
}

func strconvQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
