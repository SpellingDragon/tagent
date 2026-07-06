## ADDED Requirements

### Requirement: Store event as JSON Lines segment by time window

The system SHALL persist events in time-windowed segment files (JSON Lines format, one event per line, no indentation). Each segment SHALL cover a fixed time window (default: 1 hour, aligned to `timestamp / windowSize * windowSize`). Events SHALL be appended to the current active segment in write order.

#### Scenario: Append event to active segment

- **WHEN** `MemoryPlugin.onEvent()` calls `StoreEvent(fullEvent)`
- **THEN** the event is serialized as a single JSON line and appended to the active segment file with a trailing `\n`

#### Scenario: Active segment auto-seals on hour boundary

- **WHEN** the current time crosses an hour boundary (e.g., 14:00:00)
- **THEN** the active segment file SHALL be closed, an `.idx` index file SHALL be built from it, and a new active segment file SHALL be created for the new hour

#### Scenario: Seal produces offset index file

- **WHEN** an active segment is sealed
- **THEN** a `.idx` file SHALL be created containing `eventKey:byteOffset` pairs, sorted by EventKey, for binary search lookup within the segment

### Requirement: Get event by EventKey with O(log N) lookup

The system SHALL retrieve a single event by its EventKey. The lookup SHALL first check the EventCache (LRU), then derive the segment file name from the EventKey's embedded timestamp, then use the `.idx` index file for binary search to locate the byte offset, and finally read and deserialize the single JSON line.

#### Scenario: Get event in active segment

- **WHEN** `GetEvent(key)` is called for an event still in the active segment (no `.idx` file exists)
- **THEN** the active segment SHALL be scanned sequentially to find the matching event

#### Scenario: Get event in sealed L1 segment

- **WHEN** `GetEvent(key)` is called for an event in a sealed L1 segment
- **THEN** the `.idx` file SHALL be binary-searched to locate the byte offset, and only the single matching line SHALL be read from the segment file

#### Scenario: Get event not found

- **WHEN** `GetEvent(key)` is called for a non-existent EventKey
- **THEN** an error SHALL be returned indicating the event was not found

#### Scenario: Get event from EventCache

- **WHEN** `GetEvent(key)` is called and the event exists in EventCache LRU
- **THEN** the cached event SHALL be returned immediately without any file IO

### Requirement: Query events by time range with segment-level pruning

The system SHALL support querying events by time range (`StartTime`/`EndTime`). Only segment files whose time windows overlap the query range SHALL be scanned. Segments outside the range SHALL be skipped via their `meta.json` min/max timestamp.

#### Scenario: Query events in last 2 hours

- **WHEN** `QueryEvents(StartTime=T-2h, EndTime=T, Limit=10)` is called
- **THEN** only the active segment and up to 2 L1 segments covering the 2-hour window SHALL be scanned, and at most 10 matching events SHALL be returned

#### Scenario: Query events with keyword filter across multiple segments

- **WHEN** `QueryEvents(Keyword="deploy", StartTime=T-24h, EndTime=T)` is called
- **THEN** all segments within the 24-hour range SHALL be scanned, events matching the keyword SHALL be collected, and results SHALL be sorted by Timestamp descending, truncated to Limit

#### Scenario: Query with short-circuit termination

- **WHEN** `QueryEvents(Limit=10)` is called and 10 matching events are found before scanning all eligible segments
- **THEN** the scan SHALL terminate immediately without reading remaining segments

### Requirement: Segment file format integrity on crash

The system SHALL ensure that an incomplete write to an active segment file (due to crash) does not corrupt the segment. On restart, the last incomplete line (lacking a trailing `\n`) SHALL be truncated.

#### Scenario: Recover from crash during active segment write

- **WHEN** tagent restarts after a crash that occurred mid-write to the active segment
- **THEN** the active segment SHALL be truncated to the last complete JSON line (ending with `\n`), and the incomplete trailing data SHALL be discarded

### Requirement: Partition-level concurrency

The system SHALL allow concurrent writes to different PartitionIDs without blocking each other. Each partition SHALL have its own active segment file and independent mutex.

#### Scenario: Concurrent writes to different partitions

- **WHEN** Partition 42 and Partition 99 simultaneously write events
- **THEN** both writes SHALL proceed without blocking each other
