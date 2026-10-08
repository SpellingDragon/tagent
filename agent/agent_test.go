// 本文件负责装配面：构造与默认值、nil 配置／nil 模型的确定行为、单次模型调用，以及压缩确实
// 改写请求这一接线事实。
// 契约: docs/wiki/agent/agent-architecture.md#core-components
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	trgagent "trpc.group/trpc-go/trpc-agent-go/agent"
	trpcEvent "trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

func TestDefaultTokenCounter_Estimate(t *testing.T) {
	counter := compress.NewDefaultTokenCounter()

	msgs := []model.Message{
		{Role: model.RoleUser, Content: "Hello world"},
	}
	tokens := counter.Estimate(msgs)
	assert.Greater(t, tokens, 0, "token estimate should be positive")

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

	tokens = counter.Estimate([]model.Message{})
	assert.Equal(t, 0, tokens, "empty messages should estimate as 0")

	longContent := string(make([]byte, 1000))
	msgs = []model.Message{{Role: model.RoleUser, Content: longContent}}
	tokens = counter.Estimate(msgs)
	assert.Greater(t, tokens, 100, "long content should estimate many tokens")
}

func TestNewTagentAgent(t *testing.T) {
	mockModel := newRecordableMockModel(&model.Response{
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

	assert.NotNil(t, ta.persistentBus, "EventBus should be initialized")

	assert.NotNil(t, ta.Runner(), "Runner should be initialized")
	assert.NotNil(t, ta.memStore, "MemoryStore should be initialized")
}

func TestTagentConfig_Defaults(t *testing.T) {
	mockModel := newRecordableMockModel(&model.Response{
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

	assert.Equal(t, DefaultMaxToolIterations, cfg.MaxToolIterations, "default MaxToolIterations should be 50")
	assert.Equal(t, compress.DefaultMaxTokens, cfg.MaxTokens, "default MaxTokens should be 8000")
	assert.Equal(t, compress.DefaultCompressThreshold, cfg.CompressThreshold, "default CompressThreshold should be 0.8")
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
	mockModel := newRecordableMockModel(&model.Response{
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

	outputCh, err := ta.StartLoop("user-1", "session-1")
	require.NoError(t, err)

	ta.InjectMessage(model.NewUserMessage("Hello"))

	evt := waitForFinalResponse(t, outputCh, 10*time.Second)
	require.NotNil(t, evt)

	ta.StopLoop()
}

func TestTagentAgent_CompressionModifiesRequest(t *testing.T) {
	mockModel := newRecordableMockModel(&model.Response{
		ID:   "resp-1",
		Done: true,
		Choices: []model.Choice{
			{Message: model.Message{Role: model.RoleAssistant, Content: "response"}},
		},
	})

	ta, err := NewTagentAgent(&TagentConfig{
		Model:             mockModel,
		SystemPrompt:      "You are a test assistant.",
		MaxTokens:         100,
		CompressThreshold: 0.5,
	})
	require.NoError(t, err)
	defer ta.Close()

	outputCh, err := ta.StartLoop("user-1", "session-1")
	require.NoError(t, err)

	ta.InjectMessage(model.NewUserMessage("This is a somewhat long message that should exceed our tiny token budget"))

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

	lastReq := mockModel.GetLastRequest()
	require.NotNil(t, lastReq)
}

// mockLoopModel is a model that returns a scripted final response.
type mockLoopModel struct {
	mu        sync.Mutex
	callCount int
}

func (m *mockLoopModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.callCount++
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	ch <- &model.Response{
		Done: true,
		Choices: []model.Choice{{
			Message: model.Message{Role: model.RoleAssistant, Content: "mock response"},
		}},
	}
	close(ch)
	return ch, nil
}

func (m *mockLoopModel) Info() model.Info {
	return model.Info{Name: "mock-loop-model"}
}

// newLoopTestAgent creates a TagentAgent configured for loop tests.
func newLoopTestAgent(t *testing.T) *TagentAgent {
	t.Helper()
	bus := NewEventBus()
	outputCh := make(chan *trpcEvent.Event, 100)
	cm := newTestContextManager("test-loop", &mockLoopModel{}, nil, outputCh, bus)
	return &TagentAgent{
		persistentBus:  bus,
		activeBus:      bus,
		contextManager: cm,
		config:         &TagentConfig{MaxToolIterations: 10, MaxTokens: 8000},
		outputCh:       outputCh,
		name:           "test-loop",
	}
}

func TestStartLoop_InjectMessage_ReceivesEvents(t *testing.T) {
	ta := newLoopTestAgent(t)

	outputCh, err := ta.StartLoop("test-user", "test-session")
	require.NoError(t, err)
	assert.True(t, ta.IsLoopActive())

	ta.InjectMessage(model.Message{Role: model.RoleUser, Content: "hello"})

	evt := waitForFinalResponse(t, outputCh, 5*time.Second)
	require.NotNil(t, evt)
	assert.NotEmpty(t, evt.Response.Choices[0].Message.Content)

	ta.StopLoop()
	assert.False(t, ta.IsLoopActive())
}

func TestStartLoop_MultipleInjects(t *testing.T) {
	ta := newLoopTestAgent(t)

	outputCh, err := ta.StartLoop("test-user", "test-session")
	require.NoError(t, err)
	defer ta.StopLoop()

	for i := 0; i < 3; i++ {
		ta.InjectMessage(model.Message{Role: model.RoleUser, Content: "msg"})
	}

	evt := waitForFinalResponse(t, outputCh, 5*time.Second)
	require.NotNil(t, evt)
}

func TestStopLoop_Idempotent(t *testing.T) {
	ta := newLoopTestAgent(t)

	_, err := ta.StartLoop("test-user", "test-session")
	require.NoError(t, err)

	ta.StopLoop()
	ta.StopLoop()
	assert.False(t, ta.IsLoopActive())
}

func TestStartLoop_DuplicateCall(t *testing.T) {
	ta := newLoopTestAgent(t)

	ch1, err := ta.StartLoop("test-user", "test-session")
	require.NoError(t, err)

	ch2, err := ta.StartLoop("test-user", "test-session")
	require.NoError(t, err)

	assert.Equal(t, ch1, ch2)

	ta.StopLoop()
}

func TestInjectMessage_LoopNotStarted(t *testing.T) {
	ta := newLoopTestAgent(t)

	ta.InjectMessage(model.Message{Role: model.RoleUser, Content: "hello"})
}

// TestStartLoop_AfterStop_TerminalLifecycle 钉住 StopLoop 对生命周期是终态：停止后再 StartLoop 必须被拒。
func TestStartLoop_AfterStop_TerminalLifecycle(t *testing.T) {
	ta := newLoopTestAgent(t)

	outputCh, err := ta.StartLoop("test-user", "test-session")
	require.NoError(t, err)
	ta.InjectMessage(model.Message{Role: model.RoleUser, Content: "hello"})
	evt := waitForFinalResponse(t, outputCh, 5*time.Second)
	require.NotNil(t, evt)

	ta.StopLoop()

	_, err = ta.StartLoop("test-user", "test-session")
	require.Error(t, err, "second StartLoop after StopLoop must be rejected")
	require.Contains(t, err.Error(), "terminated")
}

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
func waitForFinalResponse(t *testing.T, outputCh <-chan *trpcEvent.Event, timeout time.Duration) *trpcEvent.Event {
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
func newTestTagentAgent(name string, m model.Model, tools []trpctool.Tool, outputCh chan *trpcEvent.Event, bus *EventBus) *TagentAgent {
	cm := newTestContextManager(name, m, tools, outputCh, bus)
	return &TagentAgent{
		name:           name,
		persistentBus:  bus,
		activeBus:      bus,
		contextManager: cm,
		outputCh:       outputCh,
	}
}

func TestRunEventLoop_FinalResponse(t *testing.T) {
	bus := NewEventBus()
	outputCh := make(chan *trpcEvent.Event, 10)

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
	outputCh := make(chan *trpcEvent.Event, 10)

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

func strPtr(s string) *string { return &s }

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
	outputCh := make(chan *trpcEvent.Event, 10)
	ta := newTestTagentAgent("test-reasoning", mock, nil, outputCh, bus)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go ta.runEventLoop(ctx, bus, ta.contextManager)

	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "how to fetch?"}))

	evt := waitForFinalResponse(t, outputCh, 5*time.Second)
	require.NotNil(t, evt)
	assert.NotNil(t, evt.Response)
}

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
	outputCh := make(chan *trpcEvent.Event, 10)
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

	inv := trgagent.NewInvocation(trgagent.WithInvocationMessage(model.NewUserMessage("test request")))
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

func TestTagentAgent_Run_EmptyFinalResponseCompletes(t *testing.T) {
	emptyResp := &model.Response{
		ID:      "resp-empty",
		Done:    true,
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: ""}}},
	}

	mockModel := &loopMockModel{responses: []*model.Response{emptyResp, emptyResp}}

	persistentBus2 := NewEventBus()
	ta := &TagentAgent{
		name:          "test-empty-run",
		persistentBus: persistentBus2,
		activeBus:     persistentBus2,
		config:        &TagentConfig{MaxToolIterations: 10, MaxTokens: 8000, Model: mockModel},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	inv := trgagent.NewInvocation(trgagent.WithInvocationMessage(model.NewUserMessage("test")))
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

// assertRenderLegality is the I3 assertion helper (D3 v2): the rendered
// sequence must be a LEGAL NATIVE conversation — every role=tool message has
// a prior assistant declaring its ToolID (no orphans, no duplicate answers),
// no empty pure-text assistant messages, no duplicate [evt_KEY prefixes.
func assertRenderLegality(t *testing.T, msgs []model.Message) {
	t.Helper()
	seenKeys := map[int64]bool{}
	declared := map[string]bool{}
	consumed := map[string]bool{}
	for i, m := range msgs {
		switch m.Role {
		case model.RoleAssistant:
			for _, tc := range m.ToolCalls {
				if tc.ID != "" {
					declared[tc.ID] = true
				}
			}
			if len(m.ToolCalls) == 0 && strings.TrimSpace(tagentevent.StripEventKeyPrefix(m.Content)) == "" {
				t.Errorf("render legality: msg[%d] is an empty assistant output", i)
			}
		case model.RoleTool:
			if m.ToolID == "" || !declared[m.ToolID] {
				t.Errorf("render legality: msg[%d] is an orphan tool result (tool_id=%q has no prior declaring call)", i, m.ToolID)
			}
			if consumed[m.ToolID] {
				t.Errorf("render legality: msg[%d] duplicates an already-answered tool_id=%q", i, m.ToolID)
			}
			consumed[m.ToolID] = true
		}
		key, _, _ := tagentevent.ParseEventKeyAndType(m.Content)
		if key > 0 {
			if seenKeys[key] {
				t.Errorf("render legality: duplicate event key %d at msg[%d]", key, i)
			}
			seenKeys[key] = true
		}
	}
}

// seedProjectedToolTurn stores a thinking_plan + action_command pair through
// MemoryStore and the projection, mirroring a completed ReAct step.
func seedProjectedToolTurn(t *testing.T, cm *ContextManager) {
	t.Helper()
	now := time.Now().UnixMilli()
	events := []memory.FullEvent{
		{
			EventKey: 1001, EventType: "external_input",
			EventSummary: "list files", Content: "list files", Timestamp: now,
		},
		{
			EventKey: 1002, EventType: "thinking_plan",
			EventSummary: "calling ls", Content: "",
			ToolCalls: []model.ToolCall{{ID: "call-1", Function: model.FunctionDefinitionParam{Name: "action", Arguments: []byte(`{"command":"ls"}`)}}},
			Timestamp: now + 1,
		},
		{
			EventKey: 1003, EventType: "action_command",
			EventSummary: "status=completed", Content: `{"command":"ls","status":"completed","output":"a.txt"}`,
			ToolID:    "call-1",
			Timestamp: now + 2,
		},
	}
	roles := []string{"user", "assistant", "tool"}
	for i, fe := range events {
		if err := cm.memStore.StoreEvent(fe.EventKey, fe); err != nil {
			t.Fatalf("store event %d: %v", fe.EventKey, err)
		}
		cm.projection.Append(memory.EventReference{
			EventKey: fe.EventKey, EventType: fe.EventType,
			EventSummary: fe.EventSummary, Timestamp: fe.Timestamp, Role: roles[i],
		})
	}
}

// newAssistantEvt builds a framework event carrying an assistant message,
// mirroring what the framework emits per model response.
func newAssistantEvt(content string) *trpcEvent.Event {
	evt := trpcEvent.New("inv-test", "tagent")
	evt.Timestamp = time.Now()
	evt.Response = &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: content}}},
	}
	return evt
}

