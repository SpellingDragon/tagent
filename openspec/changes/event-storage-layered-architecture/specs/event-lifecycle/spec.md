## ADDED Requirements

### Requirement: Time-based TTL expiration

The system SHALL support a time-to-live (TTL) policy that marks events as tombstoned when their age exceeds a configurable threshold. The TTL SHALL be configurable per partition and per event type. Expired events SHALL be marked tombstone but NOT immediately deleted—physical deletion SHALL occur during the next compaction cycle.

#### Scenario: Event expires by global TTL

- **WHEN** the system scans events and finds an event older than the configured `ttl_days` (default: 7)
- **THEN** the event SHALL be marked as tombstoned

#### Scenario: Event type-specific TTL overrides global TTL

- **WHEN** `external_input` has `type_ttl: 30` and the event is 10 days old (within type TTL but beyond global TTL of 7)
- **THEN** the event SHALL NOT be tombstoned (type-specific TTL takes precedence)

#### Scenario: Low-value event expires sooner

- **WHEN** `context_compress` has `type_ttl: 3` and the event is 4 days old
- **THEN** the event SHALL be marked as tombstoned

### Requirement: Capacity-based eviction

The system SHALL support a maximum event count or maximum disk size per partition. When a partition exceeds the configured threshold, the oldest events SHALL be marked tombstone until the partition is back within limits.

#### Scenario: Evict oldest events on capacity overflow

- **WHEN** a partition exceeds `max_events: 100000` and has 100,050 events
- **THEN** the oldest 50+ events SHALL be marked tombstone, bringing the live event count below the threshold

#### Scenario: Eviction preserves causal chain integrity

- **WHEN** the oldest event (E1) is marked tombstone and is the parent of E2 (which is not being evicted)
- **THEN** E2's parent reference SHALL be repaired to point to E1's nearest alive ancestor before E1 is tombstoned

### Requirement: Tombstone lifecycle management

The system SHALL manage the full lifecycle of tombstoned events: mark (lazy or on schedule), filter (during reads and queries), repair (fix dangling references), and purge (physical removal during compaction). Tombstones SHALL be invisible to `QueryEvents` and `GetEvent` results.

#### Scenario: Tombstoned event is invisible to queries

- **WHEN** event E1 is marked tombstone and `QueryEvents()` is called
- **THEN** E1 SHALL NOT appear in the results

#### Scenario: GetEvent on tombstoned key returns not found

- **WHEN** `GetEvent(key=tombstonedKey)` is called
- **THEN** an error SHALL be returned as if the event does not exist

#### Scenario: Tombstone marking triggers dangling reference repair

- **WHEN** `MarkTombstone(key=E3)` is called and E3 has children [E4, E5]
- **THEN** before the tombstone is set, `SetParent(E4, aliveAncestorOf(E3))` and `SetParent(E5, aliveAncestorOf(E3))` SHALL be called to repair dangling references
