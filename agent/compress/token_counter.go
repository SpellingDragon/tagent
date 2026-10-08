// 契约: docs/wiki/agent/compression-and-telemetry.md
package compress

import (
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/modelutil"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TokenCounter estimates token count for message lists.
type TokenCounter interface {
	Estimate(messages []model.Message) int
}

// DefaultTokenCounter estimates tokens using a character-based heuristic.
type DefaultTokenCounter struct {
	CharsPerToken float64
}

// NewDefaultTokenCounter 构造默认计量器，字符/token 比值取中英混排的保守近似。
//
// 常数单源：2.0 / 10 / 20 取自 modelutil 的导出常数，本包不复写——
// modelutil.RequestBudget 与压缩器计数逐位同源，否则触发线与预算会各自漂移。
func NewDefaultTokenCounter() *DefaultTokenCounter {
	return &DefaultTokenCounter{CharsPerToken: modelutil.CharsPerToken}
}

// Estimate 按"字符数/比值 ＋ 每条固定开销"累加估算 token 用量。空消息集返回 0，**不计**任何
// 每条开销——否则空集合会被估出正成本，压缩判定会误以为还有内容要处理。
func (c *DefaultTokenCounter) Estimate(messages []model.Message) int {
	if len(messages) == 0 {
		return 0
	}
	total := 0
	for i := range messages {
		msg := &messages[i]
		total += int(float64(len([]rune(msg.Content))) / c.CharsPerToken)
		total += modelutil.PerMessageOverheadTokens
		if len(msg.ToolCalls) > 0 {
			total += modelutil.PerToolCallOverheadTokens * len(msg.ToolCalls)
		}
	}
	if total < 1 {
		total = 1
	}
	return total
}

// truncateString truncates s to at most n characters, appending "..." if truncated.
func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// EventTypeToRole maps an event type to its pairing-free timeline role.
//
// - external_input → user.
// - agent_output → assistant.
// - action_command → user: tool results are input events, never role=tool.
// - thinking_plan → assistant.
// - default → user as the safe degradation.
func EventTypeToRole(eventType string) model.Role {
	return tagentevent.EventTypeRole(eventType)
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
