package agent

import (
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
	"github.com/SpellingDragon/tagent/state"
)

// 契约: docs/wiki/agent/event-flow.md#e2e-turn-sequence
//
// Invocation is the scheduling record of one responsibility range: the identity
// and address of the range, the input that is ready for its next decision, the
// decision slot the session granted it, and the terminal its latest decision
// reduced to.
//
// The record is the agent's own view of the range, so the slot it carries is a
// projection of the session site rather than a second source of truth. One
// invocation may hold several decisions, because an observation that returns from
// a background task makes new input ready and drives the next decision of the
// same range.
type Invocation struct {
	// InvocationID identifies the responsibility range.
	InvocationID event.InvocationID
	// Address is the explicit agent session address the range belongs to; it is
	// never defaulted to a most-recent session.
	Address event.Address
	// Instance is the runtime instance the range was admitted on; rebuilding an
	// agent under the same name yields a different instance.
	Instance event.InstanceID
	// Ready lists the input aggregated for the next decision of this range, in
	// the order the session admitted it.
	Ready []event.Event
	// Slot is the decision slot of the session as this range sees it.
	Slot DecisionSlot
	// Terminal is the single terminal the latest decision of this range reduced
	// to, or KindUnknown while no decision of the range has reached a terminal.
	Terminal model.Kind
}

// Aggregate merges the input that is ready for this range into the batch of its
// next decision.
//
// Ready input of one invocation belongs to one decision, so input that belongs to
// a different invocation of the same session is never mixed into the same batch.
func (inv *Invocation) Aggregate(ready []event.Event) error {
	return errNotImplemented
}

// DecisionSlot records the execution occupancy of one session: the invocation
// whose effective model decision is in flight and the identity of that decision.
//
// A session grants at most one effective model decision at a time, so the other
// invocations of the session keep their input queued until the slot is free. A
// call that waits for an approval holds no slot, so such a wait never blocks the
// other invocations of the session.
type DecisionSlot struct {
	// InvocationID is the range that holds the slot; it is empty while no
	// decision is in flight.
	InvocationID event.InvocationID
	// DecisionID identifies the decision in flight; it is empty while the slot is
	// free.
	DecisionID event.DecisionID
}

// Decision is one model decision of an invocation: the generation of the state
// binding it borrowed, the native request assembled from the ready input of the
// range, and the single terminal its stream reduced to.
//
// Admission is not settlement: a call this decision admitted keeps its obligation
// until that obligation is explicitly cleared. A response that arrives after the
// terminal is kept as diagnostics only, so it dispatches no tool and never
// revives the terminal.
type Decision struct {
	// DecisionID identifies this decision.
	DecisionID event.DecisionID
	// InvocationID is the range this decision belongs to.
	InvocationID event.InvocationID
	// Binding is the binding generation borrowed from state when the decision
	// started executing; a publication during the decision does not change it.
	Binding state.AgentBinding
	// Request is the native request assembled from the ready input of the range.
	Request model.Request
	// Terminal is the single terminal the model stream of this decision reduced
	// to; exactly one terminal is recorded per decision.
	Terminal model.Kind
}
