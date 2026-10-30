package replay

import (
	"context"
	"strings"
	"testing"
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
