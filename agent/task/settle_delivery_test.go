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
				tm := NewTaskManager(TaskManagerConfig{OnSettleAccepted: func(*Task, SettleSignal) bool { return true }, OnInlineSettleAccepted: func(*Task, SettleSignal) bool { return true }, OnRetireResend: func([]BatchRetired) { t.Error("normal delivery must not be resent") }})
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

// admittedTestDetector exposes activation and receipt acknowledgement separately from observation.
type admittedTestDetector struct {
	*ManualDetector
	starts atomic.Int32
	acks   chan SettleSignal
}

func newAdmittedTestDetector() *admittedTestDetector {
	return &admittedTestDetector{ManualDetector: NewManualDetector(), acks: make(chan SettleSignal, 8)}
}

func (d *admittedTestDetector) Start()                             { d.starts.Add(1) }
func (d *admittedTestDetector) AcknowledgeSignal(sig SettleSignal) { d.acks <- sig }

// TestExplicitExitWaitKeepsObservationsOutOfForeground checks that a live producer's observations cannot complete its exit wait.
func TestExplicitExitWaitKeepsObservationsOutOfForeground(t *testing.T) {
	observations := make(chan SettleSignal, 1)
	tm := NewTaskManager(TaskManagerConfig{
		OnSettleAccepted:       func(_ *Task, sig SettleSignal) bool { observations <- sig; return true },
		OnInlineSettleAccepted: func(*Task, SettleSignal) bool { return true },
	})
	d := NewManualDetector()
	t.Cleanup(func() { require.NoError(t, tm.Close(context.Background())) })
	started := make(chan *Task, 1)
	result := make(chan SpawnResult, 1)
	go func() {
		result <- tm.Spawn(TaskSpec{Kind: "command", WaitFor: WaitExit, OnRoundStart: func(tk *Task) { started <- tk }}, d)
	}()
	tk := <-started
	d.Emit(SettleSignal{Kind: SettleStable, Output: "no output", Final: true})
	observation := <-observations
	require.False(t, observation.SatisfiesWait())
	require.Equal(t, "stable", observation.Observation)
	require.Equal(t, TaskRunning, tk.Status())
	select {
	case <-result:
		t.Fatal("final observation released an exit wait")
	case <-time.After(30 * time.Millisecond):
	}
	d.Emit(SettleSignal{Kind: SettleCompleted, Output: "exit"})
	res := <-result
	require.True(t, res.Settled)
	require.True(t, res.Signal.SatisfiesWait())
	require.Equal(t, observation.RoundID, res.Signal.RoundID)
	require.Greater(t, res.Signal.Seq, observation.Seq)
}

// TestAdmissionCommitsIntentAndBindingBeforeActivation checks that rejected admission cannot start business work.
func TestAdmissionCommitsIntentAndBindingBeforeActivation(t *testing.T) {
	for _, refuseIntent := range []bool{true, false} {
		t.Run(map[bool]string{true: "intent", false: "binding"}[refuseIntent], func(t *testing.T) {
			d := newAdmittedTestDetector()
			var prepared atomic.Int32
			tm := NewTaskManager(TaskManagerConfig{
				OnSpawnAccepted:        func(*Task) bool { return !refuseIntent },
				OnStartAccepted:        func(*Task) bool { return false },
				OnRoundAbortedAccepted: func(*Task, SettleSignal) bool { return true },
			})
			t.Cleanup(func() { require.NoError(t, tm.Close(context.Background())) })
			res := tm.Spawn(TaskSpec{Kind: "command", Start: func(tk *Task) (TaskSpec, SettleDetector, error) {
				prepared.Add(1)
				return tk.SpecSnapshot(), d, nil
			}}, nil)
			require.NotEmpty(t, res.Blocked)
			require.Zero(t, d.starts.Load())
			if refuseIntent {
				require.Zero(t, prepared.Load())
				require.Nil(t, res.Task)
			} else {
				require.EqualValues(t, 1, prepared.Load())
				require.True(t, d.Cancelled())
			}
		})
	}
}

