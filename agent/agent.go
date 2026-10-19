// 契约: docs/wiki/agent/agent-architecture.md#module-position
//
// Package agent is the runtime's event scheduling and single model decision
// layer: it drives the events of one invocation with the binding generation that
// state lent it, admits a sub-agent call as a task, and closes an agent by real
// obligation settlement.
//
// The package owns no state of its own. A decision borrows one generation of the
// state binding and keeps it for the whole decision, and the task layer keeps the
// obligations of every call the agent admitted. The execution face is a port the
// composition root wires; the loop, the delegation ledger and the close gate are
// the behaviour behind it.
package agent

import (
	"context"

	"github.com/SpellingDragon/tagent/task"
)

// Agent is one agent's execution face: the single model decision entry that
// drives the ready events of an invocation, together with the sub-delegation and
// close faces of the same execution.
//
// The decision entry takes the binding generation effective when the decision
// starts executing, so a publication during a decision never changes the binding
// it runs with, and input that is still queued is not bound in advance.
// The redline list (4 rules):
// MUST take the binding generation effective at the start of the decision and keep it for what it sets in motion.
// MUST reduce the model stream to its single terminal through the one reduction entry.
// MUST NOT run a second tool-iteration loop beside the event loop, and MUST NOT judge a terminal on stream mechanics.
// MUST NOT report a receipt as a result: an admitted call settles only after its own obligations clear.
type Agent interface {
	// Decide runs one model decision for one invocation: it takes the session's
	// decision slot, borrows the binding generation effective at that moment, and
	// reduces the model stream to its single terminal. A session grants at most
	// one effective model decision at a time, so input admitted while a decision
	// is in flight waits for the next round instead of starting a second one.
	Decide(ctx context.Context, inv Invocation) (Decision, error)

	// Delegate admits one sub-agent call as an asynchronous task and returns the
	// admission receipt of that call. The receipt states admission rather than a
	// result: the call settles only after its own obligations are cleared, and
	// its progress and terminal arrive as notifications of the admitted task.
	Delegate(ctx context.Context, req Delegation) (task.AdmissionReceipt, error)

	// Close refuses new input, settles the obligations that were already
	// accepted, and reports whether the drain reached real termination.
	Close(ctx context.Context, req CloseRequest) (DrainReport, error)
}
