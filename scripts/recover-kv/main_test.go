// 契约: docs/wiki/memory/memory-architecture.md#event-shape
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
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
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// storedRefFixture is the whole reference record the original store persists: the
// scope it was accepted under, its acceptance time and the completeness
// information of its body.
type storedRefFixture struct {
	OwnerAgent      string    `json:"owner_agent"`
	OwnerSession    string    `json:"owner_session"`
	Format          string    `json:"format"`
	RetentionPolicy string    `json:"retention_policy"`
	StoredAt        time.Time `json:"stored_at"`
	Digest          string    `json:"digest"`
	Length          int64     `json:"length"`
}

// TestVerifyReferencesFreshRootWithoutBlobIsAligned pins the root the runtime writes today as the state the scan clears.
//   - the fact lands through the whole-fact commit, so the reference is staged alone and the text lives inside the event body
//   - the root holds no independent body, yet the scan reports no truly lost and no mismatched reference and exits 0
//   - the scan writes no byte and the recall serves the exact text out of the event body
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestVerifyReferencesFreshRootWithoutBlobIsAligned(t *testing.T) {
	ctx := context.Background()
	const key = "1408-9f2c1d3e4b5a60718293a4b5c6d7e8f9"
	const content = "the text the event body carries"
	evt := chainEvent(key, content)
	tier, root := seedRoot(t, ctx)
	commitFact(t, ctx, tier, evt, content, 1)
	closeSeeded(t, tier)
	if held := blobKeysOf(t, ctx, root); len(held) != 0 {
		t.Fatalf("the whole-fact commit staged the independent bodies %v, want none", held)
	}
	before := rootFiles(t, root)

	scanned, err := verifyOriginalReferences(ctx, root, "")
	if err != nil {
		t.Fatalf("the scan of the fresh root %s returned %v", root, err)
	}
	if len(scanned.TrulyLost) != 0 || len(scanned.Mismatch) != 0 {
		t.Fatalf("the scan of the fresh root = %+v, want no truly lost and no mismatched reference", scanned)
	}
	if scanned.exitCode() != 0 {
		t.Fatalf("the scan exit code = %d, want 0: the event body answers the reference", scanned.exitCode())
	}
	assertRootUnchanged(t, root, before)
	got := openRoot(t, ctx, root).Originals.Get(ctx, event.EventID(key))
	if got.Outcome != ledger.RecallFound || !bytes.Equal(got.Body, []byte(content)) {
		t.Fatalf("the recall of the fresh root = %+v, want the exact text %q", got, content)
	}
}

// TestRecoverKVVerifiesArchivedReferencesByIdentity pins the scan over a root whose chain keys moved.
//   - the reference is answered by the text the archive holds, so the scan reports no truly lost and no mismatched reference and exits 0
//   - the same root read without the archive root reports the reference truly lost, so the answer comes through the archive locator
//   - an archived segment the manifest states and the archive lost is reported as ErrArchiveMissing, never as a truly lost reference
//   - neither scan writes a byte of the hot root or the archive root
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestRecoverKVVerifiesArchivedReferencesByIdentity(t *testing.T) {
	ctx := context.Background()
	const key = "1408-2b1c0d9e8f7a6b5c4d3e2f1a0b9c8d7e"
	const content = "the text the archived segment carries"
	owner := event.Address{AgentID: "agent-a", SessionID: "session-a"}
	hotRoot, archiveRoot := archivedReferenceFixture(t, ctx, key, content, owner)

	hotOnly, err := verifyOriginalReferences(ctx, hotRoot, "")
	if err != nil {
		t.Fatalf("the scan of the hot root alone returned %v", err)
	}
	if got := referenceKeys(hotOnly.TrulyLost); !reflect.DeepEqual(got, []string{key}) {
		t.Fatalf("the truly lost keys of the hot root alone = %v, want the key %q the archive carries", got, key)
	}

	beforeHot := rootFiles(t, hotRoot)
	beforeArchive := rootFiles(t, archiveRoot)
	scanned, err := verifyOriginalReferences(ctx, hotRoot, archiveRoot)
	if err != nil {
		t.Fatalf("the scan over %s beside %s returned %v", hotRoot, archiveRoot, err)
	}
	if len(scanned.TrulyLost) != 0 || len(scanned.Mismatch) != 0 {
		t.Fatalf("the scan beside the archive = %+v, want no truly lost and no mismatched reference", scanned)
	}
	if scanned.exitCode() != 0 {
		t.Fatalf("the scan exit code = %d, want 0: the archived body answers the reference", scanned.exitCode())
	}
	assertRootUnchanged(t, hotRoot, beforeHot)
	assertRootUnchanged(t, archiveRoot, beforeArchive)

	lost := filepath.Join(archiveRoot, archiveSegmentDirectory, archivedSegmentLabelOf(t, archiveRoot)+archiveSegmentLogSuffix)
	if err := os.Remove(lost); err != nil {
		t.Fatalf("remove the archived segment %s: %v", lost, err)
	}
	if _, err := verifyOriginalReferences(ctx, hotRoot, archiveRoot); !errors.Is(err, ledger.ErrArchiveMissing) {
		t.Fatalf("the scan beside an archive missing a stated segment returned %v, want ErrArchiveMissing", err)
	}
}

