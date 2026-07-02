package agent

import (
	"context"
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

// mockTokenCounter is a simple token counter that always returns a fixed estimate.
type mockTokenCounter struct {
	tokens int
}

func (m *mockTokenCounter) Estimate(messages []model.Message) int { return m.tokens }

// makeTestSession creates a session pre-populated with events for tests.
// Each event is built from a model.Message.
func makeTestSession(messages []model.Message) *session.Session {
	sess := &session.Session{}
	for _, msg := range messages {
		sess.Events = append(sess.Events, event.Event{
			Response: &model.Response{
				Choices: []model.Choice{{Message: msg}},
			},
		})
	}
	return sess
}

func TestPreprocessor_ExternalInput(t *testing.T) {
	compressor := NewSmartCompressor(WithMaxTokens(8000))
	counter := &mockTokenCounter{tokens: 100}
	p := NewPreprocessor(compressor, counter, 8000, 0.8)

	sess := makeTestSession([]model.Message{
		{Role: model.RoleUser, Content: "hi"},
	})
	events := []*AgentEvent{
		NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "hi"}),
	}

	result := p.Process(context.Background(), events, sess)
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

	result := p.Process(context.Background(), events, nil)
	assert.False(t, result.ShouldCallModel)
	assert.Empty(t, result.Messages)
}

func TestPreprocessor_Mixed(t *testing.T) {
	compressor := NewSmartCompressor(WithMaxTokens(8000))
	counter := &mockTokenCounter{tokens: 100}
	p := NewPreprocessor(compressor, counter, 8000, 0.8)

	sess := makeTestSession([]model.Message{
		{Role: model.RoleSystem, Content: "tmux done"},
	})
	events := []*AgentEvent{
		NewToolUseEvent(model.ToolCall{ID: "tc1", Function: model.FunctionDefinitionParam{Name: "action"}}),
		NewExternalInputEvent("tmux", model.Message{Role: model.RoleSystem, Content: "tmux done"}),
	}

	result := p.Process(context.Background(), events, sess)
	assert.True(t, result.ShouldCallModel)
	require.Len(t, result.Messages, 1)
	assert.Equal(t, "tmux done", result.Messages[0].Content)
	assert.Equal(t, model.RoleSystem, result.Messages[0].Role)
}

func TestPreprocessor_Empty(t *testing.T) {
	compressor := NewSmartCompressor(WithMaxTokens(8000))
	counter := &mockTokenCounter{tokens: 0}
	p := NewPreprocessor(compressor, counter, 8000, 0.8)

	result := p.Process(context.Background(), nil, nil)
	assert.False(t, result.ShouldCallModel)
	assert.Empty(t, result.Messages)

	result2 := p.Process(context.Background(), []*AgentEvent{}, nil)
	assert.False(t, result2.ShouldCallModel)
}

func TestPreprocessor_BuildsFromSession(t *testing.T) {
	compressor := NewSmartCompressor(WithMaxTokens(8000))
	counter := &mockTokenCounter{tokens: 100}
	p := NewPreprocessor(compressor, counter, 8000, 0.8)

	// Session contains prior history + the new external_input event
	// (onEvent callback persists the batch to session before Process is called).
	sess := makeTestSession([]model.Message{
		{Role: model.RoleUser, Content: "msg1"},
		{Role: model.RoleAssistant, Content: "response1"},
		{Role: model.RoleUser, Content: "msg2"},
	})
	events := []*AgentEvent{
		NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "msg2"}),
	}

	result := p.Process(context.Background(), events, sess)
	assert.True(t, result.ShouldCallModel)
	require.Len(t, result.Messages, 3)
	assert.Equal(t, "msg1", result.Messages[0].Content)
	assert.Equal(t, "response1", result.Messages[1].Content)
	assert.Equal(t, "msg2", result.Messages[2].Content)
}

// TestPreprocessor_CompressTrigger verifies that compression is triggered
// when the token estimate exceeds the threshold.
func TestPreprocessor_CompressTrigger(t *testing.T) {
	compressor := NewSmartCompressor(WithMaxTokens(100))
	counter := &mockTokenCounter{tokens: 200} // > threshold (100 * 0.8 = 80)
	p := NewPreprocessor(compressor, counter, 100, 0.8)

	sess := makeTestSession([]model.Message{
		{Role: model.RoleUser, Content: "big content"},
	})
	events := []*AgentEvent{
		NewExternalInputEvent("user", model.Message{Content: "big content"}),
	}

	result := p.Process(context.Background(), events, sess)
	assert.True(t, result.ShouldCallModel)
	require.NotEmpty(t, result.Messages)
}

// TestPreprocessor_CompressOnFullHistory verifies that compression considers
// the complete session history, not just the new batch.
func TestPreprocessor_CompressOnFullHistory(t *testing.T) {
	compressor := NewSmartCompressor(WithMaxTokens(100), WithKeepRecentTasks(1))
	counter := &mockTokenCounter{tokens: 200} // > threshold
	p := NewPreprocessor(compressor, counter, 100, 0.8)

	// 5 messages in session history.
	var history []model.Message
	for i := 0; i < 5; i++ {
		history = append(history, model.Message{Role: model.RoleUser, Content: "msg"})
	}
	sess := makeTestSession(history)
	events := []*AgentEvent{
		NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "new"}),
	}

	result := p.Process(context.Background(), events, sess)
	assert.True(t, result.ShouldCallModel)
	// With keepRecentTasks=1 and high token count, compressor should produce
	// at least a context_compress summary message.
	require.NotEmpty(t, result.Messages)
}

// TestPreprocessor_EventKeyPrefixInjection verifies that event_key prefixes
// are injected from session.Events StateDelta.
func TestPreprocessor_EventKeyPrefixInjection(t *testing.T) {
	compressor := NewSmartCompressor(WithMaxTokens(8000))
	counter := &mockTokenCounter{tokens: 100}
	p := NewPreprocessor(compressor, counter, 8000, 0.8)

	sess := &session.Session{
		Events: []event.Event{
			{
				Response: &model.Response{
					Choices: []model.Choice{{Message: model.Message{Role: model.RoleUser, Content: "hello"}}},
				},
				StateDelta: map[string][]byte{
					"event_key":  []byte("12345"),
					"event_type": []byte(tagentevent.TypeExternalInput),
				},
			},
		},
	}
	events := []*AgentEvent{
		NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "hello"}),
	}

	result := p.Process(context.Background(), events, sess)
	assert.True(t, result.ShouldCallModel)
	require.Len(t, result.Messages, 1)
	assert.Contains(t, result.Messages[0].Content, "[evt_12345|external_input]")
}
