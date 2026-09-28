// 本文件负责一次回合的端到端装配：子 agent 运行在工具结果处正确停止、请求顺序（系统在前、
// 用户在后）、外部上下文应用的纯函数性与待处理事件的单次交接。
// 契约: docs/wiki/agent/event-flow.md#e2e-turn-sequence
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
package agent

import (
	"context"
	"encoding/json"
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
	"github.com/stretchr/testify/require"
	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	trpcEvent "trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/plugin"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// mockRecordingTool is a CallableTool that records every invocation and
// returns a fixed short result. It simulates a shell/openspec tool.
type mockRecordingTool struct {
	mu    sync.Mutex
	calls []string
}

func (t *mockRecordingTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name:        "action",
		Description: "Execute a shell command.",
		InputSchema: &trpctool.Schema{
			Type: "object",
			Properties: map[string]*trpctool.Schema{
				"command": {Type: "string", Description: "shell command"},
			},
			Required: []string{"command"},
		},
	}
}

func (t *mockRecordingTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var a struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(jsonArgs, &a)
	t.mu.Lock()
	t.calls = append(t.calls, a.Command)
	t.mu.Unlock()
	return "OK: " + a.Command + " done.", nil
}

func (t *mockRecordingTool) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.calls)
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// delayingSequenceModel returns responses in sequence, sleeping `delay` before
// every response AFTER the first. This simulates a slow real LLM (e.g. glm-5.2
// with thinking takes ~16s per round), which is the condition under which the
// sub-agent single-turn drain window (500ms) expires prematurely.
type delayingSequenceModel struct {
	responses []*model.Response
	delay     time.Duration
	mu        sync.Mutex
	idx       int
}

func (m *delayingSequenceModel) GenerateContent(
	ctx context.Context,
	request *model.Request,
) (<-chan *model.Response, error) {
	m.mu.Lock()
	idx := m.idx
	m.idx++
	m.mu.Unlock()

	if idx > 0 && m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			ch := make(chan *model.Response)
			close(ch)
			return ch, ctx.Err()
		}
	}

	ch := make(chan *model.Response, 1)
	if idx < len(m.responses) {
		ch <- m.responses[idx]
	}
	close(ch)
	return ch, nil
}

func (m *delayingSequenceModel) Info() model.Info { return model.Info{Name: "delaying-seq-model"} }

func (m *delayingSequenceModel) calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.idx
}

// toolCallResponse builds a model.Response containing a single tool call.
func toolCallResponse(id, cmd string) *model.Response {
	args, _ := json.Marshal(map[string]string{"command": cmd})
	return &model.Response{
		ID:   id,
		Done: true,
		Choices: []model.Choice{{
			Message: model.Message{
				Role: model.RoleAssistant,
				ToolCalls: []model.ToolCall{{
					Type: "function",
					ID:   id,
					Function: model.FunctionDefinitionParam{
						Name:      "action",
						Arguments: args,
					},
				}},
			},
		}},
	}
}

// finalTextResponse builds a plain assistant final response.
func finalTextResponse(id, text string) *model.Response {
	return &model.Response{
		ID:   id,
		Done: true,
		Choices: []model.Choice{{
			Message: model.Message{Role: model.RoleAssistant, Content: text},
		}},
	}
}

// TestSubAgentRun_ToolResultStopsPrematurely 钉住 单轮语义下的错误终止点：工具结果（不含后续工具调用）被当作最终答复时，子 agent 在第一次工具调用后就收场。
// - 断言锁定这一失败形态，防止"见工具结果即结束"被重新引入。
func TestSubAgentRun_ToolResultStopsPrematurely(t *testing.T) {
	callCount := 0
	seqModel := &sequenceMockModel{
		callCount: &callCount,
		responses: []*model.Response{
			toolCallResponse("call-1", "openspec init --tools none"),
			toolCallResponse("call-2", "openspec new change demo"),
			finalTextResponse("call-3", "Plan created."),
		},
	}

	recTool := &mockRecordingTool{}

	ta, err := NewTagentAgent(&TagentConfig{
		Model:             seqModel,
		SystemPrompt:      "You are a plan agent.",
		Name:              "plan",
		Description:       "Plan agent",
		MaxToolIterations: 15,
		Tools:             []trpctool.Tool{recTool},
	})
	if err != nil {
		t.Fatalf("NewTagentAgent: %v", err)
	}
	defer ta.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("Create a plan for demo.")))
	eventCh, err := ta.Run(ctx, inv)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var finalOutput string
	evtIdx := 0
	for evt := range eventCh {
		if evt == nil || evt.Response == nil || len(evt.Response.Choices) == 0 {
			continue
		}
		choice := evt.Response.Choices[len(evt.Response.Choices)-1]
		evtIdx++
		t.Logf("evt[%d] role=%s content=%q tool_calls=%d done=%v",
			evtIdx, choice.Message.Role, truncateStr(choice.Message.Content, 40),
			len(choice.Message.ToolCalls), evt.Response.Done)
		if choice.Message.Content != "" && len(choice.Message.ToolCalls) == 0 &&
			choice.Message.Role == model.RoleAssistant {
			finalOutput = choice.Message.Content
		}
	}

	toolCalls := recTool.count()
	t.Logf("tool calls executed: %d, model calls: %d, finalOutput=%q", toolCalls, callCount, finalOutput)

	if toolCalls < 2 {
		t.Errorf("❌ BUG REPRODUCED: sub-agent stopped after %d tool call(s); expected 2 "+
			"(tool result event was misinterpreted as final response). finalOutput=%q",
			toolCalls, finalOutput)
	}
	if finalOutput != "Plan created." {
		t.Errorf("❌ finalOutput = %q, want \"Plan created.\" (sub-agent returned tool "+
			"result instead of the assistant's final message)", finalOutput)
	}
}

// TestSubAgentRun_SlowLLM_ToolResultStops 钉住 模型响应慢于排水窗口时，单轮语义会在第一个工具结果后停下——窗口先于下一轮模型完成而到期。
// - 本用例把该时序竞争固定为可复现断言，防止过短窗口被重新引入。
func TestSubAgentRun_SlowLLM_ToolResultStops(t *testing.T) {
	seqModel := &delayingSequenceModel{
		delay: 1500 * time.Millisecond,
		responses: []*model.Response{
			toolCallResponse("call-1", "openspec init --tools none"),
			toolCallResponse("call-2", "openspec new change demo"),
			finalTextResponse("call-3", "Plan created."),
		},
	}

	recTool := &mockRecordingTool{}

	ta, err := NewTagentAgent(&TagentConfig{
		Model:             seqModel,
		SystemPrompt:      "You are a plan agent.",
		Name:              "plan",
		Description:       "Plan agent",
		MaxToolIterations: 15,
		Tools:             []trpctool.Tool{recTool},
	})
	if err != nil {
		t.Fatalf("NewTagentAgent: %v", err)
	}
	defer ta.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("Create a plan for demo.")))
	eventCh, err := ta.Run(ctx, inv)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var finalOutput string
	for evt := range eventCh {
		if evt == nil || evt.Response == nil || len(evt.Response.Choices) == 0 {
			continue
		}
		choice := evt.Response.Choices[len(evt.Response.Choices)-1]
		if choice.Message.Content != "" && len(choice.Message.ToolCalls) == 0 &&
			choice.Message.Role == model.RoleAssistant {
			finalOutput = choice.Message.Content
		}
	}

	toolCalls := recTool.count()
	t.Logf("tool calls executed: %d, model calls: %d, finalOutput=%q", toolCalls, seqModel.calls(), finalOutput)

	if toolCalls < 2 {
		t.Errorf("❌ BUG REPRODUCED (slow LLM): sub-agent stopped after %d tool call(s); "+
			"expected 2. The 500ms drain window expired before round 2 completed, so the "+
			"tool RESULT of round 1 was returned as the final output. finalOutput=%q",
			toolCalls, finalOutput)
	}
	if finalOutput != "Plan created." {
		t.Errorf("❌ finalOutput = %q, want \"Plan created.\"", finalOutput)
	}
}

// recordingSequenceModel returns responses in sequence and records the
// request.Messages passed on each GenerateContent call (i.e. AFTER the
// BeforeModel rebuild), so tests can assert message ordering.
type recordingSequenceModel struct {
	responses []*model.Response
	mu        sync.Mutex
	idx       int
	requests  [][]model.Message
}

func (m *recordingSequenceModel) GenerateContent(
	ctx context.Context,
	request *model.Request,
) (<-chan *model.Response, error) {
	m.mu.Lock()
	snapshot := make([]model.Message, len(request.Messages))
	copy(snapshot, request.Messages)
	m.requests = append(m.requests, snapshot)
	idx := m.idx
	m.idx++
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	if idx < len(m.responses) {
		ch <- m.responses[idx]
	}
	close(ch)
	return ch, nil
}

func (m *recordingSequenceModel) Info() model.Info { return model.Info{Name: "recording-seq-model"} }

func (m *recordingSequenceModel) lastRequest() []model.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.requests) == 0 {
		return nil
	}
	return m.requests[len(m.requests)-1]
}

// TestSubAgentRun_RequestOrdering_UserAfterSystem 钉住 子 agent 的驱动请求在整条时间线里保持在最前，紧跟系统消息之后。
// - 多轮推理迭代后也不得被累积的助手与工具历史埋到末尾。
func TestSubAgentRun_RequestOrdering_UserAfterSystem(t *testing.T) {
	seqModel := &recordingSequenceModel{
		responses: []*model.Response{
			toolCallResponse("call-1", "openspec init --tools none"),
			toolCallResponse("call-2", "openspec new change demo"),
			finalTextResponse("call-3", "Plan created."),
		},
	}
	recTool := &mockRecordingTool{}

	ta, err := NewTagentAgent(&TagentConfig{
		Model:             seqModel,
		SystemPrompt:      "You are a plan agent.",
		Name:              "plan",
		Description:       "Plan agent",
		MaxToolIterations: 15,
		Tools:             []trpctool.Tool{recTool},
	})
	if err != nil {
		t.Fatalf("NewTagentAgent: %v", err)
	}
	defer ta.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	const request = "Create a plan for demo."
	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage(request)))
	eventCh, err := ta.Run(ctx, inv)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for range eventCh {
	}

	last := seqModel.lastRequest()
	if len(last) < 3 {
		t.Fatalf("expected accumulated messages in final request, got %d: %+v", len(last), last)
	}

	if last[0].Role != model.RoleSystem {
		t.Errorf("expected messages[0] to be system, got %s", last[0].Role)
	}

	requestIdx, requestCount := -1, 0
	for i, m := range last {
		if m.Role == model.RoleUser && strings.Contains(m.Content, request) {
			requestCount++
			if requestIdx < 0 {
				requestIdx = i
			}
		}
	}
	if requestCount != 1 {
		t.Fatalf("❌ expected the request exactly once, got %d (duplication or drop): %s",
			requestCount, dumpRoles(last))
	}

	if requestIdx != 1 {
		t.Errorf("❌ BUG: user request at index %d, want 1 (right after system). Order: %s",
			requestIdx, dumpRoles(last))
	}
	if requestIdx == len(last)-1 {
		t.Errorf("❌ BUG: user request is the LAST message (buried after ReAct history). Order: %s",
			dumpRoles(last))
	}
	hasReActAfterUser := false
	for _, m := range last[requestIdx+1:] {
		if m.Role == model.RoleAssistant {
			hasReActAfterUser = true
			break
		}
	}
	if !hasReActAfterUser {
		t.Errorf("expected assistant ReAct messages after the user request. Order: %s", dumpRoles(last))
	}

	t.Logf("✅ final request order: %s", dumpRoles(last))
}

func dumpRoles(msgs []model.Message) string {
	var b strings.Builder
	for i, m := range msgs {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(string(m.Role))
	}
	return b.String()
}

// TestApplyExternalContext_IsPureAndCompact 钉住 钉住 applyExternalContext 是纯函数：只把给定事件折进消息，不改其它状态、不累积副本。
func TestApplyExternalContext_IsPureAndCompact(t *testing.T) {
	msg := model.NewUserMessage("do the thing")
	events := []memory.FullEvent{
		{EventType: "note", EventSummary: "first note"},
		{EventType: "note", EventSummary: "second note"},
	}
	out := applyExternalContext(msg, events)
	require.Contains(t, out.Content, "first note")
	require.Contains(t, out.Content, "second note")
	require.Contains(t, out.Content, "do the thing")

	untouched := applyExternalContext(model.NewUserMessage("plain"), nil)
	require.Equal(t, "plain", untouched.Content)

	require.Equal(t, "do the thing", msg.Content)
}

