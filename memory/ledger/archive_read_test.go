// The read path of one journal view over its archive: a key the hot index does not
// state is located through the archive manifest, its segment index and its segment
// bytes, and the three failures of that read stay named.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// archivedViewFixture builds one hot root with the given sealed intervals, moves the
// oldest of them into a fresh archive root, shrinks the hot files to the intervals
// that remain, and answers the hot directory, the archive root, the intervals that
// left and the intervals the hot root keeps.
func archivedViewFixture(t *testing.T, seals, moved int) (string, string, []journalSealedSegment, []journalSealedSegment) {
	t.Helper()
	hotDir, segments := hotShrinkFixture(t, seals)
	if moved < 1 || moved >= len(segments) {
		t.Fatalf("the check moves %d of %d interval(s), want at least one on both sides of the move", moved, len(segments))
	}
	leaving, retained := segments[:moved], segments[moved:]
	archiveRoot := t.TempDir()
	window := hotWindowAfterMoveForTest(t, hotDir, leaving, retained)
	manifest, err := moveSealedSegmentsToArchive(hotDir, archiveRoot, leaving, window)
	if err != nil {
		t.Fatalf("move the sealed intervals into the archive: %v", err)
	}
	if err := shrinkHotIndexAfterArchive(hotDir, manifest); err != nil {
		t.Fatalf("shrink the hot files after the move: %v", err)
	}
	return hotDir, archiveRoot, leaving, retained
}

// archivedKeysForTest answers the keys the given intervals carry.
func archivedKeysForTest(segments []journalSealedSegment) []string {
	keys := make([]string, 0, len(segments)*3)
	for _, segment := range segments {
		keys = append(keys, hotSegmentKeysForTest(int(segment.id))...)
	}
	return keys
}

// TestIdentityReadCrossesHotAndArchive pins one read face over moved and hot intervals.
//   - An archived key reads back the bytes its commit wrote.
//   - A retained hot key reads back the same bytes in the same view.
//   - The key listing states both tiers instead of only one.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestIdentityReadCrossesHotAndArchive(t *testing.T) {
	ctx := context.Background()
	hotDir, archiveRoot, leaving, retained := archivedViewFixture(t, 3, 2)
	kv, err := openJournalViewWithArchive(hotDir, false, 0, archiveRoot, 0)
	if err != nil {
		t.Fatalf("open the journal view beside the archive: %v", err)
	}
	t.Cleanup(func() { kv.Close() })
	for _, key := range archivedKeysForTest(leaving) {
		pairs, err := kv.Read(ctx, Fetch{Key: key})
		if err != nil {
			t.Fatalf("read the archived key %q: %v", key, err)
		}
		if len(pairs) != 1 || !bytes.Equal(pairs[0].Value, hotValueForTest(key)) {
			t.Fatalf("the archived read of %q = %+v, want the bytes the commit wrote", key, pairs)
		}
	}
	hotKeys := append(archivedKeysForTest(retained), "open-a", "open-b")
	for _, key := range hotKeys {
		pairs, err := kv.Read(ctx, Fetch{Key: key})
		if err != nil {
			t.Fatalf("read the hot key %q beside the archive: %v", key, err)
		}
		if len(pairs) != 1 || !bytes.Equal(pairs[0].Value, hotValueForTest(key)) {
			t.Fatalf("the hot read of %q = %+v, want the bytes the commit wrote", key, pairs)
		}
	}
	held, err := kv.Keys(ctx, "")
	if err != nil {
		t.Fatalf("read the keys of the shrunk view: %v", err)
	}
	if want := append(archivedKeysForTest(leaving), hotKeys...); !equalStringSets(held, want) {
		t.Fatalf("the key listing states %v, want the %v both tiers carry", held, want)
	}
}

// TestArchivedRootWithoutDeclarationDoesNotServeArchivedKeys pins the unsupported rollback form.
//   - an archived root opened without an archive declaration still serves retained hot keys
//   - archived keys are not served or reported as a supported zero-migration rollback
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestArchivedRootWithoutDeclarationDoesNotServeArchivedKeys(t *testing.T) {
	ctx := context.Background()
	hotDir, _, leaving, retained := archivedViewFixture(t, 2, 1)
	view, err := openJournalView(hotDir, true)
	if err != nil {
		t.Fatalf("open the archived root without an archive declaration: %v", err)
	}
	t.Cleanup(func() { view.Close() })
	for _, key := range archivedKeysForTest(leaving) {
		pairs, err := view.Read(ctx, Fetch{Key: key})
		if err != nil || len(pairs) != 0 {
			t.Fatalf("the undeclared archive read of %q = (%+v, %v), want no held value", key, pairs, err)
		}
	}
	for _, hotKey := range hotSegmentKeysForTest(int(retained[0].id)) {
		pairs, err := view.Read(ctx, Fetch{Key: hotKey})
		if err != nil || len(pairs) != 1 || !bytes.Equal(pairs[0].Value, hotValueForTest(hotKey)) {
			t.Fatalf("the retained hot read of %q = (%+v, %v), want its committed bytes", hotKey, pairs, err)
		}
	}
}

