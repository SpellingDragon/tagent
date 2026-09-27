package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/model"
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

// TestSubAgentRun_ToolResultStopsPrematurely proves the sub-agent single-turn
// semantics in Run() incorrectly treats a tool RESULT event (RoleTool, no
// tool_calls) as the "final response", terminating the sub-agent after the
// first tool call — before it can execute subsequent tool calls.
//
// Scenario (deterministic via sequenceMockModel):
//   - Model call 1 -> tool_call: action("openspec init")
//   - Model call 2 -> tool_call: action("openspec new change demo")
//   - Model call 3 -> final text "Plan created."
//
// Expected (correct behavior): 2 tool calls executed, final output "Plan created."
// Bug behavior: only 1 tool call executed; sub-agent returns the tool result
// of call 1 and never reaches call 2/3.
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

	// Sub-agent path: agent.Run() (same path AgentToolWrapper.Call uses).
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

// TestSubAgentRun_SlowLLM_ToolResultStops deterministically reproduces the
// production bug: with a SLOW LLM (delay > 500ms drain window), the sub-agent
// single-turn semantics stops right after the first tool RESULT event, because
// the drain timer expires before the next model round completes.
//
// This is the exact failure seen in wechat-bot logs: plan agent executes
// `openspec init` (round 1), then stops — never reaching `openspec new change`.
func TestSubAgentRun_SlowLLM_ToolResultStops(t *testing.T) {
	seqModel := &delayingSequenceModel{
		delay: 1500 * time.Millisecond, // > 500ms drain window
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

// TestSubAgentRun_RequestOrdering_UserAfterSystem verifies that a sub-agent's
// driving request stays at the FRONT of the timeline (right after system)
// across multiple ReAct iterations — instead of being buried at the end after
// the accumulated assistant/tool history.
//
// Reproduces the wechat-bot plan-agent ordering bug: the request was the
// framework's unprefixed invocation seed, treated as current-turn, and
// appended AFTER the projection history that accumulated during the turn.
// The fix persists the request into the projection at turn start.
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
	for range eventCh { // drain until turn completes
	}

	// Inspect the LAST request (final iteration — most accumulated history).
	last := seqModel.lastRequest()
	if len(last) < 3 {
		t.Fatalf("expected accumulated messages in final request, got %d: %+v", len(last), last)
	}

	// 1. First message is system.
	if last[0].Role != model.RoleSystem {
		t.Errorf("expected messages[0] to be system, got %s", last[0].Role)
	}

	// 2. The REQUEST appears exactly once (no duplication/drop). Under
	// pairing-free rendering (unified-event-projection D3) tool results are
	// ALSO role=user input events, so uniqueness is asserted on content.
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

	// 3. The user (request) is at the FRONT: right after system, and BEFORE
	//    the assistant/tool ReAct history. This is the core regression guard —
	//    the old bug placed the user at the END.
	if requestIdx != 1 {
		t.Errorf("❌ BUG: user request at index %d, want 1 (right after system). Order: %s",
			requestIdx, dumpRoles(last))
	}
	if requestIdx == len(last)-1 {
		t.Errorf("❌ BUG: user request is the LAST message (buried after ReAct history). Order: %s",
			dumpRoles(last))
	}
	// There must be ReAct history (assistant steps) AFTER the request.
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
