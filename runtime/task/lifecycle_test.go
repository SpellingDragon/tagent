package task

import (
	"context"
	"errors"
	"github.com/SpellingDragon/tagent/api/event"
	"strings"
	"sync"
	"testing"
	"time"
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
		registry, commit := invariantRegistry(t)
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		if result, err := registry.Advance(ctx, taskID, StatusRunning, "running", nil); err != nil || result != CommitCommitted {
			t.Fatalf("move the task under way = (%v, %v), want a committed change", result, err)
		}
		settled := []Envelope{invariantEnvelope(taskID, NoticeSettled, 0)}
		if result, err := registry.Advance(ctx, taskID, StatusSettled, "settled", settled); err != nil || result != CommitCommitted {
			t.Fatalf("settle the task = (%v, %v), want a committed change", result, err)
		}
		stored := len(commit.Facts(taskID))
		if _, err := registry.Advance(ctx, taskID, StatusFailed, "failed", nil); !errors.Is(err, ErrTerminalState) {
			t.Fatalf("a transition beside a terminal fact = %v, want %v", err, ErrTerminalState)
		}
		if _, err := registry.Advance(ctx, taskID, StatusSettled, "settled-again", settled); !errors.Is(err, ErrTerminalState) {
			t.Fatalf("a second terminal fact = %v, want %v", err, ErrTerminalState)
		}
		if got := len(commit.Facts(taskID)); got != stored {
			t.Fatalf("facts after the refused transitions = %d, want the stored ones alone", got)
		}
		if _, status, err := registry.Lookup(ctx, taskID); err != nil || status != StatusSettled {
			t.Fatalf("status after the refused transitions = (%v, %v), want the settled fact", status, err)
		}
	})

	t.Run("an undeterminable status and a re-admission are refused by name", func(t *testing.T) {
		registry, commit := invariantRegistry(t)
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		stored := len(commit.Facts(taskID))
		if _, err := registry.Advance(ctx, taskID, StatusUnknown, "unknown", nil); !errors.Is(err, ErrStatusUnknown) {
			t.Fatalf("a transition to an undeterminable status = %v, want %v", err, ErrStatusUnknown)
		}
		if _, err := registry.Advance(ctx, taskID, StatusAccepted, "admitted-again", nil); !errors.Is(err, ErrIllegalTransition) {
			t.Fatalf("a transition back to admission = %v, want %v", err, ErrIllegalTransition)
		}
		if got := len(commit.Facts(taskID)); got != stored {
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
		registry, commit := invariantRegistry(t)
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		if result, err := registry.Advance(ctx, taskID, StatusRunning, "running", nil); err != nil || result != CommitCommitted {
			t.Fatalf("move the task under way = (%v, %v), want a committed change", result, err)
		}
		stored := len(commit.Facts(taskID))
		revision := commit.Revision(taskID)
		if result, err := registry.Advance(ctx, taskID, StatusRunning, "running", nil); err != nil || result != CommitCommitted {
			t.Fatalf("repeat the mutation = (%v, %v), want the stored fact reported again", result, err)
		}
		if got := len(commit.Facts(taskID)); got != stored {
			t.Fatalf("facts after a repeated mutation = %d, want the stored ones alone", got)
		}
		if got := commit.Revision(taskID); got != revision {
			t.Fatalf("revision after a repeated mutation = %d, want it unmoved", got)
		}
		if _, err := registry.Advance(ctx, taskID, StatusSettled, "running", nil); !errors.Is(err, ErrMutationConflict) {
			t.Fatalf("one mutation identity beside another status = %v, want %v", err, ErrMutationConflict)
		}
	})

	t.Run("an admission past the concurrency limit is refused and leaves the admitted tasks untouched", func(t *testing.T) {
		commit := NewMemoryCommit(nil)
		registry := NewMemoryRegistry(commit, NewMemoryAdmission(), WithConcurrencyLimit(1))
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
		if got := len(commit.Facts(taskID)); got != 1 {
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
		registry, commit := invariantRegistry(t)
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
		revision := commit.Revision(taskID)
		if receipt, err := registry.Resume(ctx, taskID, event.Event{Key: "input-1"}); err != nil || receipt.Kind != AdmissionFresh {
			t.Fatalf("repeat the resume declaration = (%+v, %v), want the stored fact reported", receipt, err)
		}
		if got := invariantObligationCount(t, manager, taskID); got != opened+1 {
			t.Fatalf("obligations after a repeated resume = %d, want no second one", got)
		}
		if got := commit.Revision(taskID); got != revision {
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

// invariantRegistry returns one registry over a fresh in-memory commit.
func invariantRegistry(t *testing.T) (*MemoryRegistry, *MemoryCommit) {
	t.Helper()
	commit := NewMemoryCommit(nil)
	return NewMemoryRegistry(commit, NewMemoryAdmission()), commit
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

// TestReentryRefusalAndStaleSignals pins the refusal face of a re-entry and the round-local life of the detector's verdict.
// - a round still in flight refuses the re-entry by name and leaves the task as it stands
// - a round reported as suspected is still a round in flight: the observation never opens a re-entry of its own
// - the named refusals guide the caller to the way out (wait for the terminal, relaunch or re-spawn)
// - the verdict the detector read over the round before is not carried into the new round
// - the new round is classified by its own silence, so its suspicion is its own and arrives only when its own threshold passes
// 契约: docs/wiki/agent/task-lifecycle.md#detach-suppression
func TestReentryRefusalAndStaleSignals(t *testing.T) {
	ctx := context.Background()

	t.Run("rejects_inflight_reentry", func(t *testing.T) {
		registry, commit := invariantRegistry(t)
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		if result, err := registry.Advance(ctx, taskID, StatusRunning, "running", nil); err != nil || result != CommitCommitted {
			t.Fatalf("move the task under way = (%v, %v), want a committed change", result, err)
		}
		stored := len(commit.Facts(taskID))
		if _, err := registry.Reactivate(ctx, taskID); !errors.Is(err, ErrRoundInFlight) {
			t.Fatalf("re-entering a task whose round is in flight = %v, want %v", err, ErrRoundInFlight)
		}
		if _, status, err := registry.Lookup(ctx, taskID); err != nil || status != StatusRunning {
			t.Fatalf("status after the refused re-entry = (%v, %v), want the round already running", status, err)
		}
		if got := len(commit.Facts(taskID)); got != stored {
			t.Fatalf("facts after the refused re-entry = %d, want the stored ones alone", got)
		}
		if result, err := registry.Advance(ctx, taskID, StatusSettled, "settled", []Envelope{invariantEnvelope(taskID, NoticeSettled, 0)}); err != nil || result != CommitCommitted {
			t.Fatalf("settle the round = (%v, %v), want a committed change", result, err)
		}
		if round, err := registry.Reactivate(ctx, taskID); err != nil || round != 2 {
			t.Fatalf("re-entering the task once its round settled = (%d, %v), want the second round", round, err)
		}
	})

	t.Run("a suspected round is still a round in flight", func(t *testing.T) {
		registry := logicalRegistry(logicalThresholds).registry
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		if result, err := registry.Advance(ctx, taskID, StatusRunning, "running", nil); err != nil || result != CommitCommitted {
			t.Fatalf("move the task under way = (%v, %v), want a committed change", result, err)
		}
		probe := time.Now().Add(logicalThresholds.SuspectAfter + logicalMargin)
		if got, err := registry.Logical(ctx, taskID, probe); err != nil || got != LogicalSuspect {
			t.Fatalf("logical status of the silent round = (%q, %v), want %q", got, err, LogicalSuspect)
		}
		if _, err := registry.Reactivate(ctx, taskID); !errors.Is(err, ErrRoundInFlight) {
			t.Fatalf("re-entering a suspected round = %v, want %v", err, ErrRoundInFlight)
		}
		if _, status, err := registry.Lookup(ctx, taskID); err != nil || status != StatusRunning {
			t.Fatalf("status after the refused re-entry = (%v, %v), want the round the observation never moved", status, err)
		}
	})

	t.Run("the named refusals guide the caller", func(t *testing.T) {
		for _, one := range []struct {
			err     error
			guides  string
			subject string
		}{
			{ErrReactivationFailed, "relaunch", "a failed task"},
			{ErrReactivationCancelled, "re-spawn", "a cancelled task"},
			{ErrRoundInFlight, "in flight", "a round under way"},
		} {
			if !strings.Contains(one.err.Error(), one.guides) {
				t.Fatalf("the refusal of %s = %q, want it to name %q", one.subject, one.err.Error(), one.guides)
			}
		}
	})

	t.Run("stale_signal_ignored", func(t *testing.T) {
		registry := logicalRegistry(logicalThresholds).registry
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		if result, err := registry.Advance(ctx, taskID, StatusRunning, "running", nil); err != nil || result != CommitCommitted {
			t.Fatalf("move the task under way = (%v, %v), want a committed change", result, err)
		}
		probe := time.Now().Add(logicalThresholds.SuspectAfter + logicalMargin)
		if changes := registry.Watch(ctx, probe); len(changes) != 1 || changes[0].To != LogicalSuspect {
			t.Fatalf("changes the old round was reported under = %+v, want the one suspected verdict", changes)
		}
		if result, err := registry.Advance(ctx, taskID, StatusSettled, "settled", []Envelope{invariantEnvelope(taskID, NoticeSettled, 0)}); err != nil || result != CommitCommitted {
			t.Fatalf("settle the round = (%v, %v), want a committed change", result, err)
		}
		if _, err := registry.Reactivate(ctx, taskID); err != nil {
			t.Fatalf("re-enter the settled task: %v", err)
		}
		if changes := registry.Watch(ctx, time.Now()); len(changes) != 0 {
			t.Fatalf("changes the new round was reported under = %+v, want none: the old round's verdict belongs to the round it was read over", changes)
		}
		if got, err := registry.Logical(ctx, taskID, time.Now()); err != nil || got != LogicalActive {
			t.Fatalf("logical status of the new round = (%q, %v), want %q", got, err, LogicalActive)
		}
	})

	t.Run("the new round is classified by its own silence", func(t *testing.T) {
		registry := logicalRegistry(logicalThresholds).registry
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		if result, err := registry.Advance(ctx, taskID, StatusRunning, "running", nil); err != nil || result != CommitCommitted {
			t.Fatalf("move the task under way = (%v, %v), want a committed change", result, err)
		}
		if changes := registry.Watch(ctx, time.Now().Add(logicalThresholds.SuspectAfter+logicalMargin)); len(changes) != 1 {
			t.Fatalf("changes the old round was reported under = %+v, want the one suspected verdict", changes)
		}
		if result, err := registry.Advance(ctx, taskID, StatusSettled, "settled", []Envelope{invariantEnvelope(taskID, NoticeSettled, 0)}); err != nil || result != CommitCommitted {
			t.Fatalf("settle the round = (%v, %v), want a committed change", result, err)
		}
		if _, err := registry.Reactivate(ctx, taskID); err != nil {
			t.Fatalf("re-enter the settled task: %v", err)
		}
		if changes := registry.Watch(ctx, time.Now()); len(changes) != 0 {
			t.Fatalf("changes at the opening of the new round = %+v, want none", changes)
		}
		if changes := registry.Watch(ctx, time.Now().Add(logicalThresholds.SuspectAfter+logicalMargin)); len(changes) != 1 || changes[0].To != LogicalSuspect {
			t.Fatalf("changes the new round reached on its own = %+v, want the one suspected verdict of the new round", changes)
		}
	})
}

// TestTerminalTTLPrunesTheAccount pins the terminal TTL of one registry: task_terminal_ttl read as the bound of the in-memory account, never of the fact chain.
// - a terminal record older than the bound is dropped by a sweep, and a call naming it answers ErrTaskNotFound
// - a terminal record inside the bound is kept, and a task in flight is never dropped however long it stands
// - an admission sweeps beside the record it adds, so an account that keeps admitting stays bounded
// - a task whose record was pruned is admitted fresh again instead of answering the record that left
// - a registry built beside no bound keeps every record it ever admitted
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func TestTerminalTTLPrunesTheAccount(t *testing.T) {
	ctx := context.Background()
	const ttl = time.Hour
	start := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

	t.Run("a_terminal_record_past_its_bound_is_dropped", func(t *testing.T) {
		registry, _, clock := ttlRegistry(t, ttl, start)
		taskID := ttlSettled(t, registry, "call-1", "dedup-1")
		clock.advance(ttl / 2)
		if dropped := registry.PruneTerminal(clock.now()); dropped != 0 {
			t.Fatalf("records dropped inside the bound = %d, want none", dropped)
		}
		if _, _, err := registry.Lookup(ctx, taskID); err != nil {
			t.Fatalf("lookup of a terminal task inside its bound = %v, want the record it keeps", err)
		}
		clock.advance(ttl)
		if dropped := registry.PruneTerminal(clock.now()); dropped != 1 {
			t.Fatalf("records dropped past the bound = %d, want the one terminal record", dropped)
		}
		if _, _, err := registry.Lookup(ctx, taskID); !errors.Is(err, ErrTaskNotFound) {
			t.Fatalf("lookup of a pruned task = %v, want %v", err, ErrTaskNotFound)
		}
		if err := registry.Reentrant(ctx, taskID); !errors.Is(err, ErrTaskNotFound) {
			t.Fatalf("re-entry of a pruned task = %v, want %v", err, ErrTaskNotFound)
		}
	})

	t.Run("a_round_in_flight_is_never_dropped", func(t *testing.T) {
		registry, _, clock := ttlRegistry(t, ttl, start)
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		if result, err := registry.Advance(ctx, taskID, StatusRunning, "running", nil); err != nil || result != CommitCommitted {
			t.Fatalf("move the task under way = (%v, %v), want a committed change", result, err)
		}
		clock.advance(10 * ttl)
		if dropped := registry.PruneTerminal(clock.now()); dropped != 0 {
			t.Fatalf("records dropped beside a round in flight = %d, want none", dropped)
		}
		if _, status, err := registry.Lookup(ctx, taskID); err != nil || status != StatusRunning {
			t.Fatalf("the round in flight after the sweep = (%v, %v), want the round it stands in", status, err)
		}
	})

	t.Run("an_admission_sweeps_beside_the_record_it_adds", func(t *testing.T) {
		registry, _, clock := ttlRegistry(t, ttl, start)
		first := ttlSettled(t, registry, "call-1", "dedup-1")
		clock.advance(2 * ttl)
		second := invariantRegister(t, registry, "call-2", "dedup-2")
		if _, _, err := registry.Lookup(ctx, first); !errors.Is(err, ErrTaskNotFound) {
			t.Fatalf("lookup of the settled task after the next admission = %v, want %v", err, ErrTaskNotFound)
		}
		if _, status, err := registry.Lookup(ctx, second); err != nil || status != StatusAccepted {
			t.Fatalf("the task the admission added = (%v, %v), want the accepted task it registered", status, err)
		}
	})

	t.Run("a_pruned_task_is_admitted_fresh_again", func(t *testing.T) {
		registry, _, clock := ttlRegistry(t, ttl, start)
		first := ttlSettled(t, registry, "call-1", "dedup-1")
		clock.advance(2 * ttl)
		registry.PruneTerminal(clock.now())
		receipt, err := registry.Register(ctx, invariantTaskSpec("call-1", "dedup-1"))
		if err != nil {
			t.Fatalf("admit the pruned call again: %v", err)
		}
		if receipt.Kind != AdmissionFresh || receipt.TaskID == first {
			t.Fatalf("admission of the pruned call = %+v, want a fresh admission: the dedup index holds no record that left the account", receipt)
		}
	})

	t.Run("a_registry_beside_no_bound_keeps_every_record", func(t *testing.T) {
		registry, _, clock := ttlRegistry(t, 0, start)
		taskID := ttlSettled(t, registry, "call-1", "dedup-1")
		clock.advance(100 * ttl)
		if dropped := registry.PruneTerminal(clock.now()); dropped != 0 {
			t.Fatalf("records dropped beside no bound = %d, want none", dropped)
		}
		if _, status, err := registry.Lookup(ctx, taskID); err != nil || status != StatusSettled {
			t.Fatalf("the settled task beside no bound = (%v, %v), want the terminal it holds", status, err)
		}
	})
}

// ttlClock is the clock one registry of the bounds is read under: the check moves it instead of
// waiting for a real hour to pass.
type ttlClock struct {
	mu sync.Mutex
	at time.Time
}

// now reports the instant this clock stands at.
func (c *ttlClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

// advance moves the clock by one interval.
func (c *ttlClock) advance(by time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(by)
}

// ttlRegistry returns a registry read under its own clock and bounded by the named terminal TTL.
func ttlRegistry(t *testing.T, ttl time.Duration, start time.Time) (*MemoryRegistry, *MemoryCommit, *ttlClock) {
	t.Helper()
	clock := &ttlClock{at: start}
	commit := NewMemoryCommit(nil)
	registry := NewMemoryRegistry(commit, NewMemoryAdmission(), WithClock(clock.now), WithTerminalTTL(ttl))
	return registry, commit, clock
}

// ttlSettled admits one task and settles it, so the check starts from the terminal a sweep reads.
func ttlSettled(t *testing.T, registry *MemoryRegistry, callID, dedup string) event.TaskID {
	t.Helper()
	ctx := context.Background()
	taskID := invariantRegister(t, registry, callID, dedup)
	if result, err := registry.Advance(ctx, taskID, StatusRunning, "running", nil); err != nil || result != CommitCommitted {
		t.Fatalf("move the task of %s under way = (%v, %v), want a committed change", callID, result, err)
	}
	if result, err := registry.Advance(ctx, taskID, StatusSettled, "settled", []Envelope{invariantEnvelope(taskID, NoticeSettled, 0)}); err != nil || result != CommitCommitted {
		t.Fatalf("settle the task of %s = (%v, %v), want a committed change", callID, result, err)
	}
	return taskID
}
