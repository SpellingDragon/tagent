// Package event provides event type constants and summary utilities for tagent.
// These are the unified event definitions that extend trpc-agent-go's event system
// with trpcclaw's event classification philosophy.
package event

import (
	"encoding/json"
	"fmt"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// Event type constants.
// All external inputs (user, API, system-injected) use TypeExternalInput.
// This ensures consistent treatment: any role that is NOT agent_output/action_command
// is classified as external_input. For content that exceeds context limits,
// SmartCompress handles it through multiple compression rounds.
const (
	// TypeExternalInput represents all external input events.
	// Includes user messages, API calls, and system-injected messages (RoleSystem).
	// All external inputs are treated uniformly as user role input.
	TypeExternalInput = "external_input"

	// TypeAgentOutput represents final agent output events.
	// Used for Agent's final response to user.
	TypeAgentOutput = "agent_output"

	// TypeActionCommand represents action/command execution events.
	// Used when Agent executes tools or commands.
	TypeActionCommand = "action_command"

	// TypeThinkingPlan indicates planning-related events.
	TypeThinkingPlan = "thinking_plan"

	// TypeThinkingRecall indicates memory recall events.
	TypeThinkingRecall = "thinking_recall"

	// TypeThinkingKnowledge indicates knowledge retrieval events.
	TypeThinkingKnowledge = "thinking_knowledge"

	// TypeContextCompressSummary represents curated segment-summary artifacts
	// (L3 archive output). Long-term memory: exempt from TTL and eviction.
	TypeContextCompressSummary = "context_compress_summary"

	// TypeContextCompress represents context compression events.
	// Used when Agent performs context window management.
	TypeContextCompress = "context_compress"

	// TypeToolChain represents a consolidated tool-run synthetic reference
	// (tool-chain-consolidation D2): a run of aged complete tool pairs
	// (thinking_plan + action_command) folded into one compact line. It is a
	// synthetic projection ref (negative key, like the rolling summary) that
	// renders as "- 工具链: name1→name2→…（N步）[evt_first→evt_last]" and carries
	// a recall ticket. Distinct from context_compress so buildRetainedRefs does
	// NOT absorb it into the rolling-summary count.
	TypeToolChain = "tool_chain"
)

// ExtractEventType determines the event type from a model.Message.
// This is the canonical way to classify events by role.
// Note: System prompt is NOT part of the event stream — it is injected by
// InstructionProcessor at initialization and preserved through compression.
// RoleSystem may appear in the event stream (e.g., TmuxMonitor state notifications)
// and is classified as external_input.
func ExtractEventType(msg model.Message) string {
	switch msg.Role {
	case model.RoleUser:
		return TypeExternalInput
	case model.RoleAssistant:
		if len(msg.ToolCalls) > 0 {
			return TypeThinkingPlan
		}
		return TypeAgentOutput
	case model.RoleTool:
		return TypeActionCommand
	case model.RoleSystem:
		// RoleSystem appears in event stream via TmuxMonitor injections,
		// classified as external_input. System prompt is separate (see above).
		return TypeExternalInput
	default:
		return TypeExternalInput
	}
}

// IsSpecialEventType checks if an event type should use original content as summary.
// Special events (external_input, agent_output, thinking_plan) contain the full original content.
// Most events (action_command, context_compress) only contain a summary.
func IsSpecialEventType(eventType string) bool {
	// 委托事件类型注册表（唯一权威源）：Special = external_input/agent_output/thinking_plan。
	return specOrDefault(eventType).Special
}

// EventSummaryOptions configures how to generate event summary.
// IMPORTANT: Content truncation is STRICTLY PROHIBITED. Content exceeding context
// limits must be handled through multiple SmartCompress rounds, not truncation.
// Any non-design information loss (e.g., truncation) corrupts compression quality.
type EventSummaryOptions struct {
	// StructuredFormat enables multi-line format (true) or single-line (false)
	StructuredFormat bool
}

// DefaultOptionsForLLMContext is optimized for LLM context (frequent calls).
// No truncation - EventSummary must be complete.
func DefaultOptionsForLLMContext() EventSummaryOptions {
	return EventSummaryOptions{
		StructuredFormat: false, // Single-line to save tokens
	}
}

// DefaultOptionsForCompression is optimized for SmartCompress (infrequent calls).
// No truncation - preserve all information for compression.
func DefaultOptionsForCompression() EventSummaryOptions {
	return EventSummaryOptions{
		StructuredFormat: true, // Multi-line for clarity
	}
}

// GenerateEventSummary generates the `event_summary` metadata view for an
// event. NOTE: despite the historical name, this is NOT content
// summarization — it is a verbatim-content view (original content for most
// types, a mechanical tool-call line for action_command). Content-level
// summarization lives in the compression/curation pipeline.
// IMPORTANT: No truncation - content exceeding context is handled by SmartCompress.
func GenerateEventSummary(msg model.Message, eventType string, opts EventSummaryOptions) string {
	spec := specOrDefault(eventType)
	// Pure tool-call thinking_plan (empty prose, has tool calls): summarize as
	// "调用 <names>" so aged rendering carries the tool names instead of an
	// empty-summary placeholder, and tool-chain consolidation can read the
	// names from EventSummary without refetching full content
	// (tool-chain-consolidation D1).
	if eventType == TypeThinkingPlan && msg.Content == "" && len(msg.ToolCalls) > 0 {
		return formatToolNames(msg.ToolCalls)
	}
	// Special events: Summary = Original content (no truncation, no prefix).
	if spec.Special {
		return msg.Content
	}
	// Tool-line types (action_command): mechanical tool-call summary (registry-driven).
	if spec.ToolLineSummary {
		return formatToolCallSummary(msg, opts)
	}
	// Default: original content (verbatim view).
	return msg.Content
}

// FormatEventDescription formats a complete event description for SmartCompress.
// This generates structured multi-line text preserving all information.
func FormatEventDescription(index int, msg model.Message) string {
	var desc strings.Builder
	desc.WriteString(fmt.Sprintf("[%d] %s", index, msg.Role))

	if msg.Content != "" {
		desc.WriteString(fmt.Sprintf(": %s", msg.Content))
	}

	if len(msg.ToolCalls) > 0 {
		desc.WriteString("\n  → ToolCalls:")
		for _, tc := range msg.ToolCalls {
			args := string(tc.Function.Arguments)
			desc.WriteString(fmt.Sprintf("\n    - %s(%s)", tc.Function.Name, args))
		}
	}

	return desc.String()
}

// EstimateTokens estimates the number of tokens in text.
// Simple heuristic: ~3 characters per token.
func EstimateTokens(text string) int {
	return len([]rune(text)) / 3
}

// formatToolNames summarizes a set of tool calls as "调用 name1、name2"
// (names only, no args) — the compact "what tools were called" view used for
// aged pure-tool-call thinking_plans and tool-chain consolidation.
func formatToolNames(toolCalls []model.ToolCall) string {
	names := make([]string, 0, len(toolCalls))
	for _, tc := range toolCalls {
		name := tc.Function.Name
		if name == "" {
			name = "工具"
		}
		names = append(names, name)
	}
	return "调用 " + strings.Join(names, "、")
}

// formatToolCallSummary generates a tool call summary.
func formatToolCallSummary(msg model.Message, opts EventSummaryOptions) string {
	if len(msg.ToolCalls) == 0 {
		if msg.Role == model.RoleTool {
			return summarizeToolResult(msg.Content)
		}
		return "命令执行"
	}

	toolName := msg.ToolCalls[0].Function.Name
	args := string(msg.ToolCalls[0].Function.Arguments)

	if opts.StructuredFormat {
		return fmt.Sprintf("调用工具: %s\n  参数: %s", toolName, args)
	}
	return fmt.Sprintf("调用工具: %s(%s)", toolName, args)
}

// summarizeToolResult generates a readable text summary from a tool result.
// If the content is valid JSON, it extracts key fields (type, title, status,
// session_id, count, results) to produce a concise human-readable summary.
// If the content is not JSON, it returns the original text.
//
// This prevents large JSON tool results from being stored verbatim as
// EventSummary, which causes nested JSON escaping when recall serializes
// summaries into its response.
func summarizeToolResult(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}

	// Try to parse as JSON
	var raw any
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		// Not JSON — return as-is (plain text result)
		return content
	}

	// Extract key fields for a readable summary
	var summaryParts []string

	if m, ok := raw.(map[string]any); ok {
		// Common fields in tool results
		if v, ok := m["status"].(string); ok {
			summaryParts = append(summaryParts, fmt.Sprintf("status=%s", v))
		}
		if v, ok := m["session_id"].(string); ok {
			summaryParts = append(summaryParts, fmt.Sprintf("session=%s", v))
		}
		if v, ok := m["count"].(float64); ok {
			summaryParts = append(summaryParts, fmt.Sprintf("count=%d", int(v)))
		}
		if v, ok := m["message"].(string); ok && v != "" {
			summaryParts = append(summaryParts, v)
		}
		if v, ok := m["error"].(string); ok && v != "" {
			summaryParts = append(summaryParts, fmt.Sprintf("error=%s", v))
		}

		// Extract titles from results array (knowledge/recall tools)
		if results, ok := m["results"].([]any); ok && len(results) > 0 {
			var titles []string
			for i, r := range results {
				if i >= 5 {
					titles = append(titles, fmt.Sprintf("...(%d more)", len(results)-5))
					break
				}
				if rm, ok := r.(map[string]any); ok {
					if title, ok := rm["title"].(string); ok {
						titles = append(titles, title)
					} else if typ, ok := rm["type"].(string); ok {
						titles = append(titles, typ)
					}
				}
			}
			if len(titles) > 0 {
				summaryParts = append(summaryParts, "items: "+strings.Join(titles, ", "))
			}
		}

		// Extract event count from recall results
		if events, ok := m["events"].([]any); ok && len(events) > 0 {
			summaryParts = append(summaryParts, fmt.Sprintf("events=%d", len(events)))
		}
	}

	if len(summaryParts) > 0 {
		return strings.Join(summaryParts, "; ")
	}

	// JSON but no known fields — return type info
	return fmt.Sprintf("[JSON object, %d chars]", len(content))
}
