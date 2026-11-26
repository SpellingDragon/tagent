// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
//
// The archive of a journal root holds the sealed segments a cut has closed behind
// it, stated by one manifest generation: the segments it carries, the key domain
// each one answers, its own byte count and digest, the cut its boundary closed at
// and the offset map that locates a key inside it. The manifest is the only
// statement of which segments live in the archive; the segments themselves stay
// self-describing, and a segment or index a statement misses is copied again
// rather than guessed at. A move follows one order — stage the copies and their
// barriers, state the generation that names them with their digests, give the
// staged copies those names, delete the hot copies — so a named segment or index
// file stands only behind the generation that states it, and every stop in the
// move is recovered by reading the manifest back: the names it states are
// finished, and the staged files no statement names are removed. A root holding a
// named file beside no manifest is therefore corrupt rather than empty.
package ledger

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrArchiveCorrupt reports an archive whose statement this build cannot read: a
// manifest file present without one generation it can decode, two slots stating
// one generation in different words, a stated segment whose bytes or digest do
// not match the statement, or a key domain the segments do not answer. The
// statement itself is damaged, so no read is served from a guess.
var ErrArchiveCorrupt = errors.New("memory: archive manifest is not readable as one generation")

// ErrArchiveMissing reports a segment the manifest states whose bytes are absent:
// the authority names an interval the archive does not hold, which is never read
// as the empty archive a root without any manifest states.
var ErrArchiveMissing = errors.New("memory: a segment the archive manifest states is absent")

const (
	// archiveManifestAName and archiveManifestBName name the two files one manifest
	// generation is stated in: each write states the new generation in both, and a
	// reader takes the greater generation it can decode.
	archiveManifestAName = "manifest.a"
	archiveManifestBName = "manifest.b"
	// archiveManifestVersion is the manifest format this build writes and the only
	// one it reads.
	archiveManifestVersion = 1
)

// archiveKeyDomain states the keys one archived segment answers: the conservative
// interval per partition beside the digest of the segment's sorted key set, so a
// lookup reads the interval and a segment never answers a key its domain excludes.
type archiveKeyDomain struct {
	Partition int    `json:"partition"`
	FirstKey  string `json:"first_key"`
	LastKey   string `json:"last_key"`
	KeysSHA   string `json:"keys_sha256"`
}

// archiveKeyDomainOf computes one segment's conservative interval and the digest
// of its sorted index keys, including keys whose newest entry is a tombstone. The
// digest covers the canonical JSON of the sorted key set, which states the boundary
// between neighbouring keys; a separator join would spell two key sets alike. A
// string slice always marshals, so the failure branch states no key set at all: the
// domain carries no digest and no manifest that recorded one matches it.
func archiveKeyDomainOf(partition int, entries map[string]journalSegmentIndexEntry) archiveKeyDomain {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	domain := archiveKeyDomain{Partition: partition}
	if len(keys) == 0 {
		return domain
	}
	domain.FirstKey = keys[0]
	domain.LastKey = keys[len(keys)-1]
	if body, err := json.Marshal(keys); err == nil {
		domain.KeysSHA = digestOf(body)
	}
	return domain
}

// archiveCut states the cut one segment boundary closed at: the checkpoint it
// filed and the byte of the segment the commit batch starts at.
type archiveCut struct {
	Partition   int   `json:"partition"`
	Seq         int64 `json:"seq"`
	FrameOffset int64 `json:"frame_offset"`
}

// archiveOffsetMap states where the offset map of one archived segment lives: the
// commit ranks it spans, its index entry count, and the path and digest of the
// index file beside the segment.
type archiveOffsetMap struct {
	FirstSeq int64 `json:"first_seq"`
	LastSeq  int64 `json:"last_seq"`
	// FrameCount is the number of index entries, one per commit key. The manifest
	// states it under frame_count, so it is neither the frame count of the index
	// file nor a count of distinct frame offsets.
	FrameCount  int    `json:"frame_count"`
	IndexPath   string `json:"index_path"`
	IndexSHA256 string `json:"index_sha256"`
}

// archiveWatermark states how far the archive has moved: the cut it is through and
// the hot-file figures a boot restates its hot index against.
type archiveWatermark struct {
	ArchivedThroughCut archiveCut `json:"archived_through_cut"`
	HotSeq             int64      `json:"hot_seq"`
	HotValuesSize      int64      `json:"hot_values_size"`
	HotOrderSize       int64      `json:"hot_order_size"`
}

// archiveSegment is one segment as the manifest states it.
type archiveSegment struct {
	SegmentID string           `json:"segment_id"`
	KeyDomain archiveKeyDomain `json:"key_domain"`
	Bytes     int64            `json:"bytes"`
	SHA256    string           `json:"sha256"`
	Cut       archiveCut       `json:"cut"`
	OffsetMap archiveOffsetMap `json:"offset_map"`
	Watermark archiveWatermark `json:"watermark"`
}

