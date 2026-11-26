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
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
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

// openCutArchiveViewForTest opens one archive-backed writer after two cuts, so
// the oldest sealed interval is eligible and the newest one stays hot.
func openCutArchiveViewForTest(t *testing.T, age time.Duration) (*journalKV, string, string) {
	t.Helper()
	hotDir, archiveRoot := t.TempDir(), t.TempDir()
	kv, err := openJournalViewWithArchive(hotDir, true, segmentTestBudget, archiveRoot, age)
	if err != nil {
		t.Fatalf("open the archive-backed journal view: %v", err)
	}
	t.Cleanup(func() { kv.Close() })
	ctx := context.Background()
	for _, pair := range []Pair{
		{Key: "archived-key", Value: []byte("archived-value")},
		{Key: "checkpoint:1:1", Value: []byte("cut-one")},
		{Key: "latest-key", Value: []byte("latest-value")},
		{Key: "checkpoint:1:2", Value: []byte("cut-two")},
	} {
		if err := kv.Commit(ctx, Batch{Puts: []Pair{pair}}); err != nil {
			t.Fatalf("commit %q: %v", pair.Key, err)
		}
	}
	return kv, hotDir, archiveRoot
}

// TestCutCommitMaintainsLiveJournal pins live maintenance after a cut barrier.
//   - age alone leaves the log untouched, and an eligible maintenance call moves only the oldest sealed interval
//   - the same view commits a new hot key and reads the moved key through the archive without a resident second copy
//   - a reopen reads the moved, latest and new keys with the same bytes
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestCutCommitMaintainsLiveJournal(t *testing.T) {
	kv, hotDir, archiveRoot := openCutArchiveViewForTest(t, time.Hour)
	ctx := context.Background()
	original := kv.log
	beforeSize := fileSizeForTest(t, filepath.Join(hotDir, journalFileName))
	sealed := readSealedSegmentsForTest(t, hotDir)
	wantWindow := hotWindowAfterMoveForTest(t, hotDir, sealed[:1], sealed[1:])
	kv.mu.Lock()
	gotWindow, windowErr := archiveWatermarkAfterMove(kv.log, sealed[:1])
	kv.mu.Unlock()
	if windowErr != nil || gotWindow != wantWindow {
		t.Fatalf("the live hot window = %+v err=%v, want %+v", gotWindow, windowErr, wantWindow)
	}
	kv.mu.Lock()
	err := kv.maintainAfterCutLocked(time.Now())
	kv.mu.Unlock()
	if err != nil {
		t.Fatalf("maintain before the age window: %v", err)
	}
	if kv.log != original || fileSizeForTest(t, filepath.Join(hotDir, journalFileName)) != beforeSize {
		t.Fatalf("age alone replaced the log or changed its bytes")
	}
	if _, found, err := readArchiveManifest(archiveRoot); err != nil || found {
		t.Fatalf("the young archive states found=%v err=%v, want no manifest", found, err)
	}

	kv.mu.Lock()
	err = kv.maintainAfterCutLocked(time.Now().Add(2 * time.Hour))
	kv.mu.Unlock()
	if err != nil {
		t.Fatalf("maintain the eligible cut: %v", err)
	}
	if kv.log == original {
		t.Fatalf("maintenance kept the closed log handle")
	}
	if got := fileSizeForTest(t, filepath.Join(hotDir, journalFileName)); got >= beforeSize {
		t.Fatalf("maintenance left %d log byte(s) beside the %d before the move", got, beforeSize)
	}
	manifest, found, err := readArchiveManifest(archiveRoot)
	if err != nil || !found || len(manifest.Segments) != 1 || manifest.Segments[0].SegmentID != journalSegmentLabelOf(1) {
		t.Fatalf("the archive states found=%v segments=%+v err=%v, want only the oldest interval", found, manifest, err)
	}
	segments := readSealedSegmentsForTest(t, hotDir)
	if len(segments) != 1 || segments[0].id != 2 {
		t.Fatalf("the hot root keeps %+v, want only the newest cut interval", segments)
	}
	kv.mu.Lock()
	_, resident := kv.resident.get("archived-key")
	_, hot, hotErr := kv.log.read("archived-key")
	verified := len(kv.archive.verified)
	kv.mu.Unlock()
	if resident || hot || hotErr != nil {
		t.Fatalf("the moved key remains resident=%t hot=%t err=%v, want only its archive statement", resident, hot, hotErr)
	}
	if verified != 0 {
		t.Fatalf("maintenance retained %d verified archive digest(s)", verified)
	}
	value, held := readOne(t, kv, "archived-key")
	if !held || string(value) != "archived-value" {
		t.Fatalf("the live archive read = (%q, %t), want (archived-value, true)", value, held)
	}
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "new-hot", Value: []byte("new-value")}}}); err != nil {
		t.Fatalf("commit through the maintained view: %v", err)
	}
	kv.mu.Lock()
	value, held, err = kv.log.read("new-hot")
	kv.mu.Unlock()
	if err != nil || !held || string(value) != "new-value" {
		t.Fatalf("the new key hot read = (%q, %t, %v), want (new-value, true, nil)", value, held, err)
	}
	keys, err := kv.Keys(ctx, "")
	if err != nil || !equalStringSets(keys, []string{"archived-key", "checkpoint:1:1", "latest-key", "checkpoint:1:2", "new-hot"}) {
		t.Fatalf("the maintained keys = %v err=%v, want the keys of the archived interval, the newest interval and the open segment", keys, err)
	}
	if err := kv.Close(); err != nil {
		t.Fatalf("close the maintained view: %v", err)
	}
	reopened, err := openJournalViewWithArchive(hotDir, true, segmentTestBudget, archiveRoot, time.Hour)
	if err != nil {
		t.Fatalf("reopen the maintained root: %v", err)
	}
	t.Cleanup(func() { reopened.Close() })
	for key, want := range map[string]string{"archived-key": "archived-value", "latest-key": "latest-value", "new-hot": "new-value"} {
		value, held := readOne(t, reopened, key)
		if !held || string(value) != want {
			t.Fatalf("the reopened read of %q = (%q, %t), want (%q, true)", key, value, held, want)
		}
	}
}

