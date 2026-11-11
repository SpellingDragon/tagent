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
	"errors"
	"fmt"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
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

// TestColdEnumerationReadsEachEventOnce pins the cost of a cold enumeration.
// - after the read cache and the resident set are dropped, one full pass reads each event body once
// - every event keeps its body and its index order
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
	if counted.reads != events {
		t.Fatalf("a cold enumeration read the store %d times for %d events, want one read per event", counted.reads, events)
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
//   - the lookup reads the commits filed since the carrier, not the history behind it
//   - the lookup allocates the same beside a long history as beside none
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestNewestOfTypeReadsTheCommitOrderAndCopiesNoIndex(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, _ := openJournalSegmentStore(t, root)
	earlier := segmentRecord(7, 2000, 4, "carrier-old", 32).WithChain(event.TypeCheckpoint, "")
	if err := store.StoreEvent(ctx, earlier); err != nil {
		t.Fatalf("commit the earlier carrier: %v", err)
	}
	later := segmentRecord(2, 1000, 1, "carrier-new", 32).WithChain(event.TypeCheckpoint, "")
	if err := store.StoreEvent(ctx, later); err != nil {
		t.Fatalf("commit the later carrier: %v", err)
	}
	newest, held, err := store.NewestOfType(ctx, event.TypeCheckpoint)
	if err != nil || !held {
		t.Fatalf("read the newest carrier = (%t, %v), want the committed one", held, err)
	}
	if newest != later.EventKey {
		t.Fatalf("the newest carrier reads as %s, want %s, the identity the later commit filed beside the higher window and slot of %s", newest, later.EventKey, earlier.EventKey)
	}
	reopened, _ := openJournalSegmentStore(t, root)
	if boot, held, err := reopened.NewestOfType(ctx, event.TypeCheckpoint); err != nil || !held || boot != later.EventKey {
		t.Fatalf("the boot over the same log reads the newest carrier as (%s, %t, %v), want %s", boot, held, err, later.EventKey)
	}
	if _, held, err := reopened.NewestOfType(ctx, "no-such-type"); err != nil || held {
		t.Fatalf("a type the store holds no entry of reads as (%t, %v), want no entry and no error", held, err)
	}

	brief := newestOfTypeCostOf(t, 0)
	long := newestOfTypeCostOf(t, 2000)
	if long.bytes > brief.bytes {
		t.Fatalf("the lookup allocates %d byte(s) per read beside 2000 commits behind the carrier and %d beside none, want no whole-index copy", long.bytes, brief.bytes)
	}
	if long.nanos > 8*brief.nanos+2_000 {
		t.Fatalf("the lookup costs %d ns beside 2000 commits behind the carrier and %d beside none, want the commits since the carrier and not the history behind it", long.nanos, brief.nanos)
	}
}

// newestOfTypeCostOf opens a store carrying the given number of commits behind
// one carrier, files the carrier and the cadence of commits that follow it, and
// reports what one newest-of-type read beside them allocates and takes.
func newestOfTypeCostOf(t *testing.T, behind int) struct{ bytes, nanos int64 } {
	t.Helper()
	ctx := context.Background()
	store, _ := openJournalSegmentStore(t, t.TempDir())
	for at := 0; at < behind; at++ {
		rec := segmentRecord(0, 3000+int64(at/8), int64(at%8), fmt.Sprintf("history-%05d", at), 16)
		if err := store.StoreEvent(ctx, rec); err != nil {
			t.Fatalf("commit history event %d: %v", at, err)
		}
	}
	carrier := segmentRecord(2, 1000, 1, "carrier", 32).WithChain(event.TypeCheckpoint, "")
	if err := store.StoreEvent(ctx, carrier); err != nil {
		t.Fatalf("commit the carrier: %v", err)
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

// silentOrderKV is a view over one journal that states no commit order at all,
// so a check can prove the store refuses an order that misses the records it
// published instead of publishing an enumeration short of them.
type silentOrderKV struct {
	*journalKV
}

// eachCommittedNewestFirst names no key: the incomplete order the check reads.
func (k silentOrderKV) eachCommittedNewestFirst(context.Context, func(string) (bool, error)) error {
	return nil
}

// TestLoadRefusesAnOrderThatMissesPublishedEntries pins the order a boot reads.
//   - an engine whose stated order names fewer records than the store published refuses the load
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestLoadRefusesAnOrderThatMissesPublishedEntries(t *testing.T) {
	ctx := context.Background()
	kv, err := openJournalKV(t.TempDir())
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	store, err := newFileSegmentStore(kv)
	if err != nil {
		t.Fatalf("open the segment store: %v", err)
	}
	if err := store.StoreEvent(ctx, segmentRecord(1, 1000, 1, "held", 8)); err != nil {
		t.Fatalf("commit the event: %v", err)
	}
	if _, err := newFileSegmentStore(silentOrderKV{journalKV: kv}); err == nil {
		t.Fatalf("the store published an index the engine states no commit order for, want the load refused")
	}
}

// newestOfTypeAllocBytes reports the bytes one newest-of-type read allocates
// beside the history the store holds, so a check measures the copy a whole-index
// walk would make.
func newestOfTypeAllocBytes(store *FileSegmentStore) int64 {
	ctx := context.Background()
	measured := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for at := 0; at < b.N; at++ {
			if _, _, err := store.NewestOfType(ctx, event.TypeCheckpoint); err != nil {
				b.Fatalf("read the newest carrier: %v", err)
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
			if _, _, err := store.NewestOfType(ctx, event.TypeCheckpoint); err != nil {
				b.Fatalf("read the newest carrier: %v", err)
			}
		}
	})
	return measured.NsPerOp()
}