// TestDrainPendingExternalEvents_SingleHandoff 钉住 钉住 direct-Ingest 槽是一次性交收：一次 drain 取走全部并清空槽位，再次 drain 返回 nil。
func TestDrainPendingExternalEvents_SingleHandoff(t *testing.T) {
	ta := &TagentAgent{name: "sink"}
	ta.IngestExternalEvents([]memory.FullEvent{{EventSummary: "one"}})

	first := ta.drainPendingExternalEvents()
	require.Len(t, first, 1)
	require.Equal(t, "one", first[0].EventSummary)

	require.Nil(t, ta.drainPendingExternalEvents(), "second drain must be empty")
	require.Nil(t, ta.pendingExternalEvents, "slot cleared after handoff")
}

// TestDrainPendingExternalEvents_ConcurrentNoTear 钉住 钉住 并发 Ingest 与 drain 不得撕裂共享槽位。
func TestDrainPendingExternalEvents_ConcurrentNoTear(t *testing.T) {
	ta := &TagentAgent{name: "sink"}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(2)
		go func() {
			defer wg.Done()
			ta.IngestExternalEvents([]memory.FullEvent{{EventSummary: fmt.Sprint("ingest", i)}})
		}()
		go func() {
			defer wg.Done()
			got := ta.drainPendingExternalEvents()
			for _, e := range got {
				require.Contains(t, e.EventSummary, "ingest")
			}
		}()
	}
	wg.Wait()
}

// TestRun_MediaOnlyInputIsNotDropped 钉住 只有图片或文件内容的委派（正文为空但分块合法）不得被压成空文本。
// - 事件因此仍然建成并抵达模型，而不是被当成空输入跳过。
func TestRun_MediaOnlyInputIsNotDropped(t *testing.T) {
	callCount := 0
	seq := &sequenceMockModel{
		callCount: &callCount,
		responses: []*model.Response{finalTextResponse("c1", "handled")},
	}
	ta, err := NewTagentAgent(&TagentConfig{
		Model: seq, SystemPrompt: "You are a vision agent.",
		Name: "vision", Description: "v", MaxToolIterations: 3,
	})
	require.NoError(t, err)
	defer ta.Close()

	mediaOnly := model.Message{
		Role: model.RoleUser, Content: "",
		ContentParts: []model.ContentPart{
			{Type: model.ContentTypeImage, Image: &model.Image{URL: "http://host/img.png"}},
		},
	}
	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(mediaOnly))
	ch, err := ta.Run(context.Background(), inv)
	require.NoError(t, err, "media-only input must NOT be rejected as empty (parts must survive)")
	require.NotNil(t, ch)
	for range ch {
	}
	require.GreaterOrEqual(t, callCount, 1,
		"the model must be invoked for a media-only turn — not silently skipped")
}

// TestRun_EmptyInputIsRejectedExplicitly 钉住 既无内容也无分块的委派必须在边界处响亮失败。
// - 任其流到持久化的空输入跳过路径，等于用零事件悄悄关掉调用方的通道。
func TestRun_EmptyInputIsRejectedExplicitly(t *testing.T) {
	callCount := 0
	seq := &sequenceMockModel{callCount: &callCount, responses: []*model.Response{finalTextResponse("c1", "x")}}
	ta, err := NewTagentAgent(&TagentConfig{Model: seq, SystemPrompt: "s", Name: "empty", Description: "e"})
	require.NoError(t, err)
	defer ta.Close()

	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("")))
	ch, err := ta.Run(context.Background(), inv)
	require.Error(t, err, "truly-empty delegation input must be rejected explicitly")
	require.Nil(t, ch)
	require.Equal(t, 0, callCount, "no model call for a rejected input")
}

// spawnerProbeTool spawns one task through whatever task.TaskSpawner its call context
// carries. Its Declaration matches the shared toolCallResponse helper ("action"
// with a "command" arg) so the scripted model reaches it via the normal tool
// dispatch path.
type spawnerProbeTool struct{ sawSpawner bool }

func (p *spawnerProbeTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name:        "action",
		Description: "probe",
		InputSchema: &trpctool.Schema{
			Type:       "object",
			Properties: map[string]*trpctool.Schema{"command": {Type: "string"}},
			Required:   []string{"command"},
		},
	}
}

func (p *spawnerProbeTool) Call(ctx context.Context, _ []byte) (any, error) {
	spawner, ok := task.TaskSpawnerFromContext(ctx)
	p.sawSpawner = ok
	if !ok {
		return "no spawner", nil
	}
	spawner.Spawn(
		task.TaskSpec{Kind: "probe", Desc: "ownership probe", Key: "probe-ownership"},
		task.NewFuncSettleDetector(context.Background(),
			func(context.Context) (string, error) { return "done", nil },
			10*time.Millisecond),
	)
	return "spawned", nil
}

// TestSubagentSpawnerOwnership 钉住 pins design D3.3: "继承调用版本／来源不等于继承父任务管理器".
func TestSubagentSpawnerOwnership(t *testing.T) {
	parentTM := task.NewTaskManager(task.TaskManagerConfig{})
	ctx := task.WithTaskSpawner(context.Background(), parentTM)

	callCount := 0
	seqModel := &sequenceMockModel{
		callCount: &callCount,
		responses: []*model.Response{
			toolCallResponse("call-1", "spawn something"),
			finalTextResponse("call-2", "done"),
		},
	}
	probe := &spawnerProbeTool{}

	b, err := NewTagentAgent(&TagentConfig{
		Model:             seqModel,
		SystemPrompt:      "You are callee B.",
		Name:              "callee-b",
		Description:       "callee",
		MaxToolIterations: 5,
		Tools:             []trpctool.Tool{probe},
	})
	require.NoError(t, err)
	defer b.Close()

	inv := trpcagent.NewInvocation(
		trpcagent.WithInvocationMessage(model.NewUserMessage("do your work")),
	)
	eventCh, err := b.Run(ctx, inv)
	require.NoError(t, err)
	for range eventCh {
	}

	require.True(t, probe.sawSpawner, "callee flow should carry a task spawner")

	require.Len(t, b.taskManager.List(), 1,
		"callee-spawned task must register in the CALLEE's own task manager (design D3.3)")
	require.Empty(t, parentTM.List(),
		"父 spawner 遮蔽: the callee's task leaked into the PARENT's manager")
}

// TestNewDelegationEvent_CarriesCorrelationHandle 钉住 请求响应式子调用的输入携带其发起调用的关联句柄。
// - 常驻属主之后正是用它把后台续发路由回正确的那条等待中的请求。
func TestNewDelegationEvent_CarriesCorrelationHandle(t *testing.T) {
	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("do it")))
	require.NotEmpty(t, inv.InvocationID, "NewInvocation always assigns a UUID")

	evt := newDelegationEvent(inv, model.NewUserMessage("do it"))
	require.Equal(t, tagentevent.TypeExternalInput, evt.Type)
	require.Equal(t, "user", evt.Source)
	require.Equal(t, inv.InvocationID, evt.Metadata[metaKeyInvocationID],
		"the correlation handle must ride the delegation input")
	require.NotNil(t, evt.Message)
	require.Equal(t, "do it", evt.Message.Content, "the message payload is preserved unchanged")
}

// TestNewDelegationEvent_NilInvocationIsSafe 钉住 guards the helper against a nil/ID-less invocation (no panic, no bogus handle).
func TestNewDelegationEvent_NilInvocationIsSafe(t *testing.T) {
	evt := newDelegationEvent(nil, model.NewUserMessage("x"))
	require.NotContains(t, evt.Metadata, metaKeyInvocationID)
}

// TestInvocationIDIsControlKey 钉住 关联句柄属控制元数据：绝不被外提为调用元数据。
// - 一旦被外提，它就会以用户可见字段或模型可见文本的形式回流，而模型可伪造它来劫持路由。
func TestInvocationIDIsControlKey(t *testing.T) {
	require.True(t, controlMetaKeys[metaKeyInvocationID],
		"invocation_id must be a control key (D4: 控制字段不透传给模型)")

	evt := NewExternalInputEvent("user", model.NewUserMessage("hi"))
	evt.Metadata[metaKeyInvocationID] = "corr-123"
	evt.Metadata["chat_id"] = "chat-9"

	md := extractRootMetadata([]*AgentEvent{evt})
	require.Equal(t, "chat-9", md["chat_id"], "ordinary metadata still propagates normally")
	require.NotContains(t, md, metaKeyInvocationID,
		"the correlation handle must not leak into invocation/model-visible metadata")
}

// TestDelegationOriginCarriesInvocationID 钉住 携带关联句柄的委派回合里派生的任务，其不透明 Origin 必须带上同一句柄。
// - 这是越窗结算复用的路由袋：Origin 原样进元数据，按调用的循环据此把迟到结果送回发起它的那次调用；
// - 该句柄当前没有消费者读取，属为后续路由预置的中性事实。
// 契约: docs/wiki/platform/reincarnation-notice.md#delivery
func TestDelegationOriginCarriesInvocationID(t *testing.T) {
	callCount := 0
	seq := &sequenceMockModel{
		callCount: &callCount,
		responses: []*model.Response{
			toolCallResponse("call-1", "spawn something"),
			finalTextResponse("call-2", "done"),
		},
	}
	probe := &spawnerProbeTool{}
	b, err := NewTagentAgent(&TagentConfig{
		Model: seq, SystemPrompt: "You are callee B.",
		Name: "callee-b-s2m", Description: "c", MaxToolIterations: 5,
		Tools: []trpctool.Tool{probe},
	})
	require.NoError(t, err)
	defer b.Close()

	inv := trpcagent.NewInvocation(
		trpcagent.WithInvocationMessage(model.NewUserMessage("do your work")),
	)
	inv.InvocationID = "deleg-x-42"

	ch, err := b.Run(context.Background(), inv)
	require.NoError(t, err)
	for range ch {
	}

	require.True(t, probe.sawSpawner, "callee flow must carry a task spawner")
	require.Len(t, b.taskManager.List(), 1, "D3: the task registers in B's own manager")

	tk := b.taskManager.List()[0]
	require.Equal(t, "deleg-x-42", tk.Spec.Origin[metaKeyInvocationID],
		"S2m: spawned task Origin must carry the delegation invocation id for越窗 routing (M2)")
}

// TestControlKeyNotModelVisible 钉住 路由袋（如会话标识）照常传入调用元数据，而控制键不得进入模型可见的管线。
// - 两件事必须同时成立：新增控制键不改变模型可见内容，也不得顺手掐断既有路由信息。
func TestControlKeyNotModelVisible(t *testing.T) {
	events := []*AgentEvent{
		{
			Type:     tagentevent.TypeExternalInput,
			Metadata: map[string]any{"chat_id": "c-1", metaKeyInvocationID: "deleg-x-42"},
		},
	}
	md := extractRootMetadata(events)
	require.Equal(t, "c-1", md["chat_id"], "chat_id must propagate to invocation metadata")
	_, leaked := md[metaKeyInvocationID]
	require.False(t, leaked,
		"S1/S2m: invocation_id is a control key — must NEVER reach meta_*/model-visible metadata")

	require.Equal(t, "deleg-x-42", extractDelegationInvocationID(events))
}

// TestExtractDelegationInvocationID locks the extractor's behavior-neutral edges.
func TestExtractDelegationInvocationID(t *testing.T) {
	require.Equal(t, "", extractDelegationInvocationID(nil), "nil events → empty")
	require.Equal(t, "", extractDelegationInvocationID([]*AgentEvent{nil}), "nil event → empty")
	plain := &AgentEvent{Type: tagentevent.TypeExternalInput, Metadata: map[string]any{"chat_id": "c"}}
	require.Equal(t, "", extractDelegationInvocationID([]*AgentEvent{plain}), "no handle → empty (non-delegation turn)")
	other := &AgentEvent{Type: "response_done", Metadata: map[string]any{metaKeyInvocationID: "zzz"}}
	require.Equal(t, "", extractDelegationInvocationID([]*AgentEvent{other}))
	settle := &AgentEvent{
		Type:     tagentevent.TypeExternalInput,
		Source:   SourceTask,
		Metadata: map[string]any{metaKeyInvocationID: "deleg-x-42", "task_id": "t1"},
	}
	require.Equal(t, "", extractDelegationInvocationID([]*AgentEvent{settle}),
		"W-2: settle/reclaim events must not carry invocation_id into a new turn")
	input := &AgentEvent{
		Type:     tagentevent.TypeExternalInput,
		Metadata: map[string]any{metaKeyInvocationID: "deleg-x-42"},
	}
	require.Equal(t, "deleg-x-42", extractDelegationInvocationID([]*AgentEvent{input}))
}

