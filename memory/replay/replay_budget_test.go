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
		if len(first) != 1 || !strings.Contains(first[0].Content, "[evt_fact-1|") {
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
		if !strings.HasPrefix(card, "[notice ") || !strings.HasSuffix(card, TicketOf(key, event.TypeFeedback)) {
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
		if !strings.HasSuffix(card, TicketOf(key, event.TypeFeedback)) {
			t.Fatalf("the notice card = %q, want it to end with the whole ticket %q", card, TicketOf(key, event.TypeFeedback))
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
		if !strings.HasPrefix(TicketOf(key, event.TypeFeedback), card) {
			t.Fatalf("the tight notice card = %q, want the ticket the room can carry rather than a card built past its bound", card)
		}
	})
}

// TestApplyProductsDropsTheMaterialOfTheCoveredEntries pins the same cleanup on the production fold face: the products a consumer applies under PlanByAge.
//   - every covered process-detail key leaves the kept originals, the dedup keys and the consumed marks
//   - the entries no product covers keep their originals, so a later assembly still presents them whole
//   - every key the three maps speak for is a key the entries carry, and applying the same products twice drops nothing more
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestApplyProductsDropsTheMaterialOfTheCoveredEntries(t *testing.T) {
	ctx := context.Background()
	plan := AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 1}
	entries := append(ageTurn("t1", 0, 2), ageTurn("t2", 1, 2)...)
	projection := ageProjection(t, entries)
	report, err := projection.PlanByAge(ctx, plan)
	if err != nil {
		t.Fatalf("PlanByAge: %v", err)
	}
	if len(report.Products) == 0 {
		t.Fatal("the plan minted no product, want the material folded under the threshold")
	}
	projection.ApplyProducts(report.Products)

	covered := make(map[event.EventID]struct{}, len(entries))
	for _, product := range report.Products {
		for _, key := range product.Covered {
			covered[key] = struct{}{}
		}
	}
	if len(covered) == 0 {
		t.Fatal("the applied products cover no key, want the folded entries named")
	}
	for key := range covered {
		if original, kept := projection.Original(key); kept {
			t.Fatalf("the covered key %q still answers the original %q", key, original)
		}
		projection.mu.Lock()
		_, seen := projection.seen[key]
		_, consumed := projection.consumed[key]
		projection.mu.Unlock()
		if seen || consumed {
			t.Fatalf("the covered key %q still speaks for material the projection dropped (seen=%v consumed=%v)", key, seen, consumed)
		}
	}
	for _, kept := range []struct {
		key   event.EventID
		opens string
	}{{"t2-in", "用户输入 t2"}, {"t2-out", "回答 t2"}} {
		original, holds := projection.Original(kept.key)
		if !holds || !strings.HasPrefix(original, kept.opens) {
			t.Fatalf("kept key %q original = %q kept=%v, want the text of the entry the projection still carries", kept.key, original, holds)
		}
	}
	assertMapsSpeakForEntries(t, projection)

	held := RenderProjection(projection.Entries())
	projection.ApplyProducts(report.Products)
	if string(RenderProjection(projection.Entries())) != string(held) {
		t.Fatal("applying the same products twice moved the projection")
	}
	assertMapsSpeakForEntries(t, projection)
}

