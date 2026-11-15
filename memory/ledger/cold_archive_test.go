// The relocation contract of the journal tier: a pass moves the facts the caller's
// live key set does not name out of the hot journal, and every settled fact still
// answers the bytes and the position it was committed at.
//
// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
package ledger

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

// settledFixture commits the facts one relocation pass is measured on: three
// generations of settled events beside the fold each generation filed, and one
// event of the last generation the live set still names.
//   - every generation writes its own window, so a window stays hot only while a live event of that window remains
//   - each generation's fold is the newest checkpoint of its time, so a rewrite that lost the commit order would answer an older fold as the newest
type settledFixture struct {
	tier  *Tier
	root  string
	keys  []event.EventID
	live  event.EventID
	types map[event.EventID]string
}

func commitSettledFacts(t *testing.T, ctx context.Context, root string, generations, perGeneration int) settledFixture {
	t.Helper()
	tier, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: root, RetireFacts: true})
	if err != nil {
		t.Fatalf("open the journal tier: %v", err)
	}
	fixture := settledFixture{tier: tier, root: root, types: map[event.EventID]string{}}
	for generation := 0; generation < generations; generation++ {
		for at := 0; at < perGeneration; at++ {
			record := SegmentRecord{
				PartitionID: 1,
				WindowTS:    int64(generation),
				Seq:         int64(at + 1),
				EventKey:    event.EventID(fmt.Sprintf("settled-%d-%d", generation, at)),
				EventTime:   int64(generation*100 + at),
				Body:        bytes.Repeat([]byte("settled-material "), 64),
			}.WithChain("fact", "session-cold")
			if err := tier.Segments.StoreEvent(ctx, record); err != nil {
				t.Fatalf("commit the settled fact %s: %v", record.EventKey, err)
			}
			fixture.keys = append(fixture.keys, record.EventKey)
			fixture.types[record.EventKey] = "fact"
		}
		fold := SegmentRecord{
			PartitionID: 1,
			WindowTS:    int64(generation),
			Seq:         int64(perGeneration + 1),
			EventKey:    event.EventID(fmt.Sprintf("settled-fold-%d", generation)),
			EventTime:   int64(generation * 100),
			Body:        []byte(fmt.Sprintf(`{"fold":%d}`, generation)),
		}.WithChain("checkpoint", "session-cold")
		if err := tier.Segments.StoreEvent(ctx, fold); err != nil {
			t.Fatalf("commit the fold of generation %d: %v", generation, err)
		}
		fixture.keys = append(fixture.keys, fold.EventKey)
		fixture.types[fold.EventKey] = "checkpoint"
		fixture.live = fold.EventKey
	}
	return fixture
}

