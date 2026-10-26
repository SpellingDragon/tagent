// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
package subagent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestDeclaredHandoffEnvelope holds the envelope one call declares beside its request:
// - a declared handoff becomes an envelope carrying the intent, the constraints and the pending decisions the call named
// - the grants of that envelope are the tool, skill and MCP references the assembly declares, each with its kind
// - the refs of that envelope are the stored fact identities the call named
// - a call declaring no handoff builds no envelope, so its request keeps the shape it carried without one
// - a handoff naming no intent or no acceptance constraint is refused by name
// - a handoff object naming a key the declaration does not name is refused by name
// - the declaration the model reads names the four declared handoff keys
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestDeclaredHandoffEnvelope(t *testing.T) {
	assembly := &Assembly{
		Definition: Definition{
			Name:   "reviewer",
			Model:  "model-reviewer",
			Skills: []string{"go-service-ops"},
			Tools:  []string{"exec_run"},
			MCP:    []string{"mcp-lsp"},
		},
	}

	t.Run("a declared handoff becomes an envelope", func(t *testing.T) {
		args := callArgs{Handoff: &handoffArgs{
			Intent:           "review the change",
			Constraints:      []string{"every finding names a file"},
			PendingDecisions: []string{"whether the change ships"},
		}}
		envelope, err := args.envelope(assembly)
		if err != nil {
			t.Fatalf("build the declared envelope = %v, want it accepted", err)
		}
		if envelope == nil {
			t.Fatal("envelope = nil, want the handoff the call declared")
		}
		if envelope.Intent != "review the change" {
			t.Fatalf("envelope intent = %q, want the declared intent", envelope.Intent)
		}
		if len(envelope.Constraints) != 1 || envelope.Constraints[0] != "every finding names a file" {
			t.Fatalf("envelope constraints = %v, want the declared constraints", envelope.Constraints)
		}
		if len(envelope.PendingDecisions) != 1 || envelope.PendingDecisions[0] != "whether the change ships" {
			t.Fatalf("envelope pending decisions = %v, want the declared decisions", envelope.PendingDecisions)
		}
	})

	t.Run("the grants are the assembled sub-agent references", func(t *testing.T) {
		args := callArgs{Handoff: &handoffArgs{
			Intent:      "review the change",
			Constraints: []string{"an explicit verdict"},
		}}
		envelope, err := args.envelope(assembly)
		if err != nil {
			t.Fatalf("build the declared envelope = %v, want it accepted", err)
		}
		want := []string{"tool:exec_run", "skill:go-service-ops", "mcp:mcp-lsp"}
		if len(envelope.Grants) != len(want) {
			t.Fatalf("envelope grants = %v, want %v", envelope.Grants, want)
		}
		for i, grant := range want {
			if envelope.Grants[i] != grant {
				t.Fatalf("envelope grants = %v, want %v", envelope.Grants, want)
			}
		}
	})

	t.Run("the refs are the stored fact identities the call named", func(t *testing.T) {
		args := callArgs{Handoff: &handoffArgs{
			Intent:      "review the change",
			Constraints: []string{"an explicit verdict"},
			Refs:        []string{"ev-context-1", "ev-context-2"},
		}}
		envelope, err := args.envelope(assembly)
		if err != nil {
			t.Fatalf("build the declared envelope = %v, want it accepted", err)
		}
		if len(envelope.Refs) != 2 || string(envelope.Refs[0]) != "ev-context-1" || string(envelope.Refs[1]) != "ev-context-2" {
			t.Fatalf("envelope refs = %v, want the declared fact identities", envelope.Refs)
		}
	})

	t.Run("a call declaring no handoff builds none", func(t *testing.T) {
		envelope, err := callArgs{}.envelope(assembly)
		if err != nil {
			t.Fatalf("build the envelope of a call declaring no handoff = %v, want none", err)
		}
		if envelope != nil {
			t.Fatalf("envelope = %+v, want nil so the request keeps its shape", envelope)
		}
	})

	t.Run("a handoff naming no intent is refused", func(t *testing.T) {
		args := callArgs{Handoff: &handoffArgs{Constraints: []string{"an explicit verdict"}}}
		_, err := args.envelope(assembly)
		if !errors.Is(err, agent.ErrHandoffIntentMissing) {
			t.Fatalf("build an envelope with no intent = %v, want %v", err, agent.ErrHandoffIntentMissing)
		}
	})

	t.Run("a handoff naming no acceptance constraint is refused", func(t *testing.T) {
		args := callArgs{Handoff: &handoffArgs{Intent: "review the change"}}
		_, err := args.envelope(assembly)
		if !errors.Is(err, agent.ErrHandoffConstraintsMissing) {
			t.Fatalf("build an envelope with no constraint = %v, want %v", err, agent.ErrHandoffConstraintsMissing)
		}
	})

	t.Run("a handoff key the declaration does not name is refused", func(t *testing.T) {
		_, _, err := parseCallArgs([]byte(`{"handoff":{"intent":"review","budget":"2"}}`))
		if !errors.Is(err, ErrHandoffArguments) {
			t.Fatalf("parse a handoff naming an undeclared key = %v, want %v", err, ErrHandoffArguments)
		}
	})

	t.Run("the declaration names the four handoff keys", func(t *testing.T) {
		schema := handoffSchema()
		if schema.Type != "object" {
			t.Fatalf("handoff schema type = %q, want an object", schema.Type)
		}
		for _, key := range []string{"intent", "constraints", "pending_decisions", "refs"} {
			if schema.Properties[key] == nil {
				t.Fatalf("handoff schema declares no %q argument", key)
			}
		}
	})
}