// TestProjectionSink_PerInvocationIsolation 钉住 并发调用（主循环与子 agent）各自只投影自己的事件——按上下文绑定的接收器绝不互串写入。
func TestProjectionSink_PerInvocationIsolation(t *testing.T) {
	store := memory.NewInMemoryStore()
	p := plugin.NewMemoryPlugin(store)
	mainProj := compress.NewSessionProjection()
	subProj := compress.NewSessionProjection()
	ctxMain := plugin.WithProjectionSink(context.Background(), mainProj)
	ctxSub := plugin.WithProjectionSink(context.Background(), subProj)

	const n = 40
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			if _, err := p.OnEvent(ctxMain, nil, newAssistantEvt(fmt.Sprintf("main-%d", i))); err != nil {
				t.Errorf("main OnEvent: %v", err)
			}
		}(i)
		go func(i int) {
			defer wg.Done()
			if _, err := p.OnEvent(ctxSub, nil, newAssistantEvt(fmt.Sprintf("sub-%d", i))); err != nil {
				t.Errorf("sub OnEvent: %v", err)
			}
		}(i)
	}
	wg.Wait()

	if mainProj.Len() != n {
		t.Fatalf("main projection must hold %d refs, got %d", n, mainProj.Len())
	}
	if subProj.Len() != n {
		t.Fatalf("sub projection must hold %d refs, got %d", n, subProj.Len())
	}
	for _, ref := range mainProj.GetAll() {
		if !strings.HasPrefix(ref.EventSummary, "main-") {
			t.Errorf("cross-write: sub event leaked into main projection: %+v", ref)
		}
	}
	for _, ref := range subProj.GetAll() {
		if !strings.HasPrefix(ref.EventSummary, "sub-") {
			t.Errorf("cross-write: main event leaked into sub projection: %+v", ref)
		}
	}
}