// TestJournalMaintenanceRecoveryKeepsViewUsable pins a recovered move failure.
//   - the failed maintenance reports ErrCommitUncertain after the cut barrier
//   - structural recovery reopens the unchanged hot log and the same view commits again
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalMaintenanceRecoveryKeepsViewUsable(t *testing.T) {
	kv, _, archiveRoot := openCutArchiveViewForTest(t, 0)
	if err := os.WriteFile(filepath.Join(archiveRoot, journalSegmentDirName), []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("block the archive segment directory: %v", err)
	}
	kv.mu.Lock()
	err := kv.maintainAfterCutLocked(time.Now())
	kv.mu.Unlock()
	if !errors.Is(err, ErrCommitUncertain) || errors.Is(err, ErrJournalMaintenanceFailed) {
		t.Fatalf("the recovered maintenance error = %v, want ErrCommitUncertain alone", err)
	}
	if kv.maintenanceErr != nil || kv.log == nil {
		t.Fatalf("the recovered view remains unavailable: err=%v log=%v", kv.maintenanceErr, kv.log)
	}
	if err := kv.Commit(context.Background(), Batch{Puts: []Pair{{Key: "after-recovery", Value: []byte("value")}}}); err != nil {
		t.Fatalf("commit through the recovered view: %v", err)
	}
	value, held := readOne(t, kv, "after-recovery")
	if !held || string(value) != "value" {
		t.Fatalf("the recovered view read = (%q, %t), want (value, true)", value, held)
	}
}

