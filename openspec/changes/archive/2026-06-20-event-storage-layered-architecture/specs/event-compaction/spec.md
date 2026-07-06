## ADDED Requirements

### Requirement: L0 to L1 active segment sealing

The system SHALL automatically seal the active segment (L0) on each hour boundary, converting it to an L1 segment. The seal process SHALL: close the active file, build the `.idx` offset index, write `meta.json`, move files to L1 directory, and create a new empty active segment.

#### Scenario: Automatic hourly seal

- **WHEN** the wall clock crosses an hour boundary (e.g., 14:00:00.000)
- **THEN** within 1 minute the active segment SHALL be sealed and moved to L1, and a new active segment SHALL be ready for writes

#### Scenario: Seal on startup with existing active segment

- **WHEN** tagent starts and finds an active segment from a previous hour
- **THEN** the old active segment SHALL be immediately sealed before any new events are written

### Requirement: L1 to L2 compaction merges hourly segments into daily segments

The system SHALL compact L1 hourly segments into L2 daily segments when L1 segment count reaches a configurable threshold (default: 24). The compaction SHALL: (1) merge all L1 segments in timestamp order, (2) filter out tombstoned events, (3) repair dangling parent references to nearest alive ancestor, (4) gzip compress the output, (5) build `.idx` index for the new L2 segment, (6) atomically delete source L1 segments only after the L2 segment is fully written.

#### Scenario: Compact 24 L1 segments into one L2 daily segment

- **WHEN** L1 directory contains 24 hourly segments and the compactor runs
- **THEN** a single gzip-compressed L2 daily segment SHALL be created containing all non-tombstoned events in timestamp order, with an accompanying `.idx` file, and all 24 source L1 segments SHALL be deleted

#### Scenario: Tombstone filtering during compaction

- **WHEN** compaction processes events where some are marked as tombstoned in the Bitmap
- **THEN** tombstoned events SHALL be skipped and NOT appear in the output L2 segment

#### Scenario: Dangling reference repair during compaction

- **WHEN** compaction processes an event whose parent is tombstoned (E5 → E4(墓碑) → E3(墓碑) → E2(活))
- **THEN** the event's parent reference in the output segment SHALL be updated to the nearest alive ancestor (E5 → E2)

#### Scenario: Crash during compaction is safe

- **WHEN** tagent crashes mid-compaction (L2 segment partially written)
- **THEN** on restart, the incomplete L2 segment SHALL be detected and discarded, source L1 segments SHALL still exist, and compaction SHALL retry on next schedule

### Requirement: L2 to L3 deep compaction with summarization

The system SHALL compact L2 daily segments into L3 weekly segments when L2 segment count reaches a configurable threshold (default: 7). In addition to the L1→L2 compaction steps, L2→L3 SHALL apply event summarization: low-value event types (thinking_plan, context_compress) SHALL have their Content and ToolCalls fields discarded, retaining only EventSummary.

#### Scenario: Summarize low-value events in L3

- **WHEN** an L2→L3 compaction processes a `thinking_plan` event
- **THEN** the output event SHALL retain EventKey, EventType, EventSummary, and Timestamp, but Content and ToolCalls SHALL be empty

#### Scenario: Preserve high-value events in L3

- **WHEN** an L2→L3 compaction processes an `external_input` or `agent_output` event
- **THEN** the output event SHALL retain all fields unchanged

### Requirement: Compaction runs asynchronously without blocking writes

The system SHALL execute all compaction operations in a background goroutine. Compaction SHALL only operate on sealed L1/L2 segments, never on the active L0 segment. Online event writes to the active segment SHALL not be blocked or delayed by ongoing compaction.

#### Scenario: Write during compaction

- **WHEN** a L1→L2 compaction is running in the background
- **THEN** `StoreEvent()` calls to the active segment SHALL complete without blocking or increased latency

#### Scenario: Only one compaction runs at a time

- **WHEN** a compaction is already in progress and another trigger fires
- **THEN** the second compaction SHALL be skipped or queued, not executed concurrently

### Requirement: Tombstone bitmap management

The system SHALL maintain a memory-resident Bitmap of tombstoned EventKeys. `MarkTombstone(key)` SHALL add a key to the bitmap. `IsTombstone(key)` SHALL check membership. The bitmap SHALL support set operations (intersection, union, difference) for efficient compaction filtering. The bitmap SHALL be persisted to disk for crash recovery.

#### Scenario: Mark and check tombstone

- **WHEN** `MarkTombstone(key=300)` is called
- **THEN** `IsTombstone(300)` SHALL return true, and `IsTombstone(301)` SHALL return false

#### Scenario: Remove tombstone entries after compaction

- **WHEN** compaction completes and successfully filters out tombstoned keys [100, 200, 300]
- **THEN** those keys SHALL be removed from the tombstone bitmap via difference operation
