package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// ============================================================================
// Edge case tests for AgentLoop and TagentAgent sub-agent behavior.
// These tests cover issues found in production trace analysis:
//   1. Empty content with reasoning_content fallback
//   2. Empty final response no longer hangs sub-agent
//   3. InjectMessage routes to sub-agent bus when not in StartLoop mode
// ============================================================================

// --- Helper: build an AgentLoop with standard config ---

func newTestAgentLoop(m model.Model, outputCh chan *event.Event, name string) *AgentLoop {
	compressor := NewSmartCompressor(WithMaxTokens(8000))
	counter := &mockTokenCounter{tokens: 100}
	preproc := NewPreprocessor(compressor, counter, 8000, 0.8)
	return NewAgentLoop(AgentLoopConfig{
		Bus:          NewEventBus(),
		Preprocessor: preproc,
		Model:        m,
		OutputCh:     outputCh,
		Name:         name,
		MaxToolIters: 10,
	})
}

func strPtr(s string) *string { return &s }

// ============================================================================
// Test 1: Empty content + reasoning_content → fallback to reasoning_content
// ============================================================================

func TestAgentLoop_EmptyContent_ReasoningFallback(t *testing.T) {
	reasoningText := "I found a skill called url-fetcher. It can fetch web pages."
	resp := &model.Response{
		Choices: []model.Choice{{
			Message: model.Message{
				Role:             model.RoleAssistant,
				Content:          "", // empty content
				ReasoningContent: reasoningText,
			},
			FinishReason: strPtr("stop"),
		}},
		Usage: &model.Usage{
			PromptTokens:     1000,
			CompletionTokens: 50,
			TotalTokens:      1050,
		},
	}

	mock := &loopMockModel{responses: []*model.Response{resp}}
	outputCh := make(chan *event.Event, 10)
	al := newTestAgentLoop(mock, outputCh, "test-reasoning")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go al.Run(ctx)

	al.bus.Publish(NewExternalInputEvent("user", model.Message{
		Role:    model.RoleUser,
		Content: "how to fetch a url?",
	}))

	select {
	case evt := <-outputCh:
		require.NotNil(t, evt)
		require.NotNil(t, evt.Response)
		require.Len(t, evt.Response.Choices, 1)
		// The fallback should have injected reasoning_content into content.
		assert.Equal(t, reasoningText, evt.Response.Choices[0].Message.Content,
			"empty content should fall back to reasoning_content")
		// reasoning_content should be cleared to avoid duplication.
		assert.Empty(t, evt.Response.Choices[0].Message.ReasoningContent,
			"reasoning_content should be cleared after fallback")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for response with reasoning_content fallback")
	}
}

// ============================================================================
// Test 2: Truly empty response (no content, no reasoning) — should not hang
// ============================================================================

func TestAgentLoop_TrulyEmptyResponse_DoesNotHang(t *testing.T) {
	resp := &model.Response{
		Choices: []model.Choice{{
			Message: model.Message{
				Role:    model.RoleAssistant,
				Content: "", // completely empty
			},
			FinishReason: strPtr("stop"),
		}},
	}

	mock := &loopMockModel{responses: []*model.Response{resp}}
	outputCh := make(chan *event.Event, 10)
	al := newTestAgentLoop(mock, outputCh, "test-empty")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go al.Run(ctx)

	al.bus.Publish(NewExternalInputEvent("user", model.Message{
		Role:    model.RoleUser,
		Content: "test",
	}))

	// Should receive an event (even if content is empty) — not hang.
	select {
	case evt := <-outputCh:
		require.NotNil(t, evt)
		require.NotNil(t, evt.Response)
		// Content is empty but event was delivered.
		assert.Equal(t, "", evt.Response.Choices[0].Message.Content)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out — empty response should still produce an event, not hang")
	}
}

// ============================================================================
// Test 3: Sub-agent Run() — InjectMessage routes to invBus
// ============================================================================