// TestArchiveMissingIsNamed pins a segment the manifest states and the archive does not hold.
//   - a key the manifest states beside a segment file the archive lost is ErrArchiveMissing, never held and never ErrOriginalMissing
//   - a key no statement carries stays ErrOriginalMissing, so the two states stay apart
//   - a key of an interval the hot root keeps still reads while the archive segment is gone
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestArchiveMissingIsNamed(t *testing.T) {
	ctx := context.Background()
	hotDir, archiveRoot, leaving, retained := archivedViewFixture(t, 2, 1)
	lost := journalSegmentNameOf(leaving[0].id)
	if err := os.Remove(filepath.Join(archiveRoot, journalSegmentDirName, lost)); err != nil {
		t.Fatalf("remove the archived segment %s: %v", lost, err)
	}
	kv, err := openJournalViewWithArchive(hotDir, false, 0, archiveRoot, 0)
	if err != nil {
		t.Fatalf("open the journal view beside the archive: %v", err)
	}
	t.Cleanup(func() { kv.Close() })
	key := hotSegmentKeysForTest(int(leaving[0].id))[0]
	pairs, err := kv.Read(ctx, Fetch{Key: key})
	if !errors.Is(err, ErrArchiveMissing) {
		t.Fatalf("the read of the stated key %q = (%+v, %v), want ErrArchiveMissing", key, pairs, err)
	}
	if errors.Is(err, ErrOriginalMissing) || len(pairs) != 0 {
		t.Fatalf("the read of the stated key %q = (%+v, %v), want the missing segment named and no pair", key, pairs, err)
	}
	if _, _, err := kv.archive.read("zzzz-no-statement-carries-this-key"); !errors.Is(err, ErrOriginalMissing) {
		t.Fatalf("the read of an unstated key = %v, want ErrOriginalMissing", err)
	}
	hotKey := hotSegmentKeysForTest(int(retained[0].id))[0]
	pairs, err = kv.Read(ctx, Fetch{Key: hotKey})
	if err != nil || len(pairs) != 1 || !bytes.Equal(pairs[0].Value, hotValueForTest(hotKey)) {
		t.Fatalf("the read of the hot key %q = (%+v, %v), want the bytes the commit wrote", hotKey, pairs, err)
	}
}

// TestArchiveCorruptIsNamed pins bytes that do not match their statement as a named state.
//   - a segment whose bytes differ from the digest the manifest states is ErrArchiveCorrupt
//   - an index whose bytes differ from the digest the manifest states is ErrArchiveCorrupt
//   - a manifest no generation decodes is ErrArchiveCorrupt
//   - no case is answered as a missing segment or an unstated key
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestArchiveCorruptIsNamed(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		damage func(t *testing.T, archiveRoot string, leaving []journalSealedSegment)
	}{
		{name: "segment bytes", damage: func(t *testing.T, archiveRoot string, leaving []journalSealedSegment) {
			path := filepath.Join(archiveRoot, journalSegmentDirName, journalSegmentNameOf(leaving[0].id))
			flipByteAt(t, path, fileSizeForTest(t, path)/2)
		}},
		{name: "index bytes", damage: func(t *testing.T, archiveRoot string, leaving []journalSealedSegment) {
			path := filepath.Join(archiveRoot, journalSegmentDirName, journalSegmentLabelOf(leaving[0].id)+journalSegmentIndexSuffix)
			flipByteAt(t, path, journalFrameHeaderBytes+8)
		}},
		{name: "manifest bytes", damage: func(t *testing.T, archiveRoot string, _ []journalSealedSegment) {
			for _, name := range []string{archiveManifestAName, archiveManifestBName} {
				path := filepath.Join(archiveRoot, name)
				if err := os.WriteFile(path, []byte("no manifest generation"), 0o600); err != nil {
					t.Fatalf("damage the manifest slot %s: %v", name, err)
				}
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hotDir, archiveRoot, leaving, _ := archivedViewFixture(t, 2, 1)
			tc.damage(t, archiveRoot, leaving)
			kv, err := openJournalViewWithArchive(hotDir, false, 0, archiveRoot, 0)
			if err != nil {
				t.Fatalf("open the journal view beside the archive: %v", err)
			}
			t.Cleanup(func() { kv.Close() })
			key := hotSegmentKeysForTest(int(leaving[0].id))[0]
			pairs, err := kv.Read(ctx, Fetch{Key: key})
			if !errors.Is(err, ErrArchiveCorrupt) {
				t.Fatalf("the read of the stated key %q = (%+v, %v), want ErrArchiveCorrupt", key, pairs, err)
			}
			if errors.Is(err, ErrOriginalMissing) || errors.Is(err, ErrArchiveMissing) || len(pairs) != 0 {
				t.Fatalf("the read of the stated key %q = (%+v, %v), want the corruption named alone", key, pairs, err)
			}
		})
	}
}

