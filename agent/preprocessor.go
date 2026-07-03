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
func (p *Preprocessor) Process(ctx context.Context, batch []*AgentEvent, sess *session.Session) ProcessResult {
	// Step 1: shouldCallModel judgement — based ONLY on the bus batch.
	// external_input triggers model call; tool_use alone does not.
	hasExternalInput := false
	externalCount := 0
	for _, evt := range batch {
		if evt != nil && evt.Type == tagentevent.TypeExternalInput {
			hasExternalInput = true
			externalCount++
		}
	}
	log.Debugf("[Preprocessor] batch size=%d external_input=%d", len(batch), externalCount)
	if !hasExternalInput {
		log.Debugf("[Preprocessor] no external_input, skip model call")
		return ProcessResult{ShouldCallModel: false}
	}

	// Step 2: Build messages from session.Events (complete conversation history).
	// The session has already been updated by onEvent callbacks before Process
	// is called, so it contains the latest external_input + prior history.
	var messages []model.Message
	if sess != nil {
		sess.EventMu.RLock()
		for i := range sess.Events {
			evt := &sess.Events[i]
			if evt.Response != nil && len(evt.Response.Choices) > 0 {
				msg := evt.Response.Choices[0].Message
				messages = append(messages, msg)
			}
		}
		sess.EventMu.RUnlock()
	}

	log.Debugf("[Preprocessor] built %d messages from session (events=%d)",
		len(messages), func() int {
			if sess == nil {
				return 0
			}
			sess.EventMu.RLock()
			defer sess.EventMu.RUnlock()
			return len(sess.Events)
		}())

	if sess != nil {
		injectEventKeyPrefixesFromSession(&messages, sess)
	}

	// Step 4: token budget check + SmartCompress.
	// KeepRecentTasks is stateless: restore original value after this call
	// to prevent cross-request state leakage.
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
	p.logAccess(usedTokens, len(messages), compressed, beforeTokens, beforeCount)

	// Step 6: debug-level context dump for deep inspection.
	log.Debugf("[Preprocessor] final context messages:\n%s", formatMessages(messages))

	return ProcessResult{
		Messages:        messages,
		ShouldCallModel: true,
	}
}

// injectEventKeyPrefixesFromSession adds [evt_<KEY>|<type>] prefix to
// user/assistant messages by positionally matching them to Session.Events.
// This replaces the previous injectEventKeyPrefixes implementation. The
// logic is identical; only the session source differs (Preprocessor.session
// instead of Invocation.Session).
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
		log.Debugf("[Preprocessor] prefix injected: msg[%d] -> %s", i, msg.Content[:minInt(len(msg.Content), 120)])
	}
}

// minInt returns the smaller of a and b.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
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

// logAccess outputs a single audit line per LLM invocation.
func (p *Preprocessor) logAccess(
	tokens int,
	msgCount int,
	compressed bool,
	beforeTokens int,
	beforeCount int,
) {
	threshold := int(float64(p.maxTokens) * p.thresholdPct)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[Preprocessor] tokens=%d/%d(threshold=%d) msgs=%d",
		tokens, p.maxTokens, threshold, msgCount))

	if compressed {
		sb.WriteString(fmt.Sprintf(" compressed(%d->%d tokens %d->%d msgs)",
			beforeTokens, tokens, beforeCount, msgCount))
	}

	log.Infof(sb.String())
}

// formatMessages returns a human-readable summary of messages for debug logs.
func formatMessages(messages []model.Message) string {
	var sb strings.Builder
	for i, msg := range messages {
		role := msg.Role
		if role == "" {
			role = "unknown"
		}
		content := msg.Content
		if len(content) > 200 {
			content = content[:200] + "..."
		}
		toolInfo := ""
		if len(msg.ToolCalls) > 0 {
			names := make([]string, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				names = append(names, tc.Function.Name)
			}
			toolInfo = fmt.Sprintf(" tool_calls=%v", names)
		}
		if msg.ToolID != "" {
			toolInfo += fmt.Sprintf(" tool_id=%s", msg.ToolID)
		}
		sb.WriteString(fmt.Sprintf("  [%d] %s: %q%s\n", i, role, content, toolInfo))
	}
	return sb.String()
}
