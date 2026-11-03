// 契约: docs/FACT-VOCABULARY.md#families
package replay

import (
	"bytes"
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// TestFoldIsIdempotentOverTheSameMaterial pins the fold contract:
//   - the summary and every synthetic ref are filed under content-addressed negative keys
//   - the same material folds to the same payload, key and render, whichever projection offers it
//   - a second fold of an already folded projection mints no second product and moves nothing
//   - two identical keyless entries keep two refs, so a rebuild never deduplicates one away
func TestFoldIsIdempotentOverTheSameMaterial(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	material := func() []ProjectionEntry {
		return []ProjectionEntry{
			{Key: "f-1", Type: event.TypeExternalInput, Summary: "in", Content: "输入原文", Time: base},
			{Key: "f-2", Type: event.TypeAgentOutput, Summary: "out", Content: "输出正文", Time: base.Add(time.Minute)},
			{Key: "f-3", Type: event.TypeActionCommand, Summary: "call", Content: "grep -n TODO", Time: base.Add(2 * time.Minute)},
			{Type: event.TypeActionCommand, Summary: "chain", Content: "工具链 grep → sed", Time: base.Add(3 * time.Minute)},
			{Type: event.TypeActionCommand, Summary: "chain", Content: "工具链 grep → sed", Time: base.Add(3 * time.Minute)},
		}
	}
	project := func(entries []ProjectionEntry) *ReplayProjection {
		projection := NewReplayProjection()
		for _, entry := range entries {
			projection.Append(ctx, entry)
		}
		return projection
	}

	first := project(material())
	summary, err := first.Fold(ctx)
	if err != nil {
		t.Fatalf("Fold: %v", err)
	}
	payload := first.Compaction()
	if payload == nil {
		t.Fatal("Fold left no compaction payload")
	}
	if !SyntheticKey(summary.Key) || !strings.HasPrefix(string(summary.Key), "-fold-") {
		t.Fatalf("summary key = %q, want a content-addressed negative fold key", summary.Key)
	}
	if summary.Key != payload.Summary.Key {
		t.Fatalf("summary key = %q, want the payload summary key %q", summary.Key, payload.Summary.Key)
	}
	if payload.Summary.Content != "compaction: in; out; call; chain; chain" {
		t.Fatalf("summary content = %q, want the folded summaries in payload order", payload.Summary.Content)
	}
	if len(payload.Refs) != 3 {
		t.Fatalf("refs = %+v, want the retained tool fact and the two keyless entries", payload.Refs)
	}
	if payload.Refs[0].Key != "f-3" || !SyntheticKey(payload.Refs[1].Key) || !SyntheticKey(payload.Refs[2].Key) {
		t.Fatalf("ref keys = %v, want one positive retained ref and two synthetic refs", refKeys(payload.Refs))
	}
	if payload.Refs[1].Key == payload.Refs[2].Key {
		t.Fatalf("synthetic refs = %q and %q, want two refs for two identical keyless entries", payload.Refs[1].Key, payload.Refs[2].Key)
	}

	second := project(material())
	secondSummary, err := second.Fold(ctx)
	if err != nil {
		t.Fatalf("second Fold: %v", err)
	}
	if !reflect.DeepEqual(second.Compaction(), payload) || secondSummary.Key != summary.Key {
		t.Fatalf("second fold payload = %+v, want the same content-addressed fold %+v", second.Compaction(), payload)
	}
	if !bytes.Equal(RenderProjection(second.Entries()), RenderProjection(first.Entries())) {
		t.Fatal("the same material rendered two different folds")
	}

	folded := RenderProjection(first.Entries())
	again, err := first.Fold(ctx)
	if err != nil {
		t.Fatalf("Fold of a folded projection: %v", err)
	}
	if again.Key != summary.Key || !reflect.DeepEqual(first.Compaction(), payload) {
		t.Fatalf("second fold = %+v, want the payload already filed", again)
	}
	if !bytes.Equal(RenderProjection(first.Entries()), folded) {
		t.Fatal("a second fold of the same projection moved it")
	}

	other := project([]ProjectionEntry{{Key: "f-1", Type: event.TypeExternalInput, Summary: "in", Content: "另一份原文", Time: base}})
	otherSummary, err := other.Fold(ctx)
	if err != nil {
		t.Fatalf("Fold of other material: %v", err)
	}
	if otherSummary.Key == summary.Key {
		t.Fatalf("summary key = %q, want different material under a different content-addressed key", otherSummary.Key)
	}
}

// readFoldMeasurementFile reads one repository file the fold-cost decision cites.
func readFoldMeasurementFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

// TestFoldDepthMeasurementExists pins the independent measurements the fold-cost decision reads.
// - the fold-depth benchmark stays a smoke build measuring exactly the two approved windows
// - the recovery anchors the same decision cites stay named test functions
//
// 契约: docs/FACT-VOCABULARY.md#reduction-criteria
func TestFoldDepthMeasurementExists(t *testing.T) {
	benchmark := readFoldMeasurementFile(t, "../../examples/wechat-bot/smoke_bench_test.go")
	if !strings.HasPrefix(benchmark, "//go:build smoke\n") {
		t.Fatalf("the fold-depth benchmark is not behind the smoke build tag, so the measured windows cannot be run as the decision commands run them")
	}
	if !strings.Contains(benchmark, "func BenchmarkFoldDepthCost(") {
		t.Fatalf("the fold-depth benchmark is absent, so the fold cost the capacity decision reads has no independent measurement")
	}
	if !strings.Contains(benchmark, "[]int{256, 32768}") {
		t.Fatalf("the fold-depth benchmark no longer measures exactly the approved windows [256 32768], so a window the long matrix does not run could be reported as measured")
	}

	restore := readFoldMeasurementFile(t, "../../tests/integration/host/compress_pipeline_test.go")
	if !strings.Contains(restore, "func TestCompressProductRestoresAfterRestart(") {
		t.Fatalf("the restart recovery anchor is absent, so the product restore the fold-cost decision cites is not measured")
	}
	refold := readFoldMeasurementFile(t, "../../tests/integration/host/age_fold_test.go")
	if !strings.Contains(refold, "func TestRebuildRefoldsIdempotent(") {
		t.Fatalf("the rebuild anchor is absent, so the refold idempotence the fold-cost decision cites is not measured")
	}
}

func refKeys(refs []CompactionRef) []event.EventID {
	keys := make([]event.EventID, 0, len(refs))
	for _, ref := range refs {
		keys = append(keys, ref.Key)
	}
	return keys
}
