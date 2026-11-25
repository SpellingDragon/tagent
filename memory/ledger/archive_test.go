package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// pendingArchiveShrinkFixture commits one archive generation and restores its
// hot segment copies without shrinking the log, as a stop before hot GC leaves it.
func pendingArchiveShrinkFixture(t *testing.T, seals, moved int) (string, string, []journalSealedSegment, []journalSealedSegment, archiveWatermark) {
	t.Helper()
	hotDir, segments := hotShrinkFixture(t, seals)
	if moved < 1 || moved >= len(segments) {
		t.Fatalf("the check moves %d of %d interval(s), want at least one on both sides of the move", moved, len(segments))
	}
	leaving, retained := segments[:moved], segments[moved:]
	archiveRoot := t.TempDir()
	window := hotWindowAfterMoveForTest(t, hotDir, leaving, retained)
	if _, err := moveSealedSegmentsToArchive(hotDir, archiveRoot, leaving, window); err != nil {
		t.Fatalf("commit the archive generation: %v", err)
	}
	for _, segment := range leaving {
		restoreHotCopy(t, hotDir, archiveRoot, journalSegmentLabelOf(segment.id))
	}
	return hotDir, archiveRoot, leaving, retained, window
}

// assertArchiveIdempotent pins that one more recovery changes nothing: the stored
// generation and both file sets answer what the run before them stated.
func assertArchiveIdempotent(t *testing.T, hotDir, archiveRoot string) {
	t.Helper()
	before, found, err := readArchiveManifest(archiveRoot)
	if err != nil || !found {
		t.Fatalf("read the stored generation: found=%v err=%v", found, err)
	}
	archiveFiles := archiveFileNames(t, archiveRoot)
	hotFiles := hotSegmentFileNames(t, hotDir)
	if _, err := recoverArchive(hotDir, archiveRoot); err != nil {
		t.Fatalf("recover the archive again: %v", err)
	}
	after, found, err := readArchiveManifest(archiveRoot)
	if err != nil || !found {
		t.Fatalf("read the generation after the second recovery: found=%v err=%v", found, err)
	}
	if after.Generation != before.Generation || len(after.Segments) != len(before.Segments) {
		t.Fatalf("the second recovery stated generation %d with %d segment(s), want the %d with %d the run before stated",
			after.Generation, len(after.Segments), before.Generation, len(before.Segments))
	}
	if got := archiveFileNames(t, archiveRoot); !equalStringSets(got, archiveFiles) {
		t.Fatalf("the second recovery left the archive files %v, want the %v the run before left", got, archiveFiles)
	}
	if got := hotSegmentFileNames(t, hotDir); !equalStringSets(got, hotFiles) {
		t.Fatalf("the second recovery left the hot segment files %v, want the %v the run before left", got, hotFiles)
	}
}

// archiveFileNames answers the file names the archive segment directory holds.
func archiveFileNames(t *testing.T, archiveRoot string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(archiveRoot, journalSegmentDirName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read the archive segment directory: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// hotSegmentFileNames answers the segment files the hot root holds.
func hotSegmentFileNames(t *testing.T, hotDir string) []string {
	t.Helper()
	names := sealedSegmentsOf(t, hotDir)
	indexes := make([]string, 0, len(names))
	for _, name := range names {
		index := name[:len(name)-len(journalSegmentNameSuffix)] + ".idx"
		if _, err := os.Stat(filepath.Join(hotDir, journalSegmentDirName, index)); err == nil {
			indexes = append(indexes, index)
		}
	}
	return append(names, indexes...)
}

// equalStringSets reports whether two name sets hold the same names.
func equalStringSets(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	held := map[string]int{}
	for _, name := range left {
		held[name]++
	}
	for _, name := range right {
		held[name]--
		if held[name] < 0 {
			return false
		}
	}
	return true
}

// TestArchiveCutFrameOffsetNamesItsCut pins the manifest cut offset to the checkpoint naming the segment header's cut.
//   - plain and indexed checkpoint forms select their own frame beside another checkpoint
//   - a header whose partition and sequence name no checkpoint is refused
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestArchiveCutFrameOffsetNamesItsCut(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name      string
		firstKey  string
		cutKey    string
		partition int
		seq       int64
	}{
		{name: "plain", firstKey: "checkpoint:1:1", cutKey: "checkpoint:1:2", partition: 1, seq: 2},
		{name: "indexed", firstKey: "checkpoint:2:1", cutKey: "2:idx:checkpoint:2:2", partition: 2, seq: 2},
		{name: "missing", firstKey: "checkpoint:1:1", partition: 2, seq: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			hotDir := t.TempDir()
			kv, err := openJournalViewFace(hotDir, true, 0)
			if err != nil {
				t.Fatalf("open the journal view: %v", err)
			}
			defer kv.log.close()
			if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: test.firstKey, Value: []byte("first")}}}); err != nil {
				t.Fatalf("commit the first checkpoint: %v", err)
			}
			wantOffset := kv.log.size
			lastKey, lastValue := test.cutKey, []byte("cut")
			if lastKey == "" {
				lastKey, lastValue = "retained", []byte("value")
			}
			if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: lastKey, Value: lastValue}}}); err != nil {
				t.Fatalf("commit the last record: %v", err)
			}
			kv.log.mu.Lock()
			err = kv.log.sealSegment(test.partition, test.seq)
			kv.log.mu.Unlock()
			if err != nil {
				t.Fatalf("seal the segment at its stated cut: %v", err)
			}
			segments := readSealedSegmentsForTest(t, hotDir)
			if len(segments) != 1 {
				t.Fatalf("the fixture sealed %d segment(s), want one", len(segments))
			}
			entry, _, err := archiveSegmentEntryOf(segments[0])
			if test.cutKey == "" {
				if !errors.Is(err, ErrArchiveCorrupt) {
					t.Fatalf("the segment with no named cut = (%+v, %v), want ErrArchiveCorrupt", entry, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("state the archive entry: %v", err)
			}
			cut := entry.Cut
			if cut.Partition != test.partition || cut.Seq != test.seq || cut.FrameOffset != wantOffset {
				t.Fatalf("the archive cut = %+v, want partition=%d seq=%d frame_offset=%d",
					cut, test.partition, test.seq, wantOffset)
			}
		})
	}
}

// TestArchiveAgeKeepsUnknownV1Segments pins unknown commit time as retention rather than a read refusal.
//   - a version one record with no commit-time field states time zero, and a version one segment remains readable
//   - a positive archive age retains that segment, while age zero leaves its movability unchanged
func TestArchiveAgeKeepsUnknownV1Segments(t *testing.T) {
	legacyPayload := []byte(`{"version":1,"puts":[{"key":"legacy","value":"alpha"}]}`)
	spans, err := journalSpansAt(legacyPayload, 0)
	if err != nil {
		t.Fatalf("read a version one record without a commit time: %v", err)
	}
	if spans.version != journalFormatVersionV1 || spans.committedAtNS != 0 {
		t.Fatalf("the legacy record states version=%d time=%d, want version one and unknown time", spans.version, spans.committedAtNS)
	}
	hotDir := t.TempDir()
	kv, err := openJournalViewFace(hotDir, true, segmentTestBudget)
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	if err := kv.log.append(journalRecord{
		Version: journalFormatVersionV1,
		Puts: []journalPut{
			{Key: "legacy", Value: []byte("alpha")},
			{Key: "checkpoint:4:9", Value: []byte("cut")},
		},
	}); err != nil {
		t.Fatalf("append the version one record: %v", err)
	}
	if err := kv.log.sync(); err != nil {
		t.Fatalf("sync the version one record: %v", err)
	}
	kv.log.mu.Lock()
	err = kv.log.sealSegment(4, 9)
	kv.log.mu.Unlock()
	if err != nil {
		t.Fatalf("seal the version one record: %v", err)
	}
	if err := kv.log.close(); err != nil {
		t.Fatalf("close the journal view: %v", err)
	}
	segments := readSealedSegmentsForTest(t, hotDir)
	if len(segments) != 1 {
		t.Fatalf("the fixture sealed %d segment(s), want one", len(segments))
	}
	segment := segments[0]
	header, headerBytes, err := journalSegmentHeaderOf(mustOpenSegment(t, segment.path), segment.path)
	if err != nil {
		t.Fatalf("read the version two header: %v", err)
	}
	legacyHeader, err := json.Marshal(struct {
		Magic        uint32 `json:"magic"`
		Version      int    `json:"version"`
		SegmentID    string `json:"segment_id"`
		FirstSeq     int64  `json:"first_seq"`
		LastSeq      int64  `json:"last_seq"`
		CutPartition int    `json:"cut_partition"`
		CutSeq       int64  `json:"cut_seq"`
		Bytes        int64  `json:"bytes"`
	}{
		Magic:        header.Magic,
		Version:      journalSegmentVersionV1,
		SegmentID:    header.SegmentID,
		FirstSeq:     header.FirstSeq,
		LastSeq:      header.LastSeq,
		CutPartition: header.CutPartition,
		CutSeq:       header.CutSeq,
		Bytes:        header.Bytes,
	})
	if err != nil {
		t.Fatalf("state the version one header: %v", err)
	}
	body, err := os.ReadFile(segment.path)
	if err != nil {
		t.Fatalf("read the segment file: %v", err)
	}
	rewritten := append(journalFrameOf(legacyHeader), body[headerBytes:]...)
	if err := os.WriteFile(segment.path, rewritten, 0o600); err != nil {
		t.Fatalf("retain the segment with a version one header: %v", err)
	}
	segments = readSealedSegmentsForTest(t, hotDir)
	segment = segments[0]
	if segment.version != journalSegmentVersionV1 || segment.commitTimeMinNS != 0 || segment.commitTimeMaxNS != 0 {
		t.Fatalf("the legacy segment states version=%d times=%d..%d, want version one and unknown time",
			segment.version, segment.commitTimeMinNS, segment.commitTimeMaxNS)
	}
	now := time.Now()
	if !segment.archiveMovableAt(now, 0) {
		t.Fatalf("age zero retains a time window over an unknown-time segment")
	}
	if segment.archiveMovableAt(now, time.Hour) {
		t.Fatalf("a positive age moves a segment whose commit time is unknown")
	}
	view, err := openJournalViewWithArchive(hotDir, false, 0, "", time.Hour)
	if err != nil {
		t.Fatalf("open the legacy segment beside a positive archive age: %v", err)
	}
	t.Cleanup(func() { view.log.close() })
	value, held := readOne(t, view, "legacy")
	if !held || string(value) != "alpha" {
		t.Fatalf("the legacy segment reads (%q, %t), want (alpha, true)", value, held)
	}
}

