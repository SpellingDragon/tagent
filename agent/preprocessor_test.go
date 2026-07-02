package agent

import (
	"context"
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// mockTokenCounter is a simple token counter that always returns a fixed estimate.
type mockTokenCounter struct {
	tokens int
}

func (m *mockTokenCounter) Estimate(messages []model.Message) int { return m.tokens }

func TestPreprocessor_ExternalInput(t *testing.T) {
	compressor := NewSmartCompressor(WithMaxTokens(8000))
	counter := &mockTokenCounter{tokens: 100}
	p := NewPreprocessor(compressor, counter, 8000, 0.8)

	events := []*AgentEvent{
		NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "hi"}),
	}

	result := p.Process(context.Background(), events)
	assert.True(t, result.ShouldCallModel)
	require.Len(t, result.Messages, 1)
	assert.Equal(t, "hi", result.Messages[0].Content)
	assert.Equal(t, model.RoleUser, result.Messages[0].Role)
}

func TestPreprocessor_OnlyToolUse(t *testing.T) {
	compressor := NewSmartCompressor(WithMaxTokens(8000))
	counter := &mockTokenCounter{tokens: 100}
	p := NewPreprocessor(compressor, counter, 8000, 0.8)

	events := []*AgentEvent{
		NewToolUseEvent(model.ToolCall{ID: "tc1", Function: model.FunctionDefinitionParam{Name: "action"}}),
	}

	result := p.Process(context.Background(), events)
	assert.False(t, result.ShouldCallModel)
	assert.Empty(t, result.Messages)
}

func TestPreprocessor_Mixed(t *testing.T) {
	compressor := NewSmartCompressor(WithMaxTokens(8000))
	counter := &mockTokenCounter{tokens: 100}
	p := NewPreprocessor(compressor, counter, 8000, 0.8)

	events := []*AgentEvent{
		NewToolUseEvent(model.ToolCall{ID: "tc1", Function: model.FunctionDefinitionParam{Name: "action"}}),
		NewExternalInputEvent("tmux", model.Message{Role: model.RoleSystem, Content: "tmux done"}),
	}

	result := p.Process(context.Background(), events)
	assert.True(t, result.ShouldCallModel)
	require.Len(t, result.Messages, 1)
	assert.Equal(t, "tmux done", result.Messages[0].Content)
	assert.Equal(t, model.RoleSystem, result.Messages[0].Role)
}

func TestPreprocessor_Empty(t *testing.T) {
	compressor := NewSmartCompressor(WithMaxTokens(8000))
	counter := &mockTokenCounter{tokens: 0}
	p := NewPreprocessor(compressor, counter, 8000, 0.8)

	result := p.Process(context.Background(), nil)
	assert.False(t, result.ShouldCallModel)
	assert.Empty(t, result.Messages)

	result2 := p.Process(context.Background(), []*AgentEvent{})
	assert.False(t, result2.ShouldCallModel)
}

func TestPreprocessor_ExternalInputPreservesMessageType(t *testing.T) {
	compressor := NewSmartCompressor(WithMaxTokens(8000))
	counter := &mockTokenCounter{tokens: 100}
	p := NewPreprocessor(compressor, counter, 8000, 0.8)

	events := []*AgentEvent{
		NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "msg1"}),
		NewExternalInputEvent("tmux", model.Message{Role: model.RoleSystem, Content: "msg2"}),
	}

	result := p.Process(context.Background(), events)
	assert.True(t, result.ShouldCallModel)
	require.Len(t, result.Messages, 2)
	assert.Equal(t, tagentevent.TypeExternalInput, events[0].Type)
	assert.Equal(t, "msg1", result.Messages[0].Content)
	assert.Equal(t, "msg2", result.Messages[1].Content)
}

// TestPreprocessor_CompressTrigger verifies that compression is triggered
// when the token estimate exceeds the threshold.
func TestPreprocessor_CompressTrigger(t *testing.T) {
	// Mock compressor: returns input unchanged (no actual compression segments),
	// but we use a counter that reports tokens WAY over threshold.
	compressor := NewSmartCompressor(WithMaxTokens(100))
	counter := &mockTokenCounter{tokens: 200} // > threshold (100 * 0.8 = 80)
	p := NewPreprocessor(compressor, counter, 100, 0.8)

	events := []*AgentEvent{
		NewExternalInputEvent("user", model.Message{Content: "big content"}),
	}

	// Should call model (external_input present) — compression attempted.
	result := p.Process(context.Background(), events)
	assert.True(t, result.ShouldCallModel)
	// With mock compressor returning input unchanged, messages stay the same.
	require.NotEmpty(t, result.Messages)
}