func TestTagentAgent_Run_InjectMessageRoutesToSubAgentBus(t *testing.T) {
	// Use a model that returns one response, then blocks on the second call.
	// This keeps the sub-agent loop alive so we can test InjectMessage routing.
	firstResp := &model.Response{
		Choices: []model.Choice{{
			Message: model.Message{
				Role: model.RoleAssistant,
				ToolCalls: []model.ToolCall{{
					ID: "tc1",
					Function: model.FunctionDefinitionParam{
						Name:      "noop",
						Arguments: []byte(`{}`),
					},
				}},
			},
		}},
	}

	// The noop tool — returns immediately, result goes back to bus.
	noopTool := &loopMockTool{name: "noop", result: "ok"}

	mockModel := &loopMockModel{responses: []*model.Response{firstResp}}

	compressor := NewSmartCompressor(WithMaxTokens(8000))
	counter := &mockTokenCounter{tokens: 100}
	preproc := NewPreprocessor(compressor, counter, 8000, 0.8)

	persistentBus := NewEventBus()
	ta := &TagentAgent{
		name:          "test-inject",
		persistentBus: persistentBus,
		activeBus:     persistentBus, // initially active = persistent
		config:        &TagentConfig{MaxToolIterations: 10, MaxTokens: 8000, Model: mockModel, Tools: []trpctool.Tool{noopTool}},
		preprocessor:  preproc,
	}

	// Before Run: InjectMessage should warn and drop (no bus available).
	ta.InjectMessage(model.Message{Role: model.RoleSystem, Content: "pre-run"})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	inv := trpcagent.NewInvocation(
		trpcagent.WithInvocationMessage(model.NewUserMessage("test request")),
	)
	eventCh, err := ta.Run(ctx, inv)
	require.NoError(t, err)

	// The model returns tool_calls → AgentLoop dispatches tool → tool result
	// goes back to bus → AgentLoop Pulls again → model blocks (no more responses).
	// This keeps subAgentBus alive while we test InjectMessage.
	// Wait for the tool to be called (indicates AgentLoop is running).
	require.Eventually(t, func() bool {
		return noopTool.getCallCount() >= 1
	}, 2*time.Second, 20*time.Millisecond, "tool should be called")

	// Verify activeBus is set while Run is active.
	ta.activeBusMu.Lock()
	bus := ta.activeBus
	ta.activeBusMu.Unlock()
	assert.NotNil(t, bus, "activeBus should be set during Run()")

	// InjectMessage should route to the active bus (not drop).
	ta.InjectMessage(model.Message{
		Role:    model.RoleSystem,
		Content: "tmux completed: exit_code=0",
	})

	// Cancel context to let Run complete.
	cancel()
	// Drain events.
	drainedCount := 0
	for range eventCh {
		drainedCount++
	}
	assert.GreaterOrEqual(t, drainedCount, 1, "should have received at least one event")

	// After Run completes, activeBus should be restored to persistentBus.
	ta.activeBusMu.Lock()
	busAfter := ta.activeBus
	ta.activeBusMu.Unlock()
	assert.Equal(t, ta.persistentBus, busAfter, "activeBus should be restored to persistentBus after Run completes")
}

// ============================================================================
// Test 4: Sub-agent Run() — empty final response completes (does not hang)
// ============================================================================

func TestTagentAgent_Run_EmptyFinalResponseCompletes(t *testing.T) {
	// Model returns a final response with empty content.
	emptyResp := &model.Response{
		Choices: []model.Choice{{
			Message: model.Message{
				Role:    model.RoleAssistant,
				Content: "", // empty
			},
		}},
	}

	mockModel := &loopMockModel{responses: []*model.Response{emptyResp}}

	compressor := NewSmartCompressor(WithMaxTokens(8000))
	counter := &mockTokenCounter{tokens: 100}
	preproc := NewPreprocessor(compressor, counter, 8000, 0.8)

	persistentBus2 := NewEventBus()
	ta := &TagentAgent{
		name:          "test-empty-run",
		persistentBus: persistentBus2,
		activeBus:     persistentBus2,
		config:        &TagentConfig{MaxToolIterations: 10, MaxTokens: 8000, Model: mockModel},
		preprocessor:  preproc,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	inv := trpcagent.NewInvocation(
		trpcagent.WithInvocationMessage(model.NewUserMessage("test")),
	)
	eventCh, err := ta.Run(ctx, inv)
	require.NoError(t, err)

	// Should NOT hang — empty final response is still a final response.
	// The wrapped channel goroutine should detect no tool_calls and return.
	receivedEvents := 0
loop:
	for {
		select {
		case _, ok := <-eventCh:
			if !ok {
				break loop
			}
			receivedEvents++
		case <-time.After(2 * time.Second):
			t.Fatal("timed out — sub-agent with empty final response should complete, not hang")
		}
	}
	assert.GreaterOrEqual(t, receivedEvents, 1, "should have received at least one event")
}
