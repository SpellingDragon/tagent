// The residency checks of the segment store: the store keeps a small index and a
// bounded read cache, and reads an evicted event body back from its persisted
// entry. The memory assertions are process-level (force GC, then ReadMemStats),
// not the store's own accounting: what a bounded process holds is the claim.
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
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
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// dropSegmentCache clears the read cache of one store, so a check exercises the
// cold read path the eviction policy would reach after enough commits.
func dropSegmentCache(store *FileSegmentStore) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.cache = newSegmentCache()
}

// segmentRecord builds one event record with a distinct body of the given size.
func segmentRecord(partition int, window, seq int64, key string, size int) SegmentRecord {
	body := []byte(fmt.Sprintf("%d|%d|%d|%s|", partition, window, seq, key))
	if size > len(body) {
		body = append(body, bytes.Repeat([]byte("e"), size-len(body))...)
	}
	return SegmentRecord{
		PartitionID: partition,
		WindowTS:    window,
		Seq:         seq,
		EventKey:    event.EventID(key),
		EventTime:   window + seq,
		Body:        body,
	}
}

// openJournalSegmentStore opens a segment store over a fresh journal view rooted
// at root and answers both sides.
func openJournalSegmentStore(t *testing.T, root string) (*FileSegmentStore, *journalKV) {
	t.Helper()
	kv, err := openJournalKV(root)
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	store, err := newFileSegmentStore(kv)
	if err != nil {
		t.Fatalf("open the segment store: %v", err)
	}
	return store, kv
}

// commitSegmentEvents commits count events of the given body size into one
// partition window sequence, dropping the key-value resident set after each
// commit so the check measures the segment cache rather than both layers.
func commitSegmentEvents(t *testing.T, store *FileSegmentStore, kv *journalKV, count int, bodySize int) []SegmentRecord {
	t.Helper()
	ctx := context.Background()
	records := make([]SegmentRecord, 0, count)
	for at := 0; at < count; at++ {
		rec := segmentRecord(0, 1000+int64(at/4), int64(at%4), fmt.Sprintf("event-%04d", at), bodySize)
		if err := store.StoreEvent(ctx, rec); err != nil {
			t.Fatalf("commit event %d: %v", at, err)
		}
		records = append(records, SegmentRecord{
			PartitionID: rec.PartitionID,
			WindowTS:    rec.WindowTS,
			Seq:         rec.Seq,
			EventKey:    rec.EventKey,
			EventTime:   rec.EventTime,
		})
		dropJournalResident(kv)
	}
	return records
}

// segmentBodyOf regenerates the body a committed test record carries, so a
// check never has to keep every body in memory to verify a read.
func segmentBodyOf(rec SegmentRecord, size int) []byte {
	return segmentRecord(rec.PartitionID, rec.WindowTS, rec.Seq, string(rec.EventKey), size).Body
}

// segmentRecordWithBody returns the metadata record carrying its regenerated
// body, which is the record a repeat commit names.
func segmentRecordWithBody(rec SegmentRecord, size int) SegmentRecord {
	rec.Body = segmentBodyOf(rec, size)
	return rec
}

// TestSegmentCacheRespectsByteBudget pins the segment read cache.
// - a store far larger than the cache budget keeps the process bounded after a forced collection
// - every event still reads back exactly
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestSegmentCacheRespectsByteBudget(t *testing.T) {
	root := t.TempDir()
	store, kv := openJournalSegmentStore(t, root)
	before := heapInUse()
	const events = 16
	const bodySize = 1 << 20
	records := commitSegmentEvents(t, store, kv, events, bodySize)
	after := heapInUse()
	if growth := int64(after) - int64(before); growth > segmentCacheBudgetBytes+4<<20 {
		t.Fatalf("resident heap growth = %d bytes after committing %d MiB, want the %d-byte cache budget plus slack",
			growth, events*bodySize>>20, segmentCacheBudgetBytes)
	}
	if store.cache.bytes > segmentCacheBudgetBytes {
		t.Fatalf("cache bytes = %d, want at most the %d-byte budget", store.cache.bytes, segmentCacheBudgetBytes)
	}
	dropSegmentCache(store)
	for at := range records {
		stored, held, err := store.GetEvent(context.Background(), records[at].EventKey)
		if err != nil || !held {
			t.Fatalf("read event %d = (held=%t, %v)", at, held, err)
		}
		if !bytes.Equal(stored.Body, segmentBodyOf(records[at], bodySize)) {
			t.Fatalf("cold read event %d carries different bytes", at)
		}
	}
}

// TestSegmentGetEventColdMiss pins the read of an evicted event.
// - GetEvent answers the committed body and index position from the persisted entry
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestSegmentGetEventColdMiss(t *testing.T) {
	root := t.TempDir()
	store, kv := openJournalSegmentStore(t, root)
	records := commitSegmentEvents(t, store, kv, 6, 256<<10)
	dropSegmentCache(store)
	for at := range records {
		rec := records[at]
		stored, held, err := store.GetEvent(context.Background(), rec.EventKey)
		if err != nil || !held {
			t.Fatalf("cold GetEvent(%s) = (held=%t, %v)", rec.EventKey, held, err)
		}
		if stored.PartitionID != rec.PartitionID || stored.WindowTS != rec.WindowTS || stored.Seq != rec.Seq {
			t.Fatalf("cold GetEvent(%s) index = %d/%d/%d, want %d/%d/%d",
				rec.EventKey, stored.PartitionID, stored.WindowTS, stored.Seq, rec.PartitionID, rec.WindowTS, rec.Seq)
		}
		if !bytes.Equal(stored.Body, segmentBodyOf(rec, 256<<10)) {
			t.Fatalf("cold GetEvent(%s) carries different bytes", rec.EventKey)
		}
	}
}

// TestArchiveReadsBypassResidentAndSegmentCaches pins archive bodies staying cold while hot bodies stay warm.
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestArchiveReadsBypassResidentAndSegmentCaches(t *testing.T) {
	ctx := context.Background()
	root, archiveRoot := t.TempDir(), t.TempDir()
	cfg := StorageConfig{Type: StorageJournal, Root: root, WriteRights: true}
	cfg.ArchiveRoot = archiveRoot
	cfg.HotLogMaxBytes = 1
	writer, err := OpenTier(ctx, cfg)
	if err != nil {
		t.Fatalf("open the archive-backed writer: %v", err)
	}
	t.Cleanup(func() { writer.Close() })
	archived := segmentRecord(7, 1000, 1, "archived-event", 256)
	hot := segmentRecord(7, 1001, 2, "hot-event", 256)
	if err := writer.Segments.StoreEvent(ctx, archived); err != nil {
		t.Fatalf("store the archived event: %v", err)
	}
	if err := writer.Store.Commit(ctx, Batch{Puts: []Pair{{Key: "checkpoint:7:1", Value: []byte("cut")}}}); err != nil {
		t.Fatalf("commit the first checkpoint: %v", err)
	}
	if err := writer.Segments.StoreEvent(ctx, hot); err != nil {
		t.Fatalf("store the hot event: %v", err)
	}
	if err := writer.Store.Commit(ctx, Batch{Puts: []Pair{{Key: "checkpoint:7:2", Value: []byte("cut")}}}); err != nil {
		t.Fatalf("commit the second checkpoint: %v", err)
	}
	segments, err := filepath.Glob(filepath.Join(archiveRoot, journalSegmentDirName, "seg-*.log"))
	if err != nil || len(segments) != 1 {
		t.Fatalf("the archive segments = %v err=%v, want one moved interval", segments, err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close the archive writer: %v", err)
	}
	cfg.WriteRights = false
	reader, err := OpenTier(ctx, cfg)
	if err != nil {
		t.Fatalf("reopen the archive reader: %v", err)
	}
	t.Cleanup(func() { reader.Close() })
	kv, ok := reader.Store.(*journalKV)
	if !ok {
		t.Fatalf("the journal tier holds %T, want its journal view", reader.Store)
	}
	stored, held, err := reader.Segments.GetEvent(ctx, archived.EventKey)
	if err != nil || !held || !bytes.Equal(stored.Body, archived.Body) {
		t.Fatalf("the archived event read = (%t, %v), want its committed bytes", held, err)
	}
	archivedBodyKey := segmentBodyKey(indexPosition{partition: archived.PartitionID, window: archived.WindowTS, seq: archived.Seq})
	kv.mu.Lock()
	_, resident := kv.resident.get(archivedBodyKey)
	kv.mu.Unlock()
	reader.Segments.mu.Lock()
	_, cached := reader.Segments.cache.get(archived.EventKey)
	reader.Segments.mu.Unlock()
	if resident || cached {
		t.Fatalf("the archived body stayed warm: resident=%t segment-cache=%t", resident, cached)
	}
	stored, held, err = reader.Segments.GetEvent(ctx, hot.EventKey)
	if err != nil || !held || !bytes.Equal(stored.Body, hot.Body) {
		t.Fatalf("the hot event read = (%t, %v), want its committed bytes", held, err)
	}
	hotBodyKey := segmentBodyKey(indexPosition{partition: hot.PartitionID, window: hot.WindowTS, seq: hot.Seq})
	kv.mu.Lock()
	_, resident = kv.resident.get(hotBodyKey)
	kv.mu.Unlock()
	reader.Segments.mu.Lock()
	_, cached = reader.Segments.cache.get(hot.EventKey)
	reader.Segments.mu.Unlock()
	if !resident || !cached {
		t.Fatalf("the hot body did not stay warm: resident=%t segment-cache=%t", resident, cached)
	}
}

// TestCheckpointSliceReadsArchivedBodyByIdentity restores a checkpoint reference after its original body leaves the hot log.
//   - a real checkpoint slice names an original whose event and reference are sealed into the archive
//   - OpenTier decodes the checkpoint and resolves the slice through the scoped original identity read
//   - no blob key or hot segment copy holds the original, and the returned bytes equal the committed body
func TestCheckpointSliceReadsArchivedBodyByIdentity(t *testing.T) {
	ctx := context.Background()
	root, archiveRoot := t.TempDir(), t.TempDir()
	cfg := StorageConfig{Type: StorageJournal, Root: root, WriteRights: true}
	cfg.ArchiveRoot = archiveRoot
	cfg.HotLogMaxBytes = 1
	writer, err := OpenTier(ctx, cfg)
	if err != nil {
		t.Fatalf("open the archive-backed writer: %v", err)
	}
	const partition = 7
	owner := event.Address{AgentID: "checkpoint-agent", SessionID: "checkpoint-session"}
	originalKey := event.EventID("checkpoint-original")
	originalBody := []byte("first line\nsecond λ line\x00last\n")
	originalRef := OriginalRef{
		Key:             originalKey,
		Owner:           owner,
		Format:          "text/plain",
		RetentionPolicy: "keep",
		Integrity:       Integrity{Digest: digestOf(originalBody), Length: int64(len(originalBody))},
	}
	bodyOf := func(key event.EventID, typ string, content []byte) []byte {
		t.Helper()
		body, err := json.Marshal(struct {
			Key     event.EventID `json:"key"`
			Type    string        `json:"type"`
			Message model.Message `json:"message"`
		}{Key: key, Type: typ, Message: model.Message{Content: string(content)}})
		if err != nil {
			t.Fatalf("encode the body of %s: %v", key, err)
		}
		return body
	}
	commit := func(key event.EventID, typ string, seq, window int64, content []byte) {
		t.Helper()
		outcome, err := CommitFact(ctx, writer, FactCommit{
			Record: SegmentRecord{
				PartitionID: partition,
				WindowTS:    window,
				Seq:         seq,
				EventKey:    key,
				EventTime:   window,
				Body:        bodyOf(key, typ, content),
			}.WithChain(typ, ""),
			Ref:  OriginalRef{Key: key, Owner: owner, Format: "text/plain", RetentionPolicy: "keep"},
			Body: content,
		})
		if err != nil || outcome != FactCommitted {
			t.Fatalf("commit %s = (%v, %v), want FactCommitted", key, outcome, err)
		}
	}
	commit(originalKey, event.TypeExternalInput, 1, 1000, originalBody)
	slicePayload, err := json.Marshal(originalRef)
	if err != nil {
		t.Fatalf("encode the checkpoint's original reference: %v", err)
	}
	commitCheckpoint := func(cut int64) event.EventID {
		t.Helper()
		checkpoint, err := event.CheckpointFor(event.CheckpointOffset{partition: cut}, []event.CheckpointSlice{{
			Name:    "originals",
			Payload: slicePayload,
		}})
		if err != nil {
			t.Fatalf("render checkpoint cut %d: %v", cut, err)
		}
		checkpoint.Key = event.CheckpointKey(partition, cut)
		payload, err := event.CheckpointPayloadOf(checkpoint)
		if err != nil {
			t.Fatalf("dump checkpoint cut %d: %v", cut, err)
		}
		key := event.EventID(checkpoint.Key)
		commit(key, event.TypeCheckpoint, cut+1, 1000+cut, payload)
		return key
	}
	commitCheckpoint(1)
	commit(event.EventID("checkpoint-filler"), event.TypeExternalInput, 3, 1001, []byte("over budget"))
	latestCheckpoint := commitCheckpoint(3)

	manifest, found, err := readArchiveManifest(archiveRoot)
	if err != nil || !found || len(manifest.Segments) != 1 || manifest.Segments[0].SegmentID != journalSegmentLabelOf(1) {
		t.Fatalf("the archive manifest = found=%t segments=%+v err=%v, want only the original's first interval", found, manifest, err)
	}
	hotDir := filepath.Join(root, kvDirName)
	hotSegments := readSealedSegmentsForTest(t, hotDir)
	if len(hotSegments) != 1 || hotSegments[0].id != 2 {
		t.Fatalf("the hot sealed intervals = %+v, want only the newest checkpoint interval", hotSegments)
	}
	archivePath := filepath.Join(archiveRoot, journalSegmentDirName, journalSegmentNameOf(1))
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("stat the archived original interval: %v", err)
	}
	hotPath := filepath.Join(hotDir, journalSegmentDirName, journalSegmentNameOf(1))
	if _, err := os.Stat(hotPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the original's archived interval still has a hot copy: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close the archive writer: %v", err)
	}

	cfg.WriteRights = false
	reader, err := OpenTier(ctx, cfg)
	if err != nil {
		t.Fatalf("reopen the archive reader: %v", err)
	}
	t.Cleanup(func() { reader.Close() })
	checkpointKey, checkpointPartition, held, err := reader.Segments.NewestOfType(ctx, event.TypeCheckpoint)
	if err != nil || !held || checkpointKey != latestCheckpoint || checkpointPartition != partition {
		t.Fatalf("the newest checkpoint = (%s, %d, %t, %v), want %s in partition %d", checkpointKey, checkpointPartition, held, err, latestCheckpoint, partition)
	}
	stored, held, err := reader.Segments.GetEventInPartition(ctx, checkpointKey, checkpointPartition)
	if err != nil || !held {
		t.Fatalf("read the newest checkpoint = (held=%t, %v)", held, err)
	}
	var carried struct {
		Key     event.EventID `json:"key"`
		Type    string        `json:"type"`
		Message model.Message `json:"message"`
	}
	if err := json.Unmarshal(stored.Body, &carried); err != nil {
		t.Fatalf("decode the checkpoint body: %v", err)
	}
	checkpoint, stated := event.CheckpointOf(event.Event{Key: carried.Key, Type: carried.Type, Message: carried.Message})
	if !stated || len(checkpoint.Slices) != 1 || checkpoint.Slices[0].Name != "originals" {
		t.Fatalf("the stored checkpoint = stated=%t slices=%+v, want the one original-reference slice", stated, checkpoint.Slices)
	}
	var sliceRef OriginalRef
	if err := json.Unmarshal(checkpoint.Slices[0].Payload, &sliceRef); err != nil {
		t.Fatalf("decode the checkpoint's original reference: %v", err)
	}
	if sliceRef.Key != originalRef.Key || sliceRef.ReadScope() != originalRef.ReadScope() {
		t.Fatalf("the checkpoint reference = %+v, want identity %s under %+v", sliceRef, originalRef.Key, originalRef.ReadScope())
	}
	blobPairs, err := reader.Store.Read(ctx, Fetch{Key: blobKVKey(originalKey)})
	if err != nil || len(blobPairs) != 0 {
		t.Fatalf("the original's blob entries = (%+v, %v), want no second body copy", blobPairs, err)
	}
	originals, held := reader.Originals.(*kvOriginalStore)
	if !held {
		t.Fatalf("the reopened tier holds originals %T, want the journal original store", reader.Originals)
	}
	recalled := originals.GetScoped(ctx, sliceRef.Key, sliceRef.ReadScope())
	if recalled.Outcome != RecallFound || recalled.Err != nil || !bytes.Equal(recalled.Body, originalBody) {
		t.Fatalf("the checkpoint's archived original = (%v, %v, %d bytes), want (%v, nil, %d bytes)", recalled.Outcome, recalled.Err, len(recalled.Body), RecallFound, len(originalBody))
	}
	loads, _, identityKeys := originals.OriginalsReads()
	if loads != 0 || identityKeys != 2 {
		t.Fatalf("the restored reference paid %d whole-family load(s) and %d identity key read(s), want zero loads and the blob/ref pair", loads, identityKeys)
	}
}