// TestColdArchiveRelocatesSettledKeys pins the relocation pass: the facts the live set does not name leave the hot journal.
//   - positive: the settled index entries leave the hot journal and the cold journal holds their bodies byte for byte
//   - positive: every relocated fact still reads back with the position it was committed at
//   - negative: the live key set never leaves the hot journal, because nothing settled about it
func TestColdArchiveRelocatesSettledKeys(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	fixture := commitSettledFacts(t, ctx, root, 3, 4)
	journal := filepath.Join(root, kvDirName, journalFileName)
	before, err := os.Stat(journal)
	if err != nil {
		t.Fatalf("stat the hot journal before the relocation: %v", err)
	}
	committed := fixture.tier.Segments.EventCount()
	if want := len(fixture.keys); committed != want {
		t.Fatalf("the store states %d committed events, want the %d the fixture committed", committed, want)
	}

	live := map[event.EventID]struct{}{fixture.live: {}}
	report, err := fixture.tier.Segments.RetireFacts(ctx, live)
	if err != nil {
		t.Fatalf("relocate the settled facts: %v", err)
	}
	if report.Settled == 0 {
		t.Fatal("the relocation settled no key, want every fact the live set does not name")
	}
	holding, err := fixture.tier.Store.Keys(ctx, "")
	if err != nil {
		t.Fatalf("enumerate the hot journal after the relocation: %v", err)
	}
	if want := 5; len(holding) != want {
		t.Fatalf("the hot journal holds %d key(s) after the relocation (%v), want the %d of the single live fact beside the settled count and the settled commit bound",
			len(holding), holding, want)
	}
	for _, key := range holding {
		if _, settled := fixture.types[event.EventID(key)]; settled {
			t.Fatalf("the hot journal still holds %q, want only the live fact", key)
		}
	}
	if entries := fixture.tier.Segments.IndexEntries(); entries != 1 {
		t.Fatalf("the resident index holds %d entry(ies) after the relocation, want the single live fact", entries)
	}
	if count := fixture.tier.Segments.EventCount(); count != committed {
		t.Fatalf("the store states %d committed event(s) after the relocation, want the %d it committed: a bounded hot journal must not read as a store that lost its history", count, committed)
	}
	after, err := os.Stat(journal)
	if err != nil {
		t.Fatalf("stat the hot journal after the relocation: %v", err)
	}
	if after.Size() >= before.Size() {
		t.Fatalf("the hot journal holds %d byte(s) after the relocation beside the %d it held before, want the settled bytes gone", after.Size(), before.Size())
	}

	for at, key := range fixture.keys {
		stored, held, err := fixture.tier.Segments.GetEvent(ctx, key)
		if err != nil {
			t.Fatalf("read the settled fact %s by identity: %v", key, err)
		}
		if !held {
			t.Fatalf("the settled fact %s answers missing, want the bytes it was committed with", key)
		}
		if want := int64(at%(len(fixture.keys)/3)) + 1; stored.Seq != want {
			t.Fatalf("the settled fact %s answers sequence %d, want the %d it was committed at: a relocation moves bytes, never identities", key, stored.Seq, want)
		}
	}
}

// TestColdTierReadSurvivesReopen pins the identity read across a restart that opened no pass.
//   - the cold journal is opened lazily by the read that needs it, so a process that never read a settled fact pays nothing for it
func TestColdTierReadSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	fixture := commitSettledFacts(t, ctx, root, 2, 3)
	settled := fixture.keys[0]
	settledBody, held, err := fixture.tier.Segments.GetEvent(ctx, settled)
	if err != nil || !held {
		t.Fatalf("read the fact before its relocation answered (%v, held=%v)", err, held)
	}
	if _, err := fixture.tier.Segments.RetireFacts(ctx, map[event.EventID]struct{}{fixture.live: {}}); err != nil {
		t.Fatalf("relocate the settled facts: %v", err)
	}
	if err := fixture.tier.Segments.Close(); err != nil {
		t.Fatalf("close the tier before the restart: %v", err)
	}

	reopened, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: root, RetireFacts: true})
	if err != nil {
		t.Fatalf("reopen the tier over the relocated root: %v", err)
	}
	defer reopened.Segments.Close()
	if entries := reopened.Segments.IndexEntries(); entries != 1 {
		t.Fatalf("the reopened store publishes %d entry(ies), want the single live fact: the boot published the settled history again", entries)
	}
	got, held, err := reopened.Segments.GetEvent(ctx, settled)
	if err != nil {
		t.Fatalf("read the settled fact after the restart: %v", err)
	}
	if !held {
		t.Fatalf("the settled fact %s answers missing after the restart, want the cold journal to answer it", settled)
	}
	if !bytes.Equal(got.Body, settledBody.Body) {
		t.Fatalf("the settled fact %s reads back %q after the restart, want the bytes %q it was committed with", settled, got.Body, settledBody.Body)
	}
	if got.PartitionID != settledBody.PartitionID || got.WindowTS != settledBody.WindowTS || got.Seq != settledBody.Seq {
		t.Fatalf("the settled fact %s answers position %d/%d/%d after the restart, want the %d/%d/%d it was committed at",
			settled, got.PartitionID, got.WindowTS, got.Seq, settledBody.PartitionID, settledBody.WindowTS, settledBody.Seq)
	}
}