// TestSequentialDelegationsPerCallAttribution 钉住 关联标识不得跨调用黏住：同一被调方的两次顺序委派各自把派生任务记到自己的标识下。
// - 该值只从本次调用的上下文读取，绝不缓存在共享管理器状态上。
func TestSequentialDelegationsPerCallAttribution(t *testing.T) {
	newRun := func(id string) *task.Task {
		callCount := 0
		seq := &sequenceMockModel{
			callCount: &callCount,
			responses: []*model.Response{
				toolCallResponse("c1", "spawn"),
				finalTextResponse("c2", "done"),
			},
		}
		probe := &spawnerProbeTool{}
		b, err := NewTagentAgent(&TagentConfig{
			Model: seq, SystemPrompt: "callee", Name: "callee-seq-" + id,
			Description: "c", MaxToolIterations: 5, Tools: []trpctool.Tool{probe},
		})
		require.NoError(t, err)
		defer b.Close()
		inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("work")))
		inv.InvocationID = id
		ch, err := b.Run(context.Background(), inv)
		require.NoError(t, err)
		for range ch {
		}
		tks := b.taskManager.List()
		require.Len(t, tks, 1)
		return tks[0]
	}
	require.Equal(t, "inv-AAA", newRun("inv-AAA").Spec.Origin[metaKeyInvocationID])
	require.Equal(t, "inv-BBB", newRun("inv-BBB").Spec.Origin[metaKeyInvocationID])
}

// bgProbeTool spawns one task through whatever task.TaskSpawner its call context
// carries, using a manual detector whose sync-wait window CLOSES quickly (detach),
// and hands that detector to the test only AFTER Spawn has returned from that window.
// So by the time the test holds the detector the spawn is provably a BACKGROUND one
// (SpawnResult.Settled == false) → its later Emit drives OnSettle → route → the M2
// tail. Declaration matches the shared toolCallResponse helper ("action"/"command").
type bgProbeTool struct{ spawned chan *task.ManualDetector }

func (p *bgProbeTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name:        "action",
		Description: "probe",
		InputSchema: &trpctool.Schema{
			Type:       "object",
			Properties: map[string]*trpctool.Schema{"command": {Type: "string"}},
			Required:   []string{"command"},
		},
	}
}

func (p *bgProbeTool) Call(ctx context.Context, _ []byte) (any, error) {
	spawner, ok := task.TaskSpawnerFromContext(ctx)
	if !ok {
		return "no spawner", nil
	}
	det := task.NewManualDetectorDetach(20 * time.Millisecond)
	spawner.Spawn(task.TaskSpec{Kind: "probe", Desc: "bg job", Key: "bg-1"}, det)
	p.spawned <- det
	return "spawned", nil
}

// TestWindowCrossingContinuation 钉住 主子同构的越窗判据：子调用的回合派生了后台任务时，初始答复之后同一条通道必须继续开着。
// - 它要收到该任务的迟到结算，在自己的调用管理器上跑一次续发回合，而后才静默并关闭；
// - 调用方因此同时拿到初始与续发两个答复。
func TestWindowCrossingContinuation(t *testing.T) {
	callCount := 0
	seq := &sequenceMockModel{
		callCount: &callCount,
		responses: []*model.Response{
			toolCallResponse("t1", "go"),
			finalTextResponse("t1", "first answer"),
			finalTextResponse("t2", "continuation answer"),
		},
	}
	spawned := make(chan *task.ManualDetector, 4)
	probe := &bgProbeTool{spawned: spawned}

	b, err := NewTagentAgent(&TagentConfig{
		Model: seq, SystemPrompt: "You are callee B.",
		Name: "callee-s3mb", Description: "c", MaxToolIterations: 5,
		Tools: []trpctool.Tool{probe},
	})
	require.NoError(t, err)
	defer b.Close()

	inv := trpcagent.NewInvocation(
		trpcagent.WithInvocationMessage(model.NewUserMessage("do the work")),
	)
	ch, err := b.Run(context.Background(), inv)
	require.NoError(t, err)

	det := <-spawned
	det.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "async result"})
	det.Done()

	var texts []string
	closed := make(chan struct{})
	go func() {
		for evt := range ch {
			if evt == nil || evt.Response == nil || len(evt.Response.Choices) == 0 {
				continue
			}
			m := evt.Response.Choices[len(evt.Response.Choices)-1].Message
			if m.Role == model.RoleAssistant && m.Content != "" && len(m.ToolCalls) == 0 {
				texts = append(texts, m.Content)
			}
		}
		close(closed)
	}()

	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("invocation channel did not close within 5s — the M2 tail did not quiesce")
	}

	require.Contains(t, texts, "first answer", "the initial response reaches the caller")
	require.Contains(t, texts, "continuation answer",
		"the越窗 background settle drove a continuation turn delivered on the SAME call channel")
	require.Less(t, indexOfStr(texts, "first answer"), indexOfStr(texts, "continuation answer"),
		"the continuation answer must come after the initial answer (two-stage ACK→补最终, D-a)")
}

func indexOfStr(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// TestRunDoesNotStompSharedSessionContext 钉住 子调用派生的会话上下文只在本地生效，绝不覆写属主共享的用户/会话标识：否则并发的 Run 会把属主的空闲会话上下文改掉，这正是共享可变状态的反模式。
func TestRunDoesNotStompSharedSessionContext(t *testing.T) {
	callCount := 0
	seq := &sequenceMockModel{
		callCount: &callCount,
		responses: []*model.Response{finalTextResponse("t1", "answer")},
	}
	b, err := NewTagentAgent(&TagentConfig{
		Model: seq, SystemPrompt: "callee", Name: "callee-sessionishare",
		Description: "c", MaxToolIterations: 5,
	})
	require.NoError(t, err)
	defer b.Close()

	b.setSessionContext("owner-user", "owner-sentinel-session")

	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("hi")))
	ch, err := b.Run(context.Background(), inv)
	require.NoError(t, err)
	for range ch {
	}

	require.Equal(t, "owner-user", b.lastUserID,
		"family-3: a delegation must not overwrite the shared owner userID")
	require.Equal(t, "owner-sentinel-session", b.lastSessionID,
		"family-3: a delegation must not stomp the shared owner session context")
}

// TestContainmentNonAsyncCallClosesAfterFirstAnswer 钉住 未派生后台任务的子调用，其尾巴是空操作：无记账时立即判定静默。
// - 通道因此在单轮之后立刻关闭，事件数与请求响应形态都与同步行为一致——不多事件、不悬挂。
func TestContainmentNonAsyncCallClosesAfterFirstAnswer(t *testing.T) {
	callCount := 0
	seq := &sequenceMockModel{
		callCount: &callCount,
		responses: []*model.Response{
			finalTextResponse("t1", "plain answer"),
			finalTextResponse("t2", "should never run"),
		},
	}
	b, err := NewTagentAgent(&TagentConfig{
		Model: seq, SystemPrompt: "You are callee B.",
		Name: "callee-s3mb-contain", Description: "c", MaxToolIterations: 5,
	})
	require.NoError(t, err)
	defer b.Close()

	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("hi")))
	ch, err := b.Run(context.Background(), inv)
	require.NoError(t, err)

	var texts []string
	for evt := range ch {
		if evt == nil || evt.Response == nil || len(evt.Response.Choices) == 0 {
			continue
		}
		m := evt.Response.Choices[len(evt.Response.Choices)-1].Message
		if m.Role == model.RoleAssistant && m.Content != "" && len(m.ToolCalls) == 0 {
			texts = append(texts, m.Content)
		}
	}
	require.Equal(t, []string{"plain answer"}, texts,
		"a non-background-spawning sub-call runs exactly one turn and closes (containment)")
}

// TestCallerCancelClosesChannelNotCallee 钉住 调用方挂断（上下文取消或超时）只终结本次调用的循环与通道。
// - 不得关掉被调方，也不得连带取消被调方在跑的后台任务；
// - 接收者注销之后才到达的结算回落到总线安全丢弃，不得 panic。
// 契约: docs/wiki/agent/execution-generations.md#turn-local-execution-face
func TestCallerCancelClosesChannelNotCallee(t *testing.T) {
	callCount := 0
	seq := &sequenceMockModel{
		callCount: &callCount,
		responses: []*model.Response{
			toolCallResponse("t1", "go"),
			finalTextResponse("t1", "first answer"),
		},
	}
	spawned := make(chan *task.ManualDetector, 4)
	probe := &bgProbeTool{spawned: spawned}

	b, err := NewTagentAgent(&TagentConfig{
		Model: seq, SystemPrompt: "You are callee B.",
		Name: "callee-s4-cancel", Description: "c", MaxToolIterations: 5,
		Tools: []trpctool.Tool{probe},
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	inv := trpcagent.NewInvocation(
		trpcagent.WithInvocationMessage(model.NewUserMessage("do the work")),
	)
	ch, err := b.Run(ctx, inv)
	require.NoError(t, err)

	det := <-spawned

	cancel()

	closed := make(chan struct{})
	go func() {
		for range ch {
		}
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("caller cancel did not close the invocation channel — the tail must exit on ctx")
	}

	require.NotEmpty(t, b.taskManager.List(),
		"D-c: cancel terminates only the call's loop+channel — never the callee's in-flight task")

	require.NotPanics(t, func() {
		det.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "late"})
		det.Done()
	})

	b.Close()
}

// TestCalleeTimeoutViaCtx 钉住 同一终止也可由截止时刻触发（不只是显式取消）：短超时的上下文会让尾巴结束，通道照样关闭。
// - 此刻仍有后台任务未结算，超时也必须生效。
func TestCalleeTimeoutViaCtx(t *testing.T) {
	callCount := 0
	seq := &sequenceMockModel{
		callCount: &callCount,
		responses: []*model.Response{
			toolCallResponse("t1", "go"),
			finalTextResponse("t1", "first answer"),
		},
	}
	spawned := make(chan *task.ManualDetector, 4)
	probe := &bgProbeTool{spawned: spawned}

	b, err := NewTagentAgent(&TagentConfig{
		Model: seq, SystemPrompt: "callee", Name: "callee-s4-timeout",
		Description: "c", MaxToolIterations: 5, Tools: []trpctool.Tool{probe},
	})
	require.NoError(t, err)
	defer b.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("work")))
	ch, err := b.Run(ctx, inv)
	require.NoError(t, err)

	<-spawned

	closed := make(chan struct{})
	go func() {
		for range ch {
		}
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("ctx deadline did not end the tail — the loop must be bounded by the caller's ctx")
	}
}

// TestSettleSinkRegistry_ConcurrentPerInvocationIsolation 钉住 同一被调方的并发委派绝不互串接收者。
// - 路由表按调用绑定各自的总线句柄，只发到该句柄的总线；
// - 每个循环回收到的事件都须是自己那份（按来源标记），屏障独立到达静默；
// - 本测试在竞态检测下同时验证注册表的锁纪律。
// 契约: docs/wiki/agent/execution-generations.md#turn-local-execution-face
func TestSettleSinkRegistry_ConcurrentPerInvocationIsolation(t *testing.T) {
	r := newSettleSinkRegistry()

	const invocations = 8
	const perInvocation = 25

	var wg sync.WaitGroup
	results := make([][]string, invocations)

	for i := 0; i < invocations; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("inv-%d", i)
			invBus := NewEventBus()
			r.bind(id, invBus)

			for k := 0; k < perInvocation; k++ {
				r.noteSpawn(id)
			}

			// A producer for a DIFFERENT-looking handle must never land in our bus:
			// deliver our own settles (each tagged with our id) and reclaim them.
			var mine []string
			for k := 0; k < perInvocation; k++ {
				evtID := fmt.Sprintf("%s#%d", id, k)
				require.True(t, r.route(id, &AgentEvent{ID: evtID}), "publish to own bound bus")
				for _, e := range invBus.TryPull() {
					mine = append(mine, e.ID)
				}
			}
			for _, e := range invBus.TryPull() {
				mine = append(mine, e.ID)
			}

			require.True(t, r.quiescent(id), "own barrier reaches 0 despite concurrent siblings")

			results[i] = mine
			r.unbind(id)
		}(i)
	}
	wg.Wait()

	for i := 0; i < invocations; i++ {
		id := fmt.Sprintf("inv-%d", i)
		for _, evtID := range results[i] {
			require.Truef(t, strings.HasPrefix(evtID, id+"#"),
				"invocation %s reclaimed a foreign event %s — call buses crossed", id, evtID)
		}
	}
}

