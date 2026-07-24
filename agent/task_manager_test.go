package agent

import (
	"context"
	"sync"
	"testing"
	"time"
)

// manualDetector is a test SettleDetector whose signals are driven by the test.
type manualDetector struct {
	ch         chan SettleSignal
	mu         sync.Mutex
	can        bool
	det        chan struct{}
	detachOnce sync.Once
}

func newManualDetector() *manualDetector {
	return &manualDetector{ch: make(chan SettleSignal, 4), det: make(chan struct{})}
}

// newManualDetectorDetach returns a detector that auto-detaches after `after`,
// mirroring the retired sync_wait window (detach ≈ old timeout) so migrated
// tests keep their emit-timing semantics.
func newManualDetectorDetach(after time.Duration) *manualDetector {
	m := newManualDetector()
	go func() {
		time.Sleep(after)
		m.fireDetach()
	}()
	return m
}
func (m *manualDetector) Settled() <-chan SettleSignal { return m.ch }
func (m *manualDetector) Detached() <-chan struct{}    { return m.det }
func (m *manualDetector) Cancel() {
	m.mu.Lock()
	m.can = true
	m.mu.Unlock()
}
func (m *manualDetector) cancelled() bool       { m.mu.Lock(); defer m.mu.Unlock(); return m.can }
func (m *manualDetector) emit(sig SettleSignal) { m.ch <- sig }
func (m *manualDetector) done()                 { close(m.ch) }
func (m *manualDetector) fireDetach()           { m.detachOnce.Do(func() { close(m.det) }) }
func (m *manualDetector) triggerDetach()        { m.fireDetach() }

// TestTaskManager_SettleWithinWindow_Inline: settle before the sync-wait window
// elapses → Spawn returns inline with the signal, task Completed.
func TestTaskManager_SettleWithinWindow_Inline(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	d := newManualDetectorDetach(500 * time.Millisecond)
	go func() {
		time.Sleep(30 * time.Millisecond)
		d.emit(SettleSignal{Kind: SettleCompleted, Output: "done"})
		d.done()
	}()

	res := tm.Spawn(TaskSpec{Kind: "generic", Desc: "quick"}, d)

	if !res.Settled {
		t.Fatalf("expected inline settle within window, got ack")
	}
	if res.Signal.Output != "done" || res.Signal.Kind != SettleCompleted {
		t.Errorf("unexpected signal: %+v", res.Signal)
	}
	if got := res.Task.Status(); got != TaskCompleted {
		t.Errorf("status = %s, want completed", got)
	}
}

// TestTaskManager_SettleAfterWindow_BackgroundAck: no settle within window →
// Spawn returns ack (Settled=false); later settle fires OnSettle exactly once.
func TestTaskManager_SettleAfterWindow_BackgroundAck(t *testing.T) {
	var mu sync.Mutex
	var bg []SettleSignal
	tm := NewTaskManager(TaskManagerConfig{
		OnSettle: func(task *Task, sig SettleSignal) {
			mu.Lock()
			bg = append(bg, sig)
			mu.Unlock()
		},
	})
	d := newManualDetectorDetach(80 * time.Millisecond)

	res := tm.Spawn(TaskSpec{Kind: "command", Desc: "long build"}, d)
	if res.Settled {
		t.Fatalf("expected ack (not settled in window)")
	}
	if got := res.Task.Status(); got != TaskRunning {
		t.Errorf("status after ack = %s, want running", got)
	}

	// Background settle arrives after the window.
	d.emit(SettleSignal{Kind: SettleCompleted, Output: "late"})
	d.done()

	waitUntil(t, 2*time.Second, func() bool { mu.Lock(); defer mu.Unlock(); return len(bg) == 1 })
	mu.Lock()
	if len(bg) != 1 || bg[0].Output != "late" {
		t.Errorf("background settles = %+v, want exactly [late]", bg)
	}
	mu.Unlock()
	if got := res.Task.Status(); got != TaskCompleted {
		t.Errorf("final status = %s, want completed", got)
	}
}

