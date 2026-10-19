package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// invariantShutdownAddress is the address the invariants' close names.
var invariantShutdownAddress = event.Address{AgentID: "agent-a", SessionID: "session-a"}

// TestShutdownDrainInvariants pins the gate, the drain and the honest report of one close.
// - the gate refuses new input before the first settlement pass, and no admission happens beside the drain
// - an accepted obligation is settled only by its real settlement, and the drain ends when it clears
// - a producer that has not confirmed its stop keeps the drain open and is named beside it
// - a producer that confirms its stop lets the drain end with no unsettled obligation
// - a second close reports the first result and runs no second settlement
// - a booking still held by the ledger keeps the drain open
// - a watch naming no work identity or no producer is refused by name
// - only a fresh admission is booked beside the close, so a dedup adds no task to drain
// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
func TestShutdownDrainInvariants(t *testing.T) {
	ctx := context.Background()

	t.Run("the gate refuses new input before the first settlement pass", func(t *testing.T) {
		face := newInvariantShutdownTasks()
		face.hold("task-1", 1)
		close := NewShutdown(face)
		close.Track("task-1")
		report, err := close.Close(ctx, invariantCloseRequest(40*time.Millisecond))
		if !errors.Is(err, ErrDrainUnsettled) {
			t.Fatalf("close beside an open obligation = %v, want %v", err, ErrDrainUnsettled)
		}
		if report.Drained {
			t.Fatalf("close report = %+v, want it never drained beside an open obligation", report)
		}
		if len(report.Unsettled) != 1 || report.Unsettled[0].TaskID != "task-1" {
			t.Fatalf("unsettled obligations = %+v, want the open one named", report.Unsettled)
		}
		receipt, err := close.Admit(ctx, invariantShutdownSpec("call-2"))
		if !errors.Is(err, ErrShutdownRefused) {
			t.Fatalf("input offered after the close began = %v, want %v", err, ErrShutdownRefused)
		}
		if receipt.Accepted {
			t.Fatalf("input offered after the close began = %+v, want it refused", receipt)
		}
		if got := face.admitCount(); got != 0 {
			t.Fatalf("admissions beside the drain = %d, want none", got)
		}
	})

	t.Run("an accepted obligation is settled only by its real settlement", func(t *testing.T) {
		face := newInvariantShutdownTasks()
		face.hold("task-1", 1)
		close := NewShutdown(face)
		close.Track("task-1")
		go func() {
			time.Sleep(10 * time.Millisecond)
			face.settle("task-1")
		}()
		report, err := close.Close(ctx, invariantCloseRequest(2*time.Second))
		if err != nil {
			t.Fatalf("close beside an obligation that clears = %v, want a drained close", err)
		}
		if !report.Drained || len(report.Unsettled) != 0 {
			t.Fatalf("close report = %+v, want the drain ended beside the real settlement", report)
		}
		if got := face.flushCount(); got == 0 {
			t.Fatal("the drain flushed no notification of the task it settled")
		}
	})

	t.Run("a producer that has not confirmed its stop keeps the drain open", func(t *testing.T) {
		face := newInvariantShutdownTasks()
		close := NewShutdown(face)
		polls := &invariantPollCount{}
		if err := close.Watch("task-1", polls.stopped); err != nil {
			t.Fatalf("watch the producer: %v", err)
		}
		report, err := close.Close(ctx, invariantCloseRequest(40*time.Millisecond))
		if !errors.Is(err, ErrDrainUnsettled) {
			t.Fatalf("close beside a producer still running = %v, want %v", err, ErrDrainUnsettled)
		}
		if report.Drained {
			t.Fatal("a close reported a drain beside a producer that has not stopped")
		}
		named := false
		for _, open := range report.Unsettled {
			if open.TaskID == "task-1" && open.Reason == reasonProducerNotStopped {
				named = true
			}
		}
		if !named {
			t.Fatalf("unsettled report = %+v, want the unconfirmed producer named beside its task", report.Unsettled)
		}
		if got := polls.count(); got < 2 {
			t.Fatalf("stop confirmations polled = %d, want the close to keep asking instead of assuming", got)
		}
	})

	t.Run("a producer that confirms its stop lets the drain end", func(t *testing.T) {
		face := newInvariantShutdownTasks()
		close := NewShutdown(face)
		polls := &invariantPollCount{}
		close.Watch("task-1", polls.stopped)
		go func() {
			time.Sleep(10 * time.Millisecond)
			polls.stop()
		}()
		report, err := close.Close(ctx, invariantCloseRequest(2*time.Second))
		if err != nil {
			t.Fatalf("close beside a producer that stops = %v, want a drained close", err)
		}
		if !report.Drained || len(report.Unsettled) != 0 {
			t.Fatalf("close report = %+v, want the drain ended beside the confirmed stop", report)
		}
	})

	t.Run("a second close reports the first result and runs no second settlement", func(t *testing.T) {
		face := newInvariantShutdownTasks()
		close := NewShutdown(face)
		reconciler := &invariantReconciler{}
		close.SetReconciler(reconciler)
		close.Track("task-1")
		first, err := close.Close(ctx, invariantCloseRequest(2*time.Second))
		if err != nil || !first.Drained {
			t.Fatalf("first close = (%+v, %v), want a drained close", first, err)
		}
		flushed := face.flushCount()
		landed := reconciler.landingCount()
		second, err := close.Close(ctx, invariantCloseRequest(2*time.Second))
		if err != nil {
			t.Fatalf("second close = %v, want the first result reported again", err)
		}
		if !second.Drained || len(second.Unsettled) != len(first.Unsettled) {
			t.Fatalf("second close = %+v, want the first report %+v", second, first)
		}
		if got := face.flushCount(); got != flushed {
			t.Fatalf("flushes after a second close = %d, want no second settlement", got)
		}
		if got := reconciler.landingCount(); got != landed {
			t.Fatalf("landings after a second close = %d, want no second settlement", got)
		}
	})

	t.Run("a booking still held by the ledger keeps the drain open", func(t *testing.T) {
		face := newInvariantShutdownTasks()
		close := NewShutdown(face)
		reconciler := &invariantReconciler{pending: 1}
		close.SetReconciler(reconciler)
		close.Track("task-1")
		report, err := close.Close(ctx, invariantCloseRequest(40*time.Millisecond))
		if !errors.Is(err, ErrDrainUnsettled) {
			t.Fatalf("close beside a held booking = %v, want %v", err, ErrDrainUnsettled)
		}
		if report.Drained {
			t.Fatal("a close reported a drain beside a terminal it has not landed")
		}
		if got := reconciler.pendingCount(); got != 1 {
			t.Fatalf("bookings held by the ledger = %d, want the unlanded one kept", got)
		}
	})

	t.Run("a watch naming no work identity or no producer is refused by name", func(t *testing.T) {
		close := NewShutdown(newInvariantShutdownTasks())
		if err := close.Watch("", func() bool { return true }); !errors.Is(err, ErrProducerIdentityMissing) {
			t.Fatalf("watch under no task identity = %v, want %v", err, ErrProducerIdentityMissing)
		}
		if err := close.Watch("task-1", nil); !errors.Is(err, ErrProducerMissing) {
			t.Fatalf("watch naming no producer = %v, want %v", err, ErrProducerMissing)
		}
	})

	t.Run("only a fresh admission is booked beside the close", func(t *testing.T) {
		face := newInvariantShutdownTasks()
		face.receipt = task.AdmissionReceipt{CallID: "call-1", TaskID: "task-1", Accepted: true, Kind: task.AdmissionDedup}
		close := NewShutdown(face)
		if _, err := close.Admit(ctx, invariantShutdownSpec("call-1")); err != nil {
			t.Fatalf("admit a repeated call: %v", err)
		}
		if got := invariantBooked(t, close); got != 0 {
			t.Fatalf("booked tasks after a dedup admission = %d, want none", got)
		}
		face.deliverFresh(task.AdmissionReceipt{CallID: "call-2", TaskID: "task-2", Accepted: true, Kind: task.AdmissionFresh})
		if _, err := close.Admit(ctx, invariantShutdownSpec("call-2")); err != nil {
			t.Fatalf("admit a fresh call: %v", err)
		}
		if got := invariantBooked(t, close); got != 1 {
			t.Fatalf("booked tasks after a fresh admission = %d, want the one obligation in the drain", got)
		}
	})
}