// TestArchiveIndexKeyDigestMismatchIsCorrupt pins the manifest's sorted key-set digest against the decoded index.
//   - changing an interior index key while restating the index digest and frame count leaves only the key-domain digest contradicted
//   - the point read answers ErrArchiveCorrupt rather than an unstated key
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestArchiveIndexKeyDigestMismatchIsCorrupt(t *testing.T) {
	ctx := context.Background()
	hotDir, archiveRoot, leaving, _ := archivedViewFixture(t, 2, 1)
	manifest, stated, err := readArchiveManifest(archiveRoot)
	if err != nil || !stated {
		t.Fatalf("read the archive manifest: stated=%t err=%v", stated, err)
	}
	segmentID := journalSegmentLabelOf(leaving[0].id)
	at := -1
	for i := range manifest.Segments {
		if manifest.Segments[i].SegmentID == segmentID {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("the manifest states no segment %q", segmentID)
	}
	segment := manifest.Segments[at]
	indexPath := filepath.Join(archiveRoot, journalSegmentDirName, segment.OffsetMap.IndexPath)
	indexFrame, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read the archived index: %v", err)
	}
	payload, err := journalPayloadOf(indexFrame)
	if err != nil {
		t.Fatalf("decode the archived index frame: %v", err)
	}
	var index journalSegmentIndex
	if err := json.Unmarshal(payload, &index); err != nil {
		t.Fatalf("decode the archived index: %v", err)
	}
	key := hotSegmentKeysForTest(int(leaving[0].id))[1]
	changed := false
	for i := range index.Entries {
		if index.Entries[i].Key == key {
			index.Entries[i].Key = key + "-changed"
			changed = true
			break
		}
	}
	if !changed {
		t.Fatalf("the archived index states no interior key %q", key)
	}
	body, err := json.Marshal(index)
	if err != nil {
		t.Fatalf("encode the changed index: %v", err)
	}
	indexFrame = journalFrameOf(body)
	if err := os.WriteFile(indexPath, indexFrame, 0o600); err != nil {
		t.Fatalf("write the changed index: %v", err)
	}
	segment.OffsetMap.IndexSHA256 = digestOf(indexFrame)
	segment.OffsetMap.FrameCount = len(index.Entries)
	manifest.Segments[at] = segment
	if err := writeArchiveManifest(archiveRoot, *manifest); err != nil {
		t.Fatalf("restate the changed index in the manifest: %v", err)
	}
	view, err := openJournalViewWithArchive(hotDir, false, 0, archiveRoot, 0)
	if err != nil {
		t.Fatalf("open the journal view beside the changed index: %v", err)
	}
	t.Cleanup(func() { view.Close() })
	value, held, err := (TierFacts{kv: view}).Get(ctx, key)
	if !errors.Is(err, ErrArchiveCorrupt) || held || value != nil {
		t.Fatalf("the read with a contradicted key digest = (%q, %t, %v), want (nil, false, ErrArchiveCorrupt)", value, held, err)
	}
}

// TestArchiveKeyDomainDigestSeparatesKeys pins the key-set digest to the canonical JSON of the sorted keys.
//   - the sets {a\nb, c} and {a, b\nc} sort to the same joined text and MUST state different digests
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestArchiveKeyDomainDigestSeparatesKeys(t *testing.T) {
	entriesOf := func(keys ...string) map[string]journalSegmentIndexEntry {
		entries := make(map[string]journalSegmentIndexEntry, len(keys))
		for _, key := range keys {
			entries[key] = journalSegmentIndexEntry{Key: key}
		}
		return entries
	}
	left := archiveKeyDomainOf(1, entriesOf("a\nb", "c"))
	right := archiveKeyDomainOf(1, entriesOf("a", "b\nc"))
	if left.KeysSHA == right.KeysSHA {
		t.Fatalf("the key sets {a\\nb, c} and {a, b\\nc} share the digest %q, want one digest per key set", left.KeysSHA)
	}
	if left.KeysSHA == "" || right.KeysSHA == "" {
		t.Fatalf("the key domains state the digests %q and %q, want one per key set", left.KeysSHA, right.KeysSHA)
	}
}

