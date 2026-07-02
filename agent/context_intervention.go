package agent

import (
	"context"
	"fmt"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/agent"
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

// BeforeModel is the BeforeModel callback.
// 1. Inject event_key prefixes (activates event_key visibility chain).
// 2. Token budget check & SmartCompress.
// 3. Consolidated audit log (one line per LLM call).
//
// Deprecated: Use Preprocessor.Process instead. This method is retained
// only for the transition period and will be removed when the AgentLoop
// migration (event-driven-agent-loop change) is complete.
func (ci *ContextIntervention) BeforeModel(
	ctx context.Context,
	args *model.BeforeModelArgs,
) (*model.BeforeModelResult, error) {
	if args == nil || args.Request == nil || len(args.Request.Messages) == 0 {
		return nil, nil
	}

	inv, _ := agent.InvocationFromContext(ctx)

	// Inject event_key prefixes so LLM can see and select keys for sub-agents.
	// Must happen before compression so collectCompressedKeys can extract keys.
	injectEventKeyPrefixes(args, inv)

	// KeepRecentTasks stateless: restore original value after this call
	// to prevent cross-request state leakage.
	originalKeepRecent := ci.compressor.KeepRecentTasks
	defer func() { ci.compressor.KeepRecentTasks = originalKeepRecent }()

	usedTokens := ci.tokenCounter.Estimate(args.Request.Messages)
	threshold := int(float64(ci.maxTokens) * ci.thresholdPct)

	beforeCount := len(args.Request.Messages)
	beforeTokens := usedTokens
	compressed := false

	if usedTokens > threshold {
		maxRounds := 5
		for round := 0; round < maxRounds; round++ {
			result := ci.compressor.Compress(ctx, args.Request.Messages, inv)
			result = ensureUserPrompt(result)
			newTokens := ci.tokenCounter.Estimate(result)

			if newTokens >= usedTokens {
				log.Debugf("[CI] compress round %d stalled (%d->%d), stopping",
					round+1, usedTokens, newTokens)
				break
			}

			args.Request.Messages = result
			usedTokens = newTokens
			compressed = true

			if usedTokens <= threshold {
				break
			}

			if ci.compressor.KeepRecentTasks > 1 {
				ci.compressor.KeepRecentTasks--
				log.Debugf("[CI] still over budget, reducing keepRecentTasks=%d",
					ci.compressor.KeepRecentTasks)
			}
		}

		if usedTokens > ci.maxTokens {
			log.Warnf("[CI] still over max after compress (%d > %d)",
				usedTokens, ci.maxTokens)
		}
	}

	// Consolidated audit line — one per LLM call
	ci.logAccess(inv, usedTokens, len(args.Request.Messages), compressed, beforeTokens, beforeCount)

	return nil, nil
}

// logAccess outputs a single audit line per LLM invocation.
func (ci *ContextIntervention) logAccess(
	inv *agent.Invocation,
	tokens int,
	msgCount int,
	compressed bool,
	beforeTokens int,
	beforeCount int,
) {
	agentName := "unknown"
	if inv != nil {
		agentName = inv.AgentName
	}

	// Find the active prompt (last user message, truncated to 120 chars)
	var prompt string
	if inv != nil && inv.Message.Content != "" {
		prompt = inv.Message.Content
	}
	if len(prompt) > 120 {
		prompt = prompt[:120] + "..."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[CI] agent=%s tokens=%d/%d msgs=%d prompt=%q",
		agentName, tokens, ci.maxTokens, msgCount, prompt))

	if compressed {
		sb.WriteString(fmt.Sprintf(" compressed(%d->%d tokens %d->%d msgs)",
			beforeTokens, tokens, beforeCount, msgCount))
	}

	log.Infof(sb.String())
}

// ensureUserPrompt checks that the compressed messages contain at least one user prompt.
// If not, it appends a guidance message so the LLM knows the context was compressed
// and can ask for new tasks.
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
			Content: "（以上是对话历史摘要。如果有新任务，请告诉我。）",
		})
	}
	return messages
}

// injectEventKeyPrefixes adds [evt_<KEY>|<type>] prefix to user/assistant messages
// by positionally matching them to Session.Events. This is the activation point for
// the entire event_key visibility chain: LLM sees keys → can pass to sub-agents via
// event_keys parameter → collectCompressedKeys can extract keys from compressed segments.
//
// Positional matching: skip system messages (not event sources) and tool messages
// (belong to previous assistant event). Match remaining user/assistant messages to
// events by index. Safe degradation when inv/Session is nil or events are exhausted.
func injectEventKeyPrefixes(args *model.BeforeModelArgs, inv *agent.Invocation) {
	if inv == nil || inv.Session == nil {
		return
	}

	inv.Session.EventMu.RLock()
	events := inv.Session.Events
	inv.Session.EventMu.RUnlock()

	if len(events) == 0 {
		return
	}

	eventIdx := 0
	for i := range args.Request.Messages {
		msg := &args.Request.Messages[i]

		// Skip system (not event source) and tool messages (belong to previous assistant event)
		if msg.Role == model.RoleSystem || msg.Role == model.RoleTool {
			continue
		}

		if eventIdx >= len(events) {
			break
		}

		evt := &events[eventIdx]
		eventIdx++

		keyBytes, ok := evt.StateDelta["event_key"]
		if !ok || len(keyBytes) == 0 {
			continue
		}

		eventType := "unknown"
		if typeBytes, ok := evt.StateDelta["event_type"]; ok && len(typeBytes) > 0 {
			eventType = string(typeBytes)
		}

		msg.Content = fmt.Sprintf("[evt_%s|%s] %s", string(keyBytes), eventType, msg.Content)
	}
}