// TestJournalMaintenanceFailureRefusesFurtherWork pins an unrecoverable view.
//   - the maintenance error names both ErrCommitUncertain and ErrJournalMaintenanceFailed
//   - later commits and reads refuse the view without appending bytes, and Close still releases its handles
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalMaintenanceFailureRefusesFurtherWork(t *testing.T) {
	kv, hotDir, archiveRoot := openCutArchiveViewForTest(t, 0)
	for _, name := range []string{archiveManifestAName, archiveManifestBName} {
		if err := os.WriteFile(filepath.Join(archiveRoot, name), []byte("no generation"), 0o600); err != nil {
			t.Fatalf("damage the manifest slot %s: %v", name, err)
		}
	}
	kv.mu.Lock()
	err := kv.maintainAfterCutLocked(time.Now())
	kv.mu.Unlock()
	if !errors.Is(err, ErrCommitUncertain) || !errors.Is(err, ErrJournalMaintenanceFailed) {
		t.Fatalf("the unrecovered maintenance error = %v, want both named failures", err)
	}
	before := fileSizeForTest(t, filepath.Join(hotDir, journalFileName))
	if err := kv.Commit(context.Background(), Batch{Puts: []Pair{{Key: "refused", Value: []byte("value")}}}); !errors.Is(err, ErrJournalMaintenanceFailed) {
		t.Fatalf("the commit after failed maintenance = %v, want ErrJournalMaintenanceFailed", err)
	}
	if _, err := kv.Read(context.Background(), Fetch{Key: "latest-key"}); !errors.Is(err, ErrJournalMaintenanceFailed) {
		t.Fatalf("the read after failed maintenance = %v, want ErrJournalMaintenanceFailed", err)
	}
	if got := fileSizeForTest(t, filepath.Join(hotDir, journalFileName)); got != before {
		t.Fatalf("the refused commit left %d log byte(s) beside the %d it met", got, before)
	}
	if err := kv.Close(); err != nil {
		t.Fatalf("close the unavailable view: %v", err)
	}
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
	if err := kv.eachCommittedNewestFirst(ctx, func(key string) (bool, error) {
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
	if err := kv.eachCommittedNewestFirst(ctx, func(string) (bool, error) {
		stops++
		return false, nil
	}); err != nil {
		t.Fatalf("walk the commit order by key: %v", err)
	}
	if stops != 1 {
		t.Fatalf("the key walk visited %d key(s) after a visit answered false, want the one it stopped at", stops)
	}
}

// TestCloseBarrierFailureStaysVisible pins the failure the close of a view reports when its index barrier does not complete.
//   - a view whose index bytes stand staged answers the barrier's own failure at Close, so a stop the process cannot confirm is named
//   - that failure is the barrier's and not a refusal decided before a write: neither the closed view nor one holding no write rights answers
//   - the store's close reports a barrier that failed over the slot table the same way, so both index files' barriers reach the caller
//   - a close over nothing staged takes no barrier and answers nil, and it states that it owed none, so the nil is a reading and not a swallow
//   - the facts the commits landed read back from the root after the failed close, so the failure is the barrier over the derived files
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestCloseBarrierFailureStaysVisible(t *testing.T) {
	ctx := context.Background()

	t.Run("a_failed_index_barrier_is_reported_by_the_views_close", func(t *testing.T) {
		dir := t.TempDir()
		kv, err := openJournalKV(dir)
		if err != nil {
			t.Fatalf("open the journal view: %v", err)
		}
		batch := Batch{Puts: []Pair{{Key: journalFactKey, Value: []byte("the settled fact")}}}
		if err := kv.Commit(ctx, batch); err != nil {
			t.Fatalf("commit the fact: %v", err)
		}
		if staged := journalBarrierStateOf(kv).indexStaged; staged <= 0 {
			t.Fatalf("the view closes with %d index byte(s) staged, want the bytes a barrier has yet to confirm", staged)
		}
		writer := journalFailingBarrier(t)
		kv.mu.Lock()
		previous := kv.log.addr.order
		kv.log.addr.order = writer
		kv.mu.Unlock()
		err = kv.Close()
		kv.mu.Lock()
		kv.log.addr.order = previous
		kv.mu.Unlock()
		if !errors.Is(err, ErrJournalBarrier) {
			t.Fatalf("close beside an index barrier that did not complete = %v, want %v", err, ErrJournalBarrier)
		}
		if errors.Is(err, ErrCommitAfterClose) || errors.Is(err, ErrCommitWithoutWriteRights) {
			t.Fatalf("close beside a failed index barrier answered %v, want the barrier's own failure and not a refusal decided before a write", err)
		}
		reopened, err := openJournalKV(dir)
		if err != nil {
			t.Fatalf("reopen the root the failed close released: %v", err)
		}
		t.Cleanup(func() { reopened.log.close() })
		value, held := readOne(t, reopened, journalFactKey)
		if !held || !bytes.Equal(value, []byte("the settled fact")) {
			t.Fatalf("the reopened view read (%q, %v), want the fact the commit returned success for: the barrier that failed is the derived files' and never the log's", value, held)
		}
	})

	t.Run("a_failed_slot_barrier_is_reported_by_the_stores_close", func(t *testing.T) {
		kv, err := openJournalKV(t.TempDir())
		if err != nil {
			t.Fatalf("open the journal view: %v", err)
		}
		store, err := newFileSegmentStore(kv)
		if err != nil {
			t.Fatalf("open the store over the view: %v", err)
		}
		batch := Batch{Puts: []Pair{{Key: journalFactKey, Value: []byte("the settled fact")}}}
		if err := kv.Commit(ctx, batch); err != nil {
			t.Fatalf("commit the fact: %v", err)
		}
		if staged := journalBarrierStateOf(kv).indexStaged; staged <= 0 {
			t.Fatalf("the store closes with %d index byte(s) staged, want the bytes a barrier has yet to confirm", staged)
		}
		writer := journalFailingBarrier(t)
		kv.mu.Lock()
		previous := kv.log.addr.slots
		kv.log.addr.slots = writer
		kv.mu.Unlock()
		err = store.Close()
		kv.mu.Lock()
		kv.log.addr.slots = previous
		kv.mu.Unlock()
		if !errors.Is(err, ErrJournalBarrier) {
			t.Fatalf("close the store beside a slot barrier that did not complete = %v, want %v", err, ErrJournalBarrier)
		}
		if errors.Is(err, ErrCommitAfterClose) || errors.Is(err, ErrCommitWithoutWriteRights) {
			t.Fatalf("close the store beside a failed slot barrier answered %v, want the barrier's own failure and not a refusal decided before a write", err)
		}
	})

	t.Run("a_close_over_nothing_staged_takes_no_barrier", func(t *testing.T) {
		kv, err := openJournalKV(t.TempDir())
		if err != nil {
			t.Fatalf("open the journal view: %v", err)
		}
		owed := journalBarrierStateOf(kv)
		if owed.indexStaged != 0 || owed.indexStagedLog != 0 || owed.logUnsynced != 0 {
			t.Fatalf("the view closes owing %d index byte(s), %d staged log byte(s) and %d unsynced log byte(s), want a view that owes no barrier",
				owed.indexStaged, owed.indexStagedLog, owed.logUnsynced)
		}
		if err := kv.Close(); err != nil {
			t.Fatalf("close a view that owes no barrier = %v, want a clean close", err)
		}
	})
}

// TestSealFailureKeepsCommittedValuePublished pins the value a barrier already confirmed.
//   - a seal failure after sync reports ErrCommitUncertain, because the boundary was not staked
//   - the committed value stays published in the same view, because the fact itself landed before the seal
func TestSealFailureKeepsCommittedValuePublished(t *testing.T) {
	dir := t.TempDir()
	kv, err := openJournalViewFace(dir, true, segmentTestBudget)
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	t.Cleanup(func() { kv.Close() })
	blocked := filepath.Join(dir, journalSegmentDirName)
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("block the segment directory: %v", err)
	}
	err = kv.Commit(context.Background(), Batch{Puts: []Pair{
		{Key: journalFactKey, Value: []byte("landed")},
		{Key: "checkpoint:1:1", Value: []byte("cut")},
	}})
	if !errors.Is(err, ErrCommitUncertain) {
		t.Fatalf("commit beside a failed seal = %v, want ErrCommitUncertain", err)
	}
	value, held := readOne(t, kv, journalFactKey)
	if !held || string(value) != "landed" {
		t.Fatalf("the view reads (%q, %t) after the failed seal, want the value its barrier published", value, held)
	}
}

// archivedEnumerationViewForTest opens one archive-backed writer and commits three
// intervals of records behind their real cuts, then moves the two oldest intervals into
// the archive through the maintenance pass a committing cut runs. The commits, oldest
// first, leave the four deletion states an enumeration has to tell apart:
//   - 1:evt:a1 is rewritten in a later archived interval and again in the open segment
//   - 1:evt:a2 is put in the oldest interval and deleted in the next, both archived
//   - 1:evt:a3 is deleted in an archived interval and put again in the open segment
//   - 1:meta:1 is archived and deleted in the open segment, and 1:meta:2 is archived and deleted at the newest interval's cut
func archivedEnumerationViewForTest(t *testing.T) (*journalKV, string, string) {
	t.Helper()
	hotDir, archiveRoot := t.TempDir(), t.TempDir()
	kv, err := openJournalViewWithArchive(hotDir, true, segmentTestBudget, archiveRoot, 0)
	if err != nil {
		t.Fatalf("open the archive-backed journal view: %v", err)
	}
	t.Cleanup(func() { kv.Close() })
	commit := func(batch Batch, what string) {
		t.Helper()
		if err := kv.Commit(context.Background(), batch); err != nil {
			t.Fatalf("commit the %s: %v", what, err)
		}
	}
	commit(Batch{Puts: []Pair{
		{Key: "1:evt:a1", Value: []byte("body-a1")},
		{Key: "1:idx:a1", Value: []byte("pos-a1")},
		{Key: "1:meta:1", Value: []byte("meta-1")},
		{Key: "1:evt:a2", Value: []byte("body-a2")},
		{Key: "1:idx:b2", Value: []byte("pos-b2")},
		{Key: "1:idx:b1", Value: []byte("pos-b1")},
		{Key: "1:idx:c1", Value: []byte("pos-c1")},
		{Key: "1:idx:a2", Value: []byte("pos-a2")},
		{Key: "1:evt:a3", Value: []byte("body-a3")},
		{Key: "1:meta:2", Value: []byte("meta-2-archived")},
		{Key: "3:evt:z1", Value: []byte("body-z1")},
		{Key: "3:idx:z1", Value: []byte("pos-z1")},
	}}, "oldest interval")
	commit(Batch{Puts: []Pair{{Key: "checkpoint:1:1", Value: []byte("cut-one")}}}, "first cut")
	commit(Batch{
		Puts:    []Pair{{Key: "1:evt:a1", Value: []byte("body-a1-newer")}, {Key: "1:idx:c1", Value: []byte("pos-c1-newer")}},
		Deletes: []string{"1:evt:a2", "1:evt:a3"},
	}, "second interval")
	commit(Batch{Puts: []Pair{{Key: "checkpoint:1:2", Value: []byte("cut-two")}}}, "second cut")
	commit(Batch{
		Puts:    []Pair{{Key: "2:evt:h1", Value: []byte("body-h1")}, {Key: "2:idx:h1", Value: []byte("pos-h1")}, {Key: "2:meta:2", Value: []byte("meta-2")}},
		Deletes: []string{"1:meta:2"},
	}, "newest interval")
	commit(Batch{Puts: []Pair{{Key: "checkpoint:1:3", Value: []byte("cut-three")}}}, "third cut")
	commit(Batch{
		Puts:    []Pair{{Key: "1:evt:a1", Value: []byte("body-a1-hot")}, {Key: "1:evt:a3", Value: []byte("body-a3-again")}, {Key: "2:evt:h2", Value: []byte("body-h2")}},
		Deletes: []string{"1:meta:1"},
	}, "open segment")
	kv.mu.Lock()
	err = kv.maintainAfterCutLocked(time.Now())
	kv.mu.Unlock()
	if err != nil {
		t.Fatalf("move the oldest intervals into the archive: %v", err)
	}
	manifest, found, err := readArchiveManifest(archiveRoot)
	if err != nil || !found || len(manifest.Segments) != 2 {
		t.Fatalf("the archive states found=%v segments=%d err=%v, want the two oldest intervals", found, len(manifest.Segments), err)
	}
	return kv, hotDir, archiveRoot
}

// assertArchiveWalkClearedForTest pins the state a finished walk owes its view: no
// pin left on the log and no maintenance still waiting behind the walk.
func assertArchiveWalkClearedForTest(t *testing.T, kv *journalKV) {
	t.Helper()
	kv.mu.Lock()
	active, pending := kv.activeWalks, kv.maintenancePending
	kv.mu.Unlock()
	if active != 0 || pending {
		t.Fatalf("the finished walk left active=%d pending=%t, want both cleared", active, pending)
	}
}

// committedOrderForTest walks one view's commit order and answers the keys it named
// beside the value it answered for each.
func committedOrderForTest(t *testing.T, kv *journalKV) ([]string, map[string]string) {
	t.Helper()
	keys := make([]string, 0, 16)
	values := map[string]string{}
	err := kv.EachCommittedNewestFirst(context.Background(), func(key string, value []byte) (bool, error) {
		keys = append(keys, key)
		values[key] = string(value)
		return true, nil
	})
	if err != nil {
		t.Fatalf("walk the commit order: %v", err)
	}
	return keys, values
}

// TestKeyListingCrossesHotAndArchive pins the key listing over both tiers.
//   - The listing states hot and archived keys in key order.
//   - Archived and hot tombstones both block older values.
//   - A put after a tombstone is held at its newest value.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestKeyListingCrossesHotAndArchive(t *testing.T) {
	ctx := context.Background()
	kv, _, _ := archivedEnumerationViewForTest(t)
	want := []string{
		"1:evt:a1", "1:evt:a3", "1:idx:a1", "1:idx:a2", "1:idx:b1", "1:idx:b2", "1:idx:c1",
		"2:evt:h1", "2:evt:h2", "2:idx:h1", "2:meta:2",
		"3:evt:z1", "3:idx:z1",
		"checkpoint:1:1", "checkpoint:1:2", "checkpoint:1:3",
	}
	keys, err := kv.Keys(ctx, "")
	if err != nil {
		t.Fatalf("list the keys of both tiers: %v", err)
	}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("the key listing = %v, want %v", keys, want)
	}
	archived, err := kv.Keys(ctx, "1:idx:")
	if err != nil || strings.Join(archived, ",") != "1:idx:a1,1:idx:a2,1:idx:b1,1:idx:b2,1:idx:c1" {
		t.Fatalf("the archived prefix listing = %v err=%v, want every key of that family", archived, err)
	}
	if reads := kv.ValueReads(); reads != 0 {
		t.Fatalf("the key listing read %d value span(s), want a listing served from the indexes", reads)
	}
}

