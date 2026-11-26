// The hot files an archive move leaves behind: the log cut to the intervals the
// manifest does not state, the index stated again from those bytes, and the slot
// header stating the watermarks of the files beside it.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
package ledger

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// hotShrinkFixture opens one hot root carrying the given number of sealed
// intervals, each closed by its own cut beside two keys, and answers the directory
// with the intervals a boot reads. The open segment carries the two keys open-a and
// open-b after the last cut.
func hotShrinkFixture(t *testing.T, seals int) (string, []journalSealedSegment) {
	t.Helper()
	dir := t.TempDir()
	kv, err := openJournalViewFace(dir, true, segmentTestBudget)
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	for at := 1; at <= seals; at++ {
		for _, key := range hotSegmentKeysForTest(at) {
			if err := kv.Commit(context.Background(), Batch{Puts: []Pair{{Key: key, Value: hotValueForTest(key)}}}); err != nil {
				t.Fatalf("commit %q: %v", key, err)
			}
		}
	}
	for _, key := range []string{"open-a", "open-b"} {
		if err := kv.Commit(context.Background(), Batch{Puts: []Pair{{Key: key, Value: hotValueForTest(key)}}}); err != nil {
			t.Fatalf("commit %q: %v", key, err)
		}
	}
	if err := kv.log.close(); err != nil {
		t.Fatalf("close the journal view: %v", err)
	}
	return dir, readSealedSegmentsForTest(t, dir)
}

// hotSegmentKeysForTest answers the keys one sealed interval of the fixture carries.
func hotSegmentKeysForTest(at int) []string {
	return []string{
		fmt.Sprintf("seg-%02d-a", at),
		fmt.Sprintf("seg-%02d-b", at),
		fmt.Sprintf("checkpoint:1:%d", at),
	}
}

// hotValueForTest answers the bytes the fixture commits under one key.
func hotValueForTest(key string) []byte {
	return []byte("value-" + key)
}

// fileSizeForTest answers the byte count one file of a journal directory holds.
func fileSizeForTest(t *testing.T, path string) int64 {
	t.Helper()
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatalf("read the size of %s: %v", path, err)
	}
	return stat.Size()
}

// slotHeaderForTest answers the header the slot table of one journal directory
// states.
func slotHeaderForTest(t *testing.T, dir string) journalSlotHeader {
	t.Helper()
	file, err := os.Open(filepath.Join(dir, journalSlotFileName))
	if err != nil {
		t.Fatalf("open the slot table of %s: %v", dir, err)
	}
	defer file.Close()
	body := make([]byte, journalSlotHeaderBytes)
	if _, err := file.ReadAt(body, 0); err != nil {
		t.Fatalf("read the slot header of %s: %v", dir, err)
	}
	header, stated := decodeSlotHeader(body)
	if !stated {
		t.Fatalf("the slot table of %s states no header this build reads", dir)
	}
	return header
}

// hotLogSectionForTest names one section of a journal file for a replay.
type hotLogSectionForTest struct {
	*io.SectionReader
}

// Name states the log the replayed bytes come from.
func (s hotLogSectionForTest) Name() string {
	return journalFileName
}

// hotWindowAfterMoveForTest answers the hot figures the index states once the given
// intervals leave the front of one log, read the way a rebuild numbers the commits
// and measures the order lines of the bytes that remain.
func hotWindowAfterMoveForTest(t *testing.T, dir string, moved, retained []journalSealedSegment) archiveWatermark {
	t.Helper()
	if len(moved) == 0 || len(retained) == 0 {
		t.Fatalf("the check moves %d interval(s) and keeps %d, want both sides of the cut", len(moved), len(retained))
	}
	drop := int64(0)
	for _, segment := range moved {
		drop += segment.bytes
	}
	file, err := os.Open(filepath.Join(dir, journalFileName))
	if err != nil {
		t.Fatalf("open the log of %s: %v", dir, err)
	}
	defer file.Close()
	size := fileSizeForTest(t, file.Name()) - drop
	seq := retained[0].firstSeq - 1
	valuesSize := int64(0)
	orderSize := int64(0)
	complete, err := journalReplay(hotLogSectionForTest{io.NewSectionReader(file, drop, size)}, 0, size,
		func(payload []byte, at int64) error {
			spans, err := journalSpansAt(payload, at)
			if err != nil {
				return err
			}
			seq++
			valuesSize = at + int64(len(payload))
			line, err := journalOrderLine(orderStatementOf(seq, valuesSize, spans))
			if err != nil {
				return err
			}
			orderSize += int64(len(line))
			return nil
		})
	if err != nil {
		t.Fatalf("replay the hot window of %s: %v", dir, err)
	}
	if complete != size {
		t.Fatalf("the hot window of %s ends at the byte %d beside the %d it holds", dir, complete, size)
	}
	return archiveWatermark{HotSeq: seq, HotValuesSize: valuesSize, HotOrderSize: orderSize}
}

