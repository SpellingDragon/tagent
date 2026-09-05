# Spec: Memory Storage Production Hardening

## ADDED Requirements

### REQ-1: RustViking KV Range Scan

The system SHALL provide a `kv range` subcommand in RustViking CLI that performs forward range scans from a start key to an end key with optional limit.

- **REQ-1.1**: `kv range --start <key> --end <key> --limit <n>` returns `{success, data: {start, end, count, entries: [{key, value}]}, api_version: "v1"}`
- **REQ-1.2**: Implementation uses `RocksDB iterator(IteratorMode::From(start, Forward))`, stops at key ≥ end or limit reached
- **REQ-1.3**: `kv batch` uses `WriteBatch::commit()` for atomicity (R1 fix)
- **REQ-1.4**: All KV subcommands output unified JSON envelope `{success, data | error, api_version: "v1"}` (R3)
- **REQ-1.5**: Benchmarks: put P99 < 3ms, batch 100 ops P99 < 10ms, range scan 1000 keys P99 < 15ms

### REQ-2: Active Partitions Bitmap

The system SHALL maintain a global active partitions bitmap for O(1) partition discovery during Init.

- **REQ-2.1**: `global:active_partitions` key stores a bitmap encoding all active partition IDs
- **REQ-2.2**: `EncodeActivePartitionsBitmap(pids []int) []byte` and `ParseActivePartitionsBitmap(b []byte) []int` provide round-trip encoding
- **REQ-2.3**: `StoreEvent` updates the bitmap atomically in the same WriteBatch as the event write
- **REQ-2.4**: `FileSegmentStore.Init()` reads the bitmap once, then concurrently reads `{pid}:cursor` for each active partition (concurrency = `runtime.NumCPU() * 4`)
- **REQ-2.5**: Init completes in < 500ms for 1000 active partitions
- **REQ-2.6**: Init tolerates individual cursor read failures (warning + zero-value state, other partitions unaffected)

### REQ-3: Batch Write Seq Collision Fix

The system SHALL prevent sequence number collisions in `StoreEvents` when writing across multiple time windows.

- **REQ-3.1**: `StoreEvents` groups events by `(pid, windowTS)` and maintains independent seq counters per group
- **REQ-3.2**: `ensureSegmentMeta` writes one meta put per new window per batch (deduplicated via `map[int64]bool`)
- **REQ-3.3**: Segment size rotation triggers when `state.currentSegmentEventCount >= cfg.MaxEventsPerSegment`
- **REQ-3.4**: `resolvePartitions(filter)` with empty `filter.PartitionIDs` returns all active partitions (not nil)
- **REQ-3.5**: Cursor writes are atomic with event writes in the same WriteBatch

### REQ-4: Per-Partition Tombstone Isolation

The system SHALL isolate tombstone sets per partition with lazy initialization.

- **REQ-4.1**: `FileSegmentStore.tombstones` is `sync.Map` (key=int pid, value=`*TombstoneSet`)
- **REQ-4.2**: `getTombstoneSet(pid)` lazily creates via `LoadOrStore`, auto-calls `RecoverFromKV()` on creation
- **REQ-4.3**: Recovery scans `{pid}:tomb:` prefix to fill in-memory set
- **REQ-4.4**: `MarkTombstone`/`IsTombstone`/`DeleteEvent` all use `getTombstoneSet(pid)`

### REQ-5: TTL Forward Cursor

The system SHALL use forward-only TTL cursors to avoid rescanning expired windows.

- **REQ-5.1**: Each partition has `{pid}:ttl_cursor` storing `TTLCursor{next_scan_window, last_evicted_ts}`
- **REQ-5.2**: `evictOldest(pid)` starts from `ttl_cursor.next_scan_window - 1`, scans backward
- **REQ-5.3**: After scan, cursor is updated to `next_scan_window = oldest_unscanned_window`
- **REQ-5.4**: Cursor only moves forward; already-scanned windows are never rescanned
- **REQ-5.5**: `LoadTTLCursor(kv, pid)` / `SaveTTLCursor(kv, pid, cursor)` helpers

### REQ-6: RelationStore Rewrite

The system SHALL rewrite RelationStore with LRU eviction, WAL persistence, and range scan queries.

- **REQ-6.1**: In-memory `childToParent` + `parentToChildren` maps with LRU eviction (default max 10000 entries)
- **REQ-6.2**: `SetParent(child, parent)` writes `{pid}:rel:{child}` + `{pid}:revrel:{parent}:{child}` in one WriteBatch
- **REQ-6.3**: `GetChildren(parent)` uses RustViking `kv range` on `{pid}:revrel:{parent}:` prefix
- **REQ-6.4**: WAL journal ensures durability; on restart, relations are recovered from KV
- **REQ-6.5**: LRU eviction evicts coldest entries when capacity exceeded; evicted entries remain queryable via KV range

### REQ-7: Compaction Hardening

The system SHALL fix compaction scheduling, tombstone filtering, and dangling reference repair.

- **REQ-7.1**: `checkL1ToL2()` / `checkL2ToL3()` actually trigger compaction (not just seal)
- **REQ-7.2**: `filterTombstoned()` calls `TombstoneSet.IsTombstone()` (not empty stub)
- **REQ-7.3**: `repairDanglingRefs` alive set = batch events ∪ existing L2/L3 events (via tombstone check)
- **REQ-7.4**: `deleteSegments()` also deletes `{pid}:idx:` and `{pid}:tomb:` KV for tombstoned events

### REQ-8: FileBackend Migration

The system SHALL migrate from FileBackend to FileSegmentStore via dual-write strategy.

- **REQ-8.1**: MemoryPlugin writes to both FileBackend (old) and FileSegmentStore (new) simultaneously
- **REQ-8.2**: RecallAgent/KnowledgeAgent read from FileSegmentStore; FileBackend is read-only
- **REQ-8.3**: `cmd/migrate-events/` tool batch-converts old FileBackend events to RustViking KV
- **REQ-8.4**: After migration, `memory/file_backend.go` and `memory/file_backend_test.go` are deleted
- **REQ-8.5**: Dual-write logic is removed; FileSegmentStore is the sole storage backend

### REQ-9: LLM Event Summarization

The system SHALL provide an LLM-driven summarizer for L2→L3 compaction.

- **REQ-9.1**: `LLMSummarizer` struct with `model`, `batchSize`, `timeout`, `fallback SummaryGenerator` fields
- **REQ-9.2**: `Generate(event FullEvent) (string, error)` for single event
- **REQ-9.3**: `GenerateBatch(events []FullEvent) ([]string, error)` for batch processing
- **REQ-9.4**: Event type → prompt template mapping (`assistant_response`, `tool_result`, `thinking_plan`, etc.)
- **REQ-9.5**: L2→L3 compaction groups events by type, calls `GenerateBatch` per bucket
- **REQ-9.6**: On batch failure, falls back to `PassthroughSummarizer.Generate` per event
- **REQ-9.7**: Circuit breaker: N consecutive timeouts → degraded mode for M minutes
- **REQ-9.8**: Config: `archive_summarizer.model`, `max_batch_size`, `timeout_ms`, `enabled`

### REQ-10: Documentation Completion

The system SHALL complete documentation for Snowflake int64 EventKey format.

- **REQ-10.1**: `docs/wiki/memory/memory-architecture.md` reflects Snowflake int64 format for event_key
