package agent

import (
	"context"
	"fmt"
	"strings"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

// Preprocessor is the explicit preprocessing stage between the AgentLoop and
// the model. It replaces the former ContextIntervention.BeforeModel hook with
// a cleaner, more testable API.
//
// Responsibilities:
//  1. Event filtering: select which bus events should enter the LLM context
//     (external_input → yes, tool_use → no).
//  2. shouldCallModel judgement: true if there is any external_input to
//     process; false when only tool_use events arrived (tool dispatch handles
//     them without involving the model).
//  3. Message construction: convert selected AgentEvents into model.Message
//     entries suitable for the model.Request.
//  4. event_key prefix injection (when a session is available) so the LLM
//     can pass event_keys to sub-agents.
//  5. Token budget check + SmartCompress trigger when the constructed
//     messages exceed the threshold.
//
// The Preprocessor holds NO business semantics beyond these rules — all
// domain-specific decisions live in the AgentLoop or the tool dispatch layer.
type Preprocessor struct {
	compressor   *SmartCompressor
	tokenCounter TokenCounter
	maxTokens    int
	thresholdPct float64

	// session is the current agent's session. It is set by the AgentLoop
	// when the session becomes available, and cleared on session close.
	// Used by injectEventKeyPrefixes for positional event_key matching.
	session *session.Session
}

// NewPreprocessor creates a Preprocessor with the given dependencies.
// compressor and tokenCounter must not be nil.
func NewPreprocessor(
	compressor *SmartCompressor,
	tokenCounter TokenCounter,
	maxTokens int,
	thresholdPct float64,
) *Preprocessor {
	return &Preprocessor{
		compressor:   compressor,
		tokenCounter: tokenCounter,
		maxTokens:    maxTokens,
		thresholdPct: thresholdPct,
	}
}

// SetSession sets the session reference used for event_key prefix injection.
// Pass nil to clear (e.g., on session close).
func (p *Preprocessor) SetSession(sess *session.Session) {
	p.session = sess
}

// ProcessResult is the output of Preprocessor.Process.
type ProcessResult struct {
	// Messages is the constructed model.Message slice for model.Request.
	// Empty when ShouldCallModel is false.
	Messages []model.Message

	// ShouldCallModel is true when the LLM should be invoked with Messages.
	// False when only tool_use events arrived (handled by tool dispatch).
	ShouldCallModel bool
}

// Process performs event filtering, message construction, event_key injection,
// and token budget checking / compression.
//
// It is safe to call Process concurrently — it holds no mutable state beyond
// the session reference (which is set once and read-only during processing).
func (p *Preprocessor) Process(ctx context.Context, events []*AgentEvent) ProcessResult {
	if len(events) == 0 {
		return ProcessResult{ShouldCallModel: false}
	}

	// Step 1: Filter events and collect messages.
	// external_input → include; tool_use → skip (dispatched separately).
	var messages []model.Message
	hasExternalInput := false
	for _, evt := range events {
		switch evt.Type {
		case tagentevent.TypeExternalInput:
			if evt.Message == nil {
				continue
			}
			// Copy the message to avoid mutating bus payloads.
			msg := *evt.Message
			messages = append(messages, msg)
			hasExternalInput = true
		case TypeToolUse:
			// Tool use events are dispatched by AgentLoop asynchronously.
			// The Preprocessor does not include them in the LLM context.
		default:
			// Unknown event types: log and skip.
			log.Debugf("[Preprocessor] skipping unknown event type %q", evt.Type)
		}
	}

	// Step 2: shouldCallModel judgement.
	if !hasExternalInput {
		return ProcessResult{ShouldCallModel: false}
	}

	// Step 3: inject event_key prefixes (requires session).
	// This activates the event_key visibility chain: LLM sees keys →
	// passes to sub-agents via event_keys parameter.
	if p.session != nil {
		injectEventKeyPrefixesFromSession(&messages, p.session)
	}

	// Step 4: token budget check + SmartCompress.
	// KeepRecentTasks is stateless: restore original value after this call
	// to prevent cross-request state leakage (same as ContextIntervention).
	originalKeepRecent := p.compressor.KeepRecentTasks
	defer func() { p.compressor.KeepRecentTasks = originalKeepRecent }()

	usedTokens := p.tokenCounter.Estimate(messages)
	threshold := int(float64(p.maxTokens) * p.thresholdPct)

	beforeCount := len(messages)
	beforeTokens := usedTokens
	compressed := false

	if usedTokens > threshold {
		maxRounds := 5
		for round := 0; round < maxRounds; round++ {
			// SmartCompressor.Compress requires an *agent.Invocation for
			// context (e.g., summary model). Pass nil — the compressor
			// handles nil invocation gracefully (falls back to Stage 1 only).
			result := p.compressor.Compress(ctx, messages, nil)
			result = ensureUserPrompt(result)
			newTokens := p.tokenCounter.Estimate(result)

			if newTokens >= usedTokens {
				log.Debugf("[Preprocessor] compress round %d stalled (%d->%d), stopping",
					round+1, usedTokens, newTokens)
				break
			}

			messages = result
			usedTokens = newTokens
			compressed = true

			if usedTokens <= threshold {
				break
			}

			if p.compressor.KeepRecentTasks > 1 {
				p.compressor.KeepRecentTasks--
				log.Debugf("[Preprocessor] still over budget, reducing keepRecentTasks=%d",
					p.compressor.KeepRecentTasks)
			}
		}

		if usedTokens > p.maxTokens {
			log.Warnf("[Preprocessor] still over max after compress (%d > %d)",
				usedTokens, p.maxTokens)
		}
	}

	// Step 5: consolidated audit line (one per LLM call).
	logAccess(usedTokens, len(messages), compressed, beforeTokens, beforeCount)

	return ProcessResult{
		Messages:        messages,
		ShouldCallModel: true,
	}
}

// injectEventKeyPrefixesFromSession adds [evt_<KEY>|<type>] prefix to
// user/assistant messages by positionally matching them to Session.Events.
// This is the new-architecture equivalent of injectEventKeyPrefixes in
// context_intervention.go. The logic is identical; only the session source
// differs (Preprocessor.session instead of Invocation.Session).
func injectEventKeyPrefixesFromSession(messages *[]model.Message, sess *session.Session) {
	if sess == nil {
		return
	}

	sess.EventMu.RLock()
	events := sess.Events
	sess.EventMu.RUnlock()

	if len(events) == 0 {
		return
	}

	eventIdx := 0
	for i := range *messages {
		msg := &(*messages)[i]

		// Skip system (not event source) and tool messages (belong to
		// previous assistant event).
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

// logAccess outputs a single audit line per LLM invocation.
func logAccess(
	tokens int,
	msgCount int,
	compressed bool,
	beforeTokens int,
	beforeCount int,
) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[Preprocessor] tokens=%d/%d msgs=%d",
		tokens, 0 /* maxTokens unknown here */, msgCount))

	if compressed {
		sb.WriteString(fmt.Sprintf(" compressed(%d->%d tokens %d->%d msgs)",
			beforeTokens, tokens, beforeCount, msgCount))
	}

	log.Infof(sb.String())
}
