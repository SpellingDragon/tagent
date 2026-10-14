// 契约: docs/wiki/agent/task-lifecycle.md#detach-suppression
package task

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestBackgroundJobObservationsNotifyBeforeCompletion verifies observations and completion all reach the task sink.
func TestBackgroundJobObservationsNotifyBeforeCompletion(t *testing.T) {
	settles := make(chan SettleSignal, 3)
	tm := NewTaskManager(TaskManagerConfig{OnSettle: func(_ *Task, sig SettleSignal) { settles <- sig }})
	detector := NewManualDetector()
	t.Cleanup(detector.Done)
	detector.FireDetach()
	res := tm.Spawn(TaskSpec{Kind: "command"}, detector)
	require.False(t, res.Settled)
	for _, kind := range []SettleKind{SettleStable, SettleSuspect, SettleCompleted} {
		detector.Emit(SettleSignal{Kind: kind, Output: string(kind)})
		select {
		case sig := <-settles:
			require.Equal(t, kind, sig.Kind)
		case <-time.After(time.Second):
			t.Fatalf("background %s never reached OnSettle", kind)
		}
	}
	require.Equal(t, TaskCompleted, res.Task.Status())
}

// TestCancelDuringSpawnUnblocksInlineWindow verifies cancelling before any observation returns inline exactly once.
func TestCancelDuringSpawnUnblocksInlineWindow(t *testing.T) {
	var tm *TaskManager
	tm = NewTaskManager(TaskManagerConfig{
		OnSpawn:  func(tk *Task) { require.True(t, tm.Cancel(tk.ID)) },
		OnSettle: func(*Task, SettleSignal) { t.Error("an inline cancellation must not also notify in background") },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	detector := NewFuncSettleDetector(ctx, func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	result := make(chan SpawnResult, 1)
	go func() { result <- tm.Spawn(TaskSpec{Kind: "command"}, detector) }()
	select {
	case res := <-result:
		require.True(t, res.Settled)
		require.True(t, res.Signal.EndsRound())
		require.Equal(t, TaskCancelled, res.Task.Status())
	case <-time.After(time.Second):
		t.Fatal("cancelling the detector stranded the inline waiter")
	}
}

type gatedWindowDetector struct {
	*ManualDetector
	enter   chan struct{}
	release chan struct{}
}

func (d *gatedWindowDetector) Detached() <-chan struct{} {
	close(d.enter)
	<-d.release
	return d.ManualDetector.Detached()
}

// TestInlineObservationKeepsFollowingCompletion verifies a blocked window handoff preserves the following terminal signal.
func TestInlineObservationKeepsFollowingCompletion(t *testing.T) {
	settles := make(chan SettleSignal, 2)
	tm := NewTaskManager(TaskManagerConfig{OnSettle: func(_ *Task, sig SettleSignal) { settles <- sig }})
	detector := &gatedWindowDetector{ManualDetector: NewManualDetector(), enter: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(detector.Done)
	result := make(chan SpawnResult, 1)
	go func() { result <- tm.Spawn(TaskSpec{Kind: "command"}, detector) }()
	<-detector.enter
	detector.Emit(SettleSignal{Kind: SettleStable, Output: "ready"})
	detector.Emit(SettleSignal{Kind: SettleCompleted, Output: "finished"})
	time.Sleep(50 * time.Millisecond)
	close(detector.release)
	res := <-result
	require.True(t, res.Settled)
	require.Equal(t, SettleStable, res.Signal.Kind)
	select {
	case sig := <-settles:
		require.Equal(t, SettleCompleted, sig.Kind)
		require.Equal(t, "finished", sig.Output)
	case <-time.After(time.Second):
		t.Fatal("the inline observation swallowed the following terminal settle")
	}
}

// TestNormalTerminalSettleConfirmsDelivery verifies ordinary inline/background terminals are not resent by the board.
func TestNormalTerminalSettleConfirmsDelivery(t *testing.T) {
	for _, inline := range []bool{false, true} {
		for _, kind := range []SettleKind{SettleCompleted, SettleFailed} {
			t.Run(string(kind)+map[bool]string{true: "_inline", false: "_background"}[inline], func(t *testing.T) {
				var now atomic.Int64
				now.Store(time.Now().UnixNano())
				tm := NewTaskManager(TaskManagerConfig{OnRetireResend: func([]BatchRetired) { t.Error("normal delivery must not be resent") }})
				tm.now = func() time.Time { return time.Unix(0, now.Load()) }
				detector := NewManualDetector()
				t.Cleanup(detector.Done)
				if inline {
					detector.Emit(SettleSignal{Kind: kind})
				} else {
					detector.FireDetach()
				}
				res := tm.Spawn(TaskSpec{Kind: "command"}, detector)
				if !inline {
					detector.Emit(SettleSignal{Kind: kind})
				}
				waitUntil(t, time.Second, func() bool {
					res.Task.mu.Lock()
					defer res.Task.mu.Unlock()
					return res.Task.retireNotified
				})
				want := TaskCompleted
				if kind == SettleFailed {
					want = TaskFailed
				}
				require.Equal(t, want, res.Task.Status())
				now.Add(int64(3 * time.Second))
				tm.reconcileRetireNotify()
			})
		}
	}
}

// TestServiceFinalSuspectStillNotifies verifies a final timeout cannot be hidden by service observation suppression.
func TestServiceFinalSuspectStillNotifies(t *testing.T) {
	settles := make(chan SettleSignal, 2)
	tm := NewTaskManager(TaskManagerConfig{OnSettle: func(_ *Task, sig SettleSignal) { settles <- sig }})
	detector := NewManualDetector()
	t.Cleanup(detector.Done)
	detector.FireDetach()
	tm.Spawn(TaskSpec{Lifetime: LifetimeService}, detector)
	for _, sig := range []SettleSignal{{Kind: SettleStable}, {Kind: SettleSuspect, Final: true}} {
		detector.Emit(sig)
		select {
		case received := <-settles:
			require.Equal(t, sig.Kind, received.Kind)
		case <-time.After(time.Second):
			t.Fatal("the service suppressed the final signal")
		}
	}
}
