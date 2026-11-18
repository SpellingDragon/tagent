// The barrier one commit reports its success behind: the record it appended is
// durable when the commit returns, so a fact a settled task committed is still
// there for the next process instead of being lost to the next power cut.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
package ledger

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	if err := kv.EachCommittedNewestFirst(ctx, func(key string, _ []byte) (bool, error) {
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
	if err := kv.EachCommittedNewestFirst(ctx, func(string, []byte) (bool, error) {
		stops++
		return false, nil
	}); err != nil {
		t.Fatalf("walk the commit order: %v", err)
	}
	if stops != 1 {
		t.Fatalf("the walk visited %d records after a visit answered false, want the one it stopped at", stops)
	}
}

// TestJournalReadThroughAClosedViewStatesTheClosedView pins the answer a view gives after Close ended it.
//   - a read of a value the resident set does not hold is refused as ErrReadAfterClose and never as a body the log cannot answer
//   - the key enumeration and the namespace enumeration of that view answer the same named refusal, never an empty store
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalReadThroughAClosedViewStatesTheClosedView(t *testing.T) {
	ctx := context.Background()
	kv, err := openJournalKV(t.TempDir())
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	key := "0:idx:closed"
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: key, Value: []byte("body")}}}); err != nil {
		t.Fatalf("commit %s: %v", key, err)
	}
	kv.resident.remove(key)
	if err := kv.Close(); err != nil {
		t.Fatalf("close the view: %v", err)
	}
	_, err = kv.Read(ctx, Fetch{Key: key})
	if !errors.Is(err, ErrReadAfterClose) {
		t.Fatalf("the read of %s beside a closed view = %v, want %v", key, err, ErrReadAfterClose)
	}
	if errors.Is(err, ErrSegmentBodyMissing) {
		t.Fatalf("the read of %s beside a closed view answered %v, want the closed view and not a missing body", key, ErrSegmentBodyMissing)
	}
	if _, err := kv.Keys(ctx, "0:"); !errors.Is(err, ErrReadAfterClose) {
		t.Fatalf("the key enumeration beside a closed view = %v, want %v", err, ErrReadAfterClose)
	}
	if _, err := kv.Namespaces(ctx); !errors.Is(err, ErrReadAfterClose) {
		t.Fatalf("the namespace enumeration beside a closed view = %v, want %v", err, ErrReadAfterClose)
	}
}

