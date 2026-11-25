// The segment layout one journal directory holds: the open segment in journal.log
// and every interval a cut closed behind it in segments/seg-<id>.log, read as one
// address space so a boot replays the segments in id order followed by the open
// segment exactly as it replayed one file.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
package ledger

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// segmentTestBudget is the hot log budget these checks open a view beside: any
// positive number states a bound, and the bound itself is no part of the layout.
const segmentTestBudget = 1 << 20

// sealedSegmentsOf answers the sealed segment files a journal directory holds, by
// name, so a check states the intervals a view wrote. The index files beside them
// state no interval, so a name that states no segment id is left out.
func sealedSegmentsOf(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, journalSegmentDirName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read the segment directory of %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if _, stated := journalSegmentIDOf(entry.Name()); !stated {
			continue
		}
		names = append(names, entry.Name())
	}
	return names
}

// TestCheckpointCutRequiresCanonicalKey pins the two checkpoint spellings.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestCheckpointCutRequiresCanonicalKey(t *testing.T) {
	for _, test := range []struct {
		key        string
		partition  int
		seq        int64
		checkpoint bool
		valid      bool
	}{
		{key: "checkpoint:3:11", partition: 3, seq: 11, checkpoint: true, valid: true},
		{key: "3:idx:checkpoint:3:11", partition: 3, seq: 11, checkpoint: true, valid: true},
		{key: "3:idx:checkpoint:4:11", checkpoint: true},
		{key: "3:idx:checkpoint:bad", checkpoint: true},
		{key: "checkpoint:bad", checkpoint: true},
		{key: "checkpoint:3:11:extra", checkpoint: true},
		{key: "fact:idx:checkpoint:3:11"},
		{key: "3:evt:fact:idx:checkpoint:3:11"},
	} {
		t.Run(test.key, func(t *testing.T) {
			partition, seq, checkpoint, valid := checkpointCutOfKey(test.key)
			if partition != test.partition || seq != test.seq || checkpoint != test.checkpoint || valid != test.valid {
				t.Fatalf("checkpointCutOfKey(%q) = (%d, %d, %t, %t), want (%d, %d, %t, %t)",
					test.key, partition, seq, checkpoint, valid, test.partition, test.seq, test.checkpoint, test.valid)
			}
		})
	}
}

// TestOrdinaryIndexedKeyDoesNotSealSegment pins a fact key that merely carries the marker.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestOrdinaryIndexedKeyDoesNotSealSegment(t *testing.T) {
	dir := t.TempDir()
	kv, err := openJournalViewFace(dir, true, segmentTestBudget)
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	t.Cleanup(func() { kv.Close() })
	if err := kv.Commit(context.Background(), Batch{Puts: []Pair{{Key: "fact:idx:checkpoint:3:11", Value: []byte("fact")}}}); err != nil {
		t.Fatalf("commit the ordinary indexed fact: %v", err)
	}
	if segments := readSealedSegmentsForTest(t, dir); len(segments) != 0 {
		t.Fatalf("the ordinary indexed fact sealed %d segment(s), want none", len(segments))
	}
}

