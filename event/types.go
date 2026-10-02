package event

import (
	"encoding/json"
	"fmt"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TypeExternalInput 事件类型常量。除 agent_output 与 action_command 外的一切角色都归为
// external_input；超出上下文的内容由多轮压缩处理，不做截断。
const (
	TypeExternalInput = "external_input"

	TypeAgentOutput = "agent_output"

	TypeActionCommand = "action_command"

	TypeThinkingPlan = "thinking_plan"

	TypeThinkingRecall = "thinking_recall"

	TypeThinkingKnowledge = "thinking_knowledge"

	TypeContextCompressSummary = "context_compress_summary"

	TypeContextCompress = "context_compress"

	TypeToolChain = "tool_chain"

	TypeSettleFold = "settle_fold"

	TypeTaskSpawned = "task_spawned"

	TypeResidentSession = "resident_session"

	TypeConsolidation = "consolidation"

	TypeGovernance = "governance"

	TypeFeedback = "feedback"

	TypeCognitiveAssetChanged = "cognitive_asset_changed"
)

// ExtractEventType 按消息角色判定事件类型。RoleSystem 会出现在事件流中
// （如常驻监控注入的状态通知）并归为 external_input；系统提示词本身不属于事件流。
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
		return TypeExternalInput
	default:
		return TypeExternalInput
	}
}

// IsSpecialEventType 报告该类型是否原文优先。集合由事件类型注册表定义。
func IsSpecialEventType(eventType string) bool {
	return specOrDefault(eventType).Special
}

// EventSummaryOptions 配置 event_summary 视图的呈现形态。
// 内容截断被严格禁止：超量内容交由多轮压缩处理，任何非设计的信息折损都会污染压缩质量。
type EventSummaryOptions struct {
	StructuredFormat bool
}

// DefaultOptionsForLLMContext 面向 LLM 上下文：单行以省 token，不截断。
func DefaultOptionsForLLMContext() EventSummaryOptions {
	return EventSummaryOptions{
		StructuredFormat: false,
	}
}

// DefaultOptionsForCompression 面向压缩：多行以保信息完整，不截断。
func DefaultOptionsForCompression() EventSummaryOptions {
	return EventSummaryOptions{
		StructuredFormat: true,
	}
}

// GenerateEventSummary 生成事件的 event_summary 元数据视图：多数类型是原文逐字视图，
// action_command 是一行机械工具调用行。内容级摘要属压缩与策展管线，不在此处。
func GenerateEventSummary(msg model.Message, eventType string, opts EventSummaryOptions) string {
	spec := specOrDefault(eventType)
	if eventType == TypeThinkingPlan && msg.Content == "" && len(msg.ToolCalls) > 0 {
		return formatToolNames(msg.ToolCalls)
	}
	if spec.Special {
		return msg.Content
	}
	if spec.ToolLineSummary {
		return formatToolCallSummary(msg, opts)
	}
	return msg.Content
}

// FormatEventDescription 为压缩生成保留全部信息的结构化多行描述。
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

// EstimateTokens 以约 3 字符一 token 估算文本 token 数。
func EstimateTokens(text string) int {
	return len([]rune(text)) / 3
}

// formatToolNames 返回「调用 名1、名2」形式的工具名视图（只含名字，不含参数）。
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

// formatToolCallSummary 生成工具调用行摘要。
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

// summarizeToolResult 为工具结果生成可读摘要：JSON 结果提取关键字段，非 JSON 原样返回。
func summarizeToolResult(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}

	var raw any
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return content
	}

	var summaryParts []string

	if m, ok := raw.(map[string]any); ok {
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

		if events, ok := m["events"].([]any); ok && len(events) > 0 {
			summaryParts = append(summaryParts, fmt.Sprintf("events=%d", len(events)))
		}
	}

	if len(summaryParts) > 0 {
		return strings.Join(summaryParts, "; ")
	}

	return fmt.Sprintf("[JSON object, %d chars]", len(content))
}
