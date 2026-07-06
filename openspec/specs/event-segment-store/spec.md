## MODIFIED Requirements

### Requirement: Store event as JSON Lines segment by time window

The system SHALL persist events in the RustViking KV store organized by time-windowed segments. Event content SHALL be stored under key `{pid}:evt:{window_ts}:{seq}` → JSON FullEvent. Events SHALL be appended with a monotonically increasing sequence number within each time window. The sequence counter SHALL be recovered from existing KV data on startup to prevent overwriting existing events.

#### Scenario: Append event to active segment

- **WHEN** `MemoryPlugin.onEvent()` calls `StoreEvent(fullEvent)`
- **THEN** the event is serialized as JSON and stored via KV `put` under key `{pid}:evt:{window_ts}:{seq}`, and an index entry is stored under key `{pid}:idx:{event_key}` → `{window_ts}:{seq}`

#### Scenario: Active segment auto-seals on hour boundary

- **WHEN** the current time crosses an hour boundary (e.g., 14:00:00)
- **THEN** the current window's segment metadata SHALL be updated to `Sealed: true` and `Layer: 1`, and a new window SHALL start with seq=0

#### Scenario: Seal produces segment metadata

- **WHEN** an active segment is sealed
- **THEN** the segment meta KV entry (`{pid}:meta:{window_ts}`) SHALL be updated with `event_count`, `sealed: true`, and `layer: 1`

## ADDED Requirements

### Requirement: seqCounter recovered from active-partition bitmap + cursor

On startup, `FileSegmentStore.Init()` SHALL recover each partition's `seqCounter` and `currentWindow` via the `global:active_partitions` bitmap and per-partition `{pid}:cursor` (see capability `startup-active-partition-bitmap`). The legacy approach of scanning `{0..2047}:meta:*` or `{pid}:evt:*` prefixes SHALL NOT be used.

#### Scenario: Init uses bitmap + cursor path

- **WHEN** tagent restarts with 1000 active partitions
- **THEN** `Init()` SHALL read `global:active_partitions` once and issue one `{pid}:cursor` get per active partition; no prefix scan over `{pid}:meta:*` or `{pid}:evt:*` SHALL be issued

#### Scenario: Init on empty store

- **WHEN** `Init()` is called on a KV store with no existing data
- **THEN** the call SHALL succeed without error, leaving partition states at their zero values

### Requirement: StoreEvents partitions seq counter by (pid, windowTS)

`StoreEvents(events)` SHALL group incoming events by `(pid, windowTS)` before assigning sequence numbers. Each group SHALL maintain an independent seq counter derived from that partition's current `PartitionState`. Events within the same group SHALL be ordered deterministically (e.g., by sorted input map key) to guarantee reproducible seq assignment. This SHALL prevent cross-window seq collisions arising from Go map iteration order.

#### Scenario: Events across two windows keep distinct seqs

- **WHEN** a batch contains 3 events in window `1710676800` (pid 1) and 2 events in window `1710680400` (pid 1)
- **THEN** window `1710676800` events SHALL receive seq 0,1,2 and window `1710680400` events SHALL receive seq 0,1; no event key SHALL collide

#### Scenario: Events in same window follow deterministic ordering

- **WHEN** a batch contains 5 events all in window `1710676800` (pid 1), passed as a map
- **THEN** the batch processor SHALL sort input map keys before assigning seq, so repeated calls with identical input produce identical seq assignments

### Requirement: StoreEvents batch writes segment metadata and cursor atomically

`StoreEvents()` (batch write) SHALL write segment metadata (`{pid}:meta:{window_ts}`), the per-partition cursor (`{pid}:cursor`), and the `global:active_partitions` bitmap update for each new time window or newly active partition encountered during the batch — all within the same atomic WriteBatch as the event puts.

#### Scenario: StoreEvents writes meta for new window

- **WHEN** `StoreEvents()` processes the first event for window 1710676800 in partition 1
- **THEN** a meta KV entry `1:meta:1710676800` SHALL be included in the batch with SegmentedMeta{Layer:1, Sealed:false}

#### Scenario: StoreEvents skips meta for already-written window

- **WHEN** `StoreEvents()` processes multiple events in the same window 1710676800
- **THEN** only one meta entry SHALL be written (deduplicated by window)

### Requirement: resolvePartitions defaults to all active partitions

`resolvePartitions(filter)` SHALL return the full set of active partitions (loaded from `global:active_partitions`) when the filter contains no explicit `PartitionIDs` constraint. It SHALL NOT return `nil`, which previously caused silent empty iteration in callers.

#### Scenario: Unfiltered query returns all active partitions

- **WHEN** `resolvePartitions(EventFilter{})` is called with no partition IDs specified and 5 partitions are active (1, 42, 100, 500, 2000)
- **THEN** the returned slice SHALL contain exactly `[1, 42, 100, 500, 2000]` in ascending order

#### Scenario: Explicit partition filter honored

- **WHEN** `resolvePartitions(EventFilter{PartitionIDs: [1, 42]})` is called
- **THEN** the returned slice SHALL contain exactly `[1, 42]` regardless of which other partitions are active

### Requirement: eventCount reflects actual event count after deletions

`PartitionState.eventCount` SHALL be decremented when events are permanently deleted (via `DeleteEvent()`) or when compaction physically removes tombstoned events from a segment. This ensures `LifecycleManager.checkCapacity()` uses accurate counts for capacity-based eviction decisions.

#### Scenario: eventCount decremented on DeleteEvent

- **WHEN** `DeleteEvent(key)` successfully deletes an event
- **THEN** `state.eventCount` SHALL be decremented by 1

#### Scenario: eventCount decremented on compaction cleanup

- **WHEN** compaction's `deleteSegments()` removes a segment with SegmentMeta.EventCount=247
- **THEN** the partition's `eventCount` SHALL be decremented by 247 after successful deletion

### Requirement: FileSegmentStore supports graceful shutdown via Close

`FileSegmentStore` SHALL provide a `Close() error` method that gracefully shuts down all lifecycle subsystems: stops LifecycleManager scanner loop, stops Compactor scheduler loop, flushes all dirty TombstoneSet state to KV, and closes the RelationStore journal file.

#### Scenario: Close stops background goroutines

- **WHEN** `FileSegmentStore.Close()` is called
- **THEN** LifecycleManager SHALL stop scanning (no more TTL/capacity checks)
- **AND** Compactor SHALL stop scheduling (no more compaction cycles)

#### Scenario: Close flushes dirty tombstones

- **WHEN** `FileSegmentStore.Close()` is called and TombstoneSets have unpersisted markers
- **THEN** all dirty tombstone markers SHALL be written to KV before Close returns

#### Scenario: Close is safe to call multiple times

- **WHEN** `FileSegmentStore.Close()` is called more than once
- **THEN** subsequent calls SHALL be no-ops (idempotent)
