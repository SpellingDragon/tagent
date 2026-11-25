// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
//
// A journal view that declares an archive root answers a key its hot index does not
// state from the segments the archive manifest carries: the manifest's key domain
// picks the segments the key may live in, the segment index names the frame that
// carries it, and that frame is read and verified where the segment holds it. The
// answers stay apart — a key no statement carries, a stated key whose bytes are
// absent, and bytes that fail their digest — because an archive that cannot be read
// is a storage fault and never a fact that does not exist. An enumeration reads the
// same statements in the order they were committed: a listing reads the indexes, and
// a walk reads the frames of the segments it reaches and none behind its stop.
package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// archiveFileSignature identifies the segment bytes a digest verification covers.
// A changed size, modification time or manifest digest invalidates the cached
// verification before another read serves it.
type archiveFileSignature struct {
	size        int64
	modTimeNano int64
	sha256      string
}

// archiveReadLocator locates the value of one key among the segments one archive
// manifest states.
//   - The manifest is read on every miss: it is the only statement of what the archive holds, and a move states a new generation while this view lives.
//   - A segment digest is checked when this view first reads the segment's current stat signature and manifest digest, so changed bytes or a restated authority are hashed again.
type archiveReadLocator struct {
	// root is the archive root the manifest and the segment directory live under.
	root string
	// verified maps each segment to the stat and manifest signature whose digest this view matched.
	verified map[string]archiveFileSignature
}

// states reports whether the conservative interval of one key domain covers key. The
// interval never excludes a key the segment carries, so a key it covers is resolved
// by the segment index.
func (d archiveKeyDomain) states(key string) bool {
	if d.FirstKey == "" || d.LastKey == "" {
		return false
	}
	return key >= d.FirstKey && key <= d.LastKey
}

// read answers the value one key's archived commit carries, and true beside a key the
// archive states.
//   - The segments are consulted from the newest to the oldest, so a key several intervals state answers the value its newest archived statement carries.
//   - A key no segment's key domain states is ErrOriginalMissing, the key neither the hot index nor the manifest carries.
//   - A segment, index or record the manifest states and the archive does not hold is ErrArchiveMissing.
//   - Bytes that do not match the manifest's segment digest, the index digest, the sorted key-set digest or the record checksum are ErrArchiveCorrupt.
//   - A segment whose key domain covers the key and whose index carries no entry for it states a conservative interval, so the next segment is tried instead of answered.
//   - The newest segment's explicit tombstone answers not held without consulting an older segment.
func (a *archiveReadLocator) read(key string) ([]byte, bool, error) {
	manifest, found, err := readArchiveManifest(a.root)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, ErrOriginalMissing
	}
	segments, err := archiveSegmentsOf(manifest)
	if err != nil {
		return nil, false, err
	}
	for _, segment := range segments {
		if !segment.KeyDomain.states(key) {
			continue
		}
		value, held, deleted, err := a.readInSegment(segment, key)
		if err != nil {
			return nil, false, err
		}
		if deleted {
			return nil, false, nil
		}
		if held {
			return value, true, nil
		}
	}
	return nil, false, ErrOriginalMissing
}

