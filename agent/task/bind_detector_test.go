package task

import (
	"sync"
	"testing"
	"time"
)

// hardening-review-batch2 3.1/3.4：BindDetector——重挂 detector 绑定到恢复
// 任务后，watch/settle 信号必须真实到达 TaskManager（「tracked=true」不再
// 是充分证据）；终态任务拒绝绑定（fencing）。
func TestBindDetector_SignalsReachManager(t *testing.T) {
	var mu sync.Mutex
	var settles []SettleSignal
	tm := NewTaskManager(TaskManagerConfig{OnSettle: func(_ *Task, sig SettleSignal) {
		mu.Lock()
		settles = append(settles, sig)
		mu.Unlock()
	}})
	tk := tm.RestoreTask("t-bind", TaskSpec{
		Kind: "command", Desc: "restored",
		Alive:       func() bool { return true },
		Declarative: &Declarative{Kind: "command", TaskID: "n-x"},
	}, time.Now(), TaskSuspect)
	tm.MarkTaskRunning("t-bind")

	d := NewManualDetector()
	if err := tm.BindDetector("t-bind", d); err != nil {
		t.Fatalf("bind: %v", err)
	}
	// 模拟重挂会话的真实输出：稳定 → alive_detached 通知（信号可达）。
	d.Emit(SettleSignal{Kind: SettleStable, Output: "ready"})
	waitUntil(t, time.Second, func() bool { return tk.Status() == TaskAliveDetached })

	// 随后真实完成：终态结算可达。
	d.Emit(SettleSignal{Kind: SettleCompleted, Output: "done"})
	waitUntil(t, time.Second, func() bool { return tk.Status() == TaskCompleted })

	mu.Lock()
	defer mu.Unlock()
	if len(settles) != 2 {
		t.Fatalf("settles = %d, want 2 (ready + completion)", len(settles))
	}
	// 终态后再绑：fencing 拒绝。
	d2 := NewManualDetector()
	if err := tm.BindDetector("t-bind", d2); err == nil {
		t.Fatal("bind to finalized task must be refused")
	}
}

func TestBindDetector_NotFound(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	if err := tm.BindDetector("nope", NewManualDetector()); err == nil {
		t.Fatal("bind to unknown task must fail")
	}
}
