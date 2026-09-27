package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// recordingModel queues responses like loopMockModel but RECORDS the message
// list of every request — the assumption pin needs to inspect what the model
// actually received on each call.
type recordingModel struct {
	mu        sync.Mutex
	responses []*model.Response
	requests  [][]model.Message
}

func (m *recordingModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	idx := len(m.requests)
	msgs := make([]model.Message, len(req.Messages))
	copy(msgs, req.Messages)
	m.requests = append(m.requests, msgs)
	var resp *model.Response
	if idx < len(m.responses) {
		resp = m.responses[idx]
	} else {
		resp = &model.Response{Done: true, Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "exhausted"}}}}
	}
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- resp
	close(ch)
	return ch, nil
}

func (m *recordingModel) Info() model.Info { return model.Info{Name: "recording-model"} }

// TestI2_BeforeModelCompleteness_RealPipeline is the ASSUMPTION PIN for the
// projection-completeness invariant (I2, invariants_test.go header): the
// upstream plugin pipeline must synchronously wait for tool-result event
// processing, so by the time the NEXT model call is assembled, every prior
// event (user input, assistant tool_call, tool result) is present in the
// request. This walks the REAL upstream pipeline (runner + llmflow + plugins,
// not a mock sequence). If a trpc-agent-go upgrade makes plugin handling
// asynchronous, this test fails HERE — before the invariant silently breaks
// in production (implementation-hardening 7A.2).
func TestI2_BeforeModelCompleteness_RealPipeline(t *testing.T) {
	bus := NewEventBus()
	outputCh := make(chan *event.Event, 10)

	mockTool := &pinEchoTool{name: "echo", result: "echo result"}
	toolCallResp := &model.Response{ID: "resp-tc", Done: true, Choices: []model.Choice{{Message: model.Message{
		Role:      model.RoleAssistant,
		ToolCalls: []model.ToolCall{{ID: "tc-pin", Function: model.FunctionDefinitionParam{Name: "echo", Arguments: []byte(`{"msg":"hi"}`)}}},
	}}}}
	finalResp := &model.Response{ID: "resp-final", Done: true, Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "done"}}}}

	mock := &recordingModel{responses: []*model.Response{toolCallResp, finalResp}}
	ta := newTestTagentAgent("pin-loop", mock, []tool.Tool{mockTool}, outputCh, bus)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	go ta.runEventLoop(ctx, bus, ta.contextManager)

	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "call echo"}))

	// Poll for the SECOND model call (the first returns a tool_call; the tool
	// result event itself also reaches outputCh without tool_calls, so a
	// naive final-response wait would race ahead of call #2).
	deadline := time.Now().Add(10 * time.Second)
	for {
		mock.mu.Lock()
		n := len(mock.requests)
		mock.mu.Unlock()
		if n >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("model calls = %d, want >= 2 (timed out)", n)
		}
		time.Sleep(10 * time.Millisecond)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	second := mock.requests[1]

	hasUserInput := false
	hasAssistantToolCall := false
	hasToolResult := false
	for _, msg := range second {
		switch {
		case msg.Role == model.RoleUser && strings.Contains(msg.Content, "call echo"):
			hasUserInput = true
		case msg.Role == model.RoleAssistant && len(msg.ToolCalls) > 0 && msg.ToolCalls[0].ID == "tc-pin":
			hasAssistantToolCall = true
		case msg.Role == model.RoleTool && strings.Contains(msg.Content, "echo result"):
			hasToolResult = true
		}
	}
	require.True(t, hasUserInput, "I2: second call must contain the original user input")
	require.True(t, hasAssistantToolCall, "I2: second call must contain the assistant tool_call turn")
	require.True(t, hasToolResult, "I2: second call must contain the TOOL RESULT — its absence means the upstream pipeline no longer waits synchronously for tool-result event processing (the pinned internal behavior changed)")
}

// pinEchoTool is the loopMockTool shape without the shared-mutex race noted
// in F-5 (single-goroutine use here).
type pinEchoTool struct {
	name   string
	result any
}

func (t *pinEchoTool) Declaration() *tool.Declaration {
	return &tool.Declaration{Name: t.name, Description: "mock"}
}

func (t *pinEchoTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	return t.result, nil
}
