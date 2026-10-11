// Package agentcfg reads the files that decide how a checkpoint behaves as
// an agent: chat template, tokenizer settings and generation defaults. None
// of them is weights, and all of them are untrusted. A chat template is
// code; this package never renders or evaluates it, it only looks at its
// text.
//
// Strings kept on the types here are raw file text. They are sanitized
// where scan results are displayed, the same as every other check detail.
package agentcfg

import (
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/cecilkootz/model-inspector/internal/jsonx"
	"github.com/cecilkootz/model-inspector/internal/safeopen"
)

const (
	// MaxJSONBytes caps every JSON file read here, tokenizer.json included.
	// It equals the loader's cap for config.json so one rule covers all
	// config-like files; larger files are reported as not read.
	MaxJSONBytes = 64 << 20
	// MaxTemplateBytes caps the template text that is scanned.
	MaxTemplateBytes = 256 << 10

	maxTemplates     = 16
	maxAdded         = 1 << 17
	maxAdditional    = 256
	maxIDs           = 64
	maxTokenText     = 512
	maxTokenizerNode = 8_000_000
)

// Files this package looks at, in the order they are reported.
const (
	FileTokenizerConfig = "tokenizer_config.json"
	FileTemplateJinja   = "chat_template.jinja"
	FileTemplateJSON    = "chat_template.json"
	FileGeneration      = "generation_config.json"
	FileSpecialMap      = "special_tokens_map.json"
	FileTokenizerJSON   = "tokenizer.json"
)

// State is what happened when a file was looked for.
type State int

const (
	Missing State = iota
	Loaded
	OverCap
	Failed
)

// FileInfo is one file's read outcome. Err is a fileerr-clipped message.
type FileInfo struct {
	Name  string
	State State
	Size  int64
	Err   string
}

// Num is an optional JSON number. Set is "the key was present"; OK is "and
// it is a finite number".
type Num struct {
	V       float64
	Set, OK bool
}

// IDList is a token id or list of ids. Bad marks a value that is not a
// whole non-negative number or list of them.
type IDList struct {
	IDs []int64
	Set bool
	Bad bool
}

// AddedToken is one entry from added_tokens_decoder or added_tokens.
type AddedToken struct {
	ID      int64
	Content string
	Special bool
}

// Template is one chat template and where it came from. Text is cut at
// MaxTemplateBytes; Len is the full byte length.
type Template struct {
	File string
	Name string
	Text string
	Len  int
	Sum  [32]byte
	Over bool
}

// TokenizerConfig holds the fields read from tokenizer_config.json.
type TokenizerConfig struct {
	Have           bool
	Tokens         map[string]string
	Added          []AddedToken
	AddedTotal     int
	BadAddedKeys   int
	ModelMaxLength Num
}

// SpecialTokens holds special_tokens_map.json.
type SpecialTokens struct {
	Have            bool
	Tokens          map[string]string
	Additional      []string
	AdditionalTotal int
}

// GenerationConfig holds generation_config.json. Params are pre-formatted
// "key value" pairs of numbers and booleans only.
type GenerationConfig struct {
	Have   bool
	EOS    IDList
	BOS    IDList
	PAD    IDList
	Params [][2]string
}

// TokenizerJSON is what was extracted from tokenizer.json.
type TokenizerJSON struct {
	Added      []AddedToken
	AddedTotal int
	VocabSize  int64
	HaveVocab  bool
}

// Surface is everything read from one model directory.
type Surface struct {
	Files     []FileInfo
	Templates []Template
	Tok       TokenizerConfig
	Special   SpecialTokens
	Gen       GenerationConfig
	TokJSON   *TokenizerJSON
	// Problems are shape errors inside files that did parse.
	Problems []string
}

// Info returns the read outcome for name.
func (s *Surface) Info(name string) FileInfo {
	for _, f := range s.Files {
		if f.Name == name {
			return f
		}
	}
	return FileInfo{Name: name}
}

// Any reports whether any of the files exists.
func (s *Surface) Any() bool {
	if s == nil {
		return false
	}
	for _, f := range s.Files {
		if f.State != Missing {
			return true
		}
	}
	return false
}

