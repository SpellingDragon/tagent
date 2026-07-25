package compress

import "trpc.group/trpc-go/trpc-agent-go/model"

// TokenCounter estimates token count for message lists.
type TokenCounter interface {
	Estimate(messages []model.Message) int
}

// DefaultTokenCounter estimates tokens using a character-based heuristic.
type DefaultTokenCounter struct {
	CharsPerToken float64
}

func NewDefaultTokenCounter() *DefaultTokenCounter {
	return &DefaultTokenCounter{CharsPerToken: 2.0}
}

func (c *DefaultTokenCounter) Estimate(messages []model.Message) int {
	total := 0
	for i := range messages {
		msg := &messages[i]
		total += int(float64(len([]rune(msg.Content))) / c.CharsPerToken)
		total += 10
		if len(msg.ToolCalls) > 0 {
			total += 20 * len(msg.ToolCalls)
		}
	}
	if total < 1 {
		total = 1
	}
	return total
}

// ---------------------------------------------------------------------------

// truncateString truncates s to at most n characters, appending "..." if truncated.
func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ---------------------------------------------------------------------------

// EventTypeToRole maps an event type to its pairing-free timeline role
// (unified-event-projection D3):
//
//	external_input → user
//	agent_output   → assistant
//	action_command → user (tool results are input events, never role=tool)
//	thinking_plan  → assistant
//	(default)      → user (safe degradation)
func EventTypeToRole(eventType string) model.Role {
	switch eventType {
	case "external_input":
		return model.RoleUser
	case "agent_output":
		return model.RoleAssistant
	case "action_command":
		return model.RoleUser
	case "thinking_plan":
		return model.RoleAssistant
	default:
		return model.RoleUser
	}
}

// truncate shortens s to maxLen runes with an ellipsis (local copy of the
// engine-side helper; both are tiny and stable).
func truncate(s string, maxLen int) string {
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen]) + "..."
}