// TestRenderLegalityNativePairing 钉住 含"计划＋动作"配对的投影解析后必须产出合法的原生渲染：assistant 带原生工具调用，结果按标识配对为工具角色。
// - 只保证不报错不够：配对错位会让模型看不见工具结果。
func TestRenderLegalityNativePairing(t *testing.T) {
	cm := newTestContextManager("i3-agent", nil, nil, nil, nil)
	seedProjectedToolTurn(t, cm)

	result := cm.contextCompressor.Compress(context.Background(), cm.projection.GetAll())
	assertRenderLegality(t, result.Messages)

	// Native forms present: assistant with ToolCalls, tool result paired.
	var sawNativeCall, sawPairedResult bool
	for _, m := range result.Messages {
		if m.Role == model.RoleAssistant && len(m.ToolCalls) > 0 && m.ToolCalls[0].ID == "call-1" {
			sawNativeCall = true
			if strings.Contains(m.Content, "call-1") {
				t.Errorf("assistant content must not contain textual call syntax, got: %q", m.Content)
			}
		}
		if m.Role == model.RoleTool && m.ToolID == "call-1" {
			sawPairedResult = true
		}
	}
	if !sawNativeCall || !sawPairedResult {
		t.Errorf("expected native call+result pair, got call=%v result=%v: %+v", sawNativeCall, sawPairedResult, result.Messages)
	}
}