// archiveSegmentsOf answers valid manifest segments newest first.
//   - Segment commit ranges SHALL be disjoint and ordered by segment id.
//   - A segment without a commit rank is ordered by id alone.
func archiveSegmentsOf(manifest *archiveManifest) ([]archiveSegment, error) {
	ids := make([]int64, 0, len(manifest.Segments))
	byID := make(map[int64]archiveSegment, len(manifest.Segments))
	for _, segment := range manifest.Segments {
		id, held := journalSegmentIDOf(segment.SegmentID + journalSegmentNameSuffix)
		if !held || journalSegmentLabelOf(id) != segment.SegmentID {
			return nil, archiveCorruptOf("the manifest states the segment %q", segment.SegmentID)
		}
		if _, stated := byID[id]; stated {
			return nil, archiveCorruptOf("two entries state the segment %s", segment.SegmentID)
		}
		span := segment.OffsetMap
		if span.FirstSeq >= 1 && span.LastSeq < span.FirstSeq {
			return nil, archiveCorruptOf("the manifest states the commits %d to %d of %s", span.FirstSeq, span.LastSeq, segment.SegmentID)
		}
		ids = append(ids, id)
		byID[id] = segment
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })
	segments := make([]archiveSegment, 0, len(ids))
	for _, id := range ids {
		segments = append(segments, byID[id])
	}
	for at := 1; at < len(segments); at++ {
		newer, older := segments[at-1].OffsetMap, segments[at].OffsetMap
		if newer.FirstSeq < 1 || older.LastSeq < 1 {
			continue
		}
		if newer.FirstSeq <= older.LastSeq {
			return nil, archiveCorruptOf("the manifest states %s from the commit %d beside %s, which states the commits to %d",
				segments[at-1].SegmentID, newer.FirstSeq, segments[at].SegmentID, older.LastSeq)
		}
	}
	return segments, nil
}

// keyStates answers the newest archived state of each key under prefix.
//   - Segments are read newest first; the first statement wins.
//   - Only indexes are read; values are read only by a value lookup.
func (a *archiveReadLocator) keyStates(prefix string) (map[string]bool, error) {
	states := map[string]bool{}
	manifest, found, err := readArchiveManifest(a.root)
	if err != nil || !found {
		return states, err
	}
	segments, err := archiveSegmentsOf(manifest)
	if err != nil {
		return nil, err
	}
	for _, segment := range segments {
		entries, err := a.segmentIndexEntries(segment)
		if err != nil {
			return nil, err
		}
		for key, entry := range entries {
			if !strings.HasPrefix(key, prefix) {
				continue
			}
			if _, stated := states[key]; stated {
				continue
			}
			states[key] = !entry.Deleted
		}
	}
	return states, nil
}