// TestSettleSinkRegistry_ConcurrentSameNameDistinctHandles 钉住 句柄不同而被调方同名的两次委派不得合并。
// - 屏障与总线绑定严格按句柄划分：记在一个句柄名下的结算，不被另一个计数或投递；
// - 若改用按 agent 共享的字段，这对同名委派就会被并成一团。
func TestSettleSinkRegistry_ConcurrentSameNameDistinctHandles(t *testing.T) {
	r := newSettleSinkRegistry()
	busX := NewEventBus()
	busY := NewEventBus()
	r.bind("callee/X", busX)
	r.bind("callee/Y", busY)

	var wg sync.WaitGroup
	for _, id := range []string{"callee/X", "callee/Y"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			for k := 0; k < 50; k++ {
				r.noteSpawn(id)
			}
			for k := 0; k < 50; k++ {
				require.True(t, r.route(id, &AgentEvent{ID: id}))
			}
		}(id)
	}
	wg.Wait()

	require.True(t, r.quiescent("callee/X"))
	require.True(t, r.quiescent("callee/Y"))
	require.Len(t, drainIDs(busX), 50, "X's bus holds exactly X's settles")
	require.Len(t, drainIDs(busY), 50, "Y's bus holds exactly Y's settles")
}

// echoModel is a STATELESS, content-derived model — safe for concurrent delegation
// tests because it holds no per-agent counter that two racing Runs could interleave
// (the shared sequenceMockModel's flaw). Its rule, a pure function of the request:
// - the history has no assistant tool-call yet → emit ONE tool call (spawns a bg task)
// - an assistant tool-call already exists → emit a final that echoes the LAST message
//
// So each invocation: turn1 first call → tool call; thereafter → "answer:<last>" (the
// initial answer after the tool result, and the continuation echoing the settle event
// whose rendered content carries the settle Output "SETTLE-<k>"). Detecting on the
// assistant tool-call (not on tool-role messages) is representation-stable. That echo
// lets a test correlate which background settle reached which caller.
type echoModel struct{}

func (m *echoModel) GenerateContent(_ context.Context, request *model.Request) (<-chan *model.Response, error) {
	nonSystem := 0
	for _, msg := range request.Messages {
		if msg.Role != model.RoleSystem {
			nonSystem++
		}
	}
	last := ""
	if n := len(request.Messages); n > 0 {
		last = request.Messages[n-1].Content
	}
	var resp *model.Response
	if nonSystem <= 1 {
		resp = toolCallResponse("spawn", "go")
	} else {
		resp = finalTextResponse("echo", "answer:"+last)
	}
	ch := make(chan *model.Response, 1)
	ch <- resp
	close(ch)
	return ch, nil
}

func (m *echoModel) Info() model.Info { return model.Info{Name: "echo"} }

// e2eProbeTool spawns one background task through the ctx spawner with an EMPTY Key
// (empty Key disables the manager's idempotency dedup, so two concurrent delegations
// each get their OWN task instead of the second being collapsed onto the first) and a
// ManualDetector whose sync-wait window detaches quickly; it hands the detector to the
// test AFTER Spawn returns (so the test's settle Emit is provably post-window/
// background → fires OnSettle → routes to that invocation's sink).
type e2eProbeTool struct{ spawned chan *task.ManualDetector }

func (p *e2eProbeTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name:        "action",
		Description: "probe",
		InputSchema: &trpctool.Schema{
			Type:       "object",
			Properties: map[string]*trpctool.Schema{"command": {Type: "string"}},
			Required:   []string{"command"},
		},
	}
}

func (p *e2eProbeTool) Call(ctx context.Context, _ []byte) (any, error) {
	spawner, ok := task.TaskSpawnerFromContext(ctx)
	if !ok {
		return "no spawner", nil
	}
	det := task.NewManualDetectorDetach(20 * time.Millisecond)
	spawner.Spawn(task.TaskSpec{Kind: "probe", Desc: "i1 e2e bg"}, det)
	p.spawned <- det
	return "spawned", nil
}

// TestConcurrentDelegationsEndToEnd 钉住 主子同构在并发下成立：同一被调方的两个并发委派各自派生迟到的后台任务，每个都要交付初次答复、在**自己的**结算到达同一条返回通道时独立续跑，且接收者互不串台——串台表现为某条通道悬挂（结算被偷）或某条收到两次（重复续跑）。
func TestConcurrentDelegationsEndToEnd(t *testing.T) {
	spawned := make(chan *task.ManualDetector, 4)
	probe := &e2eProbeTool{spawned: spawned}

	b, err := NewTagentAgent(&TagentConfig{
		Model: &echoModel{}, SystemPrompt: "You are callee B.",
		Name: "callee-i1-e2e", Description: "c", MaxToolIterations: 5,
		Tools: []trpctool.Tool{probe},
	})
	require.NoError(t, err)
	defer b.Close()

	ids := []string{"inv-A", "inv-B"}
	texts := make([][]string, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			runCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			inv := trpcagent.NewInvocation(
				trpcagent.WithInvocationMessage(model.NewUserMessage("do work " + id)),
			)
			inv.InvocationID = id
			ch, err := b.Run(runCtx, inv)
			require.NoError(t, err)
			for evt := range ch {
				if evt == nil || evt.Response == nil || len(evt.Response.Choices) == 0 {
					continue
				}
				m := evt.Response.Choices[len(evt.Response.Choices)-1].Message
				if m.Role == model.RoleAssistant && m.Content != "" && len(m.ToolCalls) == 0 {
					texts[i] = append(texts[i], m.Content)
				}
			}
		}(i, id)
	}

	dets := make([]*task.ManualDetector, 0, 2)
	for len(dets) < 2 {
		select {
		case d := <-spawned:
			dets = append(dets, d)
		case <-time.After(30 * time.Second):
			t.Fatalf("concurrent delegations must each spawn one background task: captured %d of 2", len(dets))
		}
	}

	for k, d := range dets {
		d.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: fmt.Sprintf("SETTLE-%d", k)})
		d.Done()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	timedOut := false
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		timedOut = true
	}

	t.Logf("inv-A texts=%v", texts[0])
	t.Logf("inv-B texts=%v", texts[1])
	if timedOut {
		t.Errorf("each concurrent delegation must close its own channel (see texts above)")
	}

	// Each delegation received exactly ONE continuation, and the two continuations
	// carry DIFFERENT settle tags → no receiver saw the other's settle.
	var tags []string
	for i := range texts {
		var mine, initial []string
		for _, tx := range texts[i] {
			if strings.Contains(tx, "SETTLE-") {
				mine = append(mine, tx)
			} else {
				initial = append(initial, tx)
			}
		}
		require.NotEmpty(t, initial,
			"delegation %s must receive its initial (first-answer) turn — texts=%v", ids[i], texts[i])
		require.Len(t, mine, 1,
			"delegation %s received %d continuations (want exactly its own 1) — texts=%v", ids[i], len(mine), texts[i])
		tags = append(tags, mine[0])
	}
	require.NotEqual(t, tags[0], tags[1],
		"both delegations received the SAME settle — call receivers are not isolated")
}

// TestHostDirectFormEquivalence 钉住 主子同构的另一半：同一越窗场景改走宿主直连形态也必须成立。
// - 常驻循环注入消息→本回合派生后台任务→迟到结算经持久总线→循环拉取→续跑→输出到循环出口；
// - 与被调方形态各自消费自己的迟到结算，并经同一续跑原语续到自己的接收者；
// - 两种形态等价不依赖跨发布与代际矩阵。
func TestHostDirectFormEquivalence(t *testing.T) {
	spawned := make(chan *task.ManualDetector, 4)
	probe := &e2eProbeTool{spawned: spawned}

	b, err := NewTagentAgent(&TagentConfig{
		Model: &echoModel{}, SystemPrompt: "You are a host agent.",
		Name: "host-form-eq", Description: "c", MaxToolIterations: 5,
		Tools: []trpctool.Tool{probe},
	})
	require.NoError(t, err)
	defer b.Close()

	outCh, err := b.StartLoop("host-user", "host-sess-eq")
	require.NoError(t, err)

	var mu sync.Mutex
	var texts []string
	stop := make(chan struct{})
	var collectWg sync.WaitGroup
	collectWg.Add(1)
	go func() {
		defer collectWg.Done()
		for {
			select {
			case <-stop:
				return
			case evt, ok := <-outCh:
				if !ok {
					return
				}
				if evt == nil || evt.Response == nil || len(evt.Response.Choices) == 0 {
					continue
				}
				m := evt.Response.Choices[len(evt.Response.Choices)-1].Message
				if m.Role == model.RoleAssistant && m.Content != "" && len(m.ToolCalls) == 0 {
					mu.Lock()
					texts = append(texts, m.Content)
					mu.Unlock()
				}
			}
		}
	}()

	b.InjectMessage(model.NewUserMessage("host go"))

	// The host turn spawns one background task (no delegation id); capture it.
	var det *task.ManualDetector
	select {
	case det = <-spawned:
	case <-time.After(5 * time.Second):
		mu.Lock()
		got := texts
		mu.Unlock()
		close(stop)
		b.StopLoop()
		collectWg.Wait()
		t.Fatalf("host form: no background spawn captured (owner turn did not reach the spawner); texts=%v", got)
	}

	det.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "SETTLE-HOST"})
	det.Done()

	deadline := time.After(5 * time.Second)
	sawContinuation := false
	for !sawContinuation {
		mu.Lock()
		for _, tx := range texts {
			if strings.Contains(tx, "SETTLE-HOST") {
				sawContinuation = true
			}
		}
		mu.Unlock()
		if sawContinuation {
			break
		}
		select {
		case <-deadline:
			sawContinuation = true
		case <-time.After(20 * time.Millisecond):
		}
	}

	mu.Lock()
	final := append([]string(nil), texts...)
	mu.Unlock()
	close(stop)
	b.StopLoop()
	collectWg.Wait()

	// Host form must produce BOTH an initial answer and a continuation from its OWN
	// late task_settled — the same越窗-continue-to-receiver behavior the callee form
	// (d8/d11) shows, via runEventLoop/processTurn instead of sink/tail.
	var initial, continuation int
	for _, tx := range final {
		if strings.Contains(tx, "SETTLE-HOST") {
			continuation++
		} else if strings.HasPrefix(tx, "answer:") {
			initial++
		}
	}
	require.GreaterOrEqual(t, initial, 1, "host form: initial (first-answer) turn must reach outputCh — texts=%v", final)
	require.Equal(t, 1, continuation,
		"host form: the owner's own late task_settled must drive exactly one continuation turn on outputCh — texts=%v", final)
}

// TestSettleRoutePublishesToBoundBus 钉住 钉住 已绑定调用的越窗结算必须落在它自己的 bus 上（经 TryPull），不走旁路队列。
func TestSettleRoutePublishesToBoundBus(t *testing.T) {
	sinks := newSettleSinkRegistry()
	invBus := NewEventBus()
	fallback := NewEventBus()
	tk := func(id string) *task.Task {
		return &task.Task{Spec: task.TaskSpec{Origin: map[string]string{metaKeyInvocationID: id}}}
	}

	sinks.bind("inv-1", invBus)

	evt := &AgentEvent{ID: "settle-1", Source: SourceTask}
	deliverTaskSettled(sinks, fallback, tk("inv-1"), evt)

	var onInvBus bool
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !onInvBus {
		for _, e := range invBus.TryPull() {
			if e != nil && e.ID == "settle-1" {
				onInvBus = true
			}
		}
		if !onInvBus {
			time.Sleep(5 * time.Millisecond)
		}
	}
	require.True(t, onInvBus, "I-3: a bound invocation's settle must appear on its OWN bus (TryPull), not a bypass queue")
	for _, e := range fallback.TryPull() {
		require.False(t, e != nil && e.ID == "settle-1", "a routed settle must NOT also hit the fallback bus")
	}
}

// TestSettleUnboundFallsBackToBus 钉住 收敛之后行为保持不变：无绑定的标识（入口属主、解绑后迟到的结算）照样落到共享总线。
func TestSettleUnboundFallsBackToBus(t *testing.T) {
	sinks := newSettleSinkRegistry()
	fallback := NewEventBus()
	tk := func(id string) *task.Task {
		return &task.Task{Spec: task.TaskSpec{Origin: map[string]string{metaKeyInvocationID: id}}}
	}

	deliverTaskSettled(sinks, fallback, tk("nobody"), &AgentEvent{ID: "b1", Source: SourceTask})
	var onBus bool
	for _, e := range fallback.TryPull() {
		if e != nil && e.ID == "b1" {
			onBus = true
		}
	}
	require.True(t, onBus, "no binding → fall back to the shared bus (entry owner path unchanged)")
}