// TestColdArchiveRewriteKeepsCommitOrder pins the order invariant of the rewrite.
//   - the newest checkpoint is still the newest after every settled fact left the hot journal
//   - negative: a rewrite that walked the log forwards would name an older fold as the newest one
func TestColdArchiveRewriteKeepsCommitOrder(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	fixture := commitSettledFacts(t, ctx, root, 4, 3)
	newest := ""
	if key, held, err := fixture.tier.Segments.NewestOfType(ctx, "checkpoint"); err != nil || !held {
		t.Fatalf("read the newest checkpoint before the relocation answered (%v, held=%v)", err, held)
	} else {
		newest = string(key)
	}
	if _, err := fixture.tier.Segments.RetireFacts(ctx, map[event.EventID]struct{}{fixture.live: {}}); err != nil {
		t.Fatalf("relocate the settled facts: %v", err)
	}
	key, held, err := fixture.tier.Segments.NewestOfType(ctx, "checkpoint")
	if err != nil {
		t.Fatalf("read the newest checkpoint after the relocation: %v", err)
	}
	if !held {
		t.Fatalf("the store answers no checkpoint after the relocation, want the newest fold kept")
	}
	if string(key) != newest {
		t.Fatalf("the rewrite names %s as the newest checkpoint, want the %s the log committed last: the rewrite reordered the commits", key, newest)
	}
	first := true
	if err := fixture.tier.Segments.EachIndexNewestFirst(ctx, func(key event.EventID, partition int, seq int64, typ string, session event.SessionID) error {
		if first {
			first = false
			if string(key) != newest {
				return fmt.Errorf("the newest-first walk opened with %s, want the newest commit %s", key, newest)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("walk the rewritten index newest first: %v", err)
	}
}

// TestColdArchiveRanksTheSettledCommitsBesideTheResidentOnes pins the one order that spans both journals.
//   - positive: a settled fact committed after a resident one is answered as the newer, by NewestOfType and by the walk
//   - positive: a store whose newest resident entry is past the settled bound answers newest-first without opening the cold journal
//   - negative: an identity left hot beside settled ones that are newer is not answered as the newest, which a merge in turn would answer
func TestColdArchiveRanksTheSettledCommitsBesideTheResidentOnes(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	tier, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: root, RetireFacts: true})
	if err != nil {
		t.Fatalf("open the journal tier: %v", err)
	}
	commit := func(key string, seq int64, typ string) event.EventID {
		t.Helper()
		record := SegmentRecord{
			PartitionID: 1,
			WindowTS:    1,
			Seq:         seq,
			EventKey:    event.EventID(key),
			EventTime:   seq,
			Body:        []byte("material of " + key),
		}.WithChain(typ, "session-rank")
		if err := tier.Segments.StoreEvent(ctx, record); err != nil {
			t.Fatalf("commit %s: %v", key, err)
		}
		return record.EventKey
	}
	survivor := commit("rank-survivor-fold", 1, "checkpoint")
	commit("rank-settled-fact", 2, "fact")
	newestFold := commit("rank-settled-fold", 3, "checkpoint")

	if _, err := tier.Segments.RetireFacts(ctx, map[event.EventID]struct{}{survivor: {}}); err != nil {
		t.Fatalf("relocate the settled facts: %v", err)
	}
	if entries := tier.Segments.IndexEntries(); entries != 1 {
		t.Fatalf("the resident index holds %d entry(ies) after the relocation, want the single surviving fold", entries)
	}

	answered, held, err := tier.Segments.NewestOfType(ctx, "checkpoint")
	if err != nil || !held {
		t.Fatalf("read the newest checkpoint after the relocation answered (%v, held=%v)", err, held)
	}
	if answered != newestFold {
		t.Fatalf("the newest checkpoint is %s, want the %s committed last: the settled commits rank below the resident one",
			answered, newestFold)
	}
	var walked []string
	if err := tier.Segments.EachIndexNewestFirst(ctx, func(key event.EventID, _ int, _ int64, _ string, _ event.SessionID) error {
		walked = append(walked, string(key))
		return nil
	}); err != nil {
		t.Fatalf("walk the index newest first: %v", err)
	}
	want := []string{"rank-settled-fold", "rank-settled-fact", "rank-survivor-fold"}
	if len(walked) != len(want) {
		t.Fatalf("the walk named %d entries (%v), want the %d the store committed", len(walked), walked, len(want))
	}
	for at, key := range want {
		if walked[at] != key {
			t.Fatalf("the walk named %v, want %v: the commit order the two journals state was not merged", walked, want)
		}
	}

	tail := commit("rank-tail-fact", 4, "fact")
	if err := tier.Segments.Close(); err != nil {
		t.Fatalf("close the tier before the restart: %v", err)
	}
	reopened, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: root, RetireFacts: true})
	if err != nil {
		t.Fatalf("reopen the tier over the relocated root: %v", err)
	}
	defer reopened.Segments.Close()
	if answered, held, err := reopened.Segments.NewestOfType(ctx, "fact"); err != nil || !held {
		t.Fatalf("read the newest fact after the restart answered (%v, held=%v)", err, held)
	} else if answered != tail {
		t.Fatalf("the newest fact is %s, want the %s committed after the relocation", answered, tail)
	}
	if reopened.Segments.cold != nil {
		t.Fatal("the newest-first walk opened the cold journal beside a resident entry past the settled bound, want the settled history left closed")
	}
}

