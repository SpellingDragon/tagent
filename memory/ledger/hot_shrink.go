// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
//
// The hot directory of one journal holds the intervals its cuts closed behind the
// open segment. A move copies the oldest of them into the archive and states them
// in one manifest generation; the hot files then shrink to the intervals that
// generation leaves behind, so the log loses the copied bytes and the index states
// the keys of the bytes that remain. The manifest is the only statement of which
// intervals left, so a hot directory it does not account for is refused by name
// instead of rewritten.
package ledger

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// errHotSegmentUnstated reports a hot directory whose sealed intervals the archive
// manifest does not account for as one prefix: a stated interval with an older hot
// interval the manifest does not state beside it, a hot copy the statement places
// behind an unstated interval, or a gap no statement fills. The bytes of an
// interval nobody states are never cut away as bytes that left.
var errHotSegmentUnstated = errors.New("memory: the archive manifest does not state the hot log intervals")

// errHotWindowUnstated reports a manifest whose hot window this build cannot
// restate: a generation stating no hot figures, figures the hot files contradict,
// or a rebuilt index whose watermark differs from the stated one. The hot files are
// the bytes a reader answers from, so a window nobody stated is refused instead of
// guessed.
var errHotWindowUnstated = errors.New("memory: the archive manifest states no hot log window this build can restate")

// hotShrinkTempSuffix names the file a shrunk log is written into before it takes
// the log's name, so a stop inside the rewrite leaves the log it started with.
const hotShrinkTempSuffix = ".shrink"

// shrinkHotIndexAfterArchive shrinks the hot files of one journal directory to the
// sealed intervals its archive manifest does not state: the hot copies of the stated
// intervals go, the log loses the bytes in front of the oldest interval that
// remains, and the slot table and order file are stated again from the bytes that
// remain, with the slot header stating their watermarks.
//   - Only a hot copy the manifest states is deleted, and the stated intervals must form the prefix the archive moved, so a manifest that skips a hot interval is refused as errHotSegmentUnstated with every file left as it stood.
//   - The cut lands at the byte the manifest's hot window states, and the bytes behind it must be the retained intervals followed by the open segment; a window the hot files contradict is refused as errHotWindowUnstated.
//   - A directory whose log already starts at its oldest retained interval is settled by the open alone, so a repeated call changes nothing.
//   - The caller holds the root's write rights with no view of the directory open, because the log is rewritten and the index files are discarded under the handles a writer would hold.
func shrinkHotIndexAfterArchive(hotDir string, manifest *archiveManifest) error {
	segments, err := openSealedSegments(hotDir)
	if err != nil {
		return err
	}
	stated, statedIDs, err := statedArchiveSegments(manifest)
	if err != nil {
		return err
	}
	if len(statedIDs) == 0 {
		return nil
	}
	newest := statedIDs[len(statedIDs)-1]
	ids := make(map[int64]bool, len(statedIDs)+len(segments))
	for _, id := range statedIDs {
		ids[id] = true
	}
	for _, segment := range segments {
		ids[segment.id] = true
	}
	reach := make([]int64, 0, len(ids))
	for id := range ids {
		reach = append(reach, id)
	}
	sort.Slice(reach, func(i, j int) bool { return reach[i] < reach[j] })
	for at, id := range reach {
		if id != int64(at)+1 {
			return fmt.Errorf("%w: the intervals of %s skip the id %d", errHotSegmentUnstated, hotDir, at+1)
		}
	}
	covered := make([]journalSealedSegment, 0, len(segments))
	retained := make([]journalSealedSegment, 0, len(segments))
	for _, segment := range segments {
		if segment.id > newest {
			retained = append(retained, segment)
			continue
		}
		if !stated[journalSegmentLabelOf(segment.id)] {
			return fmt.Errorf("%w: %s", errHotSegmentUnstated, segment.path)
		}
		covered = append(covered, segment)
	}
	if len(retained) == 0 {
		return fmt.Errorf("%w: %s holds no interval the archive leaves behind", errHotSegmentUnstated, hotDir)
	}
	retainedBytes := int64(0)
	for _, segment := range retained {
		retainedBytes += segment.bytes
	}
	logPath := filepath.Join(hotDir, journalFileName)
	stat, err := os.Stat(logPath)
	if err != nil {
		return fmt.Errorf("tagent: read the log of %s: %w", hotDir, err)
	}
	size := stat.Size()
	if retainedBytes > size {
		return fmt.Errorf("%w: %s holds %d byte(s) beside the %d its retained intervals state",
			errHotWindowUnstated, logPath, size, retainedBytes)
	}
	shrunk, err := hotLogPrefixStatesSegments(logPath, retained, retainedBytes)
	if err != nil {
		return err
	}
	if shrunk {
		if err := removeHotSegments(hotDir, covered); err != nil {
			return err
		}
		return settleShrunkHotLog(hotDir, nil)
	}
	window, statedWindow := archiveHotWindowOf(manifest)
	if !statedWindow {
		return fmt.Errorf("%w: the manifest states no hot figures for %s", errHotWindowUnstated, hotDir)
	}
	drop := size - window.HotValuesSize
	if drop <= 0 || window.HotValuesSize < retainedBytes {
		return fmt.Errorf("%w: %s holds %d byte(s) beside the stated hot window of %d byte(s) over %d retained byte(s)",
			errHotWindowUnstated, logPath, size, window.HotValuesSize, retainedBytes)
	}
	if err := cutJournalFront(logPath, drop, size, retained, retainedBytes); err != nil {
		return err
	}
	if err := removeHotSegments(hotDir, covered); err != nil {
		return err
	}
	return settleShrunkHotLog(hotDir, &window)
}

