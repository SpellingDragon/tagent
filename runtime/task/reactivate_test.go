package task

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// TestTaskReactivation pins the settled-outcome re-entry of one task:
// - a settled task re-enters as the next round of the same identity, and the chain stores the reactivation fact
// - the reactivated task holds an open execution obligation again and settles its new round
// - each settlement of one task may be re-entered, and every round keeps its own fact
// - a failed task, a cancelled task and a task whose round is still in flight are each refused by name
// - two reactivations racing on one settled task commit one round: the first wins and the second is refused by name
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func TestTaskReactivation(t *testing.T) {
	ctx := context.Background()

	t.Run("a settled task re-enters as the next round", func(t *testing.T) {
		registry, carrier := invariantRegistry(t)
		taskID := reactivationSettled(t, registry)

		round, err := registry.Reactivate(ctx, taskID)
		if err != nil {
			t.Fatalf("re-enter the settled task: %v", err)
		}
		if round != 2 {
			t.Fatalf("re-entered round = %d, want the second round of the same task", round)
		}
		_, status, err := registry.Lookup(ctx, taskID)
		if err != nil || status != StatusRunning {
			t.Fatalf("status after the re-entry = (%v, %v), want the active round", status, err)
		}
		fact, found := reactivationFact(carrier, taskID, 1)
		if !found {
			t.Fatalf("chain of %s carries no reactivation fact: %+v", taskID, carrier.Facts(taskID))
		}
		if fact.Status != StatusRunning {
			t.Fatalf("reactivation fact status = %v, want the active round the chain reads back", fact.Status)
		}
	})

	t.Run("the reactivated task settles its new round", func(t *testing.T) {
		registry, carrier := invariantRegistry(t)
		taskID := reactivationSettled(t, registry)
		if _, err := registry.Reactivate(ctx, taskID); err != nil {
			t.Fatalf("re-enter the settled task: %v", err)
		}
		second := []Envelope{invariantEnvelope(taskID, NoticeSettled, 0)}
		if result, err := registry.Advance(ctx, taskID, StatusSettled, "settled-again", second); err != nil || result != CommitCommitted {
			t.Fatalf("settle the re-entered round = (%v, %v), want a committed change", result, err)
		}
		round, err := registry.Reactivate(ctx, taskID)
		if err != nil {
			t.Fatalf("re-enter the second settlement: %v", err)
		}
		if round != 3 {
			t.Fatalf("round after the second re-entry = %d, want the third round", round)
		}
		if _, found := reactivationFact(carrier, taskID, 2); !found {
			t.Fatalf("chain of %s carries no second reactivation fact: %+v", taskID, carrier.Facts(taskID))
		}
	})

	t.Run("rejects_failed_reactivation", func(t *testing.T) {
		registry, carrier := invariantRegistry(t)
		taskID := reactivationTerminal(t, registry, StatusFailed)
		stored := len(carrier.Facts(taskID))
		if _, err := registry.Reactivate(ctx, taskID); !errors.Is(err, ErrReactivationFailed) {
			t.Fatalf("re-entering a failed task = %v, want %v", err, ErrReactivationFailed)
		}
		if got := len(carrier.Facts(taskID)); got != stored {
			t.Fatalf("facts after the refused re-entry = %d, want the stored ones alone", got)
		}
	})

	t.Run("rejects_cancelled_reactivation", func(t *testing.T) {
		registry, carrier := invariantRegistry(t)
		taskID := reactivationTerminal(t, registry, StatusCancelled)
		stored := len(carrier.Facts(taskID))
		if _, err := registry.Reactivate(ctx, taskID); !errors.Is(err, ErrReactivationCancelled) {
			t.Fatalf("re-entering a cancelled task = %v, want %v", err, ErrReactivationCancelled)
		}
		if got := len(carrier.Facts(taskID)); got != stored {
			t.Fatalf("facts after the refused re-entry = %d, want the stored ones alone", got)
		}
	})

	t.Run("a round in flight is refused by name", func(t *testing.T) {
		registry, _ := invariantRegistry(t)
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		if result, err := registry.Advance(ctx, taskID, StatusRunning, "running", nil); err != nil || result != CommitCommitted {
			t.Fatalf("move the task under way = (%v, %v), want a committed change", result, err)
		}
		if _, err := registry.Reactivate(ctx, taskID); !errors.Is(err, ErrRoundInFlight) {
			t.Fatalf("re-entering a task under way = %v, want %v", err, ErrRoundInFlight)
		}
	})

	t.Run("rejects_concurrent_reactivation", func(t *testing.T) {
		registry, carrier := invariantRegistry(t)
		taskID := reactivationSettled(t, registry)
		const racers = 8
		rounds := make([]int64, racers)
		errs := make([]error, racers)
		var start sync.WaitGroup
		var done sync.WaitGroup
		start.Add(1)
		done.Add(racers)
		for at := 0; at < racers; at++ {
			go func(at int) {
				defer done.Done()
				start.Wait()
				rounds[at], errs[at] = registry.Reactivate(ctx, taskID)
			}(at)
		}
		start.Done()
		done.Wait()
		won := 0
		for at := 0; at < racers; at++ {
			switch {
			case errs[at] == nil && rounds[at] == 2:
				won++
			case !errors.Is(errs[at], ErrRoundInFlight):
				t.Fatalf("racer %d = (%d, %v), want the next round or %v", at, rounds[at], errs[at], ErrRoundInFlight)
			}
		}
		if won != 1 {
			t.Fatalf("reactivations that committed a round = %d, want exactly one", won)
		}
		if _, found := reactivationFact(carrier, taskID, 1); !found {
			t.Fatalf("chain of %s carries no single reactivation fact: %+v", taskID, carrier.Facts(taskID))
		}
		if _, found := reactivationFact(carrier, taskID, 2); found {
			t.Fatalf("chain of %s carries a second reactivation fact, want one", taskID)
		}
	})
}

