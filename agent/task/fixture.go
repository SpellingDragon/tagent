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

// NewManualDetector 构造一个人工驱动的结算探测器：结算、脱离、停止都由调用方显式触发。
func NewManualDetector() *ManualDetector {
	return &ManualDetector{
		ch:      make(chan SettleSignal, 4),
		det:     make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// NewManualDetectorDetach 返回一个在 after 之后自动脱离前台的探测器，使"发出信号的
// 时机"与一个固定时长窗口等价，便于跨包测试复用同一套时序语义。
func NewManualDetectorDetach(after time.Duration) *ManualDetector {
	m := NewManualDetector()
	go func() {
		time.Sleep(after)
		m.FireDetach()
	}()
	return m
}

// Settled 交出结算信号流；Done 关闭它，因此消费方以"通道关闭"作为无进一步异议的依据。
func (m *ManualDetector) Settled() <-chan SettleSignal { return m.ch }

// Detached 交出脱离信号：关闭即表示任务可继续存活，而前台无需等待它。
func (m *ManualDetector) Detached() <-chan struct{} { return m.det }

// Stopped mirrors the production contract (the real detector closes it when its
// producer goroutine returns). A fixture has no producer, so Cancel — the signal
// that its simulated work is over — closes it; FireStop drives it explicitly when
// a test needs the "cancelled but not yet stopped" window.
func (m *ManualDetector) Stopped() <-chan struct{} { return m.stopped }

// FireStop 显式关闭停止信号，恰好一次；重复调用无副作用。
func (m *ManualDetector) FireStop() { m.stopOnce.Do(func() { close(m.stopped) }) }

// Cancel 同时做两件事：记下"已取消"供 Cancelled 查询，并触发停止信号——因为模拟工作
// 一被取消就结束了，不存在"取消了但还在跑"的中间态（要那一段请单独用 FireStop）。
func (m *ManualDetector) Cancel() {
	m.mu.Lock()
	m.can = true
	m.mu.Unlock()
	m.FireStop()
}

// Cancelled 报告是否已被取消。
func (m *ManualDetector) Cancelled() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.can }

// Emit 投递一个结算信号。通道容量为 4，超出的发射会阻塞，从而让测试能观察到背压。
func (m *ManualDetector) Emit(sig SettleSignal) { m.ch <- sig }

// Done 关闭结算通道，表示无后续异议。
func (m *ManualDetector) Done() { close(m.ch) }

// FireDetach 关闭脱离信号，恰好一次。
func (m *ManualDetector) FireDetach() { m.detachOnce.Do(func() { close(m.det) }) }

// TriggerDetach 是 FireDetach 的同义入口，供按"触发"语义书写的调用方使用。
func (m *ManualDetector) TriggerDetach() { m.FireDetach() }
