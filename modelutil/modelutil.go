// Package modelutil hosts the shared assembly for direct (non-agent) model call
// sites — summary compression and the evolution judge — so both speak the same
// ModelRef vocabulary as agents.
// 契约: docs/wiki/platform/platform-subsystems.md#model-wiring
package modelutil

import (
	"context"
	"errors"
	"fmt"
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
//
// The drain is bounded by the caller's context (O3.4): every iteration selects
// on ctx.Done, so a provider that stops delivering can neither outlive the
// summary deadline nor ignore a parent cancellation — Call returns the context
// error and DROPS the stream, which is exactly the "no late rewrite" property the
// synchronous summary round needs (there is no background reader that could hand
// a belated answer back to the caller). Providers keep owning their send side
// and are bound by the same ctx they were handed here; nothing is retried.
func Call(ctx context.Context, m model.Model, req *model.Request) (string, error) {
	ch, err := m.GenerateContent(ctx, req)
	if err != nil {
		return "", err
	}
	if ch == nil {
		return "", fmt.Errorf("modelutil.Call: %w", ErrNilStream)
	}
	var streamed strings.Builder
	var lastFull, lastReasoning string
Collect:
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case resp, ok := <-ch:
			if !ok {
				break Collect
			}
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

// ErrNilStream names the determinable failure of a provider that returned a nil
// response channel without an error. Ranging over a nil channel blocks forever,
// so the bounded synchronous summary call must treat it as a failure instead of
// hanging the BeforeModel round.
var ErrNilStream = errors.New("modelutil: provider returned a nil response stream")

type errDirect struct{ msg string }

func (e errDirect) Error() string { return e.msg }
