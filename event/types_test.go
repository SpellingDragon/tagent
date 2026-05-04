package event

import (
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

func TestExtractEventType_ThinkingPlan(t *testing.T) {
	tests := []struct {
		name     string
		msg      model.Message
		expected string
	}{
		{
			name:     "user message is external_input",
			msg:      model.Message{Role: model.RoleUser, Content: "hello"},
			expected: TypeExternalInput,
		},
		{
			name: "assistant with tool calls is thinking_plan",
			msg: model.Message{
				Role: model.RoleAssistant,
				ToolCalls: []model.ToolCall{
					{Function: model.FunctionDefinitionParam{Name: "echo", Arguments: []byte(`"hello"`)}},
				},
			},
			expected: TypeThinkingPlan,
		},
		{
			name:     "assistant without tool calls is agent_output",
			msg:      model.Message{Role: model.RoleAssistant, Content: "done"},
			expected: TypeAgentOutput,
		},
		{
			name:     "tool result is action_command",
			msg:      model.Message{Role: model.RoleTool, Content: "result"},
			expected: TypeActionCommand,
		},
		{
			name:     "system message is external_input",
			msg:      model.Message{Role: model.RoleSystem, Content: "injection"},
			expected: TypeExternalInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractEventType(tt.msg)
			if got != tt.expected {
				t.Errorf("ExtractEventType(%v) = %q, want %q", tt.msg.Role, got, tt.expected)
			}
		})
	}
}

func TestIsSpecialEventType_ThinkingPlan(t *testing.T) {
	tests := []struct {
		eventType string
		expected  bool
	}{
		{TypeExternalInput, true},
		{TypeAgentOutput, true},
		{TypeThinkingPlan, true},
		{TypeActionCommand, false},
		{TypeContextCompress, false},
		{TypeThinkingRecall, false},
		{TypeThinkingKnowledge, false},
		{"unknown_type", false},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			got := IsSpecialEventType(tt.eventType)
			if got != tt.expected {
				t.Errorf("IsSpecialEventType(%q) = %v, want %v", tt.eventType, got, tt.expected)
			}
		})
	}
}
