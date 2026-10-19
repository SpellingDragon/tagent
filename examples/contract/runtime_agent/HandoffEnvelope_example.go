package agentexample

import (
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// ExampleHandoffEnvelope renders one cross-agent handoff in structured form: the
// envelope names the task, the acceptance constraints, the grants, the pending
// decisions and the refs, validates itself and renders the external-context block a
// delegated turn reads in front of its natural-language request. The remote channel
// carries the same record as one data part, so a data part read back names the same
// fields and is held to the same validation.
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

	restored, err := agent.FromDataPart(envelope.DataPart())
	if err != nil {
		return
	}
	if err := restored.Validate(); err != nil {
		return
	}
}