// TestColdEnumerationReadsEachEventOnce pins the cost of a cold enumeration.
// - after the read cache and the resident set are dropped, one full pass reads the index prefix once and each event body once
// - every event keeps its body and its index order
// - the engine stated as the key-value seam alone reaches the enumeration, so the pass is measured on an engine that states no commit order
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestColdEnumerationReadsEachEventOnce(t *testing.T) {
	root := t.TempDir()
	kv, err := openJournalKV(root)
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	counted := &countingKV{inner: kv}
	store, err := newFileSegmentStore(counted)
	if err != nil {
		t.Fatalf("open the segment store: %v", err)
	}
	const events = 24
	records := commitSegmentEvents(t, store, kv, events, 1024)
	dropSegmentCache(store)
	dropJournalResident(kv)
	counted.reads = 0
	got := make([]event.EventID, 0, events)
	if err := store.EachEvent(context.Background(), func(stored StoredEvent) error {
		got = append(got, stored.Key)
		return nil
	}); err != nil {
		t.Fatalf("enumerate the store cold: %v", err)
	}
	if len(got) != events {
		t.Fatalf("enumerated %d events, want %d", len(got), events)
	}
	for at, key := range got {
		if key != records[at].EventKey {
			t.Fatalf("event %d = %s, want %s", at, key, records[at].EventKey)
		}
	}
	if want := events + 1; counted.reads != want {
		t.Fatalf("a cold enumeration read the store %d times for %d events, want %d: one read of the index prefix that places the entries and one read per event body", counted.reads, events, want)
	}
}

// TestSegmentCacheBoundedAndEquivalent pins both halves of the cache contract.
// - committing far more than the budget leaves every event readable
// - a read served cold carries exactly the bytes the commit carried
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestSegmentCacheBoundedAndEquivalent(t *testing.T) {
	root := t.TempDir()
	store, kv := openJournalSegmentStore(t, root)
	const events = 12
	const bodySize = 1 << 20
	records := commitSegmentEvents(t, store, kv, events, bodySize)
	dropSegmentCache(store)
	dropJournalResident(kv)
	for at := range records {
		stored, held, err := store.GetEvent(context.Background(), records[at].EventKey)
		if err != nil || !held {
			t.Fatalf("cold read %d = (held=%t, %v)", at, held, err)
		}
		if !bytes.Equal(stored.Body, segmentBodyOf(records[at], bodySize)) {
			t.Fatalf("cold read %d carries different bytes", at)
		}
	}
}

// TestSegmentLoadLazyPeak pins the lazy startup of a reopened store.
// - the index and count publish without materializing a body
// - the reading is process-level, after a forced collection
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestSegmentLoadLazyPeak(t *testing.T) {
	root := t.TempDir()
	store, kv := openJournalSegmentStore(t, root)
	records := commitSegmentEvents(t, store, kv, 12, 1<<20)
	if err := store.Close(); err != nil {
		t.Fatalf("close the store: %v", err)
	}
	store, kv = nil, nil
	before := heapInUse()
	reopened, reopenedKV := openJournalSegmentStore(t, root)
	after := heapInUse()
	if growth := int64(after) - int64(before); growth > 4<<20 {
		t.Fatalf("lazy startup heap growth = %d bytes over %d MiB of stored bodies, want the index/meta bound plus slack",
			growth, len(records))
	}
	if got := reopened.EventCount(); got != len(records) {
		t.Fatalf("reopened EventCount = %d, want %d", got, len(records))
	}
	if _, held, err := reopened.GetEvent(context.Background(), records[0].EventKey); err != nil || !held {
		t.Fatalf("first cold read after reopen = (held=%t, %v)", held, err)
	}
	dropJournalResident(reopenedKV)
}

// TestColdReadAfterRestart pins the exact read after a restart.
// - the reopened store answers every committed body and position from the persisted entries
// - the new process holds no body at startup
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestColdReadAfterRestart(t *testing.T) {
	root := t.TempDir()
	store, kv := openJournalSegmentStore(t, root)
	records := commitSegmentEvents(t, store, kv, 6, 128<<10)
	if err := store.Close(); err != nil {
		t.Fatalf("close the store: %v", err)
	}
	store, kv = nil, nil
	reopened, _ := openJournalSegmentStore(t, root)
	for _, rec := range records {
		stored, held, err := reopened.GetEvent(context.Background(), rec.EventKey)
		if err != nil || !held {
			t.Fatalf("restart read %s = (held=%t, %v)", rec.EventKey, held, err)
		}
		if !bytes.Equal(stored.Body, segmentBodyOf(rec, 128<<10)) {
			t.Fatalf("restart read %s carries different bytes", rec.EventKey)
		}
	}
}

// TestSegmentLazyLoadPreservesEventCount pins the count a lazy load publishes.
// - the window meta count covers every event whose index and meta arrived
// - a repeat commit adds no second count
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestSegmentLazyLoadPreservesEventCount(t *testing.T) {
	root := t.TempDir()
	store, kv := openJournalSegmentStore(t, root)
	records := commitSegmentEvents(t, store, kv, 6, 64<<10)
	if err := store.Close(); err != nil {
		t.Fatalf("close the store: %v", err)
	}
	reopened, reopenedKV := openJournalSegmentStore(t, root)
	if got := reopened.EventCount(); got != len(records) {
		t.Fatalf("reopened EventCount = %d, want %d", got, len(records))
	}
	if err := reopened.StoreEvent(context.Background(), segmentRecordWithBody(records[0], 64<<10)); err != nil {
		t.Fatalf("repeat commit after lazy load: %v", err)
	}
	if got := reopened.EventCount(); got != len(records) {
		t.Fatalf("count after a repeat commit = %d, want %d", got, len(records))
	}
	dropJournalResident(reopenedKV)
}

// TestSegmentDedupViaIndex pins the deduplication the resident index answers after the body left the cache.
// - a repeat of the same event is a no-op
// - different bytes under the same key are refused
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestSegmentDedupViaIndex(t *testing.T) {
	root := t.TempDir()
	store, kv := openJournalSegmentStore(t, root)
	records := commitSegmentEvents(t, store, kv, 4, 256<<10)
	dropSegmentCache(store)
	dropJournalResident(kv)
	if err := store.StoreEvent(context.Background(), segmentRecordWithBody(records[0], 256<<10)); err != nil {
		t.Fatalf("repeat commit after eviction: %v", err)
	}
	if got := store.EventCount(); got != len(records) {
		t.Fatalf("count after a repeat commit = %d, want %d", got, len(records))
	}
	other := records[0]
	other.Body = append([]byte(nil), segmentBodyOf(records[0], 256<<10)...)
	other.Body[len(other.Body)-1] = 'X'
	if err := store.StoreEvent(context.Background(), other); !errors.Is(err, ErrContentConflict) {
		t.Fatalf("different bytes under one key = %v, want %v", err, ErrContentConflict)
	}
	reopened, reopenedKV := openJournalSegmentStore(t, root)
	if err := reopened.StoreEvent(context.Background(), segmentRecordWithBody(records[0], 256<<10)); err != nil {
		t.Fatalf("repeat commit after restart: %v", err)
	}
	if got := reopened.EventCount(); got != len(records) {
		t.Fatalf("count after a repeat commit on the reopened store = %d, want %d", got, len(records))
	}
	dropJournalResident(reopenedKV)
}