// TestJournalV2CarriesCommitTimes pins commit time as record metadata beside the cut identity.
//   - Commit writes one version two timestamp shared by every put and delete in its batch
//   - replay and segment indexing read that timestamp, and the header bounds every record time in the segment
//   - only a checkpoint closes a segment, and its cut and commit ranks do not depend on the timestamps
func TestJournalV2CarriesCommitTimes(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	kv, err := openJournalViewFace(dir, true, segmentTestBudget)
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	t.Cleanup(func() { kv.log.close() })
	batch := Batch{
		Puts:    []Pair{{Key: "a", Value: []byte("alpha")}, {Key: "b", Value: []byte("beta")}},
		Deletes: []string{"gone"},
	}
	batchBefore := time.Now().UnixNano()
	if err := kv.Commit(ctx, batch); err != nil {
		t.Fatalf("commit the batch: %v", err)
	}
	batchAfter := time.Now().UnixNano()
	cutBefore := time.Now().UnixNano()
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "checkpoint:3:11", Value: []byte("cut")}}}); err != nil {
		t.Fatalf("commit the first cut: %v", err)
	}
	cutAfter := time.Now().UnixNano()
	segments := readSealedSegmentsForTest(t, dir)
	if len(segments) != 1 {
		t.Fatalf("the cut sealed %d segment(s), want the one interval", len(segments))
	}
	segment := segments[0]
	header, _, err := journalSegmentHeaderOf(mustOpenSegment(t, segment.path), segment.path)
	if err != nil {
		t.Fatalf("read the segment header: %v", err)
	}
	if header.Version != journalSegmentVersion || header.CutPartition != 3 || header.CutSeq != 11 ||
		header.FirstSeq != 1 || header.LastSeq != 2 {
		t.Fatalf("the header states version=%d cut=%d:%d commits=%d..%d, want version two, cut 3:11 and commits 1..2",
			header.Version, header.CutPartition, header.CutSeq, header.FirstSeq, header.LastSeq)
	}
	var records []journalSpans
	complete, err := journalReplay(journalSegmentBytes{segment: segment}, 0, segment.bytes, func(payload []byte, at int64) error {
		if count := bytes.Count(payload, []byte(`"committed_at_ns"`)); count != 1 {
			return fmt.Errorf("record at %d states %d commit-time fields, want the one shared by its batch", at, count)
		}
		spans, err := journalSpansAt(payload, at)
		if err != nil {
			return err
		}
		records = append(records, spans)
		return nil
	})
	if err != nil || complete != segment.bytes {
		t.Fatalf("replay the sealed records: complete=%d err=%v", complete, err)
	}
	if len(records) != 2 {
		t.Fatalf("replay read %d record(s), want the batch and its cut", len(records))
	}
	if records[0].version != journalFormatVersion || records[1].version != journalFormatVersion {
		t.Fatalf("the records state versions %d and %d, want version two", records[0].version, records[1].version)
	}
	if records[0].committedAtNS < batchBefore || records[0].committedAtNS > batchAfter ||
		records[1].committedAtNS < cutBefore || records[1].committedAtNS > cutAfter {
		t.Fatalf("the records state commit times %d and %d outside their commit calls", records[0].committedAtNS, records[1].committedAtNS)
	}
	if len(records[0].puts) != 2 || len(records[0].deletes) != 1 {
		t.Fatalf("the timestamped batch states %d put(s) and %d delete(s), want two and one", len(records[0].puts), len(records[0].deletes))
	}
	minNS, maxNS := records[0].committedAtNS, records[0].committedAtNS
	if records[1].committedAtNS < minNS {
		minNS = records[1].committedAtNS
	}
	if records[1].committedAtNS > maxNS {
		maxNS = records[1].committedAtNS
	}
	if header.CommitTimeMinNS != minNS || header.CommitTimeMaxNS != maxNS {
		t.Fatalf("the header states commit times %d..%d, want the records' %d..%d", header.CommitTimeMinNS, header.CommitTimeMaxNS, minNS, maxNS)
	}
	entries, err := journalSegmentIndexEntries(journalSegmentBytes{segment: segment}, 0, segment.bytes)
	if err != nil {
		t.Fatalf("index the version two records: %v", err)
	}
	for _, key := range []string{"a", "b", "checkpoint:3:11"} {
		if _, held := entries[key]; !held {
			t.Fatalf("the segment index states no entry for %q", key)
		}
	}
	gone, held := entries["gone"]
	if !held || !gone.Deleted {
		t.Fatalf("the segment index states no tombstone for the deleted key: %+v", gone)
	}
	firstBytes := segment.bytes
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "c", Value: []byte("gamma")}}}); err != nil {
		t.Fatalf("commit a fact without a cut: %v", err)
	}
	if len(readSealedSegmentsForTest(t, dir)) != 1 {
		t.Fatalf("a commit without a checkpoint closed another segment")
	}
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "checkpoint:3:12", Value: []byte("cut")}}}); err != nil {
		t.Fatalf("commit the second cut: %v", err)
	}
	segments = readSealedSegmentsForTest(t, dir)
	if len(segments) != 2 {
		t.Fatalf("the second cut sealed %d segment(s), want two", len(segments))
	}
	second := segments[1]
	if second.begin != firstBytes || second.firstSeq != 3 || second.lastSeq != 4 {
		t.Fatalf("the second segment states begin=%d commits=%d..%d, want begin=%d and commits 3..4",
			second.begin, second.firstSeq, second.lastSeq, firstBytes)
	}
	for key, want := range map[string]string{"a": "alpha", "b": "beta", "c": "gamma"} {
		value, held := readOne(t, kv, key)
		if !held || string(value) != want {
			t.Fatalf("read %q = (%q, %t), want (%q, true)", key, value, held, want)
		}
	}
}