// TestArchiveMovableAtUsesNewestCommit pins the conservative archive-age comparison.
//   - the whole range moves only when its newest commit is strictly earlier than now-age
//   - a commit at the cutoff or in the future stays, and an unknown version two range stays
func TestArchiveMovableAtUsesNewestCommit(t *testing.T) {
	now := time.Unix(2_000_000_000, 0)
	age := time.Hour
	cutoff := now.Add(-age).UnixNano()
	for _, test := range []struct {
		name    string
		segment journalSealedSegment
		age     time.Duration
		want    bool
	}{
		{name: "older", segment: journalSealedSegment{version: journalSegmentVersion, commitTimeMinNS: cutoff - 2, commitTimeMaxNS: cutoff - 1}, age: age, want: true},
		{name: "at_cutoff", segment: journalSealedSegment{version: journalSegmentVersion, commitTimeMinNS: cutoff - 1, commitTimeMaxNS: cutoff}, age: age},
		{name: "future", segment: journalSealedSegment{version: journalSegmentVersion, commitTimeMinNS: cutoff + 1, commitTimeMaxNS: cutoff + 2}, age: age},
		{name: "unknown_v2", segment: journalSealedSegment{version: journalSegmentVersion}, age: age},
		{name: "no_age", segment: journalSealedSegment{version: journalSegmentVersionV1}, age: 0, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.segment.archiveMovableAt(now, test.age); got != test.want {
				t.Fatalf("archiveMovableAt(now, %s) = %t, want %t", test.age, got, test.want)
			}
		})
	}
}

// TestArchiveAgeIgnoresOrderedAppendTime pins the quiet marker beside a real commit.
//   - an ordered append states no commit time of its own and does not make the segment's committed range unknown
//   - the segment's commit range comes from the checkpoint commit and moves once that range is older than the age
func TestArchiveAgeIgnoresOrderedAppendTime(t *testing.T) {
	hotDir := t.TempDir()
	kv, err := openJournalViewFace(hotDir, true, segmentTestBudget)
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	t.Cleanup(func() { kv.log.close() })
	ctx := context.Background()
	if err := kv.AppendOrdered(ctx, Batch{Puts: []Pair{{Key: "tagent/checkpoint/v1/quiet:1:7", Value: []byte("quiet")}}}); err != nil {
		t.Fatalf("append the quiet marker of the cut: %v", err)
	}
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "checkpoint:1:7", Value: []byte("cut")}}}); err != nil {
		t.Fatalf("commit the checkpoint that closes the segment: %v", err)
	}
	segments := readSealedSegmentsForTest(t, hotDir)
	if len(segments) != 1 {
		t.Fatalf("the cut sealed %d segment(s), want the one carrying the quiet marker and checkpoint", len(segments))
	}
	segment := segments[0]
	if segment.commitTimeMinNS == 0 || segment.commitTimeMinNS != segment.commitTimeMaxNS {
		t.Fatalf("the segment states the commit range %d..%d, want the checkpoint's one known commit time", segment.commitTimeMinNS, segment.commitTimeMaxNS)
	}
	if !segment.archiveMovableAt(time.Now().Add(2*time.Hour), time.Hour) {
		t.Fatalf("the segment stays beside an ordered append that confirms no commit, want it movable once the checkpoint commit is old")
	}
}

// archiveMoveStop names the point a move stops at: the staged copies completed by
// their barriers, the generation stated, the segment name taken, or the index name
// taken beside a hot delete in flight.
type archiveMoveStop int

const (
	archiveStopStaged archiveMoveStop = iota
	archiveStopStated
	archiveStopSegmentNamed
	archiveStopIndexNamed
)

