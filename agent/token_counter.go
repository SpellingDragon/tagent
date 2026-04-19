package agent

import (
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TokenCounter estimates token count for message lists.
type TokenCounter interface {
	Estimate(messages []model.Message) int
}

// DefaultTokenCounter estimates tokens using a character-based heuristic.
// For mixed Chinese/English text, CharsPerToken defaults to 2.0
// (roughly 2 Chinese characters per token, or 4 English characters per token).
type DefaultTokenCounter struct {
	CharsPerToken float64
}

// NewDefaultTokenCounter creates a DefaultTokenCounter with default settings.
func NewDefaultTokenCounter() *DefaultTokenCounter {
	return &DefaultTokenCounter{
		CharsPerToken: 2.0, // Good for mixed Chinese/English
	}
}

// Estimate estimates the token count for a list of messages.
func (c *DefaultTokenCounter) Estimate(messages []model.Message) int {
	total := 0
	for i := range messages {
		msg := &messages[i]
		total += int(float64(len([]rune(msg.Content))) / c.CharsPerToken)
		total += 10 // overhead per message (role, formatting)
		if len(msg.ToolCalls) > 0 {
			total += 20 * len(msg.ToolCalls) // tool call overhead
		}
	}
	if total < 1 {
		total = 1
	}
	return total
}
