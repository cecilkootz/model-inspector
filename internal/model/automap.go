package model

import (
	"path/filepath"
	"sort"
	"strings"

	"origin.cursor.com/cecilkootz/model-inspector/internal/jsonx"
	"origin.cursor.com/cecilkootz/model-inspector/internal/safeopen"
)

// AutoMapEntry is one auto_map value. Targets are names only; nothing is imported.
type AutoMapEntry struct {
	File   string
	Key    string
	Target string
}

// codeExts are name-based code-executing formats. Contents are not read.
var codeExts = map[string]struct{}{
	".bin": {}, ".pt": {}, ".pth": {}, ".ckpt": {},
	".pkl": {}, ".pickle": {}, ".joblib": {},
	".npy": {}, ".pyc": {}, ".py": {},
}

// auxConfigNames are the small JSON files, besides config.json, whose
// top-level auto_map is part of the name-based check.
var auxConfigNames = []string{
	"tokenizer_config.json",
	"processor_config.json",
	"preprocessor_config.json",
}

// CodeExecFiles returns files whose extension is a code-executing format.
// The match is on the base name only.
func (m *Model) CodeExecFiles() []File {
	if m == nil {
		return nil
	}
	var out []File
	for _, f := range m.Files {
		ext := strings.ToLower(filepath.Ext(f.Name))
		if _, ok := codeExts[ext]; ok {
			out = append(out, f)
		}
	}
	return out
}

// AutoMapEntries reads top-level auto_map objects. config.json comes from
// the already-parsed config. The other three files are read through the
// same size-capped helper as config.json. Missing files are skipped.
// A file that cannot be read is omitted here; load warnings cover it.
func (m *Model) AutoMapEntries() []AutoMapEntry {
	if m == nil {
		return nil
	}
	var out []AutoMapEntry
	if m.Config != nil {
		out = append(out, autoMapFrom("config.json", m.Config)...)
	}
	byName := make(map[string]File, len(m.Files))
	for _, f := range m.Files {
		byName[f.Name] = f
	}
	roots := m.allowedRoots()
	for _, name := range auxConfigNames {
		f, ok := byName[name]
		if !ok || f.Broken {
			continue
		}
		raw, err := safeopen.ReadAll(filepath.Join(m.Dir, name), maxSmallJSON, roots...)
		if err != nil {
			continue
		}
		v, err := jsonx.Unmarshal(raw)
		if err != nil {
			continue
		}
		obj, _ := v.(map[string]any)
		out = append(out, autoMapFrom(name, obj)...)
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