// TestFoldByAgeDropsTheMaterialOfTheCoveredEntries pins the cleanup on the direct age-band fold.
//   - the process detail a segment product consumes leaves the three maps with the entries
//   - the input and output entries the fold keeps still answer their original text
//   - every key the three maps speak for is a key the entries carry
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestFoldByAgeDropsTheMaterialOfTheCoveredEntries(t *testing.T) {
	ctx := context.Background()
	entries := append(ageTurn("t1", 0, 2), ageTurn("t2", 1, 2)...)
	projection := ageProjection(t, entries)
	before := make(map[event.EventID]struct{}, len(entries))
	for _, entry := range entries {
		before[entry.Key] = struct{}{}
	}
	report, err := projection.FoldByAge(ctx, AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 1})
	if err != nil {
		t.Fatalf("FoldByAge: %v", err)
	}
	if !report.Triggered || len(report.Products) == 0 {
		t.Fatalf("report = %+v, want the material folded", report)
	}

	carried := make(map[event.EventID]struct{}, projection.Len())
	for _, entry := range projection.Entries() {
		carried[entry.Key] = struct{}{}
	}
	for key := range before {
		if _, still := carried[key]; still {
			continue
		}
		if original, kept := projection.Original(key); kept {
			t.Fatalf("the folded key %q still answers the original %q", key, original)
		}
		projection.mu.Lock()
		_, seen := projection.seen[key]
		_, consumed := projection.consumed[key]
		projection.mu.Unlock()
		if seen || consumed {
			t.Fatalf("the folded key %q still speaks for material the projection dropped (seen=%v consumed=%v)", key, seen, consumed)
		}
	}
	for _, kept := range []event.EventID{"t2-in", "t2-out"} {
		if original, holds := projection.Original(kept); !holds || original == "" {
			t.Fatalf("kept key %q original = %q kept=%v, want the text of the entry the projection still carries", kept, original, holds)
		}
	}
	assertMapsSpeakForEntries(t, projection)
}

// assertMapsSpeakForEntries pins the invariant every rewrite leaves behind: the kept originals, the dedup keys and the consumed marks name keys of entries the projection carries, and nothing else.
func assertMapsSpeakForEntries(t *testing.T, projection *ReplayProjection) {
	t.Helper()
	carried := make(map[event.EventID]struct{}, projection.Len())
	for _, entry := range projection.Entries() {
		carried[entry.Key] = struct{}{}
	}
	projection.mu.Lock()
	defer projection.mu.Unlock()
	for key := range projection.originals {
		if _, holds := carried[key]; !holds {
			t.Fatalf("the kept originals still answer the key %q, want a key of an entry the projection carries", key)
		}
	}
	for key := range projection.seen {
		if _, holds := carried[key]; !holds {
			t.Fatalf("the dedup keys still hold %q, want a key of an entry the projection carries", key)
		}
	}
	for key := range projection.consumed {
		if _, holds := carried[key]; !holds {
			t.Fatalf("the consumed marks still hold %q, want a key of an entry the projection carries", key)
		}
	}
}

// TestTicketTextIsTheRecallTicketOfTheEntry pins the one ticket form.
//   - the ticket of a fact key is [evt_<key>|<type>], the form the recall surface parses
//   - the empty key and the synthetic key of a fold product answer no ticket: the address would answer missing
//   - the presented view the budget degrades to a shortened entry ends with the same ticket the fold writes
//
// 契约: docs/GLOSSARY.md
func TestTicketTextIsTheRecallTicketOfTheEntry(t *testing.T) {
	if got, want := TicketOf("02-1-plan", event.TypeActionCommand), "[evt_02-1-plan|action_command]"; got != want {
		t.Fatalf("TicketOf = %q, want %q: one form, written by one producer", got, want)
	}
	for _, key := range []event.EventID{"", "-seg-abc123", "-hist-abc123", "-recovery"} {
		if got := TicketOf(key, event.TypeActionCommand); got != "" {
			t.Fatalf("TicketOf(%q) = %q, want no ticket: a key naming no committed fact is an address that answers missing", key, got)
		}
	}

	entry := ProjectionEntry{Key: "e-9", Type: event.TypeThinkingPlan, Summary: "plan", Content: strings.Repeat("过程细节。", 40), Time: ageBase}
	view, ok := presentWithin(entry, entry.Content, 500)
	if !ok {
		t.Fatal("the budget presented nothing for an entry it had room to shorten")
	}
	if !strings.HasSuffix(view.Content, "\n"+TicketOf(entry.Key, entry.Type)) {
		t.Fatalf("the shortened view = %q, want it to end with the ticket the fold writes for the same entry", view.Content)
	}
}