// TestNamespaceListingCrossesHotAndArchive pins the namespace listing over both tiers.
//   - the namespace of a partition whose every key the archive holds is still stated
//   - the namespace a hot tombstone emptied is not stated beside the archived value behind it
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestNamespaceListingCrossesHotAndArchive(t *testing.T) {
	ctx := context.Background()
	kv, _, _ := archivedEnumerationViewForTest(t)
	namespaces, err := kv.Namespaces(ctx)
	if err != nil {
		t.Fatalf("list the namespaces of both tiers: %v", err)
	}
	if want := "1,2,3,checkpoint"; strings.Join(namespaces, ",") != want {
		t.Fatalf("the namespace listing = %v, want %s", namespaces, want)
	}
	t.Run("hot tombstone empties a namespace", func(t *testing.T) {
		view, err := openJournalViewWithArchive(t.TempDir(), true, segmentTestBudget, t.TempDir(), 0)
		if err != nil {
			t.Fatalf("open the journal view: %v", err)
		}
		t.Cleanup(func() { view.Close() })
		if err := view.Commit(ctx, Batch{Puts: []Pair{{Key: "ref:only", Value: []byte("value")}}}); err != nil {
			t.Fatalf("commit the only key of the namespace: %v", err)
		}
		if err := view.Commit(ctx, Batch{Deletes: []string{"ref:only"}}); err != nil {
			t.Fatalf("delete the only key of the namespace: %v", err)
		}
		namespaces, err := view.Namespaces(ctx)
		if err != nil || len(namespaces) != 0 {
			t.Fatalf("the namespaces after the tombstone = %v err=%v, want none", namespaces, err)
		}
	})
}

