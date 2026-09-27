package agent

import (
	"testing"

	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/stretchr/testify/require"
)

// TestSettleAccounting_DeliveryBarrier locks the D-b termination predicate: a
// delegation quiesces only when every spawned task's settle has been DELIVERED
// (published to the bound bus) — not when tasks merely reach terminal status.
// noteSpawn increments at spawn; route decrements after it publishes. So the
// "terminal-before-delivery" window (which broke the inflight==0 check, 轮五十二)
// cannot cause a premature exit, and delivery-accounting reaches quiescence exactly
// when the last settle is published to the bus.
func TestSettleAccounting_DeliveryBarrier(t *testing.T) {
	r := newSettleSinkRegistry()
	invBus := NewEventBus()
	r.bind("X", invBus)

	// No task spawned yet → quiescent (containment: a plain request/response sub-call
	// exits immediately).
	require.True(t, r.quiescent("X"), "fresh binding with nothing spawned is quiescent")

	// Spawn one task → an expected settle exists → not quiescent even before it settles.
	r.noteSpawn("X")
	require.False(t, r.quiescent("X"), "spawned task with settle not yet delivered must keep the tail alive")

	// A second spawn.
	r.noteSpawn("X")
	require.False(t, r.quiescent("X"))

	// Deliver the first settle (publishes to the bus) → still one outstanding.
	require.True(t, r.route("X", &AgentEvent{ID: "e1"}))
	require.False(t, r.quiescent("X"), "one delivered, one still expected")

	// Deliver the second → all delivered → quiescent.
	require.True(t, r.route("X", &AgentEvent{ID: "e2"}))
	require.True(t, r.quiescent("X"), "spawned == delivered → quiescent")

	// Both events are actually pullable from the bound bus (route published them).
	require.ElementsMatch(t, []string{"e1", "e2"}, drainIDs(invBus))
}

// TestSettleAccounting_NoBindingIsUnaccounted proves behavior-neutrality: with no
// bus bound (entry owner, unbound Run), noteSpawn is a no-op, quiescent is false,
// and route falls back (returns false) without touching any counter.
func TestSettleAccounting_NoBindingIsUnaccounted(t *testing.T) {
	r := newSettleSinkRegistry() // nothing bound

	r.noteSpawn("ghost") // must not create accounting for an unbound id
	require.False(t, r.quiescent("ghost"), "no binding → never quiescent (caller falls back to bus)")
	require.False(t, r.route("ghost", &AgentEvent{ID: "e"}), "no binding → route false → bus fallback")
}

// TestSettleAccounting_UnaccountedDeliveryDoesNotGoNegative: an id whose bus is
// bound but for which noteSpawn was never called (e.g. a settle arriving before
// accounting) still delivers (route true) and the counter never dips below zero.
func TestSettleAccounting_UnaccountedDeliveryDoesNotGoNegative(t *testing.T) {
	r := newSettleSinkRegistry()
	invBus := NewEventBus()
	r.bind("Y", invBus)

	require.True(t, r.route("Y", &AgentEvent{ID: "s1"}), "delivery to a bound bus succeeds regardless of accounting")
	require.True(t, r.quiescent("Y"), "pending stayed 0 (guarded decrement never negative)")
	require.Contains(t, drainIDs(invBus), "s1")
}

// TestSettleAccounting_UnbindClosesAccounting: unbinding a delegation's bus clears
// both the route target and the accounting so a late settle falls back to the bus
// (safe drop, per the deferred unbind ordering).
func TestSettleAccounting_UnbindClosesAccounting(t *testing.T) {
	r := newSettleSinkRegistry()
	invBus := NewEventBus()
	r.bind("Z", invBus)
	r.noteSpawn("Z")
	require.False(t, r.quiescent("Z"))

	r.unbind("Z")
	require.False(t, r.quiescent("Z"), "after unbind → no binding → not quiescent")
	require.False(t, r.route("Z", &AgentEvent{ID: "late"}), "after unbind → route false → bus fallback")
}

// stubSpawner returns a fixed SpawnResult so the accounting decorator can be tested
// without a real task manager or timing.
type stubSpawner struct{ settled bool }

func (s *stubSpawner) Spawn(task.TaskSpec, task.SettleDetector) task.SpawnResult {
	return task.SpawnResult{Settled: s.settled}
}

// TestCountingSpawner_BooksOnlyBackground locks the accounting decorator's rule: a
// BACKGROUND spawn (res.Settled false) books an expected settle that its route later
// clears; an INLINE spawn (res.Settled true) is booked-then-voided so it never leaves
// a phantom pending that would strand the barrier; and with no bus bound the whole
// decorator is a no-op (behavior-neutral for non-delegation turns).
func TestCountingSpawner_BooksOnlyBackground(t *testing.T) {
	r := newSettleSinkRegistry()
	bx := NewEventBus()
	r.bind("X", bx)

	// Background spawn → booked → not quiescent until its settle is delivered.
	(&countingSpawner{inner: &stubSpawner{settled: false}, sinks: r, id: "X"}).Spawn(task.TaskSpec{}, nil)
	require.False(t, r.quiescent("X"), "a background spawn books an expected settle")
	require.True(t, r.route("X", &AgentEvent{ID: "s"}), "its settle delivers")
	require.True(t, r.quiescent("X"), "spawned == delivered → quiescent")

	// Inline spawn → booked-then-voided → never strands.
	by := NewEventBus()
	r.bind("Y", by)
	(&countingSpawner{inner: &stubSpawner{settled: true}, sinks: r, id: "Y"}).Spawn(task.TaskSpec{}, nil)
	require.True(t, r.quiescent("Y"), "an inline settle is voided immediately — no phantom pending")

	// No bus for the id → decorator is a pure no-op (delegates, never books).
	res := (&countingSpawner{inner: &stubSpawner{settled: false}, sinks: r, id: "ghost"}).Spawn(task.TaskSpec{}, nil)
	require.False(t, res.Settled, "the inner spawn result is passed through unchanged")
	require.False(t, r.quiescent("ghost"), "unbound id is never accounted")
}

// quiescent lives in a _test.go file since the 2026-09-27 dead-code audit: it is
// the tests' termination oracle only (awaiting, in settle_routing.go, is what
// production consults); kept as a method so the oracle reads exactly the state
// the production predicate sees.
// quiescent reports whether the invocation's bus is bound AND every task spawned
// under it has had its settle delivered (spawned == delivered). This is the D-b
// termination predicate, immune to the terminal-before-delivery race. It is the
// shared shell's越窗 exit condition: bound ∧ pending==0 ∧ the bus drains empty.
func (r *settleSinkRegistry) quiescent(id string) bool {
	if r == nil || id == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byInv[id]; !ok {
		return false
	}
	return r.pending[id] == 0
}