// TestArchiveManifestContradictsItsSegmentIsCorrupt pins the manifest statements a segment read checks against the file it describes.
//   - a contradicted commit rank, cut identity or cut frame offset is ErrArchiveCorrupt
//   - point reads and key listings refuse the segment by name instead of serving its keys
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestArchiveManifestContradictsItsSegmentIsCorrupt(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		damage func(segment *archiveSegment)
	}{
		{name: "first_seq", damage: func(segment *archiveSegment) {
			segment.OffsetMap.FirstSeq++
			if segment.OffsetMap.FirstSeq > segment.OffsetMap.LastSeq {
				segment.OffsetMap.LastSeq = segment.OffsetMap.FirstSeq
			}
		}},
		{name: "last_seq", damage: func(segment *archiveSegment) { segment.OffsetMap.LastSeq++ }},
		{name: "cut_partition", damage: func(segment *archiveSegment) { segment.Cut.Partition++ }},
		{name: "cut_seq", damage: func(segment *archiveSegment) { segment.Cut.Seq++ }},
		{name: "cut_frame_offset", damage: func(segment *archiveSegment) { segment.Cut.FrameOffset++ }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hotDir, archiveRoot, leaving, _ := archivedViewFixture(t, 2, 1)
			manifest, stated, err := readArchiveManifest(archiveRoot)
			if err != nil || !stated || len(manifest.Segments) != 1 {
				t.Fatalf("read the archive manifest = stated=%t segments=%+v err=%v", stated, manifest, err)
			}
			tc.damage(&manifest.Segments[0])
			if err := writeArchiveManifest(archiveRoot, *manifest); err != nil {
				t.Fatalf("restate the changed segment: %v", err)
			}
			view, err := openJournalViewWithArchive(hotDir, false, 0, archiveRoot, 0)
			if err != nil {
				t.Fatalf("open the view beside the changed %s: %v", tc.name, err)
			}
			t.Cleanup(func() { view.Close() })
			key := hotSegmentKeysForTest(int(leaving[0].id))[0]
			value, held, err := (TierFacts{kv: view}).Get(ctx, key)
			if !errors.Is(err, ErrArchiveCorrupt) || held || value != nil {
				t.Fatalf("the read with the changed %s = (%q, %t, %v), want ErrArchiveCorrupt", tc.name, value, held, err)
			}
			if errors.Is(err, ErrArchiveMissing) || errors.Is(err, ErrOriginalMissing) {
				t.Fatalf("the read with the changed %s = %v, want the corruption named alone", tc.name, err)
			}
			if _, err := view.Keys(ctx, ""); !errors.Is(err, ErrArchiveCorrupt) {
				t.Fatalf("the listing with the changed %s = %v, want ErrArchiveCorrupt", tc.name, err)
			}
		})
	}
}

// TestArchiveKeyDomainMismatchIsCorrupt pins every manifest key-domain field.
//   - a changed partition or conservative interval is corruption, not a narrower archive
//   - point reads and key listings refuse the contradicted segment by name
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestArchiveKeyDomainMismatchIsCorrupt(t *testing.T) {
	ctx := context.Background()
	for _, field := range []string{"partition", "first_key", "last_key"} {
		t.Run(field, func(t *testing.T) {
			hotDir, archiveRoot, leaving, _ := archivedViewFixture(t, 2, 1)
			manifest, stated, err := readArchiveManifest(archiveRoot)
			if err != nil || !stated || len(manifest.Segments) != 1 {
				t.Fatalf("read the archive manifest = stated=%t segments=%+v err=%v", stated, manifest, err)
			}
			switch field {
			case "partition":
				manifest.Segments[0].KeyDomain.Partition++
			case "first_key":
				manifest.Segments[0].KeyDomain.FirstKey = "!"
			case "last_key":
				manifest.Segments[0].KeyDomain.LastKey = "zzzz"
			}
			if err := writeArchiveManifest(archiveRoot, *manifest); err != nil {
				t.Fatalf("restate the changed key domain: %v", err)
			}
			view, err := openJournalViewWithArchive(hotDir, false, 0, archiveRoot, 0)
			if err != nil {
				t.Fatalf("open the view beside the changed key domain: %v", err)
			}
			t.Cleanup(func() { view.Close() })
			key := hotSegmentKeysForTest(int(leaving[0].id))[0]
			value, held, err := (TierFacts{kv: view}).Get(ctx, key)
			if !errors.Is(err, ErrArchiveCorrupt) || held || value != nil {
				t.Fatalf("the read of the changed %s = (%q, %t, %v), want ErrArchiveCorrupt", field, value, held, err)
			}
			if _, err := view.Keys(ctx, ""); !errors.Is(err, ErrArchiveCorrupt) {
				t.Fatalf("the listing of the changed %s = %v, want ErrArchiveCorrupt", field, err)
			}
		})
	}
}

