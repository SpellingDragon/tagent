// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
//
// The structured handoff one call_subagent call declares beside its
// natural-language request: the fields the call names, the grants the assembled
// sub-agent actually holds, and the envelope the delegated turn receives.
package subagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/agent"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// handoffArgs is the declared handoff shape one call carries: the intent the
// delegated turn serves, the acceptance constraints its result is held to, the
// decisions it may not take on its own and the stored facts it starts from.
type handoffArgs struct {
	// Intent is the task the delegated turn exists to serve.
	Intent string `json:"intent"`
	// Constraints lists the acceptance criteria the result must satisfy.
	Constraints []string `json:"constraints"`
	// PendingDecisions lists the decisions the delegated turn may not take on its own.
	PendingDecisions []string `json:"pending_decisions"`
	// Refs names the stored facts the handed-off context starts from.
	Refs []string `json:"refs"`
}

// handoffFields is the closed key set the declared handoff names.
var handoffFields = map[string]bool{
	"intent":            true,
	"constraints":       true,
	"pending_decisions": true,
	"refs":              true,
}

// ErrHandoffArguments reports a handoff object naming a key the declaration does not name.
var ErrHandoffArguments = errors.New("call_subagent: the handoff names no declared field")

// envelope builds the handoff envelope one call declares, beside the grants the
// assembled sub-agent holds. A call that declares no handoff builds none, so its
// request keeps the shape it carried without one.
func (a callArgs) envelope(assembly *Assembly) (*agent.HandoffEnvelope, error) {
	if a.Handoff == nil {
		return nil, nil
	}
	envelope := agent.HandoffEnvelope{
		Intent:           a.Handoff.Intent,
		Constraints:      append([]string(nil), a.Handoff.Constraints...),
		Grants:           grantsOf(assembly),
		PendingDecisions: append([]string(nil), a.Handoff.PendingDecisions...),
	}
	for _, ref := range a.Handoff.Refs {
		envelope.Refs = append(envelope.Refs, event.EventID(ref))
	}
	if err := envelope.Validate(); err != nil {
		return nil, err
	}
	return &envelope, nil
}

// grantsOf reports the capabilities one assembly hands the delegated turn: the
// tool, skill and MCP references it declares, each named with the kind it belongs
// to.
func grantsOf(assembly *Assembly) []string {
	if assembly == nil {
		return nil
	}
	out := make([]string, 0, len(assembly.Tools)+len(assembly.Skills)+len(assembly.MCP))
	for _, name := range assembly.Tools {
		out = append(out, "tool:"+name)
	}
	for _, name := range assembly.Skills {
		out = append(out, "skill:"+name)
	}
	for _, name := range assembly.MCP {
		out = append(out, "mcp:"+name)
	}
	return out
}

// checkHandoffKeys refuses a handoff object naming a key the declaration does not name.
func checkHandoffKeys(raw json.RawMessage) error {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return fmt.Errorf("%w: %v", ErrArguments, err)
	}
	unknown := make([]string, 0, len(probe))
	for key := range probe {
		if !handoffFields[key] {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("%w: %s", ErrHandoffArguments, joinNames(unknown))
}

// handoffSchema is the declaration the model reads for the structured handoff.
func handoffSchema() *trpctool.Schema {
	return &trpctool.Schema{
		Type: "object",
		Description: "the structured handoff the delegated turn receives beside the request: " +
			"the intent it serves and the acceptance constraints its result is held to are required, " +
			"while the grants are read from the assembled sub-agent instead of from this object",
		Properties: map[string]*trpctool.Schema{
			"intent": {
				Type:        "string",
				Description: "the task the delegated turn exists to serve",
			},
			"constraints": {
				Type:        "array",
				Items:       &trpctool.Schema{Type: "string"},
				Description: "the acceptance criteria the result must satisfy",
			},
			"pending_decisions": {
				Type:        "array",
				Items:       &trpctool.Schema{Type: "string"},
				Description: "the decisions the delegated turn may not take on its own",
			},
			"refs": {
				Type:        "array",
				Items:       &trpctool.Schema{Type: "string"},
				Description: "the stored fact identities the handed-off context starts from",
			},
		},
	}
}
