package acceptance

import (
	"context"
	"testing"

	"github.com/SpellingDragon/tagent/memory/replay"
)

// scenarioS14Threshold is the volume threshold the three-band scenario folds
// past: the four turns render above it, so the fold runs.
const scenarioS14Threshold = 600

// TestScenario14AgeBandProducts drives the age fold of one projection over four closed turns.
// - the fold produces one segment fold for the process detail of the newest band and one history summary for the middle and oldest bands
// - the history summary leads the projection the next request reads
// - a projection inside the volume threshold is left exactly as it stands
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestScenario14AgeBandProducts(t *testing.T) {
	ctx := context.Background()
	scenario, err := ScenarioOf("S14")
	if err != nil {
		t.Fatalf("read the scenario contract: %v", err)
	}

	projection := scenarioProjection(t, scenarioFourTurns())
	report, err := projection.FoldByAge(ctx, replay.AgeFoldPlan{KeepRecent: 1, VolumeThreshold: scenarioS14Threshold})
	if err != nil {
		t.Fatalf("fold the projection by age: %v", err)
	}
	if !report.Triggered {
		t.Fatalf("fold report = %+v, want the four turns to pass the volume threshold", report)
	}
	entries := projection.Entries()

	untouched := scenarioProjection(t, scenarioFourTurns())
	untouchedBefore := replay.RenderProjection(untouched.Entries())
	quiet, err := untouched.FoldByAge(ctx, replay.AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 1 << 20})
	if err != nil {
		t.Fatalf("fold the projection inside the threshold: %v", err)
	}

	observations := map[string]any{}
	observations["age_fold_products"] = map[string]any{
		"segment_fold":    scenarioCountType(entries, replay.ProjectionTypeSegmentFold),
		"history_summary": scenarioCountType(entries, replay.ProjectionTypeHistorySummary),
	}
	observations["history_summary_leads"] = len(entries) > 0 && entries[0].Type == replay.ProjectionTypeHistorySummary
	observations["under_threshold_untouched"] = !quiet.Triggered &&
		len(quiet.Products) == 0 &&
		string(replay.RenderProjection(untouched.Entries())) == string(untouchedBefore)

	refs := []string{}
	for _, product := range report.Products {
		refs = append(refs, string(product.Key))
	}
	EmitScenarioRecord(t, scenario, refs, observations)
}
