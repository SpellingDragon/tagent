// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// TestFoldRootStatesTheFactsInCommitOrder pins the fold of a root whose facts sit in both journals.
//   - the dry run states the commit order, writes no byte of the source and opens no target
//   - the restore states every event, every original and the seat in the new root
//   - the folded root answers each identity with the source's bytes and walks the source's commit order
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestFoldRootStatesTheFactsInCommitOrder(t *testing.T) {
	ctx := context.Background()
	source, target := t.TempDir(), filepath.Join(t.TempDir(), "new-root")
	full := t.TempDir()
	events := fixtureEvents()
	commitHistory(t, ctx, full, events)
	splitSource(t, ctx, full, source, 2)
	before := snapshotRoot(t, source)

	plan, folded, err := foldRoot(ctx, source, target, false)
	if err != nil {
		t.Fatalf("the dry run over %s returned %v", source, err)
	}
	if got := eventKeys(plan); !reflect.DeepEqual(got, fixtureKeys(events)) {
		t.Fatalf("the planned commit order = %v, want %v", got, fixtureKeys(events))
	}
	if want := 2 * len(originals(events)); len(plan.Originals) != want {
		t.Fatalf("the plan holds %d original keys beside the %d the source states", len(plan.Originals), want)
	}
	if !plan.Seated || plan.Seat.Key != storeSeatKey {
		t.Fatalf("the plan seat = %+v, want the %s of the source", plan.Seat, storeSeatKey)
	}
	if len(plan.Left) != 2 {
		t.Fatalf("the plan leaves %d keys behind, want the two settled statements", len(plan.Left))
	}
	if folded != (foldReport{}) {
		t.Fatalf("the dry run committed %+v, want nothing", folded)
	}
	assertUnchanged(t, source, before)
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the dry run left %s at %v, want no target", target, err)
	}

	_, restored, err := foldRoot(ctx, source, target, true)
	if err != nil {
		t.Fatalf("the restore into %s returned %v", target, err)
	}
	want := foldReport{Folded: len(events), Written: 2 * len(originals(events))}
	if restored != want {
		t.Fatalf("the restore committed %+v, want %+v", restored, want)
	}
	assertUnchanged(t, source, before)
	foldedTier := openTier(t, ctx, target)
	for _, evt := range events {
		got, held, err := foldedTier.Segments.GetEvent(ctx, event.EventID(evt.key))
		if err != nil || !held {
			t.Fatalf("the folded root holds %q: held=%t err=%v", evt.key, held, err)
		}
		if !bytes.Equal(got.Body, []byte(evt.body)) {
			t.Fatalf("the folded root holds %q for %q, want %q", got.Body, evt.key, evt.body)
		}
		if evt.original == "" {
			continue
		}
		recalled := foldedTier.Originals.Get(ctx, event.EventID(evt.key))
		if recalled.Outcome != ledger.RecallFound || !bytes.Equal(recalled.Body, []byte(evt.original)) {
			t.Fatalf("the folded root answers %q with %+v, want %q", evt.key, recalled, evt.original)
		}
	}
	assertCommitOrder(t, ctx, foldedTier, fixtureKeys(events))
	if count := foldedTier.Segments.EventCount(); count != len(events) {
		t.Fatalf("the folded root counts %d events, want %d", count, len(events))
	}
}

