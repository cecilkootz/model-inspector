package model

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cecilkootz/model-inspector/internal/agentcfg"
	"github.com/cecilkootz/model-inspector/internal/fixture"
)

// bigTokenizerJSON is a BPE-shaped tokenizer.json of about 15 MB.
func bigTokenizerJSON() []byte {
	var b strings.Builder
	b.WriteString(`{"version":"1.0","added_tokens":[{"id":0,"content":"<|im_end|>","special":true}],"normalizer":null,"model":{"type":"BPE","vocab":{`)
	for i := 0; i < 250000; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"tok%dxyz":%d`, i, i)
	}
	b.WriteString(`},"merges":[`)
	for i := 0; i < 500000; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"tok%d tok%d"`, i, i+1)
	}
	b.WriteString(`]}}`)
	return []byte(b.String())
}

func TestTokenizerJSONLoadCost(t *testing.T) {
	files, _, err := fixture.TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	load := func() time.Duration {
		best := time.Hour
		for i := 0; i < 5; i++ {
			start := time.Now()
			if _, err := Load(dir); err != nil {
				t.Fatal(err)
			}
			best = min(best, time.Since(start))
		}
		return best
	}
	before := load()
	data := bigTokenizerJSON()
	if err := os.WriteFile(filepath.Join(dir, "tokenizer.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	after := load()
	t.Logf("tokenizer.json %.1f MB: Load %v without, %v with (+%v)", float64(len(data))/1e6, before, after, after-before)
	m, _ := Load(dir)
	if m.Partial {
		t.Fatalf("a large tokenizer.json set Partial: %v", m.Warnings)
	}
	if m.Agent.TokJSON == nil || m.Agent.TokJSON.VocabSize != 250000 {
		t.Fatalf("vocab not counted: %+v", m.Agent.TokJSON)
	}
}

func TestAgentFilesNeverSetPartial(t *testing.T) {
	files, _, err := fixture.TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "tokenizer.json")
	for name, setup := range map[string]func(){
		"garbage":  func() { os.WriteFile(p, []byte("{not json"), 0o644) },
		"over cap": func() { os.Remove(p); os.WriteFile(p, nil, 0o644); os.Truncate(p, agentcfg.MaxJSONBytes+1) },
		"dir":      func() { os.Remove(p); os.Mkdir(p, 0o755) },
		"dangling": func() { os.Remove(p); os.Symlink(filepath.Join(dir, "gone"), p) },
	} {
		setup()
		m, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		if m.Partial {
			t.Errorf("%s: Partial=%v warnings=%v", name, m.Partial, m.Warnings)
		}
		if st := m.Agent.Info(agentcfg.FileTokenizerJSON).State; st != agentcfg.Failed && st != agentcfg.OverCap {
			t.Errorf("%s: state %v", name, st)
		}
		os.RemoveAll(p)
	}
}
