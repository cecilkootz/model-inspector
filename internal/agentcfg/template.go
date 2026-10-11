package agentcfg

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Template text is never rendered. The functions here look at bytes: they
// split a template into literal text, quoted strings and code, then match
// bounded patterns over those parts. Every pass is linear in the template.

type segKind int

const (
	segLiteral segKind = iota // text outside any block
	segString                 // a quoted string inside {{ }} or {% %}
	segCode                   // the rest of a block, strings excluded
)

type segment struct {
	off  int
	text string
	kind segKind
}

const maxSegments = 200_000

// segments splits text at Jinja delimiters. Quotes inside a block are
// honored, so "}}" in a string does not end the block. Comments are dropped.
func segments(text string) []segment {
	var out []segment
	add := func(off, end int, k segKind) {
		if end > off && len(out) < maxSegments {
			out = append(out, segment{off, text[off:end], k})
		}
	}
	lit, i := 0, 0
	for i < len(text) {
		if text[i] != '{' || i+1 >= len(text) {
			i++
			continue
		}
		var closer string
		switch text[i+1] {
		case '{':
			closer = "}}"
		case '%':
			closer = "%}"
		case '#':
			closer = "#}"
		default:
			i++
			continue
		}
		add(lit, i, segLiteral)
		if closer == "#}" {
			end := strings.Index(text[i+2:], closer)
			if end < 0 {
				return out
			}
			i += 2 + end + 2
			lit = i
			continue
		}
		i = block(text, i+2, closer, add)
		lit = i
	}
	add(lit, len(text), segLiteral)
	return out
}

// block scans a block body from j and returns the index after its closer.
func block(text string, j int, closer string, add func(off, end int, k segKind)) int {
	codeStart := j
	for j < len(text) {
		c := text[j]
		switch {
		case c == '\'' || c == '"':
			add(codeStart, j, segCode)
			k := j + 1
			for k < len(text) && text[k] != c {
				if text[k] == '\\' {
					k++
				}
				k++
			}
			end := min(k, len(text))
			add(j+1, end, segString)
			j = end + 1
			codeStart = min(j, len(text))
		case strings.HasPrefix(text[j:], closer):
			add(codeStart, j, segCode)
			return j + 2
		default:
			j++
		}
	}
	add(codeStart, len(text), segCode)
	return len(text)
}

// Kind names a heuristic hit or a finding. The values are program text.
type Kind string

const (
	KindInstruction Kind = "instruction-like literal text"
	KindURL         Kind = "URL in template"
	KindToolName    Kind = "hard-coded tool name"
	KindInvisible   Kind = "invisible or control characters"
	KindBase64      Kind = "base64-looking run"
	KindEmail       Kind = "email-like text"
	KindIP          Kind = "IP literal"
)

// Hit is one heuristic match: the byte offset in the scanned text and a
// clipped window around it. Snippet is raw template text.
type Hit struct {
	Kind    Kind
	Offset  int
	Count   int
	Snippet string
	// Phrases are the distinct instruction-like phrases behind one
	// KindInstruction hit, by first offset. Program text.
	Phrases []string
}

const (
	snippetRunes = 120
	maxHitsKind  = 8
)

// snippet returns a window of text around [off, off+n), at most 120 runes,
// with line breaks written as \n so one hit stays on one line.
func snippet(text string, off, n int) string {
	start := max(0, off-24)
	for start > 0 && !utf8.RuneStart(text[start]) {
		start--
	}
	end := min(len(text), off+n+96)
	for end < len(text) && !utf8.RuneStart(text[end]) {
		end++
	}
	var b strings.Builder
	runes := 0
	for _, r := range text[start:end] {
		esc := ""
		switch r {
		case '\n':
			esc = `\n`
		case '\r':
			esc = `\r`
		case '\t':
			esc = `\t`
		}
		n := 1
		if esc != "" {
			n = 2
		}
		if runes+n > snippetRunes {
			break
		}
		if esc != "" {
			b.WriteString(esc)
		} else {
			b.WriteRune(r)
		}
		runes += n
	}
	return b.String()
}

var (
	reEmail  = regexp.MustCompile(`[A-Za-z0-9._%+-]{1,64}@[A-Za-z0-9-]{1,63}(?:\.[A-Za-z0-9-]{1,63})*\.[A-Za-z]{2,24}`)
	reIPv4   = regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`)
	reIPv6   = regexp.MustCompile(`\b(?:[0-9A-Fa-f]{1,4}:){4,7}[0-9A-Fa-f]{1,4}\b`)
	reBase64 = regexp.MustCompile(`[A-Za-z0-9+/=]{40,}`)
	reTool   = regexp.MustCompile(`\\?"name\\?"\s*:\s*\\?"[A-Za-z_]`)
)