// The handoff declaration carries the shape the tool declaration embeds.
var _ *trpctool.Schema = handoffSchema()

// TestHandoffRefsCarryTheirCommittedFacts holds the refs section one delegated turn reads:
// - a ref renders the content of the fact it names under its own identity, not the bare identity alone
// - a fact too large for the budget degrades to a ticket, then to its summary, exactly as a projection entry does
// - a ref the read face refuses states it is missing and leaves the rest of the handoff usable
// - a ref is rendered once however often it is named, and a handoff carrying none renders unchanged
// - a turn carrying no reader renders the bare identities it always carried
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestHandoffRefsCarryTheirCommittedFacts(t *testing.T) {
	ctx := context.Background()
	small := strings.Repeat("the migration decision is recorded under 0a01. ", 2)
	large := strings.Repeat("migration ", 200)

	t.Run("a fact that fits renders in full under its reference", func(t *testing.T) {
		envelope := refEnvelope("ev-small")
		block := externalContext(ctx, envelope, factTable{"ev-small": fact("ev-small", small)}.read, 0)
		if want := "refs:\n- ev-small\n  " + small + "\n"; !strings.Contains(block, want) {
			t.Fatalf("block carries no refs detail %q:\n%s", want, block)
		}
		if strings.Index(block, "pending_decisions:") > strings.Index(block, "refs:") {
			t.Fatalf("refs section is not the last section of the block:\n%s", block)
		}
		for _, want := range []string{"intent: review the change", "- every finding names a file", "- tool:exec_run", "- whether the change ships"} {
			if !strings.Contains(block, want) {
				t.Fatalf("block carries no %q of the envelope:\n%s", want, block)
			}
		}
	})

	t.Run("a fact over the budget degrades to a ticket", func(t *testing.T) {
		block := externalContext(ctx, refEnvelope("ev-large"), factTable{"ev-large": fact("ev-large", large)}.read, 0)
		if !strings.Contains(block, "[ticket ev-large: full text retrievable by segment]") {
			t.Fatalf("block carries no ticket for the fact over the budget:\n%s", block)
		}
		if preview := string([]rune(large)[:refTicketPreviewRunes]) + "…"; !strings.Contains(block, preview) {
			t.Fatalf("block carries no bounded preview %q:\n%s", preview, block)
		}
		if strings.Contains(block, large) {
			t.Fatalf("block carries the whole fact the budget refused:\n%s", block)
		}
	})

	t.Run("a fact the ticket does not fit degrades to its summary", func(t *testing.T) {
		evt := event.Event{
			Key:     "ev-large",
			Type:    event.TypeAgentOutput,
			Meta:    map[string]string{event.MetaKeyEventSummary: "a one-line read of the large fact"},
			Message: model.Message{Content: large},
		}
		block := externalContext(ctx, refEnvelope("ev-large"), factTable{"ev-large": evt}.read, refSummaryBudget)
		if !strings.Contains(block, "  a one-line read of the large fact\n") {
			t.Fatalf("block carries no summary of the fact the ticket did not fit:\n%s", block)
		}
		if strings.Contains(block, "[ticket") || strings.Contains(block, large) {
			t.Fatalf("block carries a level the remaining budget refused:\n%s", block)
		}
	})

	t.Run("a fact the budget cannot place states its bound", func(t *testing.T) {
		block := externalContext(ctx, refEnvelope("ev-large"), factTable{"ev-large": fact("ev-large", large)}.read, 1)
		if want := "refs:\n- ev-large\n" + overBudgetRefLine; !strings.Contains(block, want) {
			t.Fatalf("block carries no %q:\n%s", want, block)
		}
	})

	t.Run("a ref the read face refuses is missing beside the material that resolved", func(t *testing.T) {
		envelope := refEnvelope("ev-small", "ev-missing")
		block := externalContext(ctx, envelope, factTable{"ev-small": fact("ev-small", small)}.read, 0)
		if want := "- ev-small\n  " + small + "\n"; !strings.Contains(block, want) {
			t.Fatalf("block carries no resolved ref %q:\n%s", want, block)
		}
		if want := "- ev-missing\n" + missingRefLine; !strings.Contains(block, want) {
			t.Fatalf("block carries no %q:\n%s", want, block)
		}
		if !strings.Contains(block, "intent: review the change") {
			t.Fatalf("a missing ref refused the handoff instead of stating the miss:\n%s", block)
		}
	})

	t.Run("a ref is rendered once however often it is named", func(t *testing.T) {
		envelope := refEnvelope("ev-small", "ev-small")
		block := externalContext(ctx, envelope, factTable{"ev-small": fact("ev-small", small)}.read, 0)
		if got := strings.Count(block, "- ev-small\n"); got != 1 {
			t.Fatalf("ref rendered %d times, want once:\n%s", got, block)
		}
	})

	t.Run("a handoff carrying no refs renders the envelope unchanged", func(t *testing.T) {
		envelope := agent.HandoffEnvelope{Intent: "review the change", Constraints: []string{"every finding names a file"}}
		if got, want := externalContext(ctx, envelope, factTable{}.read, 0), envelope.ExternalContext(); got != want {
			t.Fatalf("block = %q, want the unchanged envelope %q", got, want)
		}
	})

	t.Run("a builder beside no reader renders the bare identities", func(t *testing.T) {
		envelope := refEnvelope("ev-small")
		if got, want := externalContext(ctx, envelope, nil, 0), envelope.ExternalContext(); got != want {
			t.Fatalf("block = %q, want the unchanged envelope %q", got, want)
		}
	})
}

