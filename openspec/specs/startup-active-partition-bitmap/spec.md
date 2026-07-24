# startup-active-partition-bitmap Specification

## Purpose

本规范定义 startup-active-partition-bitmap 能力。The system SHALL maintain a `global:active_partitions` KV entry as a fixed-size 2048-bit bitmap (256 bytes), with bit `N` set iff partition `N` has at least one

## Requirements

### Requirement: Active partition bitmap drives Init discovery

The system SHALL maintain a `global:active_partitions` KV entry as a fixed-size 2048-bit bitmap (256 bytes), with bit `N` set iff partition `N` has at least one persisted event. `FileSegmentStore.Init()` SHALL load this bitmap to discover active partitions instead of scanning `{0..2047}:meta:*` prefixes.

#### Scenario: Setting active partition bit on first write

- **WHEN** `StoreEvent()` or `StoreEvents()` writes the first event for partition `42`
- **THEN** the write batch SHALL include a put to `global:active_partitions` with bit 42 set, atomically with the event put

#### Scenario: Init reads bitmap instead of scanning meta

- **WHEN** `Init()` is called with 1000 active partitions (pids scattered in 0..2047)
- **THEN** exactly one KV `get` for `global:active_partitions` SHALL be issued; no `{pid}:meta:*` prefix scan SHALL be performed

### Requirement: Per-partition cursor replaces seq scanning

Each partition SHALL maintain `{pid}:cursor` → JSON `{current_window, seq_counter, last_event_key, updated_at}`. Every `StoreEvent`/`StoreEvents` batch SHALL update this cursor atomically within the same write batch as the event puts. `Init()` SHALL recover `PartitionState` from each active partition's cursor via a single point `get`.

#### Scenario: Recover currentWindow and seqCounter from cursor

- **WHEN** tagent restarts after partition `1` last wrote at `{current_window: 1710676800, seq_counter: 247}`
- **THEN** `Init()` SHALL read `1:cursor` and set `PartitionState{currentWindow: 1710676800, seqCounter: 247}` without scanning any `evt` or `meta` keys

#### Scenario: New partition with no cursor yields zero state

- **WHEN** `global:active_partitions` bit is set for pid `N` but `{N}:cursor` is missing (impossible in practice; defensive path)
- **THEN** `Init()` SHALL treat pid `N` as having zero state (`currentWindow=0, seqCounter=0`) and log a warning

### Requirement: Init cold-start latency bounded by active partition count

`Init()` SHALL complete in O(active_partitions) time. For 1000 active partitions, `Init()` SHALL use concurrent KV gets (errgroup) to read all cursors in parallel, targeting a cold-start latency under 500ms on a local RustViking instance.

#### Scenario: Parallel cursor reads on Init

- **WHEN** `Init()` discovers 1000 active partitions via bitmap
- **THEN** cursor reads SHALL be parallelized (at most `runtime.NumCPU() * 4` concurrent gets) rather than serial
