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
	d.Emit(SettleSignal{Kind: SettleStable, Output: "no output"})
	observation := <-observations
	require.False(t, observation.SatisfiesWait())
	require.Equal(t, "stable", observation.Observation)
	require.Equal(t, TaskRunning, tk.Status())
	select {
	case <-result:
		t.Fatal("non-final observation released an exit wait")
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
	sig := SettleSignal{WaitFor: WaitExit, Kind: SettleCompleted, RoundID: 3, ProducerRound: 3, RoundOwner: "owner", Seq: 8, ObservedAt: time.Now(), Output: "saved", WaitSatisfied: true}
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

// failedActivationDetector models a prepared input binding whose actual resource keeps its previous round.
type failedActivationDetector struct {
	*ManualDetector
	producer  atomic.Uint64
	requested uint64
	detach    chan struct{}
}

func (d *failedActivationDetector) ProducerRound() uint64     { return d.producer.Load() }
func (d *failedActivationDetector) BeginRound(round uint64)   { d.requested = round }
func (d *failedActivationDetector) Detached() <-chan struct{} { return d.detach }
func (d *failedActivationDetector) Start() {
	d.Emit(SettleSignal{Kind: SettleFailed, RoundID: d.requested, ProducerRound: d.producer.Load(), Seq: 9, Observation: "round_aborted", Err: context.Canceled})
}

// TestActivationFailureKeepsActualProducerAndReturnsAbortedIntent checks a committed binding cannot turn a failed input into a new producer round.
func TestActivationFailureKeepsActualProducerAndReturnsAbortedIntent(t *testing.T) {
	aborts := make(chan SettleSignal, 1)
	base := NewManualDetector()
	base.FireDetach()
	d := &failedActivationDetector{ManualDetector: base, detach: make(chan struct{})}
	d.producer.Store(1)
	tm := NewTaskManager(TaskManagerConfig{
		OnSettleAccepted:       func(*Task, SettleSignal) bool { return true },
		OnRoundAccepted:        func(*Task) bool { return true },
		OnStartAccepted:        func(*Task) bool { return true },
		OnRoundAbortedAccepted: func(_ *Task, sig SettleSignal) bool { aborts <- sig; return true },
	})
	res := tm.Spawn(TaskSpec{Kind: "command", RoundOwner: "producer-owner", ResumeFn: func(context.Context, string) (SettleDetector, error) { return d, nil }}, base)
	base.Emit(SettleSignal{Kind: SettleStable, WaitSatisfied: true})
	waitStatus(t, res.Task, TaskStable)
	anchor := res.Task.TTLAnchorMilli()
	result, err := tm.Resume(WithRoundOwner(context.Background(), "intent-owner"), res.Task.ID, "input")
	require.NoError(t, err)
	require.True(t, result.Settled)
	require.Equal(t, "round_aborted", result.Signal.Observation)
	require.Equal(t, "intent-owner", result.Signal.RoundOwner)
	require.EqualValues(t, 9, result.Signal.Seq)
	require.EqualValues(t, 2, result.Signal.RoundID)
	require.EqualValues(t, 1, result.Signal.ProducerRound)
	require.EqualValues(t, 1, res.Task.ProducerRound())
	require.Equal(t, "producer-owner", res.Task.ProducerOwner())
	require.Equal(t, TaskStable, res.Task.Status())
	require.Equal(t, anchor, res.Task.TTLAnchorMilli())
	require.Equal(t, result.Signal, <-aborts)
	require.NoError(t, tm.Close(context.Background()))
}

// TestFinalObservationEndsWaitWithoutCompletingProducer checks an ended observer releases the caller while preserving the ambiguous task state.
func TestFinalObservationEndsWaitWithoutCompletingProducer(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{OnInlineSettleAccepted: func(*Task, SettleSignal) bool { return true }})
	d := NewManualDetector()
	d.Emit(SettleSignal{Kind: SettleSuspect, Observation: "suspect", Final: true, Output: "observation ended"})
	res := tm.Spawn(TaskSpec{Kind: "command", WaitFor: WaitExit}, d)
	require.True(t, res.Settled)
	require.True(t, res.Signal.SatisfiesWait())
	require.Equal(t, TaskSuspect, res.Task.Status())
	require.False(t, isTerminalStatus(res.Task.Status()))
	require.NoError(t, tm.Close(context.Background()))
}

