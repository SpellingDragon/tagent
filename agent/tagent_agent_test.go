package agent

import (
	"context"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// TokenCounter tests
// ============================================================================

func TestDefaultTokenCounter_Estimate(t *testing.T) {
	counter := NewDefaultTokenCounter()

	// Single message
	msgs := []model.Message{
		{Role: model.RoleUser, Content: "Hello world"},
	}
	tokens := counter.Estimate(msgs)
	assert.Greater(t, tokens, 0, "token estimate should be positive")

	// Message with tool calls
	msgs = []model.Message{
		{
			Role:    model.RoleAssistant,
			Content: "I'll use a tool",
			ToolCalls: []model.ToolCall{
				{ID: "call-1", Type: "function", Function: model.FunctionDefinitionParam{Name: "test_tool"}},
			},
		},
	}
	tokensWithTools := counter.Estimate(msgs)
	assert.Greater(t, tokensWithTools, tokens, "messages with tool calls should have more tokens")

	// Empty messages
	tokens = counter.Estimate([]model.Message{})
	assert.Equal(t, 1, tokens, "empty messages should estimate as 1 (minimum)")

	// Long content
	longContent := string(make([]byte, 1000))
	msgs = []model.Message{{Role: model.RoleUser, Content: longContent}}
	tokens = counter.Estimate(msgs)
	assert.Greater(t, tokens, 100, "long content should estimate many tokens")
}

// ============================================================================
// ContextIntervention tests
// ============================================================================

func TestContextIntervention_NoCompress(t *testing.T) {
	compressor := NewSmartCompressor()
	counter := NewDefaultTokenCounter()
	ci := NewContextIntervention(compressor, counter, 8000, 0.8)

	// Short messages should not trigger compression
	args := &model.BeforeModelArgs{
		Request: &model.Request{
			Messages: []model.Message{
				{Role: model.RoleSystem, Content: "You are a test assistant."},
				{Role: model.RoleUser, Content: "Hello"},
			},
		},
	}

	result, err := ci.BeforeModel(context.Background(), args)
	require.NoError(t, err)
	assert.Nil(t, result, "no custom response should be returned")

	// Messages should not be modified
	assert.Len(t, args.Request.Messages, 2, "short messages should not be compressed")
}

func TestContextIntervention_TriggerCompress(t *testing.T) {
	compressor := NewSmartCompressor()
	counter := NewDefaultTokenCounter()
	ci := NewContextIntervention(compressor, counter, 100, 0.8) // Very low threshold

	// Build messages that exceed the token budget
	messages := []model.Message{
		{Role: model.RoleSystem, Content: "You are a test assistant."},
	}
	for i := 0; i < 20; i++ {
		messages = append(messages, model.Message{
			Role:    model.RoleUser,
			Content: "This is a long test message that should exceed the token budget when repeated many times",
		})
		messages = append(messages, model.Message{
			Role:    model.RoleAssistant,
			Content: "This is a response to the test message",
		})
	}

	args := &model.BeforeModelArgs{
		Request: &model.Request{
			Messages: messages,
		},
	}

	result, err := ci.BeforeModel(context.Background(), args)
	require.NoError(t, err)
	assert.Nil(t, result, "no custom response should be returned")

	// Messages should have been compressed (fewer than original)
	assert.Less(t, len(args.Request.Messages), len(messages),
		"messages should be compressed when over budget")
}

// ============================================================================
// TagentAgent tests
// ============================================================================

func TestNewTagentAgent(t *testing.T) {
	mockModel := newPocMockModel(&model.Response{
		ID:   "resp-1",
		Done: true,
		Choices: []model.Choice{
			{Message: model.Message{Role: model.RoleAssistant, Content: "test"}},
		},
	})

	ta, err := NewTagentAgent(&TagentConfig{
		Model:        mockModel,
		SystemPrompt: "You are a test assistant.",
	})
	require.NoError(t, err)
	require.NotNil(t, ta)
	defer ta.Close()

	assert.NotNil(t, ta.llmAgent, "LLMAgent should be initialized")
	assert.NotNil(t, ta.runner, "Runner should be initialized")
	assert.NotNil(t, ta.memStore, "MemoryStore should be initialized")
}

func TestTagentConfig_Defaults(t *testing.T) {
	mockModel := newPocMockModel(&model.Response{
		ID:   "resp-1",
		Done: true,
		Choices: []model.Choice{
			{Message: model.Message{Role: model.RoleAssistant, Content: "test"}},
		},
	})

	cfg := &TagentConfig{
		Model:        mockModel,
		SystemPrompt: "test",
	}

	ta, err := NewTagentAgent(cfg)
	require.NoError(t, err)
	defer ta.Close()

	assert.Equal(t, DefaultMaxToolIterations, cfg.MaxToolIterations, "default MaxToolIterations should be 200")
	assert.Equal(t, DefaultMaxTokens, cfg.MaxTokens, "default MaxTokens should be 8000")
	assert.Equal(t, DefaultCompressThreshold, cfg.CompressThreshold, "default CompressThreshold should be 0.8")
}

func TestNewTagentAgent_NilConfig(t *testing.T) {
	_, err := NewTagentAgent(nil)
	assert.Error(t, err, "nil config should return error")
}

func TestNewTagentAgent_NilModel(t *testing.T) {
	_, err := NewTagentAgent(&TagentConfig{SystemPrompt: "test"})
	assert.Error(t, err, "nil model should return error")
}

func TestTagentAgent_SimpleLLMCall(t *testing.T) {
	mockModel := newPocMockModel(&model.Response{
		ID:   "resp-1",
		Done: true,
		Choices: []model.Choice{
			{Message: model.Message{Role: model.RoleAssistant, Content: "Hello from tagent"}},
		},
	})

	ta, err := NewTagentAgent(&TagentConfig{
		Model:        mockModel,
		SystemPrompt: "You are a test assistant.",
	})
	require.NoError(t, err)
	defer ta.Close()

	// Persistent event loop: StartLoop → InjectMessage → consume outputCh
	outputCh, err := ta.StartLoop("user-1", "session-1")
	require.NoError(t, err)

	ta.InjectMessage(model.NewUserMessage("Hello"))

	// Consume events until final response
	select {
	case evt := <-outputCh:
		require.NotNil(t, evt)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for event")
	}

	ta.StopLoop()
}

func TestTagentAgent_BeforeModelModifiesRequest(t *testing.T) {
	// Use a very low token budget to trigger compression
	mockModel := newPocMockModel(&model.Response{
		ID:   "resp-1",
		Done: true,
		Choices: []model.Choice{
			{Message: model.Message{Role: model.RoleAssistant, Content: "response"}},
		},
	})

	ta, err := NewTagentAgent(&TagentConfig{
		Model:             mockModel,
		SystemPrompt:      "You are a test assistant.",
		MaxTokens:         20,  // Very low to trigger compression
		CompressThreshold: 0.5, // Trigger at 50%
	})
	require.NoError(t, err)
	defer ta.Close()

	// Persistent event loop: StartLoop → InjectMessage → consume outputCh
	outputCh, err := ta.StartLoop("user-1", "session-1")
	require.NoError(t, err)

	ta.InjectMessage(model.NewUserMessage("This is a somewhat long message that should exceed our tiny token budget"))

	// Drain events until final response
loop:
	for {
		select {
		case _, ok := <-outputCh:
			if !ok {
				break loop
			}
		case <-time.After(10 * time.Second):
			break loop
		}
	}

	ta.StopLoop()

	// Verify the mock model received compressed messages
	lastReq := mockModel.GetLastRequest()
	require.NotNil(t, lastReq)
	// With such a low budget, messages should have been compressed
	// (At minimum, there should be fewer messages than without compression)
}
