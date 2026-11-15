// 契约: docs/wiki/agent/agent-architecture.md#context-management
//
// The ticket injection of the request presentation: each projected entry becomes
// the model message it was assembled into plus, in front of it, the ticket of the
// fact it was minted from — once, and never for an address that answers missing.
package assembly

import (
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/replay"
)

// TestPresentedEntriesCarryTheirEventKeyTicket pins the injection of the fact address.
//   - an entry carrying a committed fact key presents its material behind [evt_<key>|<type>]
//   - the key and the type are the entry's own, so the ticket addresses the fact the material came from
//   - the material behind the ticket is the presentation the entry already had, unchanged
//   - an entry presenting no material states no ticket: the address is owed to material the request carries
func TestPresentedEntriesCarryTheirEventKeyTicket(t *testing.T) {
	input := replay.ProjectionEntry{Key: "e-1", Type: event.TypeExternalInput, Content: "the committed input"}
	if got, want := presentedProjectionContent(input), "[evt_e-1|external_input] the committed input"; got != want {
		t.Fatalf("the presented input = %q, want %q: the material travels with the address of the fact it came from", got, want)
	}
	command := replay.ProjectionEntry{Key: "e-2-call", Type: event.TypeActionCommand, Content: "ok"}
	if got, want := presentedProjectionContent(command), "[evt_e-2-call|action_command] ok"; got != want {
		t.Fatalf("the presented call = %q, want %q: the frozen type of the entry, not a type of the presentation's own", got, want)
	}
	empty := replay.ProjectionEntry{Key: "e-3", Type: event.TypeAgentOutput, Content: ""}
	if got := presentedProjectionContent(empty); got != "" {
		t.Fatalf("the presented empty entry = %q, want the empty presentation: a ticket alone would promise a read of material the model never saw", got)
	}
	hint := replay.ProjectionEntry{Key: "-recovery", Type: replay.ProjectionTypeRecoveryHint, Content: "recovery: replayed 3 entries"}
	if got := presentedProjectionContent(hint); got != hint.Content {
		t.Fatalf("the recovery hint = %q, want it as the assembly wrote it %q: the hint names no fact", got, hint.Content)
	}
}

// TestTheKeyInjectionAddsNoSecondTicket pins the injection's idempotence.
//   - content already opening with a ticket is presented as it stands
//   - no second ticket is appended, so a ticket the model imitated and fed back is never doubled
func TestTheKeyInjectionAddsNoSecondTicket(t *testing.T) {
	kept := replay.ProjectionEntry{Key: "e-4", Type: event.TypeExternalInput, Content: "[evt_e-4|external_input] the imitated line"}
	if got := presentedProjectionContent(kept); got != kept.Content {
		t.Fatalf("the entry opening with a ticket = %q, want it unchanged %q", got, kept.Content)
	}
	if got := strings.Count(presentedProjectionContent(kept), "[evt_"); got != 1 {
		t.Fatalf("the presented entry carries %d ticket(s), want the one it already had", got)
	}
}

// TestAProductEntryGetsNoSyntheticTicket pins what the injection leaves alone.
//   - a segment fold and a history summary carry a synthetic key, so neither is given a ticket
//   - the product text presents itself: its lines name the facts it folded, or it is the summary
//   - no [evt_-seg-…] or [evt_-hist-…] address appears, so the model is never handed a ticket that answers missing
func TestAProductEntryGetsNoSyntheticTicket(t *testing.T) {
	fold := replay.ProjectionEntry{
		Key:     "-seg-abc123",
		Type:    replay.ProjectionTypeSegmentFold,
		Summary: "segment fold",
		Content: "[10-01 10:00|gen:2] [evt_e-1-plan|thinking_plan] thinking_plan: plan",
	}
	if got := presentedProjectionContent(fold); got != fold.Content {
		t.Fatalf("the presented segment fold = %q, want its own lines %q", got, fold.Content)
	}
	summary := replay.ProjectionEntry{Key: "-hist-abc123", Type: replay.ProjectionTypeHistorySummary, Summary: "the store", Content: "the earlier turns"}
	if got, want := presentedProjectionContent(summary), "the store: the earlier turns"; got != want {
		t.Fatalf("the presented history summary = %q, want the summary read %q", got, want)
	}
	for _, product := range []replay.ProjectionEntry{fold, summary} {
		presented := presentedProjectionContent(product)
		for _, form := range []string{"[evt_-seg-", "[evt_-hist-"} {
			if strings.Contains(presented, form) {
				t.Fatalf("the presented product %s carries the synthetic-key ticket %q: that address answers missing", product.Key, form)
			}
		}
	}
}
