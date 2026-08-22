package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// ExtraParams: declaration + JSON message packing (plan-interaction-contract D2)
// ============================================================================

func planExtraParams() []ExtraParam {
	return []ExtraParam{
		{Name: "action", Enum: []string{"create", "update", "archive", "progress"},
			Description: "操作类型"},
		{Name: "name", Description: "计划名(kebab-case)"},
	}
}

// TestExtraParams_Declaration: declared params land in InputSchema with enum;
// reserved names are never shadowed.
func TestExtraParams_Declaration(t *testing.T) {
	wrapper := NewAgentToolWrapper(&mockAgent{name: "plan"}, "plan tool", nil, nil)
	wrapper.SetExtraParams(append(planExtraParams(),
		ExtraParam{Name: "request", Description: "must not shadow"},
		ExtraParam{Name: "event_keys", Description: "must not shadow"},
	))

	decl := wrapper.Declaration()
	actionSchema, ok := decl.InputSchema.Properties["action"]
	require.True(t, ok, "action must be declared")
	assert.Equal(t, "string", actionSchema.Type)
	assert.Len(t, actionSchema.Enum, 4)

	nameSchema, ok := decl.InputSchema.Properties["name"]
	require.True(t, ok, "name must be declared")
	assert.Equal(t, "string", nameSchema.Type)

	// Reserved built-ins keep their canonical definitions.
	assert.Equal(t, "The request or instruction to process",
		decl.InputSchema.Properties["request"].Description)
	_, hasEventKeys := decl.InputSchema.Properties["event_keys"]
	assert.False(t, hasEventKeys, "event_keys only appears via eventParams, not extra_params")
}

// TestExtraParams_CallPacksJSONBody: present extra params are packed with
// request into a JSON message body the sub-agent can parse.
func TestExtraParams_CallPacksJSONBody(t *testing.T) {
	mockAg := &mockAgent{name: "plan"}
	wrapper := NewAgentToolWrapper(mockAg, "plan tool", nil, nil)
	wrapper.SetExtraParams(planExtraParams())

	_, err := wrapper.Call(context.Background(),
		[]byte(`{"action":"progress","name":"my-plan","request":"查看进度"}`))
	require.NoError(t, err)
	require.NotNil(t, mockAg.lastInv)

	var fields map[string]any
	require.NoError(t, json.Unmarshal([]byte(mockAg.lastInv.Message.Content), &fields),
		"message body must be JSON when extra params are present: %q", mockAg.lastInv.Message.Content)
	assert.Equal(t, "progress", fields["action"])
	assert.Equal(t, "my-plan", fields["name"])
	assert.Equal(t, "查看进度", fields["request"])
}

// TestExtraParams_AbsentParamsKeepPlainText: declared but not passed → the
// message body stays plain-text request.
func TestExtraParams_AbsentParamsKeepPlainText(t *testing.T) {
	mockAg := &mockAgent{name: "plan"}
	wrapper := NewAgentToolWrapper(mockAg, "plan tool", nil, nil)
	wrapper.SetExtraParams(planExtraParams())

	_, err := wrapper.Call(context.Background(), []byte(`{"request":"纯文本请求"}`))
	require.NoError(t, err)
	require.NotNil(t, mockAg.lastInv)
	assert.Equal(t, "纯文本请求", mockAg.lastInv.Message.Content)
}

// TestExtraParams_UndeclaredWrapperUnchanged: wrappers without extra_params
// ignore stray fields — behavior identical to before (regression guard).
func TestExtraParams_UndeclaredWrapperUnchanged(t *testing.T) {
	mockAg := &mockAgent{name: "knowledge"}
	wrapper := NewAgentToolWrapper(mockAg, "knowledge tool", nil, nil)

	_, err := wrapper.Call(context.Background(),
		[]byte(`{"action":"progress","request":"do work"}`))
	require.NoError(t, err)
	require.NotNil(t, mockAg.lastInv)
	assert.Equal(t, "do work", mockAg.lastInv.Message.Content,
		"undeclared wrapper must keep plain-text request")
}

// TestExtraParams_NumberPrecision: numeric extra params survive packing with
// full precision (args are decoded with json.Number).
func TestExtraParams_NumberPrecision(t *testing.T) {
	mockAg := &mockAgent{name: "plan"}
	wrapper := NewAgentToolWrapper(mockAg, "plan tool", nil, nil)
	wrapper.SetExtraParams([]ExtraParam{{Name: "budget", Type: "number"}})

	const big = "1297371431025250304"
	_, err := wrapper.Call(context.Background(),
		[]byte(`{"budget":`+big+`,"request":"r"}`))
	require.NoError(t, err)
	assert.Contains(t, mockAg.lastInv.Message.Content, big,
		"int64-scale numbers must not lose precision through packing")
}

// ============================================================================
// Same-name single-flight (plan-interaction-contract D4)
// ============================================================================

// slowAgent blocks until released — keeps the first task in flight while a
// concurrent same-name call arrives.
type slowAgent struct {
	name    string
	release chan struct{}
}

func (s *slowAgent) Run(ctx context.Context, inv *agent.Invocation) (<-chan *event.Event, error) {
	ch := make(chan *event.Event)
	go func() {
		select {
		case <-s.release:
		case <-ctx.Done():
		}
		close(ch)
	}()
	return ch, nil
}

