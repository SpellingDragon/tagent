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