// TestJournalSealStatesTheIntervalItClosed pins the file one seal writes.
//   - the seal states one segment file named after the id the log holds next, and the log's open segment starts after the bytes the file carries
//   - the header carries the cut that asked for the seal beside the commit ranks the interval spans and its own byte count
//   - the bytes behind the header are the log bytes the interval held, copied and never re-encoded
//   - a second seal states the id after the first, so the segments of one log are ordered by the ids they state
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalSealStatesTheIntervalItClosed(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	kv, err := openJournalViewFace(dir, true, segmentTestBudget)
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	t.Cleanup(func() { kv.log.close() })
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "a", Value: []byte("alpha")}}}); err != nil {
		t.Fatalf("commit the first fact: %v", err)
	}
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "checkpoint:3:11", Value: []byte("cut")}}}); err != nil {
		t.Fatalf("close the first segment: %v", err)
	}
	kv.log.mu.Lock()
	closedInterval := append([]byte{}, mustReadLog(t, kv.log, 0, kv.log.sealedThrough())...)
	kv.log.mu.Unlock()
	names := sealedSegmentsOf(t, dir)
	if len(names) != 1 || names[0] != journalSegmentNameOf(1) {
		t.Fatalf("the seal stated the segments %v, want the one file %q", names, journalSegmentNameOf(1))
	}
	segments := readSealedSegmentsForTest(t, dir)
	if len(segments) != 1 {
		t.Fatalf("the directory states %d segment(s), want the one the seal wrote", len(segments))
	}
	segment := segments[0]
	if segment.bytes != int64(len(closedInterval)) {
		t.Fatalf("the segment carries %d byte(s) beside the %d the interval it closed held", segment.bytes, len(closedInterval))
	}
	if segment.begin != 0 {
		t.Fatalf("the first segment starts at the log byte %d, want the log's first byte", segment.begin)
	}
	header, headerBytes, err := journalSegmentHeaderOf(mustOpenSegment(t, segment.path), segment.path)
	if err != nil {
		t.Fatalf("read the header of %s: %v", segment.path, err)
	}
	if header.FirstSeq != 1 || header.LastSeq != 2 {
		t.Fatalf("the header states the commits %d to %d, want the interval from the first commit through the cut's own commit", header.FirstSeq, header.LastSeq)
	}
	if header.CutPartition != 3 || header.CutSeq != 11 {
		t.Fatalf("the header states the cut %d:%d, want the cut the seal was asked for", header.CutPartition, header.CutSeq)
	}
	payload, err := os.ReadFile(segment.path)
	if err != nil {
		t.Fatalf("read the segment file: %v", err)
	}
	if !bytes.Equal(payload[headerBytes:], closedInterval) {
		t.Fatalf("the bytes behind the header are not the interval the log held, want the copied bytes")
	}
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "b", Value: []byte("beta")}}}); err != nil {
		t.Fatalf("commit the second fact: %v", err)
	}
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "checkpoint:3:12", Value: []byte("cut")}}}); err != nil {
		t.Fatalf("close the second segment: %v", err)
	}
	names = sealedSegmentsOf(t, dir)
	if len(names) != 2 || names[0] != journalSegmentNameOf(1) || names[1] != journalSegmentNameOf(2) {
		t.Fatalf("the second seal left the segments %v, want the ids one and two", names)
	}
	segments = readSealedSegmentsForTest(t, dir)
	if segments[1].begin != segments[0].bytes {
		t.Fatalf("the second segment starts at the log byte %d beside a first of %d byte(s)",
			segments[1].begin, segments[0].bytes)
	}
	if segments[1].firstSeq != 3 || segments[1].lastSeq != 4 {
		t.Fatalf("the second segment states the commits %d to %d, want the interval up to its cut's own commit",
			segments[1].firstSeq, segments[1].lastSeq)
	}
}

