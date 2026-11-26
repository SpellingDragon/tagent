// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
//
// A journal directory holds its open segment in journal.log and every interval a
// cut has closed behind it in segments/seg-<id>.log. A sealed segment is the bytes
// of one interval, copied verbatim behind a header that states the interval it
// carries: the segment id, the commit ranks it spans, the cut it was closed at and
// its own byte count. Offsets of the log are logical and never move when a segment
// closes: the sealed segments and the open segment are one address space read as
// one file, so an index that locates a value keeps locating it across a boundary and
// a boot replays the sealed segments in id order followed by the open segment
// exactly as it replayed one file.
package ledger

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrSegmentHeader reports a sealed segment whose header this build cannot read: a
// file shorter than a header frame, a frame whose checksum does not cover the header
// it holds, a segment id this build does not state, or a byte count the file beside
// the header does not carry. The header is the segment's own statement of the
// interval it holds, so a segment whose statement is unreadable is refused by name
// instead of served as the bytes of an interval nobody stated.
var ErrSegmentHeader = errors.New("memory: journal segment header is not readable")

// ErrSegmentSealAfterClose reports a seal asked of a view whose Close ended it: the
// segment it would state belongs to no writer of the root, so the call is refused by
// name instead of writing beside the process that took the root over.
var ErrSegmentSealAfterClose = errors.New("memory: seal a segment of a closed view")

// ErrSegmentIndexUnrebuildable reports a segment whose keys this build cannot
// index: the segment states no header this build reads, holds bytes its header does
// not cover, or carries a frame no checksum verifies, so the index stated from it
// would name bytes nobody stated. The refusal is by name because an empty index
// would answer every key the segment carries as absent.
var ErrSegmentIndexUnrebuildable = errors.New("memory: sealed segment index cannot be rebuilt")

const (
	// journalSegmentDirName is the directory under one journal directory that holds
	// its sealed segments. A segment is one interval of the log, so the directory
	// holds the intervals in the order their ids state and never a file of another
	// kind: the writer states one behind a barrier and nothing rewrites one.
	journalSegmentDirName = "segments"
	// journalSegmentNamePrefix and journalSegmentNameSuffix name one sealed segment
	// file: the prefix, the segment id and the suffix. A name that does not state an
	// id is no segment of this build, which is what leaves a stopped seal's temporary
	// file unread instead of refused.
	journalSegmentNamePrefix = "seg-"
	journalSegmentNameSuffix = ".log"
	// journalSegmentTempSuffix names the file a seal writes before it renames the
	// segment into place, so a stop inside the copy leaves a file no open reads.
	journalSegmentTempSuffix = ".tmp"
	// journalSegmentIDDigits is the width every segment id is written at, so the
	// names of the segments sort in the order their ids state.
	journalSegmentIDDigits = 6
	// journalSegmentMagic names a segment header file, so the header frame of
	// another file is read as no segment instead of as a header of this build.
	journalSegmentMagic uint32 = 0x4a534547
	// journalSegmentVersion is the header format this build writes. Version one
	// remains readable and states an unknown commit-time range.
	journalSegmentVersion   = 2
	journalSegmentVersionV1 = 1
	// journalSegmentHeaderBound bounds the header frame a reader reads, so a damaged
	// length field cannot make the open allocate a span the writer never stated. It
	// is headroom over the header this build writes, never a length: the fields are
	// one id, three numbers and a cut.
	journalSegmentHeaderBound = 4 << 10
	// journalSegmentIndexSuffix names the file beside a sealed segment that states
	// the frame carrying each key the segment locates: the segment's label and this
	// suffix. It is an index derived from the segment, so a segment without one is
	// indexed by scanning the segment itself.
	journalSegmentIndexSuffix = ".idx"
	// journalSegmentIndexMagic names a segment index file, so the frame of another
	// file is read as no index instead of as one of this build.
	journalSegmentIndexMagic uint32 = 0x4a534944
	// journalSegmentIndexVersion is the index format this build writes and the only
	// one it reads: another version states no index this build places, and the
	// segment is scanned in its place.
	journalSegmentIndexVersion = 1
)

// checkpointCutOf answers the cut a commit batch states when the batch files one
// checkpoint fact: the partition and sequence its key names, read from the key the
// family commits under. A batch carrying no checkpoint key states no cut, so a plain
// commit closes no segment and a failed cut (whose checkpoint never landed) closes none
// either. A fact commit indexes its event under <partition>:idx:<eventKey>, so the
// checkpoint fact a cut files reaches a batch as that indexed form too.
func checkpointCutOf(batch Batch) (int, int64, bool) {
	for _, put := range batch.Puts {
		partition, seq, checkpoint, valid := checkpointCutOfKey(put.Key)
		if !checkpoint {
			continue
		}
		if !valid {
			return 0, 0, false
		}
		return partition, seq, true
	}
	return 0, 0, false
}