// assertHotKeysForTest pins that one view answers exactly the given keys with the
// fixture's bytes, and no other key of the root.
func assertHotKeysForTest(t *testing.T, kv *journalKV, keys []string) {
	t.Helper()
	held, err := kv.Keys(context.Background(), "")
	if err != nil {
		t.Fatalf("read the keys of the shrunk view: %v", err)
	}
	if !equalStringSets(held, keys) {
		t.Fatalf("the shrunk view holds %v, want the %v the intervals that remain carry", held, keys)
	}
	for _, key := range keys {
		value, held := readOne(t, kv, key)
		if !held || !bytes.Equal(value, hotValueForTest(key)) {
			t.Fatalf("the shrunk view read (%q, %v) for %q, want the bytes the commit wrote", value, held, key)
		}
	}
}

// TestHotIndexShrinksAfterArchive pins the hot index of the intervals a move leaves behind.
//   - the log loses the bytes of the moved intervals, and the order file and slot table state the keys of the intervals that remain alone
//   - every key of the intervals that remain reads back byte for byte through a fresh open, and no key of a moved interval is answered
//   - the slot table states one record per key of the intervals that remain, so its entries are bounded by those intervals and not by the history
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestHotIndexShrinksAfterArchive(t *testing.T) {
	hotDir, segments := hotShrinkFixture(t, 3)
	if len(segments) != 3 {
		t.Fatalf("the fixture sealed %d interval(s), want the three its cuts closed", len(segments))
	}
	moved, retained := segments[:2], segments[2:]
	beforeLog := fileSizeForTest(t, filepath.Join(hotDir, journalFileName))
	beforeOrder := fileSizeForTest(t, filepath.Join(hotDir, journalOrderFileName))
	window := hotWindowAfterMoveForTest(t, hotDir, moved, retained)
	manifest, err := moveSealedSegmentsToArchive(hotDir, t.TempDir(), moved, window)
	if err != nil {
		t.Fatalf("move the sealed intervals into the archive: %v", err)
	}
	if err := shrinkHotIndexAfterArchive(hotDir, manifest); err != nil {
		t.Fatalf("shrink the hot files after the move: %v", err)
	}
	if got := fileSizeForTest(t, filepath.Join(hotDir, journalFileName)); got != window.HotValuesSize {
		t.Fatalf("the shrunk log holds %d byte(s), want the %d the stated hot window names", got, window.HotValuesSize)
	}
	if got := fileSizeForTest(t, filepath.Join(hotDir, journalOrderFileName)); got != window.HotOrderSize {
		t.Fatalf("the shrunk order file holds %d byte(s), want the %d the stated hot window names", got, window.HotOrderSize)
	}
	if window.HotValuesSize >= beforeLog {
		t.Fatalf("the shrunk log holds %d byte(s) beside the %d it held before the move, want the moved bytes gone",
			window.HotValuesSize, beforeLog)
	}
	if window.HotOrderSize >= beforeOrder {
		t.Fatalf("the shrunk order file holds %d byte(s) beside the %d it held before the move, want the moved commits gone",
			window.HotOrderSize, beforeOrder)
	}
	keys := append(hotSegmentKeysForTest(int(retained[0].id)), "open-a", "open-b")
	kv := mustOpenHotView(t, hotDir)
	t.Cleanup(func() { kv.Close() })
	assertHotKeysForTest(t, kv, keys)
	if header := slotHeaderForTest(t, hotDir); header.Used != int64(len(keys)) {
		t.Fatalf("the slot table states %d key(s), want the %d the intervals that remain carry", header.Used, len(keys))
	}
	for at := 1; at < 3; at++ {
		for _, key := range hotSegmentKeysForTest(at) {
			if _, held := readOne(t, kv, key); held {
				t.Fatalf("the shrunk view answers %q, want no key of a moved interval", key)
			}
		}
	}
}

