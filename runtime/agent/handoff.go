package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/SpellingDragon/tagent/api/event"
	a2aprotocol "trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// HandoffEnvelope is one cross-agent handoff in structured form: the task the
// delegated turn serves, the acceptance constraints its result is held to, the
// capabilities the handoff grants it, the decisions it may not take on its own and
// the stored facts it starts from. It travels beside the natural-language request
// of the call and never replaces it, and one schema serves every carrier: the
// JSON field names below are the wire names it travels under, an A2A data part
// carries that JSON as its payload, and the local channel renders the same
// fields as the text block ExternalContext states.
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
	Intent string `json:"intent"`
	// Constraints lists the acceptance criteria the handed-back result must satisfy.
	Constraints []string `json:"constraints,omitempty"`
	// Grants lists the capabilities the handoff grants the delegated turn, each
	// named with the kind it belongs to (tool, skill or mcp).
	Grants []string `json:"grants,omitempty"`
	// PendingDecisions lists the decisions the delegated turn may not take on its own.
	PendingDecisions []string `json:"pending_decisions,omitempty"`
	// Refs names the stored facts the handed-off context starts from.
	Refs []event.EventID `json:"refs,omitempty"`
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

// ErrHandoffPartNotData reports a part that is not an A2A data part, so no
// envelope payload sits in it.
var ErrHandoffPartNotData = errors.New("agent: the part is not a data part")

// ErrHandoffPartPayload reports a data part whose payload does not decode as a
// handoff envelope.
var ErrHandoffPartPayload = errors.New("agent: the data part carries no handoff envelope")

// DataPart returns the A2A data part one handoff travels as on the remote
// channel: the envelope itself as the structured payload, under the field names
// the record declares, so the receiver decodes the same schema the local channel
// renders as text.
func (h HandoffEnvelope) DataPart() a2aprotocol.DataPart {
	return a2aprotocol.NewDataPart(h)
}

// FromDataPart reports the handoff envelope one A2A data part carries. A payload
// already decoded into a map, an envelope carried in process and JSON kept as
// bytes are each read. A part of another kind, or a payload that does not decode,
// is refused by name.
//
// The envelope is decoded, not accepted: the caller holds it to Validate against
// the acceptance the receiving side owes, so a decoded envelope naming no intent
// is still refused there.
func FromDataPart(part a2aprotocol.Part) (HandoffEnvelope, error) {
	payload, err := handoffPayload(part)
	if err != nil {
		return HandoffEnvelope{}, err
	}
	raw, err := handoffPayloadJSON(payload)
	if err != nil {
		return HandoffEnvelope{}, err
	}
	var envelope HandoffEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return HandoffEnvelope{}, fmt.Errorf("%w: %v", ErrHandoffPartPayload, err)
	}
	return envelope, nil
}

// handoffPayload returns the payload of one part, refusing a part of another kind
// and a data part that carries nothing.
func handoffPayload(part a2aprotocol.Part) (any, error) {
	var payload any
	switch typed := part.(type) {
	case a2aprotocol.DataPart:
		payload = typed.Data
	case *a2aprotocol.DataPart:
		if typed == nil {
			return nil, ErrHandoffPartNotData
		}
		payload = typed.Data
	default:
		return nil, ErrHandoffPartNotData
	}
	if payload == nil {
		return nil, ErrHandoffPartPayload
	}
	return payload, nil
}

// handoffPayloadJSON renders one payload as the JSON an envelope decodes from;
// a payload that already is JSON keeps its own bytes.
func handoffPayloadJSON(payload any) ([]byte, error) {
	switch typed := payload.(type) {
	case json.RawMessage:
		return typed, nil
	case []byte:
		return typed, nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrHandoffPartPayload, err)
	}
	return raw, nil
}
