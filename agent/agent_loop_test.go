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

// loopMockModel returns scripted responses.
type loopMockModel struct {
	mu        sync.Mutex
	responses []*model.Response
	callCount int
}

func (m *loopMockModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
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
			<-ctx.Done()
		}
	}()
	return ch, nil
}

func (m *loopMockModel) Info() model.Info { return model.Info{Name: "mock-loop-model"} }

func (m *loopMockModel) getCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

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

// waitForFinalResponse reads outputCh until it finds a final response (no tool_calls).
func waitForFinalResponse(t *testing.T, outputCh <-chan *event.Event, timeout time.Duration) *event.Event {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case evt := <-outputCh:
			if evt == nil || evt.Response == nil || len(evt.Response.Choices) == 0 {
				continue
			}
			choice := evt.Response.Choices[len(evt.Response.Choices)-1]
			if len(choice.Message.ToolCalls) == 0 {
				return evt
			}
		case <-deadline:
			t.Fatal("timed out waiting for final response on outputCh")
			return nil
		}
	}
}

// newTestTagentAgent creates a TagentAgent with mock model for loop tests.
func newTestTagentAgent(name string, m model.Model, tools []trpctool.Tool, outputCh chan *event.Event, bus *EventBus) *TagentAgent {
	cm := newTestContextManager(name, m, tools, outputCh, bus)
	return &TagentAgent{
		name:           name,
		persistentBus:  bus,
		activeBus:      bus,
		contextManager: cm,
		outputCh:       outputCh,
	}
}

// ============================================================================
// Tests
// ============================================================================

func TestRunEventLoop_FinalResponse(t *testing.T) {
	bus := NewEventBus()
	outputCh := make(chan *event.Event, 10)

	finalResp := &model.Response{ID: "resp-1", Done: true, Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "final answer"}}}}
	mock := &loopMockModel{responses: []*model.Response{finalResp}}
	ta := newTestTagentAgent("test-loop", mock, nil, outputCh, bus)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go ta.runEventLoop(ctx, bus, ta.contextManager)

	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "hello"}))

	evt := waitForFinalResponse(t, outputCh, 5*time.Second)
	require.NotNil(t, evt)
	assert.Contains(t, evt.Response.Choices[0].Message.Content, "final answer")
}

func TestRunEventLoop_ToolCallResponse(t *testing.T) {
	bus := NewEventBus()
	outputCh := make(chan *event.Event, 10)

	mockTool := &loopMockTool{name: "echo", result: "echo result"}
	toolCallResp := &model.Response{ID: "resp-tc", Done: true, Choices: []model.Choice{{Message: model.Message{
		Role:      model.RoleAssistant,
		ToolCalls: []model.ToolCall{{ID: "tc1", Function: model.FunctionDefinitionParam{Name: "echo", Arguments: []byte(`{"msg":"hi"}`)}}},
	}}}}
	finalResp := &model.Response{ID: "resp-final", Done: true, Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "done"}}}}

	mock := &loopMockModel{responses: []*model.Response{toolCallResp, finalResp}}
	ta := newTestTagentAgent("test-loop", mock, []trpctool.Tool{mockTool}, outputCh, bus)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	go ta.runEventLoop(ctx, bus, ta.contextManager)

	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "call echo"}))

	evt := waitForFinalResponse(t, outputCh, 10*time.Second)
	require.NotNil(t, evt)
	content := evt.Response.Choices[0].Message.Content
	assert.NotEmpty(t, content)
	assert.GreaterOrEqual(t, mock.getCallCount(), 1)
	assert.Equal(t, 1, mockTool.getCallCount())
}

func TestRunEventLoop_OnlyToolUse_NoModelCall(t *testing.T) {
	bus := NewEventBus()
	outputCh := make(chan *event.Event, 10)

	mock := &loopMockModel{responses: nil}
	ta := newTestTagentAgent("test-loop", mock, nil, outputCh, bus)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go ta.runEventLoop(ctx, bus, ta.contextManager)

	bus.Publish(NewToolUseEvent(model.ToolCall{ID: "tc1", Function: model.FunctionDefinitionParam{Name: "unknown_tool"}}))
	<-ctx.Done()

	assert.Equal(t, 0, mock.getCallCount(), "model should not be called for tool_use-only batches")
}
