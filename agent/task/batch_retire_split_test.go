// 契约: docs/wiki/agent/task-lifecycle.md#finalize-lineage
//
// 批量折叠的产生侧分流：有 delegation 归属的退役结算不进批汇总、走 per-task 路由；无归属条目折叠行为不变。
package task

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestBatchRetire_AttributedEntryEscapesBatch 钉住同波退役按归属分流。
// - 带 invocation 归属的条目绕过批折叠，按 per-task OnSettle 交付（父循环投递记账屏障靠它递减）。
// - 无归属条目才进汇总；汇总只发共享总线，父循环等不到。
func TestBatchRetire_AttributedEntryEscapesBatch(t *testing.T) {
	var mu sync.Mutex
	var batch []BatchRetired
	perSettle := map[string]SettleSignal{}
	tm := NewTaskManager(TaskManagerConfig{
		TerminalTTL: time.Minute,
		OnSettle: func(tk *Task, sig SettleSignal) {
			mu.Lock()
			perSettle[tk.ID] = sig
			mu.Unlock()
		},
		OnBatchRetire: func(b []BatchRetired) {
			mu.Lock()
			batch = append(batch, b...)
			mu.Unlock()
		},
	})
	base := time.Now()
	attributed := orphanSpec("k-attr", "sess-attr")
	attributed.Origin = map[string]string{originKeyInvocationID: "inv-A"}
	require.NotNil(t, tm.RestoreTask("attr", attributed, base.Add(-2*time.Hour), TaskSuspect))
	require.NotNil(t, tm.RestoreTask("plain", orphanSpec("k-plain", "sess-plain"), base.Add(-2*time.Hour), TaskSuspect))
	tm.now = func() time.Time { return base }

	require.Equal(t, 2, tm.RetireOrphans(func(string) bool { return false }))

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, batch, 1, "only the unattributed retire may collapse into the batch")
	require.Equal(t, "plain", batch[0].Task.ID)
	sig, ok := perSettle["attr"]
	require.True(t, ok, "the attributed retire must keep the per-task settle path")
	require.Equal(t, SettleFailed, sig.Kind)
	require.Equal(t, LineageRetired, sig.Lineage, "the routed retire settle still carries the retirement lineage")
}

// TestBatchRetire_UnattributedWaveStillCollapses 钉住无归属条目行为不变。
// - 整波退役全部进一次批汇总，per-task OnSettle 保持静默。
func TestBatchRetire_UnattributedWaveStillCollapses(t *testing.T) {
	var mu sync.Mutex
	var batch []BatchRetired
	perSettle := 0
	tm := NewTaskManager(TaskManagerConfig{
		TerminalTTL: time.Minute,
		OnSettle: func(_ *Task, _ SettleSignal) {
			mu.Lock()
			perSettle++
			mu.Unlock()
		},
		OnBatchRetire: func(b []BatchRetired) {
			mu.Lock()
			batch = append(batch, b...)
			mu.Unlock()
		},
	})
	base := time.Now()
	for i := 0; i < 3; i++ {
		require.NotNil(t, tm.RestoreTask(
			"p"+string(rune('0'+i)), orphanSpec("k", "sess-"+string(rune('0'+i))), base.Add(-2*time.Hour), TaskSuspect))
	}
	tm.now = func() time.Time { return base }

	require.Equal(t, 3, tm.RetireOrphans(func(string) bool { return false }))

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, batch, 3, "an unattributed wave still collapses into ONE OnBatchRetire")
	require.Zero(t, perSettle, "per-task OnSettle must stay suppressed for unattributed batch entries")
}
