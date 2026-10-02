// 契约: docs/wiki/agent/task-lifecycle.md#finalize-lineage
//
// 有归属退役经 per-task 路由到达绑定总线并递减投递记账，父循环因此能静止退出。
package agent

import (
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
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