// TestSegmentColdReadFailureSurfaces pins the failure shape of a read whose body is gone.
// - the store names the missing body instead of answering an absent event
// - corruption is never mistaken for a key that was never there
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestSegmentColdReadFailureSurfaces(t *testing.T) {
	root := t.TempDir()
	store, kv := openJournalSegmentStore(t, root)
	records := commitSegmentEvents(t, store, kv, 3, 256<<10)
	dropSegmentCache(store)
	dropJournalResident(kv)
	evtKey := fmt.Sprintf("%d:evt:%d:%d", records[0].PartitionID, records[0].WindowTS, records[0].Seq)
	if err := kv.Commit(context.Background(), Batch{Deletes: []string{evtKey}}); err != nil {
		t.Fatalf("delete the persisted body: %v", err)
	}
	_, held, err := store.GetEvent(context.Background(), records[0].EventKey)
	if held {
		t.Fatal("GetEvent answered held beside a missing body")
	}
	if !errors.Is(err, ErrSegmentBodyMissing) {
		t.Fatalf("GetEvent beside a missing body = %v, want %v", err, ErrSegmentBodyMissing)
	}
}

// TestSegmentLazyLoadPreservesConsumerContract pins the consumer-facing read across a lazy load.
// - the body, the index position, the enumeration order and the count all read the same as before the restart
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestSegmentLazyLoadPreservesConsumerContract(t *testing.T) {
	root := t.TempDir()
	store, kv := openJournalSegmentStore(t, root)
	records := commitSegmentEvents(t, store, kv, 5, 64<<10)
	before, err := store.Events(context.Background())
	if err != nil {
		t.Fatalf("events before restart: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close the store: %v", err)
	}
	reopened, reopenedKV := openJournalSegmentStore(t, root)
	after, err := reopened.Events(context.Background())
	if err != nil {
		t.Fatalf("events after lazy load: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("events after lazy load = %d, want %d", len(after), len(before))
	}
	for at := range before {
		if before[at].Key != after[at].Key || before[at].PartitionID != after[at].PartitionID ||
			before[at].WindowTS != after[at].WindowTS || before[at].Seq != after[at].Seq ||
			!bytes.Equal(before[at].Body, after[at].Body) {
			t.Fatalf("event %d after lazy load differs from the committed one", at)
		}
	}
	if reopened.EventCount() != len(records) {
		t.Fatalf("EventCount after lazy load = %d, want %d", reopened.EventCount(), len(records))
	}
	var streamed []event.EventID
	if err := reopened.EachEvent(context.Background(), func(stored StoredEvent) error {
		streamed = append(streamed, stored.Key)
		return nil
	}); err != nil {
		t.Fatalf("stream events: %v", err)
	}
	if len(streamed) != len(records) {
		t.Fatalf("streamed events = %d, want %d", len(streamed), len(records))
	}
	dropJournalResident(reopenedKV)
}

// BenchmarkSegmentWindowRead measures one event read off a populated segment
// store: the windows and their bodies are committed first, then the read loop
// asks for the event in the middle window by its key.
func BenchmarkSegmentWindowRead(b *testing.B) {
	store, err := newFileSegmentStore(newMemKV())
	if err != nil {
		b.Fatalf("open the segment store: %v", err)
	}
	ctx := context.Background()
	const events = 1024
	for at := 0; at < events; at++ {
		window := int64(at / 16)
		key := event.EventID(fmt.Sprintf("bench-event-%04d", at))
		rec := SegmentRecord{
			PartitionID: 0,
			WindowTS:    window,
			Seq:         int64(at % 16),
			EventKey:    key,
			EventTime:   window,
			Body:        []byte(fmt.Sprintf("%0256d", at)),
		}
		if err := store.StoreEvent(ctx, rec); err != nil {
			b.Fatalf("commit the event %s: %v", key, err)
		}
	}
	key := event.EventID(fmt.Sprintf("bench-event-%04d", events/2))
	b.ResetTimer()
	for at := 0; at < b.N; at++ {
		stored, held, err := store.GetEvent(ctx, key)
		if err != nil || !held || len(stored.Body) == 0 {
			b.Fatalf("read %q = (%t, %v)", key, held, err)
		}
	}
}

// TestNewestOfTypeReadsTheCommitOrderAndCopiesNoIndex pins the newest-of-type read.
//   - the answer is the newest commit, whatever window and slot that commit took
//   - the answered partition is the one that commit landed in, not the highest partition the store holds
//   - the lookup reads the commits filed since the checkpoint, not the history behind it
//   - the lookup allocates the one order chunk it walks and never a copy of the index behind it
//   - control: the same read beside an engine that states no commit order enumerates the whole index and allocates with the history
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestNewestOfTypeReadsTheCommitOrderAndCopiesNoIndex(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, _ := openJournalSegmentStore(t, root)
	earlier := segmentRecord(7, 2000, 4, "checkpoint-old", 32).WithChain(event.TypeCheckpoint, "")
	if err := store.StoreEvent(ctx, earlier); err != nil {
		t.Fatalf("commit the earlier checkpoint: %v", err)
	}
	later := segmentRecord(2, 1000, 1, "checkpoint-new", 32).WithChain(event.TypeCheckpoint, "")
	if err := store.StoreEvent(ctx, later); err != nil {
		t.Fatalf("commit the later checkpoint: %v", err)
	}
	newest, partition, held, err := store.NewestOfType(ctx, event.TypeCheckpoint)
	if err != nil || !held {
		t.Fatalf("read the newest checkpoint = (%t, %v), want the committed one", held, err)
	}
	if newest != later.EventKey {
		t.Fatalf("the newest checkpoint reads as %s, want %s, the identity the later commit filed beside the higher window and slot of %s", newest, later.EventKey, earlier.EventKey)
	}
	if partition != later.PartitionID {
		t.Fatalf("the newest checkpoint reads as partition %d, want %d, the partition its commit landed in beside the %d of %s",
			partition, later.PartitionID, earlier.PartitionID, earlier.EventKey)
	}
	reopened, _ := openJournalSegmentStore(t, root)
	if boot, bootPartition, held, err := reopened.NewestOfType(ctx, event.TypeCheckpoint); err != nil || !held || boot != later.EventKey || bootPartition != later.PartitionID {
		t.Fatalf("the boot over the same log reads the newest checkpoint as (%s, partition %d, %t, %v), want %s in partition %d", boot, bootPartition, held, err, later.EventKey, later.PartitionID)
	}
	if _, _, held, err := reopened.NewestOfType(ctx, "no-such-type"); err != nil || held {
		t.Fatalf("a type the store holds no entry of reads as (%t, %v), want no entry and no error", held, err)
	}

	brief := newestOfTypeCostOf(t, 0)
	long := newestOfTypeCostOf(t, 2000)
	if grown := long.bytes - brief.bytes; grown > journalOrderChunkBytes {
		t.Fatalf("the lookup allocates %d byte(s) per read beside 2000 commits behind the checkpoint beside the %d of none, over the %d one order chunk covers: the walk copies the index behind the checkpoint instead of the chunk it reads",
			long.bytes, brief.bytes, int64(journalOrderChunkBytes))
	}
	if long.nanos > 8*brief.nanos+2_000 {
		t.Fatalf("the lookup costs %d ns beside 2000 commits behind the checkpoint and %d beside none, want the commits since the checkpoint and not the history behind it", long.nanos, brief.nanos)
	}

	copied := newestOfTypeCostOver(t, 2000, func(kv AtomicStore) AtomicStore { return indexOnlyKV{AtomicStore: kv} })
	if copied.bytes <= long.bytes+journalOrderChunkBytes {
		t.Fatalf("the read beside an engine that states no commit order allocates %d byte(s) beside the %d of the walk, want more: the enumeration of the whole index is the copy the bound has to tell apart from the walked chunk",
			copied.bytes, long.bytes)
	}
}

// indexOnlyKV is one engine stated as the key-value seam alone, so a check reads
// the store over an engine that states no commit order.
type indexOnlyKV struct{ AtomicStore }

// newestOfTypeCostOf opens a store carrying the given number of commits behind
// one checkpoint, files the checkpoint and the cadence of commits that follow it, and
// reports what one newest-of-type read beside them allocates and takes.
func newestOfTypeCostOf(t *testing.T, behind int) struct{ bytes, nanos int64 } {
	t.Helper()
	return newestOfTypeCostOver(t, behind, func(kv AtomicStore) AtomicStore { return kv })
}

// newestOfTypeCostOver is newestOfTypeCostOf over the engine wrap answers beside
// the journal view, so a check measures the read over an engine of another shape.
func newestOfTypeCostOver(t *testing.T, behind int, wrap func(AtomicStore) AtomicStore) struct{ bytes, nanos int64 } {
	t.Helper()
	ctx := context.Background()
	kv, err := openJournalKV(t.TempDir())
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	store, err := newFileSegmentStore(wrap(kv))
	if err != nil {
		t.Fatalf("open the segment store: %v", err)
	}
	for at := 0; at < behind; at++ {
		rec := segmentRecord(0, 3000+int64(at/8), int64(at%8), fmt.Sprintf("history-%05d", at), 16)
		if err := store.StoreEvent(ctx, rec); err != nil {
			t.Fatalf("commit history event %d: %v", at, err)
		}
	}
	checkpoint := segmentRecord(2, 1000, 1, "checkpoint", 32).WithChain(event.TypeCheckpoint, "")
	if err := store.StoreEvent(ctx, checkpoint); err != nil {
		t.Fatalf("commit the checkpoint: %v", err)
	}
	for at := 0; at < 4; at++ {
		rec := segmentRecord(0, 4000+int64(at), 1, fmt.Sprintf("tail-%02d", at), 16)
		if err := store.StoreEvent(ctx, rec); err != nil {
			t.Fatalf("commit tail event %d: %v", at, err)
		}
	}
	return struct{ bytes, nanos int64 }{bytes: newestOfTypeAllocBytes(store), nanos: newestOfTypeNanoseconds(store)}
}

// TestEachIndexNewestFirstStreamsTheCommitOrder pins the order the index enumeration states.
//   - the first entry is the newest commit across partitions, so the walk is commit order
//   - a walk that stops early never ranks the entries it did not reach
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestEachIndexNewestFirstStreamsTheCommitOrder(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, _ := openJournalSegmentStore(t, root)
	for _, rec := range []SegmentRecord{
		segmentRecord(9, 5000, 1, "first", 8),
		segmentRecord(1, 1000, 2, "second", 8),
		segmentRecord(9, 4000, 3, "third", 8),
	} {
		if err := store.StoreEvent(ctx, rec); err != nil {
			t.Fatalf("commit %s: %v", rec.EventKey, err)
		}
	}
	reopened, _ := openJournalSegmentStore(t, root)
	var walked []event.EventID
	if err := reopened.EachIndexNewestFirst(ctx, func(key event.EventID, _ int, _ int64, _ string, _ event.SessionID) error {
		walked = append(walked, key)
		return nil
	}); err != nil {
		t.Fatalf("walk the index: %v", err)
	}
	want := []event.EventID{"third", "second", "first"}
	if len(walked) != len(want) {
		t.Fatalf("the walk names %v, want %v", walked, want)
	}
	for at := range want {
		if walked[at] != want[at] {
			t.Fatalf("the walk names %v, want %v", walked, want)
		}
	}
	firsts := 0
	if err := reopened.EachIndexNewestFirst(ctx, func(key event.EventID, _ int, _ int64, _ string, _ event.SessionID) error {
		firsts++
		if firsts == 1 && key != "third" {
			t.Fatalf("the first entry of the walk is %s, want the newest commit", key)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk the index: %v", err)
	}
	if firsts != 3 {
		t.Fatalf("the walk visited %d entries, want every published one", firsts)
	}
}

// TestZeroedOrderWordNeverChangesTheLedgerWalk pins that a damaged order-line word cannot change what a walk from one key answers.
//   - every key's own seek names that key and the entries behind it, and pays the entries it names alone
//   - the words the last commit stated are then set to zero, which the index accepts as legal bytes: no record is unplaceable and no repair path runs
//   - the walk entered there names what the walk from the newest entry names, because a seek is answered only once the walk names the key it was read for
//   - the reading view that discarded it states no word beside the log, so the fallback is the walk such a root takes and never a repair of the index
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestZeroedOrderWordNeverChangesTheLedgerWalk(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writer, _ := openJournalSegmentStore(t, root)
	committed := []event.EventID{"walk-first", "walk-second", "walk-third"}
	for at, key := range committed {
		if err := writer.StoreEvent(ctx, segmentRecord(0, 1000, int64(at+1), string(key), 8)); err != nil {
			t.Fatalf("commit %s: %v", key, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close the writing view: %v", err)
	}
	openReader := func() (*FileSegmentStore, *journalKV) {
		t.Helper()
		kv, err := openJournalView(root, false)
		if err != nil {
			t.Fatalf("open the reading view: %v", err)
		}
		store, err := newFileSegmentStore(kv)
		if err != nil {
			kv.Close()
			t.Fatalf("open the segment store over the reading view: %v", err)
		}
		return store, kv
	}
	walkFrom := func(store *FileSegmentStore, from event.EventID) []event.EventID {
		t.Helper()
		var walked []event.EventID
		if err := store.EachIndexNewestFirstFrom(ctx, from, func(key event.EventID, _ int, _ int64, _ string, _ event.SessionID) (bool, error) {
			walked = append(walked, key)
			return true, nil
		}); err != nil {
			t.Fatalf("walk the index from %s: %v", from, err)
		}
		return walked
	}
	reader, readerView := openReader()
	var whole []event.EventID
	if err := reader.EachIndexNewestFirst(ctx, func(key event.EventID, _ int, _ int64, _ string, _ event.SessionID) error {
		whole = append(whole, key)
		return nil
	}); err != nil {
		t.Fatalf("walk the index: %v", err)
	}
	if len(whole) != len(committed) {
		t.Fatalf("the walk names %v, want the %d committed event(s)", whole, len(committed))
	}
	for _, key := range committed {
		want := []event.EventID(nil)
		for at, walked := range whole {
			if walked == key {
				want = whole[at:]
			}
		}
		visitsBefore, _, _ := reader.IndexWalkReads()
		answer := walkFrom(reader, key)
		visits, _, _ := reader.IndexWalkReads()
		if !slices.Equal(answer, want) {
			t.Fatalf("the walk from %s named %v, want %v: the seek costs the commits after the key and never an entry behind it", key, answer, want)
		}
		if paid := visits - visitsBefore; paid != len(answer) {
			t.Fatalf("the walk from %s paid %d visit(s) for %d entry(ies), want the entries it named alone", key, paid, len(answer))
		}
	}
	if err := readerView.Close(); err != nil {
		t.Fatalf("close the reading view: %v", err)
	}

	linesPath := filepath.Join(root, journalOrderLineFileName)
	words, err := os.ReadFile(linesPath)
	if err != nil {
		t.Fatalf("read the order-line index: %v", err)
	}
	if len(words) == 0 || len(words)%journalOrderLineWordBytes != 0 {
		t.Fatalf("the order-line index states %d byte(s), want whole %d-byte words", len(words), journalOrderLineWordBytes)
	}
	newest := int64(0)
	for at := 0; at < len(words); at += journalOrderLineWordBytes {
		if word := int64(binary.LittleEndian.Uint64(words[at:])); word > newest {
			newest = word
		}
	}
	if newest <= 0 {
		t.Fatal("the order-line index states no seek, want the word of the last commit")
	}
	zeroed := 0
	for at := 0; at < len(words); at += journalOrderLineWordBytes {
		if int64(binary.LittleEndian.Uint64(words[at:])) == newest {
			binary.LittleEndian.PutUint64(words[at:], 0)
			zeroed++
		}
	}
	if err := os.WriteFile(linesPath, words, 0o600); err != nil {
		t.Fatalf("zero the words of the last commit: %v", err)
	}
	t.Logf("order.line: %d of %d word(s) stated %d and now state 0", zeroed, len(words)/journalOrderLineWordBytes, newest)

	before := journalTestRootState(t, root)
	damaged, damagedView := openReader()
	last := committed[len(committed)-1]
	if lineEnd, named, err := damagedView.OrderLineEnd(ctx, fmt.Sprintf("%d:%s%s", 0, segmentIndexPrefix, last)); err != nil || !named || lineEnd != 0 {
		t.Fatalf("the seek of %s = (%d, %v, %v), want the zero the index accepts as a legal byte", last, lineEnd, named, err)
	}
	visitsBefore, _, _ := damaged.IndexWalkReads()
	answer := walkFrom(damaged, last)
	visits, _, _ := damaged.IndexWalkReads()
	if !slices.Equal(answer, whole) {
		t.Fatalf("the walk entered at the zeroed word named %v, want %v: a seek that never names the key it was read for is discarded for the walk from the newest entry", answer, whole)
	}
	if paid := visits - visitsBefore; paid != len(whole) {
		t.Fatalf("the discarded seek paid %d visit(s) for a walk of %d entry(ies), want the walk such a root takes and no second walk of the seek", paid, len(whole))
	}
	if err := damagedView.Close(); err != nil {
		t.Fatalf("close the reading view over the damaged index: %v", err)
	}
	journalTestRequireSameFiles(t, before, journalTestRootState(t, root))
}

// TestEachIndexNewestFirstRanksCarriedCommitsWithoutEngineOrder pins the walk over an engine stating no commit order.
//   - entries rank by the commit sequence each index value carries, across partitions and windows
//   - a visit that answers an error ends the walk where it stands and answers that error
//   - the newest-of-type answer is the newest commit, whatever window and partition it took
//   - a key the engine does not hold is never named
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestEachIndexNewestFirstRanksCarriedCommitsWithoutEngineOrder(t *testing.T) {
	ctx := context.Background()
	kv := newMemKV()
	store, err := newFileSegmentStore(kv)
	if err != nil {
		t.Fatalf("open the segment store: %v", err)
	}
	for _, rec := range []SegmentRecord{
		segmentRecord(9, 5000, 1, "first", 8),
		segmentRecord(1, 1000, 2, "second", 8),
		segmentRecord(9, 4000, 3, "third", 8),
	} {
		if err := store.StoreEvent(ctx, rec); err != nil {
			t.Fatalf("commit %s: %v", rec.EventKey, err)
		}
	}
	walk := func() []event.EventID {
		t.Helper()
		var walked []event.EventID
		if err := store.EachIndexNewestFirst(ctx, func(key event.EventID, _ int, _ int64, _ string, _ event.SessionID) error {
			walked = append(walked, key)
			return nil
		}); err != nil {
			t.Fatalf("walk the index: %v", err)
		}
		return walked
	}
	want := []event.EventID{"third", "second", "first"}
	if walked := walk(); len(walked) != len(want) || walked[0] != want[0] || walked[1] != want[1] || walked[2] != want[2] {
		t.Fatalf("the walk names %v, want %v: the carried commit sequence ranks the entries, not their windows and partitions", walked, want)
	}
	stop := errors.New("stop the walk")
	visits := 0
	err = store.EachIndexNewestFirst(ctx, func(key event.EventID, _ int, _ int64, _ string, _ event.SessionID) error {
		visits++
		return stop
	})
	if !errors.Is(err, stop) || visits != 1 {
		t.Fatalf("the stopped walk answered (%v, %d visits), want the visit's error after one visit", err, visits)
	}
	older := segmentRecord(9, 9000, 1, "checkpoint-old", 8).WithChain(event.TypeCheckpoint, "")
	newer := segmentRecord(1, 500, 1, "checkpoint-new", 8).WithChain(event.TypeCheckpoint, "")
	for _, rec := range []SegmentRecord{older, newer} {
		if err := store.StoreEvent(ctx, rec); err != nil {
			t.Fatalf("commit %s: %v", rec.EventKey, err)
		}
	}
	newest, _, held, err := store.NewestOfType(ctx, event.TypeCheckpoint)
	if err != nil || !held || newest != newer.EventKey {
		t.Fatalf("the newest checkpoint reads as (%s, %t, %v), want %s, the later commit beside the lower window", newest, held, err, newer.EventKey)
	}
	if err := kv.Commit(ctx, Batch{Deletes: []string{"9:idx:third"}}); err != nil {
		t.Fatalf("drop the index entry of third: %v", err)
	}
	want = []event.EventID{"checkpoint-new", "checkpoint-old", "second", "first"}
	if walked := walk(); len(walked) != len(want) || walked[0] != want[0] || walked[1] != want[1] || walked[2] != want[2] || walked[3] != want[3] {
		t.Fatalf("the walk names %v, want %v: a key the engine no longer holds is never named", walked, want)
	}
}

// TestEachIndexNewestFirstFallbackMaterializesTheWholeIndex pins the cost of the walk over an engine stating no commit order.
//   - a walk that stops at its first visit still reads every published index entry: the fallback materializes the whole index before it ranks
//   - the read volume grows linearly with the depth: ten times the commits answer ten times the reads, the intrinsic O(history) of the key layout
//   - the first visit names the newest commit, so the measured walk answers the order it declares
//   - control: an engine answering a bounded prefix reads fewer entries than the depth, so the full-materialization assertion can fail
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestEachIndexNewestFirstFallbackMaterializesTheWholeIndex(t *testing.T) {
	ctx := context.Background()
	stop := errors.New("stop the walk")
	measure := func(depth int) (reads int, first event.EventID, visits int) {
		t.Helper()
		log := &seatReads{inner: newMemKV(), keyReads: map[string]int{}}
		store, err := newFileSegmentStore(log)
		if err != nil {
			t.Fatalf("open the segment store: %v", err)
		}
		for at := 0; at < depth; at++ {
			rec := segmentRecord(0, int64(at/8), int64(at%8), fmt.Sprintf("fallback-%05d", at), 16)
			if err := store.StoreEvent(ctx, rec); err != nil {
				t.Fatalf("commit event %d: %v", at, err)
			}
		}
		before := log.entries
		err = store.EachIndexNewestFirst(ctx, func(key event.EventID, _ int, _ int64, _ string, _ event.SessionID) error {
			visits++
			first = key
			return stop
		})
		if !errors.Is(err, stop) {
			t.Fatalf("the stopped walk answered %v, want the visit's stop", err)
		}
		return log.entries - before, first, visits
	}
	shallow, deep := 200, 2000
	shallowReads, shallowFirst, shallowVisits := measure(shallow)
	deepReads, deepFirst, deepVisits := measure(deep)
	if shallowReads < shallow || deepReads < deep {
		t.Fatalf("the walk stopped at its first visit read (%d, %d) entries at depths (%d, %d), want every published index entry at both depths: the fallback materializes the whole index before it ranks, so the stop never bounds the reads", shallowReads, deepReads, shallow, deep)
	}
	if deepReads < 9*shallowReads || deepReads > 11*shallowReads {
		t.Fatalf("the reads grew from %d at %d commits to %d at %d commits, want ten times within the linear band: the materialization is the intrinsic O(history) of the key layout", shallowReads, shallow, deepReads, deep)
	}
	if shallowVisits != 1 || deepVisits != 1 {
		t.Fatalf("the stopped walk visited (%d, %d) entries at depths (%d, %d), want one visit each: the stop bounds the visits", shallowVisits, deepVisits, shallow, deep)
	}
	if shallowFirst != "fallback-00199" || deepFirst != "fallback-01999" {
		t.Fatalf("the first visit named (%s, %s), want the newest commit at each depth", shallowFirst, deepFirst)
	}
	t.Logf("the fallback walk stopped at its first visit read %d entries beside %d commits and %d beside %d", shallowReads, shallow, deepReads, deep)
}

// newestOfTypeAllocBytes reports the bytes one newest-of-type read allocates
// beside the history the store holds, so a check measures the copy a whole-index
// walk would make.
func newestOfTypeAllocBytes(store *FileSegmentStore) int64 {
	ctx := context.Background()
	measured := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for at := 0; at < b.N; at++ {
			if _, _, _, err := store.NewestOfType(ctx, event.TypeCheckpoint); err != nil {
				b.Fatalf("read the newest checkpoint: %v", err)
			}
		}
	})
	return measured.AllocedBytesPerOp()
}

// newestOfTypeNanoseconds reports the time one newest-of-type read takes beside
// the history the store holds, so a check measures the entries a whole-index walk
// would visit.
func newestOfTypeNanoseconds(store *FileSegmentStore) int64 {
	ctx := context.Background()
	measured := testing.Benchmark(func(b *testing.B) {
		for at := 0; at < b.N; at++ {
			if _, _, _, err := store.NewestOfType(ctx, event.TypeCheckpoint); err != nil {
				b.Fatalf("read the newest checkpoint: %v", err)
			}
		}
	})
	return measured.NsPerOp()
}

// seatReads counts what one store open read from the engine it opened over, so a check states the cost of the answer instead of inferring it from a wall clock.
//   - keyReads counts the single-key reads by the key each one named
//   - entries counts the pairs the prefix reads answered beside the keys the key enumerations answered, which is the history a reader of the entries pays
type seatReads struct {
	inner    AtomicStore
	mu       sync.Mutex
	keyReads map[string]int
	entries  int
}

// Read forwards one read and records what it cost.
func (r *seatReads) Read(ctx context.Context, fetch Fetch) ([]Pair, error) {
	pairs, err := r.inner.Read(ctx, fetch)
	r.mu.Lock()
	defer r.mu.Unlock()
	if fetch.Key != "" {
		r.keyReads[fetch.Key]++
		return pairs, err
	}
	r.entries += len(pairs)
	return pairs, err
}

// Keys forwards one key enumeration and records the keys it answered.
func (r *seatReads) Keys(ctx context.Context, prefix string) ([]string, error) {
	keys, err := r.inner.Keys(ctx, prefix)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries += len(keys)
	return keys, err
}

// Namespaces forwards the namespace enumeration.
func (r *seatReads) Namespaces(ctx context.Context) ([]string, error) {
	return r.inner.Namespaces(ctx)
}

// Commit forwards one batch.
func (r *seatReads) Commit(ctx context.Context, batch Batch) error {
	return r.inner.Commit(ctx, batch)
}

// seatReadsOf answers the reads one open of the store at root made, beside the store it published.
func seatReadsOf(t *testing.T, root string) (*FileSegmentStore, *seatReads) {
	t.Helper()
	kv, err := openJournalKV(root)
	if err != nil {
		t.Fatalf("open the journal view of %s: %v", root, err)
	}
	log := &seatReads{inner: kv, keyReads: map[string]int{}}
	store, err := newFileSegmentStore(log)
	if err != nil {
		t.Fatalf("open the segment store of %s: %v", root, err)
	}
	return store, log
}

// writeRawSeat states one seat value in the chain of root, which is the write an external tool makes and the store never makes over a root it is reading.
func writeRawSeat(t *testing.T, root string, value []byte) {
	t.Helper()
	kv, err := openJournalKV(root)
	if err != nil {
		t.Fatalf("open the journal view of %s: %v", root, err)
	}
	if err := kv.Commit(context.Background(), Batch{Puts: []Pair{{Key: storeSeatKey, Value: value}}}); err != nil {
		t.Fatalf("state the seat of %s: %v", root, err)
	}
	if err := kv.Close(); err != nil {
		t.Fatalf("close the journal view of %s: %v", root, err)
	}
}

// readRawSeat answers the seat value the chain of root states.
func readRawSeat(t *testing.T, root string) ([]byte, bool) {
	t.Helper()
	kv, err := openJournalKV(root)
	if err != nil {
		t.Fatalf("open the journal view of %s: %v", root, err)
	}
	defer kv.Close()
	pairs, err := kv.Read(context.Background(), Fetch{Key: storeSeatKey})
	if err != nil {
		t.Fatalf("read the seat of %s: %v", root, err)
	}
	if len(pairs) == 0 {
		return nil, false
	}
	return pairs[0].Value, true
}

// dropRawSeat deletes the seat of root, which is the chain a store reads beside no seat at all.
func dropRawSeat(t *testing.T, root string) {
	t.Helper()
	kv, err := openJournalKV(root)
	if err != nil {
		t.Fatalf("open the journal view of %s: %v", root, err)
	}
	if err := kv.Commit(context.Background(), Batch{Deletes: []string{storeSeatKey}}); err != nil {
		t.Fatalf("drop the seat of %s: %v", root, err)
	}
	if err := kv.Close(); err != nil {
		t.Fatalf("close the journal view of %s: %v", root, err)
	}
}

// TestStoreSeatIsReadWithOnePointReadAtOpen states that a store answers its count from one read of the seat key.
//   - the seat is one key, so the store reads it with one single-key read and never by enumerating the entries behind the count
//   - the count the store answers is the count the seat states, so the seat is the answer and not a check beside a count the open derived itself
//   - the open reads no index entry at either depth, because the seat and the partition set it reads are one key each
//   - control: an open that derives the count instead of reading the seat pays the entries, so its wall clock grows with the chain
//   - control: a store whose seat states nothing answers the count its entries state, so the seated answer above is a decision the chain can fail
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestStoreSeatIsReadWithOnePointReadAtOpen(t *testing.T) {
	depths := []int{40, 160, 640}
	seatKeysRead := make([]int, 0, len(depths))
	entriesRead := make([]int, 0, len(depths))
	for _, depth := range depths {
		root := t.TempDir()
		kv, err := openJournalKV(root)
		if err != nil {
			t.Fatalf("open the journal view: %v", err)
		}
		filled, err := newFileSegmentStore(kv)
		if err != nil {
			t.Fatalf("open the segment store: %v", err)
		}
		commitSegmentEvents(t, filled, kv, depth, 64)
		if err := filled.Close(); err != nil {
			t.Fatalf("close the filled store: %v", err)
		}

		store, log := seatReadsOf(t, root)
		if got := log.keyReads[storeSeatKey]; got != 1 {
			t.Fatalf("the open of a %d-event chain read the seat %d time(s), want the one read the seat is filed under", depth, got)
		}
		if got := store.EventCount(); got != depth {
			t.Fatalf("the open of a %d-event chain answered %d event(s), want the count its seat states", depth, got)
		}
		seatKeysRead = append(seatKeysRead, log.keyReads[storeSeatKey])
		entriesRead = append(entriesRead, store.IndexEntriesRead())
		if err := store.Close(); err != nil {
			t.Fatalf("close the seated store: %v", err)
		}
	}
	if want := 1; seatKeysRead[0] != want || seatKeysRead[len(seatKeysRead)-1] != want {
		t.Fatalf("the seat cost %v read(s) over the depths %v, want %d at every depth", seatKeysRead, depths, want)
	}
	for at, depth := range depths {
		if entriesRead[at] != 0 {
			t.Fatalf("the open of a %d-event chain enumerated %d index entr(ies), want none: the seat states the count, the commit sequence and the partitions in one key, whatever the history", depth, entriesRead[at])
		}
	}

	t.Run("the_count_the_seat_states_is_the_count_the_store_answers", func(t *testing.T) {
		root := t.TempDir()
		kv, err := openJournalKV(root)
		if err != nil {
			t.Fatalf("open the journal view: %v", err)
		}
		filled, err := newFileSegmentStore(kv)
		if err != nil {
			t.Fatalf("open the segment store: %v", err)
		}
		commitSegmentEvents(t, filled, kv, 3, 64)
		if err := filled.Close(); err != nil {
			t.Fatalf("close the filled store: %v", err)
		}
		stated, err := json.Marshal(storeSeat{Count: 7, CommitSeq: 7, Partitions: []int{0}})
		if err != nil {
			t.Fatalf("encode the seat the chain states: %v", err)
		}
		writeRawSeat(t, root, stated)
		store, _ := seatReadsOf(t, root)
		defer store.Close()
		if got := store.EventCount(); got != 7 {
			t.Fatalf("the open beside a seat stating 7 over three committed entries answered %d, want the seat to be the count", got)
		}
		if got := store.IndexEntriesRead(); got != 0 {
			t.Fatalf("the open beside a seat stating 7 enumerated %d index entr(ies), want none: the seat is the count and it states the partitions a read probes", got)
		}
	})

	t.Run("a_chain_that_states_no_seat_answers_the_count_its_entries_state", func(t *testing.T) {
		root := t.TempDir()
		kv, err := openJournalKV(root)
		if err != nil {
			t.Fatalf("open the journal view: %v", err)
		}
		filled, err := newFileSegmentStore(kv)
		if err != nil {
			t.Fatalf("open the segment store: %v", err)
		}
		commitSegmentEvents(t, filled, kv, 5, 64)
		if err := filled.Close(); err != nil {
			t.Fatalf("close the filled store: %v", err)
		}
		dropper, err := openJournalKV(root)
		if err != nil {
			t.Fatalf("open the journal view: %v", err)
		}
		if err := dropper.Commit(context.Background(), Batch{Deletes: []string{storeSeatKey}}); err != nil {
			t.Fatalf("drop the seat of the chain: %v", err)
		}
		if err := dropper.Close(); err != nil {
			t.Fatalf("close the journal view: %v", err)
		}
		store, _ := seatReadsOf(t, root)
		defer store.Close()
		if got := store.EventCount(); got != 5 {
			t.Fatalf("the open of a chain that states no seat answered %d event(s), want the 5 its entries state", got)
		}
	})

	t.Run("the_aggregate_a_derived_count_pays_grows_with_the_chain", func(t *testing.T) {
		depths := []int{100, 800, 3200}
		paid := make([]time.Duration, 0, len(depths))
		enumerated := make([]int, 0, len(depths))
		for _, depth := range depths {
			root := t.TempDir()
			kv, err := openJournalKV(root)
			if err != nil {
				t.Fatalf("open the journal view: %v", err)
			}
			filled, err := newFileSegmentStore(kv)
			if err != nil {
				t.Fatalf("open the segment store: %v", err)
			}
			commitSegmentEvents(t, filled, kv, depth, 64)
			if err := filled.Close(); err != nil {
				t.Fatalf("close the filled store: %v", err)
			}
			dropRawSeat(t, root)
			var fastest time.Duration
			var entries int
			for at := 0; at < 3; at++ {
				view, err := openJournalKV(root)
				if err != nil {
					t.Fatalf("open the journal view: %v", err)
				}
				reads := &seatReads{inner: view, keyReads: map[string]int{}}
				started := time.Now()
				store, err := newFileSegmentStore(reads)
				spent := time.Since(started)
				if err != nil {
					t.Fatalf("open the store of a %d-event chain beside no seat: %v", depth, err)
				}
				if at == 0 || spent < fastest {
					fastest = spent
				}
				entries = store.IndexEntriesRead()
				if got := store.EventCount(); got != depth {
					t.Fatalf("the open beside no seat answered %d event(s), want the %d its entries state", got, depth)
				}
				if err := store.Close(); err != nil {
					t.Fatalf("close the store: %v", err)
				}
			}
			paid = append(paid, fastest)
			enumerated = append(enumerated, entries)
		}
		t.Logf("the open that derives the count paid %v over the depths %v", paid, depths)
		if shallow, deep := paid[0], paid[len(paid)-1]; deep < 2*shallow {
			t.Fatalf("the open that derives the count paid %v over the depths %v, want the deepest to cost more than twice the shallowest",
				paid, depths)
		}
		if shallow, deep := enumerated[0], enumerated[len(enumerated)-1]; deep <= shallow {
			t.Fatalf("the open that derives the count enumerated %v entr(ies) over the depths %v, want them growing with the chain so the zero a seated open pays is a bound the chain can fail",
				enumerated, depths)
		}
	})
}

// TestACorruptStoreSeatRederivesFromTheChain states the rule a seat this build cannot read takes.
//   - a seat that does not decode answers the count the entries state, never a zero the store would then report as an empty history
//   - the entries stay readable by identity, so the fallback restores the whole store and not only its count
//   - a reader beside the unreadable seat states the seat it found, because a view holding no write rights states no key of the chain it reads
//   - control: the entries the rederivation reads grow with the chain, so the count it answers is not a flat reading beside a chain it skipped
//   - the next commit states a seat in the frame it lands, and a fresh open of the chain answers the count that commit published
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestACorruptStoreSeatRederivesFromTheChain(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	kv, err := openJournalKV(root)
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	filled, err := newFileSegmentStore(kv)
	if err != nil {
		t.Fatalf("open the segment store: %v", err)
	}
	records := commitSegmentEvents(t, filled, kv, 4, 64)
	if err := filled.Close(); err != nil {
		t.Fatalf("close the filled store: %v", err)
	}
	corrupt := []byte(`{"count":`)
	writeRawSeat(t, root, corrupt)

	store, _ := seatReadsOf(t, root)
	if got := store.EventCount(); got != len(records) {
		t.Fatalf("the open beside an unreadable seat answered %d event(s), want the %d its entries state", got, len(records))
	}
	for _, rec := range records {
		stored, held, err := store.GetEvent(ctx, rec.EventKey)
		if err != nil {
			t.Fatalf("read the event %s beside the unreadable seat: %v", rec.EventKey, err)
		}
		if !held || !bytes.Equal(stored.Body, segmentBodyOf(rec, 64)) {
			t.Fatalf("the event %s beside the unreadable seat = (%q, %v), want the bytes it was committed with", rec.EventKey, stored.Body, held)
		}
	}
	if got, held := readRawSeat(t, root); !held || !bytes.Equal(got, corrupt) {
		t.Fatalf("the reader stated the seat of the chain as %q, want the %q it read", got, corrupt)
	}

	next := segmentRecord(0, 2000, 1, "event-after-the-unreadable-seat", 64)
	if err := store.StoreEvent(ctx, next); err != nil {
		t.Fatalf("commit beside the unreadable seat: %v", err)
	}
	if got := store.EventCount(); got != len(records)+1 {
		t.Fatalf("the store answered %d event(s) after the commit beside the unreadable seat, want %d", got, len(records)+1)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close the store: %v", err)
	}
	reopened, _ := seatReadsOf(t, root)
	defer reopened.Close()
	if got := reopened.EventCount(); got != len(records)+1 {
		t.Fatalf("a fresh open of the chain answered %d event(s), want the %d the commit stated in its frame", got, len(records)+1)
	}

	t.Run("the_rederivation_reads_the_chain_it_answers_from", func(t *testing.T) {
		depths := []int{40, 160, 640}
		read := make([]int, 0, len(depths))
		for _, depth := range depths {
			chainRoot := t.TempDir()
			kv, err := openJournalKV(chainRoot)
			if err != nil {
				t.Fatalf("open the journal view: %v", err)
			}
			filled, err := newFileSegmentStore(kv)
			if err != nil {
				t.Fatalf("open the segment store: %v", err)
			}
			commitSegmentEvents(t, filled, kv, depth, 64)
			if err := filled.Close(); err != nil {
				t.Fatalf("close the filled store: %v", err)
			}
			writeRawSeat(t, chainRoot, []byte(`{"count":`))
			store, log := seatReadsOf(t, chainRoot)
			if got := store.EventCount(); got != depth {
				t.Fatalf("the open beside an unreadable seat on a %d-event chain answered %d, want the count its entries state", depth, got)
			}
			read = append(read, log.entries)
			if err := store.Close(); err != nil {
				t.Fatalf("close the store: %v", err)
			}
		}
		t.Logf("the rederivation read %v entries over the depths %v", read, depths)
		if !(read[0] < read[len(read)-1]) {
			t.Fatalf("the rederivation read %v entries over the depths %v, want them growing so the count comes from the chain",
				read, depths)
		}
	})
}

// TestASeatBelowItsCountRederivesTheRank states the rule a seat whose commit sequence is below its count takes.
//   - a seat stating a commit sequence below its count is refused, and the count the entries state is the count the store answers
//   - the rank the next commit takes is past every rank the entries state, which is the collision the refused seat would have caused
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestASeatBelowItsCountRederivesTheRank(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	kv, err := openJournalKV(root)
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	filled, err := newFileSegmentStore(kv)
	if err != nil {
		t.Fatalf("open the segment store: %v", err)
	}
	records := commitSegmentEvents(t, filled, kv, 4, 64)
	if err := filled.Close(); err != nil {
		t.Fatalf("close the filled store: %v", err)
	}
	stated, err := json.Marshal(storeSeat{Count: 9, CommitSeq: 2})
	if err != nil {
		t.Fatalf("encode the seat the chain states: %v", err)
	}
	writeRawSeat(t, root, stated)

	store, _ := seatReadsOf(t, root)
	defer store.Close()
	if got := store.EventCount(); got != len(records) {
		t.Fatalf("the open beside a seat stating a commit below its count answered %d event(s), want the %d its entries state", got, len(records))
	}
	highest := int64(0)
	for _, rec := range records {
		position, held, err := store.publishedPositionLocked(ctx, rec.EventKey)
		if err != nil {
			t.Fatalf("read the position of %s: %v", rec.EventKey, err)
		}
		if !held {
			t.Fatalf("the store published no position for %s, want every entry its chain states", rec.EventKey)
		}
		if position.commit > highest {
			highest = position.commit
		}
	}
	if err := store.StoreEvent(ctx, segmentRecord(0, 2000, 1, "event-past-the-refused-seat", 64)); err != nil {
		t.Fatalf("commit past the refused seat: %v", err)
	}
	position, held, err := store.publishedPositionLocked(ctx, "event-past-the-refused-seat")
	if err != nil {
		t.Fatalf("read the position past the refused seat: %v", err)
	}
	if !held {
		t.Fatalf("the store published no position for the commit past the refused seat")
	}
	if position.commit <= highest {
		t.Fatalf("the commit past the refused seat took the rank %d beside the %d the entries state, want a rank past every one of them", position.commit, highest)
	}
}

// mustFinishWithin runs one read and fails the check when it does not return in time.
// The bound is the only signal a wait on a lock gives, because a walk that holds the
// store against the read its own visit makes waits forever.
func mustFinishWithin(t *testing.T, within time.Duration, what string, run func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- run() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	case <-time.After(within):
		t.Fatalf("%s did not return within %s: the walk and the read it makes wait on each other", what, within)
	}
}

// TestIndexWalkReadsABodyBesideAConcurrentPointRead pins what one index walk does to the store it walks.
//   - a visit that reads one event body answers it beside the walk that names every entry
//   - the walk completes beside a filer reading and committing through the same store
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestIndexWalkReadsABodyBesideAConcurrentPointRead(t *testing.T) {
	ctx := context.Background()
	store, _ := openJournalSegmentStore(t, t.TempDir())
	const entries = 8
	for at := 0; at < entries; at++ {
		rec := segmentRecord(0, 1000+int64(at), 1, fmt.Sprintf("event-%02d", at), 32)
		if err := store.StoreEvent(ctx, rec); err != nil {
			t.Fatalf("commit event %d: %v", at, err)
		}
	}
	dropSegmentCache(store)
	walkReadingBodies := func() error {
		read := 0
		err := store.EachIndexNewestFirst(ctx, func(key event.EventID, _ int, _ int64, _ string, _ event.SessionID) error {
			stored, held, err := store.GetEvent(ctx, key)
			if err != nil {
				return err
			}
			if !held || len(stored.Body) == 0 {
				return fmt.Errorf("the walk named %s beside no body", key)
			}
			read++
			return nil
		})
		if err != nil {
			return err
		}
		if read != entries {
			return fmt.Errorf("the walk read %d bod(ies), want the %d entries it names", read, entries)
		}
		return nil
	}
	mustFinishWithin(t, 15*time.Second, "the index walk reading a body of every entry it names", walkReadingBodies)
	filed := make(chan error, 1)
	go func() {
		for at := 0; at < 200; at++ {
			if _, _, err := store.GetEvent(ctx, event.EventID(fmt.Sprintf("event-%02d", at%entries))); err != nil {
				filed <- err
				return
			}
			rec := segmentRecord(0, 3000+int64(at%4), 1, fmt.Sprintf("filed-%03d", at), 16)
			if err := store.StoreEvent(ctx, rec); err != nil {
				filed <- err
				return
			}
		}
		filed <- nil
	}()
	mustFinishWithin(t, 20*time.Second, "the index walk reading bodies beside a checkpoint filer", walkReadingBodies)
	select {
	case err := <-filed:
		if err != nil {
			t.Fatalf("read and commit beside the walk: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatalf("the filer did not return within 20s: the walk and the commit it makes wait on each other")
	}
}

// 契约: docs/wiki/memory/memory-architecture.md#overview
// factProbeEngine is the atomic store seam a fixture wraps: it records the key
// set of every batch, refuses the batches the fixture names before the engine
// sees them, and reports the ones it names after the engine committed them.
type factProbeEngine struct {
	AtomicStore
	mu      sync.Mutex
	batches [][]string
	// refuse answers a batch before the engine commits it.
	refuse func(Batch) error
	// report answers a batch after the engine committed it.
	report func(Batch) error
}

// Commit records the batch it is handed and answers what the fixture named.
func (p *factProbeEngine) Commit(ctx context.Context, batch Batch) error {
	keys := make([]string, 0, len(batch.Puts))
	for _, put := range batch.Puts {
		keys = append(keys, put.Key)
	}
	p.mu.Lock()
	p.batches = append(p.batches, keys)
	refuse, report := p.refuse, p.report
	p.mu.Unlock()
	carries := len(batch.Puts) > 0 || len(batch.Deletes) > 0
	if carries && refuse != nil {
		if err := refuse(batch); err != nil {
			return err
		}
	}
	if err := p.AtomicStore.Commit(ctx, batch); err != nil {
		return err
	}
	if carries && report != nil {
		return report(batch)
	}
	return nil
}

// Close releases the engine the probe wraps, so the storage root's write rights
// leave with the view that holds them.
func (p *factProbeEngine) Close() error {
	if closer, held := p.AtomicStore.(interface{ Close() error }); held {
		return closer.Close()
	}
	return nil
}

// written answers the recorded batches that carry key.
func (p *factProbeEngine) written(key string) [][]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out [][]string
	for _, batch := range p.batches {
		for _, name := range batch {
			if name == key {
				out = append(out, batch)
				break
			}
		}
	}
	return out
}

// factProbeRoot opens one journal tier over root through a probe the fixture
// shapes, so a test reads the key sets one fact commits.
func factProbeRoot(t *testing.T, ctx context.Context, root string, probe *factProbeEngine) *Tier {
	t.Helper()
	engine, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the engine of the root: %v", err)
	}
	probe.AtomicStore = engine
	tier, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: root, Engine: probe})
	if err != nil {
		t.Fatalf("open the tier of the root: %v", err)
	}
	return tier
}

