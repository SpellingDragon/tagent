package replay

import (
	"context"
	"errors"
	"reflect"
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

// TestPresentationRecomputedOnBudgetShrink pins that a presentation belongs to the assembly that carries it, never to an earlier one.
//   - a 200-byte entry a roomy assembly carries whole is not carried whole under a 4-byte budget
//   - widening after a shrink presents the whole text again, so a shortened view never outlives the budget that shaped it
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestPresentationRecomputedOnBudgetShrink(t *testing.T) {
	ctx := context.Background()
	original := strings.Repeat("x", 200)

	t.Run("a shrink presents below the roomy view", func(t *testing.T) {
		projection := NewReplayProjection()
		projection.Append(ctx, ProjectionEntry{Key: "shrink-1", Type: event.TypeAgentOutput, Summary: "s", Content: original, Time: budgetNow})

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
	})

	t.Run("a widening after a shrink presents the whole text again", func(t *testing.T) {
		projection := NewReplayProjection()
		projection.Append(ctx, ProjectionEntry{Key: "shrink-2", Type: event.TypeAgentOutput, Summary: "s", Content: original, Time: budgetNow})

		tight, err := projection.AssembleBudget(ctx, 1)
		if err != nil {
			t.Fatalf("AssembleBudget under the 4-byte budget: %v", err)
		}
		if len(tight) != 1 || tight[0].Content == original {
			t.Fatalf("assembly under the 4-byte budget = %+v, want the 200-byte content shortened", tight)
		}

		widened, err := projection.AssembleBudget(ctx, 50)
		if err != nil {
			t.Fatalf("AssembleBudget under the widened budget: %v", err)
		}
		if len(widened) != 1 || widened[0].Content != original {
			t.Fatalf("widened assembly = %q, want the whole text: a presentation is computed for the assembly that carries it", widened[0].Content)
		}
	})
}

// TestNoRecordedPresentationReuse pins that the projection keeps no presentation decision: every assembly computes its own view.
//   - two assemblies of one entry under different budgets present differently, each shaped by its own budget
//   - the projection holds no decision or settle state and exposes no way to record or read a decision
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestNoRecordedPresentationReuse(t *testing.T) {
	ctx := context.Background()
	original := strings.Repeat("y", 200)
	projection := NewReplayProjection()
	projection.Append(ctx, ProjectionEntry{Key: "reuse-1", Type: event.TypeAgentOutput, Summary: "s", Content: original, Time: budgetNow})

	tight, err := projection.AssembleBudget(ctx, 1)
	if err != nil {
		t.Fatalf("AssembleBudget under the 4-byte budget: %v", err)
	}
	roomy, err := projection.AssembleBudget(ctx, 60)
	if err != nil {
		t.Fatalf("AssembleBudget under the 240-byte budget: %v", err)
	}
	if len(tight) != 1 || len(roomy) != 1 {
		t.Fatalf("assemblies under 4 and 240 bytes = (%d, %d) entries, want one each", len(tight), len(roomy))
	}
	if roomy[0].Content == tight[0].Content {
		t.Fatalf("both assemblies presented %q, want each shaped by the budget it was assembled under", tight[0].Content)
	}
	if roomy[0].Content != original {
		t.Fatalf("the assembly under 240 bytes = %q, want the whole text rather than the view an earlier assembly settled on", roomy[0].Content)
	}

	shape := reflect.TypeOf(ReplayProjection{})
	for i := 0; i < shape.NumField(); i++ {
		switch name := shape.Field(i).Name; name {
		case "decisions", "settled", "settles":
			t.Fatalf("the projection keeps the presentation state %q, want every presentation computed per assembly", name)
		}
	}
	face := reflect.TypeOf(&ReplayProjection{})
	for _, name := range []string{"RecordDecision", "Decision", "Settles"} {
		if _, held := face.MethodByName(name); held {
			t.Fatalf("the projection answers %s, want no way to record or follow a presentation decision", name)
		}
	}
}

// TestNoticeCardKeepsItsBound pins the notice card's floor: the card stays inside its bound and keeps the ticket.
//   - a card with room for its status states the status and the ticket, inside the room it was given
//   - a room with no space left for the status still presents the whole ticket, so the identity survives
//   - a room too tight even for the ticket cuts the ticket itself, never a card built past its bound
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestNoticeCardKeepsItsBound(t *testing.T) {
	ctx := context.Background()
	notice := func(key event.EventID) ProjectionEntry {
		return ProjectionEntry{
			Key:     key,
			Type:    event.TypeFeedback,
			Summary: strings.Repeat("状", 200),
			Content: strings.Repeat("n", 4000),
			Time:    budgetNow,
		}
	}

	t.Run("a card with room for its status states the status and the ticket", func(t *testing.T) {
		key := event.EventID("notice-" + strings.Repeat("k", 150))
		projection := NewReplayProjection()
		projection.Append(ctx, notice(key))

		tight, err := projection.AssembleBudget(ctx, 60)
		if err != nil {
			t.Fatalf("AssembleBudget: %v", err)
		}
		if len(tight) != 1 {
			t.Fatalf("assembly = %+v, want the notice presented rather than dropped", tight)
		}
		card := tight[0].Content
		if total := contentBytes(tight); total > 240 {
			t.Fatalf("the request carried %d bytes under a 240-byte budget: %q", total, card)
		}
		if !strings.HasPrefix(card, "[notice ") || !strings.HasSuffix(card, ticketText(key)) {
			t.Fatalf("notice card = %q, want a status frame and the ticket that reaches the original", card)
		}
	})

	t.Run("a room with no space for the status still presents the whole ticket", func(t *testing.T) {
		key := event.EventID("notice-" + strings.Repeat("k", 250))
		projection := NewReplayProjection()
		projection.Append(ctx, notice(key))

		roomy, err := projection.AssembleBudget(ctx, 100)
		if err != nil {
			t.Fatalf("AssembleBudget: %v", err)
		}
		if len(roomy) != 1 {
			t.Fatalf("assembly = %+v, want the notice presented rather than dropped", roomy)
		}
		card := roomy[0].Content
		if len(card) > noticeCardChars {
			t.Fatalf("the notice card carried %d bytes, want at most %d: the ticket is reserved before the status and the head", len(card), noticeCardChars)
		}
		if !strings.HasSuffix(card, ticketText(key)) {
			t.Fatalf("the notice card = %q, want it to end with the whole ticket %q", card, ticketText(key))
		}
	})

	t.Run("a room too tight for the ticket cuts the ticket itself", func(t *testing.T) {
		key := event.EventID("notice-" + strings.Repeat("k", 250))
		projection := NewReplayProjection()
		projection.Append(ctx, notice(key))

		tight, err := projection.AssembleBudget(ctx, 60)
		if err != nil {
			t.Fatalf("AssembleBudget: %v", err)
		}
		if len(tight) != 1 {
			t.Fatalf("assembly = %+v, want the notice presented rather than dropped", tight)
		}
		card := tight[0].Content
		if total := contentBytes(tight); total > 240 {
			t.Fatalf("the request carried %d bytes under a 240-byte budget: %q", total, card)
		}
		if !strings.HasPrefix(ticketText(key), card) {
			t.Fatalf("the tight notice card = %q, want the ticket the room can carry rather than a card built past its bound", card)
		}
	})
}
