//go:build poc

// 本文件负责框架钩子能力的验证：BeforeModel 改写消息、OnEvent 改写事件、可调用工具、多钩子顺序保持。
// 契约: docs/wiki/agent/agent-architecture.md#framework-boundary
package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/plugin"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// pocMockModel mockModel records the request it receives and returns a preset response.
type pocMockModel struct {
	mu          sync.Mutex
	lastRequest *model.Request
	response    *model.Response
}

func newPocMockModel(response *model.Response) *pocMockModel {
	return &pocMockModel{response: response}
}

func (m *pocMockModel) GenerateContent(
	ctx context.Context,
	request *model.Request,
) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.lastRequest = request
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	if m.response != nil {
		ch <- m.response
	}
	close(ch)
	return ch, nil
}

func (m *pocMockModel) Info() model.Info {
	return model.Info{Name: "poc-mock-model"}
}

func (m *pocMockModel) GetLastRequest() *model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastRequest
}

// pocTestPlugin is a Plugin that modifies events via OnEvent hook.
type pocTestPlugin struct {
	tagAssigned string
	stateDelta  map[string][]byte
}

func (p *pocTestPlugin) Name() string { return "poc-test" }

func (p *pocTestPlugin) Register(r *plugin.Registry) {
	r.OnEvent(func(
		ctx context.Context,
		inv *agent.Invocation,
		e *event.Event,
	) (*event.Event, error) {
		if e == nil {
			return nil, nil
		}
		e.Tag = p.tagAssigned
		if p.stateDelta != nil {
			if e.StateDelta == nil {
				e.StateDelta = make(map[string][]byte)
			}
			for k, v := range p.stateDelta {
				e.StateDelta[k] = v
			}
		}
		return e, nil
	})
}

// pocCallableTool is a simple CallableTool for PoC verification.
type pocCallableTool struct {
	mu       sync.Mutex
	called   bool
	lastArgs []byte
	result   any
	decl     *tool.Declaration
}

func newPocCallableTool(name string, result any) *pocCallableTool {
	return &pocCallableTool{
		result: result,
		decl: &tool.Declaration{
			Name:        name,
			Description: fmt.Sprintf("PoC test tool: %s", name),
			InputSchema: &tool.Schema{
				Type: "object",
				Properties: map[string]*tool.Schema{
					"input": {Type: "string", Description: "Test input"},
				},
				Required: []string{"input"},
			},
		},
	}
}

func (t *pocCallableTool) Declaration() *tool.Declaration {
	return t.decl
}

func (t *pocCallableTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.called = true
	t.lastArgs = jsonArgs
	return t.result, nil
}

func (t *pocCallableTool) WasCalled() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.called
}

func TestPoC_BeforeModel_ModifyMessages(t *testing.T) {
	mockModel := newPocMockModel(&model.Response{
		ID:   "resp-1",
		Done: true,
		Choices: []model.Choice{
			{
				Message: model.Message{
					Role:    model.RoleAssistant,
					Content: "Hello from mock",
				},
			},
		},
	})

	cb := model.NewCallbacks()
	cb.RegisterBeforeModel(func(
		ctx context.Context,
		args *model.BeforeModelArgs,
	) (*model.BeforeModelResult, error) {
		if len(args.Request.Messages) > 2 {
			args.Request.Messages = args.Request.Messages[:2]
		}
		return nil, nil
	})

	agt := llmagent.New(
		"poc-before-model",
		llmagent.WithModel(mockModel),
		llmagent.WithModelCallbacks(cb),
		llmagent.WithInstruction("You are a test assistant."),
	)

	r := runner.NewRunner("poc-app", agt)
	defer r.Close()

	ctx := context.Background()
	msg := model.NewUserMessage("Hello, this is a test message")

	eventCh, err := r.Run(ctx, "user-1", "session-1", msg)
	require.NoError(t, err)
	require.NotNil(t, eventCh)

	for range eventCh {
	}

	lastReq := mockModel.GetLastRequest()
	require.NotNil(t, lastReq, "mock model should have received a request")

	assert.LessOrEqual(t, len(lastReq.Messages), 2,
		"BeforeModel should have truncated messages to at most 2")

	t.Logf("PoC 0.1 PASSED: BeforeModel successfully modified Request.Messages (got %d messages)", len(lastReq.Messages))
}

func TestPoC_OnEvent_ModifyEvent(t *testing.T) {
	mockModel := newPocMockModel(&model.Response{
		ID:   "resp-1",
		Done: true,
		Choices: []model.Choice{
			{
				Message: model.Message{
					Role:    model.RoleAssistant,
					Content: "Test response",
				},
			},
		},
	})

	testPlugin := &pocTestPlugin{
		tagAssigned: "external_input",
		stateDelta: map[string][]byte{
			"event_key":  []byte("test_key_001"),
			"event_type": []byte("external_input"),
		},
	}

	agt := llmagent.New(
		"poc-onevent",
		llmagent.WithModel(mockModel),
		llmagent.WithInstruction("You are a test assistant."),
	)

	r := runner.NewRunner("poc-app", agt, runner.WithPlugins(testPlugin))
	defer r.Close()

	ctx := context.Background()
	msg := model.NewUserMessage("Hello, test OnEvent")

	eventCh, err := r.Run(ctx, "user-1", "session-2", msg)
	require.NoError(t, err)
	require.NotNil(t, eventCh)

	// Collect events and verify modifications
	var events []*event.Event
	for evt := range eventCh {
		events = append(events, evt)
	}

	require.NotEmpty(t, events, "should have received events from Runner")

	foundTag := false
	foundStateDelta := false
	for _, evt := range events {
		if evt.Tag == "external_input" {
			foundTag = true
		}
		if evt.StateDelta != nil {
			if string(evt.StateDelta["event_key"]) == "test_key_001" {
				foundStateDelta = true
			}
		}
	}

	assert.True(t, foundTag, "at least one event should have Tag='external_input' set by OnEvent plugin")
	assert.True(t, foundStateDelta, "at least one event should have StateDelta['event_key']='test_key_001' set by OnEvent plugin")

	t.Logf("PoC 0.2 PASSED: OnEvent plugin successfully modified Event (Tag=%v, StateDelta=%v)", foundTag, foundStateDelta)
}