func (s *slowAgent) Tools() []trpctool.Tool               { return nil }
func (s *slowAgent) Info() agent.Info                     { return agent.Info{Name: s.name} }
func (s *slowAgent) SubAgents() []agent.Agent             { return nil }
func (s *slowAgent) FindSubAgent(name string) agent.Agent { return nil }

// TestSameNameSingleFlight: two concurrent calls carrying the same `name`
// track exactly ONE task; the loser gets the existing task id plus settle
// guidance instead of a second tracked run.
//
// NOTE(framework semantics): FuncSettleDetector starts its fn at CREATION,
// and Spawn's dedup cancels the duplicate detector afterwards — so the
// duplicate's agent.Run MAY briefly start before cancellation lands
// (pre-existing dedup behavior, same for request-keyed dedup). The contract
// asserted here is the task-layer one: one tracked task, duplicate
// cancelled, caller redirected. Do NOT assert on raw Run invocation counts
// (scheduling-dependent → flaky).
func TestSameNameSingleFlight(t *testing.T) {
	slow := &slowAgent{name: "plan", release: make(chan struct{})}
	defer close(slow.release)

	wrapper := NewAgentToolWrapper(slow, "plan tool", nil, nil)
	wrapper.SetExtraParams(planExtraParams())
	wrapper.SetAsyncDenseDuration(50 * time.Millisecond) // fast detach → ack

	tm := task.NewTaskManager(task.TaskManagerConfig{})
	ctx := task.WithTaskSpawner(context.Background(), tm)

	// First call: acks (slow agent keeps it running).
	res1, err := wrapper.Call(ctx, []byte(`{"action":"update","name":"same-plan","request":"第一次"}`))
	require.NoError(t, err)
	require.Contains(t, fmt.Sprint(res1), "后台运行", "first call should ack")

	// Second call with the SAME name but different request: deduped.
	res2, err := wrapper.Call(ctx, []byte(`{"action":"update","name":"same-plan","request":"第二次"}`))
	require.NoError(t, err)
	assert.Contains(t, fmt.Sprint(res2), "同名计划任务已在运行", "same-name call must dedup")
	assert.Contains(t, fmt.Sprint(res2), "task_settled", "loser is told to wait for settle first")
	assert.Contains(t, fmt.Sprint(res2), "不要重复发起同名调用", "loser is told not to re-spawn")
	assert.NotContains(t, fmt.Sprint(res2), "resume_task", "dedup notice must stay ticket-only (no tool-name teaching)")
	assert.NotContains(t, fmt.Sprint(res2), "get_task_result", "dedup notice must stay ticket-only (no tool-name teaching)")

	assert.Len(t, tm.List(), 1, "exactly one task tracked for the same plan name")
}

// TestDifferentNameNoDedup: different names spawn independent tasks
// (multi-plan parallel is a legal scenario).
func TestDifferentNameNoDedup(t *testing.T) {
	slow := &slowAgent{name: "plan", release: make(chan struct{})}
	defer close(slow.release)

	wrapper := NewAgentToolWrapper(slow, "plan tool", nil, nil)
	wrapper.SetExtraParams(planExtraParams())
	wrapper.SetAsyncDenseDuration(50 * time.Millisecond)

	tm := task.NewTaskManager(task.TaskManagerConfig{})
	ctx := task.WithTaskSpawner(context.Background(), tm)

	_, err := wrapper.Call(ctx, []byte(`{"action":"update","name":"plan-a","request":"r"}`))
	require.NoError(t, err)
	res2, err := wrapper.Call(ctx, []byte(`{"action":"update","name":"plan-b","request":"r"}`))
	require.NoError(t, err)
	assert.NotContains(t, fmt.Sprint(res2), "同名计划任务已在运行")
	assert.Len(t, tm.List(), 2, "different plan names run in parallel")
}

// TestRelaunchKeepsNameKey: a relaunched sub-agent task keeps the name-based
// idempotency key of the original spawn — D4 single-flight covers relaunch
// rounds too (code-review Minor-2 regression).
func TestRelaunchKeepsNameKey(t *testing.T) {
	mockAg := &mockAgent{name: "plan"} // settles immediately
	wrapper := NewAgentToolWrapper(mockAg, "plan tool", nil, nil)
	wrapper.SetExtraParams(planExtraParams())

	tm := task.NewTaskManager(task.TaskManagerConfig{})
	ctx := task.WithTaskSpawner(context.Background(), tm)

	_, err := wrapper.Call(ctx, []byte(`{"action":"update","name":"keyed-plan","request":"第一轮"}`))
	require.NoError(t, err)

	tasks := tm.List()
	require.Len(t, tasks, 1)
	orig := tasks[0]
	assert.Equal(t, "plan:keyed-plan", orig.Spec.Key, "initial spawn keys by name")
	require.NotNil(t, orig.Spec.Relaunch, "subagent task must be relaunchable")

	res, err := tm.Relaunch(orig.ID)
	require.NoError(t, err)
	assert.Equal(t, "plan:keyed-plan", res.Task.Spec.Key,
		"relaunched task must keep the name-based key, not fall back to request text")
}
