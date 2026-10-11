package scan

import (
	"fmt"

	"github.com/cecilkootz/model-inspector/internal/agentcfg"
	"github.com/cecilkootz/model-inspector/internal/model"
)

// agentSurface reports what the chat template, tokenizer and generation
// files say about tool use, and warns on content that looks planted or
// inconsistent. It never fails. The details are raw file text clipped by
// agentcfg; they are sanitized where scan results are displayed.
func agentSurface(m *model.Model) Check {
	c := Check{ID: CheckAgent, Name: "Chat template & tokenizer", Status: StatusSkip}
	if m == nil || !m.Agent.Any() {
		c.Summary = "no chat template or tokenizer files"
		return c
	}
	r := agentcfg.Analyze(m.Agent, m.AgentConfig())
	if len(r.Findings) == 0 {
		c.Status = StatusPass
		c.Summary = r.Tools + "; no suspicious template content"
		c.Facts = r.Facts
		c.Details = capOrdered(r.Facts)
		return c
	}
	c.Status = StatusWarn
	if len(r.Findings) == 1 {
		c.Summary = fmt.Sprintf("1 finding: %s", r.Findings[0].Kind)
	} else {
		c.Summary = fmt.Sprintf("%d findings, strongest: %s", len(r.Findings), r.Findings[0].Kind)
	}
	c.Summary += "; " + r.Tools
	details := make([]string, 0, len(r.Findings)+len(r.Facts))
	for _, f := range r.Findings {
		details = append(details, f.Text)
	}
	c.Facts = r.Facts
	c.Details = capOrdered(append(details, r.Facts...))
	return c
}

// capOrdered is capDetails without the sort: findings come first, strongest
// first, and the order carries that.
func capOrdered(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	if len(items) > maxDetails {
		items = append(items[:maxDetails:maxDetails], fmt.Sprintf("... and %d more", len(items)-maxDetails))
	}
	return items
}