// keys answers every key the archive holds under prefix, in key order, on the terms
// keyStates states: a key whose newest archived statement is a tombstone is held by no
// interval and is answered by no listing. The caller resolves each key's newest
// statement across hot and archive storage.
func (a *archiveReadLocator) keys(prefix string) ([]string, error) {
	states, err := a.keyStates(prefix)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(states))
	for key, held := range states {
		if held {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out, nil
}

// archiveFrame is one frame an archived segment's index attributes keys to: the byte of
// the segment's interval it starts at, the payload length and checksum its entries
// state, and the keys it locates. The frame is what an enumeration reads, because an
// index orders keys by name and the order inside one interval is the order its commits
// landed in.
type archiveFrame struct {
	// offset is the byte of the segment's log interval the frame starts at.
	offset int64
	// length is the byte length of the frame's payload, as its entries state it.
	length int
	// sum is the checksum of that payload, as its entries state it.
	sum uint32
	// keys are the keys the index attributes to this frame, in key order.
	keys []string
}

// archiveFramesOf answers the frames one archived segment's index attributes keys to,
// newest first, each with the keys it locates. Two entries that state one byte but
// different frame lengths or checksums name two frames at that byte, so the statement is
// refused instead of walked.
func archiveFramesOf(entries map[string]journalSegmentIndexEntry) ([]archiveFrame, error) {
	byOffset := make(map[int64]*archiveFrame, len(entries))
	for key, entry := range entries {
		frame, held := byOffset[entry.Offset]
		if !held {
			frame = &archiveFrame{offset: entry.Offset, length: entry.Length, sum: entry.Sum}
			byOffset[entry.Offset] = frame
		} else if frame.length != entry.Length || frame.sum != entry.Sum {
			return nil, archiveCorruptOf("the index states the frame at the byte %d as %d payload byte(s) of checksum %d beside %d of %d",
				entry.Offset, frame.length, frame.sum, entry.Length, entry.Sum)
		}
		frame.keys = append(frame.keys, key)
	}
	frames := make([]archiveFrame, 0, len(byOffset))
	for _, frame := range byOffset {
		sort.Strings(frame.keys)
		frames = append(frames, *frame)
	}
	sort.Slice(frames, func(i, j int) bool { return frames[i].offset > frames[j].offset })
	return frames, nil
}

// eachNewestFirst walks archived statements newest first and stops on false.
//   - A key is visited once, at its newest archived statement; tombstones are skipped.
//   - Values are read only when readValues stands.
//   - Segments behind the stop are not read.
func (a *archiveReadLocator) eachNewestFirst(ctx context.Context, readValues bool, visit func(key string, value []byte) (bool, error)) error {
	manifest, found, err := readArchiveManifest(a.root)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	segments, err := archiveSegmentsOf(manifest)
	if err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, segment := range segments {
		if err := ctx.Err(); err != nil {
			return err
		}
		stopped, err := a.eachSegmentNewestFirst(ctx, segment, readValues, seen, visit)
		if err != nil {
			return err
		}
		if stopped {
			return nil
		}
	}
	return nil
}

// eachSegmentNewestFirst walks one segment newest first and reports an early stop.
//   - The segment header and digest are verified before any frame answers.
//   - An index entry its frame does not carry is corruption.
func (a *archiveReadLocator) eachSegmentNewestFirst(ctx context.Context, segment archiveSegment, readValues bool, seen map[string]struct{}, visit func(key string, value []byte) (bool, error)) (bool, error) {
	file, _, headerBytes, err := a.openVerifiedSegment(segment)
	if err != nil {
		return false, err
	}
	defer file.Close()
	path := filepath.Join(a.root, journalSegmentDirName, segment.SegmentID+journalSegmentNameSuffix)
	entries, err := a.segmentIndexEntries(segment)
	if err != nil {
		return false, err
	}
	frames, err := archiveFramesOf(entries)
	if err != nil {
		return false, err
	}
	for _, frame := range frames {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		body := make([]byte, journalFrameHeaderBytes+frame.length)
		if _, err := file.ReadAt(body, headerBytes+frame.offset); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return false, fmt.Errorf("%w: the frame at the byte %d of %s", ErrArchiveMissing, frame.offset, path)
			}
			return false, archiveFaultOf(err, path)
		}
		payload, err := journalPayloadOf(body)
		if err != nil {
			return false, archiveCorruptOf("%s: %v", path, err)
		}
		if crc32.Checksum(payload, journalCRCTable) != frame.sum {
			return false, archiveCorruptOf("the index of %s states the checksum %d of the frame at the byte %d beside the %d its bytes carry",
				path, frame.sum, frame.offset, crc32.Checksum(payload, journalCRCTable))
		}
		at := headerBytes + frame.offset + journalFrameHeaderBytes
		spans, err := journalSpansAt(payload, at)
		if err != nil {
			return false, archiveCorruptOf("%s: %v", path, err)
		}
		if !journalRecordVersionReadable(spans.version) {
			return false, archiveCorruptOf("%s states the record version %d", path, spans.version)
		}
		more, err := a.walkFrameStatements(frame, entries, spans, payload, at, path, readValues, seen, visit)
		if err != nil || more {
			return more, err
		}
	}
	return false, nil
}