// archiveWatermarkAfterMove answers the hot figures a shrink of the given prefix
// states. The prefix carries whole sealed intervals, and the remaining intervals
// plus the open segment are replayed to restate commit ranks and order bytes.
func archiveWatermarkAfterMove(log *journalLog, moving []journalSealedSegment) (archiveWatermark, error) {
	if len(moving) == 0 || len(moving) >= len(log.segments) {
		return archiveWatermark{}, fmt.Errorf("%w: the move leaves no newest sealed interval hot", errHotWindowUnstated)
	}
	drop := int64(0)
	for at, segment := range moving {
		if segment.id != log.segments[at].id {
			return archiveWatermark{}, fmt.Errorf("%w: the move starts with %s beside %s",
				errHotWindowUnstated, journalSegmentLabelOf(segment.id), journalSegmentLabelOf(log.segments[at].id))
		}
		drop += segment.bytes
	}
	if drop > log.size {
		return archiveWatermark{}, fmt.Errorf("%w: the moved intervals carry %d byte(s) beside a log of %d",
			errHotWindowUnstated, drop, log.size)
	}
	retained := log.segments[len(moving):]
	seq := retained[0].firstSeq - 1
	orderSize := int64(0)
	complete, err := journalReplay(journalLogBytes{log: log}, drop, log.size, func(payload []byte, at int64) error {
		spans, err := journalSpansAt(payload, at-drop)
		if err != nil {
			return fmt.Errorf("%w: %s: %w", ErrJournalCorrupt, log.path, err)
		}
		if !journalRecordVersionReadable(spans.version) {
			return fmt.Errorf("%w: record version %d", ErrJournalFormat, spans.version)
		}
		seq++
		valuesEnd := at + int64(len(payload)) - drop
		line, err := journalOrderLine(orderStatementOf(seq, valuesEnd, spans))
		if err != nil {
			return err
		}
		orderSize += int64(len(line))
		return nil
	})
	if err != nil {
		return archiveWatermark{}, err
	}
	if complete != log.size {
		return archiveWatermark{}, fmt.Errorf("%w: the retained log ends at %d beside %d", ErrJournalCorrupt, complete, log.size)
	}
	return archiveWatermark{HotSeq: seq, HotValuesSize: log.size - drop, HotOrderSize: orderSize}, nil
}

// statedArchiveSegments answers the segment labels one manifest states beside the
// ids they reach, oldest first. A label that states no segment of this log, a label
// stated twice and a set of labels that states no prefix of the log's ids are
// refused, because the archive moves the oldest intervals and states them as one.
func statedArchiveSegments(manifest *archiveManifest) (map[string]bool, []int64, error) {
	stated := map[string]bool{}
	if manifest == nil {
		return stated, nil, nil
	}
	ids := make([]int64, 0, len(manifest.Segments))
	for _, segment := range manifest.Segments {
		id, held := journalSegmentIDOf(segment.SegmentID + journalSegmentNameSuffix)
		if !held || journalSegmentLabelOf(id) != segment.SegmentID {
			return nil, nil, fmt.Errorf("%w: segment identity %q", ErrArchiveCorrupt, segment.SegmentID)
		}
		if stated[segment.SegmentID] {
			return nil, nil, fmt.Errorf("%w: two entries state the segment %s", ErrArchiveCorrupt, segment.SegmentID)
		}
		stated[segment.SegmentID] = true
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for at, id := range ids {
		if id != int64(at)+1 {
			return nil, nil, fmt.Errorf("%w: the stated ids %v are no prefix of the log's intervals", errHotSegmentUnstated, ids)
		}
	}
	return stated, ids, nil
}

// archiveHotWindowOf answers the hot figures the newest segment of one manifest
// states in its watermark, and false beside a manifest that states none. A stated
// hot window holds at least one commit and one order line, so a figure of zero
// states no window.
func archiveHotWindowOf(manifest *archiveManifest) (archiveWatermark, bool) {
	var newest archiveSegment
	newestID := int64(0)
	for _, segment := range manifest.Segments {
		id, held := journalSegmentIDOf(segment.SegmentID + journalSegmentNameSuffix)
		if held && id > newestID {
			newest, newestID = segment, id
		}
	}
	if newestID == 0 || newest.Watermark.HotValuesSize <= 0 {
		return archiveWatermark{}, false
	}
	return newest.Watermark, true
}

// hotLogPrefixStatesSegments reports whether one log starts with the interval bytes
// of the given segments, in order, so a cut that lands on another byte is refused
// instead of read as the intervals it keeps.
func hotLogPrefixStatesSegments(path string, segments []journalSealedSegment, total int64) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("tagent: open %s: %w", path, err)
	}
	defer file.Close()
	prefix := make([]byte, total)
	if _, err := io.ReadFull(file, prefix); err != nil {
		return false, fmt.Errorf("%w: %s holds no %d byte(s) of the intervals it keeps: %w",
			errHotWindowUnstated, path, total, err)
	}
	at := int64(0)
	for _, segment := range segments {
		payload := make([]byte, segment.bytes)
		if _, err := segment.readAt(payload, 0); err != nil {
			return false, err
		}
		if !bytes.Equal(prefix[at:at+segment.bytes], payload) {
			return false, nil
		}
		at += segment.bytes
	}
	return true, nil
}

