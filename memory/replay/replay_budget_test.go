package replay

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// budgetNow is the entry time the budget invariants judge the projection under.
var budgetNow = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

// TestRequestBudgetEnforced pins the request budget: a recorded decision is reused only while its fact and its room hold.
//   - a shrink recomputes the presentation: a 200-byte content is not carried whole under a 4-byte budget, and the roomy budget carries it whole again
//   - a fact changed under a recorded decision recomputes the presentation over the changed fact instead of replaying a stale preview
//   - the whole assembled request stays under the budget, the notice card and the recovery hint included
//   - a non-positive budget is refused by name
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestRequestBudgetEnforced(t *testing.T) {
	ctx := context.Background()

	t.Run("a shrink recomputes the presentation instead of replaying the decision", func(t *testing.T) {
		original := strings.Repeat("x", 200)
		projection := NewReplayProjection()
		projection.Append(ctx, ProjectionEntry{Key: "budget-1", Type: event.TypeAgentOutput, Summary: "s", Content: original, Time: budgetNow})

		roomy, err := projection.AssembleBudget(ctx, 50)
		if err != nil {
			t.Fatalf("AssembleBudget under the roomy budget: %v", err)
		}
		if len(roomy) != 1 || roomy[0].Content != original {
			t.Fatalf("roomy assembly = %+v, want the 200-byte content carried whole", roomy)
		}

		shrunk, err := projection.AssembleBudget(ctx, 1)
		if err != nil {
			t.Fatalf("AssembleBudget under the 4-byte budget: %v", err)
		}
		if total := contentBytes(shrunk); total > 4 {
			t.Fatalf("the request carried %d bytes under a 4-byte budget: %q", total, shrunk)
		}
		for _, entry := range shrunk {
			if entry.Content == original {
				t.Fatalf("the request still carried the 200-byte content whole under the 4-byte budget: %q", entry.Content)
			}
		}

		restored, err := projection.AssembleBudget(ctx, 50)
		if err != nil {
			t.Fatalf("AssembleBudget under the restored budget: %v", err)
		}
		if len(restored) != 1 || restored[0].Content != original {
			t.Fatalf("restored assembly = %+v, want the recorded presentation reused while the fact and the budget hold", restored)
		}
	})

	t.Run("a changed fact recomputes the presentation over the changed fact", func(t *testing.T) {
		original := strings.Repeat("y", 1800)
		projection := NewReplayProjection()
		projection.Append(ctx, ProjectionEntry{Key: "fact-1", Type: event.TypeAgentOutput, Summary: "big result", Content: original, Time: budgetNow})

		first, err := projection.AssembleBudget(ctx, 60)
		if err != nil {
			t.Fatalf("AssembleBudget: %v", err)
		}
		if len(first) != 1 || !strings.Contains(first[0].Content, "[ticket fact-1:") {
			t.Fatalf("first assembly = %+v, want the ticket settlement the budget affords", first)
		}
		stale := first[0].Content

		projection.mu.Lock()
		projection.originals["fact-1"] = strings.Repeat("z", 1800)
		projection.mu.Unlock()

		second, err := projection.AssembleBudget(ctx, 60)
		if err != nil {
			t.Fatalf("AssembleBudget after the fact changed: %v", err)
		}
		if len(second) != 1 {
			t.Fatalf("second assembly = %+v, want the changed fact presented", second)
		}
		if second[0].Content == stale || !strings.Contains(second[0].Content, strings.Repeat("z", 8)) {
			t.Fatalf("second assembly = %q, want a presentation recomputed over the changed fact, not the stale preview", second[0].Content)
		}
	})

	t.Run("the whole request stays under the budget", func(t *testing.T) {
		projection := NewReplayProjection()
		projection.Append(ctx, ProjectionEntry{Key: "notice-1", Type: event.TypeFeedback, Summary: strings.Repeat("s", 200), Content: strings.Repeat("n", 1800), Time: budgetNow})

		under, err := projection.AssembleBudget(ctx, 20)
		if err != nil {
			t.Fatalf("AssembleBudget: %v", err)
		}
		if len(under) == 0 {
			t.Fatal("the notice assembly presented nothing, want the notice card under the budget")
		}
		if total := contentBytes(under); total > 80 {
			t.Fatalf("the request carried %d bytes under an 80-byte budget: %q", total, under)
		}

		hinted := NewReplayProjection()
		hinted.noteRecovery(2, false)
		hinted.Append(ctx, ProjectionEntry{Key: "hint-1", Type: event.TypeAgentOutput, Summary: "s", Content: strings.Repeat("h", 1800), Time: budgetNow})

		withHint, err := hinted.AssembleBudget(ctx, 10)
		if err != nil {
			t.Fatalf("AssembleBudget with the recovery hint: %v", err)
		}
		if len(withHint) == 0 {
			t.Fatal("the hinted assembly presented nothing, want the recovery hint under the budget")
		}
		if total := contentBytes(withHint); total > 40 {
			t.Fatalf("the request carried %d bytes under a 40-byte budget: %q", total, withHint)
		}
	})

	t.Run("a non-positive budget is refused by name", func(t *testing.T) {
		projection := NewReplayProjection()
		if _, err := projection.AssembleBudget(ctx, 0); !errors.Is(err, ErrBudgetInvalid) {
			t.Fatalf("a zero budget answered %v, want the named refusal %v", err, ErrBudgetInvalid)
		}
	})
}

// contentBytes counts the content bytes the assembled request carries.
func contentBytes(entries []ProjectionEntry) int {
	total := 0
	for _, entry := range entries {
		total += len(entry.Content)
	}
	return total
}