// archiveManifest is one generation of the archive statement.
type archiveManifest struct {
	Version            int              `json:"version"`
	Generation         int              `json:"generation"`
	Segments           []archiveSegment `json:"segments"`
	ArchivedThroughCut archiveCut       `json:"archived_through_cut"`
}

// parseArchiveManifest answers the statement one manifest file holds, and false
// beside bytes this build does not place: another version, a generation before
// the first, or a segment that states no identity, bytes or digest.
func parseArchiveManifest(raw []byte) (archiveManifest, bool) {
	var manifest archiveManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return archiveManifest{}, false
	}
	if manifest.Version != archiveManifestVersion || manifest.Generation < 1 {
		return archiveManifest{}, false
	}
	for _, segment := range manifest.Segments {
		if segment.SegmentID == "" || segment.Bytes <= 0 || segment.SHA256 == "" {
			return archiveManifest{}, false
		}
	}
	return manifest, true
}

// readArchiveManifest answers the greatest generation both slots state, and false
// beside a root whose two manifest files are both absent and whose segment
// directory holds no named file, which is the empty archive a root without a
// manifest holds.
//   - A root holding any manifest file but no decodable generation is corrupt, never empty.
//   - Two slots stating one generation in different words are corrupt, because either could be the authority.
//   - A root holding a named segment or index file beside no manifest is corrupt: a name is taken only behind the generation that states it, so the file states a generation whose statement this root lost.
//   - A named segment or index file the greatest valid generation does not state is corrupt, because the final name could only follow that statement.
func readArchiveManifest(root string) (*archiveManifest, bool, error) {
	type candidate struct {
		manifest archiveManifest
		raw      []byte
	}
	present := false
	held := make([]candidate, 0, 2)
	for _, name := range []string{archiveManifestAName, archiveManifestBName} {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, false, fmt.Errorf("%w: read %s: %w", ErrArchiveCorrupt, name, err)
		}
		present = true
		if manifest, ok := parseArchiveManifest(raw); ok {
			held = append(held, candidate{manifest: manifest, raw: raw})
		}
	}
	if !present {
		named, err := archiveNamedArtifacts(root)
		if err != nil {
			return nil, false, fmt.Errorf("%w: read the segment directory of %s: %w", ErrArchiveCorrupt, root, err)
		}
		if len(named) > 0 {
			return nil, false, fmt.Errorf("%w: %s holds the named segment files %v beside no manifest", ErrArchiveCorrupt, root, named)
		}
		return nil, false, nil
	}
	if len(held) == 0 {
		return nil, false, fmt.Errorf("%w: %s holds no manifest generation this build reads", ErrArchiveCorrupt, root)
	}
	best := held[0]
	for _, other := range held[1:] {
		if other.manifest.Generation > best.manifest.Generation {
			best = other
			continue
		}
		if other.manifest.Generation == best.manifest.Generation && !bytes.Equal(other.raw, best.raw) {
			return nil, false, fmt.Errorf("%w: two slots state generation %d differently", ErrArchiveCorrupt, other.manifest.Generation)
		}
	}
	named, err := archiveNamedArtifacts(root)
	if err != nil {
		return nil, false, fmt.Errorf("%w: read the segment directory of %s: %w", ErrArchiveCorrupt, root, err)
	}
	stated, err := statedArchiveFiles(root, &best.manifest)
	if err != nil {
		return nil, false, err
	}
	for _, name := range named {
		if !stated[name] {
			return nil, false, archiveCorruptOf("%s holds the segment file %s beside no statement in generation %d", root, name, best.manifest.Generation)
		}
	}
	return &best.manifest, true, nil
}

// archiveNamedArtifact reports whether one name is a final artifact a statement
// carries: the file of a segment or the index beside it, never the staged name a
// stopped copy leaves.
func archiveNamedArtifact(name string) bool {
	if _, held := journalSegmentIDOf(name); held {
		return true
	}
	if !strings.HasSuffix(name, journalSegmentIndexSuffix) {
		return false
	}
	_, held := journalSegmentIDOf(strings.TrimSuffix(name, journalSegmentIndexSuffix) + journalSegmentNameSuffix)
	return held
}