// archivedMoveStoppedAt stages one move of the oldest interval of a fresh hot root
// and stops it at the named point, so a check starts from the files a stop leaves.
func archivedMoveStoppedAt(t *testing.T, stop archiveMoveStop) (hotDir, archiveRoot string, moving, retained []journalSealedSegment, segment archiveSegment, window archiveWatermark) {
	t.Helper()
	hotDir, segments := hotShrinkFixture(t, 2)
	moving, retained = segments[:1], segments[1:]
	archiveRoot = t.TempDir()
	window = hotWindowAfterMoveForTest(t, hotDir, moving, retained)
	entry, indexFrame, err := archiveSegmentEntryOf(moving[0])
	if err != nil {
		t.Fatalf("state the archive entry of %s: %v", journalSegmentLabelOf(moving[0].id), err)
	}
	entry.Watermark = window
	segmentPath := archiveSegmentPathOf(archiveRoot, entry.SegmentID)
	indexPath, err := archiveIndexPathOf(archiveRoot, entry)
	if err != nil {
		t.Fatalf("read the stated index name: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(archiveRoot, journalSegmentDirName), 0o755); err != nil {
		t.Fatalf("open the archive segment directory: %v", err)
	}
	if err := stageArchiveCopy(moving[0].path, segmentPath); err != nil {
		t.Fatalf("stage the segment copy: %v", err)
	}
	if err := stageArchiveBody(indexPath, indexFrame); err != nil {
		t.Fatalf("stage the index copy: %v", err)
	}
	if stop == archiveStopStaged {
		return hotDir, archiveRoot, moving, retained, entry, window
	}
	stated := archiveManifest{Segments: []archiveSegment{entry}, ArchivedThroughCut: entry.Cut}
	if err := writeArchiveManifest(archiveRoot, stated); err != nil {
		t.Fatalf("state the generation of the stopped move: %v", err)
	}
	if stop == archiveStopStated {
		return hotDir, archiveRoot, moving, retained, entry, window
	}
	if err := promoteStagedArchiveFile(segmentPath); err != nil {
		t.Fatalf("take the segment name: %v", err)
	}
	if stop == archiveStopSegmentNamed {
		return hotDir, archiveRoot, moving, retained, entry, window
	}
	if err := promoteStagedArchiveFile(indexPath); err != nil {
		t.Fatalf("take the index name: %v", err)
	}
	if err := os.Remove(moving[0].path); err != nil {
		t.Fatalf("delete the hot segment copy of a delete in flight: %v", err)
	}
	return hotDir, archiveRoot, moving, retained, entry, window
}

// assertStatedMoveSettled pins one settled move: the stated names carry the stated
// bytes, the hot copies are gone, the hot log states the stated window, and the
// archived key reads back through the archive alone.
func assertStatedMoveSettled(t *testing.T, hotDir, archiveRoot string, segment archiveSegment, retained []journalSealedSegment, window archiveWatermark) {
	t.Helper()
	segmentPath := archiveSegmentPathOf(archiveRoot, segment.SegmentID)
	digest, size, err := digestOfFile(segmentPath)
	if err != nil || size != segment.Bytes || digest != segment.SHA256 {
		t.Fatalf("the stated segment = %d byte(s) %s err=%v, want the %d byte(s) %s the manifest states",
			size, digest, err, segment.Bytes, segment.SHA256)
	}
	indexPath, err := archiveIndexPathOf(archiveRoot, segment)
	if err != nil {
		t.Fatalf("read the stated index name: %v", err)
	}
	indexFrame, err := os.ReadFile(indexPath)
	if err != nil || digestOf(indexFrame) != segment.OffsetMap.IndexSHA256 {
		t.Fatalf("the stated index err=%v digest=%s, want the %s the manifest states", err, digestOf(indexFrame), segment.OffsetMap.IndexSHA256)
	}
	if names := archiveFileNames(t, archiveRoot); !equalStringSets(names, []string{segment.SegmentID + journalSegmentNameSuffix, segment.OffsetMap.IndexPath}) {
		t.Fatalf("the archive holds %v, want the stated segment and its index alone", names)
	}
	kept := make([]string, 0, len(retained))
	for _, interval := range retained {
		kept = append(kept, journalSegmentNameOf(interval.id))
	}
	if names := sealedSegmentsOf(t, hotDir); !equalStringSets(names, kept) {
		t.Fatalf("the hot root keeps %v, want the %v the manifest leaves behind", names, kept)
	}
	if got := fileSizeForTest(t, filepath.Join(hotDir, journalFileName)); got != window.HotValuesSize {
		t.Fatalf("the hot log holds %d byte(s), want the %d the stated window names", got, window.HotValuesSize)
	}
	view, err := openJournalViewWithArchive(hotDir, false, 0, archiveRoot, 0)
	if err != nil {
		t.Fatalf("open the view over the settled move: %v", err)
	}
	t.Cleanup(func() { view.Close() })
	for _, key := range hotSegmentKeysForTest(int(segmentIDOfLabel(t, segment.SegmentID))) {
		value, held := readOne(t, view, key)
		if !held || !bytes.Equal(value, hotValueForTest(key)) {
			t.Fatalf("the archived read of %q = (%q, %t), want the committed bytes", key, value, held)
		}
	}
}

// segmentIDOfLabel answers the id one stated segment label names.
func segmentIDOfLabel(t *testing.T, label string) int64 {
	t.Helper()
	id, held := journalSegmentIDOf(label + journalSegmentNameSuffix)
	if !held {
		t.Fatalf("the label %q states no segment", label)
	}
	return id
}

// TestArchiveCrashRecoveryIsIdempotent pins the four stops one move is recovered from.
//   - staged copies before the statement: no generation names them, so they are removed, every hot copy stands, and a second recovery changes nothing.
//   - the statement before the names: the staged segment and index take the stated names, the hot copies go, and a second recovery changes nothing.
//   - the segment name taken: the staged index takes its stated name, the hot copies go, and a second recovery changes nothing.
//   - the index name taken with the hot delete in flight: the remaining hot copy goes and a second recovery changes nothing.
//   - the three stops after the statement settle the same way through the digest recovery, which reads every stated byte.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestArchiveCrashRecoveryIsIdempotent(t *testing.T) {
	t.Run("staged_before_the_statement", func(t *testing.T) {
		hotDir, archiveRoot, moving, retained, _, window := archivedMoveStoppedAt(t, archiveStopStaged)
		before := journalTestRootState(t, hotDir)
		if _, found, err := readArchiveManifest(archiveRoot); found || err != nil {
			t.Fatalf("the root stopped before the statement reads found=%v err=%v, want the empty archive beside no error", found, err)
		}
		if _, err := recoverArchive(hotDir, archiveRoot); err != nil {
			t.Fatalf("recover the archive of a staged move: %v", err)
		}
		if names := archiveFileNames(t, archiveRoot); len(names) != 0 {
			t.Fatalf("the archive holds %v after the recovery, want no copy no generation names", names)
		}
		journalTestRequireSameFiles(t, before, journalTestRootState(t, hotDir))
		if err := convergeArchiveStructure(hotDir, archiveRoot); err != nil {
			t.Fatalf("converge the archive of a staged move: %v", err)
		}
		journalTestRequireSameFiles(t, before, journalTestRootState(t, hotDir))
		stored, err := moveSealedSegmentsToArchive(hotDir, archiveRoot, moving, window)
		if err != nil {
			t.Fatalf("move the interval the stop left hot: %v", err)
		}
		if err := shrinkHotIndexAfterArchive(hotDir, stored); err != nil {
			t.Fatalf("shrink the hot files after the repeated move: %v", err)
		}
		assertStatedMoveSettled(t, hotDir, archiveRoot, stored.Segments[0], retained, window)
		assertArchiveIdempotent(t, hotDir, archiveRoot)
	})

	for _, test := range []struct {
		name string
		stop archiveMoveStop
	}{
		{name: "statement_before_the_names", stop: archiveStopStated},
		{name: "segment_named_index_staged", stop: archiveStopSegmentNamed},
		{name: "index_named_hot_delete_in_flight", stop: archiveStopIndexNamed},
	} {
		t.Run(test.name, func(t *testing.T) {
			hotDir, archiveRoot, _, retained, segment, window := archivedMoveStoppedAt(t, test.stop)
			if err := convergeArchiveStructure(hotDir, archiveRoot); err != nil {
				t.Fatalf("converge the archive of a stopped move: %v", err)
			}
			assertStatedMoveSettled(t, hotDir, archiveRoot, segment, retained, window)
			assertArchiveIdempotent(t, hotDir, archiveRoot)
		})
	}

	for _, test := range []struct {
		name string
		stop archiveMoveStop
	}{
		{name: "statement_before_the_names_by_digests", stop: archiveStopStated},
		{name: "segment_named_index_staged_by_digests", stop: archiveStopSegmentNamed},
		{name: "index_named_hot_delete_in_flight_by_digests", stop: archiveStopIndexNamed},
	} {
		t.Run(test.name, func(t *testing.T) {
			hotDir, archiveRoot, _, retained, segment, window := archivedMoveStoppedAt(t, test.stop)
			stored, err := recoverArchive(hotDir, archiveRoot)
			if err != nil {
				t.Fatalf("recover the archive of a stopped move by its digests: %v", err)
			}
			if err := shrinkHotIndexAfterArchive(hotDir, stored); err != nil {
				t.Fatalf("shrink the hot files after the recovery: %v", err)
			}
			assertStatedMoveSettled(t, hotDir, archiveRoot, segment, retained, window)
			assertArchiveIdempotent(t, hotDir, archiveRoot)
		})
	}
}

// restoreHotCopy puts one archived segment back into the hot root, so a check
// stands where a stop between the manifest and the hot delete leaves the root.
func restoreHotCopy(t *testing.T, hotDir, archiveRoot, label string) {
	t.Helper()
	source := filepath.Join(archiveRoot, journalSegmentDirName, label+journalSegmentNameSuffix)
	body, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read the archived segment %s: %v", label, err)
	}
	target := filepath.Join(hotDir, journalSegmentDirName, label+journalSegmentNameSuffix)
	if err := os.WriteFile(target, body, 0o600); err != nil {
		t.Fatalf("restore the hot copy %s: %v", label, err)
	}
}

// TestBootArchiveConvergenceRefusesToShrinkLogBehindCorruptArchive pins the last-copy guard after hot segment deletion.
//   - a stop after deleting the hot segment but before shrinking the journal still leaves the only good bytes in the log
//   - a same-size corrupt archive is ErrArchiveCorrupt, and the refused boot leaves the journal byte for byte
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestBootArchiveConvergenceRefusesToShrinkLogBehindCorruptArchive(t *testing.T) {
	hotDir, archiveRoot, leaving, _, _ := pendingArchiveShrinkFixture(t, 2, 1)
	hotPath := filepath.Join(hotDir, journalSegmentDirName, journalSegmentNameOf(leaving[0].id))
	if err := os.Remove(hotPath); err != nil {
		t.Fatalf("remove the hot segment before the pending shrink: %v", err)
	}
	archived := filepath.Join(archiveRoot, journalSegmentDirName, journalSegmentNameOf(leaving[0].id))
	flipByteAt(t, archived, fileSizeForTest(t, archived)/2)
	logPath := filepath.Join(hotDir, journalFileName)
	before, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read the journal before boot: %v", err)
	}
	if _, err := openJournalViewWithArchive(hotDir, true, segmentTestBudget, archiveRoot, 0); !errors.Is(err, ErrArchiveCorrupt) {
		t.Fatalf("boot over a corrupt archive beside the only journal copy = %v, want ErrArchiveCorrupt", err)
	}
	after, err := os.ReadFile(logPath)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("the refused boot changed the journal: %v", err)
	}
}

