// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
package agent

import (
	"errors"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

// TestHandoffEnvelopeContract holds the structured carrier of one delegated call:
// - a complete envelope validates and keeps every field it was built with
// - an envelope naming no intent or no non-empty constraint is refused by name
// - the external-context block renders every field under its own heading in fixed order
// - a list with no entry keeps its heading and states that it carries none
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestHandoffEnvelopeContract(t *testing.T) {
	complete := HandoffEnvelope{
		Intent:           "review the change",
		Constraints:      []string{"every finding names a file", "the verdict is explicit"},
		Grants:           []string{"tool:exec_run", "skill:go-service-ops"},
		PendingDecisions: []string{"whether the change ships"},
		Refs:             []event.EventID{"ev-context-1", "ev-context-2"},
	}

	t.Run("a complete envelope validates", func(t *testing.T) {
		if err := complete.Validate(); err != nil {
			t.Fatalf("validate the complete envelope = %v, want it accepted", err)
		}
	})

	t.Run("an envelope naming no intent is refused", func(t *testing.T) {
		blank := complete
		blank.Intent = "   "
		if err := blank.Validate(); !errors.Is(err, ErrHandoffIntentMissing) {
			t.Fatalf("validate an envelope with no intent = %v, want %v", err, ErrHandoffIntentMissing)
		}
	})

	t.Run("an envelope naming no constraint is refused", func(t *testing.T) {
		blank := complete
		blank.Constraints = nil
		if err := blank.Validate(); !errors.Is(err, ErrHandoffConstraintsMissing) {
			t.Fatalf("validate an envelope with no constraint = %v, want %v", err, ErrHandoffConstraintsMissing)
		}
	})

	t.Run("an envelope whose constraints are all blank is refused", func(t *testing.T) {
		blank := complete
		blank.Constraints = []string{"", "   "}
		if err := blank.Validate(); !errors.Is(err, ErrHandoffConstraintsMissing) {
			t.Fatalf("validate an envelope with blank constraints = %v, want %v", err, ErrHandoffConstraintsMissing)
		}
	})

	t.Run("the external context renders every field under its own heading", func(t *testing.T) {
		block := complete.ExternalContext()
		for _, want := range []string{
			"[handoff envelope]",
			"intent: review the change",
			"constraints:",
			"- every finding names a file",
			"- the verdict is explicit",
			"grants:",
			"- tool:exec_run",
			"- skill:go-service-ops",
			"pending_decisions:",
			"- whether the change ships",
			"refs:",
			"- ev-context-1",
			"- ev-context-2",
		} {
			if !strings.Contains(block, want) {
				t.Fatalf("external context carries no %q:\n%s", want, block)
			}
		}
		order := []string{"intent:", "constraints:", "grants:", "pending_decisions:", "refs:"}
		last := -1
		for _, heading := range order {
			at := strings.Index(block, heading)
			if at <= last {
				t.Fatalf("heading %q sits at %d, want it after %d:\n%s", heading, at, last, block)
			}
			last = at
		}
	})

	t.Run("a list with no entry keeps its heading", func(t *testing.T) {
		sparse := HandoffEnvelope{Intent: "review the change", Constraints: []string{"an explicit verdict"}}
		block := sparse.ExternalContext()
		for _, want := range []string{"grants: (none)", "pending_decisions: (none)", "refs: (none)"} {
			if !strings.Contains(block, want) {
				t.Fatalf("external context carries no %q:\n%s", want, block)
			}
		}
	})
}