// archiveNamedArtifacts answers the final artifact names one archive root's
// segment directory holds. A directory that is absent, or a path of another kind,
// holds no entry; a directory this build cannot read is reported rather than read
// as empty.
func archiveNamedArtifacts(root string) ([]string, error) {
	segDir := filepath.Join(root, journalSegmentDirName)
	entries, err := os.ReadDir(segDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		if stat, statErr := os.Stat(segDir); statErr == nil && !stat.IsDir() {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !archiveNamedArtifact(entry.Name()) {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

// archiveSegmentPathOf answers the file one stated segment takes under an archive
// root: the label the manifest states and the suffix every segment file carries.
func archiveSegmentPathOf(archiveRoot, segmentID string) string {
	return filepath.Join(archiveRoot, journalSegmentDirName, segmentID+journalSegmentNameSuffix)
}

// archiveIndexPathOf answers the file one stated segment's index takes under an
// archive root. A stated path that names another directory is refused as corrupt,
// so a statement can name no file outside the archive.
func archiveIndexPathOf(archiveRoot string, segment archiveSegment) (string, error) {
	name := segment.OffsetMap.IndexPath
	if name == "" || filepath.Base(name) != name {
		return "", archiveCorruptOf("the manifest states the index %q of %s", name, segment.SegmentID)
	}
	return filepath.Join(archiveRoot, journalSegmentDirName, name), nil
}

// writeArchiveManifest states the next generation of one manifest in both slots,
// each under a temporary name completed by a barrier and a rename, so a reader
// meets a whole statement or the generation before it.
func writeArchiveManifest(root string, manifest archiveManifest) error {
	current, _, err := readArchiveManifest(root)
	if err != nil {
		return err
	}
	manifest.Version = archiveManifestVersion
	manifest.Generation = 1
	if current != nil {
		manifest.Generation = current.Generation + 1
	}
	if manifest.Segments == nil {
		manifest.Segments = []archiveSegment{}
	}
	body, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("tagent: state the archive manifest: %w", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("tagent: open the archive root %s: %w", root, err)
	}
	for _, name := range []string{archiveManifestAName, archiveManifestBName} {
		if err := writeArchiveManifestSlot(filepath.Join(root, name), body); err != nil {
			return err
		}
	}
	return nil
}

// writeArchiveManifestSlot states one manifest slot: the bytes under a temporary
// name, its own barrier, then the rename a reader meets.
func writeArchiveManifestSlot(path string, body []byte) error {
	temp := path + ".tmp"
	file, err := os.OpenFile(temp, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("tagent: open the manifest slot %s: %w", temp, err)
	}
	discard := func(cause error) error {
		file.Close()
		os.Remove(temp)
		return cause
	}
	if _, err := file.Write(body); err != nil {
		return discard(fmt.Errorf("tagent: state the manifest slot %s: %w", temp, err))
	}
	if err := file.Sync(); err != nil {
		return discard(fmt.Errorf("tagent: complete the barrier of %s: %w", temp, err))
	}
	if err := file.Close(); err != nil {
		os.Remove(temp)
		return fmt.Errorf("tagent: close the manifest slot %s: %w", temp, err)
	}
	if err := os.Rename(temp, path); err != nil {
		os.Remove(temp)
		return fmt.Errorf("tagent: name the manifest slot %s: %w", path, err)
	}
	return nil
}

// digestOfFile answers the canonical digest of one file's bytes, computed over the
// file as it stands rather than read from a statement about it.
func digestOfFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	sum := sha256.New()
	written, err := io.Copy(sum, file)
	if err != nil {
		return "", 0, err
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil)), written, nil
}

// archiveSegmentEntryOf states one sealed segment as the manifest carries it: the
// cut and commit ranks its header states, the key domain and offset map its index
// answers, and the file's own byte count and digest.
func archiveSegmentEntryOf(segment journalSealedSegment) (archiveSegment, []byte, error) {
	file, err := os.Open(segment.path)
	if err != nil {
		return archiveSegment{}, nil, fmt.Errorf("tagent: open the segment %s: %w", segment.path, err)
	}
	header, headerBytes, err := journalSegmentHeaderOf(file, segment.path)
	file.Close()
	if err != nil {
		return archiveSegment{}, nil, err
	}
	entries, err := sealedSegmentIndexOf(segment)
	if err != nil {
		return archiveSegment{}, nil, err
	}
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	domain := archiveKeyDomainOf(header.CutPartition, entries)
	cutOffset := int64(-1)
	for _, key := range keys {
		partition, seq, checkpoint, valid := checkpointCutOfKey(key)
		if !checkpoint || !valid || partition != header.CutPartition || seq != header.CutSeq {
			continue
		}
		offset := entries[key].Offset
		if cutOffset >= 0 && cutOffset != offset {
			return archiveSegment{}, nil, fmt.Errorf("%w: %s states its cut at partition %d seq %d in different frames",
				ErrArchiveCorrupt, segment.path, header.CutPartition, header.CutSeq)
		}
		cutOffset = offset
	}
	if cutOffset < 0 {
		return archiveSegment{}, nil, fmt.Errorf("%w: %s states the cut at partition %d seq %d but names no checkpoint for it",
			ErrArchiveCorrupt, segment.path, header.CutPartition, header.CutSeq)
	}
	indexFrame, err := segmentIndexFrameOf(journalSegmentLabelOf(segment.id), entries)
	if err != nil {
		return archiveSegment{}, nil, err
	}
	digest, size, err := digestOfFile(segment.path)
	if err != nil {
		return archiveSegment{}, nil, fmt.Errorf("tagent: digest the segment %s: %w", segment.path, err)
	}
	if header.Bytes < 0 || size != headerBytes+header.Bytes {
		return archiveSegment{}, nil, fmt.Errorf("%w: %s holds %d byte(s) beside the header stating %d of its own",
			ErrArchiveCorrupt, segment.path, size, headerBytes+header.Bytes)
	}
	return archiveSegment{
		SegmentID: journalSegmentLabelOf(segment.id),
		KeyDomain: domain,
		Bytes:     size,
		SHA256:    digest,
		Cut: archiveCut{
			Partition:   header.CutPartition,
			Seq:         header.CutSeq,
			FrameOffset: cutOffset,
		},
		OffsetMap: archiveOffsetMap{
			FirstSeq:    header.FirstSeq,
			LastSeq:     header.LastSeq,
			FrameCount:  len(entries),
			IndexPath:   filepath.Base(journalSegmentIndexPathOf(segment)),
			IndexSHA256: digestOf(indexFrame),
		},
	}, indexFrame, nil
}

// stageArchiveCopy copies one file into the archive under the temporary name beside
// the final name a statement will carry, and completes the copy's own barrier, so
// the bytes stand on the archive volume before any statement names them. The final
// name is taken by promoteStagedArchiveFile.
func stageArchiveCopy(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("tagent: open %s: %w", source, err)
	}
	defer in.Close()
	temp := target + journalSegmentTempSuffix
	out, err := os.OpenFile(temp, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("tagent: open %s: %w", temp, err)
	}
	discard := func(cause error) error {
		out.Close()
		os.Remove(temp)
		return cause
	}
	if _, err := io.Copy(out, in); err != nil {
		return discard(fmt.Errorf("tagent: copy %s: %w", source, err))
	}
	if err := out.Sync(); err != nil {
		return discard(fmt.Errorf("tagent: complete the barrier of %s: %w", temp, err))
	}
	if err := out.Close(); err != nil {
		os.Remove(temp)
		return fmt.Errorf("tagent: close %s: %w", temp, err)
	}
	return nil
}

// promoteStagedArchiveFile gives one staged archive file the final name a statement
// carries. A staged name that is absent is reported, so a caller never answers a
// file it did not write.
func promoteStagedArchiveFile(target string) error {
	if err := os.Rename(target+journalSegmentTempSuffix, target); err != nil {
		return fmt.Errorf("tagent: name the archive file %s: %w", target, err)
	}
	return nil
}

// removeStagedArchiveFile removes the staged file beside one final name, which is a
// copy no statement needs once the final name stands.
func removeStagedArchiveFile(target string) error {
	if err := os.Remove(target + journalSegmentTempSuffix); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("tagent: remove the staged archive file %s: %w", target, err)
	}
	return nil
}

