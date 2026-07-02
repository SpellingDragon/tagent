package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// loopMockModel is a flexible mock model for AgentLoop tests.
// It returns a scripted sequence of responses, one per GenerateContent call.
type loopMockModel struct {
	mu        sync.Mutex
	responses []*model.Response // consumed in order; when exhausted, blocks forever
	callCount int
}

func (m *loopMockModel) GenerateContent(
	ctx context.Context, req *model.Request,
) (<-chan *model.Response, error) {
	m.mu.Lock()
	idx := m.callCount
	m.callCount++
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	go func() {
		defer close(ch)
		if idx < len(m.responses) {
			ch <- m.responses[idx]
		} else {
			// Block until ctx is cancelled when responses are exhausted.
			<-ctx.Done()
		}
	}()
	return ch, nil
}

func (m *loopMockModel) Info() model.Info {
	return model.Info{Name: "mock-loop-model"}
}

func (m *loopMockModel) getCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

// loopMockTool is a minimal mock tool for AgentLoop dispatch tests.
// Distinct from mockCallableTool (in output_limit_tool_test.go).
type loopMockTool struct {
	name   string
	result any
	err    error
	calls  [][]byte
	mu     sync.Mutex
}

func (t *loopMockTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: t.name, Description: "mock"}
}

func (t *loopMockTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	t.mu.Lock()
	t.calls = append(t.calls, jsonArgs)
	t.mu.Unlock()
	return t.result, t.err
}

func (t *loopMockTool) getCallCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.calls)
}

func TestAgentLoop_FinalResponse(t *testing.T) {
	bus := NewEventBus()
	outputCh := make(chan *event.Event, 10)
	compressor := NewSmartCompressor(WithMaxTokens(8000))
	counter := &mockTokenCounter{tokens: 100}
	preproc := NewPreprocessor(compressor, counter, 8000, 0.8)

	finalResp := &model.Response{
		Choices: []model.Choice{{
			Message: model.Message{
				Role:    model.RoleAssistant,
				Content: "final answer",
			},
		}},
	}

	mock := &loopMockModel{responses: []*model.Response{finalResp}}

	al := NewAgentLoop(AgentLoopConfig{
		Bus:          bus,
		Preprocessor: preproc,
		Model:        mock,
		OutputCh:     outputCh,
		Name:         "test-loop",
		MaxToolIters: 10,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Run loop in background.
	go al.Run(ctx)

	// Publish an external_input event.
	bus.Publish(NewExternalInputEvent("user", model.Message{
		Role:    model.RoleUser,
		Content: "hello",
	}))

	// Wait for the final response to appear on outputCh.
	select {
	case evt := <-outputCh:
		require.NotNil(t, evt)
		require.NotNil(t, evt.Response)
		require.Len(t, evt.Response.Choices, 1)
		assert.Equal(t, "final answer", evt.Response.Choices[0].Message.Content)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for agent_output on outputCh")
	}

	assert.Equal(t, 1, mock.getCallCount(), "model should be called exactly once")
}

func TestAgentLoop_ToolCallResponse(t *testing.T) {
	bus := NewEventBus()
	outputCh := make(chan *event.Event, 10)
	compressor := NewSmartCompressor(WithMaxTokens(8000))
	counter := &mockTokenCounter{tokens: 100}
	preproc := NewPreprocessor(compressor, counter, 8000, 0.8)

	mockTool := &loopMockTool{name: "echo", result: "echo result"}

	// First response: tool_calls; second response: final.
	toolCallResp := &model.Response{
		Choices: []model.Choice{{
			Message: model.Message{
				Role: model.RoleAssistant,
				ToolCalls: []model.ToolCall{{
					ID: "tc1",
					Function: model.FunctionDefinitionParam{
						Name:      "echo",
						Arguments: []byte(`{"msg":"hi"}`),
					},
				}},
			},
		}},
	}
	finalResp := &model.Response{
		Choices: []model.Choice{{
			Message: model.Message{
				Role:    model.RoleAssistant,
				Content: "done",
			},
		}},
	}

	mock := &loopMockModel{responses: []*model.Response{toolCallResp, finalResp}}

	al := NewAgentLoop(AgentLoopConfig{
		Bus:          bus,
		Preprocessor: preproc,
		Model:        mock,
		Tools:        []trpctool.Tool{mockTool},
		OutputCh:     outputCh,
		Name:         "test-loop",
		MaxToolIters: 10,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go al.Run(ctx)

	// Trigger the first turn.
	bus.Publish(NewExternalInputEvent("user", model.Message{
		Role:    model.RoleUser,
		Content: "call echo",
	}))

	// Wait for final response (may receive tool_call event first).
	var finalContent string
	deadline := time.After(3 * time.Second)
	for {
		select {
		case evt := <-outputCh:
			require.NotNil(t, evt)
			if evt.Response != nil && len(evt.Response.Choices) > 0 {
				choice := evt.Response.Choices[0]
				if len(choice.Message.ToolCalls) == 0 && choice.Message.Content != "" {
					finalContent = choice.Message.Content
					goto done
				}
			}
		case <-deadline:
			t.Fatal("timed out waiting for agent_output after tool call")
		}
	}
done:
	assert.Equal(t, "done", finalContent)

	// Model should be called twice (once for tool_call, once for final).
	assert.Equal(t, 2, mock.getCallCount())
	// Tool should be called once.
	assert.Equal(t, 1, mockTool.getCallCount())
}

func TestAgentLoop_OnlyToolUse_NoModelCall(t *testing.T) {
	bus := NewEventBus()
	outputCh := make(chan *event.Event, 10)
	compressor := NewSmartCompressor(WithMaxTokens(8000))
	counter := &mockTokenCounter{tokens: 100}
	preproc := NewPreprocessor(compressor, counter, 8000, 0.8)

	mock := &loopMockModel{responses: nil} // no responses — model should never be called

	al := NewAgentLoop(AgentLoopConfig{
		Bus:          bus,
		Preprocessor: preproc,
		Model:        mock,
		OutputCh:     outputCh,
		Name:         "test-loop",
		MaxToolIters: 10,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go al.Run(ctx)

	// Publish only a tool_use event.
	bus.Publish(NewToolUseEvent(model.ToolCall{
		ID:       "tc1",
		Function: model.FunctionDefinitionParam{Name: "unknown_tool"},
	}))

	// Wait for ctx to cancel.
	<-ctx.Done()

	// Model should NOT have been called.
	assert.Equal(t, 0, mock.getCallCount(), "model should not be called for tool_use-only batches")
}
