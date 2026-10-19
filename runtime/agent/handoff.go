package agent

import (
	"errors"
	"strings"

	"github.com/SpellingDragon/tagent/api/event"
)

// HandoffEnvelope is one cross-agent handoff in structured form: the task the
// delegated turn serves, the acceptance constraints its result is held to, the
// capabilities the handoff grants it, the decisions it may not take on its own and
// the stored facts it starts from. It travels beside the natural-language request
// of the call and never replaces it.
//
// The four segments of the handoff contract map onto this record:
// - the task is Intent;
// - the context summary is Refs beside the natural-language request;
// - the deliverable stays the natural-language request's own outcome;
// - the acceptance criteria are Constraints.
//
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
type HandoffEnvelope struct {
	// Intent is the task the delegated turn exists to serve.
	Intent string
	// Constraints lists the acceptance criteria the handed-back result must satisfy.
	Constraints []string
	// Grants lists the capabilities the handoff grants the delegated turn, each
	// named with the kind it belongs to (tool, skill or mcp).
	Grants []string
	// PendingDecisions lists the decisions the delegated turn may not take on its own.
	PendingDecisions []string
	// Refs names the stored facts the handed-off context starts from.
	Refs []event.EventID
}

// ErrHandoffIntentMissing reports an envelope that names no intent, so no delegated
// turn could tell which task it serves.
var ErrHandoffIntentMissing = errors.New("agent: the handoff envelope names no intent")

// ErrHandoffConstraintsMissing reports an envelope that carries no non-empty
// acceptance constraint, so no handed-back result could be accepted or refused
// against it.
var ErrHandoffConstraintsMissing = errors.New("agent: the handoff envelope names no acceptance constraint")

// Validate reports whether the envelope names what a delegated turn could serve and
// be accepted against: a non-empty intent and at least one non-empty constraint.
func (h HandoffEnvelope) Validate() error {
	if strings.TrimSpace(h.Intent) == "" {
		return ErrHandoffIntentMissing
	}
	for _, constraint := range h.Constraints {
		if strings.TrimSpace(constraint) != "" {
			return nil
		}
	}
	return ErrHandoffConstraintsMissing
}

// ExternalContext renders the envelope as the structured block one delegated turn
// carries in front of its natural-language request: the intent, the acceptance
// constraints, the grants, the pending decisions and the refs, each under its own
// heading in that fixed order. A list with no entry keeps its heading and states
// that it carries none, so the shape of the block never depends on its content.
func (h HandoffEnvelope) ExternalContext() string {
	var block strings.Builder
	block.WriteString("[handoff envelope]\n")
	block.WriteString("intent: " + strings.TrimSpace(h.Intent) + "\n")
	writeHandoffList(&block, "constraints", h.Constraints)
	writeHandoffList(&block, "grants", h.Grants)
	writeHandoffList(&block, "pending_decisions", h.PendingDecisions)
	refs := make([]string, 0, len(h.Refs))
	for _, ref := range h.Refs {
		refs = append(refs, string(ref))
	}
	writeHandoffList(&block, "refs", refs)
	return block.String()
}

// writeHandoffList renders one named list of the envelope block; a list with no
// entry states that it carries none.
func writeHandoffList(block *strings.Builder, name string, values []string) {
	if len(values) == 0 {
		block.WriteString(name + ": (none)\n")
		return
	}
	block.WriteString(name + ":\n")
	for _, value := range values {
		block.WriteString("- " + value + "\n")
	}
}