// TestFoldRootStatesARootThatMovedNothing pins the fold of a root whose facts all sit in its hot journal.
//   - the plan names the hot journal alone and leaves no key behind
//   - the folded root walks the commit order of the source and holds no cold directory
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestFoldRootStatesARootThatMovedNothing(t *testing.T) {
	ctx := context.Background()
	source, target := t.TempDir(), filepath.Join(t.TempDir(), "new-root")
	full := t.TempDir()
	events := fixtureEvents()
	commitHistory(t, ctx, full, events)
	splitSource(t, ctx, full, source, len(events))

	plan, _, err := foldRoot(ctx, source, target, true)
	if err != nil {
		t.Fatalf("the fold of a root that moved nothing returned %v", err)
	}
	if !reflect.DeepEqual(plan.Tiers, []string{sourceTiers[0].Name}) {
		t.Fatalf("the plan names the journals %v, want the hot one alone", plan.Tiers)
	}
	if len(plan.Left) != 0 {
		t.Fatalf("the plan leaves %v behind, want no key", plan.Left)
	}
	if _, err := os.Stat(filepath.Join(source, coldDirName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the source holds a cold journal at %v, want none", err)
	}
	assertCommitOrder(t, ctx, openTier(t, ctx, target), fixtureKeys(events))
}

// TestFoldRootReadsARootWhoseIndexStatesNoWatermark pins the plan of a root whose index files state no watermark this build reads.
//   - the plan states the same events, originals and seat as the read of an index this build states
//   - the source root keeps every entry and every byte, so reading such a root costs it no write
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestFoldRootReadsARootWhoseIndexStatesNoWatermark(t *testing.T) {
	ctx := context.Background()
	source, full := t.TempDir(), t.TempDir()
	events := fixtureEvents()
	commitHistory(t, ctx, full, events)
	splitSource(t, ctx, full, source, 2)
	intact, _, err := foldRoot(ctx, source, filepath.Join(t.TempDir(), "new-root"), false)
	if err != nil {
		t.Fatalf("the dry run over the source root returned %v", err)
	}
	for _, dir := range []string{filepath.Join(source, kvDirName), filepath.Join(source, coldDirName, kvDirName)} {
		stateOlderSlotHeader(t, dir)
	}
	if notice := unstatedIndexNotice(t, ctx, source); notice == "" {
		t.Fatalf("the hot journal of %s states a watermark this build reads, want the fallback the case pins", source)
	}
	before := snapshotRoot(t, source)

	older, _, err := foldRoot(ctx, source, filepath.Join(t.TempDir(), "new-root"), false)
	if err != nil {
		t.Fatalf("the dry run over a root whose index states no watermark this build reads returned %v", err)
	}
	if got, want := eventKeys(older), eventKeys(intact); !reflect.DeepEqual(got, want) {
		t.Fatalf("the plan states the commit order %v, want the %v of a root whose index states one", got, want)
	}
	if !reflect.DeepEqual(older.Originals, intact.Originals) {
		t.Fatalf("the plan states %d original key(s), want the %d the source states", len(older.Originals), len(intact.Originals))
	}
	if !older.Seated || older.Seat.Key != intact.Seat.Key || !bytes.Equal(older.Seat.Value, intact.Seat.Value) {
		t.Fatalf("the plan states the seat %+v, want the %+v of the source", older.Seat, intact.Seat)
	}
	assertUnchanged(t, source, before)
}

// TestFoldRootRefusesOneDirectoryAsSourceAndTarget pins the refusal that keeps the fold away from the root it reads.
//   - one directory named as both roots is refused and left byte for byte as it was
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestFoldRootRefusesOneDirectoryAsSourceAndTarget(t *testing.T) {
	ctx := context.Background()
	source, full := t.TempDir(), t.TempDir()
	commitHistory(t, ctx, full, fixtureEvents())
	splitSource(t, ctx, full, source, 2)
	before := snapshotRoot(t, source)

	for _, restore := range []bool{false, true} {
		_, _, err := foldRoot(ctx, source, source, restore)
		if !errors.Is(err, ledger.ErrMigrateTargetNotEmpty) {
			t.Fatalf("the fold of %s into itself (restore=%t) = %v, want ErrMigrateTargetNotEmpty", source, restore, err)
		}
	}
	assertUnchanged(t, source, before)
}

// TestFoldRootStatesTheHotStatementOfAWindowTheColdCopyTrails pins the one key the two journals may state two ways.
//   - a cold copy of a window meta an older count is not a conflict: the plan names the key and states the hot statement
//   - the folded root holds the hot journal's bytes for that window and the same commit order
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestFoldRootStatesTheHotStatementOfAWindowTheColdCopyTrails(t *testing.T) {
	ctx := context.Background()
	source, target := t.TempDir(), filepath.Join(t.TempDir(), "new-root")
	full := t.TempDir()
	events := fixtureEvents()
	commitHistory(t, ctx, full, events)
	splitSource(t, ctx, full, source, 2)
	const key = "1:meta:2"
	commitPairs(t, ctx, filepath.Join(source, coldDirName), ledger.Pair{
		Key:   key,
		Value: []byte(`{"partition_id":1,"window_ts":2,"event_count":1,"min_time":201,"max_time":202}`),
	})
	want, err := ledger.OpenAtomicStore(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: source})
	if err != nil {
		t.Fatalf("open the hot journal of %s: %v", source, err)
	}
	stated, err := want.Read(ctx, ledger.Fetch{Key: key})
	closeStore(want)
	if err != nil || len(stated) != 1 {
		t.Fatalf("read %s of the hot journal: %v %v", key, stated, err)
	}

	plan, _, err := foldRoot(ctx, source, target, true)
	if err != nil {
		t.Fatalf("the fold of a window the cold copy trails returned %v", err)
	}
	if !reflect.DeepEqual(plan.Superseded, []string{key}) {
		t.Fatalf("the plan supersedes %v, want %q alone", plan.Superseded, key)
	}
	folded, err := ledger.OpenAtomicStore(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: target})
	if err != nil {
		t.Fatalf("open the folded root %s: %v", target, err)
	}
	got, err := folded.Read(ctx, ledger.Fetch{Key: key})
	closeStore(folded)
	if err != nil || len(got) != 1 {
		t.Fatalf("read %s of the folded root: %v %v", key, got, err)
	}
	if !bytes.Equal(got[0].Value, stated[0].Value) {
		t.Fatalf("the folded root holds %q for %s, want the hot journal's %q", got[0].Value, key, stated[0].Value)
	}
	assertCommitOrder(t, ctx, openTier(t, ctx, target), fixtureKeys(events))
}

// TestFoldRootRefusesARootAnotherWriterHolds pins the named refusal of a target that a live writer owns.
//   - the restore answers ErrRootWriteLocked and names the target it could not take
//   - the held target gains no key, so the refusal lands nothing
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestFoldRootRefusesARootAnotherWriterHolds(t *testing.T) {
	ctx := context.Background()
	source, full := t.TempDir(), t.TempDir()
	commitHistory(t, ctx, full, fixtureEvents())
	splitSource(t, ctx, full, source, 2)
	target := t.TempDir()
	writer, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: target, WriteRights: true})
	if err != nil {
		t.Fatalf("open the writer of %s: %v", target, err)
	}
	defer writer.Close()

	_, _, err = foldRoot(ctx, source, target, true)
	if !errors.Is(err, ledger.ErrRootWriteLocked) {
		t.Fatalf("the fold into a root another writer holds = %v, want ErrRootWriteLocked", err)
	}
	if !strings.Contains(err.Error(), target) {
		t.Fatalf("the refusal %q does not name the target %s", err, target)
	}
	keys, err := readKeys(t, ctx, target)
	if err != nil {
		t.Fatalf("read %s beside its writer: %v", target, err)
	}
	if len(keys) != 0 {
		t.Fatalf("the refused fold left the keys %v, want none", keys)
	}
}