// TestCommittedOrderCrossesHotAndArchive pins the commit order over both tiers.
//   - Hot commits precede archived intervals, newest first, one visit per key.
//   - Archived values are the bytes their commits wrote.
//   - Tombstones are skipped and a later put is visited at its own commit.
//   - Keys in one archived frame follow the hot order's reverse write order.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestCommittedOrderCrossesHotAndArchive(t *testing.T) {
	kv, _, _ := archivedEnumerationViewForTest(t)
	wantKeys := []string{
		"2:evt:h2", "1:evt:a3", "1:evt:a1",
		"checkpoint:1:3",
		"2:meta:2", "2:idx:h1", "2:evt:h1",
		"checkpoint:1:2",
		"1:idx:c1",
		"checkpoint:1:1",
		"3:idx:z1", "3:evt:z1", "1:idx:a2", "1:idx:b1", "1:idx:b2", "1:idx:a1",
	}
	wantValues := map[string]string{
		"2:evt:h2":       "body-h2",
		"1:evt:a3":       "body-a3-again",
		"1:evt:a1":       "body-a1-hot",
		"checkpoint:1:3": "cut-three",
		"2:meta:2":       "meta-2",
		"2:idx:h1":       "pos-h1",
		"2:evt:h1":       "body-h1",
		"checkpoint:1:2": "cut-two",
		"checkpoint:1:1": "cut-one",
		"1:idx:a1":       "pos-a1",
		"1:idx:b2":       "pos-b2",
		"1:idx:b1":       "pos-b1",
		"1:idx:c1":       "pos-c1-newer",
		"1:idx:a2":       "pos-a2",
		"3:evt:z1":       "body-z1",
		"3:idx:z1":       "pos-z1",
	}
	keys, values := committedOrderForTest(t, kv)
	if strings.Join(keys, ",") != strings.Join(wantKeys, ",") {
		t.Fatalf("the commit order = %v, want %v", keys, wantKeys)
	}
	for key, want := range wantValues {
		if values[key] != want {
			t.Fatalf("the walk answers %q of %q, want %q", values[key], key, want)
		}
	}
}

// TestCommittedOrderFromReachesArchivedStatements pins a walk entered at a hot commit.
//   - the walk names the hot commits at and below the given order byte, then the archived intervals behind them
//   - a hot statement above that byte names the key no value of this walk, so the archive answers the key's newest statement at or below the byte
//   - a hot tombstone above that byte hides no archived value, and one at the byte hides the value the older interval states
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestCommittedOrderFromReachesArchivedStatements(t *testing.T) {
	ctx := context.Background()
	kv, _, _ := archivedEnumerationViewForTest(t)
	from, named, err := kv.OrderLineEnd(ctx, "2:idx:h1")
	if err != nil || !named {
		t.Fatalf("locate the commit that last stated 2:idx:h1 = (%d, %t, %v), want its line", from, named, err)
	}
	wantKeys := []string{
		"2:meta:2", "2:idx:h1", "2:evt:h1",
		"checkpoint:1:2",
		"1:idx:c1", "1:evt:a1",
		"checkpoint:1:1",
		"3:idx:z1", "3:evt:z1", "1:idx:a2", "1:idx:b1", "1:idx:b2", "1:meta:1", "1:idx:a1",
	}
	wantValues := map[string]string{
		"1:evt:a1": "body-a1-newer",
		"1:meta:1": "meta-1",
		"1:idx:a1": "pos-a1",
		"1:idx:b2": "pos-b2",
		"1:idx:b1": "pos-b1",
		"1:idx:c1": "pos-c1-newer",
		"1:idx:a2": "pos-a2",
		"3:evt:z1": "body-z1",
		"3:idx:z1": "pos-z1",
	}
	keys := make([]string, 0, len(wantKeys))
	values := map[string]string{}
	err = kv.EachCommittedNewestFirstFrom(ctx, from, func(key string, value []byte) (bool, error) {
		keys = append(keys, key)
		values[key] = string(value)
		return true, nil
	})
	if err != nil {
		t.Fatalf("walk the commit order from the hot commit: %v", err)
	}
	if strings.Join(keys, ",") != strings.Join(wantKeys, ",") {
		t.Fatalf("the commit order from the hot commit = %v, want %v", keys, wantKeys)
	}
	for key, want := range wantValues {
		if values[key] != want {
			t.Fatalf("the walk answers %q of %q, want %q", values[key], key, want)
		}
	}
}

