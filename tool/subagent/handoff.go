// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
//
// The structured handoff one call_subagent call declares beside its
// natural-language request: the fields the call names, the grants the assembled
// sub-agent actually holds, and the envelope the delegated turn receives.
package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory/replay"
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

// FactReader reads the committed fact one handoff reference names. A run answers
// it through its own committed-fact read face (Run.CommittedEvent), so a ref
// renders the fact as it committed and never a reconstruction of it; a read that
// refuses the identity renders as the missing reference it is.
type FactReader func(ctx context.Context, id event.EventID) (event.Event, error)

const (
	// defaultRefBudget is the token budget one handoff's refs render under when
	// the builder names none. It is the token budget the request assembly packs
	// under, so a ref degrades exactly as a projection entry does: its full text
	// while it fits, then a ticket, then its summary.
	defaultRefBudget = 256

	// refSummaryRunes bounds the one-line read a ref entry carries when the
	// committed fact stored no summary of its own: the read is a prefix of the
	// original, never a rewrite of it.
	refSummaryRunes = 120

	// refsHeading is the heading the envelope block renders its refs section
	// under; the refs section is the last one ExternalContext writes.
	refsHeading = "\nrefs:\n"

	// missingRefLine states a reference the read face holds no fact for, so the
	// turn reads a miss beside the material that did resolve.
	missingRefLine = "  (missing)\n"
	// overBudgetRefLine states a reference the budget assembly placed under no
	// level, so the turn reads the bound instead of a silently absent fact.
	overBudgetRefLine = "  (over budget)\n"
	// emptyRefLine states a reference whose fact carries no text to present.
	emptyRefLine = "  (no content)\n"
)

// externalContext renders the block one delegated turn carries in front of its
// request. A handoff that carries refs beside a reader renders each ref with the
// committed fact it names, under the same budget assembly the request boundary
// uses; every other case renders the envelope exactly as ExternalContext states
// it, so a handoff carrying no refs keeps the block it always carried.
func externalContext(ctx context.Context, h agent.HandoffEnvelope, read FactReader, budget int) string {
	envelope := h.ExternalContext()
	if read == nil || len(h.Refs) == 0 {
		return envelope
	}
	details, ok := refDetails(ctx, h.Refs, read, budget)
	if !ok {
		return envelope
	}
	return withRefDetails(envelope, details)
}

// refDetails renders the refs section of one handoff: every reference in its
// declared order and once each, beside the fact it names. A reference the read
// face refuses states that it is missing instead of refusing the whole handoff —
// the material that did resolve stays usable — and a reference the budget
// assembly could not place states its bound instead of vanishing.
func refDetails(ctx context.Context, refs []event.EventID, read FactReader, budget int) ([]string, bool) {
	if budget < 1 {
		budget = defaultRefBudget
	}
	projection := replay.NewReplayProjection()
	order := make([]event.EventID, 0, len(refs))
	missing := make(map[event.EventID]bool, len(refs))
	seen := make(map[event.EventID]bool, len(refs))
	for _, ref := range refs {
		if seen[ref] {
			continue
		}
		seen[ref] = true
		order = append(order, ref)
		if ref == "" {
			missing[ref] = true
			continue
		}
		evt, err := read(ctx, ref)
		if err != nil {
			missing[ref] = true
			continue
		}
		projection.Append(ctx, refEntry(ref, evt))
	}
	assembled, err := projection.AssembleBudgetPrefix(ctx, budget, -1)
	if err != nil {
		return nil, false
	}
	shown := make(map[event.EventID]string, len(assembled))
	for _, entry := range assembled {
		shown[entry.Key] = entry.Content
	}
	lines := make([]string, 0, 2*len(order))
	for _, ref := range order {
		lines = append(lines, "- "+string(ref)+"\n")
		content, held := shown[ref]
		switch {
		case missing[ref]:
			lines = append(lines, missingRefLine)
		case !held:
			lines = append(lines, overBudgetRefLine)
		case strings.TrimSpace(content) == "":
			lines = append(lines, emptyRefLine)
		default:
			lines = append(lines, indentContent(content)...)
		}
	}
	return lines, true
}

// refEntry projects one committed fact onto the entry the budget assembly
// presents it as: the canonical identity of the reference, the frozen type of the
// fact, the one-line read stored beside it and the text the model view speaks for
// it. It is the mapped entry of a committed fact, so a ref degrades exactly as
// the fact does on the request path.
func refEntry(ref event.EventID, evt event.Event) replay.ProjectionEntry {
	content := refContentOf(evt.Message)
	return replay.ProjectionEntry{
		Key:     ref,
		Type:    evt.Type,
		Summary: refSummaryOf(evt.Meta[event.MetaKeyEventSummary], content),
		Content: content,
		Time:    evt.Timestamp,
	}
}

// refContentOf reports the text one committed message speaks for: the content as
// it stands, and otherwise the call or the tool face the message carries, so a
// ref naming a call the model issued is never presented as an empty fact.
func refContentOf(msg model.Message) string {
	if msg.Content != "" {
		return msg.Content
	}
	calls := make([]string, 0, len(msg.ToolCalls))
	for _, call := range msg.ToolCalls {
		calls = append(calls, "[call "+call.Function.Name+"]")
	}
	if len(calls) > 0 {
		return strings.Join(calls, " ")
	}
	if msg.ToolName != "" {
		return "[tool " + msg.ToolName + " returned no text]"
	}
	return ""
}

// refSummaryOf reports the one-line read a ref entry carries: the summary the
// fact stored beside its original, and otherwise the first line of the original
// itself, bounded. It reads the original and never rewrites it.
func refSummaryOf(summary, content string) string {
	if summary != "" {
		return summary
	}
	line := content
	if at := strings.IndexByte(line, '\n'); at >= 0 {
		line = line[:at]
	}
	runes := []rune(line)
	if len(runes) <= refSummaryRunes {
		return line
	}
	return string(runes[:refSummaryRunes]) + "…"
}

// indentContent renders one presented fact under its reference, one indented line
// per line of the fact, so each entry of the refs section stays readable.
func indentContent(content string) []string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, "  "+line+"\n")
	}
	return out
}

// withRefDetails replaces the bare identity list of the envelope block with the
// rendered refs section. The refs section is the last one the block states, so
// every field before it keeps the text ExternalContext wrote.
func withRefDetails(envelope string, details []string) string {
	at := strings.LastIndex(envelope, refsHeading)
	if at < 0 {
		return envelope
	}
	return envelope[:at+1] + "refs:\n" + strings.Join(details, "")
}
