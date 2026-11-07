// The residency checks of the local key-value view: entries, not whole shards, stay
// in memory, and an evicted entry reads back exactly from its persisted record.
// The memory assertions are process-level (force GC, then ReadMemStats), not the
// view's own accounting: what a bounded process holds is the claim.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// heapInUse reports the live heap after two forced collections, so the reading
// covers what the process really keeps rather than what it has allocated.
func heapInUse() uint64 {
	runtime.GC()
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.HeapInuse
}

// kvTestValue renders one distinct value of the given size.
func kvTestValue(tag string, size int) []byte {
	prefix := []byte(fmt.Sprintf("%s|", tag))
	if size <= len(prefix) {
		return prefix[:size]
	}
	return append(prefix, bytes.Repeat([]byte("v"), size-len(prefix))...)
}

// commitKVEntries writes count distinct entries under the given key shape in
// bounded batches, so one commit never stages more than a few entries.
func commitKVEntries(t *testing.T, kv *localFileKV, prefix string, offset, count, size int) {
	t.Helper()
	ctx := context.Background()
	const batchSize = 8
	for start := 0; start < count; start += batchSize {
		batch := kvBatch{puts: make([]kvPair, 0, batchSize)}
		for at := start; at < start+batchSize && at < count; at++ {
			index := offset + at
			batch.puts = append(batch.puts, kvPair{
				key:   fmt.Sprintf("%s:%05d", prefix, index),
				value: kvTestValue(fmt.Sprintf("%s-%05d", prefix, index), size),
			})
		}
		if err := kv.kvCommit(ctx, batch); err != nil {
			t.Fatalf("commit %s[%d,%d): %v", prefix, offset+start, offset+start+len(batch.puts), err)
		}
	}
}

// readKVValue reads one key and answers its value.
func readKVValue(t *testing.T, kv *localFileKV, key string) []byte {
	t.Helper()
	pairs, err := kv.kvRead(context.Background(), kvFetch{key: key})
	if err != nil {
		t.Fatalf("read %q: %v", key, err)
	}
	if len(pairs) != 1 {
		t.Fatalf("read %q = %d pairs, want the one value", key, len(pairs))
	}
	return pairs[0].value
}

// dropLocalKVResident clears the resident entry set of one local view, so a
// check can isolate the segment cache budget from the key-value budget beside
// it. It is a test fixture shaping the state a cold process starts in.
func dropLocalKVResident(kv *localFileKV) {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	kv.resident = newKVResidentSet()
}

// TestLocalKVEntryResidencyBounded pins the entry-level residency of the local key-value view.
// - a store far larger than the resident budget keeps the process bounded after a forced collection
// - the shard maps hold none of the evicted values
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestLocalKVEntryResidencyBounded(t *testing.T) {
	root := t.TempDir()
	kv, err := openLocalFileKV(root)
	if err != nil {
		t.Fatalf("open the local key-value view: %v", err)
	}
	before := heapInUse()
	const entries = 64
	const valueSize = 256 << 10
	commitKVEntries(t, kv, "0:evt", 0, entries, valueSize)
	after := heapInUse()
	if growth := int64(after) - int64(before); growth > localKVResidentBudgetBytes+4<<20 {
		t.Fatalf("resident heap growth = %d bytes after committing %d MiB, want the %d-byte budget plus slack",
			growth, entries*valueSize>>20, localKVResidentBudgetBytes)
	}
	if kv.resident.bytes > localKVResidentBudgetBytes {
		t.Fatalf("resident bytes = %d, want at most the %d-byte budget", kv.resident.bytes, localKVResidentBudgetBytes)
	}
}

// TestLocalKVColdEntryReadback pins the read-back equivalence of an evicted entry.
// - every key still reads its exact value
// - a prefix scan merges cold entries with resident ones
// - a later process reads the same values
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestLocalKVColdEntryReadback(t *testing.T) {
	root := t.TempDir()
	kv, err := openLocalFileKV(root)
	if err != nil {
		t.Fatalf("open the local key-value view: %v", err)
	}
	const entries = 32
	const valueSize = 256 << 10
	commitKVEntries(t, kv, "0:evt", 0, entries, valueSize)
	for at := 0; at < entries; at++ {
		key := fmt.Sprintf("0:evt:%05d", at)
		want := kvTestValue(fmt.Sprintf("0:evt-%05d", at), valueSize)
		if got := readKVValue(t, kv, key); !bytes.Equal(got, want) {
			t.Fatalf("read %q after eviction = %d bytes with the wrong content", key, len(got))
		}
	}
	pairs, err := kv.kvRead(context.Background(), kvFetch{prefix: "0:evt:"})
	if err != nil {
		t.Fatalf("prefix scan the shard: %v", err)
	}
	if len(pairs) != entries {
		t.Fatalf("prefix scan = %d pairs, want %d", len(pairs), entries)
	}
	for _, pair := range pairs {
		at := strings.LastIndexByte(pair.key, ':') + 1
		want := kvTestValue(fmt.Sprintf("0:evt-%05d", mustAtoi(t, pair.key[at:])), valueSize)
		if !bytes.Equal(pair.value, want) {
			t.Fatalf("prefix scan %q carries the wrong value", pair.key)
		}
	}
	reopened, err := openLocalFileKV(root)
	if err != nil {
		t.Fatalf("reopen the local key-value view: %v", err)
	}
	key := "0:evt:00000"
	if got := readKVValue(t, reopened, key); !bytes.Equal(got, kvTestValue("0:evt-00000", valueSize)) {
		t.Fatalf("a reopened process read %q with the wrong content", key)
	}
}

