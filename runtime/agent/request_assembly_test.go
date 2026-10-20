package agent

import (
	"errors"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
)

// TestRoleTableIsClosedAndSingleValued pins the role table the replay path renders under.
// - every frozen event type of a native turn names exactly one role, and the settled fact keeps the tool role its result answered with
// - the product entries the replay layer mints render as user input material
// - a type the table does not declare is refused by name, never rendered under a fallback role
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestRoleTableIsClosedAndSingleValued(t *testing.T) {
	t.Run("every declared type names exactly one role", func(t *testing.T) {
		var table = []struct {
			eventType string
			want      model.Role
		}{
			{event.TypeExternalInput, model.RoleUser},
			{event.TypeContextCompress, model.RoleUser},
			{event.TypeFeedback, model.RoleUser},
			{event.TypeGovernance, model.RoleUser},
			{event.TypeThinkingPlan, model.RoleAssistant},
			{event.TypeAgentOutput, model.RoleAssistant},
			{event.TypeActionCommand, model.RoleTool},
			{event.TypeTaskSettled, model.RoleTool},
			{"recovery_hint", model.RoleUser},
			{"segment_fold", model.RoleUser},
			{"history_summary", model.RoleUser},
		}
		for _, entry := range table {
			got, err := RoleOf(entry.eventType)
			if err != nil {
				t.Fatalf("map %q onto a role = %v, want the role %q it is declared under", entry.eventType, err, entry.want)
			}
			if got != entry.want {
				t.Fatalf("role of %q = %q, want %q", entry.eventType, got, entry.want)
			}
		}
	})

	t.Run("a type the table does not declare is refused by name", func(t *testing.T) {
		got, err := RoleOf("undeclared_entry")
		if err == nil {
			t.Fatalf("map an undeclared type = %q, want the type refused by name", got)
		}
		if !errors.Is(err, ErrProjectionTypeUnmapped) {
			t.Fatalf("refusal = %v, want it to carry ErrProjectionTypeUnmapped", err)
		}
		if got != "" {
			t.Fatalf("role beside the refusal = %q, want no role rendered", got)
		}
		if !strings.Contains(err.Error(), `"undeclared_entry"`) {
			t.Fatalf("refusal = %q, want it to name the type it refused", err)
		}
	})

	t.Run("a ready event carrying an undeclared type refuses the aggregation", func(t *testing.T) {
		msgs, err := aggregateReady([]event.Event{{Key: "event-undeclared", Type: "undeclared_entry"}})
		if err == nil {
			t.Fatalf("aggregate an undeclared type = %+v, want the aggregation refused by name", msgs)
		}
		if !errors.Is(err, ErrProjectionTypeUnmapped) {
			t.Fatalf("aggregation refusal = %v, want it to carry ErrProjectionTypeUnmapped", err)
		}
		if msgs != nil {
			t.Fatalf("aggregation = %+v, want no material beside the refusal", msgs)
		}
	})

	t.Run("a ready event carrying its own role is kept as it stands", func(t *testing.T) {
		msgs, err := aggregateReady([]event.Event{{
			Key:     "event-native",
			Type:    "undeclared_entry",
			Message: model.Message{Role: model.RoleAssistant, Content: "native"},
		}})
		if err != nil {
			t.Fatalf("aggregate a native message = %v, want it kept without the table", err)
		}
		if len(msgs) != 1 || msgs[0].Role != model.RoleAssistant || msgs[0].Content != "native" {
			t.Fatalf("aggregation = %+v, want the native message as it stands", msgs)
		}
	})
}
