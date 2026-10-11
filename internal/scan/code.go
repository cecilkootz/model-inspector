package scan

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cecilkootz/model-inspector/internal/codescan"
	"github.com/cecilkootz/model-inspector/internal/model"
)

const (
	// maxCodeFiles bounds how many files the code-content check opens.
	maxCodeFiles = 512
	// maxCodeBytes bounds the total read across files; one file may overshoot
	// by its own cap.
	maxCodeBytes = 64 << 20
	// maxSummaryNames is how many names appear in a summary before "…".
	maxSummaryNames = 3
)

type codeFile struct {
	name string
	auto bool // a loader would import this .py through auto_map
	rep  codescan.Report
}

type codeLine struct {
	prio int
	text string
}

// autoMapFile maps "modeling_x.Class" to "modeling_x.py". Targets that name
// another repo ("org/repo--mod.Class") or leave the snapshot do not resolve.
func autoMapFile(target string) (string, bool) {
	i := strings.LastIndex(target, ".")
	if i <= 0 || strings.Contains(target, "--") {
		return "", false
	}
	file := strings.ReplaceAll(target[:i], ".", "/") + ".py"
	if !filepath.IsLocal(file) {
		return "", false
	}
	return file, true
}

func codeContent(m *model.Model) Check {
	c := Check{ID: CheckCode, Name: "Code & pickle contents", Status: StatusSkip}
	if m == nil || (len(m.CodeExecFiles()) == 0 && len(m.AutoMapEntries()) == 0) {
		c.Summary = "nothing to read (no flagged files)"
		return c
	}
	entries := m.AutoMapEntries()
	have := filesByName(m)
	auto := map[string]bool{}
	var autoNames []string
	var unresolved []codeLine
	for _, e := range entries {
		file, ok := autoMapFile(e.Target)
		if _, present := have[file]; !ok || !present {
			unresolved = append(unresolved, codeLine{3, fmt.Sprintf("%s auto_map %s → %s → not found in this snapshot; a loader would fetch it elsewhere", e.File, e.Key, e.Target)})
			autoNames = append(autoNames, e.Target+" (not in snapshot)")
			continue
		}
		if !auto[file] {
			auto[file] = true
			autoNames = append(autoNames, file)
		}
	}

	var names []string
	for _, f := range m.CodeExecFiles() {
		names = append(names, f.Name)
	}
	sort.SliceStable(names, func(i, j int) bool {
		pi, pj := codescan.Classify(names[i]) == codescan.KindPython, codescan.Classify(names[j]) == codescan.KindPython
		if pi != pj {
			return pi
		}
		if auto[names[i]] != auto[names[j]] {
			return auto[names[i]]
		}
		return names[i] < names[j]
	})

	var files []codeFile
	var read int64
	var skipped int
	roots := m.AllowedRoots()
	for _, name := range names {
		if len(files) >= maxCodeFiles || read >= maxCodeBytes {
			skipped++
			continue
		}
		var rep codescan.Report
		if f := have[name]; f.Broken {
			rep = codescan.Report{Kind: codescan.Classify(name), Incomplete: []string{"could not be read"}}
		} else {
			rep = codescan.ScanFile(filepath.Join(m.Dir, filepath.FromSlash(name)), name, roots...)
		}
		read += rep.Bytes
		files = append(files, codeFile{name: name, auto: auto[name], rep: rep})
	}

	lines := append([]codeLine(nil), unresolved...)
	var dangerFiles, outsideFiles, autoRisk, otherRisk, incomplete []string
	var dangerRefs, outsideRefs []string
	cleanPickles, cleanPy := 0, 0
	for _, f := range files {
		d := describeFile(f)
		lines = append(lines, d.lines...)
		if d.danger {
			dangerFiles = append(dangerFiles, f.name)
			dangerRefs = append(dangerRefs, d.dangerRefs...)
		}
		if d.outside {
			outsideFiles = append(outsideFiles, f.name)
			outsideRefs = append(outsideRefs, d.outsideRefs...)
		}
		if d.pyRisk {
			if f.auto {
				autoRisk = append(autoRisk, f.name+" "+d.pySummary)
			} else {
				otherRisk = append(otherRisk, f.name+" "+d.pySummary)
			}
		}
		complete := len(f.rep.Incomplete) == 0 && !hasMeta(f.rep)
		if !complete {
			incomplete = append(incomplete, f.name)
		}
		if complete && !d.danger && !d.outside && !d.pyRisk {
			switch f.rep.Kind {
			case codescan.KindPickle:
				cleanPickles++
			case codescan.KindPython:
				cleanPy++
			}
		}
	}
	if skipped > 0 {
		lines = append(lines, codeLine{3, fmt.Sprintf("%d more files not scanned (limit %d files or %d MiB read)", skipped, maxCodeFiles, maxCodeBytes>>20)})
		incomplete = append(incomplete, fmt.Sprintf("%d more files", skipped))
	}
	if len(unresolved) > 0 {
		incomplete = append(incomplete, "auto_map targets")
	}

	var parts []string
	if len(dangerFiles) > 0 {
		parts = append(parts, fmt.Sprintf("%s references %s", firstMore(dangerFiles), joinNames(dangerRefs)))
	}
	if len(autoRisk) > 0 {
		parts = append(parts, firstMore(autoRisk))
	}
	if len(otherRisk) > 0 {
		parts = append(parts, firstMore(otherRisk))
	}
	if len(outsideFiles) > 0 {
		parts = append(parts, fmt.Sprintf("%s imports outside the usual torch/numpy set: %s", firstMore(outsideFiles), joinNames(outsideRefs)))
	}
	if len(incomplete) > 0 {
		parts = append(parts, fmt.Sprintf("%d not fully scanned: %s", len(incomplete), firstMore(incomplete)))
	}
	switch {
	case len(dangerFiles) > 0 || len(autoRisk) > 0:
		c.Status = StatusFail
	case len(parts) > 0:
		c.Status = StatusWarn
	default:
		c.Status = StatusPass
		if cleanPickles > 0 {
			parts = append(parts, fmt.Sprintf("%s reference only torch/numpy", plural(cleanPickles, "pickle")))
		}
		if cleanPy > 0 {
			parts = append(parts, fmt.Sprintf("%s without network, process or code-loading names", plural(cleanPy, ".py file")))
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "nothing flagged in the files read")
	}
	if len(parts) > 2 {
		parts = append(parts[:2], fmt.Sprintf("+%d more in details", len(parts)-2))
	}
	c.Summary = strings.Join(parts, "; ")

	sort.SliceStable(lines, func(i, j int) bool {
		if lines[i].prio != lines[j].prio {
			return lines[i].prio < lines[j].prio
		}
		return lines[i].text < lines[j].text
	})
	details := []string{trustVerdict(m, autoNames)}
	for _, l := range lines {
		details = append(details, l.text)
	}
	if len(details) > maxDetails {
		more := len(details) - (maxDetails - 1)
		details = append(details[:maxDetails-1], fmt.Sprintf("... and %d more", more))
	}
	c.Details = details
	return c
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func firstMore(items []string) string {
	if len(items) == 1 {
		return items[0]
	}
	return fmt.Sprintf("%s (+%d more)", items[0], len(items)-1)
}

func joinNames(items []string) string {
	seen := map[string]bool{}
	var out []string
	for _, s := range items {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	if len(out) > maxSummaryNames {
		return strings.Join(out[:maxSummaryNames], ", ") + ", …"
	}
	return strings.Join(out, ", ")
}

func trustVerdict(m *model.Model, autoNames []string) string {
	switch {
	case len(autoNames) > 0:
		return "needs trust_remote_code: yes (auto_map → " + joinNames(autoNames) + ")"
	case len(m.UncheckedAutoMap) > 0:
		return "needs trust_remote_code: unknown (could not check auto_map in " + joinNames(m.UncheckedAutoMap) + ")"
	default:
		return "needs trust_remote_code: no (no auto_map in config files)"
	}
}

func hasMeta(r codescan.Report) bool {
	for _, f := range r.Findings {
		if f.Cat == codescan.CatMeta {
			return true
		}
	}
	return false
}

type fileDesc struct {
	lines           []codeLine
	danger, outside bool
	pyRisk          bool
	dangerRefs      []string
	outsideRefs     []string
	pySummary       string
}

// describeFile turns one report into detail lines and roll-up flags.
// Names stay raw here; the display boundary sanitizes them once.
func describeFile(f codeFile) fileDesc {
	var d fileDesc
	for _, why := range f.rep.Incomplete {
		d.lines = append(d.lines, codeLine{3, f.name + " → " + why})
	}
	var imports, reads, uses []string
	for _, x := range f.rep.Findings {
		if x.Cat == codescan.CatImport {
			continue
		}
		prio := 4
		if x.Class >= codescan.ClassRisk {
			d.pyRisk = true
			prio = 1
			if f.auto {
				prio = 0
			}
			switch {
			case x.Import:
				imports = append(imports, x.Name)
			default:
				uses = append(uses, x.Name)
			}
		} else if x.Cat == codescan.CatEnv && strings.HasPrefix(x.Name, "os.environ") {
			reads = append(reads, x.Name)
		} else if x.Cat == codescan.CatMeta {
			prio = 3
		}
		what := x.Name
		if x.Import {
			what = "imports " + x.Name
		}
		if x.Count > 1 {
			what += fmt.Sprintf(" (×%d)", x.Count)
		}
		at := f.name
		if x.Line > 0 {
			at = fmt.Sprintf("%s:%d", f.name, x.Line)
		}
		d.lines = append(d.lines, codeLine{prio, fmt.Sprintf("%s → %s: %s", at, x.Cat, what)})
	}
	if d.pyRisk {
		var phrase []string
		if len(imports) > 0 {
			phrase = append(phrase, "imports "+joinNames(imports))
		}
		if len(reads) > 0 {
			phrase = append(phrase, "reads "+joinNames(reads))
		}
		if rest := notImported(uses, imports); len(rest) > 0 {
			phrase = append(phrase, "uses "+joinNames(rest))
		}
		d.pySummary = joinAnd(phrase)
	}
	if p := f.rep.Pickle; p != nil {
		for _, g := range p.Globals {
			switch g.Class {
			case codescan.ClassDanger:
				d.danger = true
				d.dangerRefs = append(d.dangerRefs, g.Ref())
				d.lines = append(d.lines, codeLine{0, f.name + " → references " + g.Ref() + " (dangerous import)"})
			case codescan.ClassRisk:
				d.outside = true
				d.outsideRefs = append(d.outsideRefs, g.Ref())
				d.lines = append(d.lines, codeLine{2, f.name + " → imports outside the usual torch/numpy set: " + g.Ref()})
			}
		}
		if ops := opsLine(p.Ops); ops != "" {
			d.lines = append(d.lines, codeLine{5, f.name + " → opcodes: " + ops})
		}
	}
	return d
}

func opsLine(o codescan.Ops) string {
	var parts []string
	for _, c := range []struct {
		n int
		s string
	}{{o.Reduce, "REDUCE"}, {o.Build, "BUILD"}, {o.Inst, "INST"}, {o.Obj, "OBJ"}, {o.NewObj, "NEWOBJ"}, {o.NewObjEx, "NEWOBJ_EX"}} {
		if c.n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", c.s, c.n))
		}
	}
	return strings.Join(parts, ", ")
}

// notImported drops names already covered by an import in the same file.
func notImported(uses, imports []string) []string {
	var out []string
	for _, u := range uses {
		covered := false
		for _, i := range imports {
			if u == i || strings.HasPrefix(u, i+".") {
				covered = true
			}
		}
		if !covered {
			out = append(out, u)
		}
	}
	return out
}

func joinAnd(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}