// TestLazySpawnDeduplicatesBeforePreparation checks committed preparation runs once and activation follows resource binding.
func TestLazySpawnDeduplicatesBeforePreparation(t *testing.T) {
	var stages []string
	d := newAdmittedTestDetector()
	d.FireDetach()
	tm := NewTaskManager(TaskManagerConfig{
		OnSpawnAccepted: func(tk *Task) bool {
			stages = append(stages, "intent")
			require.Zero(t, tk.ProducerRound())
			return true
		},
		OnStartAccepted: func(*Task) bool { stages = append(stages, "binding"); require.Zero(t, d.starts.Load()); return true },
	})
	spec := TaskSpec{Kind: "command", Key: "same-resource", Start: func(tk *Task) (TaskSpec, SettleDetector, error) {
		stages = append(stages, "prepared")
		return tk.SpecSnapshot(), d, nil
	}}
	first := tm.Spawn(spec, nil)
	require.Equal(t, []string{"intent", "prepared", "binding"}, stages)
	require.EqualValues(t, 1, d.starts.Load())
	second := tm.Spawn(spec, nil)
	require.True(t, second.Deduped)
	require.Same(t, first.Task, second.Task)
	require.Len(t, stages, 3)
	require.NoError(t, tm.Close(context.Background()))
}

// recoveredTestDetector carries pinned resource metadata supplied by recovery.
type recoveredTestDetector struct {
	*admittedTestDetector
	spec TaskSpec
}

func (d *recoveredTestDetector) RecoveredSpec() TaskSpec { return d.spec }

// TestRecoveredBindingRejectionCannotActivate checks restored resource identity must be accepted before its observer starts.
func TestRecoveredBindingRejectionCannotActivate(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{OnStartAccepted: func(*Task) bool { return false }})
	tk := tm.RestoreTask("pinned", TaskSpec{Kind: "command", Origin: map[string]string{"owner": "original"}, Declarative: &Declarative{Params: map[string]string{"resource_id": "old"}}}, time.Now(), TaskRunning)
	d := &recoveredTestDetector{admittedTestDetector: newAdmittedTestDetector(), spec: TaskSpec{Kind: "command", Origin: map[string]string{"owner": "replacement"}, Declarative: &Declarative{Params: map[string]string{"resource_id": "new"}}}}
	require.Error(t, tm.BindDetector(tk.ID, d))
	require.Zero(t, d.starts.Load())
	require.True(t, d.Cancelled())
	require.False(t, tk.HasProducer())
	require.Equal(t, "old", tk.SpecSnapshot().Declarative.Params["resource_id"])
	require.Equal(t, "original", tk.SpecSnapshot().Origin["owner"])
	require.NoError(t, tm.Close(context.Background()))
}

// TestDeadlineSourceRecheckExpiresExistingTask checks a source rotation reschedules the deadline without mutating task TTL or lifetime anchor.
func TestDeadlineSourceRecheckExpiresExistingTask(t *testing.T) {
	var ttl atomic.Int64
	ttl.Store(int64(time.Hour))
	settled := make(chan struct{}, 1)
	tm := NewTaskManager(TaskManagerConfig{EnableDeadlineReaper: true, OnSettleAccepted: func(*Task, SettleSignal) bool { settled <- struct{}{}; return true }})
	tm.SetTTLSource(func() (time.Duration, time.Duration) { return time.Minute, time.Duration(ttl.Load()) })
	d := NewManualDetector()
	d.FireDetach()
	res := tm.Spawn(TaskSpec{Kind: "command"}, d)
	anchor := res.Task.TTLAnchorMilli()
	ttl.Store(int64(20 * time.Millisecond))
	tm.RecheckDeadlines()
	select {
	case <-settled:
	case <-time.After(time.Second):
		t.Fatal("TTL source rotation did not reschedule the existing quiet task")
	}
	require.Zero(t, res.Task.SpecSnapshot().TTL)
	require.Equal(t, anchor, res.Task.TTLAnchorMilli())
	require.NoError(t, tm.Close(context.Background()))
}

// TestDeadlineRetirementUnblocksDenseWait checks TTL retirement also settles a caller whose producer never detached or emitted.
func TestDeadlineRetirementUnblocksDenseWait(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{EnableDeadlineReaper: true, OnInlineSettleAccepted: func(*Task, SettleSignal) bool { return true }})
	d := NewManualDetector()
	result := make(chan SpawnResult, 1)
	go func() {
		result <- tm.Spawn(TaskSpec{Kind: "command", WaitFor: WaitExit, TTL: 20 * time.Millisecond}, d)
	}()
	select {
	case res := <-result:
		require.True(t, res.Settled)
		require.Equal(t, TaskFailed, res.Task.Status())
		require.True(t, res.Signal.SatisfiesWait())
	case <-time.After(time.Second):
		t.Fatal("TTL retirement stranded a dense foreground wait")
	}
	require.NoError(t, tm.Close(context.Background()))
}