// TestSettleAccountingSurvivesConvergence 钉住 注册表收缩为"绑定表＋屏障"后，投递记账的终结判定仍要成立。
// - 屏障只在每个已记账派生的结算都真正发到绑定总线之后才算静默；
// - 解绑同时终止路由与记账。
func TestSettleAccountingSurvivesConvergence(t *testing.T) {
	sinks := newSettleSinkRegistry()
	invBus := NewEventBus()
	sinks.bind("X", invBus)

	require.True(t, sinks.quiescent("X"))

	sinks.noteSpawn("X")
	sinks.noteSpawn("X")
	require.False(t, sinks.quiescent("X"))

	require.True(t, sinks.route("X", &AgentEvent{ID: "e1"}))
	require.False(t, sinks.quiescent("X"))

	require.True(t, sinks.route("X", &AgentEvent{ID: "e2"}))
	require.True(t, sinks.quiescent("X"))
	seen := map[string]bool{}
	for _, e := range invBus.TryPull() {
		if e != nil {
			seen[e.ID] = true
		}
	}
	require.True(t, seen["e1"] && seen["e2"], "both routed settles are pullable from the bound bus")

	sinks.unbind("X")
	require.False(t, sinks.quiescent("X"), "after unbind → no binding → not quiescent")
	require.False(t, sinks.route("X", &AgentEvent{ID: "late"}), "after unbind → route false → bus fallback")
}

// chainProbeTool spawns ONE background task per Call through whatever
// task.TaskSpawner its call context carries (detach → background), handing each
// detector to the test AFTER the sync window closes. It is invoked on the FIRST
// turn AND again on a CONTINUATION turn, so the second spawn exercises the
// property S3m-c's single-pipeline (c.2) depends on: a越窗 continuation turn's OWN
// background spawn must still be attributed to the same invocation id (threaded by
// the shell's ctx, NOT re-guessed from the settle batch, which the W-2 gate
// strips). Uses an EMPTY Key so consecutive spawns are not deduped. Declaration
// matches the shared toolCallResponse helper ("action"/"command").
type chainProbeTool struct{ spawned chan *task.ManualDetector }

func (p *chainProbeTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name:        "action",
		Description: "probe",
		InputSchema: &trpctool.Schema{
			Type:       "object",
			Properties: map[string]*trpctool.Schema{"command": {Type: "string"}},
			Required:   []string{"command"},
		},
	}
}

func (p *chainProbeTool) Call(ctx context.Context, _ []byte) (any, error) {
	spawner, ok := task.TaskSpawnerFromContext(ctx)
	if !ok {
		return "no spawner", nil
	}
	det := task.NewManualDetectorDetach(20 * time.Millisecond)
	spawner.Spawn(task.TaskSpec{Kind: "probe", Desc: "chain job"}, det)
	p.spawned <- det
	return "spawned", nil
}

// TestMultiLevelWindowCrossing 钉住 跨两级越窗的续跑链：首回合派生 A，A 的迟到结算驱动续跑并派生 B，B 的迟到结算再驱动一次续跑；所有答复必须按序到达同一条调用通道，且两次越窗结算都被消费后通道才关闭——续跑回合派生时丢失归属，就会让 B 的结算漂到共享总线、通道提前关闭。
func TestMultiLevelWindowCrossing(t *testing.T) {
	callCount := 0
	seq := &sequenceMockModel{
		callCount: &callCount,
		responses: []*model.Response{
			toolCallResponse("t1a", "spawn A"),
			finalTextResponse("t1b", "first answer"),
			toolCallResponse("t2a", "spawn B"),
			finalTextResponse("t2b", "second answer"),
			finalTextResponse("t3", "third answer"),
		},
	}
	spawned := make(chan *task.ManualDetector, 4)
	probe := &chainProbeTool{spawned: spawned}

	b, err := NewTagentAgent(&TagentConfig{
		Model: seq, SystemPrompt: "You are callee B.",
		Name: "callee-chain", Description: "c", MaxToolIterations: 5,
		Tools: []trpctool.Tool{probe},
	})
	require.NoError(t, err)
	defer b.Close()

	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("go")))
	ch, err := b.Run(context.Background(), inv)
	require.NoError(t, err)

	// Collect answers until the channel closes.
	var texts []string
	closed := make(chan struct{})
	go func() {
		for evt := range ch {
			if evt == nil || evt.Response == nil || len(evt.Response.Choices) == 0 {
				continue
			}
			m := evt.Response.Choices[len(evt.Response.Choices)-1].Message
			if m.Role == model.RoleAssistant && m.Content != "" && len(m.ToolCalls) == 0 {
				texts = append(texts, m.Content)
			}
		}
		close(closed)
	}()

	detA := <-spawned
	detA.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "A done"})
	detA.Done()

	select {
	case detB := <-spawned:
		detB.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "B done"})
		detB.Done()
	case <-time.After(5 * time.Second):
		t.Fatalf("continuation turn's own spawn (B) never appeared — attribution lost across hops; texts=%v", texts)
	}

	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatalf("channel did not close after both越窗 settles — barrier never quiesced; texts=%v", texts)
	}

	require.Equal(t, []string{"first answer", "second answer", "third answer"}, texts,
		"c.2: three successive same-loop turns (first + two continuations) must all reach the ONE call channel in order")
}

// TestFuncSettleDetector_DetachAfterDenseDuration 钉住 when fn runs longer than the dense duration, the detector signals detach (→ async ack).
func TestFuncSettleDetector_DetachAfterDenseDuration(t *testing.T) {
	d := task.NewFuncSettleDetector(context.Background(), func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}, 30*time.Millisecond)

	select {
	case <-d.Detached():
	case <-time.After(2 * time.Second):
		t.Fatal("expected detach after dense duration")
	}
	d.Cancel()
}

// TestFuncSettleDetector_NoDetachWhenSettledFirst 钉住 when fn returns within the dense duration, settle wins and detach never fires (timer cancelled).
func TestFuncSettleDetector_NoDetachWhenSettledFirst(t *testing.T) {
	d := task.NewFuncSettleDetector(context.Background(), func(context.Context) (string, error) {
		return "quick", nil
	}, 300*time.Millisecond)

	select {
	case sig := <-d.Settled():
		if sig.Output != "quick" {
			t.Errorf("settle output = %q, want quick", sig.Output)
		}
	case <-time.After(time.Second):
		t.Fatal("expected settle")
	}

	select {
	case <-d.Detached():
		t.Error("detach must not fire when the task settled first")
	case <-time.After(200 * time.Millisecond):
	}
}

// TestDetachAfter_StopPreventsFire 钉住 task.DetachAfter does not close when stop fires before the duration elapses.。
func TestDetachAfter_StopPreventsFire(t *testing.T) {
	stop := make(chan struct{})
	ch := task.DetachAfter(500*time.Millisecond, stop)
	close(stop)
	select {
	case <-ch:
		t.Error("task.DetachAfter should not fire after stop")
	case <-time.After(200 * time.Millisecond):
	}
}

// TestDetachAfter_FiresAfterDuration 钉住 task.DetachAfter closes after the duration when not stopped.。
func TestDetachAfter_FiresAfterDuration(t *testing.T) {
	ch := task.DetachAfter(20*time.Millisecond, make(chan struct{}))
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("task.DetachAfter should fire after the duration")
	}
}

// reentryChild is a named sub-agent that records how often it ran and answers
// with an instance-unique marker, so "WHICH generation's wrapper served this
// re-entry" is observable at the result, not by pointer comparison.
type reentryChild struct {
	mu      sync.Mutex
	runs    int
	name    string
	output  string
	gate    chan struct{}
	lastInv *trpcagent.Invocation
}

// armGate parks every subsequent run of this instance until the returned channel
// closes — the handle on 「这个重入还在途」 that the lease assertions need.
func (c *reentryChild) armGate() chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gate = make(chan struct{})
	return c.gate
}

func (c *reentryChild) last() *trpcagent.Invocation {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastInv
}

func newReentryChild(name, output string) *reentryChild {
	return &reentryChild{name: name, output: output}
}

func (c *reentryChild) Run(ctx context.Context, inv *trpcagent.Invocation) (<-chan *trpcEvent.Event, error) {
	c.mu.Lock()
	c.runs++
	c.lastInv = inv
	gate := c.gate
	c.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	ch := make(chan *trpcEvent.Event, 1)
	ch <- &trpcEvent.Event{Response: &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: c.output}}},
	}}
	close(ch)
	return ch, nil
}

func (c *reentryChild) Tools() []trpctool.Tool { return nil }
func (c *reentryChild) Info() trpcagent.Info {
	return trpcagent.Info{Name: c.name, Description: "reentry child"}
}
func (c *reentryChild) SubAgents() []trpcagent.Agent        { return nil }
func (c *reentryChild) FindSubAgent(string) trpcagent.Agent { return nil }
func (c *reentryChild) runCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.runs
}

// wrapper reentryWrapper mirrors what buildAgentDFS wires for every real delegation: the
// resident owner's cm (`SetParentCM`, 's handle for a re-entry with no
// initiator) plus the async window, long enough that the run settles INLINE.
func (h *reentryHarness) wrapper(cm *ContextManager, child *reentryChild) *AgentToolWrapper {
	w := NewAgentToolWrapper(child, "delegate to b", []string{"event_keys"}, nil)
	w.SetParentCM(cm)
	w.SetAsyncDenseDuration(h.dense)
	return w
}

// reentryDense is the sync-wait window of the harness wrappers. The default lets a
// run settle INLINE (so the spawned task reaches a legal re-entry state without any
// pumping); R2 shortens it to observe a re-entry still IN FLIGHT.
const reentryDense = 5 * time.Second

// reentryHarness publishes generations whose FACES carry distinct wrapper
// instances of the same declared target, then spawns a subagent task through the
// FIRST generation's wrapper — that task is the "存量任务" a later re-entry hits.
type reentryHarness struct {
	cm        *ContextManager
	spawner   *task.TaskManager
	turnG1    *ExecLease
	ctxG1     context.Context
	taskID    string
	spawnRuns int
	gen1      *leaseCountingRunner
	g1        *reentryChild
	g2        *reentryChild
	dense     time.Duration
}

func newReentryHarness(t *testing.T) *reentryHarness { return newReentryHarnessDense(t, reentryDense) }

func newReentryHarnessDense(t *testing.T, dense time.Duration) *reentryHarness {
	t.Helper()
	cm := &ContextManager{name: "d42", runner: &leaseCountingRunner{id: "boot"}}
	h := &reentryHarness{cm: cm, spawner: task.NewTaskManager(task.TaskManagerConfig{}), dense: dense}

	h.g1, h.g2 = newReentryChild("b", "SERVED-BY-G1"), newReentryChild("b", "SERVED-BY-G2")
	h.gen1 = &leaseCountingRunner{id: "g1"}
	cm.PublishExecutor(h.gen1, ContextManagerConfig{
		Name:  "d42-g1",
		Tools: []trpctool.Tool{h.wrapper(cm, h.g1)},
	})
	h.turnG1 = cm.AcquireLease(LeaseTurn)
	t.Cleanup(h.turnG1.Release)
	h.ctxG1 = h.turnG1.WithContext(task.WithTaskSpawner(context.Background(), h.spawner))

	w1 := cm.SubagentWrapper("b")
	require.NotNil(t, w1, "precondition: g1 routes b")
	out, err := w1.Call(h.ctxG1, subagentCallArgs(t))
	require.NoError(t, err)
	require.Equal(t, "SERVED-BY-G1", out, "the first delegation is served by g1's own target")

	h.spawnRuns = h.g1.runCount()
	require.Equal(t, 1, h.spawnRuns, "the first delegation ran g1's instance exactly once")
	tk := h.findSubagentTask(t)
	h.taskID = tk.ID
	require.NotNil(t, tk.Spec.Relaunch, "precondition: a subagent spawn installs a relaunch closure")
	require.NotNil(t, tk.Spec.ResumeFn, "…and a resume closure")
	return h
}

func (h *reentryHarness) findSubagentTask(t *testing.T) *task.Task {
	t.Helper()
	for _, tk := range h.spawner.List() {
		if tk.Spec.Kind == "subagent" {
			return tk
		}
	}
	t.Fatal("the delegation left no subagent task in the registry")
	return nil
}

// publishG2 replaces the effective face. tools=nil removes the target entirely.
func (h *reentryHarness) publishG2(child *reentryChild) {
	var tools []trpctool.Tool
	if child != nil {
		tools = append(tools, h.wrapper(h.cm, child))
	}
	h.cm.PublishExecutor(&leaseCountingRunner{id: "g2"}, ContextManagerConfig{Name: "d42-g2", Tools: tools})
}

// g1Refs is the diagnostic row of the generation the harness turn pinned — read
// from the lease itself, so no test hardcodes a binding sequence number. It is nil
// while that generation is still in force.
func (h *reentryHarness) g1Refs() *UnconvergedRef {
	id := h.turnG1.Generation()
	for i, u := range h.cm.UnconvergedRefs() {
		if u.Generation == id {
			return &h.cm.UnconvergedRefs()[i]
		}
	}
	return nil
}