// TestJournalSegmentCarriesTheValuesItClosed pins the read across a segment boundary.
//   - a value committed before a seal reads back byte for byte after it, through the segment that carries it
//   - a value committed after the seal reads back through the open segment, and a reopen reads both
//   - the log's bytes are one address space: what the segments carry followed by what the open segment carries is the log the commits wrote
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalSegmentCarriesTheValuesItClosed(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	kv, err := openJournalViewFace(dir, true, segmentTestBudget)
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "a", Value: []byte("alpha")}}}); err != nil {
		t.Fatalf("commit the first fact: %v", err)
	}
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "checkpoint:1:4", Value: []byte("cut")}}}); err != nil {
		t.Fatalf("close the first segment: %v", err)
	}
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "b", Value: []byte("beta")}}}); err != nil {
		t.Fatalf("commit the second fact: %v", err)
	}
	for _, key := range []string{"a", "b"} {
		value, held := readOne(t, kv, key)
		if !held || string(value) != map[string]string{"a": "alpha", "b": "beta"}[key] {
			t.Fatalf("the logged view read (%q, %v) for %q, want the value the commit wrote", value, held, key)
		}
	}
	kv.log.mu.Lock()
	logBytes := mustReadLog(t, kv.log, 0, kv.log.size)
	openBase := kv.log.openBase
	kv.log.mu.Unlock()
	kv.log.close()

	segments := readSealedSegmentsForTest(t, dir)
	var carried []byte
	for _, segment := range segments {
		payload, err := os.ReadFile(segment.path)
		if err != nil {
			t.Fatalf("read the segment file: %v", err)
		}
		carried = append(carried, payload[segment.headerBytes:]...)
	}
	if int64(len(carried)) != openBase {
		t.Fatalf("the segments carry %d byte(s) beside the open base %d", len(carried), openBase)
	}
	if !bytes.Equal(append(carried, logBytes[openBase:]...), logBytes) {
		t.Fatalf("the segments followed by the open interval are not the log the commits wrote")
	}

	reopened, err := openJournalViewFace(dir, true, segmentTestBudget)
	if err != nil {
		t.Fatalf("reopen the journal view: %v", err)
	}
	t.Cleanup(func() { reopened.log.close() })
	for _, key := range []string{"a", "b"} {
		value, held := readOne(t, reopened, key)
		if !held || string(value) != map[string]string{"a": "alpha", "b": "beta"}[key] {
			t.Fatalf("the reopened view read (%q, %v) for %q, want the value the commit wrote", value, held, key)
		}
	}
	if reopened.log.openBase != openBase {
		t.Fatalf("the reopened view starts its open segment at %d, want the %d the segments state",
			reopened.log.openBase, openBase)
	}
	if _, had := readOne(t, reopened, "c"); had {
		t.Fatalf("the reopened view read a key no commit wrote")
	}
	if err := reopened.Commit(ctx, Batch{Puts: []Pair{{Key: "c", Value: []byte("gamma")}}}); err != nil {
		t.Fatalf("commit a fact beside the interval the boot read: %v", err)
	}
	if err := reopened.Commit(ctx, Batch{Puts: []Pair{{Key: "checkpoint:1:6", Value: []byte("cut")}}}); err != nil {
		t.Fatalf("close the segment after the reopen: %v", err)
	}
	sealed := readSealedSegmentsForTest(t, dir)
	if len(sealed) != 2 || sealed[1].begin != openBase {
		t.Fatalf("the reopen stated the segments %+v, want the second to start at the %d the first interval ends at",
			sealed, openBase)
	}
}