// TestBootConvergesArchiveWithoutPrescan pins structural boot recovery.
//   - a settled archive with no residual hot copy does not fail boot over same-size archive corruption
//   - the integrity scan covers only the hot window, and the corrupted archive stays untouched
//   - reading the corrupted archived key returns ErrArchiveCorrupt while a retained hot key still reads
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestBootConvergesArchiveWithoutPrescan(t *testing.T) {
	ctx := context.Background()
	hotDir, archiveRoot, leaving, retained := archivedViewFixture(t, 2, 1)
	writer, err := openJournalViewWithArchive(hotDir, true, 0, archiveRoot, 0)
	if err != nil {
		t.Fatalf("open the settled writer: %v", err)
	}
	if err := writer.Commit(ctx, Batch{Puts: []Pair{{Key: "after-archive", Value: []byte("value")}}}); err != nil {
		t.Fatalf("commit after the settled archive: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close the settled writer: %v", err)
	}
	hotBytes := fileSizeForTest(t, filepath.Join(hotDir, journalFileName))
	archivedPath := filepath.Join(archiveRoot, journalSegmentDirName, journalSegmentNameOf(leaving[0].id))
	flipByteAt(t, archivedPath, fileSizeForTest(t, archivedPath)/2)
	kv, err := openJournalViewWithArchive(hotDir, true, segmentTestBudget, archiveRoot, 0)
	if err != nil {
		t.Fatalf("boot over same-size archive corruption without a hot copy: %v", err)
	}
	t.Cleanup(func() { kv.Close() })
	if got := fileSizeForTest(t, filepath.Join(hotDir, journalFileName)); got != hotBytes {
		t.Fatalf("boot left %d hot log byte(s), want the settled %d-byte hot window", got, hotBytes)
	}
	if got := kv.IntegrityBytesScanned(); got != hotBytes {
		t.Fatalf("boot verified %d log byte(s), want only the %d-byte hot window", got, hotBytes)
	}
	segments := readSealedSegmentsForTest(t, hotDir)
	if len(segments) != 1 || segments[0].id != retained[0].id {
		t.Fatalf("boot kept hot segments %+v, want only the retained interval", segments)
	}
	archivedKey := hotSegmentKeysForTest(int(leaving[0].id))[0]
	pairs, err := kv.Read(context.Background(), Fetch{Key: archivedKey})
	if !errors.Is(err, ErrArchiveCorrupt) || errors.Is(err, ErrArchiveMissing) || errors.Is(err, ErrOriginalMissing) || len(pairs) != 0 {
		t.Fatalf("the corrupted archive read = (%+v, %v), want ErrArchiveCorrupt alone", pairs, err)
	}
	hotKey := hotSegmentKeysForTest(int(retained[0].id))[0]
	pairs, err = kv.Read(context.Background(), Fetch{Key: hotKey})
	if err != nil || len(pairs) != 1 || string(pairs[0].Value) != string(hotValueForTest(hotKey)) {
		t.Fatalf("the retained hot read = (%+v, %v), want its committed bytes", pairs, err)
	}
}

// TestBootConvergenceReadsOnlyLatestArchiveGeneration pins bounded boot recovery.
//   - an older settled generation may be absent without blocking the hot window's boot
//   - reading the absent older segment still names ErrArchiveMissing
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestBootConvergenceReadsOnlyLatestArchiveGeneration(t *testing.T) {
	hotDir, segments := hotShrinkFixture(t, 4)
	archiveRoot := t.TempDir()
	window := hotWindowAfterMoveForTest(t, hotDir, segments[:1], segments[1:])
	stored, err := moveSealedSegmentsToArchive(hotDir, archiveRoot, segments[:1], window)
	if err != nil {
		t.Fatalf("state the first archive generation: %v", err)
	}
	if err := shrinkHotIndexAfterArchive(hotDir, stored); err != nil {
		t.Fatalf("shrink behind the first archive generation: %v", err)
	}
	window = hotWindowAfterMoveForTest(t, hotDir, segments[1:2], segments[2:])
	stored, err = moveSealedSegmentsToArchive(hotDir, archiveRoot, segments[1:2], window)
	if err != nil {
		t.Fatalf("state the second archive generation: %v", err)
	}
	if err := shrinkHotIndexAfterArchive(hotDir, stored); err != nil {
		t.Fatalf("shrink behind the second archive generation: %v", err)
	}
	olderPath := archiveSegmentPathOf(archiveRoot, journalSegmentLabelOf(segments[0].id))
	if err := os.Remove(olderPath); err != nil {
		t.Fatalf("remove the settled older segment: %v", err)
	}
	if err := os.WriteFile(olderPath+journalSegmentTempSuffix, []byte("stale"), 0o600); err != nil {
		t.Fatalf("leave a staged copy of the older generation: %v", err)
	}
	kv, err := openJournalViewWithArchive(hotDir, true, 0, archiveRoot, 0)
	if err != nil {
		t.Fatalf("boot beside an absent older archive generation: %v", err)
	}
	t.Cleanup(func() { kv.Close() })
	if _, err := os.Stat(olderPath + journalSegmentTempSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("boot left the older generation's staged copy: %v", err)
	}
	key := hotSegmentKeysForTest(int(segments[0].id))[0]
	value, held, err := (TierFacts{kv: kv}).Get(context.Background(), key)
	if !errors.Is(err, ErrArchiveMissing) || held || value != nil {
		t.Fatalf("the older absent segment read = (%q, %t, %v), want ErrArchiveMissing", value, held, err)
	}
}

// TestBootArchiveConvergenceRefusesToDeleteHotCopyBehindCorruptArchive pins the last-copy guard.
//   - a same-size corrupt segment or index beside its hot copy is ErrArchiveCorrupt
//   - the refused boot preserves the hot segment, its indexes and the hot log byte for byte
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestBootArchiveConvergenceRefusesToDeleteHotCopyBehindCorruptArchive(t *testing.T) {
	for _, artifact := range []string{"segment", "index"} {
		t.Run(artifact, func(t *testing.T) {
			hotDir, archiveRoot, leaving, _, _ := pendingArchiveShrinkFixture(t, 2, 1)
			manifest, found, err := readArchiveManifest(archiveRoot)
			if err != nil || !found || len(manifest.Segments) != 1 {
				t.Fatalf("read the stated generation: found=%v segments=%+v err=%v", found, manifest, err)
			}
			stated := manifest.Segments[0]
			archived := archiveSegmentPathOf(archiveRoot, stated.SegmentID)
			if artifact == "index" {
				archived, err = archiveIndexPathOf(archiveRoot, stated)
				if err != nil {
					t.Fatalf("read the stated index name: %v", err)
				}
			}
			flipByteAt(t, archived, fileSizeForTest(t, archived)/2)
			hotPath := filepath.Join(hotDir, journalSegmentDirName, journalSegmentNameOf(leaving[0].id))
			hotBody, err := os.ReadFile(hotPath)
			if err != nil {
				t.Fatalf("read the hot copy before boot: %v", err)
			}
			beforeNames := hotSegmentFileNames(t, hotDir)
			beforeLog := fileSizeForTest(t, filepath.Join(hotDir, journalFileName))
			if _, err := openJournalViewWithArchive(hotDir, true, segmentTestBudget, archiveRoot, 0); !errors.Is(err, ErrArchiveCorrupt) {
				t.Fatalf("boot over a corrupt archived %s beside its hot copy = %v, want ErrArchiveCorrupt", artifact, err)
			}
			after, err := os.ReadFile(hotPath)
			if err != nil || !bytes.Equal(after, hotBody) {
				t.Fatalf("the refused boot changed the hot segment: %v", err)
			}
			if got := hotSegmentFileNames(t, hotDir); !equalStringSets(got, beforeNames) {
				t.Fatalf("the refused boot left hot files %v, want %v", got, beforeNames)
			}
			if got := fileSizeForTest(t, filepath.Join(hotDir, journalFileName)); got != beforeLog {
				t.Fatalf("the refused boot left %d hot log byte(s), want %d", got, beforeLog)
			}
		})
	}
}

