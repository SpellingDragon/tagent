# adaptive-segment-size Specification

## Purpose

本规范定义 adaptive-segment-size 能力。`PartitionDefaults` SHALL expose `max_events_per_segment` (default `10000`).

## Requirements

### Requirement: Segment seals early when event count reaches threshold

`PartitionDefaults` SHALL expose `max_events_per_segment` (default `10000`). Before accepting a new event via `StoreEvent()` or processing a batch via `StoreEvents()`, the system SHALL check the current segment's event count. If it has reached or exceeded `max_events_per_segment`, the segment SHALL be sealed prematurely (before the window boundary) and a new window SHALL start for subsequent events.

#### Scenario: Burst writes trigger early seal

- **WHEN** partition `1` is writing in window `1710676800` with `max_events_per_segment=10000` and reaches the 10000th event
- **THEN** the current segment SHALL be sealed with `Sealed=true, Layer=1`, and the next event SHALL start a new window (could be the same wall-clock window with a bumped internal counter, or the next hour if crossed)

#### Scenario: Normal flow unaffected when under threshold

- **WHEN** partition `1` writes 500 events in a window (well under 10000)
- **THEN** no early seal SHALL occur; the segment rolls over on the normal hour boundary

### Requirement: Compaction honors max_events_per_segment at L2/L3

`CompactL1ToL2()` and `CompactL2ToL3()` SHALL split merged output into multiple segments if the merged event count exceeds `max_events_per_segment`. Each output segment SHALL carry a distinct sequential identifier so downstream range scans remain O(events_in_range).

#### Scenario: L1→L2 merge produces multiple daily segments

- **WHEN** 24 L1 segments each with 10000 events are merged (240000 total) and `max_events_per_segment=10000`
- **THEN** the L2 output SHALL be 24 daily segments, each with ≤ 10000 events, not a single 240000-event segment

### Requirement: Configuration is per-partition overridable

`max_events_per_segment` SHALL be overridable per partition via `PartitionDefaults.overrides[pid].max_events_per_segment`. This enables hot partitions to use a smaller threshold (earlier seals, smaller segments) without affecting quiet partitions.

#### Scenario: Per-partition override takes effect

- **WHEN** partition `42` has override `max_events_per_segment: 2000` while the default is `10000`
- **THEN** partition `42` SHALL seal segments at 2000 events; other partitions SHALL use 10000