// TestJournalSegmentIsTheOnlyCopyAReadAnswers pins the segment against the bytes the open file keeps behind it.
//   - the bytes an interval gave up are read from the segment, never from the open file: a damaged prefix of the open file leaves every value readable
//   - the reopen verifies the segment's frames and reads the values it carries
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalSegmentIsTheOnlyCopyAReadAnswers(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	kv, err := openJournalViewFace(dir, true, segmentTestBudget)
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "a", Value: []byte("alpha")}}}); err != nil {
		t.Fatalf("commit the first fact: %v", err)
	}
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "checkpoint:2:9", Value: []byte("cut")}}}); err != nil {
		t.Fatalf("close the first segment: %v", err)
	}
	kv.log.mu.Lock()
	openBase := kv.log.openBase
	kv.log.mu.Unlock()
	if err := kv.log.close(); err != nil {
		t.Fatalf("close the journal view: %v", err)
	}
	stale := bytes.Repeat([]byte{0xff}, int(openBase))
	file, err := os.OpenFile(filepath.Join(dir, journalFileName), os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open the log file: %v", err)
	}
	if _, err := file.WriteAt(stale, 0); err != nil {
		file.Close()
		t.Fatalf("damage the bytes the interval gave up: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close the log file: %v", err)
	}
	reopened, err := openJournalViewFace(dir, true, segmentTestBudget)
	if err != nil {
		t.Fatalf("reopen the journal view over the damaged prefix: %v", err)
	}
	t.Cleanup(func() { reopened.log.close() })
	value, held := readOne(t, reopened, "a")
	if !held || !bytes.Equal(value, []byte("alpha")) {
		t.Fatalf("the reopened view read (%q, %v), want the value the segment carries", value, held)
	}
}

// TestJournalSealStatesNothingWithoutABudget pins the root beside no archive config.
//   - a view opened beside no hot log budget states no segment, so the log stays the one unsealed interval it was
//   - the directory gains no segment directory, which is the layout a boot of that root reads
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalSealStatesNothingWithoutABudget(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	kv, err := openJournalKV(dir)
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	t.Cleanup(func() { kv.log.close() })
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "a", Value: []byte("alpha")}}}); err != nil {
		t.Fatalf("commit the fact: %v", err)
	}
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "checkpoint:1:2", Value: []byte("cut")}}}); err != nil {
		t.Fatalf("seal the log beside no budget: %v", err)
	}
	if names := sealedSegmentsOf(t, dir); len(names) != 0 {
		t.Fatalf("the seal stated the segments %v beside no hot log budget", names)
	}
	if _, err := os.Stat(filepath.Join(dir, journalSegmentDirName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the view stated a segment directory beside no hot log budget: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the journal directory of %s: %v", dir, err)
	}
	held := make(map[string]bool, len(entries))
	for _, entry := range entries {
		held[entry.Name()] = true
	}
	for _, name := range []string{journalFileName, journalOrderFileName, journalSlotFileName, journalOrderLineFileName} {
		if !held[name] {
			t.Fatalf("the journal directory holds %v, want the files the log states beside it", held)
		}
	}
	if len(held) != 4 {
		t.Fatalf("the journal directory holds %v, want the log and the three index files alone", held)
	}
}

// TestJournalRefusesADamagedSegmentHeader pins the named refusal of a segment whose statement is unreadable.
//   - a header byte the checksum does not cover refuses the open as ErrSegmentHeader
//   - the refusal names the file, so the segment a root lost is named instead of read as an interval nobody states
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalRefusesADamagedSegmentHeader(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	kv, err := openJournalViewFace(dir, true, segmentTestBudget)
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "a", Value: []byte("alpha")}}}); err != nil {
		t.Fatalf("commit the fact: %v", err)
	}
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "checkpoint:1:2", Value: []byte("cut")}}}); err != nil {
		t.Fatalf("close the first segment: %v", err)
	}
	if err := kv.log.close(); err != nil {
		t.Fatalf("close the journal view: %v", err)
	}
	segments := readSealedSegmentsForTest(t, dir)
	if len(segments) != 1 {
		t.Fatalf("the directory states %d segment(s), want the one the seal wrote", len(segments))
	}
	file, err := os.OpenFile(segments[0].path, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open the segment file: %v", err)
	}
	if _, err := file.WriteAt([]byte{'x'}, journalFrameHeaderBytes+1); err != nil {
		file.Close()
		t.Fatalf("damage the segment header: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close the segment file: %v", err)
	}
	if _, err := openJournalViewFace(dir, true, segmentTestBudget); !errors.Is(err, ErrSegmentHeader) {
		t.Fatalf("the open over a damaged header answered %v, want %v", err, ErrSegmentHeader)
	}
}