// TestColdArchiveRefusesToOverwriteAnIdentity pins the one-identity-one-fact rule of the cold journal.
//   - negative: a key the cold journal already holds with other bytes refuses the whole pass
//   - positive: the hot journal keeps every key it started with, and still answers the fact it refused to overwrite
func TestColdArchiveRefusesToOverwriteAnIdentity(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	fixture := commitSettledFacts(t, ctx, root, 2, 2)
	settled := fixture.keys[0]
	settledKey := fmt.Sprintf("1:idx:%s", settled)
	cold, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: filepath.Join(root, coldDirName)})
	if err != nil {
		t.Fatalf("open the cold journal: %v", err)
	}
	if err := cold.Commit(ctx, Batch{Puts: []Pair{{Key: settledKey, Value: []byte("other bytes")}}}); err != nil {
		t.Fatalf("plant the conflicting cold bytes: %v", err)
	}
	before, err := fixture.tier.Store.Keys(ctx, "")
	if err != nil {
		t.Fatalf("enumerate the hot journal before the refused pass: %v", err)
	}
	if _, err := fixture.tier.Segments.RetireFacts(ctx, map[event.EventID]struct{}{fixture.live: {}}); !errors.Is(err, ErrMigrateMismatch) {
		t.Fatalf("a cold journal holding another value under a settled identity answered %v, want it refused by name", err)
	}
	after, err := fixture.tier.Store.Keys(ctx, "")
	if err != nil {
		t.Fatalf("enumerate the hot journal after the refused pass: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("the refused pass left %d hot key(s) beside the %d there were: a copy that never verified must not remove anything", len(after), len(before))
	}
	if stored, held, err := fixture.tier.Segments.GetEvent(ctx, settled); err != nil || !held {
		t.Fatalf("read the settled fact after the refused pass answered (%v, held=%v), want the hot journal to still answer it", err, held)
	} else if !bytes.Equal(stored.Body, bytes.Repeat([]byte("settled-material "), 64)) {
		t.Fatalf("the settled fact reads back %q after the refused pass, want the bytes it was committed with", stored.Body)
	}
}

// TestColdArchiveRefusesAnUnavailableColdRoot pins the named refusal of a relocation a store cannot express.
//   - a deployment that declared retirement never reads a silent no-op as a bounded journal
func TestColdArchiveRefusesAnUnavailableColdRoot(t *testing.T) {
	ctx := context.Background()
	tier, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: t.TempDir()})
	if err != nil {
		t.Fatalf("open a tier that keeps no cold journal: %v", err)
	}
	defer tier.Segments.Close()
	if _, err := tier.Segments.RetireFacts(ctx, map[event.EventID]struct{}{}); !errors.Is(err, ErrColdArchiveUnavailable) {
		t.Fatalf("a store beside no cold journal answered %v, want the missing cold journal named", err)
	}
}