// TestBootArchiveConvergenceNeverReadsTheStatedIndex pins the structural mode of a settled stated index check.
//   - a stated index whose bytes contradict the stated digest is placed by its name alone, so boot pays no pass over the archived keys
//   - a stated index that cannot be read as one file is placed the same way, and boot names neither refusal
//   - the boot leaves the index as it met it, while move recovery refuses both as ErrArchiveCorrupt
//   - a point read names the contradiction, never an absent key
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestBootArchiveConvergenceNeverReadsTheStatedIndex(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name   string
		damage func(t *testing.T, indexPath string)
	}{
		{name: "bytes_contradict_the_stated_digest", damage: func(t *testing.T, indexPath string) {
			flipByteAt(t, indexPath, journalFrameHeaderBytes+8)
		}},
		{name: "cannot_be_read_as_one_file", damage: func(t *testing.T, indexPath string) {
			if err := os.Remove(indexPath); err != nil {
				t.Fatalf("remove the stated index file: %v", err)
			}
			if err := os.Mkdir(indexPath, 0o755); err != nil {
				t.Fatalf("stand a directory where the stated index stood: %v", err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			hotDir, archiveRoot, leaving, _ := archivedViewFixture(t, 2, 1)
			manifest, found, err := readArchiveManifest(archiveRoot)
			if err != nil || !found || len(manifest.Segments) != 1 {
				t.Fatalf("read the stated generation: found=%v segments=%+v err=%v", found, manifest, err)
			}
			indexPath, err := archiveIndexPathOf(archiveRoot, manifest.Segments[0])
			if err != nil {
				t.Fatalf("read the stated index name: %v", err)
			}
			test.damage(t, indexPath)
			before := archiveFileNames(t, archiveRoot)
			kv, err := openJournalViewWithArchive(hotDir, true, segmentTestBudget, archiveRoot, 0)
			if err != nil {
				t.Fatalf("boot over a stated index the structural mode only stats: %v", err)
			}
			if err := kv.Close(); err != nil {
				t.Fatalf("close the booted view: %v", err)
			}
			if got := archiveFileNames(t, archiveRoot); !equalStringSets(got, before) {
				t.Fatalf("the boot left %v, want the %v it met", got, before)
			}
			if _, err := recoverArchive(hotDir, archiveRoot); !errors.Is(err, ErrArchiveCorrupt) {
				t.Fatalf("recovery of a stated index whose bytes this mode reads = %v, want ErrArchiveCorrupt", err)
			}
			if got := archiveFileNames(t, archiveRoot); !equalStringSets(got, before) {
				t.Fatalf("the refusal left %v, want the %v it met", got, before)
			}
			view, err := openJournalViewWithArchive(hotDir, false, 0, archiveRoot, 0)
			if err != nil {
				t.Fatalf("open the view beside the refused index: %v", err)
			}
			t.Cleanup(func() { view.Close() })
			key := hotSegmentKeysForTest(int(leaving[0].id))[0]
			value, held, err := (TierFacts{kv: view}).Get(ctx, key)
			if !errors.Is(err, ErrArchiveCorrupt) || errors.Is(err, ErrArchiveMissing) || errors.Is(err, ErrOriginalMissing) || held || value != nil {
				t.Fatalf("the read of the stated index = (%q, %t, %v), want ErrArchiveCorrupt alone", value, held, err)
			}
		})
	}
}

// TestBootArchiveConvergenceChecksSegmentStat pins fail-closed structural checks.
//   - a stated segment with no named, staged or hot copy is ErrArchiveMissing and a different stat size is ErrArchiveCorrupt
//   - either refusal leaves every hot file at the bytes and names it held before boot
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestBootArchiveConvergenceChecksSegmentStat(t *testing.T) {
	cases := []struct {
		name   string
		damage func(t *testing.T, archived, hot string)
		want   error
	}{
		{name: "missing", damage: func(t *testing.T, archived, hot string) {
			if err := os.Remove(archived); err != nil {
				t.Fatalf("remove the stated segment: %v", err)
			}
			if err := os.Remove(hot); err != nil {
				t.Fatalf("remove the hot copy of the stated segment: %v", err)
			}
		}, want: ErrArchiveMissing},
		{name: "wrong_size", damage: func(t *testing.T, archived, _ string) {
			body, err := os.ReadFile(archived)
			if err != nil {
				t.Fatalf("read the stated segment: %v", err)
			}
			if err := os.WriteFile(archived, append(body, 0), 0o600); err != nil {
				t.Fatalf("change the stated segment size: %v", err)
			}
		}, want: ErrArchiveCorrupt},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hotDir, archiveRoot, leaving, _, _ := pendingArchiveShrinkFixture(t, 2, 1)
			path := filepath.Join(archiveRoot, journalSegmentDirName, journalSegmentNameOf(leaving[0].id))
			hotPath := filepath.Join(hotDir, journalSegmentDirName, journalSegmentNameOf(leaving[0].id))
			tc.damage(t, path, hotPath)
			beforeNames := hotSegmentFileNames(t, hotDir)
			beforeLog := fileSizeForTest(t, filepath.Join(hotDir, journalFileName))
			if _, err := openJournalViewWithArchive(hotDir, true, segmentTestBudget, archiveRoot, 0); !errors.Is(err, tc.want) {
				t.Fatalf("boot over %s archive bytes = %v, want %v", tc.name, err, tc.want)
			}
			if got := hotSegmentFileNames(t, hotDir); !equalStringSets(got, beforeNames) {
				t.Fatalf("the refused boot left hot files %v, want %v", got, beforeNames)
			}
			if got := fileSizeForTest(t, filepath.Join(hotDir, journalFileName)); got != beforeLog {
				t.Fatalf("the refused boot left %d log byte(s), want %d", got, beforeLog)
			}
		})
	}
}

// TestBootArchiveConvergenceCompletesAStatedNameFromTheHotCopy pins the completion of a stated name a stop left empty.
//   - a stated segment whose named file is absent and whose hot copy stands boots into the bytes the manifest states
//   - the covered hot copy goes, the hot log states the stated window, and the archived key reads back
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestBootArchiveConvergenceCompletesAStatedNameFromTheHotCopy(t *testing.T) {
	hotDir, archiveRoot, leaving, retained, window := pendingArchiveShrinkFixture(t, 2, 1)
	manifest, found, err := readArchiveManifest(archiveRoot)
	if err != nil || !found || len(manifest.Segments) != 1 {
		t.Fatalf("read the stated generation: found=%v segments=%+v err=%v", found, manifest, err)
	}
	stated := manifest.Segments[0]
	archived := archiveSegmentPathOf(archiveRoot, stated.SegmentID)
	if err := os.Remove(archived); err != nil {
		t.Fatalf("remove the stated segment file: %v", err)
	}
	kv, err := openJournalViewWithArchive(hotDir, true, segmentTestBudget, archiveRoot, 0)
	if err != nil {
		t.Fatalf("boot over a stated segment whose hot copy stands: %v", err)
	}
	t.Cleanup(func() { kv.Close() })
	digest, size, err := digestOfFile(archived)
	if err != nil || size != stated.Bytes || digest != stated.SHA256 {
		t.Fatalf("the completed segment = %d byte(s) %s err=%v, want the %d byte(s) %s the manifest states",
			size, digest, err, stated.Bytes, stated.SHA256)
	}
	kept := []string{journalSegmentNameOf(retained[0].id)}
	if names := sealedSegmentsOf(t, hotDir); !equalStringSets(names, kept) {
		t.Fatalf("the hot root keeps %v, want the %v the manifest leaves behind", names, kept)
	}
	if got := fileSizeForTest(t, filepath.Join(hotDir, journalFileName)); got != window.HotValuesSize {
		t.Fatalf("the hot log holds %d byte(s), want the %d the stated window names", got, window.HotValuesSize)
	}
	for _, key := range hotSegmentKeysForTest(int(leaving[0].id)) {
		value, held := readOne(t, kv, key)
		if !held || !bytes.Equal(value, hotValueForTest(key)) {
			t.Fatalf("the completed archive read of %q = (%q, %t), want the committed bytes", key, value, held)
		}
	}
}

