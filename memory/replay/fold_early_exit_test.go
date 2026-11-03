package replay

import (
	"context"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

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
