// Package event provides event type constants and summary utilities for tagent.
// These are the unified event definitions that extend trpc-agent-go's event system
// with trpcclaw's event classification philosophy.
package event

import (
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

	// TypeContextCompress represents context compression events.
	// Used when Agent performs context window management.
	TypeContextCompress = "context_compress"
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
	switch eventType {
	case TypeExternalInput, TypeAgentOutput, TypeThinkingPlan:
		return true
	default:
		return false
	}
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

// GenerateEventSummary generates a summary for an event.
// Special events (external_input, agent_output) use original content as summary.
// Most events use an abstract/summary format.
// IMPORTANT: No truncation - content exceeding context is handled by SmartCompress.
func GenerateEventSummary(msg model.Message, eventType string, opts EventSummaryOptions) string {
	// Special events: Summary = Original content (no truncation, no prefix)
	if IsSpecialEventType(eventType) {
		return msg.Content
	}

	// Most events: Summary = Abstract
	switch eventType {
	case TypeActionCommand:
		return formatToolCallSummary(msg, opts)
	default:
		return msg.Content
	}
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

// formatToolCallSummary generates a tool call summary.
func formatToolCallSummary(msg model.Message, opts EventSummaryOptions) string {
	if len(msg.ToolCalls) == 0 {
		if msg.Role == model.RoleTool {
			return msg.Content
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