// invariantCloseRequest builds one close request spending the deadline named.
func invariantCloseRequest(wait time.Duration) CloseRequest {
	return CloseRequest{Address: invariantShutdownAddress, Deadline: time.Now().Add(wait)}
}

// invariantShutdownSpec builds one delegated call declaration.
func invariantShutdownSpec(callID string) task.Spec {
	return task.Spec{
		CallID:       callID,
		DedupKey:     task.DedupKey(callID),
		InvocationID: "invocation-1",
		InstanceID:   "instance-a",
		Description:  "one delegated call",
		Source:       invariantShutdownAddress,
		Target:       invariantShutdownAddress,
		Execution:    task.Serialization("session-serialized"),
	}
}

// invariantBooked reports how many tasks the close books for its drain.
func invariantBooked(t *testing.T, close *Shutdown) int {
	t.Helper()
	close.mu.Lock()
	defer close.mu.Unlock()
	return len(close.admitted)
}

// invariantPollCount is one producer stop confirmation that counts how often it was asked.
type invariantPollCount struct {
	mu        sync.Mutex
	polls     int
	confirmed bool
}

// stopped answers the poll, reporting whether the producer really stopped.
func (p *invariantPollCount) stopped() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.polls++
	return p.confirmed
}

// stop confirms the real stop of the producer.
func (p *invariantPollCount) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.confirmed = true
}