// TestReentry_RelaunchRefusesTargetRemovedByNewGeneration 钉住 在编排仍路由到 b 时派生的任务，一旦生效代移除了该目标，重入就不得复活它——重入闭包若保留派生时的 wrapper，被移除的目标就会落在已不路由它的那一代上执行。
func TestReentry_RelaunchRefusesTargetRemovedByNewGeneration(t *testing.T) {
	h := newReentryHarness(t)
	h.publishG2(nil)
	require.Nil(t, h.cm.SubagentWrapper("b"), "precondition: the effective face really stopped routing b")

	before := h.g1.runCount()
	res, err := h.spawner.Relaunch(context.Background(), h.taskID)
	require.Error(t, err, "a re-entry whose selected generation lacks the target must be refused, got %+v", res)
	require.Contains(t, err.Error(), "b", "the refusal must name the missing target")
	require.Zero(t, h.g1.runCount()-before, "a refused re-entry must not run the retired target at all")
}

// TestReentry_RelaunchWithoutInitiatorTakesCurrentFace 钉住 没有发起方持有绑定时，重入按当前生效面解析。
// - 于是它跑的是当代的目标实例，而不是派生时捕获的那一个；
// - 两代都会路由到同一名字，只有实际被服务的实例能区分当前与捕获。
func TestReentry_RelaunchWithoutInitiatorTakesCurrentFace(t *testing.T) {
	h := newReentryHarness(t)
	h.publishG2(h.g2)

	res, err := h.spawner.Relaunch(context.Background(), h.taskID)
	require.NoError(t, err)
	require.True(t, res.Settled, "the re-spawn settles inside the dense window")
	require.Equal(t, "SERVED-BY-G2", res.Signal.Output,
		"a re-entry with no initiator must run the CURRENT generation's target, not the captured one")
	require.Equal(t, h.spawnRuns, h.g1.runCount(), "the spawn-time target instance must never be touched again")
}

// TestReentry_ResumeWithoutInitiatorTakesCurrentFace 钉住 is the same rule on the resume (送输入) entry: the resumed round's target comes from the current face.
func TestReentry_ResumeWithoutInitiatorTakesCurrentFace(t *testing.T) {
	h := newReentryHarness(t)
	h.publishG2(h.g2)

	res, err := h.spawner.Resume(context.Background(), h.taskID, "more work")
	require.NoError(t, err)
	require.True(t, res.Settled, "the resumed round settles inside the dense window")
	require.Equal(t, "SERVED-BY-G2", res.Signal.Output,
		"resume must re-enter through the CURRENT generation's target")
	require.Equal(t, h.spawnRuns, h.g1.runCount(), "and never through the captured one")
}

// TestReentry_InitiatorOnOlderGenerationKeepsItsOwnTarget 钉住 仍持旧一代租约的发起者重入时跑自己那一代的目标，即便生效面已将其移除。
// - 运行期间它要在自己那一代上取引用，被退役的那代因此不能在其脚下被回收。
func TestReentry_InitiatorOnOlderGenerationKeepsItsOwnTarget(t *testing.T) {
	h := newReentryHarnessDense(t, 30*time.Millisecond)
	gate := h.g1.armGate()
	h.publishG2(nil)
	require.Nil(t, h.cm.SubagentWrapper("b"), "precondition: the effective face removed b")

	res, err := h.spawner.Relaunch(h.ctxG1, h.taskID)
	require.NoError(t, err, "an initiator that holds G1 must not lose its own binding because G2 removed the target")
	require.False(t, res.Settled, "the re-entry is parked mid-run, which is what makes the pinning observable")
	require.Equal(t, "SERVED-BY-G1", h.g1.output, "the target that ran belongs to the initiator's generation")
	require.Equal(t, 2, h.g1.runCount(), "spawn plus this re-entry, and no other path re-ran it")

	un := h.g1Refs()
	require.NotNil(t, un, "G1 is retired and still referenced, so it must be on the unconverged list")
	require.Equal(t, 1, un.Refs[LeaseSubCall.String()],
		"the re-entry holds its OWN reference kind on the generation it resolved against")
	require.Zero(t, h.gen1.closed.Load(), "a running re-entry keeps its generation open")

	close(gate)
	require.Eventually(t, func() bool {
		u := h.g1Refs()
		return u == nil || u.Refs[LeaseSubCall.String()] == 0
	}, 5*time.Second, 10*time.Millisecond, "the reference drops exactly when the re-entered producer stops")
	require.Zero(t, h.gen1.closed.Load(), "the initiator's own turn reference still holds it")

	h.turnG1.Release()
	require.Eventually(t, func() bool { return h.gen1.closed.Load() == 1 }, 5*time.Second, 10*time.Millisecond,
		"and once the LAST holder is gone the generation is reclaimed, exactly once")
	require.Equal(t, int64(1), h.gen1.closed.Load())
}

// TestReentry_RefusedResumeLeavesTheTaskChainUntouched 钉住 被拒绝的重入不记录轮次、不消耗任何输入，任务链保持原样。
// - 一旦有代重新路由到目标，同一任务仍能以原有链条续跑；
// - 被拒后任务自身状态不变，因此第二次续跑仍被合法提供并能运行。
func TestReentry_RefusedResumeLeavesTheTaskChainUntouched(t *testing.T) {
	h := newReentryHarness(t)
	h.publishG2(nil)

	_, err := h.spawner.Resume(context.Background(), h.taskID, "phantom round")
	require.Error(t, err, "a resume whose selected generation lacks the target is refused")
	require.Zero(t, h.g2.runCount(), "and nothing runs on the way to that refusal")
	require.Equal(t, h.spawnRuns, h.g1.runCount(), "nor does it reach the spawn-time target")

	third := newReentryChild("b", "SERVED-BY-G3")
	h.publishG2(third)
	res, err := h.spawner.Resume(context.Background(), h.taskID, "continue")
	require.NoError(t, err, "the task itself stayed resumable across the refusal")
	require.True(t, res.Settled, "the resumed round ran inline")

	require.Equal(t, 1, third.runCount(), "the resumed round ran on the generation in force now")
	raw, ok := third.last().RunOptions.RuntimeState[ExternalContextKey].(json.RawMessage)
	require.True(t, ok, "the resumed run must receive the restored task-chain context")
	require.Equal(t, 1, strings.Count(string(raw), "〔本任务上一轮〕"),
		"a refused re-entry must not have appended a round (chain contents: %s)", raw)
	require.Contains(t, string(raw), "SERVED-BY-G1", "and the surviving round is the original settle result")
}

type echoObs struct {
	role, author, content string
	invocationID          string
	parentInvocationID    string
	isRoot                bool
	sessionID             string
}

type echoRecorder struct {
	mu   sync.Mutex
	seen []echoObs
}

func (rec *echoRecorder) Name() string { return "echo-recorder" }

func (rec *echoRecorder) Register(r *plugin.Registry) {
	r.OnEvent(func(ctx context.Context, inv *trpcagent.Invocation, e *trpcEvent.Event) (*trpcEvent.Event, error) {
		if e == nil || e.Response == nil || len(e.Response.Choices) == 0 {
			return e, nil
		}
		m := e.Response.Choices[0].Message
		isRoot := inv == nil || inv.GetParentInvocation() == nil
		sid := ""
		if inv != nil && inv.Session != nil {
			sid = inv.Session.ID
		}
		rec.mu.Lock()
		rec.seen = append(rec.seen, echoObs{
			role: string(m.Role), author: e.Author, content: m.Content,
			invocationID: e.InvocationID, parentInvocationID: e.ParentInvocationID,
			isRoot: isRoot, sessionID: sid,
		})
		rec.mu.Unlock()
		return e, nil
	})
}

func runEchoCapture(t *testing.T, userContent string) []echoObs {
	t.Helper()
	rec := &echoRecorder{}
	mockModel := &requestCapturingModel{resp: &model.Response{ID: "r", Done: true,
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "ok"}}}}}
	agt := llmagent.New("echo-agent", llmagent.WithModel(mockModel), llmagent.WithInstruction("test"))
	r := runner.NewRunner("echo-app", agt, runner.WithPlugins(rec))
	defer r.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, err := r.Run(ctx, "u1", "sess-1", model.NewUserMessage(userContent))
	require.NoError(t, err)
	for range ch {
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]echoObs(nil), rec.seen...)
}

// TestEchoGrounding_PluginHookReceivesInput 钉住 钉住 落盘输入是回路合成的合并消息：插件 OnEvent 必须收到 user 角色回声，且回声带 ParentInvocationID 以供根判定。
func TestEchoGrounding_PluginHookReceivesInput(t *testing.T) {
	const merged = "msg-A\n\n---\n\nmsg-B"
	seen := runEchoCapture(t, merged)
	for i, s := range seen {
		t.Logf("obs[%d] role=%q author=%q root=%v invID=%q parentID=%q content=%q",
			i, s.role, s.author, s.isRoot, s.invocationID, s.parentInvocationID, truncate(s.content, 40))
	}

	// A plugin MUST observe a user-role event for the input, else the whole dedup
	// premise is void.
	var userEcho *echoObs
	for i := range seen {
		if seen[i].role == string(model.RoleUser) {
			userEcho = &seen[i]
			break
		}
	}
	require.NotNil(t, userEcho,
		"no user-role input event reached the plugin OnEvent — §4.4 dedup premise would be void")

	require.True(t, userEcho.isRoot, "the input echo must be the ROOT invocation (empty parent) → root check is safe")
	require.Empty(t, userEcho.parentInvocationID, "echo ParentInvocationID empty → root detectable from the event too")
	require.Equal(t, merged, userEcho.content, "echo carries the exact merged input content → message-normalize match is safe")
	t.Logf("§4.4 GROUNDING: author=%q (predicate must key on fields that actually hold)", userEcho.author)
}

// callLog GROUNDING (test-only, zero product risk).  adds an execution-credential
// verify at the ACTUAL model entry ("未绑定/不匹配的执行凭据 MUST 在实际模型入口阻断") and
// requires model decorators to preserve the base IterModel capability. Both depend on
// facts about the real framework that must not be guessed:
//
//	(A) ORDERING — does MemoryPlugin's user-echo OnEvent fire BEFORE the model is
//
// entered? A model-entry gate is only safe if the  credential is already bound
// by then; otherwise every turn would false-block (catastrophic).
//
//	(B) ITERATOR PREFERENCE — when the base model implements model.IterModel, does the
//
// framework actually use GenerateContentIter (so decorators that omit it hide a
// real capability) or fall back to the channel path?
//
// This harness runs a genuine turn with a model that implements BOTH entry points and
// logs which is used, plus a plugin that logs when it sees the root user echo, and pins
// their relative order.
type callLog struct {
	mu  sync.Mutex
	seq []string
}

func (l *callLog) add(s string) {
	l.mu.Lock()
	l.seq = append(l.seq, s)
	l.mu.Unlock()
}

func (l *callLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.seq...)
}

// dualModel implements model.Model AND model.IterModel, logging which entry the
// framework invokes and whether the lazy iterator is created vs first-iterated.
type dualModel struct {
	log      *callLog
	resp     *model.Response
	iterMade atomic.Bool
	iterRan  atomic.Bool
}

func (m *dualModel) GenerateContent(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
	m.log.add("model:channel")
	ch := make(chan *model.Response, 1)
	ch <- m.resp
	close(ch)
	return ch, nil
}

func (m *dualModel) GenerateContentIter(_ context.Context, _ *model.Request) (model.Seq[*model.Response], error) {
	m.iterMade.Store(true)
	m.log.add("model:iter-created")
	return func(yield func(*model.Response) bool) {
		m.iterRan.Store(true)
		m.log.add("model:iter-first-next")
		yield(m.resp)
	}, nil
}

func (m *dualModel) Info() model.Info { return model.Info{Name: "dual"} }

// echoMarkPlugin logs the first root user echo it observes at OnEvent.
type echoMarkPlugin struct{ log *callLog }

func (echoMarkPlugin) Name() string { return "echo-mark" }

func (p echoMarkPlugin) Register(r *plugin.Registry) {
	r.OnEvent(func(_ context.Context, inv *trpcagent.Invocation, e *trpcEvent.Event) (*trpcEvent.Event, error) {
		if e == nil || e.Response == nil || len(e.Response.Choices) == 0 {
			return e, nil
		}
		if inv != nil && inv.GetParentInvocation() == nil &&
			e.Author == "user" && e.Response.Choices[0].Message.Role == model.RoleUser {
			p.log.add("plugin:user-echo")
		}
		return e, nil
	})
}