func TestPoC_CallableTool(t *testing.T) {
	pocTool := newPocCallableTool("poc_tool", map[string]any{
		"result": "tool execution successful",
	})

	callCount := 0
	mockModel := &pocMultiCallModel{
		responses: []*model.Response{
			{
				ID:   "resp-tool",
				Done: true,
				Choices: []model.Choice{
					{
						Message: model.Message{
							Role:    model.RoleAssistant,
							Content: "",
							ToolCalls: []model.ToolCall{
								{
									ID:   "call-1",
									Type: "function",
									Function: model.FunctionDefinitionParam{
										Name:      "poc_tool",
										Arguments: []byte(`{"input":"test input"}`),
									},
								},
							},
						},
					},
				},
			},
			{
				ID:   "resp-final",
				Done: true,
				Choices: []model.Choice{
					{
						Message: model.Message{
							Role:    model.RoleAssistant,
							Content: "The tool returned: tool execution successful",
						},
					},
				},
			},
		},
		callCount: &callCount,
	}

	agt := llmagent.New(
		"poc-callable-tool",
		llmagent.WithModel(mockModel),
		llmagent.WithTools([]tool.Tool{pocTool}),
		llmagent.WithInstruction("You are a test assistant that uses tools."),
	)

	r := runner.NewRunner("poc-app", agt)
	defer r.Close()

	ctx := context.Background()
	msg := model.NewUserMessage("Use the poc_tool")

	eventCh, err := r.Run(ctx, "user-1", "session-3", msg)
	require.NoError(t, err)
	require.NotNil(t, eventCh)

	// Consume events
	var events []*event.Event
	for evt := range eventCh {
		events = append(events, evt)
	}

	assert.True(t, pocTool.WasCalled(), "CallableTool.Call() should have been invoked by LLMAgent")
	assert.Equal(t, `{"input":"test input"}`, string(pocTool.lastArgs),
		"CallableTool should have received the correct JSON arguments")

	t.Logf("PoC 0.3 PASSED: CallableTool was correctly called by LLMAgent (events received: %d)", len(events))
}

type pocMultiCallModel struct {
	responses []*model.Response
	callCount *int
	mu        sync.Mutex
}

func (m *pocMultiCallModel) GenerateContent(
	ctx context.Context,
	request *model.Request,
) (<-chan *model.Response, error) {
	m.mu.Lock()
	idx := *m.callCount
	*m.callCount++
	var resp *model.Response
	if idx < len(m.responses) {
		resp = m.responses[idx]
	}
	m.mu.Unlock()

	time.Sleep(10 * time.Millisecond)

	ch := make(chan *model.Response, 1)
	if resp != nil {
		ch <- resp
	}
	close(ch)
	return ch, nil
}

func (m *pocMultiCallModel) Info() model.Info {
	return model.Info{Name: "poc-multi-call-model"}
}

func TestPoC_MultipleBeforeModel_OrderPreserved(t *testing.T) {
	mockModel := newPocMockModel(&model.Response{
		ID:   "resp-order",
		Done: true,
		Choices: []model.Choice{
			{Message: model.Message{Role: model.RoleAssistant, Content: "ordered response"}},
		},
	})

	// Track execution order.
	var order []int
	var orderMu sync.Mutex

	cb := model.NewCallbacks()
	cb.RegisterBeforeModel(func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
		orderMu.Lock()
		order = append(order, 1)
		orderMu.Unlock()
		for i := range args.Request.Messages {
			args.Request.Messages[i].Content = args.Request.Messages[i].Content + "[1]"
		}
		return nil, nil
	})
	cb.RegisterBeforeModel(func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
		orderMu.Lock()
		order = append(order, 2)
		orderMu.Unlock()
		for i := range args.Request.Messages {
			args.Request.Messages[i].Content = args.Request.Messages[i].Content + "[2]"
		}
		return nil, nil
	})

	agt := llmagent.New(
		"poc-multi-before-model",
		llmagent.WithModel(mockModel),
		llmagent.WithModelCallbacks(cb),
		llmagent.WithInstruction("You are a test assistant."),
	)

	r := runner.NewRunner("poc-app", agt)
	defer r.Close()

	ctx := context.Background()
	msg := model.NewUserMessage("hello")

	eventCh, err := r.Run(ctx, "user-1", "session-order", msg)
	require.NoError(t, err)
	for range eventCh {
	}

	require.Len(t, order, 2, "both BeforeModel callbacks should have been called")
	assert.Equal(t, 1, order[0], "first callback should execute first")
	assert.Equal(t, 2, order[1], "second callback should execute second")

	lastReq := mockModel.GetLastRequest()
	require.NotNil(t, lastReq)
	userMsg := lastReq.Messages[len(lastReq.Messages)-1]
	assert.Contains(t, userMsg.Content, "[1][2]",
		"callbacks should have appended in order: [1] then [2]")

	t.Logf("PoC 0.4 PASSED: Multiple BeforeModel callbacks executed in order (user msg: %q)", userMsg.Content)
}