// reactivationSettled admits one task and settles it, so the re-entry starts from the
// settled outcome it is defined on.
func reactivationSettled(t *testing.T, registry *MemoryRegistry) event.TaskID {
	t.Helper()
	ctx := context.Background()
	taskID := invariantRegister(t, registry, "call-1", "dedup-1")
	if result, err := registry.Advance(ctx, taskID, StatusRunning, "running", nil); err != nil || result != CommitCommitted {
		t.Fatalf("move the task under way = (%v, %v), want a committed change", result, err)
	}
	if result, err := registry.Advance(ctx, taskID, StatusSettled, "settled", []Envelope{invariantEnvelope(taskID, NoticeSettled, 0)}); err != nil || result != CommitCommitted {
		t.Fatalf("settle the task = (%v, %v), want a committed change", result, err)
	}
	return taskID
}

// reactivationTerminal admits one task and lands the named terminal fact on it, so the
// refusal of a re-entry is read off a task that really holds that outcome.
func reactivationTerminal(t *testing.T, registry *MemoryRegistry, terminal Status) event.TaskID {
	t.Helper()
	ctx := context.Background()
	taskID := invariantRegister(t, registry, "call-1", "dedup-1")
	if result, err := registry.Advance(ctx, taskID, StatusRunning, "running", nil); err != nil || result != CommitCommitted {
		t.Fatalf("move the task under way = (%v, %v), want a committed change", result, err)
	}
	if result, err := registry.Advance(ctx, taskID, terminal, "terminal", nil); err != nil || result != CommitCommitted {
		t.Fatalf("land the %v fact = (%v, %v), want a committed change", terminal, result, err)
	}
	if _, status, err := registry.Lookup(ctx, taskID); err != nil || status != terminal {
		t.Fatalf("status before the re-entry = (%v, %v), want the %v fact", status, err, terminal)
	}
	return taskID
}

// reactivationFact reports the fact of the nth re-entry of one task.
func reactivationFact(carrier *MemoryCommit, taskID event.TaskID, seq int64) (CommitRecord, bool) {
	wanted := MutationID(reactivatePrefix + string(taskID) + ":" + strconv.FormatInt(seq, 10))
	for _, fact := range carrier.Facts(taskID) {
		if fact.MutationID == wanted {
			return fact, true
		}
	}
	return CommitRecord{}, false
}

// reentryClock is the movable time source the re-entry checks read the stamps of
// one task from: a task admitted hours before it is re-entered is written by
// moving the clock, never by waiting for the hours to pass.
type reentryClock struct {
	mu  sync.Mutex
	now time.Time
}

