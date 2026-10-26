// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
//
// The relation journal's closed loop through the real rebuild: a cold-start
// replay writes the causal chain it rebuilds, and a later open serves exactly
// that chain from the journal alone.
package history

import (
	"context"
	"maps"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
)

func TestRelationJournalReplaysTheRebuiltChain(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	if err != nil {
		t.Fatalf("OpenTier: %v", err)
	}
	chain := durableChain{log: ledger.TierFactLog(tier)}
	session := replay.ChainKey{PartitionID: 2, SessionID: "journal-session"}
	base := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	facts := []replay.ChainFact{
		{Key: "k-1", Chain: session, Type: event.TypeExternalInput, Summary: "first", Content: "first turn text", Time: base},
		{Key: "k-2", Chain: session, Type: event.TypeAgentOutput, Summary: "second", Content: "second turn text", Time: base.Add(time.Minute)},
		{Key: "k-3", Chain: session, Type: event.TypeThinkingPlan, Summary: "third", Content: "third turn text", Time: base.Add(2 * time.Minute)},
		{Key: "k-4", Chain: session, Type: event.TypeAgentOutput, Summary: "fourth", Content: "fourth turn text", Time: base.Add(3 * time.Minute)},
	}
	for _, fact := range facts {
		if err := chain.commit(ctx, fact); err != nil {
			t.Fatalf("commit %s: %v", fact.Key, err)
		}
	}

	relations, err := replay.OpenMemoryRelations(root)
	if err != nil {
		t.Fatalf("OpenMemoryRelations: %v", err)
	}
	projection := replay.NewReplayProjection()
	report, err := replay.RebuildProjectionFromWAL(ctx, projection, chain, relations, session, replay.RebuildOptions{PageSize: 2})
	if err != nil {
		t.Fatalf("RebuildProjectionFromWAL: %v", err)
	}
	if !report.Scanned || report.Partial || report.Entries != len(facts) {
		t.Fatalf("rebuild report = %+v, want a scanned chain of %d entries", report, len(facts))
	}
	want := map[event.EventID]event.EventID{
		"k-2": "k-1",
		"k-3": "k-2",
		"k-4": "k-3",
	}
	if got := relations.Links(); !maps.Equal(got, want) {
		t.Fatalf("rebuilt links = %v, want %v", got, want)
	}
	if err := relations.VerifyReplay(ctx); err != nil {
		t.Fatalf("VerifyReplay over the rebuilt chain: %v", err)
	}

	reopened, err := replay.OpenMemoryRelations(root)
	if err != nil {
		t.Fatalf("reopen the relation ledger: %v", err)
	}
	if got := reopened.Links(); !maps.Equal(got, want) {
		t.Fatalf("replayed links = %v, want the rebuilt chain %v", got, want)
	}
	if parent, err := reopened.GetParent("k-4"); err != nil || parent != "k-3" {
		t.Fatalf("replayed parent of k-4 = (%q, %v), want k-3", parent, err)
	}
}
