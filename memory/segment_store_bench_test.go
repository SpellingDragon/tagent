package memory

import (
	"fmt"
	"math/rand"
	"testing"
)

// BenchmarkSegmentStore_GetEvent benchmarks GetEvent with LRU caching.
// 本文件是记忆存储行为的测试执行体。
//
// 契约: docs/wiki/memory/memory-architecture.md#tombstone
func BenchmarkSegmentStore_GetEvent(b *testing.B) {
	mockKV := newMockKV()
	store, err := NewFileSegmentStore(mockKV, nil, ":memory:", 1000)
	if err != nil {
		b.Fatal(err)
	}

	n := 1000
	keys := make([]int64, n)
	baseTS := int64(1710678000000)
	for i := 0; i < n; i++ {
		key := NewSnowflakeEventKey(1, baseTS+int64(i)*1000)
		keys[i] = key
		err := store.StoreEvent(key, FullEvent{
			PartitionID:  1,
			EventType:    "test",
			EventSummary: fmt.Sprintf("benchmark event %d", i),
			Timestamp:    baseTS + int64(i)*1000,
		})
		if err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % n
		_, err := store.GetEvent(keys[idx])
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSegmentStore_GetEvent_Cold benchmarks GetEvent without caching (cold start).
func BenchmarkSegmentStore_GetEvent_Cold(b *testing.B) {
	mockKV := newMockKV()
	store, err := NewFileSegmentStore(mockKV, nil, ":memory:", 1000)
	if err != nil {
		b.Fatal(err)
	}

	n := 100
	keys := make([]int64, n)
	baseTS := int64(1710678000000)
	for i := 0; i < n; i++ {
		key := NewSnowflakeEventKey(1, baseTS+int64(i)*1000)
		keys[i] = key
		err := store.StoreEvent(key, FullEvent{
			PartitionID:  1,
			EventType:    "test",
			EventSummary: fmt.Sprintf("benchmark event %d", i),
			Timestamp:    baseTS + int64(i)*1000,
		})
		if err != nil {
			b.Fatal(err)
		}
	}

	coldStore, err := NewFileSegmentStore(mockKV, nil, ":memory:", 1000)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % n
		_, err := coldStore.GetEvent(keys[idx])
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSegmentStore_QueryEvents benchmarks QueryEvents by time range.
func BenchmarkSegmentStore_QueryEvents(b *testing.B) {
	mockKV := newMockKV()
	store, err := NewFileSegmentStore(mockKV, nil, ":memory:", 1000)
	if err != nil {
		b.Fatal(err)
	}

	n := 500
	baseTS := int64(1710604800000)
	for i := 0; i < n; i++ {
		key := NewSnowflakeEventKey(1, baseTS+int64(i)*int64(rand.Int31n(3600000)))
		_ = store.StoreEvent(key, FullEvent{
			PartitionID:  1,
			EventType:    "bench",
			EventSummary: fmt.Sprintf("query event %d", i),
			Timestamp:    baseTS + int64(i)*1000,
		})
	}

	_ = store.SealCurrent(1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results, err := store.QueryEvents(QueryOptions{
			PartitionIDs: []int{1},
			Limit:        50,
		})
		if err != nil {
			b.Fatal(err)
		}
		_ = results
	}
}

// BenchmarkSegmentStore_StoreEvent benchmarks StoreEvent throughput.
func BenchmarkSegmentStore_StoreEvent(b *testing.B) {
	mockKV := newMockKV()
	store, err := NewFileSegmentStore(mockKV, nil, ":memory:", 1000)
	if err != nil {
		b.Fatal(err)
	}

	baseTS := int64(1710678000000)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		key := NewSnowflakeEventKey((i%64)+1, baseTS+int64(i)*1000)
		err := store.StoreEvent(key, FullEvent{
			PartitionID:  (i % 64) + 1,
			EventType:    "bench",
			EventSummary: fmt.Sprintf("store benchmark %d", i),
			Timestamp:    baseTS + int64(i)*1000,
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}