// TestRejectedSignalRetriesWithoutBoardAndRetainsReceipt checks automatic retry and producer ACK use the original signal identity.
func TestRejectedSignalRetriesWithoutBoardAndRetainsReceipt(t *testing.T) {
	calls := make(chan SettleSignal, 2)
	var attempts atomic.Int32
	tm := NewTaskManager(TaskManagerConfig{OnSettleAccepted: func(_ *Task, sig SettleSignal) bool {
		calls <- sig
		return attempts.Add(1) > 1
	}})
	d := newAdmittedTestDetector()
	d.FireDetach()
	res := tm.Spawn(TaskSpec{Kind: "command", WaitFor: WaitExit}, d)
	t.Cleanup(func() { require.NoError(t, tm.Close(context.Background())) })
	d.Emit(SettleSignal{Kind: SettleCompleted, Output: "durable result"})
	first := <-calls
	select {
	case <-d.acks:
		t.Fatal("rejected delivery acknowledged producer")
	default:
	}
	var second SettleSignal
	select {
	case second = <-calls:
	case <-time.After(2 * time.Second):
		t.Fatal("signal was only retried by an external board read")
	}
	require.Equal(t, first, second)
	select {
	case ack := <-d.acks:
		require.Equal(t, first, ack)
	case <-time.After(time.Second):
		t.Fatal("accepted delivery never acknowledged producer")
	}
	require.Eventually(t, func() bool { return !res.Task.HasPendingDelivery() }, time.Second, time.Millisecond)
}

// TestNoReceiverKeepsTerminalReceipt checks a missing receiver never counts as acceptance or permits pruning.
func TestNoReceiverKeepsTerminalReceipt(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{TerminalTTL: time.Millisecond})
	d := NewManualDetector()
	d.FireDetach()
	res := tm.Spawn(TaskSpec{Kind: "command"}, d)
	t.Cleanup(func() { require.NoError(t, tm.Close(context.Background())) })
	d.Emit(SettleSignal{Kind: SettleCompleted, Output: "unaccepted"})
	require.Eventually(t, res.Task.HasPendingDelivery, time.Second, time.Millisecond)
	time.Sleep(10 * time.Millisecond)
	tm.pruneTerminal()
	_, retained := tm.Get(res.Task.ID)
	require.True(t, retained)
	res.Task.mu.Lock()
	require.False(t, res.Task.retireNotified)
	res.Task.mu.Unlock()
}

// TestRestoredReceiptWaitsForRecoveryBarrier checks restore registration cannot race receiver recovery.
func TestRestoredReceiptWaitsForRecoveryBarrier(t *testing.T) {
	calls := make(chan SettleSignal, 1)
	tm := NewTaskManager(TaskManagerConfig{OnSettleAccepted: func(_ *Task, sig SettleSignal) bool { calls <- sig; return true }})
	tk := tm.RestoreTask("restored-receipt", TaskSpec{Kind: "command", Declarative: &Declarative{RoundID: 3, ProducerRound: 3, RoundOwner: "owner", ProducerOwner: "owner", SignalSeq: 8}}, time.Now(), TaskCompleted)
	sig := SettleSignal{Kind: SettleCompleted, RoundID: 3, ProducerRound: 3, RoundOwner: "owner", Seq: 8, ObservedAt: time.Now(), Output: "saved", WaitSatisfied: true}
	sig.SourceID = SignalSourceID(tk.ID, sig.RoundID, sig.Seq)
	require.True(t, tm.RestorePendingDelivery(tk.ID, sig))
	d := newAdmittedTestDetector()
	require.NoError(t, tm.BindDetector(tk.ID, d))
	t.Cleanup(func() { require.NoError(t, tm.Close(context.Background())) })
	select {
	case <-calls:
		t.Fatal("restore invoked a receiver before recovery barrier")
	case <-time.After(30 * time.Millisecond):
	}
	tm.ResumePendingDeliveries()
	select {
	case got := <-calls:
		require.Equal(t, sig, got)
	case <-time.After(time.Second):
		t.Fatal("recovered pending receipt never resumed")
	}
	select {
	case got := <-d.acks:
		require.Equal(t, sig, got)
	case <-time.After(time.Second):
		t.Fatal("restored terminal receipt never acknowledged")
	}
}

// stoppingTestDetector keeps cancellation and confirmed producer exit independently controllable.
type stoppingTestDetector struct {
	*ManualDetector
	producerStopped chan struct{}
	cancelRequested chan struct{}
	once            atomic.Bool
}

func (d *stoppingTestDetector) Cancel() {
	if d.once.CompareAndSwap(false, true) {
		close(d.cancelRequested)
	}
}
func (d *stoppingTestDetector) Stopped() <-chan struct{} { return d.producerStopped }