// walkFrameStatements walks the keys one frame states in the order the commit wrote
// them, and answers whether visit asked the walk to stop. A key is walked at its newest
// statement of the frame — the value of its last write, or the tombstone of a frame it
// deletes, which names nothing and hides every older interval.
//   - Every key the index attributes to this frame must be one the frame states, or the
//     statement and the bytes contradict each other and neither is served.
func (a *archiveReadLocator) walkFrameStatements(frame archiveFrame, entries map[string]journalSegmentIndexEntry, spans journalSpans, payload []byte, at int64, path string, readValues bool, seen map[string]struct{}, visit func(key string, value []byte) (bool, error)) (bool, error) {
	order := make([]string, 0, len(spans.puts))
	puts := make(map[string]journalValue, len(spans.puts))
	for at := len(spans.puts) - 1; at >= 0; at-- {
		put := spans.puts[at]
		if _, held := puts[put.key]; held {
			continue
		}
		order = append(order, put.key)
		puts[put.key] = put.value
	}
	deletes := make([]string, 0, len(spans.deletes))
	tombstones := make(map[string]struct{}, len(spans.deletes))
	for _, key := range spans.deletes {
		if _, held := tombstones[key]; !held {
			deletes = append(deletes, key)
		}
		tombstones[key] = struct{}{}
	}
	carried := make(map[string]bool, len(order)+len(deletes))
	for _, key := range order {
		entry, attributed := entries[key]
		if _, tombstoned := tombstones[key]; tombstoned || !attributed || entry.Offset != frame.offset || entry.Deleted {
			continue
		}
		carried[key] = false
		if _, walked := seen[key]; walked {
			continue
		}
		seen[key] = struct{}{}
		var value []byte
		if readValues {
			located, err := archiveValueOf(payload, at, puts[key], path, key)
			if err != nil {
				return false, err
			}
			value = located
		}
		more, err := visit(key, value)
		if err != nil {
			return false, err
		}
		if !more {
			return true, nil
		}
	}
	for _, key := range deletes {
		entry, attributed := entries[key]
		if !attributed || entry.Offset != frame.offset || !entry.Deleted {
			continue
		}
		carried[key] = true
		seen[key] = struct{}{}
	}
	for _, key := range frame.keys {
		tombstoned, held := carried[key]
		if !held || tombstoned != entries[key].Deleted {
			return false, archiveCorruptOf("%s states the key %q at a frame that does not carry it", path, key)
		}
	}
	return false, nil
}

// archiveValueOf answers the value one written key carries inside a frame payload, on
// the terms the point read answers it: the JSON token the write locates, or the bytes
// of a write that committed JSON null.
func archiveValueOf(payload []byte, at int64, found journalValue, path, key string) ([]byte, error) {
	if found.valueNull {
		return nil, nil
	}
	from := found.valueOffset - at
	to := from + int64(found.valueLength)
	if from < 0 || to > int64(len(payload)) {
		return nil, archiveCorruptOf("%s states the value of %q outside its frame", path, key)
	}
	var value []byte
	if err := json.Unmarshal(payload[from:to], &value); err != nil {
		return nil, archiveCorruptOf("%s states the value of %q: %v", path, key, err)
	}
	return value, nil
}

// openVerifiedSegment opens one manifest segment and checks its stat signature,
// header and whole-file digest before its index or records answer a read.
func (a *archiveReadLocator) openVerifiedSegment(segment archiveSegment) (file *os.File, header journalSegmentHeader, headerBytes int64, err error) {
	path := filepath.Join(a.root, journalSegmentDirName, segment.SegmentID+journalSegmentNameSuffix)
	file, err = os.Open(path)
	if err != nil {
		return nil, journalSegmentHeader{}, 0, archiveFaultOf(err, path)
	}
	defer func() {
		if err != nil {
			file.Close()
		}
	}()
	stat, err := file.Stat()
	if err != nil {
		return nil, journalSegmentHeader{}, 0, archiveFaultOf(err, path)
	}
	if stat.Size() != segment.Bytes {
		return nil, journalSegmentHeader{}, 0, archiveCorruptOf("%s holds %d byte(s) beside the %d the manifest states",
			path, stat.Size(), segment.Bytes)
	}
	header, headerBytes, err = journalSegmentHeaderOf(file, path)
	if err != nil {
		return nil, journalSegmentHeader{}, 0, archiveCorruptOf("%s", err)
	}
	if header.SegmentID != segment.SegmentID || header.Bytes != segment.Bytes-headerBytes {
		return nil, journalSegmentHeader{}, 0, archiveCorruptOf("%s states the segment %q of %d byte(s) beside the manifest's %q of %d",
			path, header.SegmentID, header.Bytes, segment.SegmentID, segment.Bytes)
	}
	signature := archiveFileSignature{size: stat.Size(), modTimeNano: stat.ModTime().UnixNano(), sha256: segment.SHA256}
	if a.verified[segment.SegmentID] != signature {
		digest, size, err := digestOfFile(path)
		if err != nil {
			return nil, journalSegmentHeader{}, 0, archiveFaultOf(err, path)
		}
		if size != segment.Bytes || digest != segment.SHA256 {
			return nil, journalSegmentHeader{}, 0, archiveCorruptOf("%s holds %d byte(s) %s beside the %d %s the manifest states",
				path, size, digest, segment.Bytes, segment.SHA256)
		}
		a.verified[segment.SegmentID] = signature
	}
	return file, header, headerBytes, nil
}