// moveSealedSegmentsToArchive moves the named segments into the archive root in
// the contract's order — stage each segment and its index beside the names a
// statement carries, state the next generation that names them with their bytes
// and digests, take those names for the staged copies, and delete the hot copies
// last — and reports the stored generation.
//   - A stop before the statement leaves staged copies no generation names, with every hot copy standing.
//   - A stop after the statement leaves a generation whose names recovery finishes, with every hot copy still standing until then.
//   - A segment the current generation already states is not copied again: a repeated move states nothing new and still removes the hot copy it finds.
func moveSealedSegmentsToArchive(hotDir, archiveRoot string, segments []journalSealedSegment, watermark archiveWatermark) (*archiveManifest, error) {
	current, _, err := readArchiveManifest(archiveRoot)
	if err != nil {
		return nil, err
	}
	next := archiveManifest{Segments: []archiveSegment{}}
	stated := map[string]bool{}
	if current != nil {
		next.Segments = append(next.Segments, current.Segments...)
		next.ArchivedThroughCut = current.ArchivedThroughCut
		for _, segment := range current.Segments {
			stated[segment.SegmentID] = true
		}
	}
	segDir := filepath.Join(archiveRoot, journalSegmentDirName)
	if err := os.MkdirAll(segDir, 0o755); err != nil {
		return nil, fmt.Errorf("tagent: open the archive segment directory %s: %w", segDir, err)
	}
	added := make([]archiveSegment, 0, len(segments))
	for _, segment := range segments {
		entry, indexFrame, err := archiveSegmentEntryOf(segment)
		if err != nil {
			return nil, err
		}
		if stated[entry.SegmentID] {
			continue
		}
		entry.Watermark = watermark
		segmentPath := archiveSegmentPathOf(archiveRoot, entry.SegmentID)
		if err := stageArchiveCopy(segment.path, segmentPath); err != nil {
			return nil, err
		}
		if matched, _, err := archiveFileMatches(segmentPath+journalSegmentTempSuffix, entry.Bytes, entry.SHA256, archiveFinishDigests); err != nil {
			return nil, err
		} else if !matched {
			return nil, archiveCorruptOf("the staged copy of %s does not carry the %d byte(s) %s it states",
				entry.SegmentID, entry.Bytes, entry.SHA256)
		}
		indexPath, err := archiveIndexPathOf(archiveRoot, entry)
		if err != nil {
			return nil, err
		}
		if err := stageArchiveBody(indexPath, indexFrame); err != nil {
			return nil, err
		}
		if matched, _, _, err := archiveIndexMatches(indexPath+journalSegmentTempSuffix, entry.OffsetMap.IndexSHA256, archiveFinishDigests); err != nil {
			return nil, err
		} else if !matched {
			return nil, archiveCorruptOf("the staged index of %s does not match the digest it states", entry.SegmentID)
		}
		added = append(added, entry)
	}
	if len(added) > 0 {
		next.Segments = append(next.Segments, added...)
		sort.Slice(next.Segments, func(i, j int) bool { return next.Segments[i].SegmentID < next.Segments[j].SegmentID })
		next.ArchivedThroughCut = added[len(added)-1].Cut
	}
	stored := next
	if len(added) > 0 || current == nil {
		if err := writeArchiveManifest(archiveRoot, next); err != nil {
			return nil, err
		}
		reloaded, _, err := readArchiveManifest(archiveRoot)
		if err != nil {
			return nil, err
		}
		stored = *reloaded
	}
	for _, entry := range added {
		if err := promoteStagedArchiveFile(archiveSegmentPathOf(archiveRoot, entry.SegmentID)); err != nil {
			return nil, err
		}
		indexPath, err := archiveIndexPathOf(archiveRoot, entry)
		if err != nil {
			return nil, err
		}
		if err := promoteStagedArchiveFile(indexPath); err != nil {
			return nil, err
		}
	}
	if err := removeHotSegments(hotDir, segments); err != nil {
		return nil, err
	}
	return &stored, nil
}