// factProbeWindow is the window every fact of these fixtures is placed in.
const factProbeWindow = 1700000000000

// factProbeEventBody answers the event body of one probe fact: the body shape a
// fact commit lands, carrying the original text as the content of its message, so
// a read of the original answers the bytes the same commit named.
func factProbeEventBody(content []byte) []byte {
	quoted, err := json.Marshal(string(content))
	if err != nil {
		panic(err)
	}
	return append(append([]byte(`{"message":{"content":`), quoted...), "}}"...)
}

// factProbeFact names one fact of partition 1 at the first slot of the probe
// window, carrying the given original bytes.
func factProbeFact(content []byte) FactCommit {
	key := event.EventID("whole-fact")
	return FactCommit{
		Record: SegmentRecord{
			PartitionID: 1,
			WindowTS:    factProbeWindow,
			Seq:         1,
			EventKey:    key,
			EventTime:   factProbeWindow,
			Body:        factProbeEventBody(content),
		}.WithChain("fact", "probe"),
		Ref: OriginalRef{
			Key:             key,
			Owner:           event.Address{AgentID: "probe", SessionID: "probe"},
			Format:          "text/plain",
			RetentionPolicy: "keep",
		},
		Body: content,
	}
}

// factProbeEventKey names the event entry of the fact one probe commits.
func factProbeEventKey() string {
	return fmt.Sprintf("1:evt:%d:1", factProbeWindow)
}

