// The barrier one commit reports its success behind: the record it appended is
// durable when the commit returns, so a fact a settled task committed is still
// there for the next process instead of being lost to the next power cut.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
package ledger

import (
	"bytes"
	"context"
	"testing"
)

// journalFactKey names one fact a commit carries in these checks.
const journalFactKey = "tagent/task/v1/fact:settled"

// TestJournalCommitCarriesItsOwnBarrier pins the barrier one commit completes.
//   - a commit leaves nothing unsynced, so its success names a durable record rather than bytes the next process may lose
//   - the fact a commit returned success for reads back after the log is reopened, with no separate barrier in between
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalCommitCarriesItsOwnBarrier(t *testing.T) {
	ctx := context.Background()

	t.Run("commit_leaves_nothing_unsynced", func(t *testing.T) {
		kv, err := openJournalKV(t.TempDir())
		if err != nil {
			t.Fatalf("open the journal view: %v", err)
		}
		t.Cleanup(func() { kv.log.close() })
		if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: journalFactKey, Value: []byte("the settled fact")}}}); err != nil {
			t.Fatalf("commit the fact: %v", err)
		}
		kv.log.mu.Lock()
		unsynced := kv.log.unsynced
		kv.log.mu.Unlock()
		if unsynced != 0 {
			t.Fatalf("the commit returned success with %d byte(s) unsynced, want the barrier it reported success behind", unsynced)
		}
	})

	t.Run("fact_reads_back_after_reopen", func(t *testing.T) {
		dir := t.TempDir()
		kv, err := openJournalKV(dir)
		if err != nil {
			t.Fatalf("open the journal view: %v", err)
		}
		if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: journalFactKey, Value: []byte("the settled fact")}}}); err != nil {
			t.Fatalf("commit the fact: %v", err)
		}
		if err := kv.log.close(); err != nil {
			t.Fatalf("close the journal view: %v", err)
		}
		reopened, err := openJournalKV(dir)
		if err != nil {
			t.Fatalf("reopen the journal view: %v", err)
		}
		t.Cleanup(func() { reopened.log.close() })
		value, held := readOne(t, reopened, journalFactKey)
		if !held || !bytes.Equal(value, []byte("the settled fact")) {
			t.Fatalf("the reopened view read (%q, %v), want the fact the commit returned success for", value, held)
		}
	})
}

// TestJournalStatesItsCommitOrderNewestFirst pins the order the log states.
//   - the walk names every committed record from the newest commit to the oldest
//   - a visit that answers false ends the walk, so a lookup reads the commits filed after it
//   - a key a later commit deleted is not held and is skipped
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalStatesItsCommitOrderNewestFirst(t *testing.T) {
	ctx := context.Background()
	kv, err := openJournalKV(t.TempDir())
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	t.Cleanup(func() { kv.log.close() })
	for _, batch := range []Batch{
		{Puts: []Pair{{Key: "1:idx:older", Value: []byte("old")}}},
		{Puts: []Pair{{Key: "1:evt:1:1", Value: []byte("body")}, {Key: "1:idx:newer", Value: []byte("new")}}},
		{Puts: []Pair{{Key: "1:idx:deleted", Value: []byte("gone")}}},
		{Deletes: []string{"1:idx:deleted"}},
	} {
		if err := kv.Commit(ctx, batch); err != nil {
			t.Fatalf("commit %v: %v", batch, err)
		}
	}
	var walked []string
	if err := kv.eachCommittedNewestFirst(ctx, func(key string) (bool, error) {
		walked = append(walked, key)
		return true, nil
	}); err != nil {
		t.Fatalf("walk the commit order: %v", err)
	}
	want := []string{"1:idx:newer", "1:evt:1:1", "1:idx:older"}
	if len(walked) != len(want) {
		t.Fatalf("the walk names %v, want %v: the newest commit first and no key the log deleted", walked, want)
	}
	for at := range want {
		if walked[at] != want[at] {
			t.Fatalf("the walk names %v, want %v: the newest commit first and no key the log deleted", walked, want)
		}
	}
	stops := 0
	if err := kv.eachCommittedNewestFirst(ctx, func(string) (bool, error) {
		stops++
		return false, nil
	}); err != nil {
		t.Fatalf("walk the commit order: %v", err)
	}
	if stops != 1 {
		t.Fatalf("the walk visited %d records after a visit answered false, want the one it stopped at", stops)
	}
}