// TestArchivedReadAnswersTheNewestStatement pins a key that several archived intervals state.
//   - the read answers the newest archived statement of the key, not the bytes a moved interval behind it wrote
//   - the hot root states the key in no interval, so the answer was located through the archive
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestArchivedReadAnswersTheNewestStatement(t *testing.T) {
	ctx := context.Background()
	hotDir := t.TempDir()
	kv, err := openJournalViewFace(hotDir, true, segmentTestBudget)
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	commit := func(key, value string) {
		t.Helper()
		if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: key, Value: []byte(value)}}}); err != nil {
			t.Fatalf("commit %q: %v", key, err)
		}
	}
	commit("rewritten", "first")
	commit("checkpoint:1:1", "cut")
	commit("rewritten", "second")
	commit("checkpoint:1:2", "cut")
	commit("other", "kept")
	commit("checkpoint:1:3", "cut")
	if err := kv.log.close(); err != nil {
		t.Fatalf("close the journal view: %v", err)
	}
	segments := readSealedSegmentsForTest(t, hotDir)
	if len(segments) != 3 {
		t.Fatalf("the fixture sealed %d interval(s), want the three its cuts closed", len(segments))
	}
	leaving, retained := segments[:2], segments[2:]
	archiveRoot := t.TempDir()
	window := hotWindowAfterMoveForTest(t, hotDir, leaving, retained)
	manifest, err := moveSealedSegmentsToArchive(hotDir, archiveRoot, leaving, window)
	if err != nil {
		t.Fatalf("move the sealed intervals into the archive: %v", err)
	}
	if err := shrinkHotIndexAfterArchive(hotDir, manifest); err != nil {
		t.Fatalf("shrink the hot files after the move: %v", err)
	}
	view, err := openJournalViewWithArchive(hotDir, false, 0, archiveRoot, 0)
	if err != nil {
		t.Fatalf("open the journal view beside the archive: %v", err)
	}
	t.Cleanup(func() { view.Close() })
	pairs, err := view.Read(ctx, Fetch{Key: "rewritten"})
	if err != nil {
		t.Fatalf("read the rewritten key: %v", err)
	}
	if len(pairs) != 1 || !bytes.Equal(pairs[0].Value, []byte("second")) {
		t.Fatalf("the archived read of the rewritten key = %+v, want the newest archived statement", pairs)
	}
}

