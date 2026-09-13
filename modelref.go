package tagent

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// BuildDirectRequest assembles a model.Request for a direct (non-agent) call
// site from a ModelRef. Only explicitly set knobs are applied so per-site
// defaults (e.g. summaryMaxTokens) stay authoritative when the ref omits them.
// (tagent-unify-model-call-config.)
func BuildDirectRequest(ref ModelRef, msgs []model.Message) *model.Request {
	req := &model.Request{Messages: msgs}
	if ref.Temperature != nil {
		req.Temperature = ref.Temperature
	}
	if ref.MaxTokens != nil {
		req.MaxTokens = ref.MaxTokens
	}
	if ref.ThinkingEnabled != nil {
		req.ThinkingEnabled = ref.ThinkingEnabled
	}
	if ref.ThinkingTokens != nil {
		req.ThinkingTokens = ref.ThinkingTokens
	}
	if ref.ReasoningEffort != nil {
		req.ReasoningEffort = ref.ReasoningEffort
	}
	return req
}

// CallDirectModel runs one direct completion for a resolved call site and
// collects the final text. It carries the reasoning-fallback drain shared by
// summary/judge call sites: when a reasoning model returns empty Content but
// non-empty ReasoningContent, the reasoning text is used instead of failing.
// (tagent-unify-model-call-config.)
func CallDirectModel(ctx context.Context, m model.Model, req *model.Request) (string, error) {
	respCh, err := m.GenerateContent(ctx, req)
	if err != nil {
		return "", err
	}
	var result, reasoning string
	for resp := range respCh {
		if resp.Error != nil {
			return "", errDirectCall{resp.Error.Message}
		}
		if len(resp.Choices) > 0 {
			result += resp.Choices[0].Message.Content
			reasoning += resp.Choices[0].Message.ReasoningContent
		}
	}
	if result == "" && reasoning != "" {
		log.Warnf("[modelref] empty content, falling back to reasoning_content (%d chars)", len(reasoning))
		result = reasoning
	}
	return result, nil
}

// errDirectCall wraps upstream model errors for direct call sites.
type errDirectCall struct{ msg string }

func (e errDirectCall) Error() string { return "model error: " + e.msg }

// FoldModelRefAliases merges legacy flat knob declarations into their unified
// ModelRef holders. Explicit ModelRef fields win per-field; legacy values only
// fill fields the ModelRef leaves unset. (tagent-unify-model-call-config.)
func (c *Config) FoldModelRefAliases() {
	for name, ac := range c.Agents {
		changed := false
		if ac.Compress.SummaryModel != "" && ac.Compress.Summary.Model == "" {
			ac.Compress.Summary.Model = ac.Compress.SummaryModel
			ac.Compress.SummaryModel = ""
			changed = true
			log.Warnf("[tagent] agent %q: compress.summary_model is deprecated, folded into compress.summary.model=%q", name, ac.Compress.Summary.Model)
		}
		if ac.Compress.SummaryProvider != "" && ac.Compress.Summary.Provider == "" {
			ac.Compress.Summary.Provider = ac.Compress.SummaryProvider
			ac.Compress.SummaryProvider = ""
			changed = true
			log.Warnf("[tagent] agent %q: compress.summary_provider is deprecated, folded into compress.summary.provider=%q", name, ac.Compress.Summary.Provider)
		}
		if changed {
			c.Agents[name] = ac
		}
	}
}