func TestModelEntryGrounding_OrderingAndIterator(t *testing.T) {
	log := &callLog{}
	m := &dualModel{log: log, resp: &model.Response{ID: "r", Done: true,
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "ok"}}}}}
	agt := llmagent.New("gate-agent", llmagent.WithModel(m), llmagent.WithInstruction("test"))
	r := runner.NewRunner("gate-app", agt, runner.WithPlugins(echoMarkPlugin{log}))
	defer r.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, err := r.Run(ctx, "u1", "sess-1", model.NewUserMessage("hello-input"))
	require.NoError(t, err)
	for range ch {
	}

	seq := log.snapshot()
	t.Logf("§4.5 GROUNDING call order: %v", seq)

	echoIdx := -1
	firstModelIdx := -1
	for i, s := range seq {
		if s == "plugin:user-echo" && echoIdx == -1 {
			echoIdx = i
		}
		switch s {
		case "model:channel", "model:iter-created", "model:iter-first-next":
			if firstModelIdx == -1 {
				firstModelIdx = i
			}
		}
	}
	require.NotEqual(t, -1, echoIdx, "the plugin must observe the root user echo at OnEvent")
	require.NotEqual(t, -1, firstModelIdx, "the model must be entered")

	require.Less(t, echoIdx, firstModelIdx,
		"§4.5(A): the input echo MUST reach the plugin before the model is entered, "+
			"otherwise a model-entry credential gate would false-block every turn")

	if m.iterMade.Load() {
		t.Logf("§4.5(B): framework PREFERRED the IterModel path (iter-created=%v iter-ran=%v)", m.iterMade.Load(), m.iterRan.Load())
	} else {
		t.Logf("§4.5(B): framework used the CHANNEL path even though the base is an IterModel " +
			"(decorators that omit GenerateContentIter do not currently lose an actively-used capability)")
	}
}

func imageOnlyEvent() *AgentEvent {
	return &AgentEvent{
		ID: "m", Type: tagentevent.TypeExternalInput, Source: "user", Timestamp: time.Now(),
		Message: &model.Message{Role: model.RoleUser, Content: "", ContentParts: []model.ContentPart{
			{Type: model.ContentTypeImage, Image: &model.Image{URL: "http://host/img.png"}}}},
		Metadata: map[string]any{},
	}
}

func TestBuildBusFact_FreezesMultimodalContentParts(t *testing.T) {
	cm := &ContextManager{partitionID: 1, memStore: memory.NewInMemoryStore(), projection: compress.NewSessionProjection()}
	fact := cm.buildBusFact(imageOnlyEvent())
	require.Len(t, fact.ContentParts, 1, "§4.3/§3.4: multimodal parts must be frozen onto the canonical fact")
	require.NotNil(t, fact.ContentParts[0].Image)
	require.Equal(t, "http://host/img.png", fact.ContentParts[0].Image.URL)
}

func TestBuildInvocation_KeepsImageOnlyInput(t *testing.T) {
	cm := &ContextManager{partitionID: 1}
	msg := cm.BuildInvocation([]*AgentEvent{imageOnlyEvent()})
	require.Empty(t, msg.Content, "image-only input has no text")
	require.NotEmpty(t, msg.ContentParts,
		"§4.3: an image-only input must NOT collapse to an empty invocation (else the loop's empty gate drops it)")
}

const chainPID = 7

type chainHarness struct {
	cm     *ContextManager
	store  *memory.InMemoryStore
	tm     *task.TaskManager
	settle chan *AgentEvent
}

func newChainHarness(t *testing.T) *chainHarness {
	t.Helper()
	store := memory.NewInMemoryStore()
	cm := &ContextManager{
		name:        "tagent",
		memStore:    store,
		projection:  compress.NewSessionProjection(),
		partitionID: chainPID,
	}
	sink := &taskRecordSink{cm: cm}
	settled := make(chan *AgentEvent, 16)
	tm := task.NewTaskManager(task.TaskManagerConfig{
		OnSettle: func(tk *task.Task, sig task.SettleSignal) {
			evt := newTaskSettledEvent(tk, sig, settleInlineCapChars, t.TempDir())
			cm.persistBusEvent(evt)
			settled <- evt
		},
		OnSpawn:        sink.onSpawn,
		OnInlineSettle: sink.onInlineSettle,
		OnCancel:       sink.onCancel,
	})
	return &chainHarness{cm: cm, store: store, tm: tm, settle: settled}
}

func (h *chainHarness) records(typ string) []memory.FullEvent {
	refs, err := h.store.QueryEvents(memory.QueryOptions{
		PartitionIDs: []int{chainPID}, EventTypes: []string{typ}, Limit: 200,
	})
	if err != nil {
		return nil
	}
	var out []memory.FullEvent
	for _, r := range refs {
		if ev, err := h.store.GetEvent(r.EventKey); err == nil && ev != nil {
			out = append(out, *ev)
		}
	}
	return out
}

func (h *chainHarness) nextSettle(t *testing.T) *AgentEvent {
	t.Helper()
	select {
	case evt := <-h.settle:
		return evt
	case <-time.After(3 * time.Second):
		t.Fatal("no background task_settled event arrived")
		return nil
	}
}

func (h *chainHarness) noSettleWithin(d time.Duration) bool {
	select {
	case <-h.settle:
		return false
	case <-time.After(d):
		return true
	}
}

// TestTaskChain_BackgroundSettleFullChain 钉住 命令任务从派走到可回收的结算走完整条链时，下游每一环都要认同同一任务标识与来源。
// - 记录事实、事件谱系、反馈裁决与注册表折叠四处必须一致，且折叠不得出幽灵项。
func TestTaskChain_BackgroundSettleFullChain(t *testing.T) {
	h := newChainHarness(t)

	spec := task.TaskSpec{
		Kind: "command", Desc: "build svc", Key: "chain-k1",
		Origin:      map[string]string{"trigger_source": "user", "chat_id": "c-42"},
		Declarative: &task.Declarative{Kind: "command", Desc: "build svc", Key: "chain-k1", Command: "make build", TaskID: "sess-1"},
	}
	det := task.NewFuncSettleDetector(context.Background(), func(context.Context) (string, error) {
		time.Sleep(40 * time.Millisecond)
		return "BUILD_OK_7788", nil
	}, time.Millisecond)

	res := h.tm.Spawn(spec, det)
	require.NotNil(t, res.Task)
	require.False(t, res.Settled, "slow fn must detach first (background settle)")
	require.False(t, res.Deduped)
	id := res.Task.ID

	// LINK 1 — task_spawned record carries declarative + Origin + lifetime.
	var spawned *memory.FullEvent
	for _, e := range h.records(tagentevent.TypeTaskSpawned) {
		if e.Metadata["task_id"] == id {
			rec := e
			spawned = &rec
		}
	}
	require.NotNil(t, spawned, "task_spawned fact record must exist")
	var decl task.Declarative
	require.NoError(t, json.Unmarshal([]byte(spawned.Content), &decl))
	require.Equal(t, "make build", decl.Command)
	require.Equal(t, "c-42", decl.Origin["chat_id"], "routing baggage persisted for cross-restart restore")
	require.Equal(t, task.LifetimeJob, decl.Lifetime, "command kind → job lifetime persisted")

	evt := h.nextSettle(t)
	require.Equal(t, SourceTask, evt.Source)
	require.Equal(t, "completed", evt.Metadata["settle_status"])
	require.Equal(t, id, evt.Metadata["task_id"])
	require.Equal(t, "c-42", evt.Metadata["chat_id"], "origin baggage copied onto the settle event")
	require.NotContains(t, evt.Metadata, "lineage_absent")
	require.Contains(t, evt.Message.Content, "[task settled]")
	require.Contains(t, evt.Message.Content, "BUILD_OK_7788")

	settledRec := false
	for _, e := range h.records(tagentevent.TypeExternalInput) {
		if e.Metadata["task_id"] == id && e.Metadata["settle_status"] == "completed" {
			settledRec = true
		}
	}
	require.True(t, settledRec, "background settle must persist an external_input record with settle_status for the registry")
	require.Len(t, h.records("feedback"), 1, "completed → exactly one feedback")
	require.Contains(t, h.records("feedback")[0].Content, `"verdict":"positive"`)

	restored := RebuildTaskRegistry(h.store, chainPID, task.NewTaskManager(task.TaskManagerConfig{}), nil)
	require.Zero(t, restored, "a terminal (completed) task must not resurrect as suspect")
}

// TestTaskChain_UnknownSourceIsHeld 钉住 无来源袋的派生结算成事件时必须显式标注谱系缺失。
// - 宿主投递门据此扣留它，而不是机械地按普通任务来源路由出去。
func TestTaskChain_UnknownSourceIsHeld(t *testing.T) {
	h := newChainHarness(t)
	spec := task.TaskSpec{
		Kind: "command", Desc: "orphan-ish", Key: "chain-unk",
		Declarative: &task.Declarative{Kind: "command", Desc: "orphan-ish", TaskID: "s-u"},
	}
	det := task.NewFuncSettleDetector(context.Background(), func(context.Context) (string, error) {
		time.Sleep(20 * time.Millisecond)
		return "done", nil
	}, time.Millisecond)
	res := h.tm.Spawn(spec, det)
	require.False(t, res.Settled)

	evt := h.nextSettle(t)
	require.Equal(t, "true", evt.Metadata["lineage_absent"], "unknown-origin settle must be flagged for host hold")
}

// TestTaskChain_InlineSettleIsRecordOnly 钉住 同步等待窗口内的内联结算只落记录，重启重放因此不出幽灵。
// - 它不发布结算事件，也不写反馈：模型在本回合里已看见这一结果。
func TestTaskChain_InlineSettleIsRecordOnly(t *testing.T) {
	h := newChainHarness(t)
	spec := task.TaskSpec{
		Kind: "command", Desc: "quick", Key: "chain-inline",
		Origin:      map[string]string{"trigger_source": "user"},
		Declarative: &task.Declarative{Kind: "command", Desc: "quick", TaskID: "s-i"},
	}
	det := task.NewFuncSettleDetector(context.Background(), func(context.Context) (string, error) {
		return "inline result", nil
	}, time.Hour)
	res := h.tm.Spawn(spec, det)
	require.True(t, res.Settled, "fast fn under a long dense window settles inline")
	require.Equal(t, "inline result", res.Signal.Output)
	id := res.Task.ID

	require.True(t, h.noSettleWithin(80*time.Millisecond), "inline settle must NOT publish a task_settled event")

	inlineRec := false
	for _, e := range h.records(tagentevent.TypeExternalInput) {
		if e.Metadata["task_id"] == id && e.Metadata[tagentevent.MetaKeyTaskInlineRecord] == "true" && e.Metadata["settle_status"] == "completed" {
			inlineRec = true
		}
	}
	require.True(t, inlineRec, "inline settle must write a registry inline record (no replay ghost)")
	require.Empty(t, h.records("feedback"), "inline settle binds no feedback (not reclaimed through the loop)")

	require.Zero(t, RebuildTaskRegistry(h.store, chainPID, task.NewTaskManager(task.TaskManagerConfig{}), nil))
}

// TestTaskChain_PostTerminalLateSignalIsFenced 钉住 任务到达终态之后才到的第二次结算，在观察信号入口处被丢弃。
// - 通知恰好一次：既不重复结算，也不刷屏。
func TestTaskChain_PostTerminalLateSignalIsFenced(t *testing.T) {
	h := newChainHarness(t)
	spec := task.TaskSpec{
		Kind: "command", Desc: "dup", Key: "chain-late",
		Origin:      map[string]string{"trigger_source": "user"},
		Declarative: &task.Declarative{Kind: "command", Desc: "dup", TaskID: "s-l"},
	}
	det := task.NewManualDetectorDetach(time.Millisecond)
	res := h.tm.Spawn(spec, det)
	require.False(t, res.Settled)

	det.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "first"})
	_ = h.nextSettle(t)

	det.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "second"})
	require.True(t, h.noSettleWithin(80*time.Millisecond), "post-terminal late signal must be fenced (dropped)")

	require.Len(t, h.records("feedback"), 1)
}