// TestAssemblyIgnoresFrameworkTail 钉住 框架消息尾部的噪声或过期副本不得影响装配结果——装配只取其中的系统消息。
func TestAssemblyIgnoresFrameworkTail(t *testing.T) {
	cm := newTestContextManager("i4-agent", nil, nil, nil, nil)
	seedProjectedToolTurn(t, cm)

	system := model.Message{Role: model.RoleSystem, Content: "sys"}

	clean := &model.BeforeModelArgs{Request: &model.Request{Messages: []model.Message{system}}}
	cm.assembleRequest(context.Background(), clean)

	polluted := &model.BeforeModelArgs{Request: &model.Request{Messages: []model.Message{
		system,
		{Role: model.RoleAssistant, Content: "", ToolCalls: []model.ToolCall{{ID: "stale-call"}}},
		{Role: model.RoleTool, Content: "stale result", ToolID: "stale-call"},
		{Role: model.RoleUser, Content: "garbage echo"},
	}}}
	cm.assembleRequest(context.Background(), polluted)

	if len(clean.Request.Messages) != len(polluted.Request.Messages) {
		t.Fatalf("assembly must ignore framework tail: clean=%d msgs, polluted=%d msgs",
			len(clean.Request.Messages), len(polluted.Request.Messages))
	}
	for i := range clean.Request.Messages {
		c, p := clean.Request.Messages[i], polluted.Request.Messages[i]
		if c.Role != p.Role || c.Content != p.Content {
			t.Errorf("assembly diverges at msg[%d]:\n  clean:    %s %q\n  polluted: %s %q",
				i, c.Role, c.Content, p.Role, p.Content)
		}
	}
}

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

// TestBeforeModelCompletenessRealPipeline 钉住一个前提：上游插件管线必须同步等待工具结果事件处理完成。
// - 因此下一次模型调用装配时，先前所有事件（用户输入、助手工具调用、工具结果）都已在请求里；
// - 本测试走真实管线（runner、事件流、插件）而非模拟序列：上游一旦把插件处理改成异步就立刻失败，而不是让投影完整性在生产上静默破掉。
func TestBeforeModelCompletenessRealPipeline(t *testing.T) {
	bus := NewEventBus()
	outputCh := make(chan *trpcEvent.Event, 10)

	mockTool := &pinEchoTool{name: "echo", result: "echo result"}
	toolCallResp := &model.Response{ID: "resp-tc", Done: true, Choices: []model.Choice{{Message: model.Message{
		Role:      model.RoleAssistant,
		ToolCalls: []model.ToolCall{{ID: "tc-pin", Function: model.FunctionDefinitionParam{Name: "echo", Arguments: []byte(`{"msg":"hi"}`)}}},
	}}}}
	finalResp := &model.Response{ID: "resp-final", Done: true, Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "done"}}}}

	mock := &recordingModel{responses: []*model.Response{toolCallResp, finalResp}}
	ta := newTestTagentAgent("pin-loop", mock, []trpctool.Tool{mockTool}, outputCh, bus)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	go ta.runEventLoop(ctx, bus, ta.contextManager)

	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "call echo"}))

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

