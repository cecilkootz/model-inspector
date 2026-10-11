package scan

import (
	"strings"
	"testing"

	"github.com/cecilkootz/model-inspector/internal/fixture"
)

func TestSkipPromotedWhenShardsExist(t *testing.T) {
	a, _ := mustShard(t, []fixture.Tensor{ten("a", "F32", 1)})
	b, _ := mustShard(t, []fixture.Tensor{ten("b", "F32", 1)})
	m := loadDir(t, map[string][]byte{
		"model-00001-of-00002.safetensors": a,
		"model-00002-of-00002.safetensors": b,
	})
	res := Run(m)
	idx := findCheck(t, res, CheckIndex)
	if idx.Status != StatusSkip || idx.EffectiveStatus != StatusWarn {
		t.Fatalf("index %+v", idx)
	}
	if !strings.Contains(idx.Summary, "(skipped although shards exist)") {
		t.Fatalf("summary %q", idx.Summary)
	}
	if res.Overall != StatusWarn {
		t.Fatalf("overall %s", res.Overall)
	}
	// Strict promotes a raw warn only. A promoted skip stays warn.
	strict := RunOptions(m, Options{Strict: true})
	idx = findCheck(t, strict, CheckIndex)
	if idx.Status != StatusSkip || idx.EffectiveStatus != StatusWarn {
		t.Fatalf("strict index %+v", idx)
	}
	code := findCheck(t, res, CheckCode)
	if code.Status != StatusSkip || code.EffectiveStatus != StatusSkip {
		t.Fatalf("code-content was promoted: %+v", code)
	}
	meta := findCheck(t, res, CheckMetadata)
	if meta.Status != StatusPlanned || meta.EffectiveStatus != StatusPlanned {
		t.Fatalf("planned was promoted: %+v", meta)
	}
	if meta.Summary != "not run: planned" {
		t.Fatalf("metadata summary %q", meta.Summary)
	}
}

func TestSingleShardWithoutIndexStaysSkip(t *testing.T) {
	a, _ := mustShard(t, []fixture.Tensor{ten("a", "F32", 1)})
	m := loadDir(t, map[string][]byte{"model.safetensors": a})
	idx := checkID(t, m, CheckIndex)
	if idx.Status != StatusSkip || idx.EffectiveStatus != StatusSkip {
		t.Fatalf("%+v", idx)
	}
	if strings.Contains(idx.Summary, "skipped although") {
		t.Fatalf("single shard was promoted: %q", idx.Summary)
	}
}

func TestRuleAndNotProvenWording(t *testing.T) {
	banned := []string{"safe", "clean", "verified", "secure", "trusted"}
	for _, id := range []string{CheckHeader, CheckFiles, CheckOffsets, CheckIndex, CheckCodeExec, CheckCode, CheckHash, CheckMetadata} {
		for _, text := range []string{Rule(id), NotProven(id), Why(id)} {
			lower := strings.ToLower(text)
			for _, word := range banned {
				if containsWord(lower, word) {
					t.Errorf("%s text contains %q: %s", id, word, text)
				}
			}
		}
	}
	if Rule("nope") != "" || NotProven("nope") != "" {
		t.Fatal("unknown id returned a rule")
	}
}

func containsWord(s, word string) bool {
	for _, field := range strings.FieldsFunc(s, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	}) {
		if field == word {
			return true
		}
	}
	return false
}

func TestPromoteSkipNil(t *testing.T) {
	promoteSkip(nil, &Check{ID: CheckIndex, Status: StatusSkip})
	c := Check{ID: CheckHash, Status: StatusPlanned, EffectiveStatus: StatusPlanned}
	promoteSkip(nil, &c)
	if c.EffectiveStatus != StatusPlanned {
		t.Fatalf("%+v", c)
	}
}