// count reports how often the stop was polled.
func (p *invariantPollCount) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.polls
}

// invariantShutdownTasks is the task face of the invariants: the obligations it still owes and the admissions it answers.
type invariantShutdownTasks struct {
	mu      sync.Mutex
	open    map[event.TaskID]int
	receipt task.AdmissionReceipt
	admits  int
	flushes int
}

// newInvariantShutdownTasks returns a task face owing nothing.
func newInvariantShutdownTasks() *invariantShutdownTasks {
	return &invariantShutdownTasks{open: map[event.TaskID]int{}}
}

// Admit answers the admission receipt the face was made to report.
func (f *invariantShutdownTasks) Admit(context.Context, task.Spec) (task.AdmissionReceipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.admits++
	return f.receipt, nil
}

// Advance reports the transition committed.
func (f *invariantShutdownTasks) Advance(context.Context, event.TaskID, task.Status, task.MutationID, []task.Envelope) (task.CommitResult, error) {
	return task.CommitCommitted, nil
}

// Obligations reports the obligations the face still owes for one task.
func (f *invariantShutdownTasks) Obligations(_ context.Context, taskID event.TaskID) (task.Obligations, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := f.open[taskID]
	out := task.Obligations{Count: count}
	for i := 0; i < count; i++ {
		out.Open = append(out.Open, task.Obligation{Kind: task.ObligationAwaitingDelivery, TaskID: taskID})
	}
	return out, nil
}

// Flush counts one offer of the task's notifications.
func (f *invariantShutdownTasks) Flush(context.Context, event.TaskID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flushes++
	return nil
}

// hold records the obligations one task still owes.
func (f *invariantShutdownTasks) hold(taskID event.TaskID, count int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.open[taskID] = count
}

// settle records the real settlement of one task's obligations.
func (f *invariantShutdownTasks) settle(taskID event.TaskID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.open[taskID] = 0
}

// deliverFresh makes the face answer the receipt named from now on.
func (f *invariantShutdownTasks) deliverFresh(receipt task.AdmissionReceipt) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.receipt = receipt
}

// admitCount reports how many admissions the face answered.
func (f *invariantShutdownTasks) admitCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.admits
}

// flushCount reports how many flushes the drain offered.
func (f *invariantShutdownTasks) flushCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.flushes
}

// invariantReconciler is the delegation ledger of the invariants: it counts the terminals it landed and the bookings it holds.
type invariantReconciler struct {
	mu       sync.Mutex
	pending  int
	landings int
}

// Reconcile counts one landing of a terminal.
func (r *invariantReconciler) Reconcile(context.Context, event.TaskID) (task.Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.landings++
	return task.StatusSettled, nil
}

// Pending reports how many bookings the ledger still holds.
func (r *invariantReconciler) Pending() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pending
}

// landingCount reports how many terminals the ledger landed.
func (r *invariantReconciler) landingCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.landings
}

// pendingCount reports how many bookings the ledger still holds.
func (r *invariantReconciler) pendingCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pending
}