// stageArchiveBody states one side file of the archive — the offset map of a
// segment — under the temporary name beside the final name a statement will carry,
// completed by a barrier, so the bytes stand before any statement names them.
func stageArchiveBody(target string, body []byte) error {
	temp := target + journalSegmentTempSuffix
	file, err := os.OpenFile(temp, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("tagent: open the archive side file %s: %w", temp, err)
	}
	discard := func(cause error) error {
		file.Close()
		os.Remove(temp)
		return cause
	}
	if _, err := file.Write(body); err != nil {
		return discard(fmt.Errorf("tagent: state the archive side file %s: %w", temp, err))
	}
	if err := file.Sync(); err != nil {
		return discard(fmt.Errorf("tagent: complete the barrier of %s: %w", temp, err))
	}
	if err := file.Close(); err != nil {
		os.Remove(temp)
		return fmt.Errorf("tagent: close the archive side file %s: %w", temp, err)
	}
	return nil
}

// removeHotSegments deletes the hot copies of the named segments and their
// indexes, each by name, so a repeated call removes nothing and reports nothing.
func removeHotSegments(hotDir string, segments []journalSealedSegment) error {
	for _, segment := range segments {
		if err := os.Remove(segment.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("tagent: remove the hot segment %s: %w", segment.path, err)
		}
		indexPath := journalSegmentIndexPathOf(segment)
		if err := os.Remove(indexPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("tagent: remove the hot index %s: %w", indexPath, err)
		}
	}
	return nil
}

// archiveFinishMode states what completing one stated move checks before it lets a
// name stand: the structure a boot reads, or the digests a move recovery confirms
// by reading the bytes.
type archiveFinishMode int

const (
	// archiveFinishStructure checks the stated names and byte counts and leaves an
	// index file it cannot complete to the read path that names it.
	archiveFinishStructure archiveFinishMode = iota
	// archiveFinishDigests checks every stated digest it reads and names an index
	// no copy can supply.
	archiveFinishDigests
)