// instructionPhrases are matched case-insensitively on word boundaries in
// literal text and string literals. Phrases ending in * are prefixes.
var instructionPhrases = []string{
	"always", "never", "you must", "do not tell", "secretly", "ignore",
	"send", "post", "fetch", "call the", "api key", "api_key", "apikey",
	"password", "token", "credential*", "exfiltrat*", "curl", "wget",
}

// Heuristics runs every tamper heuristic over text once. It is what the
// scan check uses and what the fuzz target drives; it never fails.
func Heuristics(text string) []Hit {
	segs := segments(text)
	var hits []Hit
	hits = append(hits, instructionHits(segs)...)
	hits = append(hits, urlHits(text)...)
	hits = append(hits, toolNameHits(segs)...)
	hits = append(hits, invisibleHits(text)...)
	hits = append(hits, base64Hits(text)...)
	hits = append(hits, emailHits(text)...)
	hits = append(hits, ipHits(text)...)
	return hits
}

func isLetter(b byte) bool { return b|0x20 >= 'a' && b|0x20 <= 'z' }

// foldASCII lowercases ASCII and turns every whitespace byte into a space,
// keeping byte offsets.
func foldASCII(s string) []byte {
	b := []byte(s)
	for i, c := range b {
		switch {
		case c >= 'A' && c <= 'Z':
			b[i] = c + 32
		case c == '\n' || c == '\r' || c == '\t':
			b[i] = ' '
		}
	}
	return b
}

// instructionHits returns at most one hit: the first matching phrase's
// window, with every distinct phrase listed.
func instructionHits(segs []segment) []Hit {
	found := make([]bool, len(instructionPhrases))
	type match struct {
		off     int
		phrase  string
		snippet string
	}
	var matches []match
	for _, sg := range segs {
		if sg.kind == segCode {
			continue
		}
		low := string(foldASCII(sg.text))
		for pi, ph := range instructionPhrases {
			if found[pi] {
				continue
			}
			prefix := strings.HasSuffix(ph, "*")
			word := strings.TrimSuffix(ph, "*")
			for from := 0; from < len(low); {
				i := strings.Index(low[from:], word)
				if i < 0 {
					break
				}
				i += from
				from = i + 1
				end := i + len(word)
				if i > 0 && isIdent(low[i-1]) {
					continue
				}
				if !prefix && end < len(low) && isIdent(low[end]) && !pluralOK(low[end:], ph) {
					continue
				}
				found[pi] = true
				matches = append(matches, match{sg.off + i, word, snippet(sg.text, i, len(word))})
				break
			}
		}
	}
	if len(matches) == 0 {
		return nil
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].off < matches[j].off })
	h := Hit{Kind: KindInstruction, Offset: matches[0].off, Count: len(matches), Snippet: matches[0].snippet}
	for _, m := range matches {
		h.Phrases = append(h.Phrases, m.phrase)
	}
	return []Hit{h}
}

// pluralOK lets "send", "post" and "fetch" match their -s/-es forms.
func pluralOK(rest, phrase string) bool {
	switch phrase {
	case "send", "post", "fetch", "ignore":
		for _, suf := range []string{"s", "es", "ing", "d"} {
			if strings.HasPrefix(rest, suf) && (len(rest) == len(suf) || !isIdent(rest[len(suf)])) {
				return true
			}
		}
	}
	return false
}

func urlHits(text string) []Hit {
	var hits []Hit
	total := 0
	for from := 0; from < len(text); {
		i := strings.Index(text[from:], "://")
		if i < 0 {
			break
		}
		i += from
		from = i + 3
		total++
		if len(hits) >= maxHitsKind {
			continue
		}
		start := i
		for start > 0 && i-start < 16 && (isLetter(text[start-1]) || text[start-1] == '+' || text[start-1] == '-' || text[start-1] == '.') {
			start--
		}
		hits = append(hits, Hit{Kind: KindURL, Offset: start, Count: 1, Snippet: snippet(text, start, i-start+3)})
	}
	return withTotal(hits, total)
}

// withTotal records the full match count on the first hit.
func withTotal(hits []Hit, total int) []Hit {
	if len(hits) > 0 {
		hits[0].Count = total
	}
	return hits
}

