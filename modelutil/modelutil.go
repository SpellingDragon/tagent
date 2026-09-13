// Package modelutil hosts the shared assembly for direct (non-agent) model
// call sites — summary compression and the evolution judge. Both previously
// hand-rolled model.Request without generation knobs; now they speak the same
// ModelRef vocabulary as agents. (tagent-unify-model-call-config.)
package modelutil

import (
	"context"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// Knobs carries the generation settings a direct call site may override.
// Nil fields stay untouched so per-site defaults (e.g. summaryMaxTokens)
// remain authoritative when the ModelRef omits them.
type Knobs struct {
	Temperature          *float64
	MaxTokens            *int
	ThinkingEnabled      *bool
	ThinkingTokens       *int
	ReasoningEffort      *string
	ReasoningContentMode string
}

// BuildRequest assembles a model.Request with generation knobs applied.
func BuildRequest(msgs []model.Message, k Knobs) *model.Request {
	req := &model.Request{Messages: msgs}
	if k.Temperature != nil {
		req.Temperature = k.Temperature
	}
	if k.MaxTokens != nil {
		req.MaxTokens = k.MaxTokens
	}
	if k.ThinkingEnabled != nil {
		req.ThinkingEnabled = k.ThinkingEnabled
	}
	if k.ThinkingTokens != nil {
		req.ThinkingTokens = k.ThinkingTokens
	}
	if k.ReasoningEffort != nil {
		req.ReasoningEffort = k.ReasoningEffort
	}
	return req
}

// Call runs one direct completion and collects the final text. It carries the
// reasoning-fallback drain shared by summary/judge sites: when a reasoning
// model returns empty Content but non-empty ReasoningContent, the reasoning
// text is used instead of failing (first generalized from the summary site).
func Call(ctx context.Context, m model.Model, req *model.Request) (string, error) {
	ch, err := m.GenerateContent(ctx, req)
	if err != nil {
		return "", err
	}
	var streamed strings.Builder
	var lastFull, lastReasoning string
	for resp := range ch {
		if resp == nil {
			continue
		}
		if resp.Error != nil {
			return "", errDirect{msg: resp.Error.Message}
		}
		for _, c := range resp.Choices {
			if c.Delta.Content != "" {
				streamed.WriteString(c.Delta.Content)
			}
			if c.Message.Content != "" {
				lastFull = c.Message.Content
			}
			if c.Message.ReasoningContent != "" {
				lastReasoning = c.Message.ReasoningContent
			}
		}
	}
	if streamed.Len() > 0 {
		return streamed.String(), nil
	}
	if lastFull != "" {
		return lastFull, nil
	}
	if lastReasoning != "" {
		log.Warnf("[modelutil] empty content, falling back to reasoning_content (%d chars)", len(lastReasoning))
		return lastReasoning, nil
	}
	return "", nil
}

type errDirect struct{ msg string }

func (e errDirect) Error() string { return e.msg }
