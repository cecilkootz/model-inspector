package codescan

import (
	"bytes"
	"sort"
	"strings"
	"unicode/utf8"
)

// Python scan limits.
const (
	// MaxPyBytes is the most of one .py file that is read; the rest is not scanned.
	MaxPyBytes = 1 << 20
	// MaxPyLine is the longest line scanned. A longer line is one finding.
	MaxPyLine = 4096
	// MaxPyLines stops the scan.
	MaxPyLines = 10_000
	// MaxFindings caps distinct findings per file.
	MaxFindings = 256
	// maxName bounds any file-derived name kept in a finding.
	maxName = 128
)

// Class orders findings by weight.
type Class uint8

const (
	ClassInfo   Class = iota // recorded, does not change a verdict
	ClassRisk                // network, process, code loading, or an unusual pickle import
	ClassDanger              // a pickle imports something that runs commands or reaches the interpreter
)

// Finding categories.
const (
	CatImport   = "import"
	CatNetwork  = "network"
	CatProcess  = "process"
	CatLoading  = "code-loading"
	CatEnv      = "env/filesystem"
	CatEncoding = "encoding"
	CatURL      = "url"
	CatMeta     = "meta"
)

// Meta findings (long line, NUL bytes) mark text that was not scanned.

// Finding is one distinct (category, name) in a file. Line is the first
// occurrence; Count includes the rest. Name is file-derived and unsanitized:
// it is clipped to 128 bytes here and must pass through sanitize.From (or
// fileerr.Present) once, at the display boundary.
type Finding struct {
	Line   int
	Cat    string
	Name   string
	Class  Class
	Count  int
	Import bool
}

type pyScanner struct {
	findings []Finding
	index    map[string]int
	capped   bool
}

func (s *pyScanner) add(line int, cat, name string, class Class, imp bool) {
	name = clip(name)
	key := cat + "\x00" + name
	if imp {
		key = "i" + key
	}
	if i, ok := s.index[key]; ok {
		s.findings[i].Count++
		return
	}
	if len(s.findings) >= MaxFindings {
		s.capped = true
		return
	}
	s.index[key] = len(s.findings)
	s.findings = append(s.findings, Finding{Line: line, Cat: cat, Name: name, Class: class, Count: 1, Import: imp})
}

func clip(s string) string {
	if len(s) <= maxName {
		return s
	}
	s = s[:maxName]
	for !utf8.ValidString(s) && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return s
}

// PyResult is the outcome of scanning one Python source.
type PyResult struct {
	Findings []Finding
	// Incomplete lists reasons part of the file was not scanned.
	Incomplete []string
	Lines      int
}

// ScanPython scans src line by line. src is at most MaxPyBytes; cut says the
// file was longer. There is no parser, so it cannot see names built at
// runtime.
func ScanPython(src []byte, cut bool) PyResult {
	s := &pyScanner{index: map[string]int{}}
	var res PyResult
	if cut {
		res.Incomplete = append(res.Incomplete, "truncated at 1 MiB")
	}
	var st lineState
	nul := bytes.IndexByte(src, 0) >= 0
	if nul {
		s.add(0, CatMeta, "NUL bytes in source", ClassInfo, false)
	}
	pos, n := 0, 0
	for pos < len(src) {
		if n >= MaxPyLines {
			res.Incomplete = append(res.Incomplete, "stopped at 10000 lines")
			break
		}
		end := pos
		for end < len(src) && src[end] != '\n' && src[end] != '\r' {
			end++
		}
		line := src[pos:end]
		pos = end
		if pos < len(src) {
			if src[pos] == '\r' && pos+1 < len(src) && src[pos+1] == '\n' {
				pos++
			}
			pos++
		}
		n++
		if len(line) > MaxPyLine {
			s.add(n, CatMeta, "long line, not scanned", ClassInfo, false)
			st = lineState{}
			continue
		}
		s.scanLine(n, line, &st)
	}
	res.Lines = n
	if s.capped {
		res.Incomplete = append(res.Incomplete, "more than 256 findings; rest not listed")
	}
	res.Findings = s.findings
	sort.SliceStable(res.Findings, func(i, j int) bool { return res.Findings[i].Line < res.Findings[j].Line })
	return res
}