// Read looks for the agent-surface files in dir. Every read goes through
// safeopen under roots. A missing file is not an error.
func Read(dir string, roots []string) *Surface {
	s := &Surface{}

	obj, fi := readObject(dir, FileTokenizerConfig, roots)
	if obj != nil {
		s.parseTokenizerConfig(obj)
	}
	s.Files = append(s.Files, fi)

	s.Files = append(s.Files, s.readJinja(dir, roots))

	obj, fi = readObject(dir, FileTemplateJSON, roots)
	if obj != nil {
		s.addTemplates(FileTemplateJSON, obj["chat_template"])
	}
	s.Files = append(s.Files, fi)

	obj, fi = readObject(dir, FileGeneration, roots)
	if obj != nil {
		s.parseGeneration(obj)
	}
	s.Files = append(s.Files, fi)

	obj, fi = readObject(dir, FileSpecialMap, roots)
	if obj != nil {
		s.parseSpecialMap(obj)
	}
	s.Files = append(s.Files, fi)

	s.Files = append(s.Files, s.readTokenizerJSON(dir, roots))
	return s
}

// open opens name for reading. ok is false for a missing or unusable file,
// with fi describing why.
func open(dir, name string, max int64, roots []string) (f *safeopen.File, fi FileInfo, ok bool) {
	fi = FileInfo{Name: name}
	p := filepath.Join(dir, name)
	f, err := safeopen.OpenUnder(p, max, roots...)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			if _, lerr := os.Lstat(p); lerr != nil {
				return nil, fi, false
			}
			fi.State, fi.Err = Failed, "broken symlink"
			return nil, fi, false
		}
		fi.State, fi.Err = Failed, err.Error()
		return nil, fi, false
	}
	fi.State, fi.Size = Loaded, f.Size()
	return f, fi, true
}

// readObject reads one JSON object. obj is nil when the file is missing,
// over the cap, or not a single object.
func readObject(dir, name string, roots []string) (obj map[string]any, fi FileInfo) {
	f, fi, ok := open(dir, name, MaxJSONBytes, roots)
	if !ok {
		return nil, fi
	}
	defer f.Close()
	if f.Size() > MaxJSONBytes {
		fi.State = OverCap
		return nil, fi
	}
	b, err := io.ReadAll(f)
	if err != nil {
		fi.State, fi.Err = Failed, err.Error()
		return nil, fi
	}
	if len(b) > MaxJSONBytes {
		fi.State = OverCap
		return nil, fi
	}
	v, err := jsonx.Unmarshal(b)
	if err != nil {
		fi.State, fi.Err = Failed, err.Error()
		return nil, fi
	}
	obj, _ = v.(map[string]any)
	if obj == nil {
		fi.State, fi.Err = Failed, "expected a JSON object"
	}
	return obj, fi
}

func (s *Surface) readJinja(dir string, roots []string) FileInfo {
	f, fi, ok := open(dir, FileTemplateJinja, MaxTemplateBytes, roots)
	if !ok {
		return fi
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		fi.State, fi.Err = Failed, err.Error()
		return fi
	}
	t := newTemplate(FileTemplateJinja, "", string(b))
	if fi.Size > MaxTemplateBytes {
		t.Over, t.Len = true, int(min(fi.Size, math.MaxInt32))
		t.Text = cutUTF8(t.Text, MaxTemplateBytes)
		t.Sum = sha256.Sum256([]byte(strings.TrimSpace(t.Text)))
	}
	s.addTemplate(t)
	return fi
}

func newTemplate(file, name, text string) Template {
	t := Template{File: file, Name: cutUTF8(name, 64), Len: len(text)}
	if len(text) > MaxTemplateBytes {
		t.Over = true
		text = cutUTF8(text, MaxTemplateBytes)
	}
	t.Text = text
	t.Sum = sha256.Sum256([]byte(strings.TrimSpace(text)))
	return t
}

// cutUTF8 returns at most n bytes of s, ending on a rune boundary.
func cutUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// addTemplates reads a chat_template value: a string, or a list of
// {name, template} objects.
func (s *Surface) addTemplates(file string, v any) {
	switch t := v.(type) {
	case nil:
	case string:
		s.addTemplate(newTemplate(file, "", t))
	case []any:
		for i, e := range t {
			if i >= maxTemplates {
				s.Problems = append(s.Problems, file+": chat_template list has more than 16 entries; the rest were not read")
				break
			}
			m, _ := e.(map[string]any)
			text, ok := m["template"].(string)
			if !ok {
				s.Problems = append(s.Problems, file+": a chat_template list entry has no template string")
				continue
			}
			name, _ := m["name"].(string)
			s.addTemplate(newTemplate(file, name, text))
		}
	default:
		s.Problems = append(s.Problems, file+": chat_template is neither a string nor a list")
	}
}