// TestOneBatchCarriesAWholeFact pins the commit unit of one fact: every key family lands in one commit and the reopened store answers all of them.
//   - the batch carries the event body, its index entry, the window meta and the reference that names the original the same event carries
//   - a successful commit takes one sequence slot and one event count, in the committing view and in the reopened one
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestOneBatchCarriesAWholeFact(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	probe := &factProbeEngine{}
	tier := factProbeRoot(t, ctx, root, probe)
	content := []byte(strings.Repeat("a", 20480))
	fact := factProbeFact(content)
	outcome, err := CommitFact(ctx, tier, fact)
	if err != nil || outcome != FactCommitted {
		t.Fatalf("commit one fact = %v, %v, want the committed outcome", outcome, err)
	}
	batches := probe.written(factProbeEventKey())
	if len(batches) != 1 {
		t.Fatalf("the fact landed in %d commits, want one", len(batches))
	}
	want := []string{
		factProbeEventKey(),
		"1:idx:whole-fact",
		fmt.Sprintf("1:meta:%d", factProbeWindow),
		"ref:whole-fact",
		"store:seat",
	}
	if len(batches[0]) != len(want) {
		t.Fatalf("the one commit carries %v, want the %d key families %v", batches[0], len(want), want)
	}
	for _, name := range want {
		if !slices.Contains(batches[0], name) {
			t.Fatalf("the one commit carries %v, want %s", batches[0], name)
		}
	}
	if got := tier.Segments.EventCount(); got != 1 {
		t.Fatalf("the committing view counts %d event(s), want the one it landed", got)
	}
	if err := tier.Close(); err != nil {
		t.Fatalf("close the tier: %v", err)
	}
	reopened, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("reopen the root: %v", err)
	}
	defer reopened.Close()
	stored, held, err := reopened.Segments.GetEvent(ctx, fact.Record.EventKey)
	if err != nil || !held {
		t.Fatalf("the reopened event = held %t, %v, want the record it committed", held, err)
	}
	if !bytes.Equal(stored.Body, fact.Record.Body) {
		t.Fatalf("the reopened record carries %d byte(s), want the %d it committed", len(stored.Body), len(fact.Record.Body))
	}
	if stored.PartitionID != 1 || stored.WindowTS != factProbeWindow || stored.Seq != 1 {
		t.Fatalf("the reopened slot = {%d %d %d}, want the one it committed", stored.PartitionID, stored.WindowTS, stored.Seq)
	}
	if got := reopened.Segments.EventCount(); got != 1 {
		t.Fatalf("the reopened view counts %d event(s), want the one the commit landed", got)
	}
	original := reopened.Originals.Get(ctx, fact.Ref.Key)
	if original.Outcome != RecallFound || !bytes.Equal(original.Body, content) {
		t.Fatalf("the reopened original = %v over %d byte(s), want the %d it committed", original.Outcome, len(original.Body), len(content))
	}
}