// archivedReferenceFixture builds the state one archived reference lives in: a
// source root whose chain keys one cut seals, an archive root stating that interval,
// and a hot root holding the reference beside the seat that names its partition.
func archivedReferenceFixture(t *testing.T, ctx context.Context, key, content string, owner event.Address) (string, string) {
	t.Helper()
	sourceRoot, archiveRoot := t.TempDir(), t.TempDir()
	config := ledger.StorageConfig{Type: ledger.StorageJournal, Root: sourceRoot, WriteRights: true}
	config.HotLogMaxBytes = archivedSegmentBudget
	config.ArchiveRoot = archiveRoot
	source, err := ledger.OpenTier(ctx, config)
	if err != nil {
		t.Fatalf("open the source root %s: %v", sourceRoot, err)
	}
	commitChainEvent(t, ctx, source, chainEvent(key, content), 1)
	commitPairs(t, ctx, source, ledger.Pair{Key: "checkpoint:1:1", Value: []byte("cut")})
	closeSeeded(t, source)

	archiveSealedInterval(t, sourceRoot, archiveRoot)

	hot, hotRoot := seedRoot(t, ctx)
	partition := memory.PartitionIDFromName(string(owner.AgentID))
	seat, err := json.Marshal(map[string]any{"count": 1, "commitSeq": 2, "partitions": []int{partition}})
	if err != nil {
		t.Fatalf("state the chain seat: %v", err)
	}
	commitPairs(t, ctx, hot,
		ledger.Pair{Key: "store:seat", Value: seat},
		ledger.Pair{Key: "ref:" + key, Value: refJSON(t, []storedRefFixture{refFixture([]byte(content), owner)})})
	closeSeeded(t, hot)
	return hotRoot, archiveRoot
}

// archiveSealedInterval copies the one interval the source root sealed into a fresh
// archive root and states it in both manifest slots.
func archiveSealedInterval(t *testing.T, sourceRoot, archiveRoot string) {
	t.Helper()
	segmentsDir := filepath.Join(sourceRoot, kvDirectory, archiveSegmentDirectory)
	names, err := os.ReadDir(segmentsDir)
	if err != nil {
		t.Fatalf("read the sealed intervals of %s: %v", sourceRoot, err)
	}
	label := ""
	for _, entry := range names {
		if strings.HasSuffix(entry.Name(), archiveSegmentLogSuffix) {
			label = strings.TrimSuffix(entry.Name(), archiveSegmentLogSuffix)
			break
		}
	}
	if label == "" {
		t.Fatalf("the source root %s holds no sealed interval", sourceRoot)
	}
	target := filepath.Join(archiveRoot, archiveSegmentDirectory)
	segmentBytes := copyArchiveFile(t, filepath.Join(segmentsDir, label+archiveSegmentLogSuffix),
		filepath.Join(target, label+archiveSegmentLogSuffix))
	indexBytes := copyArchiveFile(t, filepath.Join(segmentsDir, label+archiveSegmentIndexSuffix),
		filepath.Join(target, label+archiveSegmentIndexSuffix))
	header := archiveSegmentHeaderFixtureOf(t, segmentBytes)
	keys, offsetOf := archiveSegmentIndexOf(t, indexBytes)
	keySet, err := json.Marshal(keys)
	if err != nil {
		t.Fatalf("state the key set of %s: %v", label, err)
	}
	cutKey := "checkpoint:" + strconv.Itoa(header.CutPartition) + ":" + strconv.FormatInt(header.CutSeq, 10)
	cutOffset, found := offsetOf[cutKey]
	if !found {
		t.Fatalf("the sealed interval %s states its cut at partition %d seq %d in no checkpoint key",
			label, header.CutPartition, header.CutSeq)
	}
	manifest := archiveManifestFixture{
		Version:    1,
		Generation: 1,
		Segments: []archiveSegmentFixture{{
			SegmentID: label,
			KeyDomain: archiveKeyDomainFixture{
				Partition: header.CutPartition,
				FirstKey:  keys[0],
				LastKey:   keys[len(keys)-1],
				KeysSHA:   digestOf(keySet),
			},
			Bytes:  int64(len(segmentBytes)),
			SHA256: digestOf(segmentBytes),
			Cut:    archiveCutFixture{Partition: header.CutPartition, Seq: header.CutSeq, FrameOffset: cutOffset},
			OffsetMap: archiveOffsetMapFixture{
				FirstSeq:    header.FirstSeq,
				LastSeq:     header.LastSeq,
				FrameCount:  len(keys),
				IndexPath:   label + archiveSegmentIndexSuffix,
				IndexSHA256: digestOf(indexBytes),
			},
		}},
		ArchivedThroughCut: archiveCutFixture{Partition: header.CutPartition, Seq: header.CutSeq},
	}
	body, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("state the archive manifest: %v", err)
	}
	for _, name := range []string{archiveManifestAName, archiveManifestBName} {
		if err := os.WriteFile(filepath.Join(archiveRoot, name), body, 0o600); err != nil {
			t.Fatalf("write the manifest slot %s: %v", name, err)
		}
	}
}

// archivedSegmentLabelOf answers the label of the one interval an archive root states.
func archivedSegmentLabelOf(t *testing.T, archiveRoot string) string {
	t.Helper()
	names, err := os.ReadDir(filepath.Join(archiveRoot, archiveSegmentDirectory))
	if err != nil {
		t.Fatalf("read the archived intervals of %s: %v", archiveRoot, err)
	}
	for _, entry := range names {
		if strings.HasSuffix(entry.Name(), archiveSegmentLogSuffix) {
			return strings.TrimSuffix(entry.Name(), archiveSegmentLogSuffix)
		}
	}
	t.Fatalf("the archive root %s holds no segment file", archiveRoot)
	return ""
}