// commitFactWithOriginal commits one fact beside the original bytes a runtime
// keeps for it, so the two families a production journal carries are both in the
// fixture.
func commitFactWithOriginal(t *testing.T, ctx context.Context, tier *Tier, key event.EventID, seq int64, typ string, body, original []byte) {
	t.Helper()
	owner := event.Address{AgentID: "agent-a", SessionID: "session-cold"}
	record := SegmentRecord{
		PartitionID: 1,
		WindowTS:    1,
		Seq:         seq,
		EventKey:    key,
		EventTime:   seq,
		Body:        body,
	}.WithChain(typ, "session-cold")
	if err := tier.Segments.StoreEvent(ctx, record); err != nil {
		t.Fatalf("commit the fact %s: %v", key, err)
	}
	if err := tier.Originals.Put(ctx, OriginalRef{
		Key:             key,
		Owner:           owner,
		Format:          "text",
		RetentionPolicy: "default",
	}, original); err != nil {
		t.Fatalf("commit the original of %s: %v", key, err)
	}
}

// TestRelocatedFactReadsByIdentity pins the first red line of a relocation: a settled fact answers what it was committed with.
//   - positive: the fact and the original answer their committed bytes by identity after the pass and after a restart
//   - positive: the hot journal holds none of the three keys the settled fact owned, and a repeat lands no second fact
//   - negative: a pass that retired the segment family alone would leave the reference and the body hot, which the key set names
func TestRelocatedFactReadsByIdentity(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	tier, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: root, RetireFacts: true})
	if err != nil {
		t.Fatalf("open the journal tier: %v", err)
	}
	factBody := bytes.Repeat([]byte("identity-material "), 32)
	originalBody := bytes.Repeat([]byte("identity-original "), 64)
	settled := event.EventID("identity-settled-fact")
	live := event.EventID("identity-live-fold")
	commitFactWithOriginal(t, ctx, tier, settled, 1, "fact", factBody, originalBody)
	commitFactWithOriginal(t, ctx, tier, live, 2, "checkpoint", []byte(`{"fold":true}`), []byte("the fold"))

	before, held, err := tier.Segments.GetEvent(ctx, settled)
	if err != nil || !held {
		t.Fatalf("read the fact before its relocation answered (%v, held=%v)", err, held)
	}
	if _, err := tier.Segments.RetireFacts(ctx, map[event.EventID]struct{}{live: {}}); err != nil {
		t.Fatalf("relocate the settled fact: %v", err)
	}
	hot, err := tier.Store.Keys(ctx, "")
	if err != nil {
		t.Fatalf("enumerate the hot journal after the pass: %v", err)
	}
	for _, key := range hot {
		if key == "1:idx:"+string(settled) || key == "blob:"+string(settled) || key == "ref:"+string(settled) {
			t.Fatalf("the hot journal still holds %q after the pass, want the segment and original families of a settled fact cold", key)
		}
	}
	answers := func(tier *Tier, where string) {
		t.Helper()
		stored, held, err := tier.Segments.GetEvent(ctx, settled)
		if err != nil {
			t.Fatalf("read the settled fact %s by identity %s: %v", settled, where, err)
		}
		if !held {
			t.Fatalf("the settled fact %s answers missing %s, want the bytes it was committed with", settled, where)
		}
		if !bytes.Equal(stored.Body, factBody) {
			t.Fatalf("the settled fact %s answers %q %s, want the bytes %q it was committed with", settled, stored.Body, where, factBody)
		}
		if stored.PartitionID != before.PartitionID || stored.WindowTS != before.WindowTS || stored.Seq != before.Seq {
			t.Fatalf("the settled fact %s answers position %d/%d/%d %s, want the %d/%d/%d it was committed at",
				settled, stored.PartitionID, stored.WindowTS, stored.Seq, where, before.PartitionID, before.WindowTS, before.Seq)
		}
		original := tier.Originals.Get(ctx, settled)
		if original.Outcome != RecallFound {
			t.Fatalf("the original of %s reads as %v %s, want the bytes it was committed with", settled, original, where)
		}
		if !bytes.Equal(original.Body, originalBody) {
			t.Fatalf("the original of %s answers %q %s, want the bytes %q it was committed with", settled, original.Body, where, originalBody)
		}
	}
	answers(tier, "in the process that relocated it")
	if err := tier.Segments.StoreEvent(ctx, SegmentRecord{
		PartitionID: 1,
		WindowTS:    1,
		Seq:         1,
		EventKey:    settled,
		EventTime:   1,
		Body:        factBody,
	}.WithChain("fact", "session-cold")); err != nil {
		t.Fatalf("a commit repeating the settled fact %s answered %v, want the duplicate it is", settled, err)
	}
	if err := tier.Segments.StoreEvent(ctx, SegmentRecord{
		PartitionID: 1,
		WindowTS:    1,
		Seq:         1,
		EventKey:    settled,
		EventTime:   1,
		Body:        []byte("other material"),
	}.WithChain("fact", "session-cold")); !errors.Is(err, ErrContentConflict) {
		t.Fatalf("a commit carrying other bytes under the settled identity %s answered %v, want the conflict named", settled, err)
	}
	if count := tier.Segments.EventCount(); count != 2 {
		t.Fatalf("the store states %d committed event(s) after the repeats, want the 2 it committed: a repeat lands no second fact", count)
	}

	if err := tier.Segments.Close(); err != nil {
		t.Fatalf("close the tier before the restart: %v", err)
	}
	reopened, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("reopen the tier over the relocated root: %v", err)
	}
	defer reopened.Segments.Close()
	answers(reopened, "after a restart that opened no pass")
}