// TestFactCommitAnswersNewExistingConflictUncertain pins the four outcomes of one whole-fact commit and what each leaves behind.
//   - a first commit lands the fact, a repeat moves no key and takes no slot, and other content under the identity moves no key
//   - a commit whose barrier could not name its outcome leaves the committing view at absence beside a batch that landed whole
//   - only an open that took no part in that commit names what the root holds
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestFactCommitAnswersNewExistingConflictUncertain(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	probe := &factProbeEngine{}
	tier := factProbeRoot(t, ctx, root, probe)
	fact := factProbeFact([]byte("the fact the identity names"))
	if outcome, err := CommitFact(ctx, tier, fact); err != nil || outcome != FactCommitted {
		t.Fatalf("the first commit = %v, %v, want the committed outcome", outcome, err)
	}
	if outcome, err := CommitFact(ctx, tier, fact); err != nil || outcome != FactAlreadyPresent {
		t.Fatalf("the repeated commit = %v, %v, want the fact the store already holds", outcome, err)
	}
	if got := len(probe.written(factProbeEventKey())); got != 1 {
		t.Fatalf("the repeated commit landed %d record(s), want the one the first commit landed", got)
	}
	if got := tier.Segments.EventCount(); got != 1 {
		t.Fatalf("the repeated commit left the count at %d, want the one sequence slot", got)
	}
	settled, err := tier.Store.Keys(ctx, "")
	if err != nil {
		t.Fatalf("read the keys of the store: %v", err)
	}
	conflicting := factProbeFact([]byte("another content under the one identity"))
	outcome, err := CommitFact(ctx, tier, conflicting)
	if !errors.Is(err, ErrContentConflict) || outcome != FactConflict {
		t.Fatalf("the conflicting commit = %v, %v, want the byte conflict the engine names", outcome, err)
	}
	after, err := tier.Store.Keys(ctx, "")
	if err != nil {
		t.Fatalf("read the keys of the store: %v", err)
	}
	if strings.Join(after, ",") != strings.Join(settled, ",") {
		t.Fatalf("the conflicting commit left %v, want the %v it found", after, settled)
	}
	if got := tier.Segments.EventCount(); got != 1 {
		t.Fatalf("the conflicting commit left the count at %d, want the one sequence slot", got)
	}

	uncertainRoot := t.TempDir()
	unsettled := &factProbeEngine{report: func(Batch) error {
		return fmt.Errorf("tagent: the barrier reported beside the write: %w", ErrCommitUncertain)
	}}
	uncertain := factProbeRoot(t, ctx, uncertainRoot, unsettled)
	defer uncertain.Close()
	outcome, err = CommitFact(ctx, uncertain, fact)
	if !errors.Is(err, ErrCommitUncertain) || outcome != FactUncertain {
		t.Fatalf("the unsettled commit = %v, %v, want the uncertain outcome the engine names", outcome, err)
	}
	if _, held, err := uncertain.Segments.GetEvent(ctx, fact.Record.EventKey); err != nil || held {
		t.Fatalf("the committing view answered held %t, %v, want the absence it publishes beside a batch it did not see return", held, err)
	}
	if got := uncertain.Segments.EventCount(); got != 0 {
		t.Fatalf("the committing view counts %d event(s), want none beside the commit it could not name", got)
	}
	reader, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: uncertainRoot})
	if err != nil {
		t.Fatalf("open the root the unsettled commit wrote: %v", err)
	}
	defer reader.Close()
	landed, held, err := reader.Segments.GetEvent(ctx, fact.Record.EventKey)
	if err != nil || !held {
		t.Fatalf("the independent open = held %t, %v, want the record the unsettled commit landed", held, err)
	}
	if !bytes.Equal(landed.Body, fact.Record.Body) {
		t.Fatalf("the independent open carries %d byte(s), want the %d the commit landed", len(landed.Body), len(fact.Record.Body))
	}
	if got := reader.Segments.EventCount(); got != 1 {
		t.Fatalf("the independent open counts %d event(s), want the one the commit landed", got)
	}
}