// TestArchiveWithoutManifestBesideNamedArtifactIsCorrupt pins a named file beside a lost statement.
//   - removing both manifest slots leaves named segment and index files, which read as ErrArchiveCorrupt and never as the empty archive
//   - boot, move recovery and a point read name the corruption, and no named byte is removed or rewritten
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestArchiveWithoutManifestBesideNamedArtifactIsCorrupt(t *testing.T) {
	ctx := context.Background()
	hotDir, archiveRoot, leaving, _ := archivedViewFixture(t, 2, 1)
	named := []string{
		journalSegmentNameOf(leaving[0].id),
		journalSegmentLabelOf(leaving[0].id) + journalSegmentIndexSuffix,
	}
	for _, name := range []string{archiveManifestAName, archiveManifestBName} {
		if err := os.Remove(filepath.Join(archiveRoot, name)); err != nil {
			t.Fatalf("remove the manifest slot %s: %v", name, err)
		}
	}
	if got := archiveFileNames(t, archiveRoot); !equalStringSets(got, named) {
		t.Fatalf("the lost statement leaves %v, want the named segment and index alone", got)
	}
	if _, found, err := readArchiveManifest(archiveRoot); !errors.Is(err, ErrArchiveCorrupt) || found {
		t.Fatalf("named archive files beside no manifest read found=%v err=%v, want ErrArchiveCorrupt", found, err)
	}
	if err := convergeArchiveStructure(hotDir, archiveRoot); !errors.Is(err, ErrArchiveCorrupt) {
		t.Fatalf("boot over named archive files beside no manifest = %v, want ErrArchiveCorrupt", err)
	}
	if _, err := recoverArchive(hotDir, archiveRoot); !errors.Is(err, ErrArchiveCorrupt) {
		t.Fatalf("recovery of named archive files beside no manifest = %v, want ErrArchiveCorrupt", err)
	}
	if got := archiveFileNames(t, archiveRoot); !equalStringSets(got, named) {
		t.Fatalf("the refusals left %v, want the named segment and index they met", got)
	}
	view, err := openJournalViewWithArchive(hotDir, false, 0, archiveRoot, 0)
	if err != nil {
		t.Fatalf("open the view beside the lost statement: %v", err)
	}
	t.Cleanup(func() { view.Close() })
	key := hotSegmentKeysForTest(int(leaving[0].id))[0]
	value, held, err := (TierFacts{kv: view}).Get(ctx, key)
	if !errors.Is(err, ErrArchiveCorrupt) || errors.Is(err, ErrOriginalMissing) || errors.Is(err, ErrArchiveMissing) || held || value != nil {
		t.Fatalf("the read beside the lost statement = (%q, %t, %v), want ErrArchiveCorrupt alone", value, held, err)
	}
}

// TestArchiveManifestRefusesUnstatedNamedArtifact pins the statement boundary of a valid generation.
//   - a final segment or index name the greatest generation does not state is ErrArchiveCorrupt
//   - manifest reads, boot, move recovery and point reads refuse it without removing the artifact
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestArchiveManifestRefusesUnstatedNamedArtifact(t *testing.T) {
	ctx := context.Background()
	for _, artifact := range []string{"segment", "index"} {
		t.Run(artifact, func(t *testing.T) {
			hotDir, archiveRoot, leaving, retained := archivedViewFixture(t, 2, 1)
			target := archiveSegmentPathOf(archiveRoot, journalSegmentLabelOf(retained[0].id))
			source := retained[0].path
			if artifact == "index" {
				source = journalSegmentIndexPathOf(retained[0])
				target = filepath.Join(archiveRoot, journalSegmentDirName, filepath.Base(source))
			}
			if err := stageArchiveCopy(source, target); err != nil {
				t.Fatalf("stage the unstated %s: %v", artifact, err)
			}
			if err := promoteStagedArchiveFile(target); err != nil {
				t.Fatalf("name the unstated %s: %v", artifact, err)
			}
			before := archiveFileNames(t, archiveRoot)
			if _, found, err := readArchiveManifest(archiveRoot); !errors.Is(err, ErrArchiveCorrupt) || found {
				t.Fatalf("the unstated %s reads found=%v err=%v, want ErrArchiveCorrupt", artifact, found, err)
			}
			if err := convergeArchiveStructure(hotDir, archiveRoot); !errors.Is(err, ErrArchiveCorrupt) {
				t.Fatalf("boot beside the unstated %s = %v, want ErrArchiveCorrupt", artifact, err)
			}
			if _, err := recoverArchive(hotDir, archiveRoot); !errors.Is(err, ErrArchiveCorrupt) {
				t.Fatalf("recovery beside the unstated %s = %v, want ErrArchiveCorrupt", artifact, err)
			}
			view, err := openJournalViewWithArchive(hotDir, false, 0, archiveRoot, 0)
			if err != nil {
				t.Fatalf("open the read view beside the unstated %s: %v", artifact, err)
			}
			t.Cleanup(func() { view.Close() })
			key := hotSegmentKeysForTest(int(leaving[0].id))[0]
			value, held, err := (TierFacts{kv: view}).Get(ctx, key)
			if !errors.Is(err, ErrArchiveCorrupt) || errors.Is(err, ErrArchiveMissing) || errors.Is(err, ErrOriginalMissing) || held || value != nil {
				t.Fatalf("the read beside the unstated %s = (%q, %t, %v), want ErrArchiveCorrupt alone", artifact, value, held, err)
			}
			if got := archiveFileNames(t, archiveRoot); !equalStringSets(got, before) {
				t.Fatalf("the refusals left %v, want the %v they met", got, before)
			}
		})
	}
}

// TestArchiveWithoutManifestRemovesOnlyStagedCopies pins the staged files a lost statement leaves.
//   - staged copies no generation names are removed, and every hot file keeps its bytes and names
//   - the same bytes under the name a generation carries state a lost generation, so they read as ErrArchiveCorrupt
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestArchiveWithoutManifestRemovesOnlyStagedCopies(t *testing.T) {
	hotDir, archiveRoot, moving, _, _, _ := archivedMoveStoppedAt(t, archiveStopStaged)
	before := journalTestRootState(t, hotDir)
	if _, found, err := readArchiveManifest(archiveRoot); found || err != nil {
		t.Fatalf("staged copies beside no manifest read found=%v err=%v, want the empty archive beside no error", found, err)
	}
	if _, err := recoverArchive(hotDir, archiveRoot); err != nil {
		t.Fatalf("recover a root holding staged copies alone: %v", err)
	}
	if names := archiveFileNames(t, archiveRoot); len(names) != 0 {
		t.Fatalf("the archive holds %v, want no copy no generation names", names)
	}
	journalTestRequireSameFiles(t, before, journalTestRootState(t, hotDir))
	segmentPath := archiveSegmentPathOf(archiveRoot, journalSegmentLabelOf(moving[0].id))
	if err := stageArchiveCopy(moving[0].path, segmentPath); err != nil {
		t.Fatalf("stage the segment copy again: %v", err)
	}
	if err := promoteStagedArchiveFile(segmentPath); err != nil {
		t.Fatalf("take the segment name with no statement: %v", err)
	}
	if _, found, err := readArchiveManifest(archiveRoot); !errors.Is(err, ErrArchiveCorrupt) || found {
		t.Fatalf("a named segment file beside no manifest reads found=%v err=%v, want ErrArchiveCorrupt", found, err)
	}
}