// TestTaskManager_IdempotentSpawn_Dedup: a second Spawn with the same Key while
// the first is active returns Deduped and cancels the duplicate detector.
func TestTaskManager_IdempotentSpawn_Dedup(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	d1 := newManualDetectorDetach(300 * time.Millisecond) // detaches (acks) after the window
	key := "same-key"

	var res1 SpawnResult
	done := make(chan struct{})
	go func() {
		res1 = tm.Spawn(TaskSpec{Kind: "command", Desc: "a", Key: key}, d1)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond) // ensure task1 registered

	d2 := newManualDetector()
	res2 := tm.Spawn(TaskSpec{Kind: "command", Desc: "a-dup", Key: key}, d2)
	if !res2.Deduped {
		t.Fatalf("expected dedup for identical active key")
	}
	if !d2.cancelled() {
		t.Errorf("duplicate detector should be cancelled")
	}

	<-done
	if res2.Task != res1.Task {
		t.Errorf("dedup should return the existing task")
	}
}

// TestTaskManager_WindowBoundary_NoLostSettle: hammer the window boundary; every
// settle must be accounted for exactly once (inline XOR background), never lost.
func TestTaskManager_WindowBoundary_NoLostSettle(t *testing.T) {
	const n = 60
	var mu sync.Mutex
	bg := map[string]int{}
	tm := NewTaskManager(TaskManagerConfig{
		OnSettle: func(task *Task, sig SettleSignal) {
			mu.Lock()
			bg[task.ID]++
			mu.Unlock()
		},
	})

	inline := 0
	tasks := make([]*Task, 0, n)
	for i := 0; i < n; i++ {
		d := newManualDetectorDetach(2 * time.Millisecond)
		go func() {
			time.Sleep(2 * time.Millisecond) // right around the window boundary
			d.emit(SettleSignal{Kind: SettleCompleted, Output: "x"})
			d.done()
		}()
		res := tm.Spawn(TaskSpec{Kind: "generic", Desc: "boundary"}, d)
		tasks = append(tasks, res.Task)
		if res.Settled {
			inline++
		}
	}

	// Every task must eventually reach Completed (applyStatus ran).
	waitUntil(t, 3*time.Second, func() bool {
		for _, tk := range tasks {
			if tk.Status() != TaskCompleted {
				return false
			}
		}
		return true
	})

	// Accounting: inline + background == n, and no background double-count.
	mu.Lock()
	defer mu.Unlock()
	bgTotal := 0
	for _, c := range bg {
		if c != 1 {
			t.Errorf("task settled to background %d times, want 1", c)
		}
		bgTotal++
	}
	if inline+bgTotal != n {
		t.Errorf("accounting mismatch: inline=%d background=%d total=%d want %d",
			inline, bgTotal, inline+bgTotal, n)
	}
}

// TestTaskManager_Cancel marks a task cancelled and cancels its detector.
func TestTaskManager_Cancel(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	d := newManualDetectorDetach(40 * time.Millisecond) // detaches (acks) after the window
	res := tm.Spawn(TaskSpec{Kind: "command", Desc: "svc"}, d)
	if res.Settled {
		t.Fatalf("expected ack")
	}
	if !tm.Cancel(res.Task.ID) {
		t.Fatalf("cancel returned false")
	}
	if !d.cancelled() {
		t.Errorf("detector not cancelled")
	}
	if got := res.Task.Status(); got != TaskCancelled {
		t.Errorf("status = %s, want cancelled", got)
	}
}

// TestFuncSettleDetector_Generic verifies the generic goroutine detector settles
// Completed on return (with error captured).
func TestFuncSettleDetector_Generic(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	d := NewFuncSettleDetector(context.Background(), func(ctx context.Context) (string, error) {
		return "hello", nil
	})
	res := tm.Spawn(TaskSpec{Kind: "generic", Desc: "fn"}, d)
	if !res.Settled || res.Signal.Output != "hello" {
		t.Fatalf("expected inline hello, got %+v", res)
	}
}

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}