// TestSlotWatermarkRestatesOnShrunkHotFiles pins the slot header of the shrunk files.
//   - Seq, ValuesSize and OrderSize state the shrunk log, the shrunk order file and the last hot commit rank, not the figures the log carried before the move
//   - a repeated shrink changes nothing, and a reopen boots over the restated header and answers the keys that remain
//   - a commit and a cut behind the reopen seal the next interval from the rank the retained interval ended at
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestSlotWatermarkRestatesOnShrunkHotFiles(t *testing.T) {
	hotDir, segments := hotShrinkFixture(t, 2)
	if len(segments) != 2 {
		t.Fatalf("the fixture sealed %d interval(s), want the two its cuts closed", len(segments))
	}
	moved, retained := segments[:1], segments[1:]
	beforeLog := fileSizeForTest(t, filepath.Join(hotDir, journalFileName))
	beforeOrder := fileSizeForTest(t, filepath.Join(hotDir, journalOrderFileName))
	window := hotWindowAfterMoveForTest(t, hotDir, moved, retained)
	manifest, err := moveSealedSegmentsToArchive(hotDir, t.TempDir(), moved, window)
	if err != nil {
		t.Fatalf("move the sealed interval into the archive: %v", err)
	}
	if err := shrinkHotIndexAfterArchive(hotDir, manifest); err != nil {
		t.Fatalf("shrink the hot files after the move: %v", err)
	}
	header := slotHeaderForTest(t, hotDir)
	logSize := fileSizeForTest(t, filepath.Join(hotDir, journalFileName))
	orderSize := fileSizeForTest(t, filepath.Join(hotDir, journalOrderFileName))
	if header.ValuesSize != logSize || header.ValuesSize != window.HotValuesSize {
		t.Fatalf("the header states the log byte %d beside the %d the log holds and the %d the manifest names",
			header.ValuesSize, logSize, window.HotValuesSize)
	}
	if header.OrderSize != orderSize || header.OrderSize != window.HotOrderSize {
		t.Fatalf("the header states the order byte %d beside the %d the order file holds and the %d the manifest names",
			header.OrderSize, orderSize, window.HotOrderSize)
	}
	if header.Seq != window.HotSeq {
		t.Fatalf("the header states the last commit %d, want the %d the hot window names", header.Seq, window.HotSeq)
	}
	if header.ValuesSize >= beforeLog || header.OrderSize >= beforeOrder {
		t.Fatalf("the header states the log byte %d and the order byte %d beside the %d and %d the log carried before the move",
			header.ValuesSize, header.OrderSize, beforeLog, beforeOrder)
	}
	if err := shrinkHotIndexAfterArchive(hotDir, manifest); err != nil {
		t.Fatalf("shrink the hot files a second time: %v", err)
	}
	if got := fileSizeForTest(t, filepath.Join(hotDir, journalFileName)); got != logSize {
		t.Fatalf("the second shrink left a log of %d byte(s) beside the %d the first left", got, logSize)
	}
	kv := mustOpenHotView(t, hotDir)
	t.Cleanup(func() { kv.Close() })
	assertHotKeysForTest(t, kv, append(hotSegmentKeysForTest(int(retained[0].id)), "open-a", "open-b"))
	ctx := context.Background()
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "after-a", Value: hotValueForTest("after-a")}}}); err != nil {
		t.Fatalf("commit behind the reopen: %v", err)
	}
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "checkpoint:1:9", Value: hotValueForTest("checkpoint:1:9")}}}); err != nil {
		t.Fatalf("close the interval behind the reopen: %v", err)
	}
	sealed := readSealedSegmentsForTest(t, hotDir)
	if len(sealed) != 2 {
		t.Fatalf("the reopen sealed %d interval(s), want the retained one beside the new one", len(sealed))
	}
	next := sealed[1]
	nextHeader, _, err := journalSegmentHeaderOf(mustOpenSegment(t, next.path), next.path)
	if err != nil {
		t.Fatalf("read the header of %s: %v", next.path, err)
	}
	if nextHeader.FirstSeq != retained[0].lastSeq+1 || nextHeader.LastSeq != window.HotSeq+2 {
		t.Fatalf("the new interval states the commits %d to %d, want the %d to %d the retained interval and the restated header reach",
			nextHeader.FirstSeq, nextHeader.LastSeq, retained[0].lastSeq+1, window.HotSeq+2)
	}
}