// TestTaskChain_ResumeRebindsUnderSameID 钉住 续跑的换绑语义：为已脱离任务装上新的探测器，并在同一任务标识下重跑结算生命周期。
// - 因此该标识会产生第二次被回收的结算。
func TestTaskChain_ResumeRebindsUnderSameID(t *testing.T) {
	h := newChainHarness(t)

	round2 := task.NewManualDetectorDetach(time.Millisecond)
	spec := task.TaskSpec{
		Kind: "command", Desc: "long svc", Key: "chain-resume",
		Origin:      map[string]string{"trigger_source": "user"},
		Declarative: &task.Declarative{Kind: "command", Desc: "long svc", TaskID: "s-r"},
		ResumeFn: func(context.Context, string) (task.SettleDetector, error) {
			return round2, nil
		},
	}
	det1 := task.NewManualDetectorDetach(time.Millisecond)
	res := h.tm.Spawn(spec, det1)
	require.False(t, res.Settled)
	id := res.Task.ID

	det1.Emit(task.SettleSignal{Kind: task.SettleStable, Output: "ready"})
	evt1 := h.nextSettle(t)
	require.Equal(t, "alive-detached", evt1.Metadata["settle_status"])
	require.Equal(t, id, evt1.Metadata["task_id"])
	svcTask, ok := h.tm.Get(id)
	require.True(t, ok)
	require.Equal(t, task.TaskAliveDetached, svcTask.Status())

	rr, err := h.tm.Resume(context.Background(), id, "continue please")
	require.NoError(t, err)
	require.NotNil(t, rr.Task)
	require.Equal(t, id, rr.Task.ID, "resume must rebind the round under the SAME task id")

	round2.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "final"})
	evt2 := h.nextSettle(t)
	require.Equal(t, id, evt2.Metadata["task_id"], "resumed round still attributes to the original id")
	require.Equal(t, "completed", evt2.Metadata["settle_status"])
	require.Contains(t, evt2.Message.Content, "final")
}

// TestNewTaskSettledEvent_Content 钉住 完成的结算产出自足的外部输入事件，携带描述、标识、状态与结果。
// - 小结果保持内联，本路径不启用转储。
func TestNewTaskSettledEvent_Content(t *testing.T) {
	tk := &task.Task{ID: "tk-abc", Spec: task.TaskSpec{Kind: "command", Desc: "npm run build"}}
	evt := newTaskSettledEvent(tk, task.SettleSignal{Kind: task.SettleCompleted, Output: "build ok"}, 0, "")

	if evt.Type != tagentevent.TypeExternalInput {
		t.Errorf("type = %s, want external_input", evt.Type)
	}
	if evt.Source != SourceTask {
		t.Errorf("source = %s, want %s", evt.Source, SourceTask)
	}
	if evt.Message == nil {
		t.Fatal("nil message")
	}
	for _, want := range []string{"npm run build", "tk-abc", "completed", "build ok"} {
		if !strings.Contains(evt.Message.Content, want) {
			t.Errorf("content missing %q: %s", want, evt.Message.Content)
		}
	}
}

// TestNewTaskSettledEvent_Failed 钉住 an error settle is reported as failed with the error text.
func TestNewTaskSettledEvent_Failed(t *testing.T) {
	tk := &task.Task{ID: "t2", Spec: task.TaskSpec{Desc: "bad cmd"}}
	evt := newTaskSettledEvent(tk, task.SettleSignal{Kind: task.SettleCompleted, Err: fmt.Errorf("boom")}, 0, "")
	if !strings.Contains(evt.Message.Content, "failed") || !strings.Contains(evt.Message.Content, "boom") {
		t.Errorf("failed event content = %q", evt.Message.Content)
	}
}

// TestNewTaskSettledEvent_LargeResultSpills 钉住 超长结果与同步路径的三件套对齐：正文写入工具输出目录，事件内容只留尾部加文件路径票据。
// - 事件体因此有界：召回它不会把超长结果重新注入，取全文走分页读取。
func TestNewTaskSettledEvent_LargeResultSpills(t *testing.T) {
	dir := t.TempDir()
	tk := &task.Task{ID: "t3-large", Spec: task.TaskSpec{Desc: "big"}}
	large := strings.Repeat("x", 5000)
	evt := newTaskSettledEvent(tk, task.SettleSignal{Kind: task.SettleCompleted, Output: large}, 1000, dir)
	content := evt.Message.Content

	if !strings.Contains(content, "output_spilled") || !strings.Contains(content, "已保存到:") {
		t.Errorf("oversized settle must carry the spill ticket, got: %s", truncateForTest(content, 200))
	}
	if strings.Count(content, "x") >= 5000 {
		t.Error("event Content must stay bounded (tail only, not the full body)")
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "task-t3-large-*.txt"))
	if len(matches) != 1 {
		t.Fatalf("expected one spilled file under %s, got %v", dir, matches)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil || string(data) != large {
		t.Errorf("spilled file must hold the full result (err=%v, len=%d)", err, len(data))
	}
	if !strings.Contains(content, matches[0]) {
		t.Errorf("notice ticket must carry the spilled file path %s", matches[0])
	}
}

// TestNewTaskSettledEvent_SpillDisabledInline 钉住 spillover disabled (maxChars=0)
func TestNewTaskSettledEvent_SpillDisabledInline(t *testing.T) {
	tk := &task.Task{ID: "t4", Spec: task.TaskSpec{Desc: "x"}}
	body := strings.Repeat("y", 3000)
	evt := newTaskSettledEvent(tk, task.SettleSignal{Kind: task.SettleCompleted, Output: body}, 0, "")
	if strings.Contains(evt.Message.Content, "output_spilled") || strings.Count(evt.Message.Content, "y") != 3000 {
		t.Error("spillover disabled must keep the result fully inline")
	}
}

func truncateForTest(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// TestTaskManager_BackgroundSettle_PublishesTaskSettled 钉住 结算回调接到总线上时，后台结算必须发出一条结算事件。
// - 该情形发生在同步等待窗口之后，走外发路径而非内联返回。
func TestTaskManager_BackgroundSettle_PublishesTaskSettled(t *testing.T) {
	bus := NewEventBus()
	tm := task.NewTaskManager(task.TaskManagerConfig{
		OnSettle: func(tk *task.Task, sig task.SettleSignal) {
			bus.Publish(newTaskSettledEvent(tk, sig, 0, ""))
		},
	})

	d := task.NewManualDetectorDetach(40 * time.Millisecond)
	res := tm.Spawn(task.TaskSpec{Kind: "command", Desc: "long task"}, d)
	if res.Settled {
		t.Fatalf("expected ack (background), got inline settle")
	}

	d.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "done later"})
	d.Done()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, e := range bus.TryPull() {
			if e.Source == SourceTask {
				if e.Message == nil || !strings.Contains(e.Message.Content, "done later") {
					t.Errorf("task_settled content = %v", e.Message)
				}
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no task_settled event published to bus")
}

// TestBuildInvocation_IncludesTaskSettled 钉住 a task_settled event (Source=tk)
func TestBuildInvocation_IncludesTaskSettled(t *testing.T) {
	cm := &ContextManager{}
	evt := newTaskSettledEvent(&task.Task{ID: "t1", Spec: task.TaskSpec{Desc: "npm run build"}},
		task.SettleSignal{Kind: task.SettleCompleted, Output: "build done"}, 0, "")

	msg := cm.BuildInvocation([]*AgentEvent{evt})
	if !strings.Contains(msg.Content, "npm run build") || !strings.Contains(msg.Content, "build done") {
		t.Errorf("task_settled content should be included in invocation, got: %q", msg.Content)
	}
}

// TestNewTaskSettledEvent_CarriesOrigin 钉住 结算事件携带任务的不透明来源袋（含会话标识等），并由既有的元数据提取管线读出，使被回收回合的产出按来源路由。
func TestNewTaskSettledEvent_CarriesOrigin(t *testing.T) {
	tk := &task.Task{ID: "t1", Spec: task.TaskSpec{Desc: "x", Origin: map[string]string{"chat_id": "u1", "user_name": "alice"}}}
	evt := newTaskSettledEvent(tk, task.SettleSignal{Kind: task.SettleCompleted, Output: "done"}, 0, "")
	if evt.Metadata["chat_id"] != "u1" {
		t.Errorf("settle event missing origin chat_id: %v", evt.Metadata)
	}
	md := extractRootMetadata([]*AgentEvent{evt})
	if md["chat_id"] != "u1" || md["user_name"] != "alice" {
		t.Errorf("extractRootMetadata should surface origin baggage, got %v", md)
	}
}

// TestNewTaskSettledEvent_NoOriginSafe 钉住 无来源袋的任务所产事件不携带路由元数据，但三个确定性事实必须在场。
// - 结算裁决供反馈路径读取；任务标识是机器可读的登记键，绝不从内容反解；
// - 谱系缺失标记让宿主扣住回收产出，而不走机械的 task 兜底投递；三者都不是路由袋。
func TestNewTaskSettledEvent_NoOriginSafe(t *testing.T) {
	tk := &task.Task{ID: "t2", Spec: task.TaskSpec{Desc: "x"}}
	evt := newTaskSettledEvent(tk, task.SettleSignal{Kind: task.SettleCompleted}, 0, "")
	for k := range evt.Metadata {
		if k != "settle_status" && k != "task_id" && k != "lineage_absent" {
			t.Errorf("no-origin task should carry no routing metadata, got key %q in %v", k, evt.Metadata)
		}
	}
	if evt.Metadata["settle_status"] != "completed" {
		t.Errorf("settle_status must always be carried (2.3 feedback path), got %v", evt.Metadata["settle_status"])
	}
	if evt.Metadata["task_id"] != "t2" {
		t.Errorf("task_id must always be carried (R2 registry key), got %v", evt.Metadata["task_id"])
	}
	if evt.Metadata["lineage_absent"] != "true" {
		t.Errorf("lineage_absent must be marked for Origin-less tasks (1.3 gate marker), got %v", evt.Metadata["lineage_absent"])
	}
}

// TestNewTaskSettledEvent_SingleLineTrajectory 钉住 结算正文用紧凑单线轨迹：不含内嵌换行、不写独立标识长行、按状态打标。
// - 单线不等于丢信息：描述、短标识、状态与结果都必须在场。
func TestNewTaskSettledEvent_SingleLineTrajectory(t *testing.T) {
	tk := &task.Task{ID: "abcd1234ef", Spec: task.TaskSpec{Desc: "并发抓取 4 篇资料进 knowledge_base"}}
	evt := newTaskSettledEvent(tk, task.SettleSignal{Kind: task.SettleCompleted, Output: "line1\nline2\nline3"}, 0, "")
	c := evt.Message.Content

	if strings.Contains(c, "\n") {
		t.Errorf("settle body must be single-line (newlines escaped), got: %q", c)
	}
	for _, want := range []string{"✓", "并发抓取", "id=abcd1234", "completed", "line1␤line2␤line3"} {
		if !strings.Contains(c, want) {
			t.Errorf("single-line settle missing %q: %q", want, c)
		}
	}
	if strings.Contains(c, "abcd1234ef\n") || strings.Contains(c, "\n\n") {
		t.Errorf("old multi-line layout leaked: %q", c)
	}
}

// TestNewTaskSettledEvent_StatusMarkers 钉住 failed / alive-detached / suspect map to distinct markers + status words (trajectory legibility).
func TestNewTaskSettledEvent_StatusMarkers(t *testing.T) {
	tk := &task.Task{ID: "m1", Spec: task.TaskSpec{Desc: "d"}}
	cases := []struct {
		name   string
		sig    task.SettleSignal
		marker string
		word   string
	}{
		{name: "failed", sig: task.SettleSignal{Err: fmt.Errorf("e")}, marker: "✗", word: "failed"},
		{name: "alive", sig: task.SettleSignal{Kind: task.SettleStable}, marker: "∞", word: "alive-detached"},
		{name: "suspect", sig: task.SettleSignal{Kind: task.SettleSuspect}, marker: "⚠", word: "suspect"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTaskSettledEvent(tk, tc.sig, 0, "").Message.Content
			if !strings.Contains(c, tc.marker) || !strings.Contains(c, tc.word) {
				t.Errorf("%s settle missing marker %q or word %q: %q", tc.name, tc.marker, tc.word, c)
			}
		})
	}
}

// TestNewTaskSettledEvent_InfoLosslessSpilled 钉住 超长的结算既不丢无损字段（描述、短标识、状态），也要带上转储票据。
// - 票据含路径与尾部预览，且与这些字段同处一行。
func TestNewTaskSettledEvent_InfoLosslessSpilled(t *testing.T) {
	dir := t.TempDir()
	tk := &task.Task{ID: "big-9999", Spec: task.TaskSpec{Desc: "长任务描述"}}
	large := strings.Repeat("z", 5000)
	c := newTaskSettledEvent(tk, task.SettleSignal{Kind: task.SettleCompleted, Output: large}, settleInlineCapChars, dir).Message.Content
	for _, want := range []string{"✓", "长任务描述", "id=big-9999", "completed", "output_spilled", "已保存到:", "尾部:"} {
		if !strings.Contains(c, want) {
			t.Errorf("spilled settle missing %q: %q", want, truncateForTest(c, 300))
		}
	}
	if strings.Contains(c, "\n") {
		t.Errorf("spilled settle must stay single-line, got: %q", truncateForTest(c, 200))
	}
}