// TestJournalRefusesASegmentWithoutItsLog pins the open of a directory whose open segment is not there beside the segments it closed.
//   - the open refuses by name, because the bytes past the last interval are unknown
//   - it states no log of zero bytes instead, which would answer every later commit as the first one of a young root
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalRefusesASegmentWithoutItsLog(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	kv, err := openJournalViewFace(dir, true, segmentTestBudget)
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "a", Value: []byte("alpha")}}}); err != nil {
		t.Fatalf("commit the fact: %v", err)
	}
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "checkpoint:1:2", Value: []byte("cut")}}}); err != nil {
		t.Fatalf("close the first segment: %v", err)
	}
	if err := kv.log.close(); err != nil {
		t.Fatalf("close the journal view: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, journalFileName)); err != nil {
		t.Fatalf("remove the open segment: %v", err)
	}
	if _, err := openJournalViewFace(dir, true, segmentTestBudget); !errors.Is(err, ErrJournalCorrupt) {
		t.Fatalf("the open beside no open segment answered %v, want %v", err, ErrJournalCorrupt)
	}
	if _, err := os.Stat(filepath.Join(dir, journalFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the refused open stated the log it refused to read: %v", err)
	}
}

// readSealedSegmentsForTest answers the sealed segments one journal directory holds,
// read the way a boot reads them.
func readSealedSegmentsForTest(t *testing.T, dir string) []journalSealedSegment {
	t.Helper()
	segments, err := openSealedSegments(dir)
	if err != nil {
		t.Fatalf("read the sealed segments of %s: %v", dir, err)
	}
	return segments
}

// mustOpenSegment opens one segment file for the check that reads its header.
func mustOpenSegment(t *testing.T, path string) *os.File {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open the segment file %s: %v", path, err)
	}
	t.Cleanup(func() { file.Close() })
	return file
}

// mustReadLog answers the log bytes of one span, read as every pass of the log reads
// them: across the sealed segments and then the open segment.
func mustReadLog(t *testing.T, log *journalLog, from, to int64) []byte {
	t.Helper()
	out := make([]byte, to-from)
	if _, err := log.readAt(out, from); err != nil {
		t.Fatalf("read the log bytes %d to %d: %v", from, to, err)
	}
	return out
}

// TestSealedSegmentIndexLocatesEveryKey pins the index one sealed segment states beside it.
//   - every key the segment locates answers the frame of its newest write in that segment, and the value read at that frame is the bytes the commit wrote
//   - a key written in both segments locates its own write in each, so an index answers the interval it was stated for
//   - an index removed from beside a segment is rebuilt from the segment and locates the same keys and frames
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestSealedSegmentIndexLocatesEveryKey(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	kv, err := openJournalViewFace(dir, true, segmentTestBudget)
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	t.Cleanup(func() { kv.log.close() })
	commits := []Batch{
		{Puts: []Pair{{Key: "a", Value: []byte("alpha")}, {Key: "b", Value: []byte("beta")}}},
		{Puts: []Pair{{Key: "checkpoint:6:21", Value: []byte("cut-one")}}},
		{Puts: []Pair{{Key: "c", Value: []byte("gamma")}, {Key: "a", Value: []byte("alpha-two")}}},
		{Puts: []Pair{{Key: "checkpoint:6:22", Value: []byte("cut-two")}}},
	}
	for _, batch := range commits {
		if err := kv.Commit(ctx, batch); err != nil {
			t.Fatalf("commit the batch %v: %v", batch, err)
		}
	}
	wanted := []map[string]string{
		{"a": "alpha", "b": "beta", "checkpoint:6:21": "cut-one"},
		{"a": "alpha-two", "c": "gamma", "checkpoint:6:22": "cut-two"},
	}
	segments := readSealedSegmentsForTest(t, dir)
	if len(segments) != len(wanted) {
		t.Fatalf("the directory states %d segment(s), want the %d the cuts closed", len(segments), len(wanted))
	}
	for at, segment := range segments {
		if _, err := os.Stat(journalSegmentIndexPathOf(segment)); err != nil {
			t.Fatalf("the seal of %s stated no index: %v", segment.path, err)
		}
		entries, err := sealedSegmentIndexOf(segment)
		if err != nil {
			t.Fatalf("read the index of %s: %v", segment.path, err)
		}
		if len(entries) != len(wanted[at]) {
			t.Fatalf("the index of %s locates %d key(s), want the %d the interval wrote", segment.path, len(entries), len(wanted[at]))
		}
		for key, want := range wanted[at] {
			entry, held := entries[key]
			if !held {
				t.Fatalf("the index of %s locates no %q, want the frame the segment carries it in", segment.path, key)
			}
			if value := segmentIndexValueForTest(t, segment, key, entry); !bytes.Equal(value, []byte(want)) {
				t.Fatalf("the index of %s answered %q for %q, want %q", segment.path, value, key, want)
			}
		}
	}
	removed := journalSegmentIndexPathOf(segments[0])
	if err := os.Remove(removed); err != nil {
		t.Fatalf("remove the index %s: %v", removed, err)
	}
	rebuilt, err := sealedSegmentIndexOf(segments[0])
	if err != nil {
		t.Fatalf("rebuild the index of %s: %v", segments[0].path, err)
	}
	if len(rebuilt) != len(wanted[0]) {
		t.Fatalf("the rebuilt index locates %d key(s), want the %d the interval wrote", len(rebuilt), len(wanted[0]))
	}
	for key, want := range wanted[0] {
		entry, held := rebuilt[key]
		if !held {
			t.Fatalf("the rebuilt index locates no %q, want the frame the segment carries it in", key)
		}
		if value := segmentIndexValueForTest(t, segments[0], key, entry); !bytes.Equal(value, []byte(want)) {
			t.Fatalf("the rebuilt index answered %q for %q, want %q", value, key, want)
		}
	}
}

