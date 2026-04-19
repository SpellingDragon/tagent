package agent

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ContextIntervention intercepts model requests in BeforeModel callback.
// It performs token budget checking and triggers SmartCompress when needed.
//
// Core principle: this is a "view transformation" — it modifies the messages
// sent to the LLM, but does NOT modify the Session.
type ContextIntervention struct {
	compressor   *SmartCompressor
	tokenCounter TokenCounter
	maxTokens    int
	thresholdPct float64
}

// NewContextIntervention creates a new ContextIntervention.
func NewContextIntervention(
	compressor *SmartCompressor,
	tokenCounter TokenCounter,
	maxTokens int,
	thresholdPct float64,
) *ContextIntervention {
	return &ContextIntervention{
		compressor:   compressor,
		tokenCounter: tokenCounter,
		maxTokens:    maxTokens,
		thresholdPct: thresholdPct,
	}
}

// BeforeModel is the BeforeModel callback that checks token budget
// and compresses messages if needed.
func (ci *ContextIntervention) BeforeModel(
	ctx context.Context,
	args *model.BeforeModelArgs,
) (*model.BeforeModelResult, error) {
	if args == nil || args.Request == nil || len(args.Request.Messages) == 0 {
		return nil, nil
	}

	usedTokens := ci.tokenCounter.Estimate(args.Request.Messages)
	threshold := int(float64(ci.maxTokens) * ci.thresholdPct)

	if usedTokens > threshold {
		log.Infof("ContextIntervention: token usage %d exceeds threshold %d (max=%d, pct=%.0f%%), compressing",
			usedTokens, threshold, ci.maxTokens, ci.thresholdPct*100)

		compressed := ci.compressor.Compress(ctx, args.Request.Messages)
		compressed = ensureUserPrompt(compressed)
		args.Request.Messages = compressed

		newTokens := ci.tokenCounter.Estimate(args.Request.Messages)
		log.Infof("ContextIntervention: compressed from %d to %d tokens (%d messages)",
			usedTokens, newTokens, len(compressed))
	}

	return nil, nil
}

// ensureUserPrompt checks that the compressed messages contain at least one user prompt.
// If not, it appends a "继续" user message so the LLM knows to continue.
// This is critical: the LLM must never see only agent_output messages without a user prompt.
func ensureUserPrompt(messages []model.Message) []model.Message {
	hasUser := false
	for _, msg := range messages {
		if msg.Role == model.RoleUser {
			hasUser = true
			break
		}
	}
	if !hasUser {
		messages = append(messages, model.Message{
			Role:    model.RoleUser,
			Content: "继续",
		})
	}
	return messages
}
