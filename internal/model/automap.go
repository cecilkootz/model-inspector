package model

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"origin.cursor.com/cecilkootz/model-inspector/internal/safeopen"
)

// AutoMapEntry is one auto_map value. The target is recorded; nothing is imported.
type AutoMapEntry struct {
	File   string
	Key    string
	Target string
}

// codeExts are code-executing formats matched on the file name.
// The check also reads top-level auto_map from config files; it does not
// import or execute anything.
var codeExts = map[string]struct{}{
	".bin": {}, ".pt": {}, ".pth": {}, ".ckpt": {},
	".pkl": {}, ".pickle": {}, ".joblib": {},
	".npy": {}, ".pyc": {}, ".py": {},
	".h5": {}, ".keras": {}, ".nemo": {}, ".dill": {}, ".mar": {},
	".tar": {}, ".tgz": {},
}

// codeCompounds are suffixes filepath.Ext does not see as one extension,
// such as model.bin.gz. .tar is also listed on its own; .pth.tar is here
// so a tar of a pickle path is matched even if the single-suffix list changes.
var codeCompounds = []string{
	".bin.gz", ".pt.gz", ".pkl.gz", ".tar.gz",
	".pt.zip", ".ckpt.zip", ".pth.zip", ".bin.zip", ".pth.tar",
}

// auxConfigNames are the small JSON files, besides config.json, whose
// top-level auto_map is part of the code-exec check.
var auxConfigNames = []string{
	"tokenizer_config.json",
	"processor_config.json",
	"preprocessor_config.json",
}

// CodeExecFiles returns files whose name is a code-executing format.
// The match is on the base name, including compound suffixes such as .bin.gz.
func (m *Model) CodeExecFiles() []File {
	if m == nil {
		return nil
	}
	var out []File
	for _, f := range m.Files {
		if codeExecName(f.Name) {
			out = append(out, f)
		}
	}
	return out
}

func codeExecName(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	for _, s := range codeCompounds {
		if strings.HasSuffix(base, s) {
			return true
		}
	}
	_, ok := codeExts[filepath.Ext(base)]
	return ok
}

// noteUnchecked records that name exists but auto_map could not be read.
// The inspection is partial, and the name is listed once.
func (m *Model) noteUnchecked(name string) {
	m.Partial = true
	for _, have := range m.UncheckedAutoMap {
		if have == name {
			return
		}
	}
	m.UncheckedAutoMap = append(m.UncheckedAutoMap, name)
}

// loadAuxConfigs parses the three aux config files. config.json is handled
// by loadConfig. A missing file is skipped. A file that is present but
// cannot be read or parsed is a load warning and an unchecked auto_map.
func (m *Model) loadAuxConfigs() {
	if m.auxParsed == nil {
		m.auxParsed = map[string]map[string]any{}
	}
	byName := make(map[string]File, len(m.Files))
	for _, f := range m.Files {
		byName[f.Name] = f
	}
	for _, name := range auxConfigNames {
		f, ok := byName[name]
		p := filepath.Join(m.Dir, name)
		if !ok {
			if _, err := os.Lstat(p); err != nil {
				continue
			}
			m.warnf("%s: present but not a readable file", name)
			m.noteUnchecked(name)
			continue
		}
		if f.Broken {
			m.warnf("%s: could not be read", name)
			m.noteUnchecked(name)
			continue
		}
		raw, err := safeopen.ReadAll(p, maxSmallJSON, m.allowedRoots()...)
		if err != nil {
			m.warnf("%s: %v", name, err)
			m.noteUnchecked(name)
			continue
		}
		obj, err := parseConfigObject(name, raw)
		if err != nil {
			m.warnf("%s", err.Error())
			m.noteUnchecked(name)
			continue
		}
		m.auxParsed[name] = obj
	}
}

// AutoMapEntries reads top-level auto_map objects from config files that
// parsed. Unreadable files are listed on UncheckedAutoMap instead.
func (m *Model) AutoMapEntries() []AutoMapEntry {
	if m == nil {
		return nil
	}
	var out []AutoMapEntry
	if m.Config != nil {
		out = append(out, autoMapFrom("config.json", m.Config)...)
	}
	for _, name := range auxConfigNames {
		out = append(out, autoMapFrom(name, m.auxParsed[name])...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		if out[i].Key != out[j].Key {
			return out[i].Key < out[j].Key
		}
		return out[i].Target < out[j].Target
	})
	return out
}

func autoMapFrom(file string, obj map[string]any) []AutoMapEntry {
	if obj == nil {
		return nil
	}
	raw, ok := obj["auto_map"]
	if !ok || raw == nil {
		return nil
	}
	am, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(am))
	for k := range am {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []AutoMapEntry
	for _, k := range keys {
		for _, target := range autoMapTargets(am[k]) {
			out = append(out, AutoMapEntry{File: file, Key: k, Target: target})
		}
	}
	return out
}

func autoMapTargets(v any) []string {
	switch t := v.(type) {
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []any:
		var out []string
		for _, item := range t {
			s, ok := item.(string)
			if ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