// TestArchiveDirectoryLeavesUnrelatedTemporaryFiles pins the cleanup namespace.
//   - a temporary file whose final name is not a segment or index is left alone
//   - a temporary file with a valid segment name but no statement is removed
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestArchiveDirectoryLeavesUnrelatedTemporaryFiles(t *testing.T) {
	hotDir, archiveRoot, _, _ := archivedViewFixture(t, 2, 1)
	segDir := filepath.Join(archiveRoot, journalSegmentDirName)
	notes := filepath.Join(segDir, "notes.tmp")
	if err := os.WriteFile(notes, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write the unrelated temporary file: %v", err)
	}
	stale := archiveSegmentPathOf(archiveRoot, journalSegmentLabelOf(99)) + journalSegmentTempSuffix
	if err := os.WriteFile(stale, []byte("remove"), 0o600); err != nil {
		t.Fatalf("write the unstated segment temporary file: %v", err)
	}
	if err := convergeArchiveStructure(hotDir, archiveRoot); err != nil {
		t.Fatalf("converge beside unrelated temporary files: %v", err)
	}
	if body, err := os.ReadFile(notes); err != nil || string(body) != "keep" {
		t.Fatalf("the unrelated temporary file = (%q, %v), want its original bytes", body, err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the unstated segment temporary file remains: %v", err)
	}
}

// TestArchiveStatedSegmentWithoutACopyIsMissing pins a stated segment no bytes can supply.
//   - boot and move recovery refuse the stated name as ErrArchiveMissing, never as an archive of another generation
//   - the stated index and the refused statement keep their bytes, and a point read names the missing segment rather than an absent key
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestArchiveStatedSegmentWithoutACopyIsMissing(t *testing.T) {
	ctx := context.Background()
	hotDir, archiveRoot, leaving, _ := archivedViewFixture(t, 2, 1)
	manifest, found, err := readArchiveManifest(archiveRoot)
	if err != nil || !found || len(manifest.Segments) != 1 {
		t.Fatalf("read the stated generation: found=%v segments=%+v err=%v", found, manifest, err)
	}
	stated := manifest.Segments[0]
	if err := os.Remove(archiveSegmentPathOf(archiveRoot, stated.SegmentID)); err != nil {
		t.Fatalf("remove the stated segment file: %v", err)
	}
	before := archiveFileNames(t, archiveRoot)
	if err := convergeArchiveStructure(hotDir, archiveRoot); !errors.Is(err, ErrArchiveMissing) {
		t.Fatalf("boot over a stated segment no copy supplies = %v, want ErrArchiveMissing", err)
	}
	if _, err := recoverArchive(hotDir, archiveRoot); !errors.Is(err, ErrArchiveMissing) {
		t.Fatalf("recovery of a stated segment no copy supplies = %v, want ErrArchiveMissing", err)
	}
	if got := archiveFileNames(t, archiveRoot); !equalStringSets(got, before) {
		t.Fatalf("the refusals left %v, want the %v they met", got, before)
	}
	view, err := openJournalViewWithArchive(hotDir, false, 0, archiveRoot, 0)
	if err != nil {
		t.Fatalf("open the view beside the missing segment: %v", err)
	}
	t.Cleanup(func() { view.Close() })
	key := hotSegmentKeysForTest(int(leaving[0].id))[0]
	value, held, err := (TierFacts{kv: view}).Get(ctx, key)
	if !errors.Is(err, ErrArchiveMissing) || errors.Is(err, ErrOriginalMissing) || errors.Is(err, ErrArchiveCorrupt) || held || value != nil {
		t.Fatalf("the read of the missing segment = (%q, %t, %v), want ErrArchiveMissing alone", value, held, err)
	}
}

// TestArchiveStatedIndexWithoutACopyIsMissing pins a stated index no copy can supply.
//   - move recovery refuses the stated index as ErrArchiveMissing and rewrites neither the statement nor the segment
//   - boot completes the structure it can and the point read names the missing index, never an absent key
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestArchiveStatedIndexWithoutACopyIsMissing(t *testing.T) {
	ctx := context.Background()
	hotDir, archiveRoot, leaving, _ := archivedViewFixture(t, 2, 1)
	manifest, found, err := readArchiveManifest(archiveRoot)
	if err != nil || !found || len(manifest.Segments) != 1 {
		t.Fatalf("read the stated generation: found=%v segments=%+v err=%v", found, manifest, err)
	}
	stated := manifest.Segments[0]
	indexPath, err := archiveIndexPathOf(archiveRoot, stated)
	if err != nil {
		t.Fatalf("read the stated index name: %v", err)
	}
	if err := os.Remove(indexPath); err != nil {
		t.Fatalf("remove the stated index file: %v", err)
	}
	if _, err := recoverArchive(hotDir, archiveRoot); !errors.Is(err, ErrArchiveMissing) {
		t.Fatalf("recovery of a stated index no copy supplies = %v, want ErrArchiveMissing", err)
	}
	if err := convergeArchiveStructure(hotDir, archiveRoot); err != nil {
		t.Fatalf("boot over a stated index no copy supplies: %v", err)
	}
	after, found, err := readArchiveManifest(archiveRoot)
	if err != nil || !found || len(after.Segments) != 1 || after.Segments[0].OffsetMap.IndexPath != stated.OffsetMap.IndexPath {
		t.Fatalf("the boot restated found=%v segments=%+v err=%v, want the generation it met", found, after, err)
	}
	view, err := openJournalViewWithArchive(hotDir, false, 0, archiveRoot, 0)
	if err != nil {
		t.Fatalf("open the view beside the missing index: %v", err)
	}
	t.Cleanup(func() { view.Close() })
	key := hotSegmentKeysForTest(int(leaving[0].id))[0]
	value, held, err := (TierFacts{kv: view}).Get(ctx, key)
	if !errors.Is(err, ErrArchiveMissing) || errors.Is(err, ErrOriginalMissing) || held || value != nil {
		t.Fatalf("the read of the missing index = (%q, %t, %v), want ErrArchiveMissing alone", value, held, err)
	}
}

// TestArchiveRecoveryNeverInstallsADamagedStagedCopy pins the staged copy that contradicts its statement.
//   - a staged segment of the stated byte count whose bytes differ is not the stated segment, so the hot copy states the name
//   - a staged index whose bytes differ from the stated digest is not the stated index, so the hot index states the name
//   - both completions carry the stated digests, no staged copy stands behind a name, and a second recovery changes nothing
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestArchiveRecoveryNeverInstallsADamagedStagedCopy(t *testing.T) {
	for _, test := range []struct {
		name   string
		damage func(t *testing.T, archiveRoot string, segment archiveSegment) string
	}{
		{name: "segment", damage: func(t *testing.T, archiveRoot string, segment archiveSegment) string {
			staged := archiveSegmentPathOf(archiveRoot, segment.SegmentID) + journalSegmentTempSuffix
			flipByteAt(t, staged, fileSizeForTest(t, staged)/2)
			return staged
		}},
		{name: "index", damage: func(t *testing.T, archiveRoot string, segment archiveSegment) string {
			indexPath, err := archiveIndexPathOf(archiveRoot, segment)
			if err != nil {
				t.Fatalf("read the stated index name: %v", err)
			}
			staged := indexPath + journalSegmentTempSuffix
			flipByteAt(t, staged, journalFrameHeaderBytes+8)
			return staged
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			hotDir, archiveRoot, _, _, segment, _ := archivedMoveStoppedAt(t, archiveStopStated)
			staged := test.damage(t, archiveRoot, segment)
			if _, err := recoverArchive(hotDir, archiveRoot); err != nil {
				t.Fatalf("recover a move beside a staged copy that contradicts the statement: %v", err)
			}
			digest, size, err := digestOfFile(archiveSegmentPathOf(archiveRoot, segment.SegmentID))
			if err != nil || size != segment.Bytes || digest != segment.SHA256 {
				t.Fatalf("the completed segment = %d byte(s) %s err=%v, want the %d byte(s) %s the manifest states",
					size, digest, err, segment.Bytes, segment.SHA256)
			}
			indexPath, err := archiveIndexPathOf(archiveRoot, segment)
			if err != nil {
				t.Fatalf("read the stated index name: %v", err)
			}
			indexFrame, err := os.ReadFile(indexPath)
			if err != nil || digestOf(indexFrame) != segment.OffsetMap.IndexSHA256 {
				t.Fatalf("the completed index err=%v digest=%s, want the %s the manifest states", err, digestOf(indexFrame), segment.OffsetMap.IndexSHA256)
			}
			if _, err := os.Stat(staged); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("the staged copy %s stands after the completion, want the name it took", staged)
			}
			if names := sealedSegmentsOf(t, hotDir); len(names) != 1 {
				t.Fatalf("the hot root keeps %v, want only the interval the manifest leaves behind", names)
			}
			assertArchiveIdempotent(t, hotDir, archiveRoot)
		})
	}
}

// TestArchiveMoveRefusesASegmentItsHeaderDoesNotCover pins a segment whose bytes the header does not state.
//   - a segment file shorter than the interval its header states is ErrArchiveCorrupt before any statement is written
//   - the hot root keeps every file and the archive root keeps no manifest and no staged copy
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestArchiveMoveRefusesASegmentItsHeaderDoesNotCover(t *testing.T) {
	hotDir, segments := hotShrinkFixture(t, 2)
	moving, retained := segments[:1], segments[1:]
	archiveRoot := t.TempDir()
	window := hotWindowAfterMoveForTest(t, hotDir, moving, retained)
	stat, err := os.Stat(moving[0].path)
	if err != nil {
		t.Fatalf("stat the segment to shorten: %v", err)
	}
	if err := os.Truncate(moving[0].path, stat.Size()-1); err != nil {
		t.Fatalf("shorten the segment behind its header: %v", err)
	}
	before := journalTestRootState(t, hotDir)
	if _, err := moveSealedSegmentsToArchive(hotDir, archiveRoot, moving, window); !errors.Is(err, ErrArchiveCorrupt) {
		t.Fatalf("the move of a segment its header does not cover = %v, want ErrArchiveCorrupt", err)
	}
	journalTestRequireSameFiles(t, before, journalTestRootState(t, hotDir))
	if names := archiveFileNames(t, archiveRoot); len(names) != 0 {
		t.Fatalf("the refused move left %v, want no staged copy and no named file", names)
	}
	if _, found, err := readArchiveManifest(archiveRoot); found || err != nil {
		t.Fatalf("the refused move states found=%v err=%v, want no generation", found, err)
	}
}

