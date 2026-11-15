package replay

import (
	"context"
	"fmt"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

// oldBandTurns lays out turns that have already been through one fold: each
// closed segment carries the product its process detail folded into, so every
// segment outside the recent window is the oldest band and the next fold states
// all of them in one history summary.
func oldBandTurns(turns int) []ProjectionEntry {
	out := make([]ProjectionEntry, 0, turns*3)
	for step := 0; step < turns; step++ {
		turn := ageTurn(fmt.Sprintf("o%d", step), step, 2)
		out = append(out, turn[0])
		out = append(out, ProjectionEntry{
			Key:     event.EventID(fmt.Sprintf("-seg-old-%02d", step)),
			Type:    ProjectionTypeSegmentFold,
			Summary: "segment fold",
			Content: provenancePrefixOf(turn[1]) + turn[1].Type + ": " + turn[1].Summary,
			Time:    turn[1].Time,
		})
		out = append(out, turn[len(turn)-1])
	}
	return out
}

// historySummaryUnder folds one fixed old-band sequence under one plan and
// answers the history summary the fold minted beside the projection it left.
func historySummaryUnder(t *testing.T, plan AgeFoldPlan) (ProjectionEntry, *ReplayProjection) {
	t.Helper()
	projection := NewReplayProjection()
	ctx := context.Background()
	for _, entry := range oldBandTurns(20) {
		projection.Append(ctx, entry)
	}
	foldOnce(t, projection, plan)
	for _, entry := range projection.Entries() {
		if entry.Type == ProjectionTypeHistorySummary {
			return entry, projection
		}
	}
	t.Fatal("the fold minted no history summary over the old-band sequence")
	return ProjectionEntry{}, nil
}

// TestHistorySummaryObeysThePlanBudget pins the summary text bound the fold reads
// off its plan: the plan's SummaryBudgetBytes bounds the mechanical summary, the
// zero plan reads the policy default, and folding the standing summary again
// answers the same text instead of growing past the bound.
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestHistorySummaryObeysThePlanBudget(t *testing.T) {
	const declared = 512
	tight, projection := historySummaryUnder(t, AgeFoldPlan{
		KeepRecent:         DefaultAgeKeepRecent,
		VolumeThreshold:    600,
		SummaryBudgetBytes: declared,
	})
	if len(tight.Content) > declared {
		t.Fatalf("summary carries %d bytes, want at most the declared %d: the bound comes from the plan", len(tight.Content), declared)
	}
	if tight.Content == "" {
		t.Fatal("summary carries no text, want the folded reads of the oldest band")
	}

	loose, _ := historySummaryUnder(t, AgeFoldPlan{KeepRecent: DefaultAgeKeepRecent, VolumeThreshold: 600})
	if len(loose.Content) <= declared {
		t.Fatalf("default summary carries %d bytes, want more than the declared %d: the two plans must not fold under one bound", len(loose.Content), declared)
	}
	if len(loose.Content) > DefaultAgeSummaryTextLimit {
		t.Fatalf("default summary carries %d bytes, want at most the policy default %d", len(loose.Content), DefaultAgeSummaryTextLimit)
	}

	foldOnce(t, projection, AgeFoldPlan{
		KeepRecent:         DefaultAgeKeepRecent,
		VolumeThreshold:    1,
		SummaryBudgetBytes: declared,
	})
	again := ProjectionEntry{}
	for _, entry := range projection.Entries() {
		if entry.Type == ProjectionTypeHistorySummary {
			again = entry
		}
	}
	if again.Key != tight.Key || again.Content != tight.Content {
		t.Fatalf("a second fold answered %q where the first answered %q, want the same bounded text: the fold stays idempotent under the declared budget", again.Content, tight.Content)
	}
}
