## MODIFIED Requirements

### Requirement: L1 to L2 compaction merges hourly segments into daily segments

The system SHALL compact L1 hourly segments into L2 daily segments when L1 segment count reaches a configurable threshold (default: 24). The compaction SHALL: (1) merge all L1 segments in timestamp order, (2) filter out tombstoned events by checking each event's EventKey against the TombstoneSet, (3) repair dangling parent references to nearest alive ancestor, (4) write output as KV entries under the L2 daily window key, (5) update index entries to point to the new L2 locations, (6) atomically delete source L1 event keys, index keys, and meta keys only after the L2 segment is fully written via atomic batch.

#### Scenario: Compact 24 L1 segments into one L2 daily segment

- **WHEN** L1 contains 24 hourly segments and the compactor runs
- **THEN** a single L2 daily segment SHALL be created in KV containing all non-tombstoned events in timestamp order, with updated index entries, and all 24 source L1 segments SHALL be deleted atomically

#### Scenario: Tombstone filtering during compaction

- **WHEN** compaction processes events where some EventKeys are present in the partition's TombstoneSet
- **THEN** tombstoned events SHALL be skipped and NOT appear in the output L2 segment, and their old index entries SHALL be deleted

#### Scenario: Dangling reference repair during compaction

- **WHEN** compaction processes an event whose parent is tombstoned (E5 → E4(墓碑) → E3(墓碑) → E2(活))
- **THEN** the event's parent reference in RelationStore SHALL be updated to the nearest alive ancestor (E5 → E2)

#### Scenario: Crash during compaction is safe

- **WHEN** tagent crashes mid-compaction (L2 data partially written)
- **THEN** on restart, the incomplete L2 data SHALL be overwritten on next compaction, source L1 data SHALL still exist, and compaction SHALL retry on next schedule

### Requirement: L2 to L3 deep compaction with summarization

The system SHALL compact L2 daily segments into L3 weekly segments when L2 segment count reaches a configurable threshold (default: 7). In addition to the L1→L2 compaction steps, L2→L3 SHALL apply event summarization: low-value event types (thinking_plan, context_compress) SHALL have their Content and ToolCalls fields discarded, retaining only EventSummary. Tombstone filtering SHALL also apply.

#### Scenario: Summarize low-value events in L3

- **WHEN** an L2→L3 compaction processes a `thinking_plan` event
- **THEN** the output event SHALL retain EventKey, EventType, EventSummary, and Timestamp, but Content and ToolCalls SHALL be empty

#### Scenario: Preserve high-value events in L3

- **WHEN** an L2→L3 compaction processes an `external_input` or `agent_output` event
- **THEN** the output event SHALL retain all fields unchanged

#### Scenario: Tombstone filtering in deep compaction

- **WHEN** an L2→L3 compaction processes events where some are tombstoned
- **THEN** tombstoned events SHALL be skipped and NOT appear in the L3 output

## ADDED Requirements

### Requirement: Compaction deletes old index entries on cleanup

When source segments are deleted during compaction cleanup, the system SHALL also delete the index entries (`{pid}:idx:{event_key}`) for events that were tombstoned and filtered out, preventing index key leakage.

#### Scenario: Delete index entries for filtered tombstone events

- **WHEN** compaction filters out tombstoned events with EventKeys [100, 200, 300]
- **THEN** the KV batch for cleanup SHALL include delete operations for `{pid}:idx:100`, `{pid}:idx:200`, `{pid}:idx:300`

### Requirement: Batch writes are atomic via RocksDB WriteBatch

All compaction batch operations SHALL be executed as a single atomic RocksDB WriteBatch, ensuring that either all puts and deletes succeed or none are applied. This SHALL be enforced on the RustViking CLI side by using `KvStore::batch()` instead of individual `put`/`delete` calls.

#### Scenario: Atomic batch ensures all-or-nothing

- **WHEN** a compaction batch contains 500 put operations and 24 delete operations
- **THEN** either all 524 operations SHALL be persisted atomically, or if the process crashes mid-batch, zero operations SHALL be persisted

### Requirement: Tombstone KV keys cleaned after compaction physical deletion