// TestCloseWaitsForCallbackAndActualProducerExit checks the bounded close result never fabricates quiescence.
func TestCloseWaitsForCallbackAndActualProducerExit(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	tm := NewTaskManager(TaskManagerConfig{OnSettleAccepted: func(*Task, SettleSignal) bool { close(entered); <-release; return true }})
	d := &stoppingTestDetector{ManualDetector: NewManualDetector(), producerStopped: make(chan struct{}), cancelRequested: make(chan struct{})}
	d.FireDetach()
	tm.Spawn(TaskSpec{Kind: "command"}, d)
	d.Emit(SettleSignal{Kind: SettleCompleted})
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, tm.Close(ctx), context.DeadlineExceeded)
	<-d.cancelRequested
	close(d.producerStopped)
	select {
	case <-tm.CloseDone():
		t.Fatal("close ignored callback still running")
	default:
	}
	close(release)
	select {
	case <-tm.CloseDone():
	case <-time.After(time.Second):
		t.Fatal("close did not finish after all owners returned")
	}
	require.NotEmpty(t, tm.Spawn(TaskSpec{}, nil).Blocked)
}

// TestDeadlineWorkerExpiresQuietTask checks TTL reclaim runs without a board read or input turn.
func TestDeadlineWorkerExpiresQuietTask(t *testing.T) {
	settles := make(chan SettleSignal, 1)
	tm := NewTaskManager(TaskManagerConfig{EnableDeadlineReaper: true, OnSettleAccepted: func(_ *Task, sig SettleSignal) bool { settles <- sig; return true }})
	d := NewManualDetector()
	d.FireDetach()
	res := tm.Spawn(TaskSpec{Kind: "command", TTL: 30 * time.Millisecond}, d)
	t.Cleanup(func() { require.NoError(t, tm.Close(context.Background())) })
	select {
	case sig := <-settles:
		require.Equal(t, SettleFailed, sig.Kind)
		require.Equal(t, LineageRetired, sig.Lineage)
	case <-time.After(time.Second):
		t.Fatal("quiet task did not expire without a board/input sweep")
	}
	require.Equal(t, TaskFailed, res.Task.Status())
	require.True(t, d.Cancelled())
}

// TestFailedAcceptedResumeConsumesIntentAndPreservesProducer checks a retry cannot recycle the failed intent or renew the old producer's TTL.
func TestFailedAcceptedResumeConsumesIntentAndPreservesProducer(t *testing.T) {
	aborts := make(chan SettleSignal, 1)
	d := NewManualDetector()
	d.FireDetach()
	calls := 0
	tm := NewTaskManager(TaskManagerConfig{OnSettleAccepted: func(*Task, SettleSignal) bool { return true }, OnRoundAccepted: func(*Task) bool { return true }, OnRoundAbortedAccepted: func(_ *Task, sig SettleSignal) bool { aborts <- sig; return true }})
	res := tm.Spawn(TaskSpec{Kind: "command", RoundOwner: "original", ResumeFn: func(context.Context, string) (SettleDetector, error) {
		calls++
		if calls == 1 {
			return nil, context.Canceled
		}
		next := NewManualDetector()
		next.FireDetach()
		return next, nil
	}}, d)
	d.Emit(SettleSignal{Kind: SettleStable, WaitSatisfied: true})
	waitStatus(t, res.Task, TaskStable)
	anchor := res.Task.TTLAnchorMilli()
	_, err := tm.Resume(WithRoundOwner(context.Background(), "failed-owner"), res.Task.ID, "fail")
	require.Error(t, err)
	abort := <-aborts
	require.EqualValues(t, 2, abort.RoundID)
	require.EqualValues(t, 1, abort.ProducerRound)
	require.Equal(t, "round_aborted", abort.Observation)
	require.EqualValues(t, 1, res.Task.ProducerRound())
	require.Equal(t, "original", res.Task.ProducerOwner())
	require.Equal(t, anchor, res.Task.TTLAnchorMilli())
	_, err = tm.Resume(WithRoundOwner(context.Background(), "new-owner"), res.Task.ID, "retry")
	require.NoError(t, err)
	require.EqualValues(t, 3, res.Task.RoundID())
	require.EqualValues(t, 3, res.Task.ProducerRound())
	require.NoError(t, tm.Close(context.Background()))
}
