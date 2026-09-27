package task

import (
	"sync"
	"time"
)

// NewTaskFixture builds a Task in a given status without driving the full
// spawn/settle lifecycle. For tests and board/digest previews only — real
// tasks are always produced by TaskManager.Spawn.
func NewTaskFixture(id, desc string, st TaskStatus, startedAt time.Time) *Task {
	t := &Task{ID: id, Spec: TaskSpec{Desc: desc}, StartedAt: startedAt}
	t.status = st
	return t
}

// ManualDetector is a SettleDetector driven manually — for tests across packages whose signals are driven by the test.
type ManualDetector struct {
	ch         chan SettleSignal
	mu         sync.Mutex
	can        bool
	det        chan struct{}
	detachOnce sync.Once
	stopped    chan struct{}
	stopOnce   sync.Once
}

func NewManualDetector() *ManualDetector {
	return &ManualDetector{
		ch:      make(chan SettleSignal, 4),
		det:     make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// NewManualDetectorDetach returns a detector that auto-detaches after `after`,
// mirroring the retired sync_wait window (detach ≈ old timeout) so migrated
// tests keep their emit-timing semantics.
func NewManualDetectorDetach(after time.Duration) *ManualDetector {
	m := NewManualDetector()
	go func() {
		time.Sleep(after)
		m.FireDetach()
	}()
	return m
}
func (m *ManualDetector) Settled() <-chan SettleSignal { return m.ch }
func (m *ManualDetector) Detached() <-chan struct{}    { return m.det }

// Stopped mirrors the production contract (the real detector closes it when its
// producer goroutine returns). A fixture has no producer, so Cancel — the signal
// that its simulated work is over — closes it; FireStop drives it explicitly when
// a test needs the "cancelled but not yet stopped" window.
func (m *ManualDetector) Stopped() <-chan struct{} { return m.stopped }
func (m *ManualDetector) FireStop()                { m.stopOnce.Do(func() { close(m.stopped) }) }

func (m *ManualDetector) Cancel() {
	m.mu.Lock()
	m.can = true
	m.mu.Unlock()
	m.FireStop()
}
func (m *ManualDetector) Cancelled() bool       { m.mu.Lock(); defer m.mu.Unlock(); return m.can }
func (m *ManualDetector) Emit(sig SettleSignal) { m.ch <- sig }
func (m *ManualDetector) Done()                 { close(m.ch) }
func (m *ManualDetector) FireDetach()           { m.detachOnce.Do(func() { close(m.det) }) }
func (m *ManualDetector) TriggerDetach()        { m.FireDetach() }