// convergeArchiveStructure finishes the structural work a committed manifest
// states: it removes staged files no statement needs, completes the latest
// watermark generation from the staged or hot copies a stop left, and shrinks the
// hot files when their prefix still carries moved bytes. A settled boot stats only
// the latest generation's archive files; a stop that left hot bytes beside the
// statement verifies that generation's digests before those last copies are deleted.
//   - No manifest beside no named artifact is the empty archive: only the staged files no statement names are removed.
//   - A named segment or index file beside a lost or unstated name is ErrArchiveCorrupt, and the file is never removed.
//   - A stated segment whose staged copy, hot copy and named file are all absent is ErrArchiveMissing.
//   - A stated segment whose named or hot file carries another byte count or digest is ErrArchiveCorrupt.
//   - A stated index no copy can supply is left for the read path to name in a settled boot, because that boot pays no pass over the archived keys.
func convergeArchiveStructure(hotDir, archiveRoot string) error {
	manifest, found, err := readArchiveManifest(archiveRoot)
	if err != nil {
		return err
	}
	if !found {
		return reconcileArchiveSegmentDirectory(archiveRoot, nil, nil)
	}
	_, statedIDs, err := statedArchiveSegments(manifest)
	if err != nil {
		return err
	}
	named, err := statedArchiveFiles(archiveRoot, manifest)
	if err != nil {
		return err
	}
	latest := manifest.Segments
	window, statedWindow := archiveHotWindowOf(manifest)
	if statedWindow {
		latest = make([]archiveSegment, 0, len(manifest.Segments))
		for _, segment := range manifest.Segments {
			if segment.Watermark == window {
				latest = append(latest, segment)
			}
		}
	}
	if err := reconcileArchiveSegmentDirectory(archiveRoot, named, statedStagedFiles(archiveRoot, latest)); err != nil {
		return err
	}
	shrinkPending := false
	if len(statedIDs) > 0 {
		segments, err := openSealedSegments(hotDir)
		if err != nil {
			return err
		}
		newestStated := statedIDs[len(statedIDs)-1]
		for _, segment := range segments {
			if segment.id <= newestStated {
				continue
			}
			settled, err := hotLogPrefixStatesSegments(filepath.Join(hotDir, journalFileName), []journalSealedSegment{segment}, segment.bytes)
			if err != nil {
				return err
			}
			shrinkPending = !settled
			break
		}
	}
	for _, segment := range latest {
		mode := archiveFinishStructure
		if shrinkPending {
			mode = archiveFinishDigests
		} else {
			indexPath, err := archiveIndexPathOf(archiveRoot, segment)
			if err != nil {
				return err
			}
			hotPaths := []string{
				filepath.Join(hotDir, journalSegmentDirName, segment.SegmentID+journalSegmentNameSuffix),
				filepath.Join(hotDir, journalSegmentDirName, filepath.Base(indexPath)),
			}
			for _, path := range hotPaths {
				if _, err := os.Stat(path); err == nil {
					mode = archiveFinishDigests
					break
				} else if !errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("%w: stat %s: %w", ErrArchiveCorrupt, path, err)
				}
			}
		}
		if err := finishStatedArchiveMove(hotDir, archiveRoot, segment, mode); err != nil {
			return err
		}
	}
	return shrinkHotIndexAfterArchive(hotDir, manifest)
}

// recoverArchive finishes a move a stop interrupted: it reads the stored
// generation, removes the staged files no statement names, completes every stated
// segment and index from the staged or hot copy, verifies every stated segment's
// byte count and digest beside its index digest, and deletes the hot copies the
// statement covers. Running it over a consistent archive changes nothing.
//   - A named segment or index file beside a lost manifest is ErrArchiveCorrupt, and neither the file nor the statement is rewritten.
//   - A stated segment or index no copy can supply is ErrArchiveMissing.
//   - Stated bytes or an index that contradict their statement are ErrArchiveCorrupt.
func recoverArchive(hotDir, archiveRoot string) (*archiveManifest, error) {
	manifest, found, err := readArchiveManifest(archiveRoot)
	if err != nil {
		return nil, err
	}
	if !found {
		if err := reconcileArchiveSegmentDirectory(archiveRoot, nil, nil); err != nil {
			return nil, err
		}
		return &archiveManifest{Version: archiveManifestVersion}, nil
	}
	if _, err := archiveSegmentsOf(manifest); err != nil {
		return nil, err
	}
	named, err := statedArchiveFiles(archiveRoot, manifest)
	if err != nil {
		return nil, err
	}
	if err := reconcileArchiveSegmentDirectory(archiveRoot, named, statedStagedFiles(archiveRoot, manifest.Segments)); err != nil {
		return nil, err
	}
	for _, segment := range manifest.Segments {
		if err := finishStatedArchiveMove(hotDir, archiveRoot, segment, archiveFinishDigests); err != nil {
			return nil, err
		}
		id, held := journalSegmentIDOf(segment.SegmentID + journalSegmentNameSuffix)
		if !held {
			return nil, archiveCorruptOf("segment identity %q", segment.SegmentID)
		}
		hot := journalSealedSegment{id: id, path: filepath.Join(hotDir, journalSegmentDirName, segment.SegmentID+journalSegmentNameSuffix)}
		if err := removeHotSegments(hotDir, []journalSealedSegment{hot}); err != nil {
			return nil, err
		}
	}
	return manifest, nil
}

