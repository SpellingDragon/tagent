## MODIFIED Requirements

### Requirement: Time-based TTL expiration

The system SHALL support a time-to-live (TTL) policy that marks events as tombstoned when their age exceeds a configurable threshold. The TTL SHALL be configurable per partition and per event type. Expired events SHALL be marked tombstone but NOT immediately deleted—physical deletion SHALL occur during the next compaction cycle. The TTL scanner SHALL correctly extract the EventKey from the event's JSON value (not from the KV key) to perform tombstone checks, and SHALL use the event's actual `timestamp` field (not the window timestamp) for age calculation.

#### Scenario: Event expires by global TTL

- **WHEN** the system scans events and finds an event older than the configured `ttl_days` (default: 7)
- **THEN** the event SHALL be marked as tombstoned using its EventKey extracted from the event JSON value

#### Scenario: Event type-specific TTL overrides global TTL

- **WHEN** `external_input` has `type_ttl: 30` and the event is 10 days old (within type TTL but beyond global TTL of 7)
- **THEN** the event SHALL NOT be tombstoned (type-specific TTL takes precedence)

#### Scenario: Low-value event expires sooner

- **WHEN** `context_compress` has `type_ttl: 3` and the event is 4 days old
- **THEN** the event SHALL be marked as tombstoned

#### Scenario: TTL scanner correctly obtains EventKey from event value

- **WHEN** the TTL scanner processes a KV pair `{pid}:evt:{window_ts}:{seq}` → `{"event_key": 1777198738547555000, "event_type": "...", "timestamp": 1710678000000, ...}`
- **THEN** the scanner SHALL extract `event_key` from the JSON value (not from the KV key) and use it for `IsTombstone()` check and `MarkTombstone()` call

#### Scenario: TTL age calculated from actual timestamp

- **WHEN** the TTL scanner processes an event with `timestamp: 1710678000000` (07:00:00) stored in window `1710676800` (07:00:00 aligned)
- **THEN** the event age SHALL be calculated using the actual `timestamp` field (1710678000000), NOT the window timestamp (1710676800 * 1000)

### Requirement: Capacity-based eviction

The system SHALL support a maximum event count or maximum disk size per partition. When a partition exceeds the configured threshold, the oldest events SHALL be marked tombstone until the partition is back within limits. The eviction scanner SHALL correctly extract the EventKey from the event's JSON value (not from the KV key).

#### Scenario: Evict oldest events on capacity overflow

- **WHEN** a partition exceeds `max_events: 100000` and has 100,050 events
- **THEN** the oldest 50+ events SHALL be marked tombstone, bringing the live event count below the threshold

#### Scenario: Eviction preserves causal chain integrity

- **WHEN** the oldest event (E1) is marked tombstone and is the parent of E2 (which is not being evicted)
- **THEN** E2's parent reference SHALL be repaired to point to E1's nearest alive ancestor before E1 is tombstoned

#### Scenario: Eviction scanner correctly obtains EventKey

- **WHEN** the eviction scanner processes events to find the oldest ones
- **THEN** the scanner SHALL extract `event_key` from each event's JSON value and use it for `IsTombstone()` check and `MarkTombstone()` call

## ADDED Requirements

### Requirement: TTL scan progresses via per-partition cursor

TTL scanning SHALL be driven by the `{pid}:ttl_cursor` capability defined in `ttl-cursor-scan`. Per-partition `checkTTL` SHALL NOT perform full scans of all `{pid}:meta:*` or `{pid}:evt:*` keys. The scanner SHALL start from `ttl_cursor.next_scan_window`, advance only past expired windows, and persist the updated cursor before returning.

#### Scenario: TTL scan respects cursor

- **WHEN** `checkTTL(1)` runs and partition 1's `ttl_cursor.next_scan_window = 1710003600`
- **THEN** scanning SHALL begin at window `1710003600`; windows earlier than this SHALL NOT be re-examined

### Requirement: Capacity eviction avoids full-partition scan

`evictOldest` SHALL rely on `PartitionState.eventCount` (maintained per `event-segment-store` capability) to determine over-capacity state, and SHALL scan event keys only within the oldest window(s) discovered via `ttl_cursor.next_scan_window` minus one, not across all historical windows. This keeps capacity-based eviction cost bounded even when a partition holds millions of events.

#### Scenario: Eviction scans oldest window first

- **WHEN** partition `1` has 100050 events across 24 hourly windows and `max_events = 100000`
- **THEN** `evictOldest` SHALL scan events starting from the oldest window, tombstone sufficient events to drop `eventCount` below the threshold, and stop without scanning newer windows