// checkpointCutOfKey reads one canonical checkpoint key, distinguishing a key of
// another family from a malformed checkpoint so a batch keeps its refusal rule.
// The indexed spelling carries the partition twice; both fields must agree.
func checkpointCutOfKey(key string) (partition int, seq int64, checkpoint, valid bool) {
	spelled, checkpoint := strings.CutPrefix(key, "checkpoint:")
	indexedPartition := 0
	indexed := false
	if !checkpoint {
		outer, rest, found := strings.Cut(key, ":idx:checkpoint:")
		if !found {
			return 0, 0, false, false
		}
		parsed, err := strconv.Atoi(outer)
		if err != nil {
			return 0, 0, false, false
		}
		indexedPartition = parsed
		spelled, checkpoint, indexed = rest, true, true
	}
	parts := strings.SplitN(spelled, ":", 2)
	if len(parts) != 2 {
		return 0, 0, true, false
	}
	partition, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, true, false
	}
	if indexed && partition != indexedPartition {
		return 0, 0, true, false
	}
	seq, err = strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, true, false
	}
	return partition, seq, true, true
}

// journalSegmentHeader is the first frame of one sealed segment: the interval the
// file carries, stated by the file itself. It answers the segment a boot replays
// without a manifest, so a segment is self-describing and no table beside it is the
// authority of what it holds.
type journalSegmentHeader struct {
	// Magic names this file as a segment header of this build.
	Magic uint32 `json:"magic"`
	// Version is the header format this build writes.
	Version int `json:"version"`
	// SegmentID is the segment's own name, which is the file's name without the
	// prefix and the suffix.
	SegmentID string `json:"segment_id"`
	// FirstSeq and LastSeq are the commit ranks this interval spans, counted in the
	// one commit order of the log: the rank the first commit of the interval holds and
	// the rank of its last. The frames a cut states its quiet point as are positions
	// and no commits, so an interval holding one spans the commits around it.
	FirstSeq int64 `json:"first_seq"`
	LastSeq  int64 `json:"last_seq"`
	// CutPartition and CutSeq are the cut that closed this segment.
	CutPartition int   `json:"cut_partition"`
	CutSeq       int64 `json:"cut_seq"`
	// Bytes is the number of log bytes the file carries behind this header. The file
	// itself is the header frame and these bytes, so a segment a writer stopped
	// inside states fewer bytes than the file holds and is refused.
	Bytes int64 `json:"bytes"`
	// CommitTimeMinNS and CommitTimeMaxNS bound the commit times of every record the
	// segment carries. Zero values state an unknown range in version one and in a
	// version two segment containing a record of unknown time.
	CommitTimeMinNS int64 `json:"commit_time_min_ns"`
	CommitTimeMaxNS int64 `json:"commit_time_max_ns"`
}

// journalSealedSegment is one closed interval of a log as a view reads it: the file
// that carries the bytes, the logical log offset the interval starts at, and the
// header frame in front of those bytes.
type journalSealedSegment struct {
	// id is the segment's id as its name and its header state it, so the next
	// interval of the log states the id after it and a reader orders the intervals by
	// number rather than by name.
	id int64
	// path is the segment file under the log's directory.
	path string
	// begin is the logical log byte this interval starts at: the sum of the bytes
	// every segment before it carries.
	begin int64
	// bytes is the number of log bytes the file carries behind its header.
	bytes int64
	// firstSeq and lastSeq are the commit ranks this interval spans, as its header
	// states them, so a seal states the interval behind it and never walks it.
	firstSeq int64
	lastSeq  int64
	// headerBytes is the byte the log bytes start at in the file: the header frame
	// the segment states itself as.
	headerBytes int64
	// version and the commit-time bounds are the header's format and the range an
	// archive age window reads without walking the segment.
	version         int
	commitTimeMinNS int64
	commitTimeMaxNS int64
}

// archiveMovableAt reports whether the segment's whole commit-time range precedes
// now-age. A non-positive age imposes no window; an unknown range is retained when
// age is positive.
func (s journalSealedSegment) archiveMovableAt(now time.Time, age time.Duration) bool {
	if age <= 0 {
		return true
	}
	if s.version != journalSegmentVersion ||
		s.commitTimeMinNS == 0 || s.commitTimeMaxNS == 0 ||
		s.commitTimeMinNS > s.commitTimeMaxNS {
		return false
	}
	return time.Unix(0, s.commitTimeMaxNS).Before(now.Add(-age))
}

