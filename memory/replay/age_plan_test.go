package replay

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

// TestPlanAndApplyAnswerTheFold checks a plan reads the projection untouched and the applied products answer one direct fold.
// - a plan leaves the entries exactly where they stood
// - applying the planned products answers the entries one direct fold answers
// - applying the same products twice changes nothing
//
// 契约: docs/FACT-VOCABULARY.md#families
func TestPlanAndApplyAnswerTheFold(t *testing.T) {
	plan := AgeFoldPlan{KeepRecent: 2, VolumeThreshold: 2048}
	build := func() []ProjectionEntry {
		var entries []ProjectionEntry
		for step, seed := range []string{"t1", "t2", "t3", "t4"} {
			entries = append(entries, ageTurn(seed, step, 2)...)
		}
		return entries
	}

	t.Run("a plan reads without touching the projection", func(t *testing.T) {
		projection := ageProjection(t, build())
		before := digestOf(projection.Entries())
		report, err := projection.PlanByAge(context.Background(), plan)
		if err != nil {
			t.Fatalf("plan the fold: %v", err)
		}
		if !report.Triggered || len(report.Products) == 0 {
			t.Fatalf("plan = %+v, want the products the material is due", report)
		}
		if got := digestOf(projection.Entries()); got != before {
			t.Fatalf("the plan rewrote the projection: before %s after %s", before, got)
		}
	})

	t.Run("applying the plan answers one direct fold", func(t *testing.T) {
		direct := ageProjection(t, build())
		if _, err := direct.FoldByAge(context.Background(), plan); err != nil {
			t.Fatalf("fold directly: %v", err)
		}
		planned := ageProjection(t, build())
		report, err := planned.PlanByAge(context.Background(), plan)
		if err != nil {
			t.Fatalf("plan the fold: %v", err)
		}
		planned.ApplyProducts(report.Products)
		if got, want := digestOf(planned.Entries()), digestOf(direct.Entries()); got != want {
			t.Fatalf("planned entries = %s, want the direct fold's %s", got, want)
		}

		again := digestOf(planned.Entries())
		planned.ApplyProducts(report.Products)
		if got := digestOf(planned.Entries()); got != again {
			t.Fatalf("applying the plan twice rewrote the projection: %s -> %s", again, got)
		}
	})
}

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

// earlyExitTurn lays one closed turn out on a projection: the input that opens
// it, the process detail a fold consumes and the output that closes it.
func earlyExitTurn(seed string, payload, process int) []ProjectionEntry {
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

// earlyExitProjection returns a projection over the entries given, in order.
func earlyExitProjection(t *testing.T, entries []ProjectionEntry) *ReplayProjection {
	t.Helper()
	projection := NewReplayProjection()
	for _, entry := range entries {
		projection.Append(context.Background(), entry)
	}
	return projection
}

// earlyExitState is what one projection presents through its public read face.
type earlyExitState struct {
	rendered string
	entries  []ProjectionEntry
}

// earlyExitStateOf reads one projection through its public read face.
func earlyExitStateOf(projection *ReplayProjection) earlyExitState {
	return earlyExitState{
		rendered: string(RenderProjection(projection.Entries())),
		entries:  projection.Entries(),
	}
}

// assertUnchanged reports whether one judgment rewrote a projection it answered.
func (s earlyExitState) assertUnchanged(t *testing.T, projection *ReplayProjection, when string) {
	t.Helper()
	now := earlyExitStateOf(projection)
	if now.rendered != s.rendered {
		t.Fatalf("%s rewrote the rendered projection: before %q after %q", when, s.rendered, now.rendered)
	}
	if len(now.entries) != len(s.entries) {
		t.Fatalf("%s rewrote the entries: before %d after %d", when, len(s.entries), len(now.entries))
	}
	for i := range now.entries {
		if now.entries[i] != s.entries[i] {
			t.Fatalf("%s rewrote entry %d: before %+v after %+v", when, i, s.entries[i], now.entries[i])
		}
	}
}

// TestFoldCapacityEarlyExitDecision pins the capacity gate as an early exit.
// - inside its threshold a judgment answers from the volume the projection keeps and never reads the material
// - past the threshold the judgment scans the material and mints the candidates it finds
// - a judgment inside its threshold writes nothing and mints no product
//
// 契约: docs/wiki/agent/event-flow.md#unified-compression
func TestFoldCapacityEarlyExitDecision(t *testing.T) {
	ctx := context.Background()
	entries := append(earlyExitTurn("early-1", 48, 2), earlyExitTurn("early-2", 48, 2)...)
	entries = append(entries, earlyExitTurn("early-3", 48, 2)...)
	threshold := len(RenderProjection(entries))
	plan := AgeFoldPlan{KeepRecent: 2, VolumeThreshold: threshold}

	t.Run("inside capacity answers without reading the material", func(t *testing.T) {
		projection := earlyExitProjection(t, entries)
		before := earlyExitStateOf(projection)

		planAllocs := testing.AllocsPerRun(50, func() {
			projection.PlanByAge(ctx, plan)
		})
		if planAllocs != 0 {
			t.Fatalf("PlanByAge inside capacity allocated %.1f objects per run, want the judgment to answer from the volume it keeps without reading the material", planAllocs)
		}
		before.assertUnchanged(t, projection, "PlanByAge inside capacity")

		foldAllocs := testing.AllocsPerRun(50, func() {
			projection.FoldByAge(ctx, plan)
		})
		if foldAllocs != 0 {
			t.Fatalf("FoldByAge inside capacity allocated %.1f objects per run, want the judgment to answer from the volume it keeps without reading the material", foldAllocs)
		}
		before.assertUnchanged(t, projection, "FoldByAge inside capacity")
	})

	t.Run("past the threshold scans the candidates", func(t *testing.T) {
		projection := earlyExitProjection(t, entries)
		over := plan
		over.VolumeThreshold = threshold / 2

		allocs := testing.AllocsPerRun(20, func() {
			projection.PlanByAge(ctx, over)
		})
		if allocs == 0 {
			t.Fatalf("PlanByAge past the threshold allocated nothing, want the judgment to scan the material for candidates")
		}

		report, err := projection.PlanByAge(ctx, over)
		if err != nil {
			t.Fatalf("PlanByAge past the threshold: %v", err)
		}
		if !report.Triggered || len(report.Products) == 0 {
			t.Fatalf("plan = %+v, want the material past the threshold folded", report)
		}
		if report.Counts.Segment == 0 && report.Counts.Middle == 0 {
			t.Fatalf("plan = %+v, want the scanned material counted", report)
		}
		if report.Volume != threshold || report.Volume <= report.Threshold {
			t.Fatalf("report volume = %d (threshold %d), want the volume the projection keeps, past its threshold", report.Volume, report.Threshold)
		}
	})
}

// digestOf reports a stable digest of one entry sequence: keys and contents in
// order, so two sequences compare by what they present.
func digestOf(entries []ProjectionEntry) string {
	var digest strings.Builder
	for _, entry := range entries {
		digest.WriteString("|")
		digest.WriteString(string(entry.Key))
		digest.WriteString("=")
		digest.WriteString(entry.Content)
	}
	return digest.String()
}