// statedArchiveFiles answers the final file names one generation states inside the
// archive segment directory: each segment's file and its index.
func statedArchiveFiles(archiveRoot string, manifest *archiveManifest) (map[string]bool, error) {
	named := make(map[string]bool, 2*len(manifest.Segments))
	for _, segment := range manifest.Segments {
		named[segment.SegmentID+journalSegmentNameSuffix] = true
		indexPath, err := archiveIndexPathOf(archiveRoot, segment)
		if err != nil {
			return nil, err
		}
		named[filepath.Base(indexPath)] = true
	}
	return named, nil
}

// statedStagedFiles answers the staged names a completion of the given segments
// may still need: the temporary file beside each segment and index.
func statedStagedFiles(archiveRoot string, segments []archiveSegment) map[string]bool {
	staged := make(map[string]bool, 2*len(segments))
	for _, segment := range segments {
		staged[archiveSegmentPathOf(archiveRoot, segment.SegmentID)+journalSegmentTempSuffix] = true
		if indexPath, err := archiveIndexPathOf(archiveRoot, segment); err == nil {
			staged[indexPath+journalSegmentTempSuffix] = true
		}
	}
	return staged
}

// reconcileArchiveSegmentDirectory compares one archive root's segment directory
// with the names one generation states: a staged file no stated name needs is
// removed, because a copy nobody stated carries no commit; and a named segment or
// index file no generation states is ErrArchiveCorrupt, because a name is taken
// only behind the generation that states it. A named file is never removed and a
// file of another kind is left alone, because the statement is the part that cannot
// be trusted and an unknown file states nothing either way.
func reconcileArchiveSegmentDirectory(archiveRoot string, named, keep map[string]bool) error {
	segDir := filepath.Join(archiveRoot, journalSegmentDirName)
	entries, err := os.ReadDir(segDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if stat, statErr := os.Stat(segDir); statErr == nil && !stat.IsDir() {
			return nil
		}
		return fmt.Errorf("tagent: read the archive segment directory %s: %w", segDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, journalSegmentTempSuffix) {
			if archiveNamedArtifact(name) && !named[name] {
				return archiveCorruptOf("%s holds the segment file %s beside no statement", archiveRoot, name)
			}
			continue
		}
		if !archiveNamedArtifact(strings.TrimSuffix(name, journalSegmentTempSuffix)) {
			continue
		}
		path := filepath.Join(segDir, name)
		if keep[path] {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("tagent: remove the staged archive file %s: %w", path, err)
		}
	}
	return nil
}

// finishStatedArchiveMove makes the segment and index files of one stated segment
// stand under the names the manifest states. A name already taken is checked, a name
// a stop left staged takes that name, and a name no copy can supply is
// ErrArchiveMissing.
//   - The digest mode checks every byte it promotes against the statement, so a completion never installs bytes the manifest contradicts.
//   - The structural mode places a name by its stat alone and leaves each digest to the read path that serves it, because a boot pays no pass over the archived keys.
//   - The hot copies are left in place, so a segment's only copy is never removed to complete its own move.
func finishStatedArchiveMove(hotDir, archiveRoot string, segment archiveSegment, mode archiveFinishMode) error {
	if err := finishStatedArchiveSegment(hotDir, archiveRoot, segment, mode); err != nil {
		return err
	}
	return finishStatedArchiveIndex(hotDir, archiveRoot, segment, mode)
}

// finishStatedArchiveSegment makes the file of one stated segment stand under its
// stated name, from the staged copy beside it or from the hot copy a stop left.
func finishStatedArchiveSegment(hotDir, archiveRoot string, segment archiveSegment, mode archiveFinishMode) error {
	target := archiveSegmentPathOf(archiveRoot, segment.SegmentID)
	matched, present, err := archiveFileMatches(target, segment.Bytes, segment.SHA256, mode)
	switch {
	case err != nil:
		return err
	case matched:
		return removeStagedArchiveFile(target)
	case present:
		return fmt.Errorf("%w: %s carries bytes beside the %d byte(s) %s the manifest states",
			ErrArchiveCorrupt, target, segment.Bytes, segment.SHA256)
	}
	staged := target + journalSegmentTempSuffix
	if matched, _, err := archiveFileMatches(staged, segment.Bytes, segment.SHA256, mode); err != nil {
		return err
	} else if matched {
		return promoteStagedArchiveFile(target)
	}
	hot := filepath.Join(hotDir, journalSegmentDirName, segment.SegmentID+journalSegmentNameSuffix)
	matched, present, err = archiveFileMatches(hot, segment.Bytes, segment.SHA256, mode)
	switch {
	case err != nil:
		return err
	case present && !matched:
		return fmt.Errorf("%w: the hot copy of %s carries bytes beside the %d byte(s) %s the manifest states",
			ErrArchiveCorrupt, segment.SegmentID, segment.Bytes, segment.SHA256)
	case !present:
		return fmt.Errorf("%w: %s", ErrArchiveMissing, segment.SegmentID)
	}
	if err := stageArchiveCopy(hot, target); err != nil {
		return err
	}
	if matched, _, err := archiveFileMatches(staged, segment.Bytes, segment.SHA256, mode); err != nil {
		return err
	} else if !matched {
		return fmt.Errorf("%w: the staged copy of %s carries bytes beside the %d byte(s) %s the manifest states",
			ErrArchiveCorrupt, segment.SegmentID, segment.Bytes, segment.SHA256)
	}
	return promoteStagedArchiveFile(target)
}

