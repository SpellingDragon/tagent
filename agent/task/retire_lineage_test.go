// 本文件钉住退役谱系的信号化：退役只盖在 SettleSignal 上，Spec.Origin 作为 spawn 时
// 谱系身份保持不可变。
package task

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestRetireNeverMutatesOrigin 钉住 退役（TTL/僵尸/孤儿）不再改写 Spec.Origin。修前
// finalizeRetired 持 t.mu 写 Origin["trigger_source"]="task-retired"，而事件构造侧
// （agent.newTaskSettledEvent）无锁读同一 map——既是数据竞争，又永久污染 resumed
// 任务后续结算的谱系。修后退役归因只出现在信号的 Lineage 字段上。
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

	// Concurrent unprivileged reader: exactly the shape of the event builder
	// reading tk.Spec.Origin with no lock. Under -race the old Origin-writing
	// retirement fails here.
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

	// Concurrent watch settles (state-neutral signals, same second stage the
	// watch loop runs) interleaved with the retirement.
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
