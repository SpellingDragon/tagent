package replay

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

// capacityTurn lays one closed turn out on a projection: the input that opens
// it, the process detail of the turn and the output that closes it. payload
// shapes the non-foldable material and process shapes the candidate material.
func capacityTurn(seed string, payload, process int) []ProjectionEntry {
	entries := []ProjectionEntry{{
		Key:     event.EventID(seed + "-in"),
		Type:    event.TypeExternalInput,
		Summary: "input " + seed,
		Content: strings.Repeat("input "+seed+" ", payload),
	}}
	for i := 0; i < process; i++ {
		suffix := string(rune('a' + i))
		entries = append(entries,
			ProjectionEntry{
				Key:     event.EventID(seed + "-plan-" + suffix),
				Type:    event.TypeThinkingPlan,
				Summary: "plan " + seed + suffix,
				Content: "small plan " + seed + suffix,
			},
			ProjectionEntry{
				Key:     event.EventID(seed + "-cmd-" + suffix),
				Type:    event.TypeActionCommand,
				Summary: "call " + seed + suffix,
				Content: "small call " + seed + suffix,
			},
		)
	}
	return append(entries, ProjectionEntry{
		Key:     event.EventID(seed + "-out"),
		Type:    event.TypeAgentOutput,
		Summary: "output " + seed,
		Content: strings.Repeat("output "+seed+" ", payload),
	})
}

// capacityProjection returns a projection over the given entries, in the order
// given.
func capacityProjection(t *testing.T, entries []ProjectionEntry) *ReplayProjection {
	t.Helper()
	projection := NewReplayProjection()
	for _, entry := range entries {
		projection.Append(context.Background(), entry)
	}
	return projection
}

// capacityState is what a projection presents through its public read face:
// the rendered bytes, the entries and the kept originals.
type capacityState struct {
	rendered  string
	entries   []ProjectionEntry
	originals map[event.EventID]string
}

// capacityStateOf reads one projection through its public read face.
func capacityStateOf(projection *ReplayProjection) capacityState {
	entries := projection.Entries()
	originals := make(map[event.EventID]string, len(entries))
	for _, entry := range entries {
		if original, held := projection.Original(entry.Key); held {
			originals[entry.Key] = original
		}
	}
	return capacityState{
		rendered:  string(RenderProjection(entries)),
		entries:   entries,
		originals: originals,
	}
}

// assertUnchanged reports whether one operation rewrote any public observable
// of the projection.
func (s capacityState) assertUnchanged(t *testing.T, projection *ReplayProjection, when string) {
	t.Helper()
	now := capacityStateOf(projection)
	if now.rendered != s.rendered {
		t.Fatalf("%s rewrote the rendered projection: before %q after %q", when, s.rendered, now.rendered)
	}
	if !reflect.DeepEqual(now.entries, s.entries) {
		t.Fatalf("%s rewrote the entries: before %+v after %+v", when, s.entries, now.entries)
	}
	if !reflect.DeepEqual(now.originals, s.originals) {
		t.Fatalf("%s rewrote the kept originals: before %+v after %+v", when, s.originals, now.originals)
	}
}

