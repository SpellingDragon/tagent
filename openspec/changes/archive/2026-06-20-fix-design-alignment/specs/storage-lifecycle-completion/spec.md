## ADDED Requirements

### Requirement: L1 to L2 compaction is automatically scheduled

Compactor.checkAndCompact SHALL check L1 segment count per partition and trigger CompactL1ToL2 when the count reaches L1Threshold. The scheduling SHALL run in the existing schedulerLoop.

#### Scenario: L1 segments reach threshold

- **WHEN** a partition has 24 L1 segments (L1Threshold=24)
- **THEN** checkAndCompact triggers CompactL1ToL2, merging 24 L1 segments into 1 L2 segment

#### Scenario: L1 segments below threshold

- **WHEN** a partition has 10 L1 segments (L1Threshold=24)
- **THEN** checkAndCompact does not trigger L1→L2 compaction

### Requirement: L2 to L3 compaction is automatically scheduled

Compactor.checkAndCompact SHALL check L2 segment count per partition and trigger CompactL2ToL3 when the count reaches L2Threshold.

#### Scenario: L2 segments reach threshold

- **WHEN** a partition has 7 L2 segments (L2Threshold=7)
- **THEN** checkAndCompact triggers CompactL2ToL3, merging 7 L2 segments into 1 L3 segment

### Requirement: filterTombstoned removes tombstoned events

Compactor.filterTombstoned SHALL check each event against TombstoneSet and remove tombstoned events. The no-op stub SHALL be replaced with actual tombstone filtering.

#### Scenario: Tombstoned events in compaction input

- **WHEN** compaction input contains events [E1, E2, E3] and E2 is tombstoned
- **THEN** filterTombstoned returns [E1, E3]

#### Scenario: No tombstone set configured

- **WHEN** Compactor.tombstone is nil
- **THEN** filterTombstoned returns all events unchanged (safe degradation)

### Requirement: TTL check uses actual event timestamp

LifecycleManager.checkTTL SHALL parse the Timestamp field from event JSON for age calculation, not use the segment window timestamp approximation.

#### Scenario: Event near window boundary

- **WHEN** an event created at 10:59 has TTL of 1 hour, and checkTTL runs at 12:01
- **THEN** the event is marked tombstoned (age = ~2 hours > 1 hour TTL), because the actual Timestamp (10:59) is used, not the window timestamp (10:00)

#### Scenario: Event JSON unparseable

- **WHEN** event JSON cannot be unmarshaled
- **THEN** the event is skipped (not marked tombstoned, no crash)