// TestFoldRootRefusesATargetThatHoldsKeys pins the refusal to fold into a root that is in use.
//   - a second restore into a folded root answers ErrMigrateTargetNotEmpty and names it
//   - the folded root keeps the facts the first restore stated
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestFoldRootRefusesATargetThatHoldsKeys(t *testing.T) {
	ctx := context.Background()
	source, target := t.TempDir(), t.TempDir()
	full := t.TempDir()
	commitHistory(t, ctx, full, fixtureEvents())
	splitSource(t, ctx, full, source, 2)
	if _, _, err := foldRoot(ctx, source, target, true); err != nil {
		t.Fatalf("the first restore into %s returned %v", target, err)
	}

	_, _, err := foldRoot(ctx, source, target, true)
	if !errors.Is(err, ledger.ErrMigrateTargetNotEmpty) {
		t.Fatalf("the second restore into a folded root = %v, want ErrMigrateTargetNotEmpty", err)
	}
	if !strings.Contains(err.Error(), target) {
		t.Fatalf("the refusal %q does not name the target %s", err, target)
	}
	assertCommitOrder(t, ctx, openTier(t, ctx, target), fixtureKeys(fixtureEvents()))
}

// TestFoldRootRefusesAColdJournalThatDisagreesWithItsStatement pins the check the settled:commit key makes.
//   - a greatest cold commit above the stated one is refused naming both numbers
//   - a cold journal beside no settled statement is refused by name
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestFoldRootRefusesAColdJournalThatDisagreesWithItsStatement(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		state func(t *testing.T, source string)
		want  error
	}{
		{
			name: "stated below the cold journal",
			state: func(t *testing.T, source string) {
				commitPairs(t, ctx, source, ledger.Pair{Key: settledCommitKey, Value: []byte("1")})
			},
			want: ErrSourceConflict,
		},
		{
			name: "no statement at all",
			state: func(t *testing.T, source string) {
				commitDelete(t, ctx, source, settledCommitKey)
			},
			want: ErrSourceIncomplete,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source, full := t.TempDir(), t.TempDir()
			commitHistory(t, ctx, full, fixtureEvents())
			splitSource(t, ctx, full, source, 2)
			tc.state(t, source)
			_, _, err := foldRoot(ctx, source, filepath.Join(t.TempDir(), "new-root"), false)
			if !errors.Is(err, tc.want) {
				t.Fatalf("the dry run beside a cold journal with %s = %v, want %v", tc.name, err, tc.want)
			}
		})
	}
}

// TestFoldRootRefusesOneCommitTwoEventsClaim pins the refusal of a source whose commit order is ambiguous.
//   - a second index entry carrying the commit of another event is refused naming both keys
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestFoldRootRefusesOneCommitTwoEventsClaim(t *testing.T) {
	ctx := context.Background()
	source, full := t.TempDir(), t.TempDir()
	commitHistory(t, ctx, full, fixtureEvents())
	splitSource(t, ctx, full, source, 2)
	commitPairs(t, ctx, filepath.Join(source, coldDirName), ledger.Pair{
		Key:   "1:idx:duplicate",
		Value: encodeIndexEntry(t, 0, 1, 1),
	})

	_, _, err := foldRoot(ctx, source, filepath.Join(t.TempDir(), "new-root"), false)
	if !errors.Is(err, ErrSourceConflict) {
		t.Fatalf("the dry run over two events claiming one commit = %v, want ErrSourceConflict", err)
	}
	for _, want := range []string{"duplicate", "commit 1"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal %q does not name %q", err, want)
		}
	}
}

// TestFoldRootRefusesAReferenceWithNoBody pins the refusal of an original whose independent body is absent.
//   - a ref: key with no blob: key is refused as the corrupt snapshot the boot load names
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestFoldRootRefusesAReferenceWithNoBody(t *testing.T) {
	ctx := context.Background()
	source, full := t.TempDir(), t.TempDir()
	commitHistory(t, ctx, full, fixtureEvents())
	splitSource(t, ctx, full, source, 2)
	commitPairs(t, ctx, filepath.Join(source, coldDirName), ledger.Pair{
		Key:   refPrefix + "orphaned",
		Value: []byte(`[{"owner_agent":"fixture","owner_session":"session-a","format":"text/plain"}]`),
	})

	_, _, err := foldRoot(ctx, source, filepath.Join(t.TempDir(), "new-root"), false)
	if !errors.Is(err, ledger.ErrSnapshotCorrupt) {
		t.Fatalf("the dry run over an orphaned reference = %v, want ErrSnapshotCorrupt", err)
	}
	if !strings.Contains(err.Error(), "orphaned") {
		t.Fatalf("the refusal %q does not name the orphaned reference", err)
	}
}

// TestFoldRootRefusesASourceWithoutAJournal pins the named refusal of a root the fold cannot read.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestFoldRootRefusesASourceWithoutAJournal(t *testing.T) {
	source := t.TempDir()
	_, _, err := foldRoot(context.Background(), source, filepath.Join(t.TempDir(), "new-root"), false)
	if !errors.Is(err, ErrSourceUnstated) {
		t.Fatalf("the dry run over a root without a journal = %v, want ErrSourceUnstated", err)
	}
	if !strings.Contains(err.Error(), filepath.Join(source, kvDirName)) {
		t.Fatalf("the refusal %q does not name the journal directory of %s", err, source)
	}
}