// TestCommittedOrderKeepsItsSnapshotAcrossCallbackWrites pins the cross-tier walk's start state.
//   - a callback commit above the start does not hide an archived value the walk must still name
//   - the callback cut answers success and leaves its maintenance to the walk, and the finished walk clears that pending maintenance
//   - maintenance requested directly during a callback waits for the walk instead of replacing its open log
func TestCommittedOrderKeepsItsSnapshotAcrossCallbackWrites(t *testing.T) {
	ctx := context.Background()
	kv, _, _ := archivedEnumerationViewForTest(t)
	kv.mu.Lock()
	kv.hotLogMaxBytes = 1
	kv.mu.Unlock()
	values := map[string]string{}
	updated := false
	err := kv.EachCommittedNewestFirst(ctx, func(key string, value []byte) (bool, error) {
		values[key] = string(value)
		if !updated && key == "2:evt:h2" {
			updated = true
			err := kv.Commit(ctx, Batch{Puts: []Pair{
				{Key: "3:evt:z1", Value: []byte("new-hot")},
				{Key: "checkpoint:1:4", Value: []byte("cut-four")},
			}})
			if err != nil {
				return false, fmt.Errorf("the cut beside the active walk = %v, want the commit to succeed with its maintenance pending", err)
			}
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("walk beside a callback commit: %v", err)
	}
	if got := values["3:evt:z1"]; got != "body-z1" {
		t.Fatalf("the walk answered %q for the archive-only key, want its start-state value", got)
	}
	assertArchiveWalkClearedForTest(t, kv)

	kv, _, _ = archivedEnumerationViewForTest(t)
	waited := false
	err = kv.EachCommittedNewestFirst(ctx, func(string, []byte) (bool, error) {
		if waited {
			return true, nil
		}
		kv.mu.Lock()
		err := kv.maintainAfterCutLocked(time.Now())
		waited = kv.maintenancePending
		kv.mu.Unlock()
		if !errors.Is(err, errArchiveMaintenanceDeferred) {
			return false, fmt.Errorf("maintenance during the walk = %v, want the deferred sentinel", err)
		}
		return true, nil
	})
	if err != nil || !waited {
		t.Fatalf("maintenance during the walk waited=%t err=%v, want a deferred request", waited, err)
	}
	assertArchiveWalkClearedForTest(t, kv)
}

// TestHotWalkPinsArchiveMaintenanceAcrossCallbackCut pins the hot-only walk's log address.
//   - a checkpoint cut inside the callback answers success and defers its maintenance
//   - the walk finishes on its original address, then maintenance clears its pending state
func TestHotWalkPinsArchiveMaintenanceAcrossCallbackCut(t *testing.T) {
	ctx := context.Background()
	kv, _, _ := archivedEnumerationViewForTest(t)
	kv.mu.Lock()
	kv.hotLogMaxBytes = 1
	kv.mu.Unlock()
	kv.mu.Lock()
	start := kv.log.addr.orderSize
	kv.mu.Unlock()
	cut := false
	err := kv.EachHotCommittedNewestFirstFrom(ctx, start, func(string, []byte) (bool, error) {
		if cut {
			return true, nil
		}
		cut = true
		err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "checkpoint:1:4", Value: []byte("cut-four")}}})
		if err != nil {
			return false, fmt.Errorf("the hot walk's callback cut = %v, want the commit to succeed with its maintenance pending", err)
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("finish the hot walk beside its callback cut: %v", err)
	}
	if !cut {
		t.Fatal("the hot walk named no key, so the cut ran beside no walk")
	}
	assertArchiveWalkClearedForTest(t, kv)
}

// TestFactCommitBesideActiveWalkStaysCommitted pins the outcome a deferred maintenance leaves the commit that met it.
//   - a fact whose batch states a checkpoint cut commits while a walk holds the archive maintenance
//   - the commit answers FactCommitted, so maintenance that waits for the walk is never a FactUncertain the caller re-reads
//   - the finished walk runs the maintenance it held back and clears the pending state
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestFactCommitBesideActiveWalkStaysCommitted(t *testing.T) {
	ctx := context.Background()
	hotDir, archiveRoot := t.TempDir(), t.TempDir()
	journal, err := openJournalViewWithArchive(hotDir, true, segmentTestBudget, archiveRoot, 0)
	if err != nil {
		t.Fatalf("open the archive-backed journal view: %v", err)
	}
	t.Cleanup(func() { journal.Close() })
	for _, batch := range []Batch{
		{Puts: []Pair{{Key: "1:evt:seed-a", Value: []byte("body-seed-a")}, {Key: "1:idx:seed-a", Value: []byte("pos-seed-a")}}},
		{Puts: []Pair{{Key: "checkpoint:1:1", Value: []byte("cut-one")}}},
		{Puts: []Pair{{Key: "1:evt:seed-b", Value: []byte("body-seed-b")}, {Key: "1:idx:seed-b", Value: []byte("pos-seed-b")}}},
		{Puts: []Pair{{Key: "checkpoint:1:2", Value: []byte("cut-two")}}},
		{Puts: []Pair{{Key: "1:evt:seed-c", Value: []byte("body-seed-c")}}},
	} {
		if err := journal.Commit(ctx, batch); err != nil {
			t.Fatalf("seed the hot window: %v", err)
		}
	}
	journal.mu.Lock()
	journal.hotLogMaxBytes = 1
	journal.mu.Unlock()
	tier, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Engine: journal})
	if err != nil {
		t.Fatalf("open the tier over the journal view: %v", err)
	}
	t.Cleanup(func() { tier.Close() })
	if outcome, err := CommitFact(ctx, tier, factProbeFact([]byte("the fact the walk passes"))); err != nil || outcome != FactCommitted {
		t.Fatalf("commit the fact the walk passes = (%v, %v), want the committed outcome", outcome, err)
	}
	cut := factProbeFact([]byte("the cut the walk defers"))
	cut.Record.EventKey = event.EventID(event.CheckpointKey(1, 3))
	cut.Record.Seq = 2
	cut.Record.EventTime = factProbeWindow + 1
	cut.Record = cut.Record.WithChain(event.TypeCheckpoint, "")
	cut.Ref.Key = cut.Record.EventKey

	committed := false
	err = journal.EachCommittedNewestFirst(ctx, func(string, []byte) (bool, error) {
		if committed {
			return true, nil
		}
		committed = true
		outcome, err := CommitFact(ctx, tier, cut)
		if err != nil || outcome != FactCommitted {
			return false, fmt.Errorf("the fact commit beside the active walk = (%v, %v), want the committed outcome a deferred maintenance leaves", outcome, err)
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("walk the journal beside the fact commit: %v", err)
	}
	if !committed {
		t.Fatal("the walk named no key, so the fact commit ran beside no walk")
	}
	assertArchiveWalkClearedForTest(t, journal)
}

// TestCommittedOrderFromHonorsHistoricalTombstones pins a seek below a key's latest slot.
//   - A delete at the seek byte blocks the older archived value.
//   - A later hot rewrite above the seek does not enter this walk's snapshot.
func TestCommittedOrderFromHonorsHistoricalTombstones(t *testing.T) {
	ctx := context.Background()
	kv, _, _ := archivedEnumerationViewForTest(t)
	if err := kv.Commit(ctx, Batch{
		Puts:    []Pair{{Key: "marker:delete", Value: []byte("delete")}},
		Deletes: []string{"1:idx:b1"},
	}); err != nil {
		t.Fatalf("commit the historical tombstone: %v", err)
	}
	from, held, err := kv.OrderLineEnd(ctx, "marker:delete")
	if err != nil || !held {
		t.Fatalf("locate the tombstone commit = (%d, %t, %v)", from, held, err)
	}
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "1:idx:b1", Value: []byte("revived-hot")}}}); err != nil {
		t.Fatalf("commit the later rewrite: %v", err)
	}
	keys, _ := committedOrderFromForTest(t, kv, from)
	for _, key := range keys {
		if key == "1:idx:b1" {
			t.Fatalf("the walk resurrected the archived value below a historical tombstone: %v", keys)
		}
	}
}