// readInSegment answers the value key's newest write carries inside one archived
// segment, whether the segment states no entry for the key, and whether its newest
// entry is a tombstone.
//   - The file, its header, its byte count and its digest are checked against the manifest before a record inside it answers.
//   - The record is verified by its frame checksum and by the checksum its index entry states, and a tombstone answers only from a frame that deletes its key.
func (a *archiveReadLocator) readInSegment(segment archiveSegment, key string) ([]byte, bool, bool, error) {
	file, _, headerBytes, err := a.openVerifiedSegment(segment)
	if err != nil {
		return nil, false, false, err
	}
	defer file.Close()
	path := filepath.Join(a.root, journalSegmentDirName, segment.SegmentID+journalSegmentNameSuffix)
	entries, err := a.segmentIndexEntries(segment)
	if err != nil {
		return nil, false, false, err
	}
	entry, stated := entries[key]
	if !stated {
		return nil, false, false, nil
	}
	frame := make([]byte, journalFrameHeaderBytes+entry.Length)
	if _, err := file.ReadAt(frame, headerBytes+entry.Offset); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, false, false, fmt.Errorf("%w: the record of %s at the byte %d of %s",
				ErrArchiveMissing, key, entry.Offset, path)
		}
		return nil, false, false, archiveFaultOf(err, path)
	}
	payload, err := journalPayloadOf(frame)
	if err != nil {
		return nil, false, false, archiveCorruptOf("%s", err)
	}
	if crc32.Checksum(payload, journalCRCTable) != entry.Sum {
		return nil, false, false, archiveCorruptOf("the index of %s states the checksum %d of the record of %q beside the %d its bytes carry",
			path, entry.Sum, key, crc32.Checksum(payload, journalCRCTable))
	}
	at := headerBytes + entry.Offset + journalFrameHeaderBytes
	spans, err := journalSpansAt(payload, at)
	if err != nil {
		return nil, false, false, archiveCorruptOf("%s: %v", path, err)
	}
	if !journalRecordVersionReadable(spans.version) {
		return nil, false, false, archiveCorruptOf("%s states the record version %d", path, spans.version)
	}
	if entry.Deleted {
		for _, deleteKey := range spans.deletes {
			if deleteKey == key {
				return nil, false, true, nil
			}
		}
		return nil, false, false, archiveCorruptOf("%s states %q deleted at a frame that does not delete it", path, key)
	}
	located := false
	var found journalValue
	for _, put := range spans.puts {
		if put.key == key {
			found, located = put.value, true
		}
	}
	if !located {
		return nil, false, false, archiveCorruptOf("%s states the key %q at a frame that does not carry it", path, key)
	}
	value, err := archiveValueOf(payload, at, found, path, key)
	if err != nil {
		return nil, false, false, err
	}
	return value, true, false, nil
}