// Now reports the instant the clock stands at.
func (c *reentryClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward by the given span.
func (c *reentryClock) Advance(span time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(span)
}

// reentryRegistry returns one registry whose stamps are read from the given
// clock under the given threshold set.
func reentryRegistry(clock *reentryClock, policy Policy) (*MemoryRegistry, *MemoryCommit) {
	carrier := NewMemoryCommit(nil)
	return NewMemoryRegistry(carrier, NewMemoryAdmission(), WithPolicy(policy), WithClock(clock.Now)), carrier
}

// reentryLifetime is the threshold set the re-entry checks read a task under: the
// only bound it places is the time to live, so an expiry the checks read is the
// time to live of the task and never a silence threshold beside it.
var reentryLifetime = Policy{TTL: 2 * time.Hour}

// reentryStart is the instant the re-entry checks admit their tasks at.
var reentryStart = time.Date(2026, time.March, 1, 9, 0, 0, 0, time.UTC)

// TestInteractiveReentryTTL pins the time to live of a task across a write-type re-entry of it.
// - a write-type re-entry of a settled task restarts its time to live, so the round it opens is not expired under a deadline counted before it
// - the deadline moves with the round rather than being dropped: the re-entered task expires again a time to live after the re-entry
// - a read-only peek of the task refreshes nothing: peeks past the deadline read the expiry and extend no deadline
// - a peek writes no anchor, so the instant just inside the deadline still reads the round as live
// 契约: docs/wiki/agent/task-lifecycle.md#ttl-reclaim
func TestInteractiveReentryTTL(t *testing.T) {
	ctx := context.Background()

	t.Run("a write re-entry restarts the time to live", func(t *testing.T) {
		clock := &reentryClock{now: reentryStart}
		registry, _ := reentryRegistry(clock, reentryLifetime)
		taskID := reactivationSettled(t, registry)

		clock.Advance(3 * time.Hour)

		if _, err := registry.Reactivate(ctx, taskID); err != nil {
			t.Fatalf("re-enter the settled task: %v", err)
		}
		if got, err := registry.Logical(ctx, taskID, clock.Now().Add(time.Minute)); err != nil || got == LogicalExpired {
			t.Fatalf("logical status a minute after the re-entry = (%q, %v), want a round within its own time to live", got, err)
		}

		clock.Advance(2*time.Hour + time.Minute)
		if got, err := registry.Logical(ctx, taskID, clock.Now()); err != nil || got != LogicalExpired {
			t.Fatalf("logical status a time to live after the re-entry = (%q, %v), want %q", got, err, LogicalExpired)
		}
	})

	t.Run("peek_does_not_refresh", func(t *testing.T) {
		clock := &reentryClock{now: reentryStart}
		registry, _ := reentryRegistry(clock, reentryLifetime)
		taskID := reactivationSettled(t, registry)
		clock.Advance(3 * time.Hour)
		if _, err := registry.Reactivate(ctx, taskID); err != nil {
			t.Fatalf("re-enter the settled task: %v", err)
		}

		deadline := clock.Now().Add(reentryLifetime.TTL)
		probe := deadline.Add(time.Minute)
		for at := 1; at <= 3; at++ {
			if got, err := registry.Logical(ctx, taskID, probe); err != nil || got != LogicalExpired {
				t.Fatalf("peek %d past the deadline = (%q, %v), want %q: a read must not extend the deadline", at, got, err, LogicalExpired)
			}
			if changes := registry.Watch(ctx, probe); len(changes) != 0 && changes[0].To != LogicalExpired {
				t.Fatalf("changes peek %d reported = %+v, want the expiry the deadline names", at, changes)
			}
		}

		if got, err := registry.Logical(ctx, taskID, deadline.Add(-time.Minute)); err != nil || got == LogicalExpired {
			t.Fatalf("logical status just inside the deadline after the peeks = (%q, %v), want a live round", got, err)
		}
	})
}

// TestTaskReactivationRollback pins the compensation of a re-entry whose round no turn served.
// - the rollback puts the task back to the settled outcome it was re-entered from, and the chain stores the compensating fact
// - the obligation the reactivation opened is withdrawn with it, so a task put back owes nothing of the round it never ran
// - the task is re-entrant afterwards: the next reactivation opens a round of its own, so no round is left hanging
// - a rollback naming another round, and one naming a task that holds no round at all, are each refused by name and move nothing
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func TestTaskReactivationRollback(t *testing.T) {
	ctx := context.Background()

	t.Run("a round nothing served is put back", func(t *testing.T) {
		registry, carrier := invariantRegistry(t)
		taskID := reactivationSettled(t, registry)
		round, err := registry.Reactivate(ctx, taskID)
		if err != nil {
			t.Fatalf("re-enter the settled task: %v", err)
		}
		if got := reactivationAwaitingTool(registry, taskID); got != 1 {
			t.Fatalf("open execution obligations of the re-entered task = %d, want the one round it opened", got)
		}

		if err := registry.RollbackReactivation(ctx, taskID, round); err != nil {
			t.Fatalf("put the round back = %v", err)
		}
		if _, status, err := registry.Lookup(ctx, taskID); err != nil || status != StatusSettled {
			t.Fatalf("status after the rollback = (%v, %v), want the settled outcome the round was re-entered from", status, err)
		}
		if got := reactivationAwaitingTool(registry, taskID); got != 0 {
			t.Fatalf("open execution obligations after the rollback = %d, want none: the round that opened them was withdrawn", got)
		}
		if _, found := reactivationRollbackFact(carrier, taskID, round); !found {
			t.Fatalf("chain of %s carries no compensating fact for round %d: %+v", taskID, round, carrier.Facts(taskID))
		}
	})

	t.Run("the task is re-entrant after the rollback", func(t *testing.T) {
		registry, _ := invariantRegistry(t)
		taskID := reactivationSettled(t, registry)
		round, err := registry.Reactivate(ctx, taskID)
		if err != nil {
			t.Fatalf("re-enter the settled task: %v", err)
		}
		if err := registry.RollbackReactivation(ctx, taskID, round); err != nil {
			t.Fatalf("put the round back = %v", err)
		}
		again, err := registry.Reactivate(ctx, taskID)
		if err != nil {
			t.Fatalf("re-enter the task the rollback put back = %v, want the round it opens instead of a round left hanging", err)
		}
		if again <= round {
			t.Fatalf("round after the second re-entry = %d, want one past the rolled-back round %d: a round identity is never reused", again, round)
		}
	})

	t.Run("rejects_rollback_of_another_round", func(t *testing.T) {
		registry, carrier := invariantRegistry(t)
		taskID := reactivationSettled(t, registry)
		round, err := registry.Reactivate(ctx, taskID)
		if err != nil {
			t.Fatalf("re-enter the settled task: %v", err)
		}
		stored := len(carrier.Facts(taskID))
		if err := registry.RollbackReactivation(ctx, taskID, round+1); !errors.Is(err, ErrReactivationRollback) {
			t.Fatalf("rollback of round %d = %v, want %v", round+1, err, ErrReactivationRollback)
		}
		if got := len(carrier.Facts(taskID)); got != stored {
			t.Fatalf("facts after the refused rollback = %d, want the stored ones alone", got)
		}
		if _, status, _ := registry.Lookup(ctx, taskID); status != StatusRunning {
			t.Fatalf("status after the refused rollback = %v, want the round it still holds", status)
		}
	})

	t.Run("rejects_rollback_beside_no_round", func(t *testing.T) {
		registry, _ := invariantRegistry(t)
		taskID := reactivationSettled(t, registry)
		if err := registry.RollbackReactivation(ctx, taskID, 2); !errors.Is(err, ErrReactivationRollback) {
			t.Fatalf("rollback of a task that holds no round in flight = %v, want %v", err, ErrReactivationRollback)
		}
	})
}

// reactivationAwaitingTool counts the execution obligations one task still holds open,
// so a rollback is read by the obligation it withdrew rather than by a status alone.
func reactivationAwaitingTool(registry *MemoryRegistry, taskID event.TaskID) int {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	entry, ok := registry.tasks[taskID]
	if !ok {
		return 0
	}
	count := 0
	for _, held := range entry.obligations {
		if held.obligation.Kind == ObligationAwaitingTool {
			count++
		}
	}
	return count
}

// reactivationRollbackFact reports the compensating fact of the nth re-entry of one task.
func reactivationRollbackFact(carrier *MemoryCommit, taskID event.TaskID, round int64) (CommitRecord, bool) {
	wanted := MutationID(rollbackPrefix + string(taskID) + ":" + strconv.FormatInt(round, 10))
	for _, fact := range carrier.Facts(taskID) {
		if fact.MutationID == wanted {
			return fact, true
		}
	}
	return CommitRecord{}, false
}
