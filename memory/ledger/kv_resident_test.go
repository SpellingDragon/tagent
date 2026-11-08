// The residency checks of the durable key-value engine: entries, not whole
// records, stay in memory, and an evicted entry reads back exactly from the
// journal record that carries it. The memory assertions are process-level (force
// GC, then ReadMemStats), not the engine's own accounting: what a bounded process
// holds is the claim.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
package ledger

import (
	"bytes"
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
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
func commitKVEntries(t *testing.T, kv *journalKV, prefix string, offset, count, size int) {
	t.Helper()
	ctx := context.Background()
	const batchSize = 8
	for start := 0; start < count; start += batchSize {
		batch := Batch{Puts: make([]Pair, 0, batchSize)}
		for at := start; at < start+batchSize && at < count; at++ {
			index := offset + at
			batch.Puts = append(batch.Puts, Pair{
				Key:   fmt.Sprintf("%s:%05d", prefix, index),
				Value: kvTestValue(fmt.Sprintf("%s-%05d", prefix, index), size),
			})
		}
		if err := kv.Commit(ctx, batch); err != nil {
			t.Fatalf("commit %s[%d,%d): %v", prefix, offset+start, offset+start+len(batch.Puts), err)
		}
	}
}

// readKVValue reads one key and answers its value.
func readKVValue(t *testing.T, kv *journalKV, key string) []byte {
	t.Helper()
	pairs, err := kv.Read(context.Background(), Fetch{Key: key})
	if err != nil {
		t.Fatalf("read %q: %v", key, err)
	}
	if len(pairs) != 1 {
		t.Fatalf("read %q = %d pairs, want the one value", key, len(pairs))
	}
	return pairs[0].Value
}

// dropJournalResident clears the resident entry set of one journal view, so a
// check isolates the segment cache budget from the key-value budget beside it.
// It is a test fixture shaping the state a cold process starts in.
func dropJournalResident(kv *journalKV) {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	kv.resident = newKVResidentSet()
}

// TestJournalKVEntryResidencyBounded pins the entry-level residency of the durable key-value engine.
// - a store far larger than the resident budget keeps the process bounded after a forced collection
// - the resident set holds none of the evicted values
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalKVEntryResidencyBounded(t *testing.T) {
	dir := t.TempDir()
	kv, err := openJournalKV(dir)
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	before := heapInUse()
	const entries = 64
	const valueSize = 256 << 10
	commitKVEntries(t, kv, "0:evt", 0, entries, valueSize)
	after := heapInUse()
	if growth := int64(after) - int64(before); growth > residentSetBudgetBytes+4<<20 {
		t.Fatalf("resident heap growth = %d bytes after committing %d MiB, want the %d-byte budget plus slack",
			growth, entries*valueSize>>20, residentSetBudgetBytes)
	}
	if kv.resident.bytes > residentSetBudgetBytes {
		t.Fatalf("resident bytes = %d, want at most the %d-byte budget", kv.resident.bytes, residentSetBudgetBytes)
	}
}

// TestJournalKVColdEntryReadback pins the read-back equivalence of an evicted entry.
// - every key still reads its exact value
// - a prefix read merges cold entries with resident ones
// - a later process reads the same values
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalKVColdEntryReadback(t *testing.T) {
	dir := t.TempDir()
	kv, err := openJournalKV(dir)
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	const entries = 32
	const valueSize = 256 << 10
	commitKVEntries(t, kv, "0:evt", 0, entries, valueSize)
	dropJournalResident(kv)
	for at := 0; at < entries; at++ {
		key := fmt.Sprintf("0:evt:%05d", at)
		want := kvTestValue(fmt.Sprintf("0:evt-%05d", at), valueSize)
		if got := readKVValue(t, kv, key); !bytes.Equal(got, want) {
			t.Fatalf("read %q after eviction = %d bytes with the wrong content", key, len(got))
		}
	}
	pairs, err := kv.Read(context.Background(), Fetch{Prefix: "0:evt:"})
	if err != nil {
		t.Fatalf("prefix read the journal: %v", err)
	}
	if len(pairs) != entries {
		t.Fatalf("prefix read = %d pairs, want %d", len(pairs), entries)
	}
	for _, pair := range pairs {
		at := strings.LastIndexByte(pair.Key, ':') + 1
		want := kvTestValue(fmt.Sprintf("0:evt-%05d", mustAtoi(t, pair.Key[at:])), valueSize)
		if !bytes.Equal(pair.Value, want) {
			t.Fatalf("prefix read %q carries the wrong value", pair.Key)
		}
	}
	reopened, err := openJournalKV(dir)
	if err != nil {
		t.Fatalf("reopen the journal view: %v", err)
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

// TestJournalKVColdEntryMiss pins the absent-read behavior.
// - a key never held, a deleted key and an empty prefix all answer no pair
// - the deletion survives a reopen
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalKVColdEntryMiss(t *testing.T) {
	dir := t.TempDir()
	kv, err := openJournalKV(dir)
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	if pairs, err := kv.Read(context.Background(), Fetch{Key: "0:evt:absent"}); err != nil || len(pairs) != 0 {
		t.Fatalf("absent key = (%d pairs, %v), want no pair", len(pairs), err)
	}
	if pairs, err := kv.Read(context.Background(), Fetch{Prefix: "9:evt:"}); err != nil || len(pairs) != 0 {
		t.Fatalf("absent prefix = (%d pairs, %v), want no pair", len(pairs), err)
	}
	commitKVEntries(t, kv, "0:evt", 0, 1, 64<<10)
	if err := kv.Commit(context.Background(), Batch{Deletes: []string{"0:evt:00000"}}); err != nil {
		t.Fatalf("delete the entry: %v", err)
	}
	if pairs, err := kv.Read(context.Background(), Fetch{Key: "0:evt:00000"}); err != nil || len(pairs) != 0 {
		t.Fatalf("deleted key = (%d pairs, %v), want no pair", len(pairs), err)
	}
	reopened, err := openJournalKV(dir)
	if err != nil {
		t.Fatalf("reopen the journal view: %v", err)
	}
	if pairs, err := reopened.Read(context.Background(), Fetch{Key: "0:evt:00000"}); err != nil || len(pairs) != 0 {
		t.Fatalf("deleted key after reopen = (%d pairs, %v), want no pair", len(pairs), err)
	}
}