// TestUnrebuildableSegmentIndexIsRefusedByName pins the refusal of an index no segment can state.
//   - an index removed from beside a segment that lost bytes is refused as ErrSegmentIndexUnrebuildable, through the ErrSegmentHeader the scan meets
//   - no empty index is answered in its place, which would state every key the segment carries as absent
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestUnrebuildableSegmentIndexIsRefusedByName(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	kv, err := openJournalViewFace(dir, true, segmentTestBudget)
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	t.Cleanup(func() { kv.log.close() })
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "a", Value: []byte("alpha")}}}); err != nil {
		t.Fatalf("commit the fact: %v", err)
	}
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "checkpoint:2:5", Value: []byte("cut")}}}); err != nil {
		t.Fatalf("close the segment: %v", err)
	}
	segments := readSealedSegmentsForTest(t, dir)
	if len(segments) != 1 {
		t.Fatalf("the directory states %d segment(s), want the one the cut closed", len(segments))
	}
	if err := os.Remove(journalSegmentIndexPathOf(segments[0])); err != nil {
		t.Fatalf("remove the index of the segment: %v", err)
	}
	stat, err := os.Stat(segments[0].path)
	if err != nil {
		t.Fatalf("read the segment size: %v", err)
	}
	if err := os.Truncate(segments[0].path, stat.Size()-1); err != nil {
		t.Fatalf("truncate the segment: %v", err)
	}
	entries, err := sealedSegmentIndexOf(segments[0])
	if !errors.Is(err, ErrSegmentIndexUnrebuildable) {
		t.Fatalf("the index of a truncated segment answered (%v, %v), want %v", entries, err, ErrSegmentIndexUnrebuildable)
	}
	if !errors.Is(err, ErrSegmentHeader) {
		t.Fatalf("the refusal answered %v, want the %v the scan met", err, ErrSegmentHeader)
	}
}

// segmentIndexValueForTest answers the value one index entry locates, read through
// the segment at the frame the entry names.
func segmentIndexValueForTest(t *testing.T, segment journalSealedSegment, key string, entry journalSegmentIndexEntry) []byte {
	t.Helper()
	frame := make([]byte, journalFrameHeaderBytes+entry.Length)
	if _, err := segment.readAt(frame, entry.Offset); err != nil {
		t.Fatalf("read the frame of %q: %v", key, err)
	}
	if sum := binary.BigEndian.Uint32(frame[4:8]); sum != entry.Sum {
		t.Fatalf("the frame of %q states the checksum %08x beside the %08x the index names", key, sum, entry.Sum)
	}
	payload, err := journalPayloadOf(frame)
	if err != nil {
		t.Fatalf("read the payload of %q: %v", key, err)
	}
	spans, err := journalSpansAt(payload, entry.Offset+journalFrameHeaderBytes)
	if err != nil {
		t.Fatalf("read the spans of the frame carrying %q: %v", key, err)
	}
	for _, put := range spans.puts {
		if put.key != key {
			continue
		}
		token := make([]byte, put.value.valueLength)
		if _, err := segment.readAt(token, put.value.valueOffset); err != nil {
			t.Fatalf("read the value of %q: %v", key, err)
		}
		var value []byte
		if err := json.Unmarshal(token, &value); err != nil {
			t.Fatalf("decode the value of %q: %v", key, err)
		}
		return value
	}
	t.Fatalf("the frame the index names carries no write of %q", key)
	return nil
}

