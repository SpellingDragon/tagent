## ADDED Requirements

### Requirement: TombstoneSet is per-partition

`FileSegmentStore` SHALL maintain a separate `TombstoneSet` instance for each partition, ensuring tombstone keys are persisted under the correct partition prefix (`{pid}:tomb:{event_key}`). The mapping SHALL be lazy-initialized on first access.

#### Scenario: Tombstone for partition 2 uses partition 2's prefix

- **WHEN** `MarkTombstone(key)` is called for an event in partition 2
- **THEN** the tombstone KV key SHALL be `2:tomb:{event_key}`, NOT `1:tomb:{event_key}`

#### Scenario: Tombstone for partition 1 is invisible to partition 2

- **WHEN** `IsTombstone(key)` is called for an event in partition 1 using partition 2's TombstoneSet
- **THEN** the result SHALL be false regardless of partition 1's tombstone state

#### Scenario: Lazy initialization of per-partition TombstoneSet

- **WHEN** `getTombstoneSet(pid=99)` is called for the first time
- **THEN** a new `TombstoneSet` SHALL be created with `pid=99` and `kv`/`rel` from the store, and `RecoverFromKV()` SHALL be called automatically

### Requirement: TombstoneSet auto-recovers from KV on initialization

When a per-partition `TombstoneSet` is lazy-initialized, it SHALL automatically call `RecoverFromKV()` to restore tombstone state from the KV store, without requiring the caller to invoke recovery explicitly.

#### Scenario: Recover tombstones after restart

- **WHEN** tagent restarts after marking events 100 and 200 as tombstoned in partition 1
- **THEN** on first access to partition 1's TombstoneSet, `RecoverFromKV()` SHALL scan `1:tomb:` prefix and restore both keys to the in-memory set