// finishStatedArchiveIndex makes the index file of one stated segment stand under
// the name the manifest states, from the staged copy beside it or from the hot
// index a stop left. An index no copy can supply is left for the read path to name
// in the structural mode and refused as ErrArchiveMissing in the digest mode.
func finishStatedArchiveIndex(hotDir, archiveRoot string, segment archiveSegment, mode archiveFinishMode) error {
	target, err := archiveIndexPathOf(archiveRoot, segment)
	if err != nil {
		return err
	}
	matched, present, _, err := archiveIndexMatches(target, segment.OffsetMap.IndexSHA256, mode)
	switch {
	case err != nil:
		return err
	case matched:
		return removeStagedArchiveFile(target)
	case present:
		return archiveCorruptOf("%s does not match the digest the manifest states", target)
	}
	staged := target + journalSegmentTempSuffix
	if matched, _, _, err := archiveIndexMatches(staged, segment.OffsetMap.IndexSHA256, mode); err != nil {
		return err
	} else if matched {
		return promoteStagedArchiveFile(target)
	}
	hot := filepath.Join(hotDir, journalSegmentDirName, filepath.Base(target))
	matched, present, body, err := archiveIndexMatches(hot, segment.OffsetMap.IndexSHA256, mode)
	switch {
	case err != nil:
		return err
	case present && !matched:
		return archiveCorruptOf("the hot index of %s does not match the digest the manifest states", segment.SegmentID)
	case !present:
		if mode == archiveFinishStructure {
			return nil
		}
		return fmt.Errorf("%w: the index %s", ErrArchiveMissing, segment.OffsetMap.IndexPath)
	}
	if body == nil {
		var err error
		body, err = os.ReadFile(hot)
		if err != nil {
			return fmt.Errorf("%w: read %s: %w", ErrArchiveCorrupt, hot, err)
		}
	}
	if err := stageArchiveBody(target, body); err != nil {
		return err
	}
	return promoteStagedArchiveFile(target)
}

// archiveFileMatches answers whether one file carries the bytes a statement names,
// whether it is present at all, and the fault of a file this build cannot stat. A
// present file of another byte count, or of another digest in the digest mode, is
// present and not matching; the caller decides whether to complete or refuse.
func archiveFileMatches(path string, bytes int64, sha256 string, mode archiveFinishMode) (matched, present bool, err error) {
	stat, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("%w: stat %s: %w", ErrArchiveCorrupt, path, err)
	}
	if stat.Size() != bytes {
		return false, true, nil
	}
	if mode != archiveFinishDigests {
		return true, true, nil
	}
	digest, size, err := digestOfFile(path)
	if err != nil {
		return false, true, fmt.Errorf("%w: read %s: %w", ErrArchiveCorrupt, path, err)
	}
	if size != bytes || digest != sha256 {
		return false, true, nil
	}
	return true, true, nil
}

// archiveIndexMatches answers whether one index file matches the digest a statement
// names, whether it is present at all, and the bytes of the file when it read them.
// The structural mode stats the file alone and places any present index, because a
// boot pays no pass over the archived keys; the read path verifies the digest it
// serves.
func archiveIndexMatches(path, sha256 string, mode archiveFinishMode) (matched, present bool, body []byte, err error) {
	if mode != archiveFinishDigests {
		stat, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			return false, false, nil, nil
		}
		if err != nil {
			return false, false, nil, fmt.Errorf("%w: stat %s: %w", ErrArchiveCorrupt, path, err)
		}
		return stat.Mode().IsRegular(), stat.Mode().IsRegular(), nil, nil
	}
	body, err = os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil, nil
	}
	if err != nil {
		return false, false, nil, fmt.Errorf("%w: read %s: %w", ErrArchiveCorrupt, path, err)
	}
	return digestOf(body) == sha256, true, body, nil
}
