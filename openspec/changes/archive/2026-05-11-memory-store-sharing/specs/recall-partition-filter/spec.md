## ADDED Requirements

### Requirement: Recall sub-tools accept optional PartitionID filter

The recall agent's sub-tools (`memory_query`, `memory_recent`) SHALL accept an optional `partition_ids` parameter. When provided, the query SHALL be scoped to only the specified partition IDs. When omitted, behavior SHALL remain unchanged (query across all partitions in the store).

#### Scenario: memory_query with partition_ids

- **WHEN** the LLM calls `memory_query` with args `{"query": "deployment", "partition_ids": [201, 202]}`
- **THEN** the `QueryEvents` call to MemoryStore includes `PartitionIDs: [201, 202]`, and results are limited to those partitions

#### Scenario: memory_query without partition_ids

- **WHEN** the LLM calls `memory_query` with args `{"query": "deployment"}` (no `partition_ids`)
- **THEN** the `QueryEvents` call to MemoryStore has no PartitionID filter, returning results from all partitions

#### Scenario: memory_recent with partition_ids

- **WHEN** the LLM calls `memory_recent` with args `{"limit": 5, "partition_ids": [387]}`
- **THEN** the `QueryEvents` call to MemoryStore includes `PartitionIDs: [387]`, and results are limited to that partition

### Requirement: memory_get remains unchanged

The recall agent's `memory_get` sub-tool SHALL NOT require a `partition_ids` parameter. Its `GetEvent` call uses the EventKey which self-encodes the PartitionID, making an explicit partition filter redundant.

#### Scenario: memory_get by key

- **WHEN** the LLM calls `memory_get` with args `{"key": 12345}`
- **THEN** the `GetEvent` call retrieves the event by key directly, without needing partition filtering

### Requirement: Backward compatibility of sub-tool signatures

All recall sub-tools SHALL remain backward compatible. Existing callers that do not pass `partition_ids` SHALL observe no change in behavior.

#### Scenario: Existing caller without partition_ids

- **WHEN** `memory_query` is called with `{"query": "test", "limit": 5}` (no `partition_ids`)
- **THEN** behavior is identical to before the change