// journalSegmentIndex is the one frame of a segment index file: the segment it
// indexes and, per key, the frame of that segment carrying the key's newest write.
// It is a derived statement, so a file this build cannot place costs a scan of the
// segment and never a refusal of the segment.
type journalSegmentIndex struct {
	// Magic names this file as a segment index of this build.
	Magic uint32 `json:"magic"`
	// Version is the index format this build writes.
	Version int `json:"version"`
	// SegmentID is the label of the segment this index locates keys inside.
	SegmentID string `json:"segment_id"`
	// Entries locates the segment's keys, ordered by key.
	Entries []journalSegmentIndexEntry `json:"entries"`
}

// journalSegmentIndexEntry locates one key inside its segment: the frame that
// carries the key's newest write, at the frame's byte offset in the segment's log
// interval, its payload length and its payload checksum.
type journalSegmentIndexEntry struct {
	// Key is the key this entry locates.
	Key string `json:"key"`
	// Offset is the byte of the segment's log interval the frame starts at.
	Offset int64 `json:"offset"`
	// Length is the byte length of the frame's payload.
	Length int `json:"length"`
	// Sum is the checksum of that payload, the four-byte one the frame states
	// beside its length.
	Sum uint32 `json:"sum"`
	// Deleted states that the newest frame touching the key deletes it. Its offset,
	// length and checksum still locate that delete frame.
	Deleted bool `json:"deleted,omitempty"`
}

// journalSegmentLabelOf answers the segment id one segment id is stated as: the
// prefix and the digits, which is the name of its file without the suffix.
func journalSegmentLabelOf(id int64) string {
	digits := strconv.FormatInt(id, 10)
	if len(digits) < journalSegmentIDDigits {
		digits = strings.Repeat("0", journalSegmentIDDigits-len(digits)) + digits
	}
	return journalSegmentNamePrefix + digits
}

// journalSegmentNameOf answers the file name one segment id is written at.
func journalSegmentNameOf(id int64) string {
	return journalSegmentLabelOf(id) + journalSegmentNameSuffix
}

// journalSegmentIndexPathOf answers the file one segment's index is stated at:
// the segment's label beside the segment under the log's directory.
func journalSegmentIndexPathOf(segment journalSealedSegment) string {
	return filepath.Join(filepath.Dir(segment.path), journalSegmentLabelOf(segment.id)+journalSegmentIndexSuffix)
}

