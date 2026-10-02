// 契约: docs/wiki/agent/task-lifecycle.md#finalize-lineage
//
// 退役谱系的信号化：退役只盖在 SettleSignal 上，Spec.Origin 作为 spawn 时谱系身份保持不可变。
package task

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestRetireNeverMutatesOrigin 钉住退役（TTL/僵尸/孤儿）不改写 Spec.Origin。
// - 退役归因只出现在信号的 Lineage 字段；Origin 是 spawn 时谱系身份，保持不可变。
// - 事件构造侧无锁读 Origin：与退役并发时不得数据竞争（-race），也不得污染 resumed 任务的后续谱系。
func TestRetireNeverMutatesOrigin(t *testing.T) {
	var mu sync.Mutex
	var sigs []SettleSignal
	tm := NewTaskManager(TaskManagerConfig{
		OnSettle: func(tk *Task, sig SettleSignal) {
			mu.Lock()
			sigs = append(sigs, sig)
			mu.Unlock()
		},
	})
	d := NewManualDetectorDetach(10 * time.Millisecond)
	res := tm.Spawn(
		TaskSpec{Kind: "command", Desc: "watched job", Origin: map[string]string{"trigger_source": "host-user"}},
		d,
	)
	require.False(t, res.Settled, "expected a background (detached) spawn")
	tk := res.Task

	stop := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = tk.Spec.Origin["trigger_source"]
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			select {
			case <-stop:
				return
			default:
				tm.emitBackground(tk, SettleSignal{Kind: SettleWatch, Output: "peek"})
			}
		}
	}()

	tm.finalizeRetired(tk, "(ttl-expired: retired)", nil)
	close(stop)
	wg.Wait()
	d.Done()

	require.Equal(t, "host-user", tk.Spec.Origin["trigger_source"],
		"retirement must not rewrite spawn-time provenance")

	mu.Lock()
	defer mu.Unlock()
	var retired *SettleSignal
	for i := range sigs {
		if sigs[i].Lineage == LineageRetired {
			retired = &sigs[i]
		}
	}
	require.NotNil(t, retired, "the retirement settle carries its lineage on the signal")
	require.Equal(t, SettleFailed, retired.Kind)
}

// TestReconcileTTL_RestoredTaskWarnsBeforeRetire 钉住恢复任务的静默泄漏告警通道。
// - 恢复任务（detector 恒 nil）被 TTL 退役时，退役结算事件自身携带"后台会话可能仍在运行"告警。
// - 一个可观测事件承载：不另发旁路通知，批折叠的排他性不被破坏。
// - 有探测器的任务退役不带恢复告警（Cancel 已接管回收）。
func TestReconcileTTL_RestoredTaskWarnsBeforeRetire(t *testing.T) {
	var mu sync.Mutex
	var sigs []SettleSignal
	tm := NewTaskManager(TaskManagerConfig{
		OnSettle: func(tk *Task, sig SettleSignal) {
			mu.Lock()
			sigs = append(sigs, sig)
			mu.Unlock()
		},
	})
	base := time.Now()
	spec := orphanSpec("kw", "sess-w")
	spec.TTL = time.Minute
	require.NotNil(t, tm.RestoreTask("w1", spec, base.Add(-2*time.Hour), TaskRunning))
	tm.now = func() time.Time { return base }

	tm.reconcileTTL()

	mu.Lock()
	wave1 := append([]SettleSignal(nil), sigs...)
	sigs = nil
	mu.Unlock()
	require.Len(t, wave1, 1, "the retire produces exactly one settle event (warning rides along)")
	require.Equal(t, LineageRetired, wave1[0].Lineage)
	require.Contains(t, wave1[0].Output, "sess-w", "the warning names the possibly-running backing session")
	require.Contains(t, wave1[0].Output, "可能仍在运行")

	det := NewManualDetector()
	tkLive := &Task{ID: "live-t", Spec: TaskSpec{Desc: "live", TTL: time.Minute}, detector: det}
	tkLive.status = TaskRunning
	tkLive.StartedAt = base.Add(-2 * time.Hour)
	tm.tasks["live-t"] = tkLive
	tm.reconcileTTL()

	mu.Lock()
	wave2 := append([]SettleSignal(nil), sigs...)
	mu.Unlock()
	require.Len(t, wave2, 1)
	require.NotContains(t, wave2[0].Output, "可能仍在运行", "a detector-carrying retire cancels instead of warning")
}