// TestFoldColdRootCommandExitCodes pins the flags and the exit status of the built command over a source root.
//   - a missing flag fails nonzero naming both flags
//   - the default scan exits 0, reports the dry run and writes no byte of the source
//   - -restore exits 0 and the folded root answers the source's commit order
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestFoldColdRootCommandExitCodes(t *testing.T) {
	ctx := context.Background()
	binary := buildFoldColdRoot(t, ctx)
	source, target := t.TempDir(), filepath.Join(t.TempDir(), "new-root")
	full := t.TempDir()
	events := fixtureEvents()
	commitHistory(t, ctx, full, events)
	splitSource(t, ctx, full, source, 2)
	before := snapshotRoot(t, source)

	_, errOut, code := runFoldColdRoot(t, ctx, binary, "-source", source)
	if code != 1 || !strings.Contains(errOut, "-source and -root are required") {
		t.Fatalf("the command without -root exited %d with stderr %q, want 1 naming the flags", code, errOut)
	}
	out, errOut, code := runFoldColdRoot(t, ctx, binary, "-source", source, "-root", target)
	if code != 0 {
		t.Fatalf("the default scan exited %d with stderr %q, want 0", code, errOut)
	}
	for _, want := range []string{"dry run", "key(s) to state", strconv.Itoa(fixtureKeyCount(events))} {
		if !strings.Contains(out, want) {
			t.Fatalf("the default scan stdout %q does not name %q", out, want)
		}
	}
	assertUnchanged(t, source, before)
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the default scan left %s at %v, want no target", target, err)
	}

	out, errOut, code = runFoldColdRoot(t, ctx, binary, "-source", source, "-root", target, "-restore")
	if code != 0 || !strings.Contains(out, "committed") {
		t.Fatalf("the restore exited %d with stdout %q and stderr %q, want 0 reporting the commit", code, out, errOut)
	}
	assertCommitOrder(t, ctx, openTier(t, ctx, target), fixtureKeys(events))
}

// TestOfflineConversionPreservesEveryFact pins the conversion of an offline root through the command an operator runs.
//   - the default scan writes no byte of the source and opens no target
//   - the restore exits 0 and states the folded root read back by identity in the source's commit order
//   - the folded root states every key of the history the source was split from, in the same bytes, and walks the same commit order
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestOfflineConversionPreservesEveryFact(t *testing.T) {
	ctx := context.Background()
	binary := buildFoldColdRoot(t, ctx)
	full, source := t.TempDir(), t.TempDir()
	target := filepath.Join(t.TempDir(), "new-root")
	events := fixtureEvents()
	commitHistory(t, ctx, full, events)
	splitSource(t, ctx, full, source, 2)
	before := snapshotRoot(t, source)

	out, errOut, code := runFoldColdRoot(t, ctx, binary, "-source", source, "-root", target)
	if code != 0 || !strings.Contains(out, "dry run") {
		t.Fatalf("the default scan exited %d with stdout %q and stderr %q, want 0 stating the dry run", code, out, errOut)
	}
	assertUnchanged(t, source, before)
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the default scan left %s at %v, want no target", target, err)
	}

	out, errOut, code = runFoldColdRoot(t, ctx, binary, "-source", source, "-root", target, "-restore")
	if code != 0 || !strings.Contains(out, "read back by identity") {
		t.Fatalf("the restore exited %d with stdout %q and stderr %q, want 0 stating the root read back by identity",
			code, out, errOut)
	}
	assertUnchanged(t, source, before)

	want := heldBytes(t, ctx, full)
	got := heldBytes(t, ctx, target)
	if len(got) != len(want) {
		t.Fatalf("the folded root states %d key(s) beside the %d the source history holds", len(got), len(want))
	}
	for key, body := range want {
		stated, held := got[key]
		if !held {
			t.Fatalf("the folded root states no %q, which the source history holds", key)
		}
		if !bytes.Equal(stated, body) {
			t.Fatalf("the folded root states %d byte(s) for %q beside the %d the source history holds",
				len(stated), key, len(body))
		}
	}

	folded := openTier(t, ctx, target)
	defer folded.Close()
	for _, evt := range events {
		stored, held, err := folded.Segments.GetEvent(ctx, event.EventID(evt.key))
		if err != nil || !held {
			t.Fatalf("the folded root answers the event %q: held=%t err=%v", evt.key, held, err)
		}
		if !bytes.Equal(stored.Body, []byte(evt.body)) {
			t.Fatalf("the folded root answers %q for %q, want %q", stored.Body, evt.key, evt.body)
		}
		if evt.original == "" {
			continue
		}
		recalled := folded.Originals.Get(ctx, event.EventID(evt.key))
		if recalled.Outcome != ledger.RecallFound || !bytes.Equal(recalled.Body, []byte(evt.original)) {
			t.Fatalf("the folded root answers %q with %+v, want %q", evt.key, recalled, evt.original)
		}
	}
	if count := folded.Segments.EventCount(); count != len(events) {
		t.Fatalf("the folded root counts %d events, want %d", count, len(events))
	}
	sourceTier := openTier(t, ctx, full)
	defer sourceTier.Close()
	if got, want := indexOrder(t, ctx, folded), indexOrder(t, ctx, sourceTier); !reflect.DeepEqual(got, want) {
		t.Fatalf("the folded root walks the commit order %v, want the %v of the source history", got, want)
	}
}

