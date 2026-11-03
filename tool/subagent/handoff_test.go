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
// - a handoff naming no intent or no acceptance constraint is refused at admission by name
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
		envelope := args.envelope(assembly)
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
		envelope := args.envelope(assembly)
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
		envelope := args.envelope(assembly)
		if len(envelope.Refs) != 2 || string(envelope.Refs[0]) != "ev-context-1" || string(envelope.Refs[1]) != "ev-context-2" {
			t.Fatalf("envelope refs = %v, want the declared fact identities", envelope.Refs)
		}
	})

	t.Run("a call declaring no handoff builds none", func(t *testing.T) {
		envelope := callArgs{}.envelope(assembly)
		if envelope != nil {
			t.Fatalf("envelope = %+v, want nil so the request keeps its shape", envelope)
		}
	})

	t.Run("a handoff naming no intent is refused", func(t *testing.T) {
		args := callArgs{Handoff: &handoffArgs{Constraints: []string{"an explicit verdict"}}}
		envelope := args.envelope(assembly)
		_, err := (delegationFaces{}).admit(context.Background(), Builder{}, task.CallFrame{}, assembly, "", envelope.Refs, envelope)
		if !errors.Is(err, agent.ErrHandoffIntentMissing) {
			t.Fatalf("admit a call with no intent = %v, want %v", err, agent.ErrHandoffIntentMissing)
		}
	})

	t.Run("a handoff naming no acceptance constraint is refused", func(t *testing.T) {
		args := callArgs{Handoff: &handoffArgs{Intent: "review the change"}}
		envelope := args.envelope(assembly)
		_, err := (delegationFaces{}).admit(context.Background(), Builder{}, task.CallFrame{}, assembly, "", envelope.Refs, envelope)
		if !errors.Is(err, agent.ErrHandoffConstraintsMissing) {
			t.Fatalf("admit a call with no constraint = %v, want %v", err, agent.ErrHandoffConstraintsMissing)
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
		facts := factTable{"0a01": fact("0a01", content)}
		delegation := &stubDelegation{}
		target := newRecallTarget(t, addr, provider, delegation, facts)

		out, err := target.Call(callFrameContext(addr), []byte(`{"input":"what did we decide about the migration?","event_keys":["0a01"]}`))
		if err != nil {
			t.Fatalf("call the target tool = %v, want an admission", err)
		}
		if _, ok := out.(task.AdmissionReceipt); !ok {
			t.Fatalf("call result = %#v, want the admission receipt", out)
		}
		waitCompletions(t, delegation, 1)

		// The source materializes as its own message beside the task (the stub
		// stands in for the resident loop), so presence is read across every
		// message the round produced.
		got := requestAllText(provider.recorded()[0])
		if !strings.Contains(got, content) {
			t.Fatalf("delegated request carries no refs content %q:\n%s", content, got)
		}
		if !strings.Contains(got, "what did we decide about the migration?") {
			t.Fatalf("delegated request dropped the natural-language request: %q", got)
		}
	})

	t.Run("a ref the run does not hold keeps the delegation usable", func(t *testing.T) {
		provider := &serveModel{name: "model-recall", answer: "the recall turn was admitted"}
		facts := factTable{}
		delegation := &stubDelegation{}
		target := newRecallTarget(t, addr, provider, delegation, facts)

		out, err := target.Call(callFrameContext(addr), []byte(`{"input":"what did we decide about the migration?","event_keys":["0a01"]}`))
		if err != nil {
			t.Fatalf("call the target tool = %v, want an admission", err)
		}
		if _, ok := out.(task.AdmissionReceipt); !ok {
			t.Fatalf("call result = %#v, want the admission receipt of a call whose ref is missing", out)
		}
		waitCompletions(t, delegation, 1)

		// A reference the run does not hold is noise a hallucination produced:
		// it silently does not materialize (Q2 ruling) — the turn still runs, the
		// bare identity stays in the envelope block, and no missing-marker text
		// is invented for what nothing backs.
		got := requestAllText(provider.recorded()[0])
		if strings.Contains(got, "  (missing)\n") {
			t.Fatalf("delegated request carries a missing-marker %q for an unbacked reference: %q", "  (missing)\n", got)
		}
		if strings.Contains(got, "0a01") {
			t.Fatalf("delegated request carries the bare identity of an unbacked reference: %q", got)
		}
		if !strings.Contains(got, "what did we decide about the migration?") {
			t.Fatalf("delegated request dropped the task beside an unbacked reference: %q", got)
		}
	})
}

// requestAllText renders every message of one request, so a presence check
// reads material that arrives as its own message beside the task.
func requestAllText(req trpcmodel.Request) string {
	parts := make([]string, 0, len(req.Messages))
	for _, message := range req.Messages {
		parts = append(parts, message.Content)
	}
	return strings.Join(parts, "\n")
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