After compaction physically removes tombstoned events, the system SHALL also delete the corresponding tombstone KV markers (`{pid}:tomb:{event_key}`) to prevent unbounded accumulation of stale tombstone keys in the KV store.

#### Scenario: Remove tombstone markers for filtered events

- **WHEN** compaction's `filterTombstoned()` filters out events with EventKeys [100, 200, 300]
- **AND** `deleteSegments()` successfully deletes the source segments
- **THEN** `TombstoneSet.RemoveTombstones([100, 200, 300])` SHALL be called to delete tombstone KV entries `{pid}:tomb:100`, `{pid}:tomb:200`, `{pid}:tomb:300`

#### Scenario: Tombstone in-memory set updated after KV deletion

- **WHEN** `RemoveTombstones(keys)` completes successfully
- **THEN** the keys SHALL be removed from the in-memory tombstone set
- **AND** the tombstone KV entries SHALL be deleted from RocksDB

### Requirement: repairDanglingRefs alive set includes non-tombstoned historical events

During compaction, `repairDanglingRefs` SHALL determine whether a parent reference is dangling by consulting BOTH (a) the alive set of events in the current compaction batch AND (b) the partition's `TombstoneSet`. A parent SHALL be considered alive if it is present in the current batch OR if it is NOT marked as tombstoned (regardless of whether it has already been compacted to L2/L3).

The previous implementation used only the current-batch alive set, which incorrectly classified any parent already compacted to a deeper layer as dead and triggered erroneous `findAliveAncestor` walks.

#### Scenario: Parent in deeper layer is recognized as alive

- **WHEN** event `E5` has `parent = E2`, `E2` has already been compacted to L2 (not in the current L1→L2 batch), and `E2` is NOT in the TombstoneSet
- **THEN** `repairDanglingRefs` SHALL treat `E2` as alive; `E5.parent` SHALL NOT be modified

#### Scenario: Tombstoned ancestor triggers repair

- **WHEN** event `E5` has `parent = E4`, `E4` and `E3` are tombstoned, `E2` is alive
- **THEN** `repairDanglingRefs` SHALL walk up via `findAliveAncestor` and patch `E5.parent = E2`

### Requirement: L3 summarization delegated to l3-archive-summarization capability

L2→L3 compaction SHALL apply the per-type summarization policy defined in capability `l3-archive-summarization` (strategies: `full`, `summary`, `partial`). The Compactor SHALL invoke the injected `SummaryGenerator` for events requiring `summary` strategy; the default `PassthroughSummarizer` SHALL return empty strings in this change. LLM-backed summarization is delegated to future change `llm-event-summary`.
## ADDED Requirements

### Requirement: extractExecutionState is internal to SmartCompressor

The `extractExecutionState` function SHALL be migrated from a standalone function to a method on SmartCompressor. Its truncation parameters (`maxExecStateChars`, `maxToolResultChars`, `maxToolArgsChars`) SHALL be fields on SmartCompressor, configurable via SmartCompressorOption. The standalone function and package-level constants SHALL be removed.

#### Scenario: extractExecutionState uses configurable parameters

- **WHEN** SmartCompressor is created with `WithMaxExecStateChars(3000)` and `WithMaxToolResultChars(800)`
- **THEN** extractExecutionState SHALL truncate tool results to 800 chars and total execution state to 3000 chars

#### Scenario: Default parameters preserve current behavior

- **WHEN** SmartCompressor is created without explicit compress options
- **THEN** maxExecStateChars SHALL default to 2000, maxToolResultChars to 500, maxToolArgsChars to 80

### Requirement: extractExecutionState extracts async tool results from system messages

extractExecutionState SHALL extract system messages containing `[system] tmux` prefix (ActionTool async results) in addition to RoleTool messages. Each extracted async result SHALL be truncated to `maxToolResultChars` and prefixed with `→ 异步结果:`.

#### Scenario: Async tmux result preserved in execution state

- **WHEN** an old segment contains a system message `[system] tmux session X state changed: running -> completed\nOutput:\n<article>`
- **THEN** extractExecutionState SHALL include `→ 异步结果: [system] tmux session X state changed...` (truncated to maxToolResultChars)