// TestSourceChangeRefusesConversion pins the refusal of a source that moves under the fold.
//   - a journal the source gains a byte in while it is read is refused as ledger.ErrMigrateSourceUnchanged, naming the log that moved
//   - the refused run states no plan, commits nothing, leaves no target and states no entry of its own in the source
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestSourceChangeRefusesConversion(t *testing.T) {
	ctx := context.Background()
	full, source := t.TempDir(), t.TempDir()
	target := filepath.Join(t.TempDir(), "new-root")
	commitHistory(t, ctx, full, fixtureEvents())
	splitSource(t, ctx, full, source, 2)
	before := snapshotRoot(t, source)
	hotLog := filepath.Join(kvDirName, journalFileName)
	coldLog := filepath.Join(coldDirName, kvDirName, journalFileName)

	stated := false
	journalReadHook = func(dir string) {
		if stated {
			return
		}
		stated = true
		commitPairs(t, ctx, source, ledger.Pair{
			Key:   "fact-late",
			Value: []byte("the fact a source still being written states mid-fold"),
		})
	}
	defer func() { journalReadHook = nil }()

	plan, folded, err := foldRoot(ctx, source, target, true)
	if !errors.Is(err, ledger.ErrMigrateSourceUnchanged) {
		t.Fatalf("the fold of a source that moved under the read = %v, want ErrMigrateSourceUnchanged", err)
	}
	if !strings.Contains(err.Error(), hotLog) {
		t.Fatalf("the refusal %q does not name the log that moved %s", err, hotLog)
	}
	if len(plan.Events) != 0 || plan.Seated || len(plan.Originals) != 0 || len(plan.Tiers) != 0 {
		t.Fatalf("the refused run stated the plan %+v, want none", plan)
	}
	if folded != (foldReport{}) {
		t.Fatalf("the refused run committed %+v, want nothing", folded)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the refused run left %s at %v, want no target", target, err)
	}
	after := snapshotRoot(t, source)
	if len(after) != len(before) {
		t.Fatalf("the refused run left %d entries under %s, want the %d the case left", len(after), source, len(before))
	}
	for name := range after {
		if _, held := before[name]; !held {
			t.Fatalf("the refused run stated %s under %s, which held no such entry", name, source)
		}
	}
	if got, want := after[coldLog], before[coldLog]; got.sum != want.sum || got.size != want.size || !got.modTime.Equal(want.modTime) {
		t.Fatalf("the refused run touched the %s of %s, which the case states no byte of", coldLog, source)
	}
}

// TestPartialTargetIsNotReportedReady pins what a conversion that cannot finish states and leaves.
//   - the restore states an original batch the journal frame bound refuses, naming the key it could not state
//   - the failed run states no ready root and leaves the source as it found it
//   - the events the failed run did commit stay in the target, and a second run is refused instead of overwriting them
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestPartialTargetIsNotReportedReady(t *testing.T) {
	ctx := context.Background()
	binary := buildFoldColdRoot(t, ctx)
	full, source := t.TempDir(), t.TempDir()
	target := filepath.Join(t.TempDir(), "new-root")
	events := fixtureEvents()
	commitHistory(t, ctx, full, events)
	splitSource(t, ctx, full, source, 2)
	commitLargeOriginals(t, ctx, source, 17<<20, 2)
	before := snapshotRoot(t, source)

	if out, errOut, code := runFoldColdRoot(t, ctx, binary, "-source", source, "-root", target); code != 0 ||
		!strings.Contains(out, "dry run") {
		t.Fatalf("the default scan exited %d with stdout %q and stderr %q, want 0 stating the dry run", code, out, errOut)
	}
	assertUnchanged(t, source, before)

	out, errOut, code := runFoldColdRoot(t, ctx, binary, "-source", source, "-root", target, "-restore")
	if code == 0 {
		t.Fatalf("the restore of an original batch over the frame bound exited 0 with stdout %q, want a nonzero exit", out)
	}
	for _, wants := range []string{"frame bound", blobPrefix} {
		if !strings.Contains(errOut, wants) {
			t.Fatalf("the refusal %q does not name %q", errOut, wants)
		}
	}
	for _, unwanted := range []string{"read back by identity", "committed"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("the failed run stated %q on stdout %q, want no ready root", unwanted, out)
		}
	}
	assertUnchanged(t, source, before)

	half := heldBytes(t, ctx, target)
	if len(half) == 0 {
		t.Fatalf("the failed run left %s holding no key, want the events it committed before the refused batch", target)
	}
	for key := range half {
		if strings.HasPrefix(key, blobPrefix) || strings.HasPrefix(key, refPrefix) {
			t.Fatalf("the failed run stated the original key %q, want the refused batch to land as none", key)
		}
	}
	folded := openTier(t, ctx, target)
	if count := folded.Segments.EventCount(); count != len(events) {
		t.Fatalf("the half stated root counts %d events, want the %d the source states", count, len(events))
	}
	folded.Close()

	out, errOut, code = runFoldColdRoot(t, ctx, binary, "-source", source, "-root", target, "-restore")
	if code == 0 {
		t.Fatalf("the restore over a half stated root exited 0 with stdout %q, want a nonzero exit", out)
	}
	for _, wants := range []string{"already holds data", target} {
		if !strings.Contains(errOut, wants) {
			t.Fatalf("the refusal %q does not name %q", errOut, wants)
		}
	}
	if strings.Contains(out, "read back by identity") {
		t.Fatalf("the refused run stated the root ready on stdout %q", out)
	}
	kept := heldBytes(t, ctx, target)
	if len(kept) != len(half) {
		t.Fatalf("the refused run left %d key(s) in %s beside the %d the failed run stated", len(kept), target, len(half))
	}
	for key, body := range half {
		stated, held := kept[key]
		if !held || !bytes.Equal(stated, body) {
			t.Fatalf("the refused run states %q with %d byte(s) beside the %d the failed run left", key, len(stated), len(body))
		}
	}
	assertUnchanged(t, source, before)
}