func toolNameHits(segs []segment) []Hit {
	var hits []Hit
	total := 0
	for _, sg := range segs {
		if sg.kind == segCode {
			continue
		}
		for _, loc := range reTool.FindAllStringIndex(sg.text, maxHitsKind) {
			total++
			if len(hits) < maxHitsKind {
				hits = append(hits, Hit{Kind: KindToolName, Offset: sg.off + loc[0], Count: 1, Snippet: snippet(sg.text, loc[0], loc[1]-loc[0])})
			}
		}
	}
	return withTotal(hits, total)
}

// isInvisible matches characters that change what a reader sees without
// showing up: C0 controls other than tab and line breaks, C1, format
// characters (zero-width, bidi, tags), variation selectors.
func isInvisible(r rune) bool {
	switch {
	case r < 0x20:
		return r != '\n' && r != '\r' && r != '\t'
	case r == 0x7F, r >= 0x80 && r <= 0x9F:
		return true
	case r >= 0x200B && r <= 0x200F, r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	case r == 0xFEFF, r == 0x2028, r == 0x2029:
		return true
	case r >= 0xFE00 && r <= 0xFE0F, r >= 0xE0100 && r <= 0xE01EF:
		return true
	}
	return unicode.Is(unicode.Cf, r)
}

// HasInvisible reports whether s holds an invisible or control character,
// or bytes that are not UTF-8.
func HasInvisible(s string) bool {
	for _, r := range s {
		if isInvisible(r) || r == utf8.RuneError {
			return true
		}
	}
	return false
}

func invisibleHits(text string) []Hit {
	first, count := -1, 0
	var firstRune rune
	for i, r := range text {
		bad := isInvisible(r)
		if r == utf8.RuneError {
			if _, size := utf8.DecodeRuneInString(text[i:]); size == 1 {
				bad = true
			}
		}
		if !bad {
			continue
		}
		if count++; first < 0 {
			first, firstRune = i, r
		}
	}
	if count == 0 {
		return nil
	}
	h := Hit{Kind: KindInvisible, Offset: first, Count: count, Snippet: snippet(text, first, utf8.RuneLen(firstRune))}
	return []Hit{h}
}

func base64Hits(text string) []Hit {
	var hits []Hit
	total := 0
	for _, loc := range reBase64.FindAllStringIndex(text, -1) {
		run := text[loc[0]:loc[1]]
		if !base64Like(run) {
			continue
		}
		if total++; len(hits) < maxHitsKind {
			hits = append(hits, Hit{Kind: KindBase64, Offset: loc[0], Count: 1, Snippet: snippet(text, loc[0], loc[1]-loc[0])})
		}
	}
	return withTotal(hits, total)
}

// base64Like rejects runs of "=" or "-" padding and one repeated byte.
func base64Like(run string) bool {
	alnum := 0
	for i := 0; i < len(run); i++ {
		if c := run[i]; c >= '0' && c <= '9' || isLetter(c) {
			alnum++
		}
	}
	return alnum >= 32 && strings.Count(run, run[:1]) != len(run)
}

func emailHits(text string) []Hit {
	var hits []Hit
	total := 0
	for _, loc := range reEmail.FindAllStringIndex(text, -1) {
		if total++; len(hits) < maxHitsKind {
			hits = append(hits, Hit{Kind: KindEmail, Offset: loc[0], Count: 1, Snippet: snippet(text, loc[0], loc[1]-loc[0])})
		}
	}
	return withTotal(hits, total)
}

func ipHits(text string) []Hit {
	var hits []Hit
	total := 0
	for _, loc := range reIPv4.FindAllStringIndex(text, -1) {
		if !octetsOK(text[loc[0]:loc[1]]) {
			continue
		}
		if total++; len(hits) < maxHitsKind {
			hits = append(hits, Hit{Kind: KindIP, Offset: loc[0], Count: 1, Snippet: snippet(text, loc[0], loc[1]-loc[0])})
		}
	}
	for _, loc := range reIPv6.FindAllStringIndex(text, -1) {
		if total++; len(hits) < maxHitsKind {
			hits = append(hits, Hit{Kind: KindIP, Offset: loc[0], Count: 1, Snippet: snippet(text, loc[0], loc[1]-loc[0])})
		}
	}
	return withTotal(hits, total)
}

func octetsOK(s string) bool {
	for _, p := range strings.Split(s, ".") {
		n := 0
		for _, c := range p {
			n = n*10 + int(c-'0')
		}
		if n > 255 {
			return false
		}
	}
	return true
}

// ToolInfo is what a template's text says about tool calling. Refs are the
// variable names used in template code; Formats are the tool-call markers
// found anywhere in the text. Format names are program text.
type ToolInfo struct {
	Refs    []string
	Formats []string
}

