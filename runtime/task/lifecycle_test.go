package task

import (
	"context"
	"errors"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

// invariantTaskAddress is the explicit address the invariants' tasks are declared from and delivered to.
var invariantTaskAddress = event.Address{AgentID: "agent-a", SessionID: "session-a"}

// TestLifecycleStateMachineInvariants pins the transition refusals and the obligation ledger of one task.
// - a terminal fact is recorded once, and every later transition is refused without storing a second fact
// - an undeterminable status, a re-admission and a wait that re-enters itself are each refused by name
// - a repeated mutation identity stores no second fact and adds no second obligation
// - an admission past the concurrency limit is refused and leaves the admitted tasks untouched
// - a running round and a cancelled task each refuse a resumed input by name
// - a repeated resume declaration opens no second round and adds no obligation
// - a dedup admission adds no obligation, and a settled task keeps its delivery obligation alone
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func TestLifecycleStateMachineInvariants(t *testing.T) {
	ctx := context.Background()

	t.Run("a terminal fact is recorded once and moves no more", func(t *testing.T) {
		registry, carrier := invariantRegistry(t)
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		if result, err := registry.Advance(ctx, taskID, StatusRunning, "running", nil); err != nil || result != CommitCommitted {
			t.Fatalf("move the task under way = (%v, %v), want a committed change", result, err)
		}
		settled := []Envelope{invariantEnvelope(taskID, NoticeSettled, 0)}
		if result, err := registry.Advance(ctx, taskID, StatusSettled, "settled", settled); err != nil || result != CommitCommitted {
			t.Fatalf("settle the task = (%v, %v), want a committed change", result, err)
		}
		stored := len(carrier.Facts(taskID))
		if _, err := registry.Advance(ctx, taskID, StatusFailed, "failed", nil); !errors.Is(err, ErrTerminalState) {
			t.Fatalf("a transition beside a terminal fact = %v, want %v", err, ErrTerminalState)
		}
		if _, err := registry.Advance(ctx, taskID, StatusSettled, "settled-again", settled); !errors.Is(err, ErrTerminalState) {
			t.Fatalf("a second terminal fact = %v, want %v", err, ErrTerminalState)
		}
		if got := len(carrier.Facts(taskID)); got != stored {
			t.Fatalf("facts after the refused transitions = %d, want the stored ones alone", got)
		}
		if _, status, err := registry.Lookup(ctx, taskID); err != nil || status != StatusSettled {
			t.Fatalf("status after the refused transitions = (%v, %v), want the settled fact", status, err)
		}
	})

	t.Run("an undeterminable status and a re-admission are refused by name", func(t *testing.T) {
		registry, carrier := invariantRegistry(t)
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		stored := len(carrier.Facts(taskID))
		if _, err := registry.Advance(ctx, taskID, StatusUnknown, "unknown", nil); !errors.Is(err, ErrStatusUnknown) {
			t.Fatalf("a transition to an undeterminable status = %v, want %v", err, ErrStatusUnknown)
		}
		if _, err := registry.Advance(ctx, taskID, StatusAccepted, "admitted-again", nil); !errors.Is(err, ErrIllegalTransition) {
			t.Fatalf("a transition back to admission = %v, want %v", err, ErrIllegalTransition)
		}
		if got := len(carrier.Facts(taskID)); got != stored {
			t.Fatalf("facts after the refused transitions = %d, want the stored one alone", got)
		}
	})

	t.Run("the approval wait is entered and left only from the statuses that allow it", func(t *testing.T) {
		registry, _ := invariantRegistry(t)
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		if result, err := registry.Advance(ctx, taskID, StatusAwaitingApproval, "park", nil); err != nil || result != CommitCommitted {
			t.Fatalf("park the admitted call in the wait = (%v, %v), want a committed change", result, err)
		}
		if _, err := registry.Advance(ctx, taskID, StatusAwaitingApproval, "park-again", nil); !errors.Is(err, ErrIllegalTransition) {
			t.Fatalf("re-entering the wait = %v, want %v", err, ErrIllegalTransition)
		}
		if _, err := registry.Advance(ctx, taskID, StatusRunning, "continue", nil); err != nil {
			t.Fatalf("leave the wait under way: %v", err)
		}
	})

	t.Run("a repeated mutation identity stores no second fact", func(t *testing.T) {
		registry, carrier := invariantRegistry(t)
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		if result, err := registry.Advance(ctx, taskID, StatusRunning, "running", nil); err != nil || result != CommitCommitted {
			t.Fatalf("move the task under way = (%v, %v), want a committed change", result, err)
		}
		stored := len(carrier.Facts(taskID))
		revision := carrier.Revision(taskID)
		if result, err := registry.Advance(ctx, taskID, StatusRunning, "running", nil); err != nil || result != CommitCommitted {
			t.Fatalf("repeat the mutation = (%v, %v), want the stored fact reported again", result, err)
		}
		if got := len(carrier.Facts(taskID)); got != stored {
			t.Fatalf("facts after a repeated mutation = %d, want the stored ones alone", got)
		}
		if got := carrier.Revision(taskID); got != revision {
			t.Fatalf("revision after a repeated mutation = %d, want it unmoved", got)
		}
		if _, err := registry.Advance(ctx, taskID, StatusSettled, "running", nil); !errors.Is(err, ErrMutationConflict) {
			t.Fatalf("one mutation identity beside another status = %v, want %v", err, ErrMutationConflict)
		}
	})

	t.Run("an admission past the concurrency limit is refused and leaves the admitted tasks untouched", func(t *testing.T) {
		carrier := NewMemoryCommit(nil)
		registry := NewMemoryRegistry(carrier, NewMemoryAdmission(), WithConcurrencyLimit(1))
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		receipt, err := registry.Register(ctx, invariantTaskSpec("call-2", "dedup-2"))
		if !errors.Is(err, ErrConcurrencyLimit) {
			t.Fatalf("an admission past the limit = %v, want %v", err, ErrConcurrencyLimit)
		}
		if receipt.Accepted {
			t.Fatalf("an admission past the limit = %+v, want it not accepted", receipt)
		}
		dedup, err := registry.Register(ctx, invariantTaskSpec("call-1", "dedup-1"))
		if err != nil || dedup.Kind != AdmissionDedup || dedup.TaskID != taskID {
			t.Fatalf("a dedup admission at the limit = (%+v, %v), want the admitted task reported", dedup, err)
		}
		if _, status, err := registry.Lookup(ctx, taskID); err != nil || status != StatusAccepted {
			t.Fatalf("status of the admitted task = (%v, %v), want admission untouched", status, err)
		}
		if got := len(carrier.Facts(taskID)); got != 1 {
			t.Fatalf("facts of the admitted task = %d, want its admission alone", got)
		}
		if _, err := registry.Advance(ctx, taskID, StatusSettled, "settled", nil); err != nil {
			t.Fatalf("settle the admitted task: %v", err)
		}
		if receipt, err := registry.Register(ctx, invariantTaskSpec("call-3", "dedup-3")); err != nil || receipt.Kind != AdmissionFresh {
			t.Fatalf("admission after the limit cleared = (%+v, %v), want a fresh admission", receipt, err)
		}
	})

	t.Run("a running round and a cancelled task each refuse a resumed input by name", func(t *testing.T) {
		registry, _ := invariantRegistry(t)
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		if _, err := registry.Resume(ctx, taskID, event.Event{}); !errors.Is(err, ErrInputMissing) {
			t.Fatalf("a resume carrying no input = %v, want %v", err, ErrInputMissing)
		}
		if _, err := registry.Advance(ctx, taskID, StatusRunning, "running", nil); err != nil {
			t.Fatalf("move the task under way: %v", err)
		}
		if _, err := registry.Resume(ctx, taskID, event.Event{Key: "input-1"}); !errors.Is(err, ErrRoundInFlight) {
			t.Fatalf("a resume beside a round under way = %v, want %v", err, ErrRoundInFlight)
		}
		if _, err := registry.Advance(ctx, taskID, StatusCancelled, "cancelled", nil); err != nil {
			t.Fatalf("cancel the task: %v", err)
		}
		if _, err := registry.Resume(ctx, taskID, event.Event{Key: "input-2"}); !errors.Is(err, ErrResumeCancelled) {
			t.Fatalf("a resume of a cancelled task = %v, want %v", err, ErrResumeCancelled)
		}
	})

	t.Run("a repeated resume declaration opens no second round", func(t *testing.T) {
		registry, carrier := invariantRegistry(t)
		manager := NewMemoryManager(registry, nil)
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		opened := invariantObligationCount(t, manager, taskID)
		if receipt, err := registry.Resume(ctx, taskID, event.Event{Key: "input-1"}); err != nil || receipt.Kind != AdmissionFresh {
			t.Fatalf("resume the task under a declared input = (%+v, %v), want a fresh admission", receipt, err)
		}
		if got := invariantObligationCount(t, manager, taskID); got != opened+1 {
			t.Fatalf("obligations after the resume = %d, want the resumed round's one beside %d", got, opened)
		}
		envelope, err := registry.IssueNotice(ctx, taskID, NoticeAccepted, "fact-1")
		if err != nil {
			t.Fatalf("issue a notification of the resumed round: %v", err)
		}
		if got := envelope.Notice.Round; got != 2 {
			t.Fatalf("round of the resumed notification = %d, want the resumed round", got)
		}
		revision := carrier.Revision(taskID)
		if receipt, err := registry.Resume(ctx, taskID, event.Event{Key: "input-1"}); err != nil || receipt.Kind != AdmissionFresh {
			t.Fatalf("repeat the resume declaration = (%+v, %v), want the stored fact reported", receipt, err)
		}
		if got := invariantObligationCount(t, manager, taskID); got != opened+1 {
			t.Fatalf("obligations after a repeated resume = %d, want no second one", got)
		}
		if got := carrier.Revision(taskID); got != revision {
			t.Fatalf("revision after a repeated resume = %d, want it unmoved", got)
		}
		again, err := registry.IssueNotice(ctx, taskID, NoticeAccepted, "fact-2")
		if err != nil {
			t.Fatalf("issue a second notification: %v", err)
		}
		if got := again.Notice.Round; got != 2 {
			t.Fatalf("round of the notification issued after a repeated resume = %d, want no third round", got)
		}
	})

	t.Run("a dedup admission adds no obligation and a settled task keeps its delivery obligation alone", func(t *testing.T) {
		registry, _ := invariantRegistry(t)
		manager := NewMemoryManager(registry, nil)
		first, err := registry.Register(ctx, invariantTaskSpec("call-1", "dedup-1"))
		if err != nil || first.Kind != AdmissionFresh {
			t.Fatalf("admit the task = (%+v, %v), want a fresh admission", first, err)
		}
		held, err := manager.Obligations(ctx, first.TaskID)
		if err != nil {
			t.Fatalf("read the obligations of the admitted task: %v", err)
		}
		if held.Count != 1 || len(held.Open) != 1 || held.Open[0].Kind != ObligationAwaitingTool {
			t.Fatalf("obligations of the admitted task = %+v, want the admission's own one", held)
		}
		dedup, err := registry.Register(ctx, invariantTaskSpec("call-1", "dedup-1"))
		if err != nil || dedup.Kind != AdmissionDedup || dedup.TaskID != first.TaskID {
			t.Fatalf("repeat the admission = (%+v, %v), want the admitted task reported as a dedup", dedup, err)
		}
		if got, err := manager.Obligations(ctx, first.TaskID); err != nil || got.Count != 1 {
			t.Fatalf("obligations after a dedup admission = (%+v, %v), want no second obligation", got, err)
		}
		envelope := invariantEnvelope(first.TaskID, NoticeSettled, 0)
		if _, err := registry.Advance(ctx, first.TaskID, StatusSettled, "settled", []Envelope{envelope}); err != nil {
			t.Fatalf("settle the task beside its notification: %v", err)
		}
		settled, err := manager.Obligations(ctx, first.TaskID)
		if err != nil {
			t.Fatalf("read the obligations of the settled task: %v", err)
		}
		if settled.Count != 1 || len(settled.Open) != 1 {
			t.Fatalf("obligations of the settled task = %+v, want the delivery obligation alone", settled)
		}
		if got := settled.Open[0]; got.Kind != ObligationAwaitingDelivery || got.Notice != envelope.Notice {
			t.Fatalf("obligation of the settled task = %+v, want the committed notification's delivery", got)
		}
	})
}

// invariantRegistry returns one registry over a fresh in-memory commit carrier.
func invariantRegistry(t *testing.T) (*MemoryRegistry, *MemoryCommit) {
	t.Helper()
	carrier := NewMemoryCommit(nil)
	return NewMemoryRegistry(carrier, NewMemoryAdmission()), carrier
}

// invariantTaskSpec builds one task declaration of an explicitly named call.
func invariantTaskSpec(callID, dedup string) Spec {
	return Spec{
		CallID:       callID,
		DedupKey:     DedupKey(dedup),
		InvocationID: "invocation-1",
		InstanceID:   "instance-a",
		Description:  "one admitted call",
		Source:       invariantTaskAddress,
		Target:       invariantTaskAddress,
		Execution:    Serialization("session-serialized"),
	}
}

// invariantRegister admits one fresh task and returns the identity the registry assigned.
func invariantRegister(t *testing.T, registry *MemoryRegistry, callID, dedup string) event.TaskID {
	t.Helper()
	receipt, err := registry.Register(context.Background(), invariantTaskSpec(callID, dedup))
	if err != nil {
		t.Fatalf("admit the task of %s: %v", callID, err)
	}
	if receipt.Kind != AdmissionFresh {
		t.Fatalf("admission of %s = %+v, want a fresh admission", callID, receipt)
	}
	return receipt.TaskID
}

// invariantEnvelope builds one notification of the named task with the routing the declaration froze.
func invariantEnvelope(taskID event.TaskID, kind NoticeKind, seq int64) Envelope {
	return Envelope{
		Notice:         Notice{TaskID: taskID, Round: 1, Kind: kind, Seq: seq, Event: "fact-1"},
		Target:         invariantTaskAddress,
		Source:         invariantTaskAddress,
		TargetInstance: "instance-a",
	}
}

// invariantObligationCount reports the obligations the manager still holds for one task.
func invariantObligationCount(t *testing.T, manager *MemoryManager, taskID event.TaskID) int {
	t.Helper()
	held, err := manager.Obligations(context.Background(), taskID)
	if err != nil {
		t.Fatalf("read the obligations of %s: %v", taskID, err)
	}
	return held.Count
}