// TestBootArchiveConvergenceWithoutManifestChangesNothing pins the empty archive.
//   - an archive root with no manifest is not created or populated by writer boot
//   - the hot log, order file, slot table and segment files keep their names and sizes
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestBootArchiveConvergenceWithoutManifestChangesNothing(t *testing.T) {
	hotDir, _ := hotShrinkFixture(t, 2)
	archiveRoot := filepath.Join(t.TempDir(), "archive")
	beforeSegments := hotSegmentFileNames(t, hotDir)
	paths := []string{
		filepath.Join(hotDir, journalFileName),
		filepath.Join(hotDir, journalOrderFileName),
		filepath.Join(hotDir, journalSlotFileName),
	}
	for _, name := range beforeSegments {
		paths = append(paths, filepath.Join(hotDir, journalSegmentDirName, name))
	}
	before := make(map[string]string, len(paths))
	for _, path := range paths {
		digest, _, err := digestOfFile(path)
		if err != nil {
			t.Fatalf("digest %s before boot: %v", path, err)
		}
		before[path] = digest
	}
	kv, err := openJournalViewWithArchive(hotDir, true, segmentTestBudget, archiveRoot, 0)
	if err != nil {
		t.Fatalf("boot beside no manifest: %v", err)
	}
	t.Cleanup(func() { kv.Close() })
	if _, err := os.Stat(archiveRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("boot created the empty archive root: %v", err)
	}
	if got := hotSegmentFileNames(t, hotDir); !equalStringSets(got, beforeSegments) {
		t.Fatalf("boot changed hot segment files to %v, want %v", got, beforeSegments)
	}
	for _, path := range paths {
		digest, _, err := digestOfFile(path)
		if err != nil {
			t.Fatalf("digest %s after boot: %v", path, err)
		}
		if digest != before[path] {
			t.Fatalf("boot changed the bytes of %s", path)
		}
	}
}

// TestCorruptManifestRefusesArchiveRead pins the three assignments the archive statement keeps apart.
//   - A root holding no manifest file is the empty archive: no segment, no error.
//   - Two slots stating one generation in different words are corrupt, never read as the empty archive.
//   - A slot this build cannot decode is skipped beside a slot it can; only two unreadable slots are corrupt.
//   - A generation stating a segment no staged or hot copy can supply is missing, never read as empty.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestCorruptManifestRefusesArchiveRead(t *testing.T) {
	root := t.TempDir()
	if _, found, err := readArchiveManifest(root); found || err != nil {
		t.Fatalf("the root with no manifest read as found=%v err=%v, want the empty archive beside no error", found, err)
	}
	stated := `{"version":1,"generation":1,"segments":[{"segment_id":"seg-000001","bytes":10,"sha256":"sha256:x","offset_map":{"index_path":"seg-000001.idx","index_sha256":"sha256:y"}}],"archived_through_cut":{"partition":1,"seq":1,"frame_offset":0}}`
	if err := os.WriteFile(filepath.Join(root, archiveManifestAName), []byte(stated), 0o600); err != nil {
		t.Fatalf("state the first slot: %v", err)
	}
	empty := `{"version":1,"generation":1,"segments":[],"archived_through_cut":{"partition":1,"seq":1,"frame_offset":0}}`
	if err := os.WriteFile(filepath.Join(root, archiveManifestBName), []byte(empty), 0o600); err != nil {
		t.Fatalf("state the second slot: %v", err)
	}
	if _, _, err := readArchiveManifest(root); !errors.Is(err, ErrArchiveCorrupt) {
		t.Fatalf("two slots stating one generation differently read as err=%v, want ErrArchiveCorrupt", err)
	}
	if err := os.WriteFile(filepath.Join(root, archiveManifestBName), []byte(`{"version":1,"generation":`), 0o600); err != nil {
		t.Fatalf("damage the second slot: %v", err)
	}
	manifest, found, err := readArchiveManifest(root)
	if err != nil || !found || len(manifest.Segments) != 1 {
		t.Fatalf("one decodable slot beside a damaged one read as found=%v segments=%d err=%v, want the generation it states", found, len(manifest.Segments), err)
	}
	if _, err := recoverArchive(root, root); !errors.Is(err, ErrArchiveMissing) {
		t.Fatalf("a generation stating an absent segment read as err=%v, want ErrArchiveMissing", err)
	}
}

// TestMissingArchiveIsNamedNotAbsent pins the canonical missing-archive name over the existing engine proof.
//   - TestArchiveMissingIsNamed runs directly, so the canonical name shares its fixture and assertions
func TestMissingArchiveIsNamedNotAbsent(t *testing.T) {
	TestArchiveMissingIsNamed(t)
}

// TestCorruptArchiveIsNamedNotAbsent pins the canonical corrupt-archive name over the existing engine proof.
//   - TestArchiveCorruptIsNamed runs directly, so the canonical name shares its fixture and assertions
func TestCorruptArchiveIsNamedNotAbsent(t *testing.T) {
	TestArchiveCorruptIsNamed(t)
}

// TestHottestSegmentNeverArchived pins the newest cut interval outside archive selection and move requests.
//   - archiveCandidatesAt selects only the older prefix of three hot sealed intervals
//   - a manifest that also states the newest interval leaves every interval hot when shrink refuses it
//   - adding the newest interval to the selected candidates is refused before any hot file changes
func TestHottestSegmentNeverArchived(t *testing.T) {
	hotDir, segments := hotShrinkFixture(t, 3)
	if len(segments) != 3 {
		t.Fatalf("the fixture sealed %d interval(s), want three", len(segments))
	}
	newest := segments[len(segments)-1]
	now := time.Now()
	candidates, err := archiveCandidatesAt(segments, nil, now, 0)
	if err != nil {
		t.Fatalf("select archive candidates: %v", err)
	}
	if len(candidates) != 2 || candidates[0].id != segments[0].id || candidates[1].id != segments[1].id {
		t.Fatalf("the candidates = %+v, want only the two intervals older than %s", candidates, journalSegmentLabelOf(newest.id))
	}
	for _, candidate := range candidates {
		if candidate.id >= newest.id {
			t.Fatalf("the candidates include %s at or beyond the newest cut %s", journalSegmentLabelOf(candidate.id), journalSegmentLabelOf(newest.id))
		}
	}

	entries := make([]archiveSegment, 0, len(segments))
	for _, segment := range segments {
		entry, _, err := archiveSegmentEntryOf(segment)
		if err != nil {
			t.Fatalf("state the archive entry of %s: %v", journalSegmentLabelOf(segment.id), err)
		}
		entries = append(entries, entry)
	}
	manifest := &archiveManifest{Version: archiveManifestVersion, Generation: 1, Segments: entries}
	statedCandidates, err := archiveCandidatesAt(segments, manifest, now, 0)
	if err != nil || len(statedCandidates) != 0 {
		t.Fatalf("the candidates beside a manifest stating every interval = (%+v, %v), want none", statedCandidates, err)
	}
	before := journalTestRootState(t, hotDir)
	if err := shrinkHotIndexAfterArchive(hotDir, manifest); !errors.Is(err, errHotSegmentUnstated) {
		t.Fatalf("shrink beside a manifest stating the newest interval = %v, want %v", err, errHotSegmentUnstated)
	}
	journalTestRequireSameFiles(t, before, journalTestRootState(t, hotDir))

	log, err := openJournalLogFace(hotDir, false)
	if err != nil {
		t.Fatalf("open the hot log for the move request: %v", err)
	}
	t.Cleanup(func() { log.close() })
	requested := append(append([]journalSealedSegment(nil), candidates...), newest)
	if _, err := archiveWatermarkAfterMove(log, requested); !errors.Is(err, errHotWindowUnstated) {
		t.Fatalf("the move request covering the newest interval = %v, want %v", err, errHotWindowUnstated)
	}
	journalTestRequireSameFiles(t, before, journalTestRootState(t, hotDir))
}