// Yes reports whether the template has a tool section.
func (t ToolInfo) Yes() bool {
	if len(t.Formats) > 0 {
		return true
	}
	for _, r := range t.Refs {
		if r != "function" {
			return true
		}
	}
	return false
}

// Label is "Hermes-style tool_call tags" and so on, joined with " + ".
func (t ToolInfo) Label() string { return strings.Join(t.Formats, " + ") }

var toolRefs = []string{"tools", "tool_calls", "tool_call", "function", "tool_response"}

type toolFormat struct {
	name  string
	match func(text string, refs []string) bool
}

func has(text string, subs ...string) bool {
	for _, s := range subs {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

var toolFormats = []toolFormat{
	{"Hermes-style tool_call tags", func(t string, _ []string) bool { return has(t, "<tool_call>") }},
	{"Llama 3-style python_tag/ipython", func(t string, _ []string) bool { return has(t, "<|python_tag|>", "ipython") }},
	{"Mistral-style TOOL_CALLS", func(t string, _ []string) bool { return has(t, "[TOOL_CALLS]", "[AVAILABLE_TOOLS]") }},
	{"Gemma-style function markers", func(t string, refs []string) bool {
		return has(t, "<start_of_turn>") && (len(refs) > 0 || has(t, "tool_code"))
	}},
	{"DeepSeek-style tool-call tokens", func(t string, _ []string) bool {
		return has(t, "tool▁calls▁begin", "tool▁call▁begin")
	}},
	{"Phi-style tool tag", func(t string, _ []string) bool { return has(t, "<|tool|>") }},
	{"Granite-style tool_call tag", func(t string, _ []string) bool { return has(t, "<|tool_call|>") }},
}

// DetectTools looks for tool-calling variables in template code and for
// the markers each model family uses.
func DetectTools(text string) ToolInfo {
	var info ToolInfo
	segs := segments(text)
	for _, ref := range toolRefs {
		for _, sg := range segs {
			if sg.kind == segCode && hasIdent(sg.text, ref) {
				info.Refs = append(info.Refs, ref)
				break
			}
		}
	}
	for _, f := range toolFormats {
		if f.match(text, info.Refs) {
			info.Formats = append(info.Formats, f.name)
		}
	}
	if len(info.Formats) == 0 && info.Yes() {
		info.Formats = []string{"generic JSON tools variable"}
	}
	return info
}

func isIdent(b byte) bool { return isLetter(b) || b >= '0' && b <= '9' || b == '_' }

// hasIdent reports whether word occurs in s as a whole identifier.
func hasIdent(s, word string) bool {
	for from := 0; from < len(s); {
		i := strings.Index(s[from:], word)
		if i < 0 {
			return false
		}
		i += from
		from = i + 1
		end := i + len(word)
		if (i == 0 || !isIdent(s[i-1])) && (end == len(s) || !isIdent(s[end])) {
			return true
		}
	}
	return false
}

// DetectReasoning returns the reasoning markers a template mentions.
func DetectReasoning(text string) []string {
	var out []string
	for _, m := range []struct{ name, sub string }{
		{"think tags", "<think>"},
		{"thinking marker", "<|thinking|>"},
		{"enable_thinking", "enable_thinking"},
		{"reasoning_content", "reasoning_content"},
	} {
		if strings.Contains(text, m.sub) {
			out = append(out, m.name)
		}
	}
	return out
}

var systemMarkers = []string{">system", "|system|", "<<SYS>>", "[SYSTEM_PROMPT]", "<|system", "SYSTEM:", "System:"}

// HandlesSystem reports whether the template has a branch for the system
// role: a "system" string, a system_message or system_prompt variable, or a
// family's system header.
func HandlesSystem(text string) bool {
	for _, sg := range segments(text) {
		switch sg.kind {
		case segCode:
			if hasIdent(sg.text, "system_message") || hasIdent(sg.text, "system_prompt") {
				return true
			}
		default:
			if strings.TrimSpace(sg.text) == "system" || has(sg.text, systemMarkers...) {
				return true
			}
		}
	}
	return false
}

// Describe is a hit as one detail line. where names the template.
func (h Hit) Describe(where string) string {
	label := string(h.Kind)
	switch h.Kind {
	case KindInstruction:
		label += " (heuristic, not proof; matched " + strings.Join(h.Phrases, ", ") + ")"
	case KindToolName:
		label += " (a tool the template defines itself)"
	}
	more := ""
	if h.Count > 1 && h.Kind != KindInstruction {
		more = fmt.Sprintf(" (%d matches)", h.Count)
	}
	return fmt.Sprintf("%s in %s at byte %d%s: %s", label, where, h.Offset, more, h.Snippet)
}
