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

// TestFoldDropsTheMaterialOfTheEntriesItFolded pins the cleanup the compaction fold owes the projection's keyed maps.
//   - a key the summary alone represents keeps neither its kept original, its dedup key nor its consumed mark
//   - the retained ref keeps its kept original and is appended once, so the rewrite loses no material it still carries
//   - every key the three maps speak for is a key the entries carry
//   - the rewritten sequence presents the summary and the retained ref, and nothing of the entries it folded
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestFoldDropsTheMaterialOfTheEntriesItFolded(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	projection := NewReplayProjection()
	for _, entry := range []ProjectionEntry{
		{Key: "c-in", Type: event.TypeExternalInput, Summary: "in", Content: "输入的原文正文", Time: base},
		{Key: "c-plan", Type: event.TypeThinkingPlan, Summary: "plan", Content: "计划的原文正文", Time: base.Add(time.Minute)},
		{Key: "c-cmd", Type: event.TypeActionCommand, Summary: "call", Content: "工具结果的原文正文", Time: base.Add(2 * time.Minute)},
		{Key: "c-out", Type: event.TypeAgentOutput, Summary: "out", Content: "回答的原文正文", Time: base.Add(3 * time.Minute)},
		{Key: "c-note", Type: event.TypeFeedback, Summary: "note", Content: "通知的原文正文", Time: base.Add(4 * time.Minute)},
	} {
		projection.Append(ctx, entry)
	}
	if _, err := projection.Fold(ctx); err != nil {
		t.Fatalf("Fold: %v", err)
	}

	for _, key := range []event.EventID{"c-in", "c-plan", "c-out", "c-note"} {
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
	if consumed := projection.ConsumedKeys(); len(consumed) != 0 {
		t.Fatalf("consumed keys = %v, want the consumed marks of the folded plans dropped with them", consumed)
	}

	if original, kept := projection.Original("c-cmd"); !kept || original != "工具结果的原文正文" {
		t.Fatalf("retained ref c-cmd original = %q kept=%v, want its own text kept", original, kept)
	}
	held := projection.Len()
	projection.Append(ctx, ProjectionEntry{Key: "c-cmd", Type: event.TypeActionCommand, Summary: "call", Content: "工具结果的原文正文", Time: base.Add(2 * time.Minute)})
	if projection.Len() != held {
		t.Fatalf("a second append of the retained ref moved the projection: %d -> %d", held, projection.Len())
	}

	assertMapsSpeakForEntries(t, projection)

	assembled, err := projection.AssembleBudget(ctx, 4096)
	if err != nil {
		t.Fatalf("AssembleBudget: %v", err)
	}
	var presented strings.Builder
	for _, entry := range assembled {
		presented.WriteString(entry.Content)
		presented.WriteString("\n")
	}
	if !strings.Contains(presented.String(), "工具结果的原文正文") {
		t.Fatalf("assembly = %q, want the retained ref presented from the entry it still carries", presented.String())
	}
	if !strings.Contains(presented.String(), "compaction: in; plan; call; out; note") {
		t.Fatalf("assembly = %q, want the compaction summary presented first", presented.String())
	}
	for _, folded := range []string{"输入的原文正文", "计划的原文正文", "回答的原文正文", "通知的原文正文"} {
		if strings.Contains(presented.String(), folded) {
			t.Fatalf("assembly still carries %q, want nothing of the entries the summary speaks for", folded)
		}
	}
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
