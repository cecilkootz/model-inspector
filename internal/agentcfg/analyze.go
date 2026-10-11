package agentcfg

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ConfigFacts is what config.json and the tensors say, supplied by the
// loader. A zero value means unknown.
type ConfigFacts struct {
	VocabSize  int64
	EmbedRows  int64
	MaxPos     int64
	OrigMaxPos int64
	RopeType   string
	RopeFactor float64
	EOS        IDList
	BOS        IDList
}

// Finding kinds that are not template heuristics.
const (
	KindDisagree      Kind = "templates disagree"
	KindOutOfRange    Kind = "added token id out of range"
	KindTokenMismatch Kind = "added tokens differ between files"
	KindStop          Kind = "stop tokens disagree"
	KindCap           Kind = "template over cap"
	KindNotRead       Kind = "file not read"
	KindTokenText     Kind = "invisible characters in token text"
)

// findingRank orders findings, strongest first: text that talks to the
// model or sends data out, then content that hides or differs, then
// structural and coverage problems.
var findingRank = map[Kind]int{
	KindInstruction: 0, KindURL: 1, KindToolName: 2, KindDisagree: 3,
	KindInvisible: 4, KindTokenText: 5, KindBase64: 6, KindEmail: 7, KindIP: 8,
	KindOutOfRange: 9, KindTokenMismatch: 10, KindStop: 11, KindCap: 12, KindNotRead: 13,
}

// Finding is one warning: Kind for the summary, Text for the detail line.
type Finding struct {
	Kind Kind
	Text string
}

// Result is the analysis of a Surface. Facts are informational detail
// lines. Tools is the one-line tool-calling answer.
type Result struct {
	Skip     bool
	Tools    string
	Findings []Finding
	Facts    []string
}

// Analyze derives facts and findings. It reads nothing and runs no
// template; it is a function of s and cfg.
func Analyze(s *Surface, cfg ConfigFacts) Result {
	if !s.Any() {
		return Result{Skip: true}
	}
	var r Result
	r.templateFindings(s)
	r.fileFindings(s)
	r.tokenFindings(s, cfg)
	r.contextFact(s, cfg)
	r.generationFact(s)
	r.specialFact(s)
	r.stopTokens(s, cfg)
	sort.SliceStable(r.Findings, func(i, j int) bool {
		return findingRank[r.Findings[i].Kind] < findingRank[r.Findings[j].Kind]
	})
	return r
}

func (r *Result) warn(k Kind, format string, a ...any) {
	r.Findings = append(r.Findings, Finding{k, fmt.Sprintf(format, a...)})
}

func (r *Result) fact(format string, a ...any) {
	r.Facts = append(r.Facts, fmt.Sprintf(format, a...))
}

func where(t Template) string {
	if t.Name == "" {
		return t.File
	}
	return t.File + " [" + t.Name + "]"
}

func (r *Result) templateFindings(s *Surface) {
	if len(s.Templates) == 0 {
		r.Tools = "tool calling: no template"
		r.fact("chat template: none found")
		return
	}
	seen := map[[32]byte]bool{}
	var tools ToolInfo
	var reasoning []string
	system := false
	var srcs []string
	for _, t := range s.Templates {
		srcs = append(srcs, fmt.Sprintf("%s (%d bytes)", where(t), t.Len))
		if t.Over {
			r.warn(KindCap, "template over cap, first %d KiB scanned: %s has %d bytes", MaxTemplateBytes>>10, where(t), t.Len)
		}
		if seen[t.Sum] {
			continue
		}
		seen[t.Sum] = true
		for _, h := range Heuristics(t.Text) {
			r.warn(h.Kind, "%s", h.Describe(where(t)))
		}
		ti := DetectTools(t.Text)
		tools.Refs = unionStrings(tools.Refs, ti.Refs)
		tools.Formats = unionStrings(tools.Formats, ti.Formats)
		reasoning = unionStrings(reasoning, DetectReasoning(t.Text))
		system = system || HandlesSystem(t.Text)
	}
	r.fact("chat template: %s", strings.Join(srcs, ", "))
	if tools.Yes() {
		r.Tools = "tool calling: yes (" + tools.Label() + ")"
	} else {
		r.Tools = "tool calling: no tool section in template"
	}
	if len(reasoning) > 0 {
		r.fact("reasoning: template mentions %s", strings.Join(reasoning, ", "))
	} else {
		r.fact("reasoning: no reasoning markers in template")
	}
	if system {
		r.fact("system role: template handles it")
	} else {
		r.fact("system role: template has no system branch")
	}
	r.disagreement(s)
}

