package agent

import (
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/stretchr/testify/require"
)

// S3m-c.1 convergence contract. Design invariant I-3 (管线框架协调): a越窗 task
// settle for a bound invocation MUST be delivered by publishing to that
// invocation's OWN EventBus — the same transport the loop already consumes — and
// NOT via a hand-rolled side queue that re-implements EventBus's own buffering
// (the append/notify/drain/tryFinish bypass S3m-b grew). These tests pin the
// post-convergence semantics: the sink registry is a BINDING TABLE (invocation id
// → its bus) plus the delivery-accounting barrier; route's only job is to look up
// the binding and publish, so a settle becomes an ordinary pullable event on the
// owning loop's bus. Red before c.1: route appended to a private queue, so
// invBus.TryPull never saw the settle.
func TestS3mC_RoutePublishesToBoundBus(t *testing.T) {
	sinks := newSettleSinkRegistry()
	invBus := NewEventBus()
	fallback := NewEventBus()
	tk := func(id string) *task.Task {
		return &task.Task{Spec: task.TaskSpec{Origin: map[string]string{metaKeyInvocationID: id}}}
	}

	// Bind the invocation's own bus (I-1: destination confirmed at input time).
	sinks.bind("inv-1", invBus)

	evt := &AgentEvent{ID: "settle-1", Source: SourceTask}
	// The routing decision is UNCHANGED (deliverTaskSettled), only its transport is:
	// a bound id publishes to the bound bus, and NOT to the fallback persistentBus.
	deliverTaskSettled(sinks, fallback, tk("inv-1"), evt)

	var onInvBus bool
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !onInvBus {
		for _, e := range invBus.TryPull() {
			if e != nil && e.ID == "settle-1" {
				onInvBus = true
			}
		}
		if !onInvBus {
			time.Sleep(5 * time.Millisecond)
		}
	}
	require.True(t, onInvBus, "I-3: a bound invocation's settle must appear on its OWN bus (TryPull), not a bypass queue")
	for _, e := range fallback.TryPull() {
		require.False(t, e != nil && e.ID == "settle-1", "a routed settle must NOT also hit the fallback bus")
	}
}

// TestS3mC_UnboundFallsBackToBus proves behavior-neutrality is preserved by the
// convergence: an id with NO binding (entry owner, post-unbind late settle) still
// lands on the shared bus, exactly as before.
func TestS3mC_UnboundFallsBackToBus(t *testing.T) {
	sinks := newSettleSinkRegistry()
	fallback := NewEventBus()
	tk := func(id string) *task.Task {
		return &task.Task{Spec: task.TaskSpec{Origin: map[string]string{metaKeyInvocationID: id}}}
	}

	deliverTaskSettled(sinks, fallback, tk("nobody"), &AgentEvent{ID: "b1", Source: SourceTask})
	var onBus bool
	for _, e := range fallback.TryPull() {
		if e != nil && e.ID == "b1" {
			onBus = true
		}
	}
	require.True(t, onBus, "no binding → fall back to the shared bus (entry owner path unchanged)")
}

// TestS3mC_AccountingSurvivesConvergence locks that shrinking the registry to a
// binding table + barrier keeps the D-b delivery-accounting termination sound: the
// barrier only reaches quiescence once every booked spawn's settle was DELIVERED
// (published to the bound bus), and unbind stops both routing and accounting.
func TestS3mC_AccountingSurvivesConvergence(t *testing.T) {
	sinks := newSettleSinkRegistry()
	invBus := NewEventBus()
	sinks.bind("X", invBus)

	// Fresh binding, nothing booked → quiescent (containment closes immediately).
	require.True(t, sinks.quiescent("X"))

	// Book two background spawns → not quiescent.
	sinks.noteSpawn("X")
	sinks.noteSpawn("X")
	require.False(t, sinks.quiescent("X"))

	// First delivered settle (publishes to the bus) → still one outstanding.
	require.True(t, sinks.route("X", &AgentEvent{ID: "e1"}))
	require.False(t, sinks.quiescent("X"))

	// Second delivered settle → spawned == delivered → quiescent, and BOTH events are
	// retrievable from the bound bus (proving delivery went through the bus, not a
	// private queue).
	require.True(t, sinks.route("X", &AgentEvent{ID: "e2"}))
	require.True(t, sinks.quiescent("X"))
	seen := map[string]bool{}
	for _, e := range invBus.TryPull() {
		if e != nil {
			seen[e.ID] = true
		}
	}
	require.True(t, seen["e1"] && seen["e2"], "both routed settles are pullable from the bound bus")

	// Unbind → route returns false (caller falls back), accounting closed.
	sinks.unbind("X")
	require.False(t, sinks.quiescent("X"), "after unbind → no binding → not quiescent")
	require.False(t, sinks.route("X", &AgentEvent{ID: "late"}), "after unbind → route false → bus fallback")
}