// TestOldForegroundHandoffCannotCloseResumedWindow checks a delayed caller closes only the window whose signal it received.
func TestOldForegroundHandoffCannotCloseResumedWindow(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{OnInlineSettleAccepted: func(*Task, SettleSignal) bool { return true }})
	old := &gatedWindowDetector{ManualDetector: NewManualDetector(), enter: make(chan struct{}), release: make(chan struct{})}
	next := NewManualDetector()
	started := make(chan *Task, 1)
	first := make(chan SpawnResult, 1)
	go func() {
		first <- tm.Spawn(TaskSpec{Kind: "command", OnRoundStart: func(tk *Task) { started <- tk }, ResumeFn: func(context.Context, string) (SettleDetector, error) { return next, nil }}, old)
	}()
	tk := <-started
	<-old.enter
	old.Emit(SettleSignal{Kind: SettleStable, WaitSatisfied: true, Output: "old ready"})
	waitStatus(t, tk, TaskStable)
	resumed := make(chan SpawnResult, 1)
	go func() {
		res, err := tm.Resume(context.Background(), tk.ID, "next")
		require.NoError(t, err)
		resumed <- res
	}()
	require.Eventually(t, func() bool { return tk.ProducerRound() == 2 }, time.Second, time.Millisecond)
	close(old.release)
	require.Equal(t, "old ready", (<-first).Signal.Output)
	select {
	case <-resumed:
		t.Fatal("old caller closed the new round's foreground window")
	case <-time.After(30 * time.Millisecond):
	}
	next.Emit(SettleSignal{Kind: SettleCompleted, Output: "new result"})
	select {
	case res := <-resumed:
		require.Equal(t, "new result", res.Signal.Output)
	case <-time.After(time.Second):
		t.Fatal("new round lost its independent foreground handoff")
	}
	require.NoError(t, tm.Close(context.Background()))
}

// TestRejectedResumeBindingPreservesSharedProducer checks failure to record a prepared input cannot cancel the unchanged backing resource.
func TestRejectedResumeBindingPreservesSharedProducer(t *testing.T) {
	var bindings atomic.Int32
	base := newAdmittedTestDetector()
	base.FireDetach()
	prepared := &admittedTestDetector{ManualDetector: base.ManualDetector, acks: make(chan SettleSignal, 2)}
	tm := NewTaskManager(TaskManagerConfig{
		OnSettleAccepted:       func(*Task, SettleSignal) bool { return true },
		OnStartAccepted:        func(*Task) bool { return bindings.Add(1) == 1 },
		OnRoundAccepted:        func(*Task) bool { return true },
		OnRoundAbortedAccepted: func(*Task, SettleSignal) bool { return true },
	})
	res := tm.Spawn(TaskSpec{Kind: "command", ResumeFn: func(context.Context, string) (SettleDetector, error) { return prepared, nil }}, base)
	base.Emit(SettleSignal{Kind: SettleStable, WaitSatisfied: true})
	waitStatus(t, res.Task, TaskStable)
	anchor := res.Task.TTLAnchorMilli()
	_, err := tm.Resume(context.Background(), res.Task.ID, "input")
	require.Error(t, err)
	require.False(t, base.Cancelled())
	require.Zero(t, prepared.starts.Load())
	require.EqualValues(t, 1, res.Task.ProducerRound())
	require.Equal(t, anchor, res.Task.TTLAnchorMilli())
	require.Equal(t, TaskStable, res.Task.Status())
	require.NoError(t, tm.Close(context.Background()))
}

