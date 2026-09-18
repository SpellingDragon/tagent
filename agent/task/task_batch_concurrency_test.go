package task

// 3.9 独立审查 🟠#1 回归锁定：finalize() 向共享的 batchCollect collector 追加；
// 并发 reconcile（turn renderBoard + 后台冥想 + 工具 List()）可同时抵达此处。
// 修复前：collector 在 tm.mu 之外被 append → collector 切片数据竞争 + 结算可能丢失。
// 本测在单个 batch 窗口内并发驱动 N 次 finalize，-race 下须干净且 N 条全部落账。

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestBatchRetire_ConcurrentFinalizeNoRace(t *testing.T) {
	const n = 64
	var (
		delivered []BatchRetired
		mu        sync.Mutex
		settleHit int
	)
	tm := NewTaskManager(TaskManagerConfig{
		OnSettle: func(tk *Task, sig SettleSignal) {
			mu.Lock()
			settleHit++
			mu.Unlock()
		},
		OnBatchRetire: func(b []BatchRetired) {
			mu.Lock()
			delivered = append(delivered, b...)
			mu.Unlock()
		},
	})
	base := time.Now()
	tm.now = func() time.Time { return base }

	tasks := make([]*Task, n)
	for i := 0; i < n; i++ {
		tasks[i] = tm.RestoreTask(fmt.Sprintf("c%d", i), orphanSpec(fmt.Sprintf("ck%d", i), "concurrent"), base, TaskSuspect)
		if tasks[i] == nil {
			t.Fatalf("RestoreTask %d nil", i)
		}
	}

	finish := tm.beginBatchRetire() // onBatchRetire != nil → batchCollect 置位
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(tk *Task) {
			defer wg.Done()
			tm.finalize(tk, SettleCompleted, "ok", nil)
		}(tasks[i])
	}
	wg.Wait()
	finish()

	mu.Lock()
	defer mu.Unlock()
	if settleHit != 0 {
		t.Fatalf("batch mode must suppress per-task OnSettle, got %d", settleHit)
	}
	if len(delivered) != n {
		t.Fatalf("delivered batch = %d, want %d（有结算被丢弃）", len(delivered), n)
	}
	for _, r := range delivered {
		if r.Task.Status() != TaskCompleted {
			t.Fatalf("delivered task %s status = %v, want completed", r.Task.ID, r.Task.Status())
		}
	}
}
