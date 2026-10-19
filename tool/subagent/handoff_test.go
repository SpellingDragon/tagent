// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
package subagent

import (
	"errors"
	"testing"

	"github.com/SpellingDragon/tagent/runtime/agent"

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