// TestRecoveryBarrierDefersProductionDeadlineAndNewObservations checks recovery can assemble old tasks and receipts before any consumer or deadline runs.
func TestRecoveryBarrierDefersProductionDeadlineAndNewObservations(t *testing.T) {
	calls := make(chan SettleSignal, 2)
	tm := NewTaskManager(TaskManagerConfig{EnableDeadlineReaper: true, OnSettleAccepted: func(_ *Task, sig SettleSignal) bool { calls <- sig; return true }})
	tk := tm.RestoreTask("recover-before-expiry", TaskSpec{Kind: "command", TTL: time.Millisecond}, time.Now().Add(-time.Hour), TaskRunning)
	d := NewManualDetector()
	require.NoError(t, tm.BindDetector(tk.ID, d))
	d.Emit(SettleSignal{Kind: SettleStable, Observation: "stable"})
	require.Eventually(t, tk.HasPendingDelivery, time.Second, time.Millisecond)
	select {
	case <-calls:
		t.Fatal("restored observation reached a receiver before the recovery barrier")
	case <-time.After(30 * time.Millisecond):
	}
	require.Equal(t, TaskRunning, tk.Status())
	require.False(t, d.Cancelled())
	tm.ResumePendingDeliveries()
	seenTerminal := false
	for i := 0; i < 2; i++ {
		select {
		case sig := <-calls:
			if sig.Kind == SettleFailed {
				seenTerminal = true
			}
		case <-time.After(time.Second):
			t.Fatal("recovery barrier failed to resume pending observations and deadline")
		}
	}
	require.True(t, seenTerminal)
	require.Equal(t, TaskFailed, tk.Status())
	require.NoError(t, tm.Close(context.Background()))
}

// TestUnboundIntentRecoveryKeepsIntentAndCommitsResourceBeforeStart checks original-intent preparation commits before activation.
func TestUnboundIntentRecoveryKeepsIntentAndCommitsResourceBeforeStart(t *testing.T) {
	var bindings atomic.Int32
	d := newAdmittedTestDetector()
	tm := NewTaskManager(TaskManagerConfig{EnableDeadlineReaper: true, OnStartAccepted: func(tk *Task) bool {
		bindings.Add(1)
		require.Zero(t, d.starts.Load())
		require.EqualValues(t, 4, tk.RoundID())
		return true
	}})
	tk := tm.RestoreTask("unbound-intent", TaskSpec{Kind: "command", RoundOwner: "owner", Declarative: &Declarative{RoundID: 4, RoundOwner: "owner", Params: map[string]string{"phase": "intent"}}, Start: func(tk *Task) (TaskSpec, SettleDetector, error) { return tk.SpecSnapshot(), d, nil }}, time.Now(), TaskSuspect)
	require.Zero(t, tk.ProducerRound())
	require.NoError(t, tm.ResumeRestoredStart(tk.ID))
	require.EqualValues(t, 4, tk.RoundID())
	require.EqualValues(t, 4, tk.ProducerRound())
	require.EqualValues(t, 1, bindings.Load())
	require.EqualValues(t, 1, d.starts.Load())
	require.Error(t, tm.ResumeRestoredStart(tk.ID))
	tm.ResumePendingDeliveries()
	require.NoError(t, tm.Close(context.Background()))
}

// TestResumeBindingPersistsRenewedAnchor checks the accepted resource binding already contains the producer's renewed TTL anchor.
func TestResumeBindingPersistsRenewedAnchor(t *testing.T) {
	var anchor atomic.Int64
	base := time.Now()
	tm := NewTaskManager(TaskManagerConfig{OnSettleAccepted: func(*Task, SettleSignal) bool { return true }, OnStartAccepted: func(tk *Task) bool { anchor.Store(tk.TTLAnchorMilli()); return true }})
	d := NewManualDetector()
	d.FireDetach()
	res := tm.Spawn(TaskSpec{Kind: "command", ResumeFn: func(context.Context, string) (SettleDetector, error) {
		next := NewManualDetector()
		next.FireDetach()
		return next, nil
	}}, d)
	d.Emit(SettleSignal{Kind: SettleStable, WaitSatisfied: true})
	waitStatus(t, res.Task, TaskStable)
	tm.now = func() time.Time { return base.Add(time.Minute) }
	_, err := tm.Resume(context.Background(), res.Task.ID, "input")
	require.NoError(t, err)
	require.Equal(t, res.Task.TTLAnchorMilli(), anchor.Load())
	require.Equal(t, base.Add(time.Minute).UnixMilli(), anchor.Load())
	require.NoError(t, tm.Close(context.Background()))
}