// historyEvent is one event of a source fixture: the slot it takes and the body and
// original it carries.
type historyEvent struct {
	key       string
	partition int
	window    int64
	seq       int64
	body      string
	original  string
}

// fixtureEvents answers the history every case builds: three windows of two events,
// two of them carrying an original.
func fixtureEvents() []historyEvent {
	return []historyEvent{
		{key: "fact-0-1", partition: 1, window: 0, seq: 1, body: "the body of fact-0-1", original: "the original of fact-0-1\nsecond line\n"},
		{key: "fact-0-2", partition: 1, window: 0, seq: 2, body: "the body of fact-0-2"},
		{key: "fact-1-1", partition: 1, window: 1, seq: 1, body: "the body of fact-1-1", original: "the original of fact-1-1\nsecond line\n"},
		{key: "fact-1-2", partition: 1, window: 1, seq: 2, body: "the body of fact-1-2"},
		{key: "fact-2-1", partition: 1, window: 2, seq: 1, body: "the body of fact-2-1"},
		{key: "fact-2-2", partition: 1, window: 2, seq: 2, body: "the body of fact-2-2"},
	}
}

// fixtureKeys answers the event keys of a fixture in the order the commits that
// stated them landed.
func fixtureKeys(events []historyEvent) []string {
	keys := make([]string, 0, len(events))
	for _, evt := range events {
		keys = append(keys, evt.key)
	}
	return keys
}

// originals answers the events of a fixture that carry an original.
func originals(events []historyEvent) []historyEvent {
	held := make([]historyEvent, 0, len(events))
	for _, evt := range events {
		if evt.original != "" {
			held = append(held, evt)
		}
	}
	return held
}

// fixtureKeyCount answers how many distinct keys the fold of a fixture states: one
// index entry and one body per event, one window meta per window, the originals of
// the events that carry one, and the seat.
func fixtureKeyCount(events []historyEvent) int {
	windows := map[[2]int64]struct{}{}
	for _, evt := range events {
		windows[[2]int64{int64(evt.partition), evt.window}] = struct{}{}
	}
	return 2*len(events) + len(windows) + 2*len(originals(events)) + 1
}

// eventKeys answers the event keys of a plan in the order it states them.
func eventKeys(plan foldPlan) []string {
	keys := make([]string, 0, len(plan.Events))
	for _, evt := range plan.Events {
		keys = append(keys, string(evt.Key))
	}
	return keys
}

// commitHistory commits one fixture history into root through the stores a runtime
// writes, so the source of a case holds the bytes a runtime wrote.
func commitHistory(t *testing.T, ctx context.Context, root string, events []historyEvent) {
	t.Helper()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the history root %s: %v", root, err)
	}
	for _, evt := range events {
		rec := ledger.SegmentRecord{
			PartitionID: evt.partition,
			WindowTS:    evt.window,
			Seq:         evt.seq,
			EventKey:    event.EventID(evt.key),
			EventTime:   evt.window*100 + evt.seq,
			Body:        []byte(evt.body),
		}.WithChain(event.TypeAgentOutput, "session-a")
		if err := tier.Segments.StoreEvent(ctx, rec); err != nil {
			t.Fatalf("commit the event %q: %v", evt.key, err)
		}
		if evt.original == "" {
			continue
		}
		ref := ledger.OriginalRef{
			Key:             event.EventID(evt.key),
			Owner:           event.Address{AgentID: "fixture", SessionID: "session-a"},
			Format:          "text/plain",
			RetentionPolicy: "keep",
		}
		if err := tier.Originals.Put(ctx, ref, []byte(evt.original)); err != nil {
			t.Fatalf("commit the original of %q: %v", evt.key, err)
		}
	}
	if err := tier.Close(); err != nil {
		t.Fatalf("close the history root %s: %v", root, err)
	}
}

