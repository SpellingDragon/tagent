package agent

import (
	"sync"

	"github.com/SpellingDragon/tagent/agent/task"
)

// settleSinkRegistry is the S3m-c routing table for the M2 per-invocation loop.
//
// It is deliberately ONLY two things after the pipeline convergence:
//
// - a BINDING TABLE (invocation id → that invocation's own EventBus) — the I-1
// carrier: once an input event enters a loop, its derived task settles have a
// confirmed destination (the loop's bus) that routing simply looks up, never
// infers from the event; and
// - the D-b DELIVERY-ACCOUNTING BARRIER (pending = spawned − delivered per id) —
// the termination predicate that is immune to the terminal-before-delivery race.
//
// Before S3m-c this registry also owned a hand-rolled per-invocation queue
// (append / notify / wait / drain / tryFinish) that re-implemented EventBus's own
// buffering. That bypass is gone: a越窗 settle is now delivered by PUBLISHING to
// the bound bus, so the settle becomes an ordinary pullable event consumed by the
// SAME shared shell (runAgentLoop) that consumes the entry owner's bus. "作为被调
// 方" and "直连宿主" thus share one transport and one consume loop; the only
// remaining difference is the output receiver.
//
// With no binding registered (the entry owner, and any late settle after a loop's
// unbind) delivery falls back to the shared persistentBus, so the table is
// behavior-neutral for every path that never binds a bus.
type settleSinkRegistry struct {
	mu      sync.Mutex
	byInv   map[string]*EventBus
	pending map[string]int
}

func newSettleSinkRegistry() *settleSinkRegistry {
	return &settleSinkRegistry{
		byInv:   make(map[string]*EventBus),
		pending: make(map[string]int),
	}
}

// bind opens the delivery window (pending=0) for invocation id and records the
// bus its loop consumes. The shared shell (runAgentLoop) is that bus's sole
// consumer, so a settle published by route is pulled by the very loop that owns
// the id. Empty id or nil bus is a no-op (non-delegation turn, or a loop that
// binds nothing → keeps the persistentBus fallback).
func (r *settleSinkRegistry) bind(id string, bus *EventBus) {
	if r == nil || id == "" || bus == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byInv == nil {
		r.byInv = make(map[string]*EventBus)
	}
	if r.pending == nil {
		r.pending = make(map[string]int)
	}
	r.byInv[id] = bus
	r.pending[id] = 0
}

// unbind drops the binding + accounting for id. The owning loop calls it on exit
// (before its channel closes), so a settle that arrives afterward finds no
// binding and falls back to the bus — a safe drop, never a send on a dead sink.
func (r *settleSinkRegistry) unbind(id string) {
	if r == nil || id == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byInv, id)
	delete(r.pending, id)
}

// noteSpawn records that a task attributed to invocation id was spawned, so its
// future settle is expected. It only counts while a bus is bound for id — a
// delegation with no live loop (entry owner) is not accounted and keeps the bus
// behavior with no bookkeeping.
func (r *settleSinkRegistry) noteSpawn(id string) {
	if r == nil || id == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byInv[id]; ok {
		r.pending[id]++
	}
}

// voidSpawn undoes one noteSpawn booking for a task that settled INLINE (within
// the sync-wait window): OnSettle — and therefore route — never fires for an
// inline settle, so its expectation must be removed here or the barrier would
// never reach quiescence. Guarded at 0 so it can only cancel a booking this id
// actually made.
func (r *settleSinkRegistry) voidSpawn(id string) {
	if r == nil || id == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byInv[id]; ok && r.pending[id] > 0 {
		r.pending[id]--
	}
}

// awaiting reports whether a bound bus for id still has booked-but-undelivered
// spawns (bound ∧ pending > 0) — i.e. whether a越窗 settle can still arrive on it.
// This is the shared shell's keep-alive predicate: an unbound id (or nil registry)
// can receive no routed settle, so the shell drains it and exits rather than
// blocking forever on Pull. (Its bound-and-drained complement exists as the
// tests' oracle quiescent, in d7_settle_accounting_test.go; production consults
// awaiting only.)
func (r *settleSinkRegistry) awaiting(id string) bool {
	if r == nil || id == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byInv[id]; !ok {
		return false
	}
	return r.pending[id] > 0
}