// TestRejectedBatchRetainsRawReceiptAndOriginalAcknowledgement checks rejection never clears delivery or releases the wrong producer.
func TestRejectedBatchRetainsRawReceiptAndOriginalAcknowledgement(t *testing.T) {
	var calls atomic.Int32
	raw := make(chan SettleSignal, 2)
	batch := make(chan SettleSignal, 1)
	tm := NewTaskManager(TaskManagerConfig{OnBatchRetireAccepted: func(items []BatchRetired) bool { batch <- items[0].Sig; return false }, OnSettleAccepted: func(_ *Task, sig SettleSignal) bool { raw <- sig; return calls.Add(1) > 1 }})
	d := newAdmittedTestDetector()
	d.FireDetach()
	res := tm.Spawn(TaskSpec{Kind: "command", WaitFor: WaitExit}, d)
	finish := tm.beginBatchRetire()
	tm.finalizeRetired(res.Task, "retired", nil)
	require.True(t, res.Task.HasPendingDelivery())
	finish()
	original := <-batch
	require.Equal(t, original, <-raw)
	select {
	case <-d.acks:
		t.Fatal("rejected batch acknowledged its producer")
	default:
	}
	select {
	case retry := <-raw:
		require.Equal(t, original, retry)
	case <-time.After(2 * time.Second):
		t.Fatal("batch rejection did not retain raw retry")
	}
	select {
	case ack := <-d.acks:
		require.Equal(t, original, ack)
	case <-time.After(time.Second):
		t.Fatal("accepted raw fallback did not acknowledge original producer")
	}
	require.NoError(t, tm.Close(context.Background()))
}

// TestRetireResendRetainsTerminalIdentity checks terminal replay reuses its recorded sequence and observation time after a refusal.
func TestRetireResendRetainsTerminalIdentity(t *testing.T) {
	resent := make(chan SettleSignal, 1)
	raw := make(chan SettleSignal, 2)
	var attempts atomic.Int32
	tm := NewTaskManager(TaskManagerConfig{OnRetireResendAccepted: func(items []BatchRetired) bool { resent <- items[0].Sig; return false }, OnSettleAccepted: func(_ *Task, sig SettleSignal) bool { raw <- sig; return attempts.Add(1) > 1 }})
	observed := time.Now().Add(-time.Minute)
	tk := tm.RestoreTask("terminal-resend", TaskSpec{Kind: "command", WaitFor: WaitExit, Declarative: &Declarative{RoundID: 3, ProducerRound: 3, SignalSeq: 5}}, observed.Add(-time.Minute), TaskCompleted)
	tk.RestoreSettlement("saved output", nil, observed)
	d := newAdmittedTestDetector()
	require.NoError(t, tm.BindDetector(tk.ID, d))
	tm.reconcileRetireNotify()
	first := <-resent
	require.EqualValues(t, 3, first.RoundID)
	require.EqualValues(t, 5, first.Seq)
	require.Equal(t, observed, first.ObservedAt)
	require.Equal(t, SignalSourceID(tk.ID, 3, 5), first.SourceID)
	require.Equal(t, first, <-raw)
	select {
	case retry := <-raw:
		require.Equal(t, first, retry)
	case <-time.After(2 * time.Second):
		t.Fatal("terminal resend lost its retry")
	}
	select {
	case ack := <-d.acks:
		require.Equal(t, first, ack)
	case <-time.After(time.Second):
		t.Fatal("terminal resend did not acknowledge receipt")
	}
	require.NoError(t, tm.Close(context.Background()))
}

// TestCancelTerminalTaskUsesRawAcceptedDelivery checks cancelling an existing terminal is recorded without depending on a legacy cancel hook.
func TestCancelTerminalTaskUsesRawAcceptedDelivery(t *testing.T) {
	calls := make(chan SettleSignal, 2)
	var attempts atomic.Int32
	tm := NewTaskManager(TaskManagerConfig{OnSettleAccepted: func(_ *Task, sig SettleSignal) bool { calls <- sig; return attempts.Add(1) > 1 }})
	tk := tm.RestoreTask("cancel-terminal", TaskSpec{Kind: "command", Declarative: &Declarative{RoundID: 2, ProducerRound: 2, SignalSeq: 4}}, time.Now(), TaskCompleted)
	require.True(t, tm.Cancel(tk.ID))
	first := <-calls
	require.Equal(t, "cancelled", first.Observation)
	require.Equal(t, LineageRetired, first.Lineage)
	require.EqualValues(t, 5, first.Seq)
	require.True(t, tk.HasPendingDelivery())
	require.False(t, tm.Cancel(tk.ID))
	select {
	case retry := <-calls:
		require.Equal(t, first, retry)
	case <-time.After(2 * time.Second):
		t.Fatal("cancel raw receipt was not retried")
	}
	require.Equal(t, TaskCancelled, tk.Status())
	require.NoError(t, tm.Close(context.Background()))
}
