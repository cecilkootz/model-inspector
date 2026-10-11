package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cecilkootz/model-inspector/internal/agentcfg"
	"github.com/cecilkootz/model-inspector/internal/fixture"
	"github.com/cecilkootz/model-inspector/internal/model"
)

func loadFixture(t *testing.T, files []fixture.File, err error) *model.Model {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func agentCheck(t *testing.T, m *model.Model) Check {
	t.Helper()
	return checkID(t, m, CheckAgent)
}

func TestAgentSkipWithoutFiles(t *testing.T) {
	files, _, err := fixture.TinyQwen()
	c := agentCheck(t, loadFixture(t, files, err))
	if c.Status != StatusSkip || c.Summary != "no chat template or tokenizer files" {
		t.Fatalf("%+v", c)
	}
}

func TestAgentHermesPasses(t *testing.T) {
	files, err := fixture.AgentHermes()
	c := agentCheck(t, loadFixture(t, files, err))
	if c.Status != StatusPass {
		t.Fatalf("%+v", c)
	}
	if want := "tool calling: yes (Hermes-style tool_call tags); no suspicious template content"; c.Summary != want {
		t.Fatalf("summary %q", c.Summary)
	}
	detailsContain(t, c, "system role: template handles it", "added tokens: 2 (2 special), highest id 1",
		"generation defaults: temperature 0.7", "stop tokens: config.json [0]; generation_config.json [0,1]")
}

func TestAgentTampered(t *testing.T) {
	files, err := fixture.AgentTampered()
	c := agentCheck(t, loadFixture(t, files, err))
	if c.Status != StatusWarn {
		t.Fatalf("%+v", c)
	}
	if !strings.HasPrefix(c.Summary, "5 findings, strongest: instruction-like literal text") {
		t.Fatalf("summary %q", c.Summary)
	}
	detailsContain(t, c, "https://evil.example/collect", "invisible or control characters", "base64-looking run",
		"hard-coded tool name", "heuristic, not proof")
	strict := findCheck(t, RunOptions(loadFixture(t, files, err), Options{Strict: true}), CheckAgent)
	if strict.Status != StatusWarn || strict.EffectiveStatus != StatusFail {
		t.Fatalf("strict: %+v", strict)
	}
}

func TestAgentDisagree(t *testing.T) {
	files, err := fixture.AgentDisagree()
	c := agentCheck(t, loadFixture(t, files, err))
	if c.Status != StatusWarn {
		t.Fatalf("%+v", c)
	}
	detailsContain(t, c, "templates disagree: tokenizer_config.json", "chat_template.jinja", "chat_template.json")
}

func TestAgentTokenOverflow(t *testing.T) {
	files, err := fixture.AgentTokenOverflow()
	c := agentCheck(t, loadFixture(t, files, err))
	if c.Status != StatusWarn || !strings.Contains(c.Summary, "added token id out of range") {
		t.Fatalf("%+v", c)
	}
	detailsContain(t, c, "1 added token ids are at or above vocab_size 2 (lowest 99999, highest 99999)")
}

func TestAgentTokenizerJSONOverCap(t *testing.T) {
	files, err := fixture.AgentHermes()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "tokenizer.json")
	if err := os.Truncate(p, agentcfg.MaxJSONBytes+5<<20); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := agentCheck(t, m)
	if c.Status != StatusWarn {
		t.Fatalf("%+v", c)
	}
	detailsContain(t, c, "tokenizer.json not read: 69 MiB over cap (64 MiB)")
	// The template and the rest of the check still ran.
	detailsContain(t, c, "system role: template handles it")
}

func TestAgentOnlyGenerationConfig(t *testing.T) {
	files, _, err := fixture.TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, fixture.File{Name: "generation_config.json", Data: []byte(`{"temperature": 0.5}`)})
	c := agentCheck(t, loadFixture(t, files, nil))
	if c.Status != StatusPass || !strings.HasPrefix(c.Summary, "tool calling: no template") {
		t.Fatalf("%+v", c)
	}
}

func TestAgentMalformedFileWarns(t *testing.T) {
	files, _, err := fixture.TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, fixture.File{Name: "generation_config.json", Data: []byte(`{`)})
	c := agentCheck(t, loadFixture(t, files, nil))
	if c.Status != StatusWarn {
		t.Fatalf("%+v", c)
	}
	detailsContain(t, c, "generation_config.json not read")
}

func TestAgentContextAndStop(t *testing.T) {
	files, _, err := fixture.TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	for i := range files {
		if files[i].Name == "config.json" {
			files[i].Data = []byte(strings.Replace(string(files[i].Data), `"vocab_size": 2,`,
				`"vocab_size": 2, "eos_token_id": 0, "rope_scaling": {"rope_type": "yarn", "factor": 4, "original_max_position_embeddings": 32},`, 1))
		}
	}
	files = append(files,
		fixture.File{Name: "generation_config.json", Data: []byte(`{"eos_token_id": [1]}`)},
		fixture.File{Name: "tokenizer_config.json", Data: []byte(`{"model_max_length": 99}`)},
	)
	c := agentCheck(t, loadFixture(t, files, nil))
	detailsContain(t, c, "rope_scaling yarn x4 of original 32, effective 128", "model_max_length 99 disagrees with 128",
		"stop tokens disagree: config.json has [0], generation_config.json has [1]")
}

func TestAgentHostileDetailsAreRawButBounded(t *testing.T) {
	files, err := fixture.AgentHostile()
	m := loadFixture(t, files, err)
	c := agentCheck(t, m)
	if c.Status != StatusWarn {
		t.Fatalf("%+v", c)
	}
	if len(c.Details) > maxDetails+1 {
		t.Fatalf("%d details", len(c.Details))
	}
}