// mustAtoi parses the decimal suffix of a test key.
func mustAtoi(t *testing.T, text string) int {
	t.Helper()
	value := 0
	for _, digit := range text {
		if digit < '0' || digit > '9' {
			t.Fatalf("key suffix %q is not decimal", text)
		}
		value = value*10 + int(digit-'0')
	}
	return value
}

// TestLocalKVColdEntryMiss pins the absent-read behavior.
// - a key never held, a deleted key and an empty prefix all answer no pair
// - the deletion survives a reopen
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestLocalKVColdEntryMiss(t *testing.T) {
	root := t.TempDir()
	kv, err := openLocalFileKV(root)
	if err != nil {
		t.Fatalf("open the local key-value view: %v", err)
	}
	if pairs, err := kv.kvRead(context.Background(), kvFetch{key: "0:evt:absent"}); err != nil || len(pairs) != 0 {
		t.Fatalf("absent key = (%d pairs, %v), want no pair", len(pairs), err)
	}
	if pairs, err := kv.kvRead(context.Background(), kvFetch{prefix: "9:evt:"}); err != nil || len(pairs) != 0 {
		t.Fatalf("absent prefix = (%d pairs, %v), want no pair", len(pairs), err)
	}
	commitKVEntries(t, kv, "0:evt", 0, 1, 64<<10)
	if err := kv.kvCommit(context.Background(), kvBatch{deletes: []string{"0:evt:00000"}}); err != nil {
		t.Fatalf("delete the entry: %v", err)
	}
	if pairs, err := kv.kvRead(context.Background(), kvFetch{key: "0:evt:00000"}); err != nil || len(pairs) != 0 {
		t.Fatalf("deleted key = (%d pairs, %v), want no pair", len(pairs), err)
	}
	reopened, err := openLocalFileKV(root)
	if err != nil {
		t.Fatalf("reopen the local key-value view: %v", err)
	}
	if pairs, err := reopened.kvRead(context.Background(), kvFetch{key: "0:evt:00000"}); err != nil || len(pairs) != 0 {
		t.Fatalf("deleted key after reopen = (%d pairs, %v), want no pair", len(pairs), err)
	}
}

// benchLocalKVEntries is the number of keys the local key-value benchmark keeps
// so a read walks a shard several pages wide rather than one pair.
const benchLocalKVEntries = 2048

// BenchmarkLocalKVGet measures one point read of the local key-value view in
// its steady state: the shard is committed first, then the read loop asks for
// the middle key of the shard.
func BenchmarkLocalKVGet(b *testing.B) {
	kv, err := openLocalFileKV(b.TempDir())
	if err != nil {
		b.Fatalf("open the local key-value view: %v", err)
	}
	ctx := context.Background()
	batch := kvBatch{puts: make([]kvPair, 0, benchLocalKVEntries)}
	for at := 0; at < benchLocalKVEntries; at++ {
		batch.puts = append(batch.puts, kvPair{
			key:   fmt.Sprintf("0:evt:%d", at),
			value: []byte(fmt.Sprintf("%0256d", at)),
		})
	}
	if err := kv.kvCommit(ctx, batch); err != nil {
		b.Fatalf("commit the shard: %v", err)
	}
	key := fmt.Sprintf("0:evt:%d", benchLocalKVEntries/2)
	b.ResetTimer()
	for at := 0; at < b.N; at++ {
		pairs, err := kv.kvRead(ctx, kvFetch{key: key})
		if err != nil || len(pairs) != 1 {
			b.Fatalf("read %q = (%d, %v)", key, len(pairs), err)
		}
	}
}

// TestOriginalStoreRefusesLegacySharedBodies pins a clean format boundary.
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestOriginalStoreRefusesLegacySharedBodies(t *testing.T) {
	ctx := context.Background()
	backing := newMemKV()
	body := []byte("shared in the old format")
	digest := digestOf(body)
	stored, err := json.Marshal([]storedRef{{Format: "text", StoredAt: time.Now(), Digest: digest, Length: int64(len(body))}})
	if err != nil {
		t.Fatalf("encode the legacy reference: %v", err)
	}
	if err := backing.kvCommit(ctx, kvBatch{puts: []kvPair{{key: refKVKey("legacy-event"), value: stored}, {key: "blob:" + digest, value: body}}}); err != nil {
		t.Fatalf("seed the legacy shared body: %v", err)
	}
	if _, err := newKVOriginalStore(backing, nil); !errors.Is(err, ErrSnapshotCorrupt) {
		t.Fatalf("opening the legacy shared-body format returned %v, want a named refusal", err)
	}
}
