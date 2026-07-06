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

func strPtr(s string) *string { return &s }

// ============================================================================
// Test 1: Empty content + reasoning_content → fallback
// ============================================================================

func TestRunEventLoop_EmptyContent_ReasoningFallback(t *testing.T) {
	reasoningText := "I found a skill called url-fetcher."
	resp := &model.Response{
		ID:   "resp-1",
		Done: true,
		Choices: []model.Choice{{
			Message: model.Message{
				Role:             model.RoleAssistant,
				Content:          "",
				ReasoningContent: reasoningText,
			},
			FinishReason: strPtr("stop"),
		}},
	}

	mock := &loopMockModel{responses: []*model.Response{resp}}
	bus := NewEventBus()
	outputCh := make(chan *event.Event, 10)
	ta := newTestTagentAgent("test-reasoning", mock, nil, outputCh, bus)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go ta.runEventLoop(ctx, bus, ta.contextManager)

	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "how to fetch?"}))

	evt := waitForFinalResponse(t, outputCh, 5*time.Second)
	require.NotNil(t, evt)
	assert.NotNil(t, evt.Response)
}

// ============================================================================
// Test 2: Truly empty response — should not hang
// ============================================================================

func TestRunEventLoop_TrulyEmptyResponse_DoesNotHang(t *testing.T) {
	resp := &model.Response{
		ID:   "resp-1",
		Done: true,
		Choices: []model.Choice{{
			Message:      model.Message{Role: model.RoleAssistant, Content: ""},
			FinishReason: strPtr("stop"),
		}},
	}

	mock := &loopMockModel{responses: []*model.Response{resp}}
	bus := NewEventBus()
	outputCh := make(chan *event.Event, 10)
	ta := newTestTagentAgent("test-empty", mock, nil, outputCh, bus)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go ta.runEventLoop(ctx, bus, ta.contextManager)

	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "test"}))

	select {
	case evt := <-outputCh:
		require.NotNil(t, evt)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out — empty response should still produce an event")
	}
}

// ============================================================================
// Test 3: Sub-agent Run() — InjectMessage routes to invBus
// ============================================================================

func TestTagentAgent_Run_InjectMessageRoutesToSubAgentBus(t *testing.T) {
	firstResp := &model.Response{
		ID:   "resp-tc",
		Done: true,
		Choices: []model.Choice{{
			Message: model.Message{
				Role:      model.RoleAssistant,
				ToolCalls: []model.ToolCall{{ID: "tc1", Function: model.FunctionDefinitionParam{Name: "noop", Arguments: []byte(`{}`)}}},
			},
		}},
	}

	noopTool := &loopMockTool{name: "noop", result: "ok"}
	mockModel := &loopMockModel{responses: []*model.Response{firstResp}}

	persistentBus := NewEventBus()
	ta := &TagentAgent{
		name:          "test-inject",
		persistentBus: persistentBus,
		activeBus:     persistentBus,
		config:        &TagentConfig{MaxToolIterations: 10, MaxTokens: 8000, Model: mockModel, Tools: []trpctool.Tool{noopTool}},
	}

	ta.InjectMessage(model.Message{Role: model.RoleSystem, Content: "pre-run"})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("test request")))
	eventCh, err := ta.Run(ctx, inv)
	require.NoError(t, err)

	require.Eventually(t, func() bool { return noopTool.getCallCount() >= 1 }, 5*time.Second, 20*time.Millisecond)

	ta.InjectMessage(model.Message{Role: model.RoleSystem, Content: "tmux completed: exit_code=0"})

	cancel()
	drainedCount := 0
	for range eventCh {
		drainedCount++
	}
	assert.GreaterOrEqual(t, drainedCount, 1)

	ta.activeBusMu.Lock()
	busAfter := ta.activeBus
	ta.activeBusMu.Unlock()
	assert.Equal(t, ta.persistentBus, busAfter)
}

// ============================================================================
// Test 4: Sub-agent Run() — empty final response completes
// ============================================================================

func TestTagentAgent_Run_EmptyFinalResponseCompletes(t *testing.T) {
	emptyResp := &model.Response{
		ID:      "resp-empty",
		Done:    true,
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: ""}}},
	}

	mockModel := &loopMockModel{responses: []*model.Response{emptyResp}}

	persistentBus2 := NewEventBus()
	ta := &TagentAgent{
		name:          "test-empty-run",
		persistentBus: persistentBus2,
		activeBus:     persistentBus2,
		config:        &TagentConfig{MaxToolIterations: 10, MaxTokens: 8000, Model: mockModel},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("test")))
	eventCh, err := ta.Run(ctx, inv)
	require.NoError(t, err)

	receivedEvents := 0
loop:
	for {
		select {
		case _, ok := <-eventCh:
			if !ok {
				break loop
			}
			receivedEvents++
		case <-time.After(10 * time.Second):
			t.Fatal("timed out — sub-agent with empty final response should complete")
		}
	}
	assert.GreaterOrEqual(t, receivedEvents, 1)
}
