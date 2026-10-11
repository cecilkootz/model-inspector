package model

import (
	"math"

	"github.com/cecilkootz/model-inspector/internal/agentcfg"
)

// AgentConfig collects the config.json and tensor facts the agent-surface
// check compares with tokenizer and generation files. Nested text scopes
// come first, as in ConfigShapes. Unknown values are zero.
func (m *Model) AgentConfig() agentcfg.ConfigFacts {
	var c agentcfg.ConfigFacts
	if m == nil {
		return c
	}
	if rows, ok := m.TokenEmbedRows(); ok {
		c.EmbedRows = rows
	}
	if m.Config == nil {
		return c
	}
	c.VocabSize = m.textInt("vocab_size")
	c.MaxPos = m.textInt("max_position_embeddings")
	c.OrigMaxPos = m.textInt("original_max_position_embeddings")
	if _, v, ok := m.textField("eos_token_id"); ok {
		c.EOS = agentcfg.ParseIDs(v)
	}
	if _, v, ok := m.textField("bos_token_id"); ok {
		c.BOS = agentcfg.ParseIDs(v)
	}
	for _, key := range []string{"rope_scaling", "rope_parameters"} {
		_, v, ok := m.textField(key)
		rs, _ := v.(map[string]any)
		if !ok || rs == nil {
			continue
		}
		c.RopeType = strOf(rs["rope_type"])
		if c.RopeType == "" {
			c.RopeType = strOf(rs["type"])
		}
		if f := floatOf(rs["factor"]); f > 0 && !math.IsInf(f, 0) {
			c.RopeFactor = f
		}
		if n, problem := wholeNumber(rs["original_max_position_embeddings"]); problem == "" && n > 0 {
			c.OrigMaxPos = n
		}
		break
	}
	return c
}

func (m *Model) textInt(key string) int64 {
	_, v, ok := m.textField(key)
	if !ok {
		return 0
	}
	n, problem := wholeNumber(v)
	if problem != "" {
		return 0
	}
	return n
}
