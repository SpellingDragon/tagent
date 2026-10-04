// 契约: docs/wiki/agent/task-lifecycle.md#finalize-lineage
//
// 收尾谱系与循环静止：有归属退役经 per-task 路由到达绑定总线并递减投递记账，父循环因此能静止退出；
// 结算事件谱系取值优先级：信号级 Lineage > spawn 时 Origin，Origin 不因退役被改写。
package agent

import (
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/stretchr/testify/require"
)

// TestBatchRetire_AttributedSettleQuietsBoundLoop 钉住分流后的端到端形状。
// - 绑定总线 + 一次 spawn 记账：孤儿裁决回收有归属退役后，结算经 OnSettle 路由进绑定总线。
// - pending 递减归零（awaiting 判假 = 循环可退出），批汇总零条目。
func TestBatchRetire_AttributedSettleQuietsBoundLoop(t *testing.T) {
	sinks := newSettleSinkRegistry()
	fallback := NewEventBus()
	invBus := NewEventBus()
	sinks.bind("inv-Q", invBus)
	sinks.noteSpawn("inv-Q")

	batched := 0
	tm := task.NewTaskManager(task.TaskManagerConfig{
		TerminalTTL: time.Minute,
		OnSettle: func(tk *task.Task, sig task.SettleSignal) {
			deliverTaskSettled(sinks, fallback, tk, newTaskSettledEvent(tk, sig, 0, ""))
		},
		OnBatchRetire: func(b []task.BatchRetired) { batched += len(b) },
	})

	spec := task.TaskSpec{
		Kind: "subagent", Desc: "orphan under delegation", Key: "kq",
		Declarative: &task.Declarative{Kind: "subagent", TaskID: "sess-q"},
		Origin:      map[string]string{"invocation_id": "inv-Q"},
	}
	require.NotNil(t, tm.RestoreTask("q1", spec, time.Now().Add(-2*time.Hour), task.TaskSuspect))
	require.Equal(t, 1, tm.RetireOrphans(func(string) bool { return false }))

	deadline := time.Now().Add(2 * time.Second)
	var settled []*AgentEvent
	for time.Now().Before(deadline) {
		if got := invBus.TryPull(); len(got) > 0 {
			settled = got
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.NotEmpty(t, settled, "the attributed retire must land on the bound invocation bus")
	require.Zero(t, batched, "an attributed retire must never reach the batch summary")
	require.False(t, sinks.awaiting("inv-Q"), "the delivered settle decrements the barrier — the loop can quiesce")

	sinks.unbind("inv-Q")
	require.Empty(t, fallback.TryPull(), "nothing hit the fallback while the bus was bound")
	require.NotNil(t, tm.RestoreTask("q2", task.TaskSpec{
		Kind: "subagent", Desc: "late orphan", Key: "kq2",
		Declarative: &task.Declarative{Kind: "subagent", TaskID: "sess-q2"},
		Origin:      map[string]string{"invocation_id": "inv-Q"},
	}, time.Now().Add(-2*time.Hour), task.TaskSuspect))
	require.Equal(t, 1, tm.RetireOrphans(func(string) bool { return false }))
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(fallback.TryPull()) > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("post-unbind attributed retire must fall back to the shared bus")
}

// TestResumeSettleKeepsOriginalLineage 钉住谱系取值优先级与 Origin 不可变性。
// - 退役结算只在信号上盖谱系；Spec.Origin 保持 spawn 原值，resume 后的结算继承原值。
// - 事件取值：信号优先，无信号回落 Origin。
func TestResumeSettleKeepsOriginalLineage(t *testing.T) {
	tk := &task.Task{Spec: task.TaskSpec{
		Kind: "command", Desc: "job",
		Origin: map[string]string{tagentevent.MetaKeyTriggerSource: "host-user"},
	}}

	retired := newTaskSettledEvent(tk, task.SettleSignal{
		Kind: task.SettleFailed, Output: "ttl-expired", Lineage: task.LineageRetired,
	}, 0, "")
	require.Equal(t, task.LineageRetired, retired.Metadata[tagentevent.MetaKeyTriggerSource],
		"the retirement settle attributes lineage from the signal")
	require.Equal(t, "host-user", tk.Spec.Origin[tagentevent.MetaKeyTriggerSource],
		"a retired settle leaves Origin untouched for future resumes")

	resumed := newTaskSettledEvent(tk, task.SettleSignal{
		Kind: task.SettleCompleted, Output: "done after resume",
	}, 0, "")
	require.Equal(t, "host-user", resumed.Metadata[tagentevent.MetaKeyTriggerSource],
		"a post-resume settle without signal lineage keeps the original provenance")
}
