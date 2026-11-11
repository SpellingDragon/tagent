package agent

import (
	"errors"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory/replay"
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

// TestSettlementRoleMapped pins the one projection registration the settlement family needs.
//
// - The frozen type is spelled here exactly as the family is, and it maps to the user role the settlement line is read under.
// - A type outside the table is refused by name.
// - The same check runs from the assembly side, where the family const is defined, so a drift in either spelling fails there.
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestSettlementRoleMapped(t *testing.T) {
	if projectionTypeDeliverySettlement != "delivery.settlement" {
		t.Fatalf("the local settlement type reads %q, want the frozen family word delivery.settlement", projectionTypeDeliverySettlement)
	}
	role, err := RoleOf(projectionTypeDeliverySettlement)
	if err != nil {
		t.Fatalf("the settlement type maps to no role: %v", err)
	}
	if role != model.RoleUser {
		t.Fatalf("the settlement type maps to role %q, want the user role its one-line state is read under", role)
	}
	if _, err := RoleOf(projectionTypeDeliverySettlement + ".unregistered"); !errors.Is(err, ErrProjectionTypeUnmapped) {
		t.Fatalf("an unregistered type answered %v, want ErrProjectionTypeUnmapped", err)
	}
}

// TestConstantsSingleSource pins the two meters onto one registration each.
//   - The per-message framing is spelled once: the approximate measure reads it at the replay budget's registered packing ratio
//   - a message with no content of its own therefore costs the same framing token count on both meters
//   - The packing ratio is the replay budget's registered constant, read by the fuse rather than restated
//   - one ratio byte length therefore measures the same tokens on both meters
//   - The per-call framing has one home too, and the two meters diverge exactly where the budget meter is documented to be the conservative one
//   - a call with no identifier, name or arguments charges the budget framing and nothing on the approximate side
//   - The two registrations are budget-message-overhead and chars-per-token in golden/runtime/window_bounds.json
func TestConstantsSingleSource(t *testing.T) {
	if messageOverheadBytes != budgetMessageOverheadTokens*replay.BudgetCharsPerToken {
		t.Fatalf("the byte framing reads %d, want the registered token framing %d at the registered ratio %d",
			messageOverheadBytes, budgetMessageOverheadTokens, replay.BudgetCharsPerToken)
	}
	t.Run("one message framing on both meters", func(t *testing.T) {
		message := model.Message{Role: model.RoleUser}
		if got := approxPromptTokens([]model.Message{message}); got != budgetMessageOverheadTokens {
			t.Fatalf("the approximate measure reads %d tokens for the bare framing, want the registered %d", got, budgetMessageOverheadTokens)
		}
		_, _, total := AccountRequestBudget([]model.Message{message}, nil, nil, "")
		if total != budgetMessageOverheadTokens {
			t.Fatalf("the budget meter reads %d tokens for the bare framing, want the registered %d", total, budgetMessageOverheadTokens)
		}
	})
	t.Run("one packing ratio on both meters", func(t *testing.T) {
		message := model.Message{Role: model.RoleUser, Content: strings.Repeat("x", replay.BudgetCharsPerToken)}
		if got, want := approxPromptTokens([]model.Message{message}), budgetMessageOverheadTokens+1; got != want {
			t.Fatalf("the approximate measure reads %d tokens for one ratio of content, want %d", got, want)
		}
		_, _, total := AccountRequestBudget([]model.Message{message}, nil, nil, "")
		if want := budgetMessageOverheadTokens + 1; total != want {
			t.Fatalf("the budget meter reads %d tokens for one ratio of content, want %d", total, want)
		}
	})
	t.Run("per-call framing belongs to the budget meter alone", func(t *testing.T) {
		message := model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{}}}
		if got, want := approxPromptTokens([]model.Message{message}), budgetMessageOverheadTokens; got != want {
			t.Fatalf("the approximate measure reads %d tokens for a bare call, want %d: it charges no per-call framing", got, want)
		}
		_, _, total := AccountRequestBudget([]model.Message{message}, nil, nil, "")
		if want := budgetMessageOverheadTokens + budgetToolCallOverheadTokens; total != want {
			t.Fatalf("the budget meter reads %d tokens for a bare call, want %d", total, want)
		}
	})
}