// TestEnumerationIncludesReadWindowTombstones pins a read window's unwatermarked statements.
//   - A window-only delete blocks an older archived value in both listing and order walks.
func TestEnumerationIncludesReadWindowTombstones(t *testing.T) {
	ctx := context.Background()
	writer, hotDir, archiveRoot := archivedEnumerationViewForTest(t)
	if err := writer.Commit(ctx, Batch{Deletes: []string{"3:evt:z1"}}); err != nil {
		t.Fatalf("commit the window-only tombstone: %v", err)
	}
	reader, err := openJournalViewWithArchive(hotDir, false, 0, archiveRoot, 0)
	if err != nil {
		t.Fatalf("open the read window: %v", err)
	}
	t.Cleanup(func() { reader.Close() })
	if reader.log.addr.window == nil {
		t.Fatal("the read view states no window for the unwatermarked tombstone")
	}
	keys, err := reader.Keys(ctx, "")
	if err != nil {
		t.Fatalf("list keys through the read window: %v", err)
	}
	for _, key := range keys {
		if key == "3:evt:z1" {
			t.Fatalf("the key listing resurrected the window-deleted archive key: %v", keys)
		}
	}
	ordered, _ := committedOrderForTest(t, reader)
	for _, key := range ordered {
		if key == "3:evt:z1" {
			t.Fatalf("the order walk resurrected the window-deleted archive key: %v", ordered)
		}
	}
}

// TestEnumerationHandlesAWholeLogReadWindow pins an index-less read view.
//   - A read view that rebuilt only a window still enumerates hot and archived keys.
func TestEnumerationHandlesAWholeLogReadWindow(t *testing.T) {
	writer, hotDir, archiveRoot := archivedEnumerationViewForTest(t)
	if err := writer.Close(); err != nil {
		t.Fatalf("close the fixture writer: %v", err)
	}
	for _, name := range []string{journalSlotFileName, journalOrderFileName, journalOrderLineFileName} {
		if err := os.Remove(filepath.Join(hotDir, name)); err != nil {
			t.Fatalf("remove %s: %v", name, err)
		}
	}
	reader, err := openJournalViewWithArchive(hotDir, false, 0, archiveRoot, 0)
	if err != nil {
		t.Fatalf("open the whole-log read window: %v", err)
	}
	t.Cleanup(func() { reader.Close() })
	keys, _ := committedOrderForTest(t, reader)
	if !slices.Contains(keys, "3:evt:z1") {
		t.Fatalf("the whole-log walk omitted the archived key: %v", keys)
	}
}

// TestCommittedOrderFromExcludesANewerReadWindow pins the seek boundary on a read view.
//   - A window rewrite above the seek is not returned before the archived value at the seek.
func TestCommittedOrderFromExcludesANewerReadWindow(t *testing.T) {
	ctx := context.Background()
	writer, hotDir, archiveRoot := archivedEnumerationViewForTest(t)
	from, held, err := writer.OrderLineEnd(ctx, "checkpoint:1:3")
	if err != nil || !held {
		t.Fatalf("locate the pre-window commit = (%d, %t, %v)", from, held, err)
	}
	if err := writer.Commit(ctx, Batch{Puts: []Pair{{Key: "3:evt:z1", Value: []byte("window-new")}}}); err != nil {
		t.Fatalf("commit the newer window rewrite: %v", err)
	}
	reader, err := openJournalViewWithArchive(hotDir, false, 0, archiveRoot, 0)
	if err != nil {
		t.Fatalf("open the read window: %v", err)
	}
	t.Cleanup(func() { reader.Close() })
	if reader.log.addr.window == nil {
		t.Fatal("the read view states no window for the newer rewrite")
	}
	keys, values := committedOrderFromForTest(t, reader, from)
	count := 0
	for _, key := range keys {
		if key == "3:evt:z1" {
			count++
		}
	}
	if count != 1 || values["3:evt:z1"] != "body-z1" {
		t.Fatalf("the seek walk returned %d window/archive value(s) %q, want one archived value", count, values["3:evt:z1"])
	}
}

func committedOrderFromForTest(t *testing.T, kv *journalKV, from int64) ([]string, map[string]string) {
	t.Helper()
	keys := make([]string, 0, 16)
	values := map[string]string{}
	err := kv.EachCommittedNewestFirstFrom(context.Background(), from, func(key string, value []byte) (bool, error) {
		keys = append(keys, key)
		values[key] = string(value)
		return true, nil
	})
	if err != nil {
		t.Fatalf("walk the commit order from %d: %v", from, err)
	}
	return keys, values
}