// TestSegmentReadByPartitionAnswersWhatTheStoreNeverPublished pins the read that names its partition.
//   - the commit landed and its barrier named no outcome, so the store counts no event of it and states no partition for it
//   - the read names the partition the caller placed the fact in, so it addresses that index entry and answers the body the log holds
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestSegmentReadByPartitionAnswersWhatTheStoreNeverPublished(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	probe := &factProbeEngine{report: func(Batch) error {
		return fmt.Errorf("tagent: the barrier reported beside the write: %w", ErrCommitUncertain)
	}}
	tier := factProbeRoot(t, ctx, root, probe)
	defer tier.Close()
	fact := factProbeFact([]byte("the fact the by-partition read names"))
	if outcome, err := CommitFact(ctx, tier, fact); !errors.Is(err, ErrCommitUncertain) || outcome != FactUncertain {
		t.Fatalf("the unsettled commit = %v, %v, want the uncertain outcome the engine names", outcome, err)
	}
	if got := tier.Segments.EventCount(); got != 0 {
		t.Fatalf("the committing view counts %d event(s), want none beside the commit it could not name", got)
	}
	stored, held, err := tier.Segments.GetEventInPartition(ctx, fact.Record.EventKey, fact.Record.PartitionID)
	if err != nil || !held {
		t.Fatalf("the read of the fact in partition %d = held %t, %v, want the body the unsettled commit landed", fact.Record.PartitionID, held, err)
	}
	if !bytes.Equal(stored.Body, fact.Record.Body) {
		t.Fatalf("the read carries %d byte(s), want the %d the commit landed", len(stored.Body), len(fact.Record.Body))
	}
	if stored.PartitionID != fact.Record.PartitionID || stored.WindowTS != fact.Record.WindowTS || stored.Seq != fact.Record.Seq {
		t.Fatalf("the read answers the slot {%d %d %d}, want {%d %d %d}",
			stored.PartitionID, stored.WindowTS, stored.Seq, fact.Record.PartitionID, fact.Record.WindowTS, fact.Record.Seq)
	}
}

// TestSegmentSeatStatesThePartitionALandedCommitCouldNotPublish pins the seat one frame writes.
//   - the first commit landed and its barrier named no outcome, so the next frame states a seat from a store that published nothing for it
//   - the written seat carries the union of the partitions the seat states and the record's own, and one past the greater of the two counts
//   - a fresh open reads the fact of the first partition, so the partition survives the frame that would have dropped it
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestSegmentSeatStatesThePartitionALandedCommitCouldNotPublish(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	var reported atomic.Bool
	probe := &factProbeEngine{report: func(batch Batch) error {
		for _, put := range batch.Puts {
			if put.Key == storeSeatKey && reported.CompareAndSwap(false, true) {
				return fmt.Errorf("tagent: the barrier reported beside the write: %w", ErrCommitUncertain)
			}
		}
		return nil
	}}
	tier := factProbeRoot(t, ctx, root, probe)
	defer tier.Close()
	unpublished := factProbeFact([]byte("the fact the unsettled commit landed"))
	if outcome, err := CommitFact(ctx, tier, unpublished); !errors.Is(err, ErrCommitUncertain) || outcome != FactUncertain {
		t.Fatalf("the unsettled commit = %v, %v, want the uncertain outcome the engine names", outcome, err)
	}
	later := factProbeFact([]byte("the fact the next frame lands"))
	later.Record.PartitionID = 2
	later.Record.EventKey = "later-fact"
	later.Ref.Key = "later-fact"
	if outcome, err := CommitFact(ctx, tier, later); err != nil || outcome != FactCommitted {
		t.Fatalf("the commit after the unsettled one = %v, %v, want the committed outcome", outcome, err)
	}
	reader, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("open the root the two frames landed in: %v", err)
	}
	defer reader.Close()
	if got := reader.Segments.EventCount(); got != 2 {
		t.Fatalf("the fresh open counts %d event(s), want the two frames the log holds: a seat stating one count beside two landed frames was written from the published state alone", got)
	}
	stored, held, err := reader.Segments.GetEvent(ctx, unpublished.Record.EventKey)
	if err != nil || !held {
		t.Fatalf("the fresh open answers the fact of partition %d as held %t, %v, want the body it landed: the seat states the partition the unsettled frame landed in",
			unpublished.Record.PartitionID, held, err)
	}
	if !bytes.Equal(stored.Body, unpublished.Record.Body) {
		t.Fatalf("the fresh open carries %d byte(s), want the %d the unsettled commit landed", len(stored.Body), len(unpublished.Record.Body))
	}
}