func (s *Surface) addTemplate(t Template) {
	if len(s.Templates) < maxTemplates*3 {
		s.Templates = append(s.Templates, t)
	}
}

func (s *Surface) parseTokenizerConfig(obj map[string]any) {
	tc := &s.Tok
	tc.Have = true
	tc.Tokens = map[string]string{}
	s.addTemplates(FileTokenizerConfig, obj["chat_template"])
	for _, role := range []string{"bos", "eos", "pad", "unk"} {
		if c, ok := tokenContent(obj[role+"_token"]); ok {
			tc.Tokens[role] = c
		}
	}
	tc.ModelMaxLength = numOf(obj, "model_max_length")
	if dec, ok := obj["added_tokens_decoder"].(map[string]any); ok {
		keys := make([]string, 0, len(dec))
		for k := range dec {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			tc.AddedTotal++
			id, ok := parseID(k)
			if !ok {
				tc.BadAddedKeys++
				continue
			}
			if len(tc.Added) >= maxAdded {
				continue
			}
			e, _ := dec[k].(map[string]any)
			c, _ := e["content"].(string)
			sp, _ := e["special"].(bool)
			tc.Added = append(tc.Added, AddedToken{ID: id, Content: cutUTF8(c, maxTokenText), Special: sp})
		}
	}
}

func (s *Surface) parseSpecialMap(obj map[string]any) {
	sp := &s.Special
	sp.Have = true
	sp.Tokens = map[string]string{}
	for _, role := range []string{"bos", "eos", "pad", "unk"} {
		if c, ok := tokenContent(obj[role+"_token"]); ok {
			sp.Tokens[role] = c
		}
	}
	if list, ok := obj["additional_special_tokens"].([]any); ok {
		for _, e := range list {
			sp.AdditionalTotal++
			if c, ok := tokenContent(e); ok && len(sp.Additional) < maxAdditional {
				sp.Additional = append(sp.Additional, c)
			}
		}
	}
}

func (s *Surface) parseGeneration(obj map[string]any) {
	g := &s.Gen
	g.Have = true
	g.EOS = ParseIDs(obj["eos_token_id"])
	g.BOS = ParseIDs(obj["bos_token_id"])
	g.PAD = ParseIDs(obj["pad_token_id"])
	for _, k := range []string{"temperature", "top_p", "top_k", "max_length", "max_new_tokens", "do_sample"} {
		v, ok := obj[k]
		if !ok || v == nil {
			continue
		}
		g.Params = append(g.Params, [2]string{k, formatParam(v)})
	}
}

// formatParam renders a number or boolean. Anything else is reported as
// "(not a number)", so file text never lands in the result.
func formatParam(v any) string {
	if b, ok := v.(bool); ok {
		return strconv.FormatBool(b)
	}
	if f, ok := jsonx.FitFloat(v); ok {
		return strconv.FormatFloat(f, 'g', 6, 64)
	}
	return "(not a number)"
}

func tokenContent(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return cutUTF8(t, maxTokenText), true
	case map[string]any:
		c, ok := t["content"].(string)
		return cutUTF8(c, maxTokenText), ok
	}
	return "", false
}

func numOf(obj map[string]any, key string) Num {
	v, ok := obj[key]
	if !ok || v == nil {
		return Num{}
	}
	f, fit := jsonx.FitFloat(v)
	return Num{V: f, Set: true, OK: fit && !math.IsNaN(f) && !math.IsInf(f, 0)}
}

const maxWhole = 1 << 53

func parseID(s string) (int64, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil && n >= 0
}

func wholeID(v any) (int64, bool) {
	f, ok := jsonx.FitFloat(v)
	if !ok || f < 0 || f != math.Trunc(f) || f >= maxWhole {
		return 0, false
	}
	return int64(f), true
}

// ParseIDs reads a token id or a list of ids, as config.json and
// generation_config.json write eos_token_id.
func ParseIDs(v any) IDList {
	var out IDList
	switch t := v.(type) {
	case nil:
		return out
	case []any:
		out.Set = true
		for i, e := range t {
			if i >= maxIDs {
				out.Bad = true
				break
			}
			id, ok := wholeID(e)
			if !ok {
				out.Bad = true
				continue
			}
			out.IDs = append(out.IDs, id)
		}
	default:
		out.Set = true
		if id, ok := wholeID(v); ok {
			out.IDs = []int64{id}
		} else {
			out.Bad = true
		}
	}
	return out
}