// TestDelegatedTurnReadsTheRefsContent holds the request one served turn carrying refs reads:
// - the external-context block of that request carries the content of each ref
// - the bare identity of the ref still heads its entry, so the turn can cite it
// - a turn carrying no reader keeps the block it carried before refs rendered details
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestDelegatedTurnReadsTheRefsContent(t *testing.T) {
	ctx := context.Background()
	const content = "the migration decision is recorded under 0a01"

	t.Run("the delegated request carries the fact behind each ref", func(t *testing.T) {
		provider := &serveModel{name: "model-reviewer", answer: "review recorded"}
		builder := Builder{
			Model: func(context.Context, string) (trpcmodel.Model, error) { return provider, nil },
			Facts: factTable{"ev-small": fact("ev-small", content)}.read,
		}
		assembly := &Assembly{Definition: Definition{Name: "reviewer", Model: "model-reviewer"}}
		if _, err := builder.Serve(ctx, assembly, Turn{
			UserID:    "tenant-review",
			SessionID: "session-review",
			Input:     "the change under review",
			Handoff:   refEnvelopePtr("ev-small"),
		}); err != nil {
			t.Fatalf("serve one turn carrying refs = %v, want the assembled agent to answer", err)
		}
		requests := provider.recorded()
		if len(requests) != 1 {
			t.Fatalf("model requests = %d, want exactly one turn", len(requests))
		}
		got := requestContent(requests[0])
		if want := "refs:\n- ev-small\n  " + content + "\n"; !strings.Contains(got, want) {
			t.Fatalf("delegated request carries no refs content %q:\n%s", want, got)
		}
		if !strings.Contains(got, "the change under review") {
			t.Fatalf("delegated request dropped the natural-language request: %q", got)
		}
	})

	t.Run("a turn carrying no reader keeps the bare identity", func(t *testing.T) {
		provider := &serveModel{name: "model-reviewer", answer: "review recorded"}
		builder := Builder{Model: func(context.Context, string) (trpcmodel.Model, error) { return provider, nil }}
		assembly := &Assembly{Definition: Definition{Name: "reviewer", Model: "model-reviewer"}}
		if _, err := builder.Serve(ctx, assembly, Turn{
			UserID:    "tenant-review",
			SessionID: "session-review",
			Input:     "the change under review",
			Handoff:   refEnvelopePtr("ev-small"),
		}); err != nil {
			t.Fatalf("serve one turn beside no reader = %v, want the assembled agent to answer", err)
		}
		got := requestContent(provider.recorded()[0])
		if want := "refs:\n- ev-small\n"; !strings.Contains(got, want) {
			t.Fatalf("delegated request carries no %q:\n%s", want, got)
		}
		if strings.Contains(got, content) {
			t.Fatalf("delegated request carries a fact no reader answered: %q", got)
		}
	})
}

