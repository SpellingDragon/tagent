package agent

import (
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/task"
)

// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
//
// Delegation is the record of one sub-agent call the agent admitted: the
// invocation and native call it is associated with, the address the delegated
// turn runs in, and the progress and terminal the admitted task reports under.
//
// The call is admitted as a task, so the caller is answered by a receipt and this
// record carries the later progress and terminal of that same task. The delegated
// turn runs in the session the application selected explicitly, and a delegation
// never creates a private writable history of its own.
type Delegation struct {
	// InvocationID is the invocation that issued the delegation.
	InvocationID event.InvocationID
	// DecisionID is the decision that issued the delegation; it keeps the native
	// call identifier scoped to the call that produced it, because a native call
	// identifier is not globally unique.
	DecisionID event.DecisionID
	// CallID is the native provider call identifier of the delegated call, kept
	// byte-identical and never regenerated.
	CallID string
	// TaskID is the task the delegated call was admitted as, so its progress and
	// terminal associate with the call that issued it.
	TaskID event.TaskID
	// Target is the agent session address the delegated turn runs in.
	Target event.Address
	// Status is the phase the admitted task reached; a terminal status is
	// reported only after the obligations of the delegated call are cleared.
	Status task.Status
	// Progress lists the notifications the admitted task produced, each with its
	// stable identity, so a repeated delivery of one notification records no
	// second fact.
	Progress []task.Notice
}

// CloseRequest is one close of an agent: the address being closed and the
// deadline the close may spend before it reports what is still open.
//
// A close refuses new input first and settles the obligations already accepted,
// and it never reports a drain while a producer has not stopped: a producer that
// answers that it has not stopped keeps the drain open until real termination is
// confirmed or the deadline arrives.
type CloseRequest struct {
	// Address is the address being closed; new input to it is refused first.
	Address event.Address
	// Deadline is the instant after which the close reports the obligations it
	// could not settle instead of waiting longer; a zero deadline waits without a
	// deadline.
	Deadline time.Time
}

// DrainReport is the outcome of one close: whether every accepted obligation
// reached real termination, and the obligations that were still open when the
// close reported.
type DrainReport struct {
	// Drained reports whether every accepted obligation reached real termination.
	// It stays false while any producer has not confirmed its stop.
	Drained bool
	// Unsettled lists the obligations that were still open when the close
	// reported, each with the reason it could not be settled.
	Unsettled []UnsettledObligation
}

// UnsettledObligation is one obligation a close could not settle, paired with the
// reason it stayed open.
type UnsettledObligation struct {
	// TaskID is the task whose obligation stayed open.
	TaskID event.TaskID
	// Reason is the readable reason the obligation could not be settled.
	Reason string
}