// TestGCNeverDeletesASegmentTheManifestDoesNotState pins the refusal of a manifest that skips a hot interval.
//   - a manifest stating an interval without the older interval in front of it is refused as errHotSegmentUnstated
//   - the refused call deletes no segment file and leaves the log and the index files at the bytes they carried
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestGCNeverDeletesASegmentTheManifestDoesNotState(t *testing.T) {
	hotDir, segments := hotShrinkFixture(t, 3)
	if len(segments) != 3 {
		t.Fatalf("the fixture sealed %d interval(s), want the three its cuts closed", len(segments))
	}
	moved := segments[1:2]
	manifest, err := moveSealedSegmentsToArchive(hotDir, t.TempDir(), moved, archiveWatermark{HotSeq: moved[0].lastSeq})
	if err != nil {
		t.Fatalf("move the second interval into the archive: %v", err)
	}
	kept := []string{journalSegmentNameOf(segments[0].id), journalSegmentNameOf(segments[2].id)}
	if names := sealedSegmentsOf(t, hotDir); !equalStringSets(names, kept) {
		t.Fatalf("the hot root keeps %v beside the move, want the %v it did not state", names, kept)
	}
	before := make([]int64, 3)
	files := []string{journalFileName, journalOrderFileName, journalSlotFileName}
	for at, name := range files {
		before[at] = fileSizeForTest(t, filepath.Join(hotDir, name))
	}
	err = shrinkHotIndexAfterArchive(hotDir, manifest)
	if !errors.Is(err, errHotSegmentUnstated) {
		t.Fatalf("the shrink over a manifest that skips a hot interval answered %v, want %v", err, errHotSegmentUnstated)
	}
	if names := sealedSegmentsOf(t, hotDir); !equalStringSets(names, kept) {
		t.Fatalf("the refused shrink left the segment files %v, want the %v the manifest does not state beside the one it leaves hot",
			names, kept)
	}
	for at, name := range files {
		if got := fileSizeForTest(t, filepath.Join(hotDir, name)); got != before[at] {
			t.Fatalf("the refused shrink left %s at %d byte(s) beside the %d it held", name, got, before[at])
		}
	}
}

// TestGCNeverDeletesASegmentTheManifestDoesNotStateDeletesAStatedCopy pins the other side of the same rule.
//   - a hot copy the manifest states is deleted and the log then loses the bytes it states, so the rule refuses only the interval no statement places
//   - the intervals the manifest leaves hot read back byte for byte after the call
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestGCNeverDeletesASegmentTheManifestDoesNotStateDeletesAStatedCopy(t *testing.T) {
	hotDir, segments := hotShrinkFixture(t, 3)
	if len(segments) != 3 {
		t.Fatalf("the fixture sealed %d interval(s), want the three its cuts closed", len(segments))
	}
	moved, retained := segments[:1], segments[1:]
	window := hotWindowAfterMoveForTest(t, hotDir, moved, retained)
	archiveRoot := t.TempDir()
	manifest, err := moveSealedSegmentsToArchive(hotDir, archiveRoot, moved, window)
	if err != nil {
		t.Fatalf("move the oldest interval into the archive: %v", err)
	}
	restoreHotCopy(t, hotDir, archiveRoot, journalSegmentLabelOf(moved[0].id))
	if names := sealedSegmentsOf(t, hotDir); len(names) != 3 {
		t.Fatalf("the restored hot copy leaves %v, want the copied interval beside the two the move kept", names)
	}
	if err := shrinkHotIndexAfterArchive(hotDir, manifest); err != nil {
		t.Fatalf("shrink the hot files beside the stated copy: %v", err)
	}
	kept := []string{journalSegmentNameOf(retained[0].id), journalSegmentNameOf(retained[1].id)}
	if names := sealedSegmentsOf(t, hotDir); !equalStringSets(names, kept) {
		t.Fatalf("the shrunk root keeps %v, want the %v the manifest leaves hot", names, kept)
	}
	keys := append(hotSegmentKeysForTest(int(retained[0].id)), hotSegmentKeysForTest(int(retained[1].id))...)
	keys = append(keys, "open-a", "open-b")
	kv := mustOpenHotView(t, hotDir)
	t.Cleanup(func() { kv.Close() })
	assertHotKeysForTest(t, kv, keys)
}

// mustOpenHotView opens one journal view over a hot directory for a check.
func mustOpenHotView(t *testing.T, dir string) *journalKV {
	t.Helper()
	kv, err := openJournalViewFace(dir, true, segmentTestBudget)
	if err != nil {
		t.Fatalf("open the journal view of %s: %v", dir, err)
	}
	return kv
}