// TestColdArchiveRefusesCorruptColdTier pins the second red line of a relocation: a cold journal that cannot be read back is refused by name.
//   - positive: a damaged frame of the cold journal refuses the read by name, and so does the original the fact owns
//   - negative: a read that reported the settled fact missing would let a deployment read a corrupt store as one that kept fewer facts
func TestColdArchiveRefusesCorruptColdTier(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	tier, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: root, RetireFacts: true})
	if err != nil {
		t.Fatalf("open the journal tier: %v", err)
	}
	settled := event.EventID("corrupt-cold-fact")
	live := event.EventID("corrupt-cold-fold")
	commitFactWithOriginal(t, ctx, tier, settled, 1, "fact", bytes.Repeat([]byte("corrupt-material "), 32), []byte("the original"))
	commitFactWithOriginal(t, ctx, tier, live, 2, "checkpoint", []byte(`{"fold":true}`), []byte("the fold"))
	if _, err := tier.Segments.RetireFacts(ctx, map[event.EventID]struct{}{live: {}}); err != nil {
		t.Fatalf("relocate the settled fact: %v", err)
	}
	if err := tier.Segments.Close(); err != nil {
		t.Fatalf("close the tier: %v", err)
	}

	path := filepath.Join(root, coldDirName, kvDirName, journalFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the cold journal: %v", err)
	}
	damaged := append([]byte(nil), raw...)
	damaged[len(damaged)-1] ^= 0xff
	if err := os.WriteFile(path, damaged, 0o600); err != nil {
		t.Fatalf("damage the cold journal: %v", err)
	}

	reopened, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("reopen the tier over the damaged cold root: %v", err)
	}
	defer reopened.Segments.Close()
	if _, held, err := reopened.Segments.GetEvent(ctx, settled); !errors.Is(err, ErrJournalCorrupt) {
		t.Fatalf("reading a settled fact of a damaged cold journal answered (held=%v, err=%v), want the corruption named", held, err)
	}
	if original := reopened.Originals.Get(ctx, settled); original.Outcome != RecallFailed || !errors.Is(original.Err, ErrJournalCorrupt) {
		t.Fatalf("recalling the original of a settled fact of a damaged cold journal answered %v, want the corruption named", original)
	}
}