func unionStrings(a, b []string) []string {
	for _, x := range b {
		found := false
		for _, y := range a {
			if x == y {
				found = true
				break
			}
		}
		if !found {
			a = append(a, x)
		}
	}
	return a
}

// disagreement compares the default template across files. The list form
// is compared by its "default" entry, or its only entry.
func (r *Result) disagreement(s *Surface) {
	type pick struct {
		t  Template
		ok bool
	}
	byFile := map[string]*pick{}
	var order []string
	for _, t := range s.Templates {
		p := byFile[t.File]
		if p == nil {
			p = &pick{}
			byFile[t.File] = p
			order = append(order, t.File)
		}
		if !p.ok || t.Name == "default" {
			p.t, p.ok = t, true
		}
	}
	for i := 0; i < len(order); i++ {
		for j := i + 1; j < len(order); j++ {
			a, b := byFile[order[i]].t, byFile[order[j]].t
			if a.Sum == b.Sum && (!a.Over || a.Len == b.Len) {
				continue
			}
			r.warn(KindDisagree, "templates disagree: %s (%d bytes) and %s (%d bytes) differ", where(a), a.Len, where(b), b.Len)
		}
	}
}

func (r *Result) fileFindings(s *Surface) {
	for _, f := range s.Files {
		switch f.State {
		case OverCap:
			if f.Err != "" {
				r.warn(KindNotRead, "%s not read: %s", f.Name, f.Err)
			} else {
				r.warn(KindNotRead, "%s not read: %d MiB over cap (%d MiB)", f.Name, (f.Size+1<<20-1)>>20, MaxJSONBytes>>20)
			}
		case Failed:
			r.warn(KindNotRead, "%s not read: %s", f.Name, f.Err)
		}
	}
	for _, p := range s.Problems {
		r.warn(KindNotRead, "%s", p)
	}
}

func contentCheck(r *Result, what string, texts []string) {
	n := 0
	for _, t := range texts {
		if HasInvisible(t) {
			n++
		}
	}
	if n > 0 {
		r.warn(KindTokenText, "%d %s contain invisible or control characters", n, what)
	}
}