// TestCommittedOrderStopsBeforeUnreadIntervals pins the early stop of the walk.
//   - a visit that stops inside the hot order reads no archive byte, so a damaged archive is no failure of that walk
//   - a visit that stops inside the newest archived interval reads no interval behind it, so a missing older segment is no failure of that walk
//   - a walk that reaches the damaged byte reports the named failure instead of answering a shorter order
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestCommittedOrderStopsBeforeUnreadIntervals(t *testing.T) {
	ctx := context.Background()
	stopAt := func(kv *journalKV, stops int) (int, error) {
		visits := 0
		err := kv.EachCommittedNewestFirst(ctx, func(string, []byte) (bool, error) {
			visits++
			return stops <= 0 || visits < stops, nil
		})
		return visits, err
	}
	t.Run("hot stop leaves the archive unread", func(t *testing.T) {
		kv, _, archiveRoot := archivedEnumerationViewForTest(t)
		if err := os.Remove(filepath.Join(archiveRoot, journalSegmentDirName, journalSegmentLabelOf(1)+journalSegmentIndexSuffix)); err != nil {
			t.Fatalf("remove the oldest archived index: %v", err)
		}
		visits, err := stopAt(kv, 1)
		if err != nil || visits != 1 {
			t.Fatalf("the walk stopped in the hot order = %d visit(s) err=%v, want one visit and no archive read", visits, err)
		}
	})
	t.Run("archive stop leaves older intervals unread", func(t *testing.T) {
		kv, _, archiveRoot := archivedEnumerationViewForTest(t)
		if err := os.Remove(filepath.Join(archiveRoot, journalSegmentDirName, journalSegmentNameOf(1))); err != nil {
			t.Fatalf("remove the oldest archived segment: %v", err)
		}
		visits, err := stopAt(kv, 8)
		if err != nil || visits != 8 {
			t.Fatalf("the walk stopped inside the newest archived interval = %d visit(s) err=%v, want eight visits and no read of the older interval", visits, err)
		}
		visits, err = stopAt(kv, 0)
		if !errors.Is(err, ErrArchiveMissing) || visits != 9 {
			t.Fatalf("the full walk beside the missing segment = %d visit(s) err=%v, want the nine visits before the named failure", visits, err)
		}
	})
}

// TestEnumerationsCrossTheArchiveThroughAnOpenedTier pins enumerations over the production lifecycle.
//   - The key listing states archived and hot keys.
//   - The namespace listing retains a partition held only by the archive.
//   - The full-history walk of a reopened tier answers archived values.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestEnumerationsCrossTheArchiveThroughAnOpenedTier(t *testing.T) {
	ctx := context.Background()
	root, archiveRoot := t.TempDir(), t.TempDir()
	cfg := StorageConfig{Type: StorageJournal, Root: root, WriteRights: true}
	cfg.ArchiveRoot = archiveRoot
	cfg.HotLogMaxBytes = 1
	tier, err := OpenTier(ctx, cfg)
	if err != nil {
		t.Fatalf("open the tier: %v", err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			tier.Close()
		}
	})
	window := int64(1700000000000)
	commit := func(partition int, seq int64, key event.EventID) {
		t.Helper()
		body := []byte("body-" + key)
		fact := FactCommit{
			Record: SegmentRecord{
				PartitionID: partition,
				WindowTS:    window,
				Seq:         seq,
				EventKey:    key,
				EventTime:   window,
				Body:        body,
			}.WithChain("f005", "enumeration"),
			Ref: OriginalRef{
				Key:             key,
				Owner:           event.Address{AgentID: "agent", SessionID: "enumeration"},
				Format:          "text/plain",
				RetentionPolicy: "keep",
			},
			Body: body,
		}
		if outcome, err := CommitFact(ctx, tier, fact); err != nil || outcome != FactCommitted {
			t.Fatalf("commit %q = %v, %v, want the committed outcome", key, outcome, err)
		}
	}
	commit(1, 1, "archived-one")
	commit(1, 2, "archived-two")
	if err := TierFactLog(tier).Put(ctx, "checkpoint:1:1", []byte("cut-one")); err != nil {
		t.Fatalf("commit the first cut: %v", err)
	}
	commit(2, 1, "hot-one")
	if err := TierFactLog(tier).Put(ctx, "checkpoint:2:2", []byte("cut-two")); err != nil {
		t.Fatalf("commit the second cut: %v", err)
	}
	manifest, found, err := readArchiveManifest(archiveRoot)
	if err != nil || !found || len(manifest.Segments) == 0 {
		t.Fatalf("the cut past the hot budget states found=%v segments=%d err=%v, want a nonempty archive", found, len(manifest.Segments), err)
	}
	archivedKeys, err := tier.Store.Keys(ctx, "1:")
	if err != nil || len(archivedKeys) == 0 {
		t.Fatalf("the key listing of the archived partition = %v err=%v, want its event, index and meta keys", archivedKeys, err)
	}
	namespaces, err := tier.Store.Namespaces(ctx)
	if err != nil || !slices.Contains(namespaces, "1") {
		t.Fatalf("the namespace listing = %v err=%v, want the partition the archive holds", namespaces, err)
	}
	order, holds := tier.Store.(CommitOrder)
	if !holds {
		t.Fatal("the opened engine states no commit order")
	}
	stated := ""
	if err := order.EachCommittedNewestFirst(ctx, func(key string, value []byte) (bool, error) {
		if key == "1:idx:archived-one" {
			stated = string(value)
			return false, nil
		}
		return true, nil
	}); err != nil {
		t.Fatalf("walk the commit order of the opened tier: %v", err)
	}
	if stated == "" {
		t.Fatal("the commit order named no archived index entry, want the entry the moved interval states")
	}
	if err := tier.Close(); err != nil {
		t.Fatalf("close the tier: %v", err)
	}
	closed = true
	reopened, err := OpenTier(ctx, cfg)
	if err != nil {
		t.Fatalf("reopen the tier: %v", err)
	}
	t.Cleanup(func() { reopened.Close() })
	events := make([]event.EventID, 0, 3)
	if err := reopened.Segments.EachEvent(ctx, func(stored StoredEvent) error {
		events = append(events, stored.Key)
		return nil
	}); err != nil {
		t.Fatalf("enumerate the full history of the reopened tier: %v", err)
	}
	for _, key := range []event.EventID{"archived-one", "archived-two", "hot-one"} {
		if !slices.Contains(events, key) {
			t.Fatalf("the full-history enumeration = %v, want %q beside the events the hot window keeps", events, key)
		}
	}
}