// TestDeletedKeyDoesNotResurrectFromArchive pins explicit deletion across hot and archived intervals.
//   - a hot tombstone blocks an older archived value, and a later put replaces the tombstone
//   - a newer archived tombstone blocks an older archived value
//   - both deleted reads answer the public Get contract as not held without an error
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestDeletedKeyDoesNotResurrectFromArchive(t *testing.T) {
	ctx := context.Background()
	t.Run("hot tombstone", func(t *testing.T) {
		hotDir, archiveRoot, leaving, _ := archivedViewFixture(t, 2, 1)
		key := hotSegmentKeysForTest(int(leaving[0].id))[0]
		view, err := openJournalViewWithArchive(hotDir, true, segmentTestBudget, archiveRoot, 0)
		if err != nil {
			t.Fatalf("open the journal view beside the archive: %v", err)
		}
		t.Cleanup(func() { view.Close() })
		if err := view.Commit(ctx, Batch{Deletes: []string{key}}); err != nil {
			t.Fatalf("delete the archived key from the hot log: %v", err)
		}
		facts := TierFacts{kv: view}
		value, held, err := facts.Get(ctx, key)
		if err != nil || held || value != nil {
			t.Fatalf("the hot-deleted read = (%q, %t, %v), want (nil, false, nil)", value, held, err)
		}
		if err := view.Commit(ctx, Batch{Puts: []Pair{{Key: key, Value: []byte("replacement")}}}); err != nil {
			t.Fatalf("put a replacement over the hot tombstone: %v", err)
		}
		value, held, err = facts.Get(ctx, key)
		if err != nil || !held || !bytes.Equal(value, []byte("replacement")) {
			t.Fatalf("the replacement read = (%q, %t, %v), want (replacement, true, nil)", value, held, err)
		}
	})
	t.Run("archive tombstone", func(t *testing.T) {
		hotDir := t.TempDir()
		kv, err := openJournalViewFace(hotDir, true, segmentTestBudget)
		if err != nil {
			t.Fatalf("open the journal view: %v", err)
		}
		if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "deleted", Value: []byte("old")}}}); err != nil {
			t.Fatalf("commit the old value: %v", err)
		}
		if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "checkpoint:1:1", Value: []byte("cut-one")}}}); err != nil {
			t.Fatalf("commit the first cut: %v", err)
		}
		if err := kv.Commit(ctx, Batch{Deletes: []string{"deleted"}}); err != nil {
			t.Fatalf("commit the delete: %v", err)
		}
		if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "checkpoint:1:2", Value: []byte("cut-two")}}}); err != nil {
			t.Fatalf("commit the second cut: %v", err)
		}
		if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "retained", Value: []byte("value")}}}); err != nil {
			t.Fatalf("commit the retained value: %v", err)
		}
		if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "checkpoint:1:3", Value: []byte("cut-three")}}}); err != nil {
			t.Fatalf("commit the third cut: %v", err)
		}
		if err := kv.log.close(); err != nil {
			t.Fatalf("close the journal view: %v", err)
		}
		segments := readSealedSegmentsForTest(t, hotDir)
		if len(segments) != 3 {
			t.Fatalf("the fixture sealed %d segment(s), want three", len(segments))
		}
		leaving, retained := segments[:2], segments[2:]
		archiveRoot := t.TempDir()
		window := hotWindowAfterMoveForTest(t, hotDir, leaving, retained)
		manifest, err := moveSealedSegmentsToArchive(hotDir, archiveRoot, leaving, window)
		if err != nil {
			t.Fatalf("move the old and deleting segments into the archive: %v", err)
		}
		if err := shrinkHotIndexAfterArchive(hotDir, manifest); err != nil {
			t.Fatalf("shrink the hot files after the move: %v", err)
		}
		view, err := openJournalViewWithArchive(hotDir, false, 0, archiveRoot, 0)
		if err != nil {
			t.Fatalf("open the archive read view: %v", err)
		}
		t.Cleanup(func() { view.Close() })
		facts := TierFacts{kv: view}
		value, held, err := facts.Get(ctx, "deleted")
		if err != nil || held || value != nil {
			t.Fatalf("the archive-deleted read = (%q, %t, %v), want (nil, false, nil)", value, held, err)
		}
	})
}

// TestPrefixReadCrossesHotAndArchive pins archived and hot keys answering together in key order.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestPrefixReadCrossesHotAndArchive(t *testing.T) {
	hotDir, archiveRoot, leaving, retained := archivedViewFixture(t, 3, 2)
	view, err := openJournalViewWithArchive(hotDir, false, 0, archiveRoot, 0)
	if err != nil {
		t.Fatalf("open the journal view beside the archive: %v", err)
	}
	t.Cleanup(func() { view.Close() })
	pairs, err := view.Read(context.Background(), Fetch{Prefix: "seg-"})
	if err != nil {
		t.Fatalf("read the event-key prefix across hot and archive: %v", err)
	}
	segments := append(append([]journalSealedSegment{}, leaving...), retained...)
	want := make([]Pair, 0, 6)
	for _, segment := range segments {
		for _, key := range hotSegmentKeysForTest(int(segment.id)) {
			if len(key) >= 4 && key[:4] == "seg-" {
				want = append(want, Pair{Key: key, Value: hotValueForTest(key)})
			}
		}
	}
	if len(pairs) != len(want) {
		t.Fatalf("the cross-archive prefix read = %+v, want %+v", pairs, want)
	}
	for at := range want {
		if pairs[at].Key != want[at].Key || !bytes.Equal(pairs[at].Value, want[at].Value) {
			t.Fatalf("the cross-archive prefix pair %d = %+v, want %+v", at, pairs[at], want[at])
		}
	}
}