// TestOneBatchRefusesWhatItCannotCarryWhole pins the refusal of a fact the engine cannot carry: a named refusal that lands no key.
//   - the refusal names the frame bound the record passed, and it is never the uncertain outcome a caller re-reads
//   - no key family of the fact lands in the committing view or in an independent open of the root
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestOneBatchRefusesWhatItCannotCarryWhole(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	probe := &factProbeEngine{}
	tier := factProbeRoot(t, ctx, root, probe)
	fact := factProbeFact(bytes.Repeat([]byte("x"), 33<<20))
	outcome, err := CommitFact(ctx, tier, fact)
	if outcome != FactRefused {
		t.Fatalf("the oversized commit = %v, %v, want the refusal", outcome, err)
	}
	if !errors.Is(err, ErrJournalFrameTooLarge) {
		t.Fatalf("the oversized commit = %v, want the refusal the frame bound names", err)
	}
	if errors.Is(err, ErrCommitUncertain) {
		t.Fatalf("the oversized commit = %v, want a refusal the engine decided rather than an uncertain outcome", err)
	}
	keys, err := tier.Store.Keys(ctx, "")
	if err != nil {
		t.Fatalf("read the keys of the committing view: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("the refused commit left %v, want no key at all", keys)
	}
	if err := tier.Close(); err != nil {
		t.Fatalf("close the tier: %v", err)
	}
	reopened, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("reopen the root: %v", err)
	}
	defer reopened.Close()
	if keys, err := reopened.Store.Keys(ctx, ""); err != nil || len(keys) != 0 {
		t.Fatalf("the reopened root holds %v, %v, want no key of a refused fact", keys, err)
	}
	if _, held, err := reopened.Segments.GetEvent(ctx, fact.Record.EventKey); err != nil || held {
		t.Fatalf("the reopened event = held %t, %v, want no record", held, err)
	}
	if got := reopened.Originals.Get(ctx, fact.Ref.Key); got.Outcome == RecallFound {
		t.Fatalf("the reopened original = %v, want no body of a refused fact", got.Outcome)
	}
}

// requirePrewriteRefusal holds one fact commit to the refusal it must answer: the
// refused outcome beside the own name of the refusal, never the uncertain outcome a
// caller would re-read, and no key family of the fact in the view that committed.
func requirePrewriteRefusal(t *testing.T, ctx context.Context, tier *Tier, fact FactCommit, refuse error) {
	t.Helper()
	outcome, err := CommitFact(ctx, tier, fact)
	if outcome != FactRefused {
		t.Fatalf("the refused commit = %v, %v, want the refused outcome", outcome, err)
	}
	if !errors.Is(err, refuse) {
		t.Fatalf("the refused commit = %v, want the refusal %v", err, refuse)
	}
	if errors.Is(err, ErrCommitUncertain) {
		t.Fatalf("the refused commit = %v, want a refusal the engine decided rather than an outcome the caller re-reads", err)
	}
	if got := tier.Segments.EventCount(); got != 0 {
		t.Fatalf("the refused commit counts %d event(s), want none", got)
	}
	assertNoFactKey(t, ctx, tier, fact)
}

// assertNoFactKey fails when a view answers any key family of one fact: the event
// record, its original body or the reference that names it.
func assertNoFactKey(t *testing.T, ctx context.Context, view *Tier, fact FactCommit) {
	t.Helper()
	keys, err := view.Store.Keys(ctx, "")
	if err != nil {
		t.Fatalf("read the keys of the view: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("the view holds %v, want no key of the fact it refused", keys)
	}
	if _, held, err := view.Segments.GetEventInPartition(ctx, fact.Record.EventKey, fact.Record.PartitionID); err != nil || held {
		t.Fatalf("the refused fact = held %t, %v, want no record", held, err)
	}
	if got := view.Originals.Get(ctx, fact.Ref.Key); got.Outcome == RecallFound {
		t.Fatalf("the refused fact answers an original of %v, want no body", got.Outcome)
	}
}

// TestFactCommitClassifiesEveryPrewriteRefusal pins the answer one fact commit gives when the engine refuses it before writing any key.
//   - the file engine carrying several keys, a view holding no write rights, a view Close ended and a record over the frame bound all answer the refused outcome
//   - each of them answers its own name beside that outcome, and none of them answers the uncertain outcome
//   - no key family of the refused fact lands, in the view that committed or in an open of the root that took no part in it
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestFactCommitClassifiesEveryPrewriteRefusal(t *testing.T) {
	ctx := context.Background()
	fact := factProbeFact([]byte("the fact every refusal leaves unlanded"))

	t.Run("the file engine refuses a batch carrying several keys", func(t *testing.T) {
		root, cli := t.TempDir(), t.TempDir()
		writeRustVikingStub(t, cli)
		cfg := StorageConfig{Type: StorageFile, Root: root}
		cfg.RustVikingBinary = rustVikingStubPath(cli)
		cfg.RustVikingConfig = cli
		tier, err := OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("open the file tier: %v", err)
		}
		defer tier.Close()
		requirePrewriteRefusal(t, ctx, tier, fact, ErrBatchNotAtomic)
		if ran := rustVikingMutations(t, cli); ran != 0 {
			t.Fatalf("the refused fact ran %d CLI mutation(s), want none: the tier refuses the batch before any call", ran)
		}
		if got := tier.Capabilities.AtomicBatch; got {
			t.Fatalf("the file tier declares AtomicBatch=%v, want the limit its refusal states", got)
		}
	})

	t.Run("the view holds no write rights", func(t *testing.T) {
		root := t.TempDir()
		reading, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: root})
		if err != nil {
			t.Fatalf("open the reading view of a fresh root: %v", err)
		}
		requirePrewriteRefusal(t, ctx, reading, fact, ErrCommitWithoutWriteRights)
		if err := reading.Close(); err != nil {
			t.Fatalf("close the reading view: %v", err)
		}
		writer, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: root, WriteRights: true})
		if err != nil {
			t.Fatalf("open the writer of the root the refusal left free: %v", err)
		}
		defer writer.Close()
		assertNoFactKey(t, ctx, writer, fact)
	})

	t.Run("the view Close ended", func(t *testing.T) {
		root := t.TempDir()
		tier := factProbeRoot(t, ctx, root, &factProbeEngine{})
		if err := tier.Close(); err != nil {
			t.Fatalf("close the tier: %v", err)
		}
		outcome, err := CommitFact(ctx, tier, fact)
		if outcome != FactRefused {
			t.Fatalf("the commit through the ended view = %v, %v, want the refused outcome", outcome, err)
		}
		if !errors.Is(err, ErrReadAfterClose) {
			t.Fatalf("the commit through the ended view = %v, want the closed view the judgment meets before it commits", err)
		}
		if errors.Is(err, ErrCommitUncertain) {
			t.Fatalf("the commit through the ended view = %v, want a refusal the engine decided rather than an outcome the caller re-reads", err)
		}
		writer, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: root, WriteRights: true})
		if err != nil {
			t.Fatalf("open the writer of the root the ended view gave up: %v", err)
		}
		defer writer.Close()
		assertNoFactKey(t, ctx, writer, fact)
	})

	t.Run("the engine reports the ended view at its commit", func(t *testing.T) {
		root := t.TempDir()
		probe := &factProbeEngine{refuse: func(Batch) error { return ErrCommitAfterClose }}
		tier := factProbeRoot(t, ctx, root, probe)
		defer tier.Close()
		requirePrewriteRefusal(t, ctx, tier, fact, ErrCommitAfterClose)
	})

	t.Run("the record passes the frame bound", func(t *testing.T) {
		root := t.TempDir()
		tier := factProbeRoot(t, ctx, root, &factProbeEngine{})
		oversized := factProbeFact(bytes.Repeat([]byte("x"), 33<<20))
		requirePrewriteRefusal(t, ctx, tier, oversized, ErrJournalFrameTooLarge)
		if err := tier.Close(); err != nil {
			t.Fatalf("close the tier: %v", err)
		}
		reopened, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: root})
		if err != nil {
			t.Fatalf("reopen the root: %v", err)
		}
		defer reopened.Close()
		assertNoFactKey(t, ctx, reopened, oversized)
	})
}

// TestUncertainFactCommitRequiresAuthoritativeRead pins the read that decides one commit whose barrier named no outcome.
//   - the commit answers the uncertain outcome, and the view that committed publishes nothing of it: its own read answers absence beside a batch the log holds
//   - an open that took no part in the commit answers the fact by identity, with the very bytes and the very slot the commit carried
//   - the repeat the decision allows lands no key and takes no second sequence slot
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestUncertainFactCommitRequiresAuthoritativeRead(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	probe := &factProbeEngine{report: func(Batch) error {
		return fmt.Errorf("tagent: the barrier reported beside the write: %w", ErrCommitUncertain)
	}}
	tier := factProbeRoot(t, ctx, root, probe)
	fact := factProbeFact([]byte("the fact an unsettled commit landed"))
	outcome, err := CommitFact(ctx, tier, fact)
	if !errors.Is(err, ErrCommitUncertain) || outcome != FactUncertain {
		t.Fatalf("the unsettled commit = %v, %v, want the uncertain outcome the engine names", outcome, err)
	}
	if _, held, err := tier.Segments.GetEvent(ctx, fact.Record.EventKey); err != nil || held {
		t.Fatalf("the committing view answered the fact as held %t, %v, want the absence it publishes beside a commit it did not see return", held, err)
	}
	if got := tier.Segments.EventCount(); got != 0 {
		t.Fatalf("the committing view counts %d event(s), want none beside the commit it could not name", got)
	}
	if err := tier.Close(); err != nil {
		t.Fatalf("close the tier the unsettled commit wrote: %v", err)
	}

	authoritative, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the root the unsettled commit wrote: %v", err)
	}
	defer authoritative.Close()
	landed, held, err := authoritative.Segments.GetEvent(ctx, fact.Record.EventKey)
	if err != nil || !held {
		t.Fatalf("the open that took no part answers the fact as held %t, %v, want the record the commit landed", held, err)
	}
	if !bytes.Equal(landed.Body, fact.Record.Body) {
		t.Fatalf("the deciding open carries %d byte(s), want the %d the commit landed", len(landed.Body), len(fact.Record.Body))
	}
	if landed.PartitionID != fact.Record.PartitionID || landed.WindowTS != fact.Record.WindowTS || landed.Seq != fact.Record.Seq {
		t.Fatalf("the deciding open answers the slot {%d %d %d}, want {%d %d %d}",
			landed.PartitionID, landed.WindowTS, landed.Seq, fact.Record.PartitionID, fact.Record.WindowTS, fact.Record.Seq)
	}
	if got := authoritative.Segments.EventCount(); got != 1 {
		t.Fatalf("the deciding open counts %d event(s), want the one the commit landed", got)
	}
	original := authoritative.Originals.Get(ctx, fact.Ref.Key)
	if original.Outcome != RecallFound || !bytes.Equal(original.Body, []byte(fact.Body)) {
		t.Fatalf("the deciding open answers the original as %v over %d byte(s), want the %d the commit carried", original.Outcome, len(original.Body), len(fact.Body))
	}
	settled, err := authoritative.Store.Keys(ctx, "")
	if err != nil {
		t.Fatalf("read the keys the root holds: %v", err)
	}
	outcome, err = CommitFact(ctx, authoritative, fact)
	if err != nil || outcome != FactAlreadyPresent {
		t.Fatalf("the repeat the decision allows = %v, %v, want the fact the store already holds", outcome, err)
	}
	after, err := authoritative.Store.Keys(ctx, "")
	if err != nil {
		t.Fatalf("read the keys the root holds after the repeat: %v", err)
	}
	if strings.Join(after, ",") != strings.Join(settled, ",") {
		t.Fatalf("the repeat left %v, want the %v it found", after, settled)
	}
	if got := authoritative.Segments.EventCount(); got != 1 {
		t.Fatalf("the repeat left the count at %d, want the one sequence slot the unsettled commit took", got)
	}
}
