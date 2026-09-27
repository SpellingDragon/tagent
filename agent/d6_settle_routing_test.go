package agent

import (
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/stretchr/testify/require"
)

// drainIDs pulls everything currently on the bus and returns the event ids seen.
func drainIDs(bus *EventBus) []string {
	var out []string
	for _, e := range bus.TryPull() {
		if e != nil {
			out = append(out, e.ID)
		}
	}
	return out
}

// TestSettleSinkRegistry_Routing locks the S3m-c routing decision (the branch the
// real OnSettle uses): a bound invocation takes the settle by PUBLISHING it to its
// own bus (I-3 — the settle becomes an ordinary pullable event, no side queue), an
// unknown/empty id returns false so the caller falls back to the shared bus, and
// unbind stops routing.
func TestSettleSinkRegistry_Routing(t *testing.T) {
	r := newSettleSinkRegistry()
	invBus := NewEventBus()

	evt := &AgentEvent{ID: "e1"}
	require.False(t, r.route("X", evt), "no binding → fall back to bus")
	require.False(t, r.route("", evt), "empty invocation id → fall back (non-delegation)")

	r.bind("X", invBus)
	// route publishes to the bound bus; the event is pullable there.
	require.True(t, r.route("X", evt), "bound bus takes the event via Publish")
	require.Contains(t, drainIDs(invBus), "e1", "the settle is on the bound bus, not a bypass queue")

	r.unbind("X")
	require.False(t, r.route("X", evt), "after unbind → fall back (loop exited)")
}

// TestTaskInvocationID locks reading the S2m provenance handle off a settled task.
func TestTaskInvocationID(t *testing.T) {
	require.Equal(t, "", taskInvocationID(nil), "nil task → empty (non-delegation)")
	require.Equal(t, "", taskInvocationID(&task.Task{}), "no Origin → empty")
	tk := &task.Task{Spec: task.TaskSpec{Origin: map[string]string{metaKeyInvocationID: "deleg-x"}}}
	require.Equal(t, "deleg-x", taskInvocationID(tk))
}

// TestS3mA_DeliverTaskSettled_Decision locks the routing decision the agent's
// OnSettle hook calls: a bound invocation's bus takes it; otherwise the shared bus does.
func TestS3mA_DeliverTaskSettled_Decision(t *testing.T) {
	sinks := newSettleSinkRegistry()
	bus := NewEventBus()
	tk := func(id string) *task.Task {
		return &task.Task{Spec: task.TaskSpec{Origin: map[string]string{metaKeyInvocationID: id}}}
	}

	// No binding → fall back to the bus (behavior-neutral entry/unbound path).
	deliverTaskSettled(sinks, bus, tk("no-sink"), &AgentEvent{ID: "b1", Source: SourceTask})
	require.Contains(t, drainIDs(bus), "b1", "unsettled delegation id → routed to persistentBus (unchanged behavior)")

	// Bound bus → delivered there, NOT the shared fallback bus.
	invBus := NewEventBus()
	sinks.bind("X", invBus)
	deliverTaskSettled(sinks, bus, tk("X"), &AgentEvent{ID: "s1"})
	require.Contains(t, drainIDs(invBus), "s1", "bound bus owns the settle")
	require.NotContains(t, drainIDs(bus), "s1", "routed settle must NOT also hit the shared bus")
}

// TestS3mA_BackgroundSettle_RoutesToSink drives a REAL background settle through a
// taskManager whose OnSettle calls deliverTaskSettled (the same body agent.go wires),
// proving the settle reaches the bound invocation bus. Red before S3m-a: the event
// would only ever reach the shared bus.
func TestS3mA_BackgroundSettle_RoutesToSink(t *testing.T) {
	sinks := newSettleSinkRegistry()
	fallback := NewEventBus()
	invBus := NewEventBus()
	sinks.bind("deleg-x", invBus)

	tm := task.NewTaskManager(task.TaskManagerConfig{
		OnSettle: func(tk *task.Task, sig task.SettleSignal) {
			deliverTaskSettled(sinks, fallback, tk, newTaskSettledEvent(tk, sig, 0, ""))
		},
	})
	d := task.NewManualDetectorDetach(40 * time.Millisecond) // closes the sync-wait window → ack
	res := tm.Spawn(
		task.TaskSpec{Kind: "command", Desc: "late job", Origin: map[string]string{metaKeyInvocationID: "deleg-x"}},
		d,
	)
	require.False(t, res.Settled, "expected background (ack), not inline settle")
	d.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "done later"}) // the settle that fires OnSettle
	d.Done()

	deadline := time.Now().Add(3 * time.Second)
	var settled []*AgentEvent
	for time.Now().Before(deadline) {
		if got := invBus.TryPull(); len(got) > 0 {
			settled = got
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.NotEmpty(t, settled, "background settle not published to the bound invocation bus")
	require.Contains(t, settled[0].Message.Content, "done later", "the routed event is the task_settled result")
}

// TestS3mA_BackgroundSettle_FallsBackToBus proves behavior-neutrality: with no bus
// bound (entry owner, unbound invocation), a real background settle still publishes
// to the shared bus (the current path).
func TestS3mA_BackgroundSettle_FallsBackToBus(t *testing.T) {
	sinks := newSettleSinkRegistry() // nothing bound
	bus := NewEventBus()
	tm := task.NewTaskManager(task.TaskManagerConfig{
		OnSettle: func(tk *task.Task, sig task.SettleSignal) {
			deliverTaskSettled(sinks, bus, tk, newTaskSettledEvent(tk, sig, 0, ""))
		},
	})
	d := task.NewManualDetectorDetach(40 * time.Millisecond)
	res := tm.Spawn(
		task.TaskSpec{Kind: "command", Desc: "late job", Origin: map[string]string{metaKeyInvocationID: "nobody-listening"}},
		d,
	)
	require.False(t, res.Settled)
	d.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "later"})
	d.Done()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, e := range bus.TryPull() {
			if e != nil && e.Source == SourceTask {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("unbound background settle did not fall back to the bus")
}