type lineState struct {
	triple byte // quote char of an open triple-quoted string, or 0
}

func isIdent(c byte) bool {
	return c == '_' || c >= 0x80 || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isIdentStart(c byte) bool { return isIdent(c) && !(c >= '0' && c <= '9') }

// scanLine looks for URLs and path literals anywhere on the line, and for
// import statements and risky name chains in code and single-line strings.
// Comments and triple-quoted text are skipped for names.
func (s *pyScanner) scanLine(n int, line []byte, st *lineState) {
	s.scanLiterals(n, line)
	code := line
	if st.triple == 0 {
		trimmed := bytes.TrimLeft(line, " \t\x0c")
		if rest, ok := importStatement(trimmed); ok {
			var tail []byte
			s.scanImport(n, rest.kind, rest.body, &tail)
			code = tail
		}
	}
	var inStr byte
	for i := 0; i < len(code); {
		c := code[i]
		if st.triple != 0 {
			if c == '\\' {
				i += 2
				continue
			}
			if c == st.triple && i+2 < len(code) && code[i+1] == c && code[i+2] == c {
				st.triple = 0
				i += 3
				continue
			}
			i++
			continue
		}
		if inStr != 0 {
			if c == '\\' {
				i += 2
				continue
			}
			if c == inStr {
				inStr = 0
				i++
				continue
			}
		} else {
			if c == '#' {
				return
			}
			if c == '\'' || c == '"' {
				if i+2 < len(code) && code[i+1] == c && code[i+2] == c {
					st.triple = c
					i += 3
					continue
				}
				inStr = c
				i++
				continue
			}
		}
		if isIdentStart(c) {
			j := i
			for j < len(code) && (isIdent(code[j]) || code[j] == '.' && j+1 < len(code) && isIdentStart(code[j+1])) {
				j++
			}
			k := j
			for k < len(code) && (code[k] == ' ' || code[k] == '\t') {
				k++
			}
			s.checkChain(n, string(code[i:j]), k < len(code) && code[k] == '(')
			i = j
			continue
		}
		i++
	}
}

type importStmt struct {
	kind string // "import" or "from"
	body []byte
}

func importStatement(t []byte) (importStmt, bool) {
	for _, kw := range []string{"import", "from"} {
		if len(t) > len(kw) && string(t[:len(kw)]) == kw && (t[len(kw)] == ' ' || t[len(kw)] == '\t') {
			return importStmt{kind: kw, body: t[len(kw)+1:]}, true
		}
	}
	return importStmt{}, false
}

// scanImport records the modules of an import statement. Anything after a
// ';' is returned in tail for normal scanning.
func (s *pyScanner) scanImport(n int, kind string, body []byte, tail *[]byte) {
	if i := bytes.IndexByte(body, ';'); i >= 0 {
		*tail = body[i+1:]
		body = body[:i]
	}
	if i := bytes.IndexByte(body, '#'); i >= 0 {
		body = body[:i]
	}
	text := strings.NewReplacer("(", " ", ")", " ", "\\", " ").Replace(string(body))
	if kind == "import" {
		for _, part := range strings.Split(text, ",") {
			f := strings.Fields(part)
			if len(f) == 0 {
				continue
			}
			s.recordImport(n, importName(f[0]))
		}
		return
	}
	mod, names, ok := strings.Cut(text, " import ")
	if !ok {
		f := strings.Fields(text)
		if len(f) > 0 {
			s.recordImport(n, importName(f[0]))
		}
		return
	}
	mod = importName(strings.TrimSpace(mod))
	var members []string
	for _, part := range strings.Split(names, ",") {
		if f := strings.Fields(part); len(f) > 0 {
			members = append(members, importName(f[0]))
		}
	}
	if len(members) == 0 {
		s.recordImport(n, mod)
		return
	}
	s.recordImport(n, mod)
	if cat, _ := riskOf(mod, false); cat != "" || strings.Trim(mod, ".") == "" {
		return
	}
	for _, m := range members {
		if cat, class := riskOf(mod+"."+m, false); cat != "" {
			s.add(n, cat, mod+"."+m, class, true)
		}
	}
}

// importName keeps the identifier and dot characters; anything else ends it.
func importName(s string) string {
	end := 0
	for end < len(s) && (isIdent(s[end]) || s[end] == '.') {
		end++
	}
	return s[:end]
}

func (s *pyScanner) recordImport(n int, mod string) {
	if mod == "" {
		return
	}
	if cat, class := riskOf(mod, false); cat != "" {
		s.add(n, cat, mod, class, true)
		return
	}
	s.add(n, CatImport, mod, ClassInfo, true)
}

func (s *pyScanner) checkChain(n int, chain string, call bool) {
	if cat, class := riskOf(chain, call); cat != "" {
		s.add(n, cat, chain, class, false)
	}
}

var (
	networkRoots  = set("requests", "urllib", "urllib3", "socket", "aiohttp", "httpx", "ftplib", "smtplib")
	processRoots  = set("subprocess", "pty")
	loadingRoots  = set("importlib", "ctypes", "cffi", "pickle", "marshal", "shelve")
	encodingRoots = set("base64", "codecs", "zlib", "bz2", "lzma")
	envRoots      = set("pathlib", "shutil", "glob")
	secretWords   = set("token", "api_key", "secret")
)

// riskOf classifies a dotted name. call says it is followed by '(' (only
// bare exec/eval/compile/open are matched that way, so model.eval() and
// re.compile() are not).
func riskOf(chain string, call bool) (cat string, class Class) {
	segs := strings.Split(chain, ".")
	root, last := segs[0], segs[len(segs)-1]
	_, isNet := networkRoots[root]
	switch {
	case isNet || strings.HasPrefix(chain, "http.client"):
		return CatNetwork, ClassRisk
	}
	if _, ok := processRoots[root]; ok {
		return CatProcess, ClassRisk
	}
	if root == "os" && len(segs) > 1 {
		f := segs[1]
		if f == "system" || f == "popen" || strings.HasPrefix(f, "exec") || strings.HasPrefix(f, "spawn") {
			return CatProcess, ClassRisk
		}
		if f == "environ" || f == "getenv" {
			return CatEnv, ClassInfo
		}
	}
	if _, ok := loadingRoots[root]; ok || last == "__import__" {
		return CatLoading, ClassRisk
	}
	if len(segs) == 1 && call && (root == "exec" || root == "eval" || root == "compile") {
		return CatLoading, ClassRisk
	}
	if _, ok := encodingRoots[root]; ok {
		return CatEncoding, ClassInfo
	}
	if _, ok := envRoots[root]; ok {
		return CatEnv, ClassInfo
	}
	if last == "getenv" || (len(segs) == 1 && call && root == "open") {
		return CatEnv, ClassInfo
	}
	for _, seg := range segs {
		if _, ok := secretWords[strings.ToLower(seg)]; ok {
			return CatEnv, ClassInfo
		}
	}
	return "", ClassInfo
}

var pathLiterals = []string{"~/.ssh", ".aws", ".netrc"}

// scanLiterals finds URLs and credential paths anywhere on the line,
// including comments and docstrings, because a payload may sit in a string.
func (s *pyScanner) scanLiterals(n int, line []byte) {
	text := string(line)
	for _, p := range pathLiterals {
		if strings.Contains(text, p) {
			s.add(n, CatEnv, p, ClassInfo, false)
		}
	}
	rest := text
	for {
		i := strings.Index(rest, "://")
		if i < 0 {
			return
		}
		start := i
		for start > 0 && (rest[start-1] >= 'a' && rest[start-1] <= 'z' || rest[start-1] >= 'A' && rest[start-1] <= 'Z' || rest[start-1] >= '0' && rest[start-1] <= '9' || rest[start-1] == '+' || rest[start-1] == '.' || rest[start-1] == '-') {
			start--
		}
		end := i + 3
		for end < len(rest) && !strings.ContainsRune(" \t\"'<>()[]{}\\,;", rune(rest[end])) {
			end++
		}
		s.add(n, CatURL, rest[start:end], ClassInfo, false)
		rest = rest[end:]
	}
}
