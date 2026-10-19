// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
//
// The unconfirmed-delivery replay engine: the obligations a committed record
// still owes, re-delivered at the owner the current orchestration binding
// resolves. The durable fact surface beside it lives with the storage tiers in
// memory/ledger.
package memory

import (
	"context"
	"errors"
	"sync"

	"github.com/SpellingDragon/tagent/api/event"
)

// DeliveryRecord is one delivery obligation as the replay surface carries it:
// the stable notice identity beside the delivery routing frozen at the
// transition that committed it. Its fields carry built-in and event types only,
// so the task layer's committed envelopes map onto it without a package edge.
type DeliveryRecord struct {
	// TaskID is the task the notification belongs to.
	TaskID string
	// Round is the execution round the notification belongs to.
	Round int64
	// Kind is the notification category within the round.
	Kind string
	// Seq is the stable sequence number within the category.
	Seq int64
	// Event is the identity of the committed fact the notification points at.
	Event event.EventID
	// Target is the address the notification is delivered to, frozen at the
	// transition that committed it.
	Target event.Address
	// Source is the address the notification comes from.
	Source event.Address
	// TargetInstance is the runtime instance that receives the notification.
	TargetInstance event.InstanceID
}

// DeliverySink admits one replayed delivery at its frozen target. A nil error
// states the explicit acceptance of the target — the only answer that advances
// the delivery state of that notification — so the engine marks the record
// confirmed behind it and nothing else.
type DeliverySink func(ctx context.Context, rec DeliveryRecord) error

// OwnerLookup resolves the owner a delivery's target belongs to under the
// current orchestration binding. It is the same function seam the delivery sink
// beside it is: one operation, supplied per call, with nothing captured from an
// earlier run. Its signature carries built-in and event types only, so the
// state layer's binding owners satisfy it structurally and no package edge is
// required in either direction. A target the current binding refuses is
// reported by name through the error.
type OwnerLookup func(ctx context.Context, owner event.AgentID, target event.AgentID) (event.AgentID, error)

// ErrReplayUnwired reports a replay that is not wired to an owner resolver and
// a delivery sink, so no delivery could be located or admitted.
var ErrReplayUnwired = errors.New("memory: the replay is not wired to an owner resolver and a delivery sink")

// deliveryID is the stable identity one confirmed delivery is recognized by.
type deliveryID struct {
	task  string
	round int64
	kind  string
	seq   int64
}

// id returns the stable identity of one delivery record.
func (r DeliveryRecord) id() deliveryID {
	return deliveryID{task: r.TaskID, round: r.Round, kind: r.Kind, seq: r.Seq}
}

// Replay is the unconfirmed-delivery replay engine: it re-delivers the
// obligations a committed record still owes, locating each delivery's owner
// under the current orchestration binding before it admits anything. A delivery
// whose owner resolution is refused keeps its obligation, is re-routed nowhere
// and is never reported as delivered; only a sink that answers with explicit
// acceptance marks a record confirmed.
//
// The engine holds no wrapper, binding or lease: the resolver and the sink are
// supplied per call and nothing an earlier run captured is revived through it.
// The engine persists nothing of its own, so a replay reuses the committed
// task records and introduces no new orchestration persistence format.
type Replay struct {
	mu        sync.Mutex
	confirmed map[deliveryID]struct{}
}

// NewReplay returns an empty replay engine. A fresh engine confirms nothing
// yet, so every obligation it is handed is replayed; the targets it re-delivers
// to accept idempotently by the notification's stable identity, so a repeated
// replay records no second fact.
func NewReplay() *Replay {
	return &Replay{confirmed: map[deliveryID]struct{}{}}
}

// Ack records the explicit acceptance the target stated for one delivered
// record, so a later replay of the same identity delivers nothing again.
func (r *Replay) Ack(rec DeliveryRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.confirmed[rec.id()] = struct{}{}
}

// Confirmed reports whether the target explicitly accepted one delivery.
func (r *Replay) Confirmed(rec DeliveryRecord) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.confirmed[rec.id()]
	return ok
}

// ReplayOutcome is the per-delivery result of one replay: the owner the current
// binding resolved the target to, and the named refusal of a delivery that was
// not admitted.
type ReplayOutcome struct {
	// Delivery is the obligation the outcome is about.
	Delivery DeliveryRecord
	// Owner is the owner the current binding resolved the delivery's target
	// to; it is empty when the resolution refused.
	Owner event.AgentID
	// Delivered reports whether the target explicitly accepted the delivery.
	Delivered bool
	// Skipped reports a delivery an earlier acceptance already confirmed, so
	// this replay delivered it nothing a second time.
	Skipped bool
	// Refused is the named refusal of the delivery: the owner resolution or
	// the target refused it. The obligation stays open either way.
	Refused error
}

// ReplayReport is the report one replay returns, one outcome per delivery, in
// the order the pending obligations were handed in.
type ReplayReport struct {
	// Outcomes lists the per-delivery outcomes of the replay.
	Outcomes []ReplayOutcome
}

// Replay re-delivers every unconfirmed delivery in pending. Each delivery is
// located first: its target's owner is resolved under the current orchestration
// binding, and a resolution refused by name keeps the obligation open and
// delivers nothing — the delivery is never faked as a success and never
// silently re-routed to another target. Only a sink that answers with explicit
// acceptance marks the record confirmed.
func (r *Replay) Replay(ctx context.Context, pending []DeliveryRecord, owners OwnerLookup, sink DeliverySink) (ReplayReport, error) {
	if owners == nil || sink == nil {
		return ReplayReport{}, ErrReplayUnwired
	}
	report := ReplayReport{Outcomes: make([]ReplayOutcome, 0, len(pending))}
	for _, rec := range pending {
		outcome := ReplayOutcome{Delivery: rec}
		if r.Confirmed(rec) {
			outcome.Skipped = true
			report.Outcomes = append(report.Outcomes, outcome)
			continue
		}
		resolved, err := owners(ctx, event.AgentID(rec.Source.AgentID), event.AgentID(rec.Target.AgentID))
		if err != nil {
			outcome.Refused = err
			report.Outcomes = append(report.Outcomes, outcome)
			continue
		}
		if err := sink(ctx, rec); err != nil {
			outcome.Refused = err
			report.Outcomes = append(report.Outcomes, outcome)
			continue
		}
		r.Ack(rec)
		outcome.Owner = resolved
		outcome.Delivered = true
		report.Outcomes = append(report.Outcomes, outcome)
	}
	return report, nil
}