// splitSource writes the newest keep events of the history root into the hot journal
// of source and the rest into its cold journal, the layout a root holds after its
// facts left it, and states what the cold journal reached beside the hot one.
func splitSource(t *testing.T, ctx context.Context, full, source string, keep int) {
	t.Helper()
	pairs := readPairs(t, ctx, full)
	commitOf := map[string]int64{}
	var eventCommits []int64
	for _, pair := range pairs {
		entry, named, err := indexEntryOf(sourcePair{Pair: pair, Tier: sourceTiers[0].Name})
		if err != nil {
			t.Fatalf("read the index entry %q: %v", pair.Key, err)
		}
		if !named {
			continue
		}
		eventCommits = append(eventCommits, entry.Commit)
		for _, key := range []string{pair.Key, bodyKeyOf(entry), metaKeyOf(entry)} {
			if commitOf[key] < entry.Commit {
				commitOf[key] = entry.Commit
			}
		}
	}
	sort.Slice(eventCommits, func(i, j int) bool { return eventCommits[i] < eventCommits[j] })
	moved := map[int64]struct{}{}
	for at := 0; at < len(eventCommits)-keep && at < len(eventCommits); at++ {
		moved[eventCommits[at]] = struct{}{}
	}
	var hot, cold []ledger.Pair
	for _, pair := range pairs {
		if _, went := moved[commitOf[pair.Key]]; went {
			cold = append(cold, pair)
			continue
		}
		hot = append(hot, pair)
	}
	commitPairs(t, ctx, source, hot...)
	if len(cold) > 0 {
		commitPairs(t, ctx, filepath.Join(source, coldDirName), cold...)
	}
	if len(moved) == 0 {
		return
	}
	commitPairs(t, ctx, source,
		ledger.Pair{Key: settledCommitKey, Value: []byte(strconv.FormatInt(eventCommits[len(moved)-1], 10))},
		ledger.Pair{Key: settledEventsKey, Value: []byte(strconv.Itoa(len(moved)))},
	)
}

// readPairs answers every pair of one journal root, keys in key order.
func readPairs(t *testing.T, ctx context.Context, root string) []ledger.Pair {
	t.Helper()
	store, err := ledger.OpenAtomicStore(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("open the journal root %s: %v", root, err)
	}
	pairs, readErr := store.Read(ctx, ledger.Fetch{})
	closeErr := closeStore(store)
	if readErr != nil {
		t.Fatalf("read the journal root %s: %v", root, readErr)
	}
	if closeErr != nil {
		t.Fatalf("close the journal root %s: %v", root, closeErr)
	}
	return pairs
}

// heldBytes answers every key one journal root holds beside the value the root states
// for it, so a case compares one root with another key by key.
func heldBytes(t *testing.T, ctx context.Context, root string) map[string][]byte {
	t.Helper()
	held := map[string][]byte{}
	for _, pair := range readPairs(t, ctx, root) {
		held[pair.Key] = pair.Value
	}
	return held
}

// readKeys answers the keys one journal root holds, in key order.
func readKeys(t *testing.T, ctx context.Context, root string) ([]string, error) {
	t.Helper()
	store, err := ledger.OpenAtomicStore(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	if err != nil {
		return nil, err
	}
	keys, readErr := store.Keys(ctx, "")
	closeErr := closeStore(store)
	if readErr != nil {
		return nil, readErr
	}
	return keys, closeErr
}

// commitPairs commits pairs into the journal of one root behind one commit.
func commitPairs(t *testing.T, ctx context.Context, root string, pairs ...ledger.Pair) {
	t.Helper()
	store, err := ledger.OpenAtomicStore(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the writer of %s: %v", root, err)
	}
	if err := store.Commit(ctx, ledger.Batch{Puts: pairs}); err != nil {
		closeStore(store)
		t.Fatalf("commit %d pairs into %s: %v", len(pairs), root, err)
	}
	if err := closeStore(store); err != nil {
		t.Fatalf("close the writer of %s: %v", root, err)
	}
}

// commitDelete writes a batch that deletes one key of a root.
func commitDelete(t *testing.T, ctx context.Context, root, key string) {
	t.Helper()
	store, err := ledger.OpenAtomicStore(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the writer of %s: %v", root, err)
	}
	if err := store.Commit(ctx, ledger.Batch{Deletes: []string{key}}); err != nil {
		closeStore(store)
		t.Fatalf("delete %q of %s: %v", key, root, err)
	}
	if err := closeStore(store); err != nil {
		t.Fatalf("close the writer of %s: %v", root, err)
	}
}

// openTier opens a fresh component view of one journal root.
func openTier(t *testing.T, ctx context.Context, root string) *ledger.Tier {
	t.Helper()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("open the journal root %s: %v", root, err)
	}
	return tier
}

// assertCommitOrder reports the newest-first index walk of one tier as the reverse of
// the commit order the source stated.
func assertCommitOrder(t *testing.T, ctx context.Context, tier *ledger.Tier, want []string) {
	t.Helper()
	walked := indexOrder(t, ctx, tier)
	expected := make([]string, 0, len(want))
	for at := len(want) - 1; at >= 0; at-- {
		expected = append(expected, want[at])
	}
	if !reflect.DeepEqual(walked, expected) {
		t.Fatalf("the newest-first walk = %v, want %v", walked, expected)
	}
}

// indexOrder answers the event keys one root states, newest first, as the index walk
// the folded root's consumers read.
func indexOrder(t *testing.T, ctx context.Context, tier *ledger.Tier) []string {
	t.Helper()
	var walked []string
	visit := func(key event.EventID, partition int, seq int64, typ string, session event.SessionID) error {
		walked = append(walked, string(key))
		return nil
	}
	if err := tier.Segments.EachIndexNewestFirst(ctx, visit); err != nil {
		t.Fatalf("walk the commit order: %v", err)
	}
	return walked
}