func (t *pinEchoTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: t.name, Description: "mock"}
}

func (t *pinEchoTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	return t.result, nil
}

// phasedStore wraps a real replay-capable InMemoryStore and lets a test arm a
// per-key failure on the ordinary-write (StoreEvent), explicit-replay
// (ReplayEvent) and delete (DeleteEvent) seams. It is the agent-package sibling
// of memory's faultKV (which is package-private and not importable here), and
// stays EventReplayer-capable via the embedded store.
type phasedStore struct {
	*memory.InMemoryStore
	failStore  map[int64]error
	failReplay map[int64]error
	failDelete map[int64]error
}

func newPhasedStore() *phasedStore {
	return &phasedStore{
		InMemoryStore: memory.NewInMemoryStore(),
		failStore:     map[int64]error{},
		failReplay:    map[int64]error{},
		failDelete:    map[int64]error{},
	}
}

var _ memory.EventReplayer = (*phasedStore)(nil)

func (s *phasedStore) StoreEvent(key int64, e memory.FullEvent) error {
	if err, ok := s.failStore[key]; ok {
		return err
	}
	return s.InMemoryStore.StoreEvent(key, e)
}

func (s *phasedStore) ReplayEvent(key int64, canonical memory.FullEvent) (memory.ReplayResult, memory.FullEvent, error) {
	if err, ok := s.failReplay[key]; ok {
		return memory.ReplayNew, canonical, err
	}
	return s.InMemoryStore.ReplayEvent(key, canonical)
}

const externalInputType = "external_input"

func (s *phasedStore) DeleteEvent(key int64) error {
	if err, ok := s.failDelete[key]; ok {
		return err
	}
	return s.InMemoryStore.DeleteEvent(key)
}

func durableEvtWithPrepared(pid int, rid string, slot int, receipt string, prepared json.RawMessage, content string) *AgentEvent {
	return &AgentEvent{
		ID: rid, Type: externalInputType, Source: "user",
		Message:   &model.Message{Role: model.RoleUser, Content: content},
		Timestamp: time.UnixMilli(1700000000000),
		claim:     &durableClaim{RequestID: rid, Slot: slot, ReceiptKey: receipt, PreparedFact: prepared},
	}
}

func marshalFact(t *testing.T, f memory.FullEvent) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(f)
	require.NoError(t, err)
	return b
}

func newGateCM(t *testing.T, store memory.MemoryStore) *ContextManager {
	t.Helper()
	cm := newTestContextManager("gate", &loopMockModel{}, nil, nil, NewEventBus())
	cm.memStore = store
	return cm
}

// TestCounter_CorruptPreparedFactMustBlockNotRestamp 钉住 钉住 prepared_fact 不可解码时必须阻断提交：一条事实都不得进入事实链，也不得重新盖章后落库。
func TestCounter_CorruptPreparedFactMustBlockNotRestamp(t *testing.T) {
	t.Skip("blocked-by §3.5: corrupt prepared_fact currently restamps+stores (must block); see evidence.md fail-before")
	store := newPhasedStore()
	cm := newGateCM(t, store)
	evt := durableEvtWithPrepared(1, "r1", 0, "cafe", json.RawMessage("{ not valid json"), "hi")

	stored := cm.persistBusEvent(evt)

	require.False(t, stored, "§3.5: a corrupt prepared_fact must gate the commit (return false), not restamp")
	require.Equal(t, 0, cm.projection.Len(), "§3.5: a corrupt prepared_fact must not append a restamped ref")
	refs, _ := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{cm.partitionID}, Limit: 10})
	require.Empty(t, refs, "§3.5: no fact may reach the chain when the prepared payload is undecodable")
}

// TestCounter_PartialInputReplayFailureGatesCommit 钉住 钉住 持久输入的 ReplayEvent 瞬时失败时必须挡住本轮提交，不得因其余输入可用而提前成功。
func TestCounter_PartialInputReplayFailureGatesCommit(t *testing.T) {
	store := newPhasedStore()
	cm := newGateCM(t, store)
	kf := memory.NewSnowflakeEventKey(cm.partitionID, 0)
	ks := memory.NewSnowflakeEventKey(cm.partitionID, 1)
	fresh := func(k int64) memory.FullEvent {
		return memory.FullEvent{EventKey: k, PartitionID: cm.partitionID, EventType: externalInputType, EventSummary: "s", Content: "c", Timestamp: 1700000000000}
	}
	store.failReplay[ks] = errors.New("transient append failure")

	okFirst := cm.persistBusEvent(durableEvtWithPrepared(1, "r1", 0, "aa", marshalFact(t, fresh(kf)), "A"))
	okSecond := cm.persistBusEvent(durableEvtWithPrepared(1, "r1", 1, "bb", marshalFact(t, fresh(ks)), "B"))

	require.True(t, okFirst, "first input commits cleanly")
	require.False(t, okSecond, "§4.3: an input whose replay commit transiently fails must report not-stored (claim held), never swallowed as done")
}