// TestArchivedWalkUsesLastWriteOrderWithinOneFrame pins repeated writes in one commit.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestArchivedWalkUsesLastWriteOrderWithinOneFrame(t *testing.T) {
	hotDir, archiveRoot := t.TempDir(), t.TempDir()
	kv, err := openJournalViewWithArchive(hotDir, true, segmentTestBudget, archiveRoot, 0)
	if err != nil {
		t.Fatalf("open the archive-backed journal view: %v", err)
	}
	t.Cleanup(func() { kv.Close() })
	commit := func(batch Batch) {
		t.Helper()
		if err := kv.Commit(context.Background(), batch); err != nil {
			t.Fatalf("commit the test interval: %v", err)
		}
	}
	commit(Batch{Puts: []Pair{
		{Key: "a", Value: []byte("a-first")},
		{Key: "b", Value: []byte("b-first")},
		{Key: "c", Value: []byte("c")},
		{Key: "b", Value: []byte("b-last")},
	}})
	commit(Batch{Puts: []Pair{{Key: "checkpoint:1:1", Value: []byte("cut-one")}}})
	commit(Batch{Puts: []Pair{{Key: "hot", Value: []byte("hot")}}})
	commit(Batch{Puts: []Pair{{Key: "checkpoint:1:2", Value: []byte("cut-two")}}})
	kv.mu.Lock()
	err = kv.maintainAfterCutLocked(time.Now())
	kv.mu.Unlock()
	if err != nil {
		t.Fatalf("move the oldest interval into the archive: %v", err)
	}
	var order []string
	values := map[string]string{}
	if err := kv.EachCommittedNewestFirst(context.Background(), func(key string, value []byte) (bool, error) {
		if key == "a" || key == "b" || key == "c" {
			order = append(order, key)
			values[key] = string(value)
		}
		return true, nil
	}); err != nil {
		t.Fatalf("walk the archived repeated writes: %v", err)
	}
	if got := strings.Join(order, ","); got != "b,c,a" {
		t.Fatalf("the archived repeated-write order = %s, want b,c,a", got)
	}
	if values["b"] != "b-last" {
		t.Fatalf("the archived repeated write answered %q, want b-last", values["b"])
	}
}

// TestArchiveVerificationFollowsStatSignature pins digest revalidation after a segment stat change.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestArchiveVerificationFollowsStatSignature(t *testing.T) {
	hotDir, archiveRoot, leaving, _ := archivedViewFixture(t, 2, 1)
	view, err := openJournalViewWithArchive(hotDir, false, 0, archiveRoot, 0)
	if err != nil {
		t.Fatalf("open the journal view beside the archive: %v", err)
	}
	t.Cleanup(func() { view.Close() })
	key := hotSegmentKeysForTest(int(leaving[0].id))[0]
	if value, held := readOne(t, view, key); !held || !bytes.Equal(value, hotValueForTest(key)) {
		t.Fatalf("the warm archive read = (%q, %t), want the committed bytes", value, held)
	}
	segmentID := journalSegmentLabelOf(leaving[0].id)
	view.mu.Lock()
	before := view.archive.verified[segmentID]
	view.mu.Unlock()
	path := filepath.Join(archiveRoot, journalSegmentDirName, journalSegmentNameOf(leaving[0].id))
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the archived segment: %v", err)
	}
	changedAt := stat.ModTime().Add(2 * time.Second)
	if err := os.Chtimes(path, changedAt, changedAt); err != nil {
		t.Fatalf("change only the archived segment timestamp: %v", err)
	}
	if value, held := readOne(t, view, key); !held || !bytes.Equal(value, hotValueForTest(key)) {
		t.Fatalf("the read after the timestamp change = (%q, %t), want the committed bytes", value, held)
	}
	view.mu.Lock()
	after := view.archive.verified[segmentID]
	view.mu.Unlock()
	if after == before || after.modTimeNano != changedAt.UnixNano() {
		t.Fatalf("the verified signature = %+v, want the new timestamp %d beside %+v", after, changedAt.UnixNano(), before)
	}
	flipByteAt(t, path, stat.Size()/2)
	changedAt = changedAt.Add(2 * time.Second)
	if err := os.Chtimes(path, changedAt, changedAt); err != nil {
		t.Fatalf("change the damaged segment timestamp: %v", err)
	}
	pairs, err := view.Read(context.Background(), Fetch{Key: key})
	if !errors.Is(err, ErrArchiveCorrupt) || len(pairs) != 0 {
		t.Fatalf("the read after the byte change = (%+v, %v), want ErrArchiveCorrupt", pairs, err)
	}
}