// cutJournalFront rewrites the log of one journal directory to the bytes from the
// given log byte on, after the bytes in front of it are the retained intervals the
// caller states.
//   - The bytes are written under a temporary name, completed by a barrier and compared against the retained intervals before they take the log's name.
//   - The slot header is discarded before the rename, so an open beside the cut log rebuilds the index instead of reading a watermark past the bytes it holds.
func cutJournalFront(logPath string, drop, size int64, retained []journalSealedSegment, retainedBytes int64) error {
	source, err := os.Open(logPath)
	if err != nil {
		return fmt.Errorf("tagent: open the log of %s: %w", logPath, err)
	}
	temp := logPath + hotShrinkTempSuffix
	out, err := os.OpenFile(temp, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		source.Close()
		return fmt.Errorf("tagent: open %s: %w", temp, err)
	}
	discard := func(cause error) error {
		source.Close()
		out.Close()
		os.Remove(temp)
		return cause
	}
	written, err := io.Copy(out, io.NewSectionReader(source, drop, size-drop))
	if err != nil {
		return discard(fmt.Errorf("tagent: cut the front of %s: %w", logPath, err))
	}
	if written != size-drop {
		return discard(fmt.Errorf("tagent: cut the front of %s: wrote %d of %d byte(s)", logPath, written, size-drop))
	}
	if err := out.Sync(); err != nil {
		return discard(fmt.Errorf("tagent: complete the barrier of %s: %w", temp, err))
	}
	if err := out.Close(); err != nil {
		source.Close()
		os.Remove(temp)
		return fmt.Errorf("tagent: close %s: %w", temp, err)
	}
	if err := source.Close(); err != nil {
		os.Remove(temp)
		return fmt.Errorf("tagent: close %s: %w", logPath, err)
	}
	stated, err := hotLogPrefixStatesSegments(temp, retained, retainedBytes)
	if err != nil {
		os.Remove(temp)
		return err
	}
	if !stated {
		os.Remove(temp)
		return fmt.Errorf("%w: the bytes behind the stated cut of %s are not the intervals it keeps", errHotWindowUnstated, logPath)
	}
	slotsPath := filepath.Join(filepath.Dir(logPath), journalSlotFileName)
	if err := os.Truncate(slotsPath, 0); err != nil && !errors.Is(err, os.ErrNotExist) {
		os.Remove(temp)
		return fmt.Errorf("tagent: discard the slot table %s: %w", slotsPath, err)
	}
	if err := os.Rename(temp, logPath); err != nil {
		os.Remove(temp)
		return fmt.Errorf("tagent: name the shrunk log %s: %w", logPath, err)
	}
	return nil
}

// settleShrunkHotLog opens the shrunk hot log, so an index the discarded header left
// behind is stated again from the bytes the log holds, and answers a stated hot
// window beside the watermark the rebuilt index carries: a manifest a boot restates
// against describes the files beside it.
func settleShrunkHotLog(hotDir string, window *archiveWatermark) error {
	log, err := openJournalLogFace(hotDir, true)
	if err != nil {
		return err
	}
	var windowErr error
	if window != nil {
		switch {
		case log.addr.valuesSize != window.HotValuesSize:
			windowErr = fmt.Errorf("%w: the shrunk log ends at the byte %d beside the stated %d",
				errHotWindowUnstated, log.addr.valuesSize, window.HotValuesSize)
		case window.HotSeq > 0 && log.addr.seq != window.HotSeq:
			windowErr = fmt.Errorf("%w: the shrunk index states the last commit %d beside the stated %d",
				errHotWindowUnstated, log.addr.seq, window.HotSeq)
		case window.HotOrderSize > 0 && log.addr.orderSize != window.HotOrderSize:
			windowErr = fmt.Errorf("%w: the shrunk order file states %d byte(s) beside the stated %d",
				errHotWindowUnstated, log.addr.orderSize, window.HotOrderSize)
		}
	}
	return errors.Join(windowErr, log.close())
}