// commitLargeOriginals commits count originals of size bytes each into the hot journal
// of root, one commit per original, so a case can state a plan whose original keys do
// not fit one journal frame together.
func commitLargeOriginals(t *testing.T, ctx context.Context, root string, size, count int) {
	t.Helper()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the writer of %s: %v", root, err)
	}
	for at := 0; at < count; at++ {
		key := event.EventID("bulk-" + strconv.Itoa(at))
		body := bytes.Repeat([]byte{byte('a' + at)}, size)
		ref := ledger.OriginalRef{
			Key:             key,
			Owner:           event.Address{AgentID: "fixture", SessionID: "session-a"},
			Format:          "text/plain",
			RetentionPolicy: "keep",
		}
		if err := tier.Originals.Put(ctx, ref, body); err != nil {
			t.Fatalf("commit the original %q of %s: %v", key, root, err)
		}
	}
	if err := tier.Close(); err != nil {
		t.Fatalf("close the writer of %s: %v", root, err)
	}
}

// rootEntry is one entry of a root: a directory, or a file beside the size, the time
// and the hash a write would move.
type rootEntry struct {
	// dir reports that the entry is a directory, which holds no bytes of its own.
	dir bool
	// size is the byte count of a file.
	size int64
	// modTime is the modification time of a file.
	modTime time.Time
	// sum is the SHA-256 of the bytes of a file.
	sum [sha256.Size]byte
}

// snapshotRoot answers every entry under root, so a case pins that a scan writes
// nothing: the set of entries a root holds, and the size, time and hash of each file.
func snapshotRoot(t *testing.T, root string) map[string]rootEntry {
	t.Helper()
	entries := map[string]rootEntry{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			entries[name] = rootEntry{dir: true}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		value, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		entries[name] = rootEntry{size: info.Size(), modTime: info.ModTime(), sum: sha256.Sum256(value)}
		return nil
	})
	if err != nil {
		t.Fatalf("read the entries of %s: %v", root, err)
	}
	return entries
}

// assertUnchanged names every entry the scan added to or removed from root, and every
// file whose bytes, size or modification time differs from the snapshot.
func assertUnchanged(t *testing.T, root string, before map[string]rootEntry) {
	t.Helper()
	after := snapshotRoot(t, root)
	for name := range after {
		if _, held := before[name]; !held {
			t.Fatalf("the scan stated %s under %s, which held no such entry", name, root)
		}
	}
	if len(after) != len(before) {
		t.Fatalf("the scan left %d entries under %s, want the %d it held before", len(after), root, len(before))
	}
	for name, want := range before {
		got, held := after[name]
		if !held {
			t.Fatalf("the scan removed %s of %s", name, root)
		}
		if got.dir != want.dir {
			t.Fatalf("the scan changed whether %s of %s is a directory", name, root)
		}
		if want.dir {
			continue
		}
		if got.sum != want.sum {
			t.Fatalf("the scan rewrote %s of %s: %x beside the %x it held", name, root, got.sum, want.sum)
		}
		if got.size != want.size || !got.modTime.Equal(want.modTime) {
			t.Fatalf("the scan touched %s of %s: size %d at %v beside the %d at %v it held",
				name, root, got.size, got.modTime, want.size, want.modTime)
		}
	}
}

// stateOlderSlotHeader states the header version an older build wrote into the slot
// table of one journal directory, which is the file a read replays from. The version
// sits at bytes 4 to 8 of the table, so a table stating another version is the one
// this build cannot place, and a directory holding no such table is left alone.
func stateOlderSlotHeader(t *testing.T, dir string) {
	t.Helper()
	file, err := os.OpenFile(filepath.Join(dir, "slots"), os.O_RDWR, 0o600)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatalf("open the slot table of %s: %v", dir, err)
	}
	defer file.Close()
	if _, err := file.WriteAt([]byte{1, 0, 0, 0}, 4); err != nil {
		t.Fatalf("state the header of the slot table of %s: %v", dir, err)
	}
}

// unstatedIndexNotice answers the fallback one read view of the hot journal of root
// states: the text a view answers beside a log it read whole, which is empty beside a
// view whose index files state a watermark.
func unstatedIndexNotice(t *testing.T, ctx context.Context, root string) string {
	t.Helper()
	store, err := ledger.OpenAtomicStore(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("open the journal root %s: %v", root, err)
	}
	view, held := store.(interface{ IndexUnstatedNotice() string })
	if !held {
		t.Fatalf("the journal view of %s states no unstated-index notice", root)
	}
	notice := view.IndexUnstatedNotice()
	if err := closeStore(store); err != nil {
		t.Fatalf("close the journal root %s: %v", root, err)
	}
	return notice
}

// buildFoldColdRoot builds the command into a temporary directory and returns its
// path, so a case runs the flags and the exit status an operator meets.
func buildFoldColdRoot(t *testing.T, ctx context.Context) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "fold-cold-root")
	out, err := exec.CommandContext(ctx, "go", "build", "-o", binary, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build the command: %v\n%s", err, out)
	}
	return binary
}

// runFoldColdRoot runs the built command with args and answers its stdout, its
// stderr and its exit status.
func runFoldColdRoot(t *testing.T, ctx context.Context, binary string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.CommandContext(ctx, binary, args...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if err == nil {
		return out.String(), errOut.String(), 0
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("run %s %v: %v", binary, args, err)
	}
	return out.String(), errOut.String(), exit.ExitCode()
}

// encodeIndexEntry answers the index entry value one commit states, so a case can
// state an entry the stores did not write.
func encodeIndexEntry(t *testing.T, window, seq, commit int64) []byte {
	t.Helper()
	carried, err := json.Marshal(carriedCommit{Commit: commit})
	if err != nil {
		t.Fatalf("encode the carried commit: %v", err)
	}
	return []byte(strconv.FormatInt(window, 10) + ":" + strconv.FormatInt(seq, 10) + ":" + string(carried))
}
