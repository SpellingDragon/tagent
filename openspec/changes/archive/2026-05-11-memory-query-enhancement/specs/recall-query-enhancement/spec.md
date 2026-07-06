## ADDED Requirements

### Requirement: memory_query supports time range filtering

The `memory_query` tool SHALL accept optional `since` and `until` parameters as Unix millisecond timestamps. When provided, the query SHALL be filtered to events whose `Timestamp` falls within [since, until]. When both are omitted, behavior SHALL be identical to current (no time filtering).

The `memory_recent` tool SHALL also accept optional `since` and `until` parameters with identical semantics.

#### Scenario: Query with time range

- **WHEN** LLM calls `memory_query` with `since: 1715000000000, until: 1715086400000`
- **THEN** only events with timestamp within that range are returned, sorted by `timestamp_desc`

#### Scenario: Query without time range (backward compatible)

- **WHEN** LLM calls `memory_query` without `since` or `until`
- **THEN** behavior is identical to current implementation — all matching events returned

#### Scenario: Query with only since

- **WHEN** LLM calls `memory_query` with `since: 1715000000000` (no `until`)
- **THEN** only events at or after the since timestamp are returned

#### Scenario: Invalid since > until

- **WHEN** LLM calls `memory_query` with `since > until`
- **THEN** the tool SHALL return an error message explaining the invalid range

---

### Requirement: memory_trace traverses ParentKey causal chain

The system SHALL provide a new `memory_trace` tool. Given an `event_key` and optional `max_steps` (default 10, max 20), it SHALL walk backward along the `ParentKey` chain by repeatedly calling `GetEvent(parentKey)`, returning the chain of events from newest (the given key) to oldest (where ParentKey=0 or max_steps reached).

#### Scenario: Trace a causal chain

- **WHEN** LLM calls `memory_trace(key=12345, max_steps=5)`
- **THEN** returns up to 5 events: the event at key 12345, its parent, parent's parent, etc., in newest-to-oldest order

#### Scenario: Trace reaches root

- **WHEN** LLM calls `memory_trace(key=12345)` and the chain has 3 events before reaching ParentKey=0
- **THEN** returns exactly 3 events (stops at root, no error)

#### Scenario: Trace with non-existent key

- **WHEN** LLM calls `memory_trace(key=99999)` where the key does not exist
- **THEN** returns an error: "event not found"

#### Scenario: Trace respects max_steps limit

- **WHEN** LLM calls `memory_trace(key=12345, max_steps=20)` and chain length exceeds 20
- **THEN** returns exactly 20 events, with a note that the chain continues

---

### Requirement: memory_get supports optional parent inclusion

The `memory_get` tool SHALL accept an optional `include_parent` boolean parameter. When `true`, the result SHALL include the parent event's summary (EventSummary, EventType, Timestamp) alongside the requested event, without requiring a separate call.

#### Scenario: Get event with parent

- **WHEN** LLM calls `memory_get(key=12345, include_parent=true)` and the event has ParentKey=12300
- **THEN** result includes both the event at 12345 and a `parent` field with the summary of event 12300

#### Scenario: Get event without parent (backward compatible)

- **WHEN** LLM calls `memory_get(key=12345)` without `include_parent`
- **THEN** behavior is identical to current — only the requested event is returned

#### Scenario: Get event with parent when no parent exists

- **WHEN** LLM calls `memory_get(key=12345, include_parent=true)` and ParentKey=0
- **THEN** result includes the event with `parent` field set to null/absent