// TestFoldCapacityAndCandidateRules pins the two gates of an age fold: the rendered volume and a candidate.
//
// 契约: docs/wiki/agent/compression-and-telemetry.md#projection-fold
func TestFoldCapacityAndCandidateRules(t *testing.T) {
	ctx := context.Background()

	t.Run("inside capacity leaves the projection untouched", func(t *testing.T) {
		entries := append(capacityTurn("inside-1", 32, 1), capacityTurn("inside-2", 32, 1)...)
		plan := AgeFoldPlan{KeepRecent: 2, VolumeThreshold: len(RenderProjection(entries))}
		projection := capacityProjection(t, entries)
		before := capacityStateOf(projection)

		report, err := projection.PlanByAge(ctx, plan)
		if err != nil {
			t.Fatalf("PlanByAge inside capacity: %v", err)
		}
		if report.Triggered || len(report.Products) != 0 {
			t.Fatalf("plan = %+v, want nothing to fold inside capacity", report)
		}
		if report.Volume > report.Threshold {
			t.Fatalf("report volume = %d (threshold %d), want the judgment inside capacity", report.Volume, report.Threshold)
		}
		before.assertUnchanged(t, projection, "planning inside capacity")

		projection.ApplyProducts(report.Products)
		before.assertUnchanged(t, projection, "applying an empty plan")

		foldReport, err := projection.FoldByAge(ctx, plan)
		if err != nil {
			t.Fatalf("FoldByAge inside capacity: %v", err)
		}
		if foldReport.Triggered || foldReport.NewFolded != 0 || foldReport.MidFolded != 0 || len(foldReport.Products) != 0 || len(foldReport.Requests) != 0 {
			t.Fatalf("fold = %+v, want nothing to fold inside capacity", foldReport)
		}
		before.assertUnchanged(t, projection, "folding inside capacity")
	})

	t.Run("past the threshold with no candidate is a no-op", func(t *testing.T) {
		entries := capacityTurn("no-candidate", 2048, 0)
		volume := len(RenderProjection(entries))
		plan := AgeFoldPlan{KeepRecent: 2, VolumeThreshold: volume / 2}
		projection := capacityProjection(t, entries)
		before := capacityStateOf(projection)

		report, err := projection.PlanByAge(ctx, plan)
		if err != nil {
			t.Fatalf("PlanByAge without a candidate: %v", err)
		}
		if report.Volume <= report.Threshold {
			t.Fatalf("report volume = %d (threshold %d), want the material past its capacity", report.Volume, report.Threshold)
		}
		if report.Triggered {
			t.Errorf("plan = %+v, want no trigger without a candidate", report)
		}
		if len(report.Products) != 0 || report.Counts.Segment != 0 || report.Counts.Middle != 0 {
			t.Errorf("plan = %+v, want no product and no count without a candidate", report)
		}
		before.assertUnchanged(t, projection, "planning without a candidate")

		projection.ApplyProducts(report.Products)
		before.assertUnchanged(t, projection, "applying an empty plan")

		foldReport, err := projection.FoldByAge(ctx, plan)
		if err != nil {
			t.Fatalf("FoldByAge without a candidate: %v", err)
		}
		if foldReport.Triggered {
			t.Errorf("fold = %+v, want no trigger without a candidate", foldReport)
		}
		if foldReport.NewFolded != 0 || foldReport.MidFolded != 0 || len(foldReport.Products) != 0 || len(foldReport.Requests) != 0 {
			t.Errorf("fold = %+v, want no product and no request without a candidate", foldReport)
		}
		before.assertUnchanged(t, projection, "folding without a candidate")
	})

	t.Run("past the threshold still folds a small candidate", func(t *testing.T) {
		entries := capacityTurn("small-candidate", 2048, 1)
		plan := AgeFoldPlan{KeepRecent: 2, VolumeThreshold: len(RenderProjection(entries)) / 2}
		projection := capacityProjection(t, entries)

		report, err := projection.PlanByAge(ctx, plan)
		if err != nil {
			t.Fatalf("PlanByAge over the threshold: %v", err)
		}
		if !report.Triggered || len(report.Products) != 1 || report.Counts.Segment != 2 {
			t.Fatalf("plan = %+v, want the small process detail folded beside the large skeleton", report)
		}

		foldReport, err := projection.FoldByAge(ctx, plan)
		if err != nil {
			t.Fatalf("FoldByAge over the threshold: %v", err)
		}
		if !foldReport.Triggered || foldReport.NewFolded != 2 || len(foldReport.Products) != 1 {
			t.Fatalf("fold = %+v, want the small process detail folded beside the large skeleton", foldReport)
		}
		for _, entry := range projection.Entries() {
			if entry.Type == event.TypeThinkingPlan || entry.Type == event.TypeActionCommand {
				t.Fatalf("projection kept %+v, want the candidate folded", entry)
			}
		}
	})

	t.Run("large material plan-apply equals a direct fold and is idempotent", func(t *testing.T) {
		var entries []ProjectionEntry
		for _, seed := range []string{"large-1", "large-2", "large-3", "large-4"} {
			entries = append(entries, capacityTurn(seed, 512, 2)...)
		}
		plan := AgeFoldPlan{KeepRecent: 2, VolumeThreshold: len(RenderProjection(entries)) / 2}
		direct := capacityProjection(t, entries)
		directReport, err := direct.FoldByAge(ctx, plan)
		if err != nil {
			t.Fatalf("direct FoldByAge: %v", err)
		}
		if !directReport.Triggered || len(directReport.Products) == 0 {
			t.Fatalf("direct fold = %+v, want the large material folded", directReport)
		}

		planned := capacityProjection(t, entries)
		report, err := planned.PlanByAge(ctx, plan)
		if err != nil {
			t.Fatalf("PlanByAge: %v", err)
		}
		if !report.Triggered || len(report.Products) == 0 {
			t.Fatalf("plan = %+v, want the large material folded", report)
		}
		if len(report.Products) != len(directReport.Products) {
			t.Fatalf("plan products = %d, want the direct fold's %d", len(report.Products), len(directReport.Products))
		}

		planned.ApplyProducts(report.Products)
		if got, want := string(RenderProjection(planned.Entries())), string(RenderProjection(direct.Entries())); got != want {
			t.Fatalf("planned entries = %q, want the direct fold's %q", got, want)
		}
		if !reflect.DeepEqual(planned.Entries(), direct.Entries()) {
			t.Fatalf("planned entries = %+v, want the direct fold's %+v", planned.Entries(), direct.Entries())
		}

		folded := capacityStateOf(planned)
		planned.ApplyProducts(report.Products)
		folded.assertUnchanged(t, planned, "applying the plan twice")

		again, err := planned.FoldByAge(ctx, plan)
		if err != nil {
			t.Fatalf("FoldByAge after the plan: %v", err)
		}
		if again.Triggered || len(again.Products) != 0 || len(again.Requests) != 0 {
			t.Fatalf("fold after the plan = %+v, want no candidate left to fold", again)
		}
		folded.assertUnchanged(t, planned, "folding after the plan")
	})

	t.Run("a threshold of one does not force a trigger", func(t *testing.T) {
		entries := capacityTurn("threshold-one", 2048, 0)
		plan := AgeFoldPlan{KeepRecent: DefaultAgeKeepRecent, VolumeThreshold: 1}
		projection := capacityProjection(t, entries)
		before := capacityStateOf(projection)

		report, err := projection.PlanByAge(ctx, plan)
		if err != nil {
			t.Fatalf("PlanByAge at threshold one: %v", err)
		}
		if report.Volume <= report.Threshold {
			t.Fatalf("report volume = %d (threshold %d), want the material past its capacity", report.Volume, report.Threshold)
		}
		if report.Triggered {
			t.Errorf("plan = %+v, want no forced trigger at threshold one", report)
		}
		if len(report.Products) != 0 {
			t.Errorf("plan = %+v, want no product without a candidate", report)
		}
		before.assertUnchanged(t, projection, "planning at threshold one")

		foldReport, err := projection.FoldByAge(ctx, plan)
		if err != nil {
			t.Fatalf("FoldByAge at threshold one: %v", err)
		}
		if foldReport.Triggered || len(foldReport.Products) != 0 || len(foldReport.Requests) != 0 {
			t.Errorf("fold = %+v, want no forced trigger at threshold one", foldReport)
		}
		before.assertUnchanged(t, projection, "folding at threshold one")
	})
}
