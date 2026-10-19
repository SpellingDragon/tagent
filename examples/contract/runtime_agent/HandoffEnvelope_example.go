package agentexample

import (
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// ExampleHandoffEnvelope renders one cross-agent handoff in structured form: the
// envelope names the task, the acceptance constraints, the grants, the pending
// decisions and the refs, validates itself and renders the external-context block a
// delegated turn reads in front of its natural-language request.
//
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func ExampleHandoffEnvelope() {
	envelope := agent.HandoffEnvelope{
		Intent:           "review the change",
		Constraints:      []string{"every finding names a file"},
		Grants:           []string{"tool:exec_run"},
		PendingDecisions: []string{"whether the change ships"},
		Refs:             []event.EventID{"ev-context-1"},
	}
	if err := envelope.Validate(); err != nil {
		return
	}
	_ = envelope.ExternalContext()
}