// route delivers evt to the bus bound for invocation id by PUBLISHING it there,
// then decrementing the accounting barrier. It returns true when a binding took
// the event; false only when NO bus is bound (entry owner / post-unbind late
// settle), in which case the caller falls back to the shared bus.
//
// Ordering matters for termination: the publish happens BEFORE the decrement, so
// by the time pending reaches 0 (quiescent true) every delivered settle is
// already pullable on the bus. The shared shell relies on this when it checks
// quiescent-then-TryPull to decide the越窗 tail has truly drained. The lock is
// released before Publish so a momentarily-full bus never serializes against a
// concurrent noteSpawn/quiescent from the consuming loop.
func (r *settleSinkRegistry) route(id string, evt *AgentEvent) bool {
	if r == nil || id == "" {
		return false
	}
	r.mu.Lock()
	bus := r.byInv[id]
	r.mu.Unlock()
	if bus == nil {
		return false
	}
	bus.Publish(evt)
	r.mu.Lock()
	if r.pending[id] > 0 {
		r.pending[id]--
	}
	r.mu.Unlock()
	return true
}

// taskInvocationID reads the S2m provenance handle off a settled task, or ""
// when the task carries no delegation attribution (non-delegation turn).
func taskInvocationID(tk *task.Task) string {
	if tk == nil {
		return ""
	}
	return tk.Spec.Origin[metaKeyInvocationID]
}

// deliverTaskSettled is the routing decision the agent's OnSettle hook calls:
// hand a background task_settled to the owning sub-invocation loop's bus (keyed
// by the task's S2m invocation_id), else fall back to the shared bus. The bus
// fallback is the current single-consumer path (entry owner + every unbound
// invocation), so with no binding behavior is unchanged. route's Publish never
// blocks the emitting goroutine under normal drain, so a settle is never dropped
// or deadlocked.
func deliverTaskSettled(sinks *settleSinkRegistry, bus *EventBus, tk *task.Task, evt *AgentEvent) {
	if sinks.route(taskInvocationID(tk), evt) {
		return
	}
	if bus != nil && evt != nil {
		bus.Publish(evt)
	}
}

// bindSettleBus records the bus a sub-invocation loop consumes, keyed by its
// delegation invocation_id, so its越窗 settles route back to that bus. Used by
// S3m-c; nil-safe and empty-id-safe for callers outside a delegation.
func (ta *TagentAgent) bindSettleBus(id string, bus *EventBus) {
	if ta != nil {
		ta.settleSinks.bind(id, bus)
	}
}

// unbindSettleBus drops the binding registered for id (the loop's exit; a later
// settle then falls back to the bus).
func (ta *TagentAgent) unbindSettleBus(id string) {
	if ta != nil {
		ta.settleSinks.unbind(id)
	}
}

// countingSpawner wraps a delegation turn's task spawner so tasks that will settle
// in the BACKGROUND are booked into the delivery-accounting barrier. The booking
// runs BEFORE the inner spawn, so the expectation is recorded before the task can
// possibly settle — an early route can never decrement a counter that has not been
// incremented yet. Three return shapes mean THIS call owns no future settle and
// the booking is voided immediately: an INLINE settle
// (OnSettle/route never fires), a DEDUP hit (the matched task settles under its
// ORIGINAL invocation's booking), and a gate BLOCK (no task was adopted). Without
// the dedup/block void the barrier leaks one pending unit per refused call —
// awaiting() stays true forever and the invocation loop can only exit via the
// caller's hard timeout (plan single-flight makes dedup a hot path, not an edge).
// A background spawn leaves exactly one pending unit that its eventual settle
// route decrements: the invariant quiescent depends on.
//
// It is installed only when the turn carries an invocation_id AND its ContextManager
// has the owner's settleSinks wired (a sub-call Run). With no bus bound the
// noteSpawn/voidSpawn are no-ops, so an entry-owner turn is byte-for-byte unchanged.
type countingSpawner struct {
	inner task.TaskSpawner
	sinks *settleSinkRegistry
	id    string
}

func (c *countingSpawner) Spawn(spec task.TaskSpec, detector task.SettleDetector) task.SpawnResult {
	c.sinks.noteSpawn(c.id)
	res := c.inner.Spawn(spec, detector)
	if res.Settled || res.Deduped || res.Blocked != "" {
		c.sinks.voidSpawn(c.id)
	}
	return res
}
