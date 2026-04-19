package agent

import (
	"context"
	"fmt"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// SmartCompressor performs two-stage context compression in BeforeModel.
//
// Stage 1: Drop old task segments based on task boundaries (agent_output markers)
// Stage 2: Generate LLM summary of dropped segments (if summaryModel is available)
//
// This is a "view transformation" — it modifies the messages sent to the LLM,
// but does NOT modify the Session.
type SmartCompressor struct {
	summaryModel    model.Model // Optional: used for Stage 2 LLM summary
	keepRecentTasks int         // Number of recent complete tasks to keep (default: 2)
}

// NewSmartCompressor creates a new SmartCompressor.
func NewSmartCompressor(opts ...SmartCompressorOption) *SmartCompressor {
	sc := &SmartCompressor{
		keepRecentTasks: 2,
	}
	for _, opt := range opts {
		opt(sc)
	}
	return sc
}

// SmartCompressorOption configures SmartCompressor.
type SmartCompressorOption func(*SmartCompressor)

// WithSummaryModel sets the LLM model for Stage 2 summary generation.
func WithSummaryModel(m model.Model) SmartCompressorOption {
	return func(sc *SmartCompressor) {
		sc.summaryModel = m
	}
}

// WithKeepRecentTasks sets how many recent tasks to keep.
func WithKeepRecentTasks(n int) SmartCompressorOption {
	return func(sc *SmartCompressor) {
		sc.keepRecentTasks = n
	}
}

// Compress compresses the message list by dropping old task segments.
func (sc *SmartCompressor) Compress(
	ctx context.Context, messages []model.Message,
) []model.Message {
	// 1. Separate system message
	systemMsg, rest := splitSystemMessage(messages)

	// 2. Split by task boundary
	segments := splitByTaskBoundary(rest)

	// 3. If within limit, no compression needed
	if len(segments) <= sc.keepRecentTasks {
		return messages
	}

	// 4. Split into old and recent segments
	oldSegments := segments[:len(segments)-sc.keepRecentTasks]
	recentSegments := segments[len(segments)-sc.keepRecentTasks:]

	// 5. Stage 2: Generate LLM summary of old segments (if model available)
	var summary string
	if sc.summaryModel != nil {
		summary = sc.generateSummary(ctx, oldSegments)
	}

	// 6. Reconstruct message list
	var result []model.Message
	if systemMsg != nil {
		result = append(result, *systemMsg)
	}
	if summary != "" {
		result = append(result, model.NewSystemMessage(summary))
	} else if len(oldSegments) > 0 {
		// Stage 1 only: add a brief notice about omitted messages
		result = append(result, model.NewSystemMessage(
			compressNotice(len(oldSegments)),
		))
	}
	for _, seg := range recentSegments {
		result = append(result, seg.Messages...)
	}

	return result
}

// TaskSegment is a group of messages delimited by task boundaries.
type TaskSegment struct {
	Messages   []model.Message
	IsComplete bool // true if closed by an agent_output (assistant without tool calls)
}

// splitSystemMessage separates the system message from the rest.
func splitSystemMessage(messages []model.Message) (*model.Message, []model.Message) {
	if len(messages) == 0 {
		return nil, nil
	}
	if messages[0].Role == model.RoleSystem {
		return &messages[0], messages[1:]
	}
	return nil, messages
}

// splitByTaskBoundary splits messages into segments at task boundaries.
// A task boundary is defined by an assistant message without tool calls (agent_output).
func splitByTaskBoundary(messages []model.Message) []*TaskSegment {
	if len(messages) == 0 {
		return nil
	}

	var segments []*TaskSegment
	var current *TaskSegment

	for i := range messages {
		msg := &messages[i]
		if current == nil {
			current = &TaskSegment{}
		}
		current.Messages = append(current.Messages, *msg)

		// Check for task boundary: assistant message without tool calls
		if msg.Role == model.RoleAssistant && len(msg.ToolCalls) == 0 {
			current.IsComplete = true
			segments = append(segments, current)
			current = nil
		}
	}

	// If there's an incomplete segment, add it
	if current != nil {
		segments = append(segments, current)
	}

	return segments
}

// generateSummary generates an LLM summary of old task segments.
// Stage 2 of SmartCompress: uses the summaryModel to produce a concise
// summary of the dropped segments, preserving key semantics.
func (sc *SmartCompressor) generateSummary(
	ctx context.Context, segments []*TaskSegment,
) string {
	if sc.summaryModel == nil {
		return compressNotice(len(segments))
	}

	// Build summary prompt from old segments
	var sb strings.Builder
	sb.WriteString("请对以下历史对话片段生成简洁的摘要，保留关键语义、决策和结果。不要遗漏重要信息。\n\n")
	for i, seg := range segments {
		sb.WriteString(fmt.Sprintf("--- 片段 %d ---\n", i+1))
		for _, msg := range seg.Messages {
			roleLabel := "unknown"
			switch msg.Role {
			case model.RoleSystem:
				roleLabel = "system"
			case model.RoleUser:
				roleLabel = "user"
			case model.RoleAssistant:
				roleLabel = "assistant"
			case model.RoleTool:
				roleLabel = "tool"
			}
			content := msg.Content
			if len(msg.ToolCalls) > 0 {
				var calls []string
				for _, tc := range msg.ToolCalls {
					calls = append(calls, fmt.Sprintf("%s(%s)", tc.Function.Name, string(tc.Function.Arguments)))
				}
				content = fmt.Sprintf("[tool_calls: %s]", strings.Join(calls, ", "))
				if msg.Content != "" {
					content = msg.Content + "\n" + content
				}
			}
			sb.WriteString(fmt.Sprintf("%s: %s\n", roleLabel, content))
		}
	}
	sb.WriteString("\n--- 摘要 ---")

	// Call LLM for summary
	req := &model.Request{
		Messages: []model.Message{
			model.NewSystemMessage("你是一个对话摘要助手。请为历史对话生成简洁但完整的摘要。"),
			model.NewUserMessage(sb.String()),
		},
	}

	respCh, err := sc.summaryModel.GenerateContent(ctx, req)
	if err != nil {
		log.Errorf("SmartCompress: Stage 2 LLM call failed: %v", err)
		return compressNotice(len(segments))
	}

	var summary string
	for resp := range respCh {
		if resp.Error != nil {
			log.Errorf("SmartCompress: Stage 2 LLM response error: %v", resp.Error)
			return compressNotice(len(segments))
		}
		if len(resp.Choices) > 0 {
			summary += resp.Choices[0].Message.Content
		}
	}

	if summary == "" {
		return compressNotice(len(segments))
	}

	return "[对话历史摘要] " + summary
}

// compressNotice generates a notice about omitted messages.
func compressNotice(count int) string {
	return fmt.Sprintf("[Context Summary: %d earlier task segments omitted to save tokens]", count)
}