// TestTargetDelegationReadsTheRefsContent holds the refs content a recall-shaped delegation hands its turn:
// - the delegated request carries the content of the event the call named, not its bare key
// - a key the run does not hold is admitted as a missing reference and the turn still runs
// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
func TestTargetDelegationReadsTheRefsContent(t *testing.T) {
	const content = "the migration decision is recorded under 0a01"
	addr := event.Address{AgentID: "caller", SessionID: "session-recall-delegation"}

	t.Run("the recall-shaped call hands the delegated turn the event content", func(t *testing.T) {
		provider := &serveModel{name: "model-recall", answer: "the recall turn was admitted"}
		delegation := &stubDelegation{}
		target := newRecallTarget(t, addr, provider, delegation, factTable{"0a01": fact("0a01", content)})

		out, err := target.Call(callFrameContext(addr), []byte(`{"input":"what did we decide about the migration?","event_keys":["0a01"]}`))
		if err != nil {
			t.Fatalf("call the target tool = %v, want an admission", err)
		}
		if _, ok := out.(task.AdmissionReceipt); !ok {
			t.Fatalf("call result = %#v, want the admission receipt", out)
		}
		waitCompletions(t, delegation, 1)

		got := requestContent(provider.recorded()[0])
		if want := "refs:\n- 0a01\n  " + content + "\n"; !strings.Contains(got, want) {
			t.Fatalf("delegated request carries no refs content %q:\n%s", want, got)
		}
	})

	t.Run("a ref the run does not hold keeps the delegation usable", func(t *testing.T) {
		provider := &serveModel{name: "model-recall", answer: "the recall turn was admitted"}
		delegation := &stubDelegation{}
		target := newRecallTarget(t, addr, provider, delegation, factTable{})

		out, err := target.Call(callFrameContext(addr), []byte(`{"input":"what did we decide about the migration?","event_keys":["0a01"]}`))
		if err != nil {
			t.Fatalf("call the target tool = %v, want an admission", err)
		}
		if _, ok := out.(task.AdmissionReceipt); !ok {
			t.Fatalf("call result = %#v, want the admission receipt of a call whose ref is missing", out)
		}
		waitCompletions(t, delegation, 1)

		got := requestContent(provider.recorded()[0])
		if want := "refs:\n- 0a01\n" + missingRefLine; !strings.Contains(got, want) {
			t.Fatalf("delegated request carries no %q:\n%s", want, got)
		}
	})
}