// TestFailedCutLeavesNoSegmentBoundary pins the boundary a cut that never landed leaves behind.
//   - A commit carrying the quiet marker but no checkpoint key closes no segment: the marker alone states no cut.
//   - A boot after the lost cut reads the marker's frames back from the open segment, so the boundary is absent rather than misplaced.
//   - The next commit carrying the checkpoint key closes the segment over both commits, so the boundary lands at the cut that really landed.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestFailedCutLeavesNoSegmentBoundary(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	kv, err := openJournalViewFace(dir, true, segmentTestBudget)
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	t.Cleanup(func() { kv.log.close() })
	quiet := "tagent/checkpoint/v1/quiet:3:11"
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "a", Value: []byte("alpha")}}}); err != nil {
		t.Fatalf("commit the fact ahead of the cut: %v", err)
	}
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: quiet, Value: []byte("quiet")}}}); err != nil {
		t.Fatalf("commit the quiet marker of a cut whose checkpoint never landed: %v", err)
	}
	if names := sealedSegmentsOf(t, dir); len(names) != 0 {
		t.Fatalf("the segments %v stand beside a cut whose checkpoint never landed, want no boundary", names)
	}
	kv.log.mu.Lock()
	through := kv.log.sealedThrough()
	kv.log.mu.Unlock()
	if through != 0 {
		t.Fatalf("the sealed span ends at the byte %d beside a cut that never landed, want the whole log open", through)
	}
	if err := kv.log.close(); err != nil {
		t.Fatalf("close the view before the boot: %v", err)
	}
	reopened, err := openJournalViewFace(dir, true, segmentTestBudget)
	if err != nil {
		t.Fatalf("reopen the journal view: %v", err)
	}
	t.Cleanup(func() { reopened.log.close() })
	if names := sealedSegmentsOf(t, dir); len(names) != 0 {
		t.Fatalf("the boot stated the segments %v, want the quiet marker's frames left in the open segment", names)
	}
	if value, held := readOne(t, reopened, quiet); !held || string(value) != "quiet" {
		t.Fatalf("the reopened view read (%q, %v) for the quiet marker, want the frame the lost cut left behind", value, held)
	}
	if err := reopened.Commit(ctx, Batch{Puts: []Pair{{Key: "checkpoint:3:11", Value: []byte("cut")}}}); err != nil {
		t.Fatalf("commit the checkpoint of the cut that lands: %v", err)
	}
	names := sealedSegmentsOf(t, dir)
	if len(names) != 1 || names[0] != journalSegmentNameOf(1) {
		t.Fatalf("the landed cut stated the segments %v, want the one file %q", names, journalSegmentNameOf(1))
	}
	segments := readSealedSegmentsForTest(t, dir)
	header, _, err := journalSegmentHeaderOf(mustOpenSegment(t, segments[0].path), segments[0].path)
	if err != nil {
		t.Fatalf("read the header of %s: %v", segments[0].path, err)
	}
	if header.CutPartition != 3 || header.CutSeq != 11 {
		t.Fatalf("the header states the cut %d:%d, want the cut that landed", header.CutPartition, header.CutSeq)
	}
	reopened.log.mu.Lock()
	closedInterval := append([]byte{}, mustReadLog(t, reopened.log, 0, reopened.log.sealedThrough())...)
	reopened.log.mu.Unlock()
	payload, err := os.ReadFile(segments[0].path)
	if err != nil {
		t.Fatalf("read the segment file: %v", err)
	}
	if !bytes.Equal(payload[segments[0].headerBytes:], closedInterval) {
		t.Fatalf("the segment does not carry the interval the quiet marker is in, want the bytes the log held")
	}
}