// copyArchiveFile copies one file into the archive root and answers the bytes it copies.
func copyArchiveFile(t *testing.T, source, target string) []byte {
	t.Helper()
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read %s: %v", source, err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("open the archive directory %s: %v", filepath.Dir(target), err)
	}
	if err := os.WriteFile(target, raw, 0o600); err != nil {
		t.Fatalf("write %s: %v", target, err)
	}
	return raw
}

// archiveSegmentHeaderFixtureOf answers the header one framed segment file states.
func archiveSegmentHeaderFixtureOf(t *testing.T, raw []byte) archiveSegmentHeaderFixture {
	t.Helper()
	var header archiveSegmentHeaderFixture
	if err := json.Unmarshal(framedPayloadOf(t, raw), &header); err != nil {
		t.Fatalf("decode the segment header: %v", err)
	}
	return header
}

// archiveSegmentIndexOf answers the keys one framed segment index states, in key
// order, beside the byte each key's newest frame starts at.
func archiveSegmentIndexOf(t *testing.T, raw []byte) ([]string, map[string]int64) {
	t.Helper()
	var index struct {
		Entries []struct {
			Key    string `json:"key"`
			Offset int64  `json:"offset"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(framedPayloadOf(t, raw), &index); err != nil {
		t.Fatalf("decode the segment index: %v", err)
	}
	if len(index.Entries) == 0 {
		t.Fatalf("the segment index states no key")
	}
	keys := make([]string, 0, len(index.Entries))
	offsetOf := make(map[string]int64, len(index.Entries))
	for _, entry := range index.Entries {
		keys = append(keys, entry.Key)
		offsetOf[entry.Key] = entry.Offset
	}
	sort.Strings(keys)
	return keys, offsetOf
}

// framedPayloadOf answers the JSON payload one framed file carries at its front.
func framedPayloadOf(t *testing.T, raw []byte) []byte {
	t.Helper()
	if len(raw) < 8 {
		t.Fatalf("the framed file holds %d byte(s), shorter than a frame header", len(raw))
	}
	length := int(binary.BigEndian.Uint32(raw[0:4]))
	if length < 1 || len(raw) < 8+length {
		t.Fatalf("the framed file declares %d payload byte(s) and carries %d", length, len(raw)-8)
	}
	return raw[8 : 8+length]
}

// archivedSegmentBudget is the hot log budget the source root seals one interval beside.
const archivedSegmentBudget = 1 << 20

// archiveSegmentDirectory names the segment directory under an archive root, beside
// the two manifest slots and the suffixes of one segment and its index.
const (
	archiveSegmentDirectory   = "segments"
	archiveManifestAName      = "manifest.a"
	archiveManifestBName      = "manifest.b"
	archiveSegmentLogSuffix   = ".log"
	archiveSegmentIndexSuffix = ".idx"
)

// archiveManifestFixture is one manifest generation as these cases state it: the same
// documented fields the journal writes, so the reader resolves a statement no writer
// in this package produced.
type archiveManifestFixture struct {
	Version            int                     `json:"version"`
	Generation         int                     `json:"generation"`
	Segments           []archiveSegmentFixture `json:"segments"`
	ArchivedThroughCut archiveCutFixture       `json:"archived_through_cut"`
}

// archiveSegmentFixture is one archived segment as the manifest states it.
type archiveSegmentFixture struct {
	SegmentID string                  `json:"segment_id"`
	KeyDomain archiveKeyDomainFixture `json:"key_domain"`
	Bytes     int64                   `json:"bytes"`
	SHA256    string                  `json:"sha256"`
	Cut       archiveCutFixture       `json:"cut"`
	OffsetMap archiveOffsetMapFixture `json:"offset_map"`
}

// archiveKeyDomainFixture is the conservative interval one segment answers.
type archiveKeyDomainFixture struct {
	Partition int    `json:"partition"`
	FirstKey  string `json:"first_key"`
	LastKey   string `json:"last_key"`
	KeysSHA   string `json:"keys_sha256"`
}

// archiveCutFixture is the cut one segment boundary closed at.
type archiveCutFixture struct {
	Partition   int   `json:"partition"`
	Seq         int64 `json:"seq"`
	FrameOffset int64 `json:"frame_offset"`
}

// archiveOffsetMapFixture states where the index of one segment lives and what it digests to.
type archiveOffsetMapFixture struct {
	FirstSeq    int64  `json:"first_seq"`
	LastSeq     int64  `json:"last_seq"`
	FrameCount  int    `json:"frame_count"`
	IndexPath   string `json:"index_path"`
	IndexSHA256 string `json:"index_sha256"`
}

// archiveSegmentHeaderFixture is the header one sealed segment states about itself.
type archiveSegmentHeaderFixture struct {
	SegmentID    string `json:"segment_id"`
	FirstSeq     int64  `json:"first_seq"`
	LastSeq      int64  `json:"last_seq"`
	CutPartition int    `json:"cut_partition"`
	CutSeq       int64  `json:"cut_seq"`
	Bytes        int64  `json:"bytes"`
}

// TestVerifyReferencesLostBlobIsReadBackFromTheEventBody pins a lost independent body as a root the scan clears.
//   - the reference was registered under two scopes and the chain still holds the text
//   - the scan reports neither class and exits 0, and the recall serves the exact text
//   - the scan writes no byte, so the body stays out of the root it was lost from
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestVerifyReferencesLostBlobIsReadBackFromTheEventBody(t *testing.T) {
	ctx := context.Background()
	const key = "1408-d87b587dc0a350d14b98763f8f14748c"
	const content = "the body the chain still holds"
	evt := chainEvent(key, content)
	tier, root := seedRoot(t, ctx)
	commitChainEvent(t, ctx, tier, evt, 1)
	joined := event.Address{AgentID: evt.Owner.AgentID, SessionID: "session-b"}
	records := []storedRefFixture{refFixture([]byte(content), evt.Owner), refFixture([]byte(content), joined)}
	commitPairs(t, ctx, tier, ledger.Pair{Key: "ref:" + key, Value: refJSON(t, records)})
	closeSeeded(t, tier)
	before := rootFiles(t, root)

	scanned, err := verifyOriginalReferences(ctx, root, "")
	if err != nil {
		t.Fatalf("the scan over %s returned %v", root, err)
	}
	if len(scanned.TrulyLost) != 0 || len(scanned.Mismatch) != 0 {
		t.Fatalf("the scan of a root whose body is lost = %+v, want no truly lost and no mismatched reference", scanned)
	}
	if scanned.exitCode() != 0 {
		t.Fatalf("the scan exit code = %d, want 0: the event body answers both references", scanned.exitCode())
	}
	assertRootUnchanged(t, root, before)
	if held := blobKeysOf(t, ctx, root); len(held) != 0 {
		t.Fatalf("the scan staged the bodies %v, want none", held)
	}
	got := openRoot(t, ctx, root).Originals.Get(ctx, event.EventID(key))
	if got.Outcome != ledger.RecallFound || !bytes.Equal(got.Body, []byte(content)) {
		t.Fatalf("the recall of a root whose body is lost = %+v, want the exact text %q", got, content)
	}
}

// TestVerifyReferencesDigestMismatchIsNamed pins the named report of a copy the records do not name.
//   - a digest of other bytes and a length of other bytes are both reported a mismatch by name
//   - the exit code of either is 1, and the scan writes no byte
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestVerifyReferencesDigestMismatchIsNamed(t *testing.T) {
	ctx := context.Background()
	const key = "1408-0000000000000000000000000000beef"
	const content = "the body the chain holds"
	owner := event.Address{AgentID: "agent-a", SessionID: "session-a"}
	otherDigest := refFixture([]byte("another body entirely"), owner)
	otherLength := refFixture([]byte(content), owner)
	otherLength.Length++
	cases := []struct {
		name   string
		record storedRefFixture
	}{
		{name: "digest", record: otherDigest},
		{name: "length", record: otherLength},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tier, root := seedRoot(t, ctx)
			commitChainEvent(t, ctx, tier, chainEvent(key, content), 1)
			commitPairs(t, ctx, tier, ledger.Pair{Key: "ref:" + key, Value: refJSON(t, []storedRefFixture{tc.record})})
			closeSeeded(t, tier)
			before := rootFiles(t, root)

			scanned, err := verifyOriginalReferences(ctx, root, "")
			if err != nil {
				t.Fatalf("the scan over %s returned %v", root, err)
			}
			want := reference{Key: key, OwnerAgent: string(owner.AgentID), OwnerSession: string(owner.SessionID)}
			if len(scanned.Mismatch) != 1 || scanned.Mismatch[0] != want {
				t.Fatalf("the report = %+v, want the mismatched reference %+v", scanned, want)
			}
			if len(scanned.TrulyLost) != 0 {
				t.Fatalf("the report = %+v, want no truly lost reference", scanned)
			}
			if scanned.exitCode() != 1 {
				t.Fatalf("the exit code = %d, want 1 for a reference the chain does not answer", scanned.exitCode())
			}
			assertRootUnchanged(t, root, before)
			got := openRoot(t, ctx, root).Originals.Get(ctx, event.EventID(key))
			if got.Outcome != ledger.RecallFailed || !errors.Is(got.Err, ledger.ErrIntegrityMismatch) {
				t.Fatalf("the recall of the mismatched root = %+v, want the named integrity refusal", got)
			}
		})
	}
}

// TestVerifyReferencesWithoutEventIsTrulyLost pins the reference that no copy of the root answers.
//   - the reference is reported truly lost by name with its owner, and the exit code is 1
//   - the scan writes no byte and the recall keeps refusing that reference by name
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestVerifyReferencesWithoutEventIsTrulyLost(t *testing.T) {
	ctx := context.Background()
	const key = "1408-0000000000000000000000000000dead"
	owner := event.Address{AgentID: "agent-lost", SessionID: "session-lost"}
	tier, root := seedRoot(t, ctx)
	commitPairs(t, ctx, tier, ledger.Pair{Key: "ref:" + key, Value: refJSON(t, []storedRefFixture{refFixture([]byte("the body no copy holds"), owner)})})
	closeSeeded(t, tier)
	before := rootFiles(t, root)

	scanned, err := verifyOriginalReferences(ctx, root, "")
	if err != nil {
		t.Fatalf("the scan over %s returned %v", root, err)
	}
	want := reference{Key: key, OwnerAgent: string(owner.AgentID), OwnerSession: string(owner.SessionID)}
	if len(scanned.TrulyLost) != 1 || scanned.TrulyLost[0] != want {
		t.Fatalf("the report = %+v, want the truly lost reference %+v", scanned, want)
	}
	if len(scanned.Mismatch) != 0 {
		t.Fatalf("the report = %+v, want no mismatched reference", scanned)
	}
	if scanned.exitCode() != 1 {
		t.Fatalf("the exit code = %d, want 1 for a reference no copy answers", scanned.exitCode())
	}
	assertRootUnchanged(t, root, before)
	if held := blobKeysOf(t, ctx, root); len(held) != 0 {
		t.Fatalf("the scan wrote the bodies %v, want none", held)
	}
	assertRecallRefuses(t, ctx, root, key)
}

// TestVerifyReferencesBlobDriftIsNamed pins an independent body an old root keeps as a copy held to the same account.
//   - a body equal to the recorded bytes is not reported, and the root exits 0
//   - a body of other bytes is reported a mismatch by name whatever the chain holds, and the exit code is 1
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestVerifyReferencesBlobDriftIsNamed(t *testing.T) {
	ctx := context.Background()
	const key = "1408-0000000000000000000000000000face"
	const content = "the body the reference names"
	owner := event.Address{AgentID: "agent-a", SessionID: "session-a"}
	cases := []struct {
		name   string
		staged []byte
		want   []reference
	}{
		{name: "matching", staged: []byte(content)},
		{name: "drifted", staged: []byte("another body the root staged"), want: []reference{{Key: key, OwnerAgent: string(owner.AgentID), OwnerSession: string(owner.SessionID)}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tier, root := seedRoot(t, ctx)
			commitChainEvent(t, ctx, tier, chainEvent(key, content), 1)
			commitPairs(t, ctx, tier,
				ledger.Pair{Key: "blob:" + key, Value: tc.staged},
				ledger.Pair{Key: "ref:" + key, Value: refJSON(t, []storedRefFixture{refFixture([]byte(content), owner)})})
			closeSeeded(t, tier)
			before := rootFiles(t, root)

			scanned, err := verifyOriginalReferences(ctx, root, "")
			if err != nil {
				t.Fatalf("the scan over %s returned %v", root, err)
			}
			if len(scanned.TrulyLost) != 0 {
				t.Fatalf("the report = %+v, want no truly lost reference", scanned)
			}
			if !reflect.DeepEqual(scanned.Mismatch, tc.want) {
				t.Fatalf("the mismatched references = %v, want %v", scanned.Mismatch, tc.want)
			}
			wantCode := 0
			if len(tc.want) > 0 {
				wantCode = 1
			}
			if scanned.exitCode() != wantCode {
				t.Fatalf("the exit code = %d, want %d", scanned.exitCode(), wantCode)
			}
			assertRootUnchanged(t, root, before)
		})
	}
}

// TestVerifyReferencesSortedOrder pins the key order of every class across repeated scans of one root.
//   - the references are committed out of key order and each class comes back in key order
//   - ten scans report one identical report
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestVerifyReferencesSortedOrder(t *testing.T) {
	ctx := context.Background()
	owner := event.Address{AgentID: "agent-a", SessionID: "session-a"}
	tier, root := seedRoot(t, ctx)
	fixtures := []struct {
		key     string
		chain   string
		refBody string
	}{
		{key: "1408-cccc", chain: "the third body", refBody: "the third body"},
		{key: "1408-aaaa", refBody: "the first body"},
		{key: "1408-bbbb", chain: "the body the chain holds", refBody: "the body the reference names"},
		{key: "1408-dddd", chain: "the fourth body", refBody: "the fourth body"},
		{key: "1408-eeee", refBody: "the fifth body"},
	}
	seq := int64(0)
	for _, fixture := range fixtures {
		seq++
		if fixture.chain != "" {
			commitChainEvent(t, ctx, tier, chainEvent(fixture.key, fixture.chain), seq)
		}
		records := []storedRefFixture{refFixture([]byte(fixture.refBody), owner)}
		commitPairs(t, ctx, tier, ledger.Pair{Key: "ref:" + fixture.key, Value: refJSON(t, records)})
	}
	closeSeeded(t, tier)
	before := rootFiles(t, root)
	wantLost := []string{"1408-aaaa", "1408-eeee"}
	wantMismatch := []string{"1408-bbbb"}

	var first report
	for run := 0; run < 10; run++ {
		scanned, err := verifyOriginalReferences(ctx, root, "")
		if err != nil {
			t.Fatalf("scan %d over %s returned %v", run, root, err)
		}
		if run == 0 {
			first = scanned
			if got := referenceKeys(scanned.TrulyLost); !reflect.DeepEqual(got, wantLost) {
				t.Fatalf("the truly lost keys = %v, want %v", got, wantLost)
			}
			if got := referenceKeys(scanned.Mismatch); !reflect.DeepEqual(got, wantMismatch) {
				t.Fatalf("the mismatched keys = %v, want %v", got, wantMismatch)
			}
			if scanned.exitCode() != 1 {
				t.Fatalf("the exit code = %d, want 1 for a root holding unanswered references", scanned.exitCode())
			}
			continue
		}
		if !reflect.DeepEqual(scanned, first) {
			t.Fatalf("scan %d = %+v, want the report of the first run %+v", run, scanned, first)
		}
	}
	assertRootUnchanged(t, root, before)
}

// TestVerifyReferencesUndecodableReference pins the named refusal of a reference record that states no text to verify.
//   - bytes that are no JSON array and an empty array both fail naming the key
//   - neither case writes a byte
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestVerifyReferencesUndecodableReference(t *testing.T) {
	ctx := context.Background()
	const key = "1408-0000000000000000000000000000abcd"
	cases := []struct {
		name  string
		value []byte
	}{
		{name: "not json", value: []byte("not json")},
		{name: "empty array", value: []byte("[]")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tier, root := seedRoot(t, ctx)
			commitChainEvent(t, ctx, tier, chainEvent(key, "the body the chain holds"), 1)
			commitPairs(t, ctx, tier, ledger.Pair{Key: "ref:" + key, Value: tc.value})
			closeSeeded(t, tier)
			before := rootFiles(t, root)

			if _, err := verifyOriginalReferences(ctx, root, ""); err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("the scan of the %s reference returned %v, want the refusal naming %s", tc.name, err, key)
			}
			assertRootUnchanged(t, root, before)
			if held := blobKeysOf(t, ctx, root); len(held) != 0 {
				t.Fatalf("the scan wrote the bodies %v, want none", held)
			}
		})
	}
}

// TestDigestOfCanonicalForm pins the digest text to the published sha256 of the empty body, the form the original store records.
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestDigestOfCanonicalForm(t *testing.T) {
	const want = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got := digestOf(nil); got != want {
		t.Fatalf("digestOf(nil) = %q, want %q", got, want)
	}
}

// TestVerifyKVCommandExitCodes pins the flags and the exit status of the built command over live journal roots.
//   - no --root fails nonzero naming the flag
//   - the scan of a root the event body answers exits 0, reports every reference answered and writes no byte
//   - --restore is gone: the command refuses it by name, so no run of it writes a body
//   - a truly lost reference exits nonzero and names the key with its owner on stdout
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestVerifyKVCommandExitCodes(t *testing.T) {
	ctx := context.Background()
	binary := buildVerifyKV(t, ctx)
	const key = "1408-0000000000000000000000000000c0de"
	const content = "the body the command reads back"
	owner := event.Address{AgentID: "agent-cli", SessionID: "session-cli"}
	tier, root := seedRoot(t, ctx)
	commitFact(t, ctx, tier, chainEvent(key, content), content, 1)
	lostTier, lostRoot := seedRoot(t, ctx)
	lostKey := "1408-0000000000000000000000000000d00d"
	commitPairs(t, ctx, lostTier, ledger.Pair{Key: "ref:" + lostKey, Value: refJSON(t, []storedRefFixture{refFixture([]byte("the body no copy holds"), owner)})})
	closeSeeded(t, tier)
	closeSeeded(t, lostTier)

	_, errOut, code := runVerifyKV(t, ctx, binary)
	if code != 1 || !strings.Contains(errOut, "--root is required") {
		t.Fatalf("the command without --root exited %d with stderr %q, want 1 naming the flag", code, errOut)
	}
	before := rootFiles(t, root)
	out, errOut, code := runVerifyKV(t, ctx, binary, "--root", root)
	if code != 0 || !strings.Contains(out, "every reference is answered by the text it names") {
		t.Fatalf("the scan exited %d with stdout %q and stderr %q, want 0 reporting every reference answered", code, out, errOut)
	}
	assertRootUnchanged(t, root, before)
	_, errOut, code = runVerifyKV(t, ctx, binary, "--root", root, "--restore")
	if code != 2 || !strings.Contains(errOut, "-restore") {
		t.Fatalf("the removed --restore exited %d with stderr %q, want 2 naming the flag as undefined", code, errOut)
	}
	assertRootUnchanged(t, root, before)

	lostBefore := rootFiles(t, lostRoot)
	out, errOut, code = runVerifyKV(t, ctx, binary, "--root", lostRoot)
	if code != 1 {
		t.Fatalf("the scan of a truly lost reference exited %d, want 1", code)
	}
	for _, want := range []string{lostKey, string(owner.AgentID), string(owner.SessionID), "truly lost"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the stdout %q does not name %q", out, want)
		}
	}
	if !strings.Contains(errOut, "unable to serve every original") {
		t.Fatalf("the stderr %q does not report the root as unable to serve every original", errOut)
	}
	assertRootUnchanged(t, lostRoot, lostBefore)
}

// seedRoot opens a journal root in a temporary directory for one case to commit
// into, and returns the tier beside the root path. The seeding view declares the
// root's write rights, which is the declaration a commit needs.
func seedRoot(t *testing.T, ctx context.Context) (*ledger.Tier, string) {
	t.Helper()
	root := t.TempDir()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the journal root %s to seed it: %v", root, err)
	}
	return tier, root
}

// closeSeeded ends the view one case seeded its root through: one root carries one
// writer, so the scan under test opens a root the seeding writer gave up rather
// than one it holds. It runs before the case snapshots the root, because the close
// completes the seeding barrier.
func closeSeeded(t *testing.T, tier *ledger.Tier) {
	t.Helper()
	if err := tier.Close(); err != nil {
		t.Fatalf("close the seeding view: %v", err)
	}
}

// openRoot opens a fresh view of root, so an assertion reads what the journal
// holds rather than what an earlier view published.
func openRoot(t *testing.T, ctx context.Context, root string) *ledger.Tier {
	t.Helper()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("open the journal root %s: %v", root, err)
	}
	return tier
}

// chainEvent returns one event carrying content under key, owned by the address
// the cases register their references under.
func chainEvent(key, content string) event.Event {
	return event.Event{
		Key:       event.EventID(key),
		Type:      event.TypeAgentOutput,
		Timestamp: time.UnixMilli(1760000000123),
		Owner:     event.Address{AgentID: "agent-a", SessionID: "session-a"},
		Message:   model.Message{Role: model.RoleAssistant, Content: content},
	}
}

// chainRecord returns the segment record one event commits at the sequence slot the
// case names: the encoded body of evt and the chain fields of its type and session.
func chainRecord(t *testing.T, evt event.Event, seq int64) ledger.SegmentRecord {
	t.Helper()
	body, err := memory.BodyFromEvent(evt, "text/plain", "keep")
	if err != nil {
		t.Fatalf("derive the body of %s: %v", evt.Key, err)
	}
	encoded, err := body.Encode()
	if err != nil {
		t.Fatalf("encode the body of %s: %v", evt.Key, err)
	}
	window := evt.Timestamp.UnixMilli()
	return ledger.SegmentRecord{
		PartitionID: memory.PartitionIDFromName(string(evt.Owner.AgentID)),
		WindowTS:    window,
		Seq:         seq,
		EventKey:    evt.Key,
		EventTime:   window,
		Body:        encoded,
	}.WithChain(evt.Type, evt.Owner.SessionID)
}

// commitChainEvent commits the record of evt as one chain record of the tier, at
// the sequence slot the case names, staging no reference.
func commitChainEvent(t *testing.T, ctx context.Context, tier *ledger.Tier, evt event.Event, seq int64) {
	t.Helper()
	if err := tier.Segments.StoreEvent(ctx, chainRecord(t, evt, seq)); err != nil {
		t.Fatalf("commit the chain event %s: %v", evt.Key, err)
	}
}

// commitFact commits evt and its reference through the whole-fact commit, which is
// the write the runtime performs: the reference is staged alone and content lands
// inside the event body.
func commitFact(t *testing.T, ctx context.Context, tier *ledger.Tier, evt event.Event, content string, seq int64) {
	t.Helper()
	outcome, err := ledger.CommitFact(ctx, tier, ledger.FactCommit{
		Record: chainRecord(t, evt, seq),
		Ref: ledger.OriginalRef{
			Key:             evt.Key,
			Owner:           evt.Owner,
			Format:          "text/plain",
			RetentionPolicy: "keep",
		},
		Body: []byte(content),
	})
	if err != nil || outcome != ledger.FactCommitted {
		t.Fatalf("commit the fact %s = %v, %v, want the committed outcome", evt.Key, outcome, err)
	}
}

// refFixture returns the reference record of body under owner, its completeness
// information computed over the bytes themselves rather than by the command.
func refFixture(body []byte, owner event.Address) storedRefFixture {
	sum := sha256.Sum256(body)
	return storedRefFixture{
		OwnerAgent:      string(owner.AgentID),
		OwnerSession:    string(owner.SessionID),
		Format:          "text/plain",
		RetentionPolicy: "keep",
		StoredAt:        time.UnixMilli(1760000000123),
		Digest:          "sha256:" + hex.EncodeToString(sum[:]),
		Length:          int64(len(body)),
	}
}

// refJSON encodes the reference records of one key as the value the original
// store's load reads.
func refJSON(t *testing.T, records []storedRefFixture) []byte {
	t.Helper()
	value, err := json.Marshal(records)
	if err != nil {
		t.Fatalf("encode the reference records: %v", err)
	}
	return value
}

// commitPairs commits pairs into the tier behind one barrier.
func commitPairs(t *testing.T, ctx context.Context, tier *ledger.Tier, pairs ...ledger.Pair) {
	t.Helper()
	if err := tier.Store.Commit(ctx, ledger.Batch{Puts: pairs}); err != nil {
		t.Fatalf("commit %d pairs: %v", len(pairs), err)
	}
}

// blobKeysOf returns the body keys a freshly opened view of root holds, in key
// order.
func blobKeysOf(t *testing.T, ctx context.Context, root string) []string {
	t.Helper()
	store, err := ledger.OpenAtomicStore(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("open the journal root %s: %v", root, err)
	}
	keys, err := store.Keys(ctx, "blob:")
	if err != nil {
		t.Fatalf("read the body keys of %s: %v", root, err)
	}
	return keys
}

// assertRecallRefuses reports the recall of root as the named refusal of the
// reference of key that no copy of the root carries the text for.
func assertRecallRefuses(t *testing.T, ctx context.Context, root, key string) {
	t.Helper()
	got := openRoot(t, ctx, root).Originals.Get(ctx, event.EventID(key))
	if got.Outcome != ledger.RecallFailed || !errors.Is(got.Err, ledger.ErrSnapshotCorrupt) || !strings.Contains(got.Err.Error(), key) {
		t.Fatalf("the recall of %s = %+v, want the named refusal of the reference of %s with no text", root, got, key)
	}
}

// rootWriteLockMarker names the file a write-rights open keeps inside the root it writes.
const rootWriteLockMarker = ".write-lock"

// rootFiles returns the bytes of every file under root keyed by its relative
// path, so a case can pin a scan that writes nothing byte for byte.
//   - the write-lock marker of the root is left out: it carries no journal byte, being the file the kernel holds an open's write rights on
func rootFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if name == rootWriteLockMarker {
			return nil
		}
		value, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[name] = value
		return nil
	})
	if err != nil {
		t.Fatalf("read the files of %s: %v", root, err)
	}
	return files
}

// assertRootUnchanged names every file of root whose bytes differ from the
// snapshot, and every file of the snapshot that root does not hold.
func assertRootUnchanged(t *testing.T, root string, before map[string][]byte) {
	t.Helper()
	after := rootFiles(t, root)
	if len(after) != len(before) {
		t.Fatalf("the scan left %d files under %s, want the %d it held before", len(after), root, len(before))
	}
	for name, want := range before {
		got, held := after[name]
		if !held {
			t.Fatalf("the scan removed %s of %s", name, root)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("the scan rewrote %s of %s", name, root)
		}
	}
}

// referenceKeys returns the keys of one reported class in the order it holds them.
func referenceKeys(list []reference) []string {
	keys := make([]string, 0, len(list))
	for _, item := range list {
		keys = append(keys, item.Key)
	}
	return keys
}

// kvDirectory is the directory under a journal root that holds the journal and its index
// files, which is where a case appends the torn frame a writer left.
const kvDirectory = "kv"

// TestVerifyScanStatesTheTornTailAndTheIndexItRead pins what the scan states about a root whose log ends torn.
//   - the scan completes and verifies the root's references instead of refusing the root
//   - stdout names the torn tail the open found, the bytes the scan left uncut and the whole log it replayed
//   - the root keeps every file byte for byte
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestVerifyScanStatesTheTornTailAndTheIndexItRead(t *testing.T) {
	ctx := context.Background()
	const key = "1408-8d0c9b7a6f5e4d3c2b1a0918f7e6d5c4"
	const content = "the body the chain still holds"
	evt := chainEvent(key, content)
	tier, root := seedRoot(t, ctx)
	commitChainEvent(t, ctx, tier, evt, 1)
	commitPairs(t, ctx, tier, ledger.Pair{
		Key:   "ref:" + key,
		Value: refJSON(t, []storedRefFixture{refFixture([]byte(content), evt.Owner)}),
	})
	closeSeeded(t, tier)
	kvDir := filepath.Join(root, kvDirectory)
	torn := appendTornFrame(t, filepath.Join(kvDir, "journal.log"))
	stateAnotherVersion(t, filepath.Join(kvDir, "slots"))
	before := rootFiles(t, root)

	binary := buildVerifyKV(t, ctx)
	out, errOut, code := runVerifyKV(t, ctx, binary, "--root", root)
	if code != 0 {
		t.Fatalf("the scan of a root whose index states no watermark exited %d with stderr %q, want 0: the scan reads the log whole",
			code, errOut)
	}
	for _, want := range []string{
		"states no watermark this build reads",
		"replayed the whole log",
		"the log ends in a torn tail at " + strconv.FormatInt(torn, 10),
		"and the scan cut nothing",
		"every reference is answered by the text it names",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the stdout %q does not name %q", out, want)
		}
	}
	assertRootUnchanged(t, root, before)
}

// appendTornFrame appends the first bytes of a frame the case never finishes to path, which
// is the tail a writer that stopped mid-frame left, and answers the byte that tail starts at.
func appendTornFrame(t *testing.T, path string) int64 {
	t.Helper()
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatalf("state the log %s: %v", path, err)
	}
	torn := make([]byte, 8+20)
	binary.BigEndian.PutUint32(torn[0:4], 64)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open the log %s to append a torn frame: %v", path, err)
	}
	if _, err := file.Write(torn); err != nil {
		file.Close()
		t.Fatalf("append a torn frame to %s: %v", path, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close the log %s: %v", path, err)
	}
	return stat.Size()
}

// stateAnotherVersion writes a slot table header of another version over the table at path,
// which is the header a build that wrote another format left and this build states no
// watermark from.
func stateAnotherVersion(t *testing.T, path string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open the slot table %s: %v", path, err)
	}
	version := make([]byte, 4)
	if _, err := file.ReadAt(version, 4); err != nil {
		file.Close()
		t.Fatalf("read the version of %s: %v", path, err)
	}
	binary.LittleEndian.PutUint32(version, binary.LittleEndian.Uint32(version)+1)
	if _, err := file.WriteAt(version, 4); err != nil {
		file.Close()
		t.Fatalf("state another version in %s: %v", path, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close the slot table %s: %v", path, err)
	}
}

// buildVerifyKV builds the command into a temporary directory and returns its
// path, so a case runs the flags and the exit status an operator meets.
func buildVerifyKV(t *testing.T, ctx context.Context) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "recover-kv")
	out, err := exec.CommandContext(ctx, "go", "build", "-o", binary, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build the command: %v\n%s", err, out)
	}
	return binary
}

// runVerifyKV runs the built command with args and returns its stdout, its
// stderr and its exit status.
func runVerifyKV(t *testing.T, ctx context.Context, binary string, args ...string) (string, string, int) {
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