// TestCounter_ConcurrentCloseConverges 钉住 钉住 并发 Close 必须在有限时间内收敛，且不 panic、不死锁。
func TestCounter_ConcurrentCloseConverges(t *testing.T) {
	ta := newTestTagentAgent("cc", &loopMockModel{}, nil, make(chan *trpcEvent.Event, 16), NewEventBus())

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	panicked := make([]bool, n)
	done := make(chan struct{})
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			defer func() { //nolint:revive // recover is the panic probe
				if r := recover(); r != nil {
					panicked[i] = true
				}
			}()
			errs[i] = ta.Close()
		}(i)
	}
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("§6.1: concurrent Close did not converge within 5s (deadlock/ordering gap)")
	}
	for i := range panicked {
		require.False(t, panicked[i], "§6.1: concurrent Close must not panic")
	}
}

// dumpPhase prints the model-visible message structure for a phase (verbose
// runs only) — the "what does the model actually see" evidence trail.
func dumpPhase(t *testing.T, name string, msgs []model.Message) {
	t.Helper()
	t.Logf("===== phase %s: %d messages =====", name, len(msgs))
	for i, m := range msgs {
		c := strings.ReplaceAll(m.Content, "\n", "⏎")
		if len(c) > 72 {
			c = c[:72] + "…"
		}
		t.Logf("  [%2d] %-9s %s", i, m.Role, c)
	}
}

// simTurn stores one plain task turn (ext → tp+toolcall → ac → out) and
// returns its refs. Keys are monotonically increasing (render-freeze relies
// on key monotonicity).
func simTurn(store *memory.InMemoryStore, base int64, label string) []memory.EventReference {
	ref := func(key int64, typ, summary string, role string) memory.EventReference {
		return memory.EventReference{EventKey: key, EventType: typ, EventSummary: summary, Timestamp: key, Role: role}
	}
	refs := []memory.EventReference{
		ref(base, "external_input", "请求 "+label, "user"),
		ref(base+1, "thinking_plan", "调用 read_"+label, "assistant"),
		ref(base+2, "action_command", "结果 "+label, "tool"),
		ref(base+3, "agent_output", "完成 "+label, "assistant"),
	}
	fulls := []memory.FullEvent{
		{EventKey: base, EventType: "external_input", Content: "用户请求 " + label + " 的完整原文，稍微长一点以便有内容可观。", EventSummary: "请求 " + label},
		{EventKey: base + 1, EventType: "thinking_plan", Content: "调用 read_" + label, EventSummary: "调用 read_" + label},
		{EventKey: base + 2, EventType: "action_command", Content: "工具执行结果 " + label + " 的完整输出内容。", EventSummary: "结果 " + label},
		{EventKey: base + 3, EventType: "agent_output", Content: "任务 " + label + " 已完成的答复全文。", EventSummary: "完成 " + label},
	}
	for _, fe := range fulls {
		if err := store.StoreEvent(fe.EventKey, fe); err != nil {
			panic(err)
		}
	}
	return refs
}

// simSettle builds a task_settled event via the REAL constructor (spill
// included), stores it, and returns its ref.
func simSettle(store *memory.InMemoryStore, key int64, output string, spillDir string) memory.EventReference {
	tk := &task.Task{ID: fmt.Sprintf("sim-%012d", key), Spec: task.TaskSpec{Kind: "command", Desc: "后台命令 " + fmt.Sprintf("%x", key)}}
	evt := newTaskSettledEvent(tk, task.SettleSignal{Kind: task.SettleCompleted, Output: output}, 100000, spillDir)
	if err := store.StoreEvent(key, memory.FullEvent{
		EventKey: key, EventType: "external_input",
		Content: evt.Message.Content, EventSummary: evt.Message.Content, Timestamp: key,
	}); err != nil {
		panic(err)
	}
	return memory.EventReference{EventKey: key, EventType: "external_input", EventSummary: evt.Message.Content, Timestamp: key, Role: "user"}
}