// newRecallTarget returns the target tool of one declared agent reference under the
// recall shape: the declared event param travels as the handoff refs, and the facts the
// run holds answer them.
func newRecallTarget(t *testing.T, addr event.Address, provider *serveModel, delegation *stubDelegation, facts factTable) *Target {
	t.Helper()
	bindings := state.NewBindings()
	holdBinding(t, bindings, addr)
	target, err := NewTarget(TargetOptions{
		Name:        "recall",
		Description: "reads the memory this run holds",
		Assembly:    &Assembly{Definition: Definition{Name: "recall", Model: "model-recall"}},
		Builder: Builder{
			Model: func(context.Context, string) (trpcmodel.Model, error) { return provider, nil },
			Facts: facts.read,
		},
		Delegation:  delegation,
		Bindings:    bindings,
		EventParams: []string{"event_keys"},
	})
	if err != nil {
		t.Fatalf("construct the target tool: %v", err)
	}
	return target
}

// callFrameContext returns the context one call of a target tool runs under.
func callFrameContext(addr event.Address) context.Context {
	return task.WithCallFrame(context.Background(), task.CallFrame{
		Identity: task.CallIdentity{
			InstanceID:   "instance-subagent",
			InvocationID: "inv-caller",
			DecisionID:   "decision-caller",
			CallID:       "call-recall",
		},
		Address: addr,
	})
}

// waitCompletions waits for the delegated turns the stand-in delegation reported.
func waitCompletions(t *testing.T, delegation *stubDelegation, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for delegation.completions() < want {
		if time.Now().After(deadline) {
			t.Fatalf("delegated terminals = %d, want %d", delegation.completions(), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// refTicketPreviewRunes is the preview bound the projection records on a ticket
// decision; a test reads it so the expected preview is never a copy that drifts.
const refTicketPreviewRunes = 128

// refSummaryBudget is a token budget that fits a summary but neither the full text
// nor its ticket, so the ladder settles on the summary of one 2000-byte fact.
const refSummaryBudget = 38

// refEnvelope returns the envelope a call declares beside its request, naming the
// refs the test holds.
func refEnvelope(refs ...string) agent.HandoffEnvelope {
	return agent.HandoffEnvelope{
		Intent:           "review the change",
		Constraints:      []string{"every finding names a file"},
		Grants:           []string{"tool:exec_run"},
		PendingDecisions: []string{"whether the change ships"},
		Refs:             refIDs(refs...),
	}
}

// refEnvelopePtr returns the envelope of a turn carrying the refs the test holds.
func refEnvelopePtr(refs ...string) *agent.HandoffEnvelope {
	envelope := refEnvelope(refs...)
	return &envelope
}

// refIDs renders one declared ref list as the identities the envelope carries.
func refIDs(refs ...string) []event.EventID {
	out := make([]event.EventID, 0, len(refs))
	for _, ref := range refs {
		out = append(out, event.EventID(ref))
	}
	return out
}

// fact returns the committed fact one ref names, carrying the given text.
func fact(id event.EventID, content string) event.Event {
	return event.Event{
		Key:     id,
		Type:    event.TypeAgentOutput,
		Message: model.Message{Content: content},
	}
}

// factTable is the committed-fact face of one test: it answers the facts the test
// holds and refuses every other identity, as the run's own read face does.
type factTable map[event.EventID]event.Event

// read answers one committed fact and refuses an identity the test does not hold.
func (f factTable) read(_ context.Context, id event.EventID) (event.Event, error) {
	evt, held := f[id]
	if !held {
		return event.Event{}, fmt.Errorf("tagent: fact %s did not commit", id)
	}
	return evt, nil
}
