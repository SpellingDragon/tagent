package history

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// TestJournalBytesBoundedByLiveSet is the S1 symptom anchor of runtime-recovery-ideal
// (design.md §六): after N settled facts keep landing and every generation folds into
// a newer checkpoint, the journal's real bytes (os.Stat, never a body count) SHALL
// stay bounded by the live key set plus the tail, instead of growing linearly with
// the settled history.
//   - fixture: one journal root, five generations; each generation reopens the tier
//     (one process restart), commits eight settled facts beside the original text
//     each of them owns and one checkpoint fact that stands for the fold, then
//     retires the facts the fold settled into the cold journal and keeps the
//     generation's own facts and its fold hot, so every earlier generation's facts
//     and their originals are history once the next one lands
//   - the families: the facts are the segment family and the originals are the
//     ref/blob family one runtime commits beside every fact it keeps the text of,
//     so the fixture writes the two families the production journal carries rather
//     than the segment family alone
//   - positive: after the last fold, journal.log holds roughly one generation of
//     bytes — the live keys, their originals and the tail — whatever the number of
//     settled generations behind it
//   - negative: without the retirement pass the journal only appends, and a pass
//     that retires the segment family alone leaves the originals of settled facts
//     behind, so the bytes grow in step with the history and the bound fails with
//     the byte count, the generation and the families named
//   - red line: every fact a pass moved out is still readable by identity, and so
//     is every original, so a bound earned by dropping facts fails here instead of
//     reading as bounded
//   - self-evidence: the reopened store states every fact of every generation and
//     answers the original of the first generation byte for byte, so the chain
//     demonstrably grew and the red comes from the byte growth, not from an idle
//     fixture
//   - consumer: the bytes are the file the production journal engine opens at boot
//     (openJournalLog replays 0..size), the originals are written through the
//     tier's own OriginalStore (the face assembly commits every fact's text
//     through) and the pass is the store's own RetireFacts — the entry the
//     retention scaffolding calls
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalBytesBoundedByLiveSet(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	journalPath := filepath.Join(root, "kv", "journal.log")
	const generations = 5
	const factsPerGeneration = 8
	factBody := bytes.Repeat([]byte("s1-anchor-material "), 64)
	originalBody := bytes.Repeat([]byte("s1-anchor-original "), 256)
	checkpointBody := []byte(`{"checkpoint":true}`)
	owner := event.Address{AgentID: "agent-a", SessionID: "session-s1-anchor"}

	committed := make([]int64, 0, generations)
	retained := make([]int64, 0, generations)
	generationKeys := make([][]event.EventID, generations)
	checkpointKeys := make([]event.EventID, generations)
	for generation := 0; generation < generations; generation++ {
		tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, RetireFacts: true})
		if err != nil {
			t.Fatalf("open the journal of generation %d: %v", generation, err)
		}
		keep := map[event.EventID]struct{}{}
		for at := 0; at < factsPerGeneration; at++ {
			record := ledger.SegmentRecord{
				PartitionID: 1,
				WindowTS:    int64(generation),
				Seq:         int64(at + 1),
				EventKey:    event.EventID(fmt.Sprintf("s1-fact-%d-%d", generation, at)),
				EventTime:   int64(generation*100 + at),
				Body:        factBody,
			}.WithChain("fact", "session-s1-anchor")
			if err := tier.Segments.StoreEvent(ctx, record); err != nil {
				t.Fatalf("commit the settled fact %s of generation %d: %v", record.EventKey, generation, err)
			}
			generationKeys[generation] = append(generationKeys[generation], record.EventKey)
			keep[record.EventKey] = struct{}{}
			if err := tier.Originals.Put(ctx, ledger.OriginalRef{
				Key:             record.EventKey,
				Owner:           owner,
				Format:          "text",
				RetentionPolicy: "default",
			}, originalBody); err != nil {
				t.Fatalf("commit the original of the settled fact %s of generation %d: %v", record.EventKey, generation, err)
			}
		}
		checkpoint := ledger.SegmentRecord{
			PartitionID: 1,
			WindowTS:    int64(generation),
			Seq:         factsPerGeneration + 1,
			EventKey:    event.EventID(fmt.Sprintf("s1-checkpoint-%d", generation)),
			EventTime:   int64(generation*100 + factsPerGeneration),
			Body:        checkpointBody,
		}.WithChain("checkpoint", "session-s1-anchor")
		if err := tier.Segments.StoreEvent(ctx, checkpoint); err != nil {
			t.Fatalf("commit the fold of generation %d: %v", generation, err)
		}
		keep[checkpoint.EventKey] = struct{}{}
		checkpointKeys[generation] = checkpoint.EventKey
		info, err := os.Stat(journalPath)
		if err != nil {
			t.Fatalf("stat journal.log after generation %d: %v", generation, err)
		}
		committed = append(committed, info.Size())

		// The fold settles every fact of the generations behind this one: the pass
		// keeps this generation — the live key set the newest fold still reads — and
		// moves the rest into the cold journal, where each fact and each original
		// stays readable by identity.
		if _, err := tier.Segments.RetireFacts(ctx, keep); err != nil {
			t.Fatalf("retire the settled facts of generation %d: %v", generation, err)
		}
		info, err = os.Stat(journalPath)
		if err != nil {
			t.Fatalf("stat journal.log after the retirement of generation %d: %v", generation, err)
		}
		retained = append(retained, info.Size())
		if err := tier.Segments.Close(); err != nil {
			t.Fatalf("close the journal of generation %d: %v", generation, err)
		}
	}

	// The denominator: the history truly grew, so a bound failure is a claim about
	// byte growth and not about a chain that stood still. Appended is what one
	// generation wrote before its own pass, which is the live key set plus tail the
	// next generation is measured against.
	reopened, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("reopen the journal for the denominator: %v", err)
	}
	wantFacts := generations * (factsPerGeneration + 1)
	if count := reopened.Segments.EventCount(); count != wantFacts {
		t.Fatalf("the reopened store states %d facts, want the %d of %d generations: the chain did not grow, so a byte bound would prove nothing",
			count, wantFacts, generations)
	}
	appended := make([]int64, generations)
	for generation := 0; generation < generations; generation++ {
		before := int64(0)
		if generation > 0 {
			before = retained[generation-1]
		}
		appended[generation] = committed[generation] - before
		if appended[generation] <= 0 {
			t.Fatalf("generation %d appended %d bytes over the %d the previous pass retained, want a generation that wrote new facts: the fixture wrote nothing new",
				generation, appended[generation], before)
		}
	}
	liveBytes := appended[generations-1]
	t.Logf("journal.log bytes per generation %v committed, %v retained, %v appended", committed, retained, appended)

	// The anchor: one live key set plus tail is what a bounded journal holds,
	// whatever the settled history behind it. Without the pass the journal only
	// appends, so the file holds every generation and the bound fails.
	if bound := liveBytes * 2; retained[generations-1] > bound {
		t.Fatalf("journal.log holds %d real bytes (os.Stat) after %d generations beside the %d bytes the live key set plus tail account for: the bytes grow linearly with the settled history instead of staying bounded by the live key set (the segment and original families of every settled generation are still hot: committed=%v retained=%v appended=%v)",
			retained[generations-1], generations, liveBytes, committed, retained, appended)
	}
	// The same claim across the generations: what the passes retain does not grow
	// with the settled history behind them, so a journal that merely retained a
	// fixed fraction of the history fails here.
	for generation := 2; generation < generations; generation++ {
		if bound := retained[1] * 2; retained[generation] > bound {
			t.Fatalf("the pass of generation %d retained %d bytes beside the %d of generation 1, over the bound %d: what the journal retains grows with the settled history, one generation of the segment and original families at a time (%v)",
				generation, retained[generation], retained[1], bound, retained)
		}
	}

	// The red line: every fact the passes moved into the cold journal still answers
	// the bytes it was committed with, and so does the original it owns, so the
	// bound above is earned by moving settled facts rather than by dropping them.
	for _, key := range generationKeys[0] {
		stored, held, err := reopened.Segments.GetEvent(ctx, key)
		if err != nil {
			t.Fatalf("read the settled fact %s by identity: %v", key, err)
		}
		if !held {
			t.Fatalf("the settled fact %s answers missing, want the cold journal to answer the bytes it was committed with", key)
		}
		if !bytes.Equal(stored.Body, factBody) {
			t.Fatalf("the settled fact %s answers %q, want the bytes %q it was committed with", key, stored.Body, factBody)
		}
		original := reopened.Originals.Get(ctx, key)
		if original.Outcome != ledger.RecallFound {
			t.Fatalf("the original of the settled fact %s reads as %v, want the bytes it was committed with", key, original)
		}
		if !bytes.Equal(original.Body, originalBody) {
			t.Fatalf("the original of the settled fact %s answers %q, want the bytes it was committed with", key, original.Body)
		}
	}
	if err := reopened.Segments.Close(); err != nil {
		t.Fatalf("close the reopened journal: %v", err)
	}
}