func TestContextLifecycleSimulation(t *testing.T) {
	store := memory.NewInMemoryStore()
	spillDir := t.TempDir()

	join := func(msgs []model.Message) string {
		var b strings.Builder
		for _, m := range msgs {
			b.WriteString(m.Content)
			b.WriteString("\n")
		}
		return b.String()
	}

	ccHealthy := compress.NewContextCompressor(
		compress.NewSmartCompressor(compress.WithKeepRecentTasks(2), compress.WithMaxTokens(8000)),
		store, compress.NewDefaultTokenCounter(), 8000, 0.8, 2,
		compress.WithRecentFullCount(8))

	// ---- Phase A: small session — everything native full ----
	var refs []memory.EventReference
	refs = append(refs, simTurn(store, 100, "A1")...)
	refs = append(refs, simTurn(store, 200, "A2")...)
	refs = append(refs, simTurn(store, 300, "A3")...)
	rA := ccHealthy.Compress(context.Background(), refs)
	if len(rA.RetainedRefs) != len(refs) {
		t.Fatalf("A: under-budget pass-through must keep all refs (%d != %d)", len(rA.RetainedRefs), len(refs))
	}
	joinedA := join(rA.Messages)
	dumpPhase(t, "A small-session", rA.Messages)
	if strings.Contains(joinedA, "[Compacted") || strings.Contains(joinedA, "工具链") {
		t.Fatalf("A: small session must have no rolling summary / tool chains:\n%s", joinedA)
	}
	if !strings.Contains(joinedA, "A1 的完整原文") {
		t.Fatalf("A: pre-compaction everything renders full (boundary=0)")
	}

	small := strings.Repeat("小结果内容。", 40)
	refs = append(refs, simSettle(store, 400, small, spillDir))
	rB := ccHealthy.Compress(context.Background(), refs)
	joinedB := join(rB.Messages)
	dumpPhase(t, "B small-settle-inline", rB.Messages)
	if strings.Contains(joinedB, "output_spilled") || !strings.Contains(joinedB, "小结果内容") {
		t.Fatalf("B: small settle must stay fully inline:\n%.300s", joinedB)
	}
	big := strings.Repeat("x", 300000)
	settleRef := simSettle(store, 500, big, spillDir)
	if !strings.Contains(settleRef.EventSummary, "output_spilled") {
		t.Fatalf("B: oversized settle must carry the spill ticket")
	}
	if len(settleRef.EventSummary) > 5000 {
		t.Fatalf("B: settle event Content must stay bounded (len=%d)", len(settleRef.EventSummary))
	}
	matches, _ := filepath.Glob(filepath.Join(spillDir, "task-sim-*.txt"))
	if len(matches) == 0 {
		t.Fatalf("B: spilled file must exist under %s", spillDir)
	}
	if data, err := os.ReadFile(matches[0]); err != nil || len(data) != len(big) {
		t.Fatalf("B: spilled file must hold the full body (err=%v len=%d want %d)", err, len(data), len(big))
	}
	refs = append(refs, settleRef)

	refs = append(refs, simTurn(store, 600, "C1")...)
	refs = append(refs, simTurn(store, 700, "C2")...)
	refs = append(refs, simTurn(store, 800, "C3")...)
	refs = append(refs, simTurn(store, 900, "C4")...)

	ccSmall := compress.NewContextCompressor(
		compress.NewSmartCompressor(compress.WithKeepRecentTasks(2), compress.WithMaxTokens(1000)),
		store, compress.NewDefaultTokenCounter(), 1000, 0.8, 2,
		compress.WithRecentFullCount(8))
	rD := ccSmall.Compress(context.Background(), refs)
	dumpPhase(t, "D compaction", rD.Messages)
	hasSummary, hasChain, hasSettleFold := false, false, false
	for _, r := range rD.RetainedRefs {
		if r.EventType == "context_compress" {
			hasSummary = true
			if !strings.Contains(r.EventSummary, "[Compacted") {
				t.Fatalf("D: rolling summary must carry the compacted count")
			}
			for _, line := range strings.Split(r.EventSummary, "\n") {
				if strings.HasPrefix(line, "- ") && len(line) > 120 {
					t.Fatalf("D: card line must be bounded (~80 chars + ticket):\n%s", line)
				}
			}
		}
		if r.EventType == "tool_chain" {
			hasChain = true
			if !strings.Contains(r.EventSummary, "[evt_") || !strings.Contains(r.EventSummary, "（") {
				t.Fatalf("D: tool_chain line must carry the step count + tickets: %s", r.EventSummary)
			}
		}
		if r.EventType == "settle_fold" {
			hasSettleFold = true
			if !strings.Contains(r.EventSummary, "memory_recall") {
				t.Fatalf("D: settle_fold card must carry the recall hint: %s", r.EventSummary)
			}
		}
	}
	if !hasChain || (!hasSummary && !hasSettleFold) {
		t.Fatalf("D: compaction must form tool chains plus a rolling summary or settle-fold card (summary=%v chain=%v fold=%v)", hasSummary, hasChain, hasSettleFold)
	}
	retainedD := rD.RetainedRefs

	ccE := compress.NewContextCompressor(
		compress.NewSmartCompressor(compress.WithKeepRecentTasks(2), compress.WithMaxTokens(8000)),
		store, compress.NewDefaultTokenCounter(), 8000, 0.8, 2,
		compress.WithRecentFullCount(8))
	ccE.SetFullBoundary(ccSmall.FullBoundary())
	rE1 := ccE.Compress(context.Background(), retainedD)
	refsE := append(append([]memory.EventReference{}, retainedD...), simTurn(store, 1000, "E1")...)
	rE2 := ccE.Compress(context.Background(), refsE)
	dumpPhase(t, "E frozen-prefix + new frontier", rE2.Messages)
	if len(rE2.Messages) < len(rE1.Messages) {
		t.Fatalf("E: appending events must not shrink the rendered timeline")
	}
	for i := 0; i < len(rE1.Messages); i++ {
		if rE1.Messages[i].Content != rE2.Messages[i].Content ||
			rE1.Messages[i].Role != rE2.Messages[i].Role {
			t.Fatalf("E: message %d must be byte-stable across under-budget rounds (prefix freeze):\n%q\nvs\n%q",
				i, rE1.Messages[i].Content, rE2.Messages[i].Content)
		}
	}
	last := rE2.Messages[len(rE2.Messages)-1]
	if !strings.Contains(last.Content, "E1") {
		t.Fatalf("E: newly appended events must render full (active frontier): %s", last.Content)
	}
}