// journalSegmentIDOf answers the id one file name states, and false beside a name
// that states none: a temporary file a seal left unfinished and any file of another
// kind are read as no segment at all.
func journalSegmentIDOf(name string) (int64, bool) {
	if !strings.HasPrefix(name, journalSegmentNamePrefix) || !strings.HasSuffix(name, journalSegmentNameSuffix) {
		return 0, false
	}
	digits := strings.TrimSuffix(strings.TrimPrefix(name, journalSegmentNamePrefix), journalSegmentNameSuffix)
	if digits == "" {
		return 0, false
	}
	for _, digit := range digits {
		if digit < '0' || digit > '9' {
			return 0, false
		}
	}
	id, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// journalSegmentHeaderFrame answers the bytes one segment starts with: the header
// framed as every record of this log is, so the frame's own checksum is the
// statement that the header is whole.
func journalSegmentHeaderFrame(header journalSegmentHeader) ([]byte, error) {
	body, err := json.Marshal(header)
	if err != nil {
		return nil, err
	}
	return journalFrameOf(body), nil
}

// journalSegmentHeaderOf answers the header one segment file states and the bytes
// its frame takes, and refuses a file whose first frame is unreadable, whose
// checksum does not cover the header it holds, or whose magic and version it cannot
// read.
func journalSegmentHeaderOf(file *os.File, path string) (journalSegmentHeader, int64, error) {
	var header journalSegmentHeader
	prefix := make([]byte, journalFrameHeaderBytes)
	if _, err := file.ReadAt(prefix, 0); err != nil {
		return header, 0, fmt.Errorf("%w: %s: %w", ErrSegmentHeader, path, err)
	}
	length := int(binary.BigEndian.Uint32(prefix[0:4]))
	if length <= 0 || length > journalSegmentHeaderBound {
		return header, 0, fmt.Errorf("%w: %s states a %d byte header", ErrSegmentHeader, path, length)
	}
	frame := make([]byte, journalFrameHeaderBytes+length)
	copy(frame, prefix)
	if _, err := file.ReadAt(frame[journalFrameHeaderBytes:], journalFrameHeaderBytes); err != nil {
		return header, 0, fmt.Errorf("%w: %s: %w", ErrSegmentHeader, path, err)
	}
	body, err := journalPayloadOf(frame)
	if err != nil {
		return header, 0, fmt.Errorf("%w: %s: %w", ErrSegmentHeader, path, err)
	}
	if err := json.Unmarshal(body, &header); err != nil {
		return header, 0, fmt.Errorf("%w: %s: %w", ErrSegmentHeader, path, err)
	}
	if header.Magic != journalSegmentMagic ||
		(header.Version != journalSegmentVersionV1 && header.Version != journalSegmentVersion) {
		return header, 0, fmt.Errorf("%w: %s states the version %d of another build", ErrSegmentHeader, path, header.Version)
	}
	return header, int64(len(frame)), nil
}

// openSealedSegments answers the sealed segments one journal directory holds, oldest
// first, each with the logical log byte it starts at. It reads every header and
// refuses by name a segment whose statement the file beside it does not carry, so a
// log is never replayed over bytes nobody stated.
//   - The segments are ordered by their ids and never by their names, because a name sorts by its digits only while the width holds them.
//   - The commit ranks of the segments increase: an interval states the commits between the interval before it and itself, so a segment whose ranks stand at or before its neighbour's states bytes another segment already states.
//   - A gap between two ids and between two rank spans is not refused: an interval of the log may leave the hot directory, and what states that it did is the archive the leaf that moves it states (see the archive contract).
func openSealedSegments(dir string) ([]journalSealedSegment, error) {
	segmentsDir := filepath.Join(dir, journalSegmentDirName)
	entries, err := os.ReadDir(segmentsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(entries))
	paths := make(map[int64]string, len(entries))
	for _, entry := range entries {
		id, stated := journalSegmentIDOf(entry.Name())
		if !stated {
			continue
		}
		if _, held := paths[id]; held {
			return nil, fmt.Errorf("%w: %s holds two files of the segment id %d",
				ErrSegmentHeader, segmentsDir, id)
		}
		ids = append(ids, id)
		paths[id] = filepath.Join(segmentsDir, entry.Name())
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	segments := make([]journalSealedSegment, 0, len(ids))
	begin := int64(0)
	for _, id := range ids {
		segment, err := readSealedSegment(id, paths[id], begin)
		if err != nil {
			return nil, err
		}
		if len(segments) > 0 && segment.firstSeq <= segments[len(segments)-1].lastSeq {
			return nil, fmt.Errorf("%w: %s states the commits from %d beside %s, which states them to %d",
				ErrSegmentHeader, segment.path, segment.firstSeq, segments[len(segments)-1].path, segments[len(segments)-1].lastSeq)
		}
		segments = append(segments, segment)
		begin += segment.bytes
	}
	return segments, nil
}

// readSealedSegment answers one sealed segment as the interval starting at the given
// log byte, and refuses a file whose header and length disagree: the bytes behind the
// header are the interval the header states, so a file of another length carries an
// interval nobody stated.
func readSealedSegment(id int64, path string, begin int64) (journalSealedSegment, error) {
	var segment journalSealedSegment
	file, err := os.Open(path)
	if err != nil {
		return segment, fmt.Errorf("%w: %s: %w", ErrSegmentHeader, path, err)
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return segment, fmt.Errorf("%w: %s: %w", ErrSegmentHeader, path, err)
	}
	header, headerBytes, err := journalSegmentHeaderOf(file, path)
	if err != nil {
		return segment, err
	}
	if label := journalSegmentLabelOf(id); header.SegmentID != label {
		return segment, fmt.Errorf("%w: %s states the segment %q", ErrSegmentHeader, path, header.SegmentID)
	}
	if header.Bytes < 0 || header.FirstSeq < 1 || header.LastSeq < header.FirstSeq {
		return segment, fmt.Errorf("%w: %s states the commits %d to %d in %d byte(s)",
			ErrSegmentHeader, path, header.FirstSeq, header.LastSeq, header.Bytes)
	}
	if stat.Size() != headerBytes+header.Bytes {
		return segment, fmt.Errorf("%w: %s holds %d byte(s) beside a header stating %d",
			ErrSegmentHeader, path, stat.Size(), headerBytes+header.Bytes)
	}
	return journalSealedSegment{
		id:              id,
		path:            path,
		begin:           begin,
		bytes:           header.Bytes,
		firstSeq:        header.FirstSeq,
		lastSeq:         header.LastSeq,
		headerBytes:     headerBytes,
		version:         header.Version,
		commitTimeMinNS: header.CommitTimeMinNS,
		commitTimeMaxNS: header.CommitTimeMaxNS,
	}, nil
}

// readAt answers the log bytes one sealed segment carries from the given byte of its
// interval. The file is opened for the read alone: a segment is written once and
// never rewritten, so a read opens it, takes the bytes it names and gives the handle
// back instead of the view holding one per segment it holds.
func (s journalSealedSegment) readAt(p []byte, off int64) (int, error) {
	file, err := os.Open(s.path)
	if err != nil {
		return 0, fmt.Errorf("%w: %s: %w", ErrJournalCorrupt, s.path, err)
	}
	defer file.Close()
	return file.ReadAt(p, s.headerBytes+off)
}

// sealedSegmentAt answers the sealed segment that carries one logical log byte.
func (l *journalLog) sealedSegmentAt(off int64) (journalSealedSegment, bool) {
	for _, segment := range l.segments {
		if off >= segment.begin && off < segment.begin+segment.bytes {
			return segment, true
		}
	}
	return journalSealedSegment{}, false
}

// readAt answers the bytes at one logical log offset: an offset inside a sealed
// segment reads that segment's bytes behind its header, and one inside the open
// segment reads the writer's file. Every read of the log goes through it, so the
// segment that carries an interval is the one a value of that interval is answered
// from, and the bytes the open file still holds of that interval are read by nothing.
//
// The read crosses a segment boundary, so a caller that asks for a span holding the
// end of one interval and the start of the next is answered the bytes of both.
func (l *journalLog) readAt(p []byte, off int64) (int, error) {
	total := 0
	for len(p) > 0 {
		if off >= l.openBase {
			read, err := l.file.ReadAt(p, off)
			return total + read, err
		}
		segment, stated := l.sealedSegmentAt(off)
		if !stated {
			return total, fmt.Errorf("%w: %s states no segment at the log byte %d",
				ErrSegmentHeader, l.dir, off)
		}
		span := segment.begin + segment.bytes - off
		if span > int64(len(p)) {
			span = int64(len(p))
		}
		read, err := segment.readAt(p[:span], off-segment.begin)
		total += read
		if err != nil {
			return total, err
		}
		if int64(read) != span {
			return total, fmt.Errorf("%w: %s answers %d of the %d byte(s) at the log byte %d",
				ErrJournalCorrupt, segment.path, read, span, off)
		}
		p = p[read:]
		off += int64(read)
	}
	return total, nil
}

// sealedThrough answers the log byte the newest sealed interval ends at: the open
// segment starts there, so a reader of the closed interval reads the log up to it.
func (l *journalLog) sealedThrough() int64 {
	if len(l.segments) == 0 {
		return 0
	}

	last := l.segments[len(l.segments)-1]
	return last.begin + last.bytes
}

// sealSegment closes the open segment of this log at its end and leaves an empty
// open segment behind it: the interval [openBase, size) is copied verbatim into a
// new segment file behind a header that states it, and the byte every read routes on
// moves to that end. The caller holds the view's lock, so no commit interleaves the
// copy.
//   - The copy is the writer's own bytes and never a re-encoding: the segment is the interval the log held, so a boot replaying the segments in id order reads the bytes the one file held.
//   - The segment file appears by a rename of a temporary one, so an open reads the segment whole or not at all: a stop inside the copy leaves the interval in the open file, which the boot that follows reads as one unsealed interval.
//   - The open file keeps the bytes of the interval it gave up; they are read by nothing, and the pass that shrinks the hot directory reclaims them (see the archive contract).
//   - The end the seal closes is the durable end of the log at the seal, and the header states the commit ranks that interval spans: a commit that landed after the cut's barrier and before this call stands inside the sealed interval instead of past it.
//   - An interval holding no byte states no commit, so a seal asked of an empty open segment states no segment and answers no error.
//   - The seal states the segment's index beside it, so a key the interval holds is located by its own entry instead of a walk of the interval.
func (l *journalLog) sealSegment(partition int, seq int64) error {
	begin, end := l.openBase, l.size
	carried := end - begin
	if carried == 0 {
		return nil
	}
	first := int64(1)
	if len(l.segments) > 0 {
		first = l.segments[len(l.segments)-1].lastSeq + 1
	}
	last := l.addr.seq
	if last < first {
		return fmt.Errorf("tagent: close the segment at the cut of partition %d seq %d: the interval from the log byte %d carries no commit of its own",
			partition, seq, begin)
	}
	id := int64(1)
	if len(l.segments) > 0 {
		id = l.segments[len(l.segments)-1].id + 1
	}
	commitTimeMinNS, commitTimeMaxNS := int64(0), int64(0)
	commitTimeStated := false
	commitTimesKnown := true
	complete, err := journalReplay(journalLogBytes{log: l}, begin, end, func(payload []byte, at int64) error {
		spans, err := journalSpansAt(payload, at)
		if err != nil {
			return fmt.Errorf("%w: read a record of segment %d: %w", ErrJournalCorrupt, id, err)
		}
		if !journalRecordVersionReadable(spans.version) {
			return fmt.Errorf("%w: segment %d states record version %d", ErrJournalFormat, id, spans.version)
		}
		if spans.committedAtNS == 0 {
			if spans.version == journalFormatVersionV1 {
				commitTimesKnown = false
			}
			return nil
		}
		if !commitTimeStated || spans.committedAtNS < commitTimeMinNS {
			commitTimeMinNS = spans.committedAtNS
		}
		if !commitTimeStated || spans.committedAtNS > commitTimeMaxNS {
			commitTimeMaxNS = spans.committedAtNS
		}
		commitTimeStated = true
		return nil
	})
	if err != nil {
		return fmt.Errorf("tagent: read the commit times of segment %d: %w", id, err)
	}
	if complete != end {
		return fmt.Errorf("%w: segment %d reaches the log byte %d instead of %d", ErrJournalCorrupt, id, complete, end)
	}
	if !commitTimesKnown {
		commitTimeMinNS, commitTimeMaxNS = 0, 0
	}
	segment := journalSealedSegment{
		id:              id,
		path:            filepath.Join(l.dir, journalSegmentDirName, journalSegmentNameOf(id)),
		begin:           begin,
		bytes:           carried,
		firstSeq:        first,
		lastSeq:         last,
		version:         journalSegmentVersion,
		commitTimeMinNS: commitTimeMinNS,
		commitTimeMaxNS: commitTimeMaxNS,
	}
	header := journalSegmentHeader{
		Magic:           journalSegmentMagic,
		Version:         journalSegmentVersion,
		SegmentID:       journalSegmentLabelOf(id),
		FirstSeq:        first,
		LastSeq:         last,
		CutPartition:    partition,
		CutSeq:          seq,
		Bytes:           carried,
		CommitTimeMinNS: commitTimeMinNS,
		CommitTimeMaxNS: commitTimeMaxNS,
	}
	frame, err := journalSegmentHeaderFrame(header)
	if err != nil {
		return fmt.Errorf("tagent: state the header of %s: %w", segment.path, err)
	}
	segment.headerBytes = int64(len(frame))
	if err := l.writeSealedSegment(segment, frame); err != nil {
		return err
	}
	l.segments = append(l.segments, segment)
	l.openBase = end
	return writeSegmentIndex(segment)
}

// writeSealedSegment states one sealed segment file: the header frame, then the
// bytes of the interval copied from the open file. The file takes its name by a
// rename, so the segment a reader meets is whole.
//   - The file takes its own barrier before the rename, so the bytes a segment states are the bytes it holds.
//   - The rename is what a stop loses, and what it costs is a boundary rather than a byte: the interval stays in the open file and the next open reads it there.
func (l *journalLog) writeSealedSegment(segment journalSealedSegment, frame []byte) error {
	dir := filepath.Dir(segment.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("tagent: open the segment directory %s: %w", dir, err)
	}
	temp := segment.path + journalSegmentTempSuffix
	file, err := os.OpenFile(temp, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("tagent: open the segment file %s: %w", temp, err)
	}
	discard := func(cause error) error {
		file.Close()
		os.Remove(temp)
		return cause
	}
	if _, err := file.Write(frame); err != nil {
		return discard(fmt.Errorf("tagent: state the header of %s: %w", temp, err))
	}
	source := io.NewSectionReader(l.file, segment.begin, segment.bytes)
	if _, err := io.CopyN(file, source, segment.bytes); err != nil {
		return discard(fmt.Errorf("tagent: copy the log bytes of %s: %w", temp, err))
	}
	if err := file.Sync(); err != nil {
		return discard(fmt.Errorf("tagent: complete the barrier of %s: %w", temp, err))
	}
	if err := file.Close(); err != nil {
		os.Remove(temp)
		return fmt.Errorf("tagent: close the segment file %s: %w", temp, err)
	}
	if err := os.Rename(temp, segment.path); err != nil {
		os.Remove(temp)
		return fmt.Errorf("tagent: name the segment %s: %w", segment.path, err)
	}
	return nil
}

// journalSegmentBytes is the log bytes one sealed segment file carries, as a scan
// of the segment's frames reads them.
type journalSegmentBytes struct {
	segment journalSealedSegment
}

// ReadAt answers the bytes at one byte of the segment's log interval.
func (b journalSegmentBytes) ReadAt(p []byte, off int64) (int, error) {
	return b.segment.readAt(p, off)
}

// Name answers the segment file the bytes come from.
func (b journalSegmentBytes) Name() string {
	return b.segment.path
}

// journalSegmentIndexEntries scans one span of framed log bytes and answers the
// frame carrying the newest write of every key the span locates. It states the
// index of an interval as it closes and the index of a segment whose file is
// missing, so both name the same keys at the same bytes.
//   - A key the span's newest frame deletes states an explicit tombstone at that frame, so an older interval cannot supply a value the delete removed.
//   - Every frame is verified as it is scanned and each entry keeps that frame's own checksum, so the index adds no digest of its own.
//   - A span that ends inside a frame is refused instead of indexed as the frames ahead of it, because a prefix of the span is not the interval the index states.
func journalSegmentIndexEntries(source journalByteSource, from, size int64) (map[string]journalSegmentIndexEntry, error) {
	entries := make(map[string]journalSegmentIndexEntry)
	complete, err := journalReplay(source, from, size, func(payload []byte, at int64) error {
		spans, err := journalSpansAt(payload, at)
		if err != nil {
			return fmt.Errorf("%w: %s: %w", ErrJournalCorrupt, source.Name(), err)
		}
		if !journalRecordVersionReadable(spans.version) {
			return fmt.Errorf("%w: record version %d", ErrJournalFormat, spans.version)
		}
		sum := crc32.Checksum(payload, journalCRCTable)
		for _, put := range spans.puts {
			entries[put.key] = journalSegmentIndexEntry{
				Key:    put.key,
				Offset: at - journalFrameHeaderBytes - from,
				Length: len(payload),
				Sum:    sum,
			}
		}
		for _, key := range spans.deletes {
			entries[key] = journalSegmentIndexEntry{
				Key:     key,
				Offset:  at - journalFrameHeaderBytes - from,
				Length:  len(payload),
				Sum:     sum,
				Deleted: true,
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if complete != size {
		return nil, fmt.Errorf("%w: %s holds no frame ending at the byte %d of its span", ErrJournalCorrupt, source.Name(), complete-from)
	}
	return entries, nil
}

// readSegmentIndex answers the index stated beside one segment, and false beside a
// file this build does not place: no file, a file larger than one frame of the log,
// a frame that fails its checksum, a magic or version of another build, another
// segment's label, or an entry naming bytes outside the segment. Every one of those
// states no index, so the segment is scanned in place of the file and a read never
// answers keys from a statement the segment cannot confirm.
func readSegmentIndex(segment journalSealedSegment) (map[string]journalSegmentIndexEntry, bool) {
	path := journalSegmentIndexPathOf(segment)
	stat, err := os.Stat(path)
	if err != nil || stat.Size() < journalFrameHeaderBytes || stat.Size() > journalFrameHeaderBytes+journalMaxFrameBytes {
		return nil, false
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return parseSegmentIndex(body, journalSegmentLabelOf(segment.id), segment.bytes)
}

// parseSegmentIndex answers the entries one segment index frame states for the named
// segment and the log bytes that segment carries, and false beside bytes this build
// does not place: a checksum that does not hold, a magic, version or label of another
// statement, or an entry naming bytes outside the interval. The index is a derived
// statement, so a caller answers false by scanning the segment rather than by trusting
// an entry the segment cannot confirm.
func parseSegmentIndex(body []byte, label string, bytes int64) (map[string]journalSegmentIndexEntry, bool) {
	payload, err := journalPayloadOf(body)
	if err != nil || len(body) != journalFrameHeaderBytes+len(payload) {
		return nil, false
	}
	var index journalSegmentIndex
	if err := json.Unmarshal(payload, &index); err != nil {
		return nil, false
	}
	if index.Magic != journalSegmentIndexMagic || index.Version != journalSegmentIndexVersion {
		return nil, false
	}
	if index.SegmentID != label {
		return nil, false
	}
	entries := make(map[string]journalSegmentIndexEntry, len(index.Entries))
	for _, entry := range index.Entries {
		if entry.Key == "" || entry.Offset < 0 || entry.Length <= 0 || entry.Offset > bytes {
			return nil, false
		}
		if int64(entry.Length) > bytes-entry.Offset-journalFrameHeaderBytes {
			return nil, false
		}
		if _, held := entries[entry.Key]; held {
			return nil, false
		}
		entries[entry.Key] = entry
	}
	return entries, true
}

// rebuildSegmentIndex answers the index of one segment by scanning its frames,
// which is the representation every index is derived from. The segment is read
// through its header again, so a file that lost bytes or states another format
// refuses the scan instead of answering the keys of a prefix.
func rebuildSegmentIndex(stale journalSealedSegment) (map[string]journalSegmentIndexEntry, error) {
	segment, err := readSealedSegment(stale.id, stale.path, stale.begin)
	if err != nil {
		return nil, err
	}
	return journalSegmentIndexEntries(journalSegmentBytes{segment: segment}, 0, segment.bytes)
}

// sealedSegmentIndexOf answers every key one sealed segment locates a value for,
// each at the frame carrying its newest write in that segment. It reads the index
// beside the segment and scans the segment beside no index this build places; the
// open reads neither, so a boot pays no index pass and the file is an accelerator
// for the key a caller locates.
//   - A segment that cannot be scanned is refused as ErrSegmentIndexUnrebuildable, never answered as a segment that locates no key.
func sealedSegmentIndexOf(segment journalSealedSegment) (map[string]journalSegmentIndexEntry, error) {
	if entries, stated := readSegmentIndex(segment); stated {
		return entries, nil
	}
	entries, err := rebuildSegmentIndex(segment)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrSegmentIndexUnrebuildable, segment.path, err)
	}
	return entries, nil
}

// segmentIndexFrameOf states the one frame of a segment index: the label of the
// segment it indexes and, per key, the frame that carries the key's newest write,
// ordered by key so one segment and its key set state the same index bytes. A
// caller that holds the entries of a segment states its index without reading the
// file beside it, because the entries are the statement and the file is one copy of
// them.
func segmentIndexFrameOf(label string, entries map[string]journalSegmentIndexEntry) ([]byte, error) {
	index := journalSegmentIndex{
		Magic:     journalSegmentIndexMagic,
		Version:   journalSegmentIndexVersion,
		SegmentID: label,
		Entries:   make([]journalSegmentIndexEntry, 0, len(entries)),
	}
	for _, entry := range entries {
		index.Entries = append(index.Entries, entry)
	}
	sort.Slice(index.Entries, func(i, j int) bool { return index.Entries[i].Key < index.Entries[j].Key })
	body, err := json.Marshal(index)
	if err != nil {
		return nil, fmt.Errorf("tagent: state the segment index of %s: %w", label, err)
	}
	return journalFrameOf(body), nil
}

// writeSegmentIndex states the index of one sealed segment beside it: one frame
// naming the frame that carries each key the segment locates, written under a
// temporary name and renamed into place.
//   - The file takes its own barrier before the rename, so an index a reader meets is whole.
//   - The entries are ordered by key, so one segment and its key set state the same index bytes.
//   - The segment is recorded by the seal before this runs, so a segment whose index cannot be stated still names the interval it closed and a reader that misses the index scans the segment.
func writeSegmentIndex(segment journalSealedSegment) error {
	entries, err := journalSegmentIndexEntries(journalSegmentBytes{segment: segment}, 0, segment.bytes)
	if err != nil {
		return fmt.Errorf("tagent: scan the segment %s for its index: %w", segment.path, err)
	}
	frame, err := segmentIndexFrameOf(journalSegmentLabelOf(segment.id), entries)
	if err != nil {
		return err
	}
	path := journalSegmentIndexPathOf(segment)
	temp := path + journalSegmentTempSuffix
	file, err := os.OpenFile(temp, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("tagent: open the index file %s: %w", temp, err)
	}
	discard := func(cause error) error {
		file.Close()
		os.Remove(temp)
		return cause
	}
	if _, err := file.Write(frame); err != nil {
		return discard(fmt.Errorf("tagent: state the index of %s: %w", temp, err))
	}
	if err := file.Sync(); err != nil {
		return discard(fmt.Errorf("tagent: complete the barrier of %s: %w", temp, err))
	}
	if err := file.Close(); err != nil {
		os.Remove(temp)
		return fmt.Errorf("tagent: close the index file %s: %w", temp, err)
	}
	if err := os.Rename(temp, path); err != nil {
		os.Remove(temp)
		return fmt.Errorf("tagent: name the index %s: %w", path, err)
	}
	return nil
}
