// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
//
// The structured handoff one call_subagent call declares beside its
// natural-language request: the fields the call names, the grants the assembled
// sub-agent actually holds, and the envelope the boundary renders from those flat
// parameters. The envelope is held to Validate where the call is admitted; the
// delegated turn itself receives a batch of ordinary input facts the framework
// commits — the instruction it runs under, one material projection per source
// the call handed over, and the task that closes the batch.
package subagent

import (
	"context"
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

// envelope renders the handoff envelope one call declares, beside the grants the
// assembled sub-agent holds. The envelope is rendered here, at the call boundary,
// and held to Validate where the call is admitted: rendering never decides whether
// the call may pass, so the acceptance gate stays in one place. A call that
// declares no handoff builds none, so its request keeps the shape it carried
// without one.
func (a callArgs) envelope(assembly *Assembly) *agent.HandoffEnvelope {
	if a.Handoff == nil {
		return nil
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
	return &envelope
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

// FactReader reads the committed fact one handoff reference names. A run answers
// it through its own committed-fact read face (Run.CommittedEvent), so a ref
// renders the fact as it committed and never a reconstruction of it; a read that
// refuses the identity renders as the missing reference it is.
type FactReader func(ctx context.Context, id event.EventID) (event.Event, error)

// externalMaterial renders the input one external agent face reads: the face
// has no consumption loop of its own, so the framework performs the same
// mechanical assembly a resident loop performs — the sources a handoff names
// render in front of the task, each under its own heading, and the task
// statement follows as it stands. A face beside no handoff reads the task
// alone.
func externalMaterial(ctx context.Context, turn Turn, read FactReader) string {
	if turn.Handoff == nil {
		return turn.Input
	}
	block := turn.Handoff.ExternalContext()
	if read != nil {
		for _, ref := range turn.Handoff.Refs {
			named, err := read(ctx, ref)
			if err != nil || named.Key == "" {
				continue
			}
			body := named.Message.Content
			if body == "" {
				body = named.Type
			}
			if runes := []rune(body); len(runes) > 128 {
				body = string(runes[:128]) + "…\n  full text retrievable by segment"
			}
			block += fmt.Sprintf("refs:\n- %s\n  %s\n", named.Key, body)
		}
	}
	if turn.Input == "" {
		return block
	}
	return block + "\n" + turn.Input
}
