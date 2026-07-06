## ADDED Requirements

### Requirement: Compaction scheduler triggers L1→L2 compaction automatically

The `Compactor.checkAndCompact()` method SHALL automatically detect when a partition's L1 segment count reaches the configured threshold (default: 24) and trigger L1→L2 compaction, merging the earliest 24 hourly L1 segments into a single daily L2 segment.

#### Scenario: L1→L2 triggered when 24 L1 segments exist

- **WHEN** a partition has 24 or more L1 segments (layer=1 meta entries)
- **AND** the compaction scheduler runs `checkAndCompact()`
- **THEN** the earliest 24 L1 segments SHALL be compacted into a single L2 daily segment
- **AND** source L1 event keys, index keys, meta keys, and tombstone markers SHALL be deleted after successful L2 write

#### Scenario: L1→L2 not triggered below threshold

- **WHEN** a partition has fewer than 24 L1 segments
- **THEN** `checkL1ToL2()` SHALL skip the partition without error

#### Scenario: L1→L2 runs serially across partitions

- **WHEN** multiple partitions each have 24+ L1 segments
- **THEN** only one compaction SHALL run at a time (serial execution to avoid IO contention)
- **AND** the scheduler SHALL process partitions in order, completing one before starting the next

### Requirement: Compaction scheduler triggers L2→L3 compaction automatically

The `Compactor.checkAndCompact()` method SHALL automatically detect when a partition's L2 segment count reaches the configured threshold (default: 7) and trigger L2→L3 deep compaction, merging the earliest 7 daily L2 segments into a single weekly L3 segment with summarization of low-value event types.

#### Scenario: L2→L3 triggered when 7 L2 segments exist

- **WHEN** a partition has 7 or more L2 segments (layer=2 meta entries)
- **AND** the compaction scheduler runs `checkAndCompact()`
- **THEN** the earliest 7 L2 segments SHALL be compacted into a single L3 weekly segment
- **AND** low-value event types (thinking_plan, context_compress) SHALL have Content and ToolCalls discarded
- **AND** source L2 event keys, index keys, meta keys, and tombstone markers SHALL be deleted

#### Scenario: L2→L3 lower priority than L1→L2

- **WHEN** both L1→L2 and L2→L3 conditions are met simultaneously
- **THEN** L1→L2 SHALL execute before L2→L3 (L1→L2 takes priority)

### Requirement: Scheduler discovers segments by layer

The scheduler SHALL distinguish L1, L2, and L3 segments by reading each segment's `SegmentMeta.Layer` field from KV, enabling it to count segments per layer and select the correct source segments for each compaction level.

#### Scenario: List L1 segments for compaction

- **WHEN** `checkL1ToL2()` lists segments for a partition
- **THEN** only segments with `SegmentMeta.Layer == 1` SHALL be considered as L1 compaction sources

#### Scenario: List L2 segments for deep compaction

- **WHEN** `checkL2ToL3()` lists segments for a partition
- **THEN** only segments with `SegmentMeta.Layer == 2` SHALL be considered as L2 compaction sources
