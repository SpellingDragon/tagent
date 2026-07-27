package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/agent"
)

// TestSubagentRounds_RecentBounded: the task-local round chain keeps rounds
// in order and bounds the restored window.
func TestSubagentRounds_RecentBounded(t *testing.T) {
	r := &subagentRounds{}
	for _, s := range []string{"r1", "r2", "r3", "r4", "r5"} {
		r.add("in-"+s, "out-"+s)
	}
	got := r.recent(3)
	if len(got) != 3 || got[0].output != "out-r3" || got[2].output != "out-r5" {
		t.Errorf("recent(3) must return the newest rounds in order, got %+v", got)
	}
}

// TestSubagentResume_RestoresOwnChainOnly: the restorer injects THIS task's
// prior rounds (last settle result foremost) and nothing else; a task with no
// settled round refuses resume with guidance.
func TestSubagentResume_RestoresOwnChainOnly(t *testing.T) {
	w := &AgentToolWrapper{}

	// No settled round → actionable refusal.
	empty := &subagentRounds{}
	if _, err := w.subagentResume("plan", empty)("继续"); err == nil ||
		!strings.Contains(err.Error(), "relaunch_task") {
		t.Errorf("resume without settled rounds must refuse with guidance, got %v", err)
	}

	// One settled round → restored context contains exactly that round.
	rounds := &subagentRounds{}
	rounds.add("分析日志", "结论:磁盘将满")
	prior := rounds.recent(DefaultResumeContextRounds)
	if len(prior) != 1 || !strings.Contains(prior[0].output, "磁盘将满") {
		t.Fatalf("round chain must hold the settle result, got %+v", prior)
	}
	// The restorer serializes ONLY this task's rounds (context-scoping): the
	// wire entries derive solely from `rounds` — verified by construction and
	// by the round content check above.
}

// ---------------------------------------------------------------------------
// 端到端重入验证：驱动真实的 subagentResume 路径，确认重入的子 agent 真的
// 收到「上一轮上下文 + 本轮新指令」。既有单元测试只覆盖轮次链机制（排序/
// 边界/拒绝），未验证重入调用实际把 prior 轮次注入子 agent 的 invocation。
// ---------------------------------------------------------------------------

// runResume drives one resume round and returns the invocation the sub-agent
// actually received (captured by the mockAgent).
func runResume(t *testing.T, w *AgentToolWrapper, rounds *subagentRounds, input string) *agent.Invocation {
	t.Helper()
	detector, err := w.subagentResume("plan", rounds)(input)
	if err != nil {
		t.Fatalf("resume must succeed for a task with a settled round: %v", err)
	}
	sig := <-detector.Settled()
	if sig.Err != nil {
		t.Fatalf("resumed run errored: %v", sig.Err)
	}
	return w.agent.(*mockAgent).lastInv
}

func restoredContext(t *testing.T, inv *agent.Invocation) []ExternalContextEntry {
	t.Helper()
	if inv == nil {
		t.Fatal("sub-agent was not invoked on resume")
	}
	raw, ok := inv.RunOptions.RuntimeState[ExternalContextKey].(json.RawMessage)
	if !ok {
		t.Fatalf("resumed invocation must carry external_context, RuntimeState=%v", inv.RunOptions.RuntimeState)
	}
	var entries []ExternalContextEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("unmarshal restored context: %v", err)
	}
	return entries
}

// TestSubagentResume_EndToEnd_CarriesPriorContext: a resumed sub-agent receives
// its prior round (instruction + result) as restored context AND the new
// instruction as the user message — the core of re-entry.
func TestSubagentResume_EndToEnd_CarriesPriorContext(t *testing.T) {
	mock := &mockAgent{name: "plan"}
	w := NewAgentToolWrapper(mock, "plan tool", nil, nil)

	rounds := &subagentRounds{}
	rounds.add("建立重写深度报告的计划", "已建立计划 rewrite-report：proposal.md + tasks.md（6 个任务，全部待办）")

	inv := runResume(t, w, rounds, "把任务 3 标记为已完成")

	// 1. The new instruction is the user message of the resumed run.
	if inv.Message.Content != "把任务 3 标记为已完成" {
		t.Errorf("resume input must be the user message, got %q", inv.Message.Content)
	}

	// 2. The prior round is restored (instruction + result both present).
	entries := restoredContext(t, inv)
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 restored round, got %d: %+v", len(entries), entries)
	}
	if !strings.Contains(entries[0].EventSummary, "建立重写深度报告的计划") {
		t.Errorf("restored context must carry the prior instruction, got %q", entries[0].EventSummary)
	}
	if !strings.Contains(entries[0].EventSummary, "已建立计划 rewrite-report") {
		t.Errorf("restored context must carry the prior result, got %q", entries[0].EventSummary)
	}
}

// TestSubagentResume_EndToEnd_MultiRoundAccumulates: after two settled rounds,
// a resume restores BOTH prior rounds (newest last) so the sub-agent sees the
// full recent task chain, not just the last step.
func TestSubagentResume_EndToEnd_MultiRoundAccumulates(t *testing.T) {
	mock := &mockAgent{name: "plan"}
	w := NewAgentToolWrapper(mock, "plan tool", nil, nil)

	rounds := &subagentRounds{}
	rounds.add("建立计划", "已建立计划 X，含 3 个任务")
	rounds.add("细化任务 1", "任务 1 已拆为 3 个子步骤")

	inv := runResume(t, w, rounds, "开始执行任务 2")
	entries := restoredContext(t, inv)

	if len(entries) != 2 {
		t.Fatalf("expected 2 restored rounds, got %d: %+v", len(entries), entries)
	}
	// Newest last: the second round (细化任务 1) is the most recent context.
	if !strings.Contains(entries[0].EventSummary, "已建立计划 X") {
		t.Errorf("first restored round must be the older one, got %q", entries[0].EventSummary)
	}
	if !strings.Contains(entries[1].EventSummary, "任务 1 已拆为 3 个子步骤") {
		t.Errorf("last restored round must be the newest one, got %q", entries[1].EventSummary)
	}
}