func (r *Result) tokenFindings(s *Surface, cfg ConfigFacts) {
	merged := map[int64]AddedToken{}
	mismatch := 0
	var firstMismatch int64 = -1
	add := func(list []AddedToken) {
		for _, t := range list {
			if old, ok := merged[t.ID]; ok && old.Content != t.Content {
				if mismatch++; firstMismatch < 0 {
					firstMismatch = t.ID
				}
				continue
			}
			merged[t.ID] = t
		}
	}
	add(s.Tok.Added)
	if s.TokJSON != nil {
		add(s.TokJSON.Added)
	}
	if s.Tok.BadAddedKeys > 0 {
		r.warn(KindNotRead, "tokenizer_config.json: %d added_tokens_decoder keys are not token ids", s.Tok.BadAddedKeys)
	}
	if mismatch > 0 {
		r.warn(KindTokenMismatch, "%d added token ids have different content in tokenizer_config.json and tokenizer.json (first id %d)", mismatch, firstMismatch)
	}
	var texts []string
	for _, t := range merged {
		texts = append(texts, t.Content)
	}
	for _, role := range []string{"bos", "eos", "pad", "unk"} {
		if c, ok := s.Tok.Tokens[role]; ok {
			texts = append(texts, c)
		}
		if c, ok := s.Special.Tokens[role]; ok {
			texts = append(texts, c)
		}
	}
	texts = append(texts, s.Special.Additional...)
	contentCheck(r, "token strings", texts)

	if len(merged) > 0 {
		var hi int64 = -1
		special := 0
		for id, t := range merged {
			hi = max(hi, id)
			if t.Special {
				special++
			}
		}
		r.fact("added tokens: %d (%d special), highest id %d", len(merged), special, hi)
		var overRows, overVocab int
		var firstRows, firstVocab int64 = -1, -1
		for id := range merged {
			if cfg.EmbedRows > 0 && id >= cfg.EmbedRows {
				overRows++
				if firstRows < 0 || id < firstRows {
					firstRows = id
				}
			}
			if cfg.VocabSize > 0 && id >= cfg.VocabSize {
				overVocab++
				if firstVocab < 0 || id < firstVocab {
					firstVocab = id
				}
			}
		}
		if overRows > 0 {
			r.warn(KindOutOfRange, "%d added token ids are at or above the %d token embedding rows (lowest %d, highest %d)", overRows, cfg.EmbedRows, firstRows, hi)
		}
		if overVocab > 0 {
			r.warn(KindOutOfRange, "%d added token ids are at or above vocab_size %d (lowest %d, highest %d)", overVocab, cfg.VocabSize, firstVocab, hi)
		}
	}
	if s.TokJSON != nil && s.TokJSON.HaveVocab {
		r.fact("tokenizer.json: %d vocab entries, %d added tokens", s.TokJSON.VocabSize, s.TokJSON.AddedTotal)
	}
}