// TestJournalCommitThroughAReadFaceIsRefusedByName pins the refusal a commit carrying keys meets through a view that declared no write rights.
//   - the commit answers ErrCommitWithoutWriteRights and never ErrCommitUncertain, the view holds no key afterwards, and the fresh root holds no log file
//   - a deletes-only batch meets the same refusal, so the read face appends nothing in either form
//   - beside a live writer, a second write-rights open answers ErrRootWriteLocked and the read view's commit stays refused: the lock admits one writer
//   - the read view answers the fact the writer committed, and the writer commits again after the refusal
//   - a writing open of the fresh root lands and reads back the fact the refused commits never wrote
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalCommitThroughAReadFaceIsRefusedByName(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	view, err := openJournalView(dir, false)
	if err != nil {
		t.Fatalf("open the reading view of a fresh root: %v", err)
	}
	err = view.Commit(ctx, Batch{Puts: []Pair{{Key: journalFactKey, Value: []byte("the settled fact")}}})
	if !errors.Is(err, ErrCommitWithoutWriteRights) {
		t.Fatalf("commit through the reading view = %v, want %v", err, ErrCommitWithoutWriteRights)
	}
	if errors.Is(err, ErrCommitUncertain) {
		t.Fatalf("commit through the reading view answered %v, want the refusal decided before any append", err)
	}
	if err := view.Commit(ctx, Batch{Deletes: []string{journalFactKey}}); !errors.Is(err, ErrCommitWithoutWriteRights) {
		t.Fatalf("deletes-only commit through the reading view = %v, want %v", err, ErrCommitWithoutWriteRights)
	}
	if _, held := readOne(t, view, journalFactKey); held {
		t.Fatalf("the reading view holds %s after its refused commits, want no key", journalFactKey)
	}
	if _, err := os.Stat(filepath.Join(dir, journalFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat of the log beside the refused commits = %v, want no file the read face never creates", err)
	}
	if err := view.Close(); err != nil {
		t.Fatalf("close the reading view: %v", err)
	}
	writer, err := openJournalKV(dir)
	if err != nil {
		t.Fatalf("open the writing view of the fresh root: %v", err)
	}
	if err := writer.Commit(ctx, Batch{Puts: []Pair{{Key: journalFactKey, Value: []byte("the settled fact")}}}); err != nil {
		t.Fatalf("commit through the writing view: %v", err)
	}
	value, held := readOne(t, writer, journalFactKey)
	if !held || !bytes.Equal(value, []byte("the settled fact")) {
		t.Fatalf("the writing view read (%q, %v), want the fact it committed", value, held)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close the writing view: %v", err)
	}

	root := t.TempDir()
	live, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the writer holding the root: %v", err)
	}
	defer live.(interface{ Close() error }).Close()
	if err := live.Commit(ctx, Batch{Puts: []Pair{{Key: journalFactKey, Value: []byte("the settled fact")}}}); err != nil {
		t.Fatalf("commit through the writer holding the root: %v", err)
	}
	if _, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: root, WriteRights: true}); !errors.Is(err, ErrRootWriteLocked) {
		t.Fatalf("second write-rights open beside the live writer = %v, want %v", err, ErrRootWriteLocked)
	}
	reader, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("open the read view beside the live writer: %v", err)
	}
	defer reader.(interface{ Close() error }).Close()
	if err := reader.Commit(ctx, Batch{Puts: []Pair{{Key: journalFactKey, Value: []byte("a second writer's fact")}}}); !errors.Is(err, ErrCommitWithoutWriteRights) {
		t.Fatalf("commit through the read view beside the live writer = %v, want %v: the lock admits one writer and the read view never bypasses it", err, ErrCommitWithoutWriteRights)
	}
	value, held = readOne(t, reader, journalFactKey)
	if !held || !bytes.Equal(value, []byte("the settled fact")) {
		t.Fatalf("the read view read (%q, %v) beside its writer, want the fact the writer committed", value, held)
	}
	if err := live.Commit(ctx, Batch{Puts: []Pair{{Key: journalFactKey, Value: []byte("the writer's second fact")}}}); err != nil {
		t.Fatalf("the writer's commit after the read view's refusal = %v, want the refusal to leave the writer's path whole", err)
	}
}

// TestJournalStatesItsKeyOrderWithoutReadingValues pins the key-order face beside the value one.
//   - the key walk names the keys the value walk names, newest first, once per key, and skips a key its newest commit deleted
//   - the key walk reads no value, and the value walk reads one per key it names, so the measure tells the two faces apart
//   - a visit that answers false ends the key walk where it stands
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalStatesItsKeyOrderWithoutReadingValues(t *testing.T) {
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
	want := []string{"1:idx:newer", "1:evt:1:1", "1:idx:older"}
	var walked []string
	before := kv.ValueReads()
	if err := kv.EachCommittedKeyNewestFirst(ctx, func(key string) (bool, error) {
		walked = append(walked, key)
		return true, nil
	}); err != nil {
		t.Fatalf("walk the commit order by key: %v", err)
	}
	if strings.Join(walked, ",") != strings.Join(want, ",") {
		t.Fatalf("the key walk names %v, want %v: the newest commit first and no key the log deleted", walked, want)
	}
	if reads := kv.ValueReads() - before; reads != 0 {
		t.Fatalf("the key walk read %d value(s) of the %d key(s) it named, want none: the order file states the keys and the spans are read by the caller", reads, len(want))
	}
	before = kv.ValueReads()
	named := 0
	if err := kv.EachCommittedNewestFirst(ctx, func(string, []byte) (bool, error) {
		named++
		return true, nil
	}); err != nil {
		t.Fatalf("walk the commit order beside its values: %v", err)
	}
	if reads := kv.ValueReads() - before; reads != int64(named) || named != len(want) {
		t.Fatalf("the value walk read %d value(s) of the %d key(s) it named, want one each: the two faces are told apart by the values they read", reads, named)
	}
	stops := 0
	if err := kv.EachCommittedKeyNewestFirst(ctx, func(string) (bool, error) {
		stops++
		return false, nil
	}); err != nil {
		t.Fatalf("walk the commit order by key: %v", err)
	}
	if stops != 1 {
		t.Fatalf("the key walk visited %d key(s) after a visit answered false, want the one it stopped at", stops)
	}
}
