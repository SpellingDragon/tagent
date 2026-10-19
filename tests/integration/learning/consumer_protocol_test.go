package learning

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/SpellingDragon/tagent/rl"
)

// TestSecondWaveConsumersShareTheProtocol pins the export consumer to the shared consumer protocol.
// - the export reads the authorized source facts and writes each sample line under the fact identity it was reduced from
// - the sealed snapshot is the artifact fact: its lines carry the manifest's export identity
// - the export is read-only over its source, and an already sealed identity is refused instead of overwritten
// 契约: docs/EXTENSIONS.md#consumer-protocol
func TestSecondWaveConsumersShareTheProtocol(t *testing.T) {
	ctx := context.Background()
	src := newFakeExportSource()
	src.add("p1", exportFactOf("e1", "p1", "call-1", "cap-1", "ep-a", "dec-1", "att-1", "", 1000))
	src.episodes = []rl.EpisodeRecord{{ID: "ep-a", RootID: "ep-a", Lineage: rl.LineageKnown}}
	before := map[string][]rl.ExportedFact{}
	for partition, facts := range src.partitions {
		before[partition] = append([]rl.ExportedFact(nil), facts...)
	}
	exporter := rl.NewTrainingExporter(src, rl.NewFeedbackLedger(), rl.NewRewardLedger(), exportTestRule())

	result, err := exporter.Export(ctx, rl.ExportRequest{PartitionIDs: []string{"p1"}})
	if err != nil || result.Partial || !result.Manifest.Complete || result.Written != 1 {
		t.Fatalf("export = (%+v, %v), want one complete run beside the one source fact", result, err)
	}
	lines, held := exporter.Snapshot(result.Manifest.ExportID)
	if !held || len(lines) != 1 {
		t.Fatalf("snapshot = (%d lines, held=%v), want the one sealed line", len(lines), held)
	}
	if lines[0].Fact.EventKey != "e1" {
		t.Fatalf("line fact = %q, want the source fact identity e1", lines[0].Fact.EventKey)
	}
	if lines[0].Provenance.ExportID != result.Manifest.ExportID {
		t.Fatalf("line export = %q, want the manifest identity %q", lines[0].Provenance.ExportID, result.Manifest.ExportID)
	}
	if !reflect.DeepEqual(src.partitions, before) {
		t.Fatalf("source after the export = %+v, want %+v unchanged", src.partitions, before)
	}

	sealed := append([]rl.SampleLine(nil), lines...)
	if err := exporter.SealSnapshot(result.Manifest.ExportID, nil); !errors.Is(err, rl.ErrExportConflict) {
		t.Fatalf("seal the same identity again = %v, want ErrExportConflict", err)
	}
	again, _ := exporter.Snapshot(result.Manifest.ExportID)
	if !reflect.DeepEqual(again, sealed) {
		t.Fatalf("snapshot after the refused seal = %+v, want %+v unchanged", again, sealed)
	}
}