// TestArchiveVerificationFollowsManifestDigest pins a restated digest beside unchanged bytes.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestArchiveVerificationFollowsManifestDigest(t *testing.T) {
	hotDir, archiveRoot, leaving, _ := archivedViewFixture(t, 2, 1)
	view, err := openJournalViewWithArchive(hotDir, false, 0, archiveRoot, 0)
	if err != nil {
		t.Fatalf("open the journal view beside the archive: %v", err)
	}
	t.Cleanup(func() { view.Close() })
	key := hotSegmentKeysForTest(int(leaving[0].id))[0]
	if value, held := readOne(t, view, key); !held || !bytes.Equal(value, hotValueForTest(key)) {
		t.Fatalf("the warm archive read = (%q, %t), want the committed bytes", value, held)
	}
	manifest, stated, err := readArchiveManifest(archiveRoot)
	if err != nil || !stated {
		t.Fatalf("read the archive manifest = stated=%t err=%v", stated, err)
	}
	manifest.Segments[0].SHA256 = "sha256:" + strings.Repeat("0", 64)
	if err := writeArchiveManifest(archiveRoot, *manifest); err != nil {
		t.Fatalf("restate the archive digest: %v", err)
	}
	pairs, err := view.Read(context.Background(), Fetch{Key: key})
	if !errors.Is(err, ErrArchiveCorrupt) || len(pairs) != 0 {
		t.Fatalf("the read after the digest restatement = (%+v, %v), want ErrArchiveCorrupt", pairs, err)
	}
}

// flipByteAt inverts one byte of the file at path, which leaves the bytes readable and
// every digest over them different from the one a statement recorded.
func flipByteAt(t *testing.T, path string, at int64) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open %s to flip a byte: %v", path, err)
	}
	defer file.Close()
	one := make([]byte, 1)
	if _, err := file.ReadAt(one, at); err != nil {
		t.Fatalf("read the byte %d of %s: %v", at, path, err)
	}
	one[0] ^= 0xff
	if _, err := file.WriteAt(one, at); err != nil {
		t.Fatalf("write the byte %d of %s: %v", at, path, err)
	}
}

// TestKeyListingReadsArchiveIndexesNotValues pins what a key listing pays for.
//   - The listing states archived keys while their record bytes fail checksums.
//   - A point read still names that corruption, proving the listing read no value.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestKeyListingReadsArchiveIndexesNotValues(t *testing.T) {
	ctx := context.Background()
	hotDir, archiveRoot, leaving, retained := archivedViewFixture(t, 2, 1)
	segmentID := journalSegmentLabelOf(leaving[0].id)
	segmentPath := filepath.Join(archiveRoot, journalSegmentDirName, journalSegmentNameOf(leaving[0].id))
	flipByteAt(t, segmentPath, fileSizeForTest(t, segmentPath)-1)
	manifest, stated, err := readArchiveManifest(archiveRoot)
	if err != nil || !stated {
		t.Fatalf("read the archive manifest = stated=%t err=%v", stated, err)
	}
	restated := false
	for at := range manifest.Segments {
		if manifest.Segments[at].SegmentID != segmentID {
			continue
		}
		digest, _, err := digestOfFile(segmentPath)
		if err != nil {
			t.Fatalf("digest the damaged segment: %v", err)
		}
		manifest.Segments[at].SHA256 = digest
		restated = true
	}
	if !restated {
		t.Fatalf("the manifest states no segment %q", segmentID)
	}
	if err := writeArchiveManifest(archiveRoot, *manifest); err != nil {
		t.Fatalf("restate the damaged segment in the manifest: %v", err)
	}
	view, err := openJournalViewWithArchive(hotDir, false, 0, archiveRoot, 0)
	if err != nil {
		t.Fatalf("open the journal view beside the archive: %v", err)
	}
	t.Cleanup(func() { view.Close() })
	keys, err := view.Keys(ctx, "")
	if err != nil {
		t.Fatalf("list the keys of the damaged interval: %v", err)
	}
	want := append(archivedKeysForTest(leaving), archivedKeysForTest(retained)...)
	if want = append(want, "open-a", "open-b"); !equalStringSets(keys, want) {
		t.Fatalf("the key listing = %v, want the %v both the indexes state", keys, want)
	}
	if _, err := view.Read(ctx, Fetch{Key: "checkpoint:1:1"}); !errors.Is(err, ErrArchiveCorrupt) {
		t.Fatalf("the point read of the damaged record = %v, want ErrArchiveCorrupt", err)
	}
}