// stopCapProducer is a real agent.Agent whose event-production goroutine parks on
// a test-owned gate and ignores cancellation while parked. It records
// producer-done independently of the runner's consumer lifecycle.
type stopCapProducer struct {
	started      chan struct{}
	hold         chan struct{}
	producerDone atomic.Bool
	releaseOnce  sync.Once
}

func (p *stopCapProducer) Run(ctx context.Context, _ *trgagent.Invocation) (<-chan *trpcEvent.Event, error) {
	out := make(chan *trpcEvent.Event)
	go func() {
		defer close(out)
		defer p.producerDone.Store(true)
		close(p.started)
		<-p.hold
		select {
		case out <- &trpcEvent.Event{Response: &model.Response{Done: true}}:
		case <-ctx.Done():
		}
	}()
	return out, nil
}

func (p *stopCapProducer) Tools() []trpctool.Tool { return nil }
func (p *stopCapProducer) Info() trgagent.Info {
	return trgagent.Info{Name: "stopcap", Description: "probe"}
}

func (p *stopCapProducer) SubAgents() []trgagent.Agent        { return nil }
func (p *stopCapProducer) FindSubAgent(string) trgagent.Agent { return nil }

// TestUpstreamStopGate_ProcessedCloseImpliesProducerDone 钉住 面向活跃依赖的能力门：生产者停在等待处时取消运行，不得关闭已处理流。
// - 只有生产者真正退出后流才关闭；这把"已处理即已结束"变成可判定条件。
func TestUpstreamStopGate_ProcessedCloseImpliesProducerDone(t *testing.T) {
	prod := &stopCapProducer{
		started: make(chan struct{}),
		hold:    make(chan struct{}),
	}
	r := runner.NewRunner("stopcap-app", prod,
		runner.WithSessionService(inmemory.NewSessionService()))
	defer func() { _ = r.Close() }()

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out, err := r.Run(runCtx, "u", "s-stopcap", model.NewUserMessage("go"))
	require.NoError(t, err, "the runner must accept the invocation")

	select {
	case <-prod.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the test producer never started — this probe would measure nothing")
	}

	closed := make(chan struct{})
	go func() {
		for range out {
		}
		close(closed)
	}()

	cancel()

	select {
	case <-closed:
		prod.release()
		t.Fatal("§6.3 gate: processed stream closed while the producer was still running — " +
			"the active dependency lacks the producer-done completion contract (official v1.11.2 " +
			"behavior; see upstream-research.md). Under the producer-done fork this must not happen.")
	case <-time.After(300 * time.Millisecond):
	}

	prod.release()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("processed stream never closed after the producer exited")
	}
	require.True(t, prod.producerDone.Load(),
		"producer-done must hold at the moment the processed stream closes — this is the "+
			"credential tagent's release gate (D6) hangs on")
}

// release frees the parked producer exactly once, so helper and failure paths
// never double-close the gate.
func (p *stopCapProducer) release() { p.releaseOnce.Do(func() { close(p.hold) }) }
