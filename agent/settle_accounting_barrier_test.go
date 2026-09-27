package agent

// Review-evidence reproduction (2026-09-27 deep review), for the finding:
// countingSpawner leaks a delivery-accounting booking when the inner spawn is
// BLOCKED (disk-degraded gate) or DEDUPED (same-key single-flight). In both
// shapes no OnSettle/route will ever fire for THIS call's booking, so
// pending[id] stays +1 forever, awaiting(id) is stuck true, and the
// per-invocation consume loop (runAgentLoop's quiescence check) can never
// exit on its own — the sub-agent call is dragged to the 600s
// defaultSubAgentTimeout instead of quiescing after its last settle.
//
// This test FAILS while the defect is unfixed and becomes the regression test
// for the fix (void the booking on Blocked/Deduped as well).

import (
	"testing"

	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/stretchr/testify/require"
)

type reviewStubSpawner struct {
	res task.SpawnResult
}

func (s *reviewStubSpawner) Spawn(spec task.TaskSpec, detector task.SettleDetector) task.SpawnResult {
	if detector != nil {
		detector.Cancel()
	}
	return s.res
}

func TestCountingSpawnerVoidsBookingOnDedup(t *testing.T) {
	sinks := newSettleSinkRegistry()
	bus := NewEventBus()
	id := "review-inv-1"
	sinks.bind(id, bus)

	// The dedup shape: the task layer matched an ACTIVE same-key task. The
	// matched task settles under ITS ORIGINAL invocation's booking — no
	// route will decrement anything for THIS call.
	cs := &countingSpawner{
		inner: &reviewStubSpawner{res: task.SpawnResult{Task: &task.Task{}, Deduped: true}},
		sinks: sinks,
		id:    id,
	}
	cs.Spawn(task.TaskSpec{}, nil)

	// Correct behavior: a deduped call booked nothing it owns, so the barrier
	// must be quiescent. UNFIXED the leaked booking pins awaiting() true and
	// the invocation loop never quiesces on its own.
	require.False(t, sinks.awaiting(id),
		"review P2 evidence: deduped spawn leaked a delivery booking — pending stuck >0, loop cannot quiesce")
}

func TestCountingSpawnerVoidsBookingOnBlock(t *testing.T) {
	sinks := newSettleSinkRegistry()
	bus := NewEventBus()
	id := "review-inv-2"
	sinks.bind(id, bus)

	// The blocked shape: the disk-degraded spawn gate refused adoption. No task
	// was registered, so no settle will ever route for this booking.
	cs := &countingSpawner{
		inner: &reviewStubSpawner{res: task.SpawnResult{Blocked: "disk degraded"}},
		sinks: sinks,
		id:    id,
	}
	cs.Spawn(task.TaskSpec{}, nil)

	require.False(t, sinks.awaiting(id),
		"review P2 evidence: blocked spawn leaked a delivery booking — pending stuck >0, loop cannot quiesce")
}

// TestCountingSpawnerVoidsBookingOnInlineSettle is the CONTROL: the existing
// voidSpawn-on-Settled behavior is correct and must keep holding after any fix.
func TestCountingSpawnerVoidsBookingOnInlineSettle(t *testing.T) {
	sinks := newSettleSinkRegistry()
	bus := NewEventBus()
	id := "review-inv-3"
	sinks.bind(id, bus)

	cs := &countingSpawner{
		inner: &reviewStubSpawner{res: task.SpawnResult{Task: &task.Task{}, Settled: true}},
		sinks: sinks,
		id:    id,
	}
	cs.Spawn(task.TaskSpec{}, nil)

	require.False(t, sinks.awaiting(id), "inline settle must void its booking (existing behavior)")
}

// TestDedupKeepsInvocationLoopQuiescent walks the FULL barrier chain of the
// dedup shape (spawn background → same-key second call dedups → first task's
// settle delivers): after the last real settle is published to the bound bus,
// the shared shell's exit predicate (awaiting==false + drained bus) must hold.
// UNFIXED the dedup's leaked booking kept awaiting stuck true even after every
// real settle had been delivered, pinning the invocation loop open until the
// caller's hard timeout.
func TestDedupKeepsInvocationLoopQuiescent(t *testing.T) {
	sinks := newSettleSinkRegistry()
	invBus := NewEventBus()
	id := "review-inv-4"
	sinks.bind(id, invBus)

	// Call 1: a real background spawn (settles later, out of the sync window).
	bg := &countingSpawner{
		inner: &reviewStubSpawner{res: task.SpawnResult{Task: &task.Task{}}},
		sinks: sinks, id: id,
	}
	bg.Spawn(task.TaskSpec{}, nil)
	require.True(t, sinks.awaiting(id), "background spawn must keep the tail alive")

	// Call 2: same-key retry hits single-flight dedup → books nothing it owns.
	dup := &countingSpawner{
		inner: &reviewStubSpawner{res: task.SpawnResult{Task: &task.Task{}, Deduped: true}},
		sinks: sinks, id: id,
	}
	dup.Spawn(task.TaskSpec{}, nil)

	// The real task settles: deliver via the routing decision (publishes to the
	// bound bus, decrements ITS booking).
	settled := &task.Task{Spec: task.TaskSpec{Origin: map[string]string{metaKeyInvocationID: id}}}
	deliverTaskSettled(sinks, nil, settled, &AgentEvent{ID: "settle-real", Source: SourceTask})

	// Barrier quiesces: the shared shell's keep-alive predicate is false and
	// the delivered settle is pullable — exactly the loop's exit condition.
	require.False(t, sinks.awaiting(id), "after the last real settle is delivered the loop must be able to quiesce (P2-3 gate)")
	var sawSettle bool
	for _, e := range invBus.TryPull() {
		if e != nil && e.ID == "settle-real" {
			sawSettle = true
		}
	}
	require.True(t, sawSettle, "the settle must be on the bound bus (drained by the shell before exit)")
}