func (r *Result) contextFact(s *Surface, c ConfigFacts) {
	var parts []string
	effective := int64(0)
	if c.MaxPos > 0 {
		parts = append(parts, "max_position_embeddings "+strconv.FormatInt(c.MaxPos, 10))
		effective = c.MaxPos
	}
	if c.RopeFactor > 0 {
		p := fmt.Sprintf("rope_scaling %s x%s", orDash(cutUTF8(c.RopeType, 32)), strconv.FormatFloat(c.RopeFactor, 'g', 6, 64))
		if c.OrigMaxPos > 0 {
			if f := float64(c.OrigMaxPos) * c.RopeFactor; f > 0 && f < maxWhole {
				effective = int64(math.Round(f))
				p += fmt.Sprintf(" of original %d, effective %d", c.OrigMaxPos, effective)
			} else {
				p += fmt.Sprintf(" of original %d, effective context out of range", c.OrigMaxPos)
			}
		}
		parts = append(parts, p)
	}
	if m := s.Tok.ModelMaxLength; m.Set {
		switch {
		case !m.OK:
			parts = append(parts, "tokenizer model_max_length is not a number")
		case m.V >= 1e18:
		case effective == 0:
			parts = append(parts, "model_max_length "+strconv.FormatFloat(m.V, 'g', 12, 64))
		case m.V != float64(effective):
			parts = append(parts, fmt.Sprintf("model_max_length %s disagrees with %d", strconv.FormatFloat(m.V, 'g', 12, 64), effective))
		}
	}
	if len(parts) > 0 {
		r.fact("context: %s", strings.Join(parts, "; "))
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func (r *Result) generationFact(s *Surface) {
	g := s.Gen
	if !g.Have || len(g.Params) == 0 {
		return
	}
	parts := make([]string, len(g.Params))
	for i, p := range g.Params {
		parts[i] = p[0] + " " + p[1]
	}
	r.fact("generation defaults: %s", strings.Join(parts, ", "))
}

func (r *Result) specialFact(s *Surface) {
	var parts []string
	for _, role := range []string{"bos", "eos", "pad"} {
		c, ok := s.Tok.Tokens[role]
		if !ok {
			c, ok = s.Special.Tokens[role]
		}
		if ok {
			parts = append(parts, fmt.Sprintf("%s %q", role, cutRunes(c, 40)))
		}
	}
	if len(parts) > 0 {
		r.fact("special tokens: %s", strings.Join(parts, ", "))
	}
}

func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	rs := []rune(s)
	return string(rs[:n]) + "..."
}

type stopSource struct {
	file string
	ids  []int64
}

// stopTokens resolves end-of-sequence ids from config.json,
// generation_config.json and the tokenizer's eos_token, and compares them.
// A generation_config list that only adds ids to config.json's is routine
// and is a fact; sets that each hold an id the other lacks are a finding.
func (r *Result) stopTokens(s *Surface, cfg ConfigFacts) {
	var srcs []stopSource
	for _, e := range []struct {
		file string
		l    IDList
	}{{"config.json", cfg.EOS}, {FileGeneration, s.Gen.EOS}} {
		if e.l.Set && !e.l.Bad {
			srcs = append(srcs, stopSource{e.file, sortedIDs(e.l.IDs)})
		} else if e.l.Bad {
			r.warn(KindStop, "eos_token_id in %s is not a token id or a list of ids", e.file)
		}
	}
	byContent := map[string]int64{}
	dup := map[string]bool{}
	for _, list := range [][]AddedToken{s.Tok.Added, tokJSONAdded(s)} {
		for _, t := range list {
			if old, ok := byContent[t.Content]; ok && old != t.ID {
				dup[t.Content] = true
			}
			byContent[t.Content] = t.ID
		}
	}
	eos := map[string]string{}
	for _, src := range []struct {
		file string
		m    map[string]string
	}{{FileTokenizerConfig, s.Tok.Tokens}, {FileSpecialMap, s.Special.Tokens}} {
		if c, ok := src.m["eos"]; ok {
			eos[src.file] = c
			if id, ok := byContent[c]; ok && !dup[c] {
				srcs = append(srcs, stopSource{src.file + " eos_token", []int64{id}})
			}
		}
	}
	if a, b := eos[FileTokenizerConfig], eos[FileSpecialMap]; len(eos) == 2 && a != b {
		r.warn(KindStop, "eos_token differs: tokenizer_config.json has %q, special_tokens_map.json has %q", cutRunes(a, 40), cutRunes(b, 40))
	}
	for i := range srcs {
		for j := i + 1; j < len(srcs); j++ {
			a, b := srcs[i], srcs[j]
			// A list that only adds ids to another is routine: Hugging Face
			// writes the wider list to generation_config.json.
			if !subset(a.ids, b.ids) && !subset(b.ids, a.ids) {
				r.warn(KindStop, "stop tokens disagree: %s has %s, %s has %s", a.file, idList(a.ids), b.file, idList(b.ids))
			}
		}
	}
	if len(srcs) > 0 {
		parts := make([]string, len(srcs))
		for i, p := range srcs {
			parts[i] = p.file + " " + idList(p.ids)
		}
		r.fact("stop tokens: %s", strings.Join(parts, "; "))
	}
	if cfg.BOS.Set && s.Gen.BOS.Set && !cfg.BOS.Bad && !s.Gen.BOS.Bad && !equalIDs(cfg.BOS.IDs, s.Gen.BOS.IDs) {
		r.fact("bos_token_id differs: config.json %s, generation_config.json %s", idList(cfg.BOS.IDs), idList(s.Gen.BOS.IDs))
	}
}

func tokJSONAdded(s *Surface) []AddedToken {
	if s.TokJSON == nil {
		return nil
	}
	return s.TokJSON.Added
}

func sortedIDs(ids []int64) []int64 {
	out := append([]int64(nil), ids...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func subset(a, b []int64) bool {
	for _, x := range a {
		found := false
		for _, y := range b {
			if x == y {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func diff(a, b []int64) []int64 {
	var out []int64
	for _, x := range a {
		if !subset([]int64{x}, b) {
			out = append(out, x)
		}
	}
	return out
}

func equalIDs(a, b []int64) bool { return subset(a, b) && subset(b, a) }

func idList(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return "[" + strings.Join(parts, ",") + "]"
}