// segmentIndexEntries answers the index one manifest segment states, verified against
// its index digest, entry count, segment header, commit ranks, cut and complete key
// domain.
//   - An index path outside the archive segment directory, a frame this build does not place and an index statement the manifest contradicts are ErrArchiveCorrupt.
//   - The cut the manifest states is located in the index alone: a cut no checkpoint key names, or one named at a frame beside the manifest's offset, is corruption.
func (a *archiveReadLocator) segmentIndexEntries(segment archiveSegment) (map[string]journalSegmentIndexEntry, error) {
	name := segment.OffsetMap.IndexPath
	if name == "" || filepath.Base(name) != name {
		return nil, archiveCorruptOf("the manifest states the index %q of %s", name, segment.SegmentID)
	}
	path := filepath.Join(a.root, journalSegmentDirName, name)
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, archiveFaultOf(err, path)
	}
	if digestOf(body) != segment.OffsetMap.IndexSHA256 {
		return nil, archiveCorruptOf("%s does not match the digest the manifest states", path)
	}
	segmentPath := archiveSegmentPathOf(a.root, segment.SegmentID)
	file, err := os.Open(segmentPath)
	if err != nil {
		return nil, archiveFaultOf(err, segmentPath)
	}
	header, headerBytes, err := journalSegmentHeaderOf(file, segmentPath)
	file.Close()
	if err != nil {
		return nil, archiveCorruptOf("%s", err)
	}
	if header.SegmentID != segment.SegmentID || header.Bytes != segment.Bytes-headerBytes {
		return nil, archiveCorruptOf("%s states the segment %q of %d byte(s) beside the manifest's %q of %d",
			segmentPath, header.SegmentID, header.Bytes, segment.SegmentID, segment.Bytes)
	}
	if header.FirstSeq != segment.OffsetMap.FirstSeq || header.LastSeq != segment.OffsetMap.LastSeq {
		return nil, archiveCorruptOf("%s states the commits %d to %d beside the manifest's %d to %d",
			segmentPath, header.FirstSeq, header.LastSeq, segment.OffsetMap.FirstSeq, segment.OffsetMap.LastSeq)
	}
	if header.CutPartition != segment.Cut.Partition || header.CutSeq != segment.Cut.Seq {
		return nil, archiveCorruptOf("%s states the cut at the partition %d seq %d beside the manifest's %d seq %d",
			segmentPath, header.CutPartition, header.CutSeq, segment.Cut.Partition, segment.Cut.Seq)
	}
	if segment.KeyDomain.Partition != header.CutPartition {
		return nil, archiveCorruptOf("the manifest states the key domain of %s at the partition %d beside the header's %d",
			segment.SegmentID, segment.KeyDomain.Partition, header.CutPartition)
	}
	entries, stated := parseSegmentIndex(body, segment.SegmentID, header.Bytes)
	if !stated {
		return nil, archiveCorruptOf("%s states no index this build places", path)
	}
	if len(entries) != segment.OffsetMap.FrameCount {
		return nil, archiveCorruptOf("%s states %d entries beside the %d the manifest states",
			path, len(entries), segment.OffsetMap.FrameCount)
	}
	domain := archiveKeyDomainOf(header.CutPartition, entries)
	if domain.FirstKey != segment.KeyDomain.FirstKey || domain.LastKey != segment.KeyDomain.LastKey || domain.KeysSHA != segment.KeyDomain.KeysSHA {
		return nil, archiveCorruptOf("%s states the key domain %+v beside the manifest's %+v", path, domain, segment.KeyDomain)
	}
	cutStated := false
	for key, entry := range entries {
		partition, seq, checkpoint, valid := checkpointCutOfKey(key)
		if !checkpoint || !valid || partition != segment.Cut.Partition || seq != segment.Cut.Seq {
			continue
		}
		cutStated = true
		if entry.Offset != segment.Cut.FrameOffset {
			return nil, archiveCorruptOf("%s states the cut at the partition %d seq %d in the frame at the byte %d beside the manifest's %d",
				path, segment.Cut.Partition, segment.Cut.Seq, entry.Offset, segment.Cut.FrameOffset)
		}
	}
	if !cutStated {
		return nil, archiveCorruptOf("%s states the cut at the partition %d seq %d in no checkpoint key",
			path, segment.Cut.Partition, segment.Cut.Seq)
	}
	return entries, nil
}

// archiveFaultOf answers the error one archive file raised: a file the statement names
// and the archive does not hold is ErrArchiveMissing, and any other failure to read it
// is ErrArchiveCorrupt.
func archiveFaultOf(err error, path string) error {
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrArchiveMissing, path)
	}
	return fmt.Errorf("%w: read %s: %w", ErrArchiveCorrupt, path, err)
}

// archiveCorruptOf answers the named corruption one statement of the archive carries,
// formatted with the given message.
func archiveCorruptOf(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrArchiveCorrupt}, args...)...)
}
