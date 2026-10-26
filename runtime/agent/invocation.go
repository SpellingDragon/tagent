package agent

import (
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// 契约: docs/wiki/agent/event-flow.md#e2e-turn-sequence
//
// Invocation is the scheduling record of one responsibility range: the identity
// and address of the range and the input that is ready for its next decision.
//
// The record is the agent's own view of the range: the session site it belongs to
// stays the state owner's ledger, and the terminal of each decision is reported
// on the turn beside it. One invocation may hold several decisions, because an
// observation that returns from a background task makes new input ready and
// drives the next decision of the same range.
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
