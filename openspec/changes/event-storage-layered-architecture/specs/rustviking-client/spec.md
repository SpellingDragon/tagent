## ADDED Requirements

### Requirement: KV put and get operations via RustViking CLI

The system SHALL provide a Go client (`RustVikingClient`) that executes KV storage operations by invoking the RustViking CLI binary with JSON-formatted input/output. `KVPut(key, value)` SHALL store a key-value pair. `KVGet(key)` SHALL retrieve the value by key.

#### Scenario: Store an event via KVPut

- **WHEN** `KVPut(key="42:evt:1710678000:0", value={"event_key":...})` is called
- **THEN** RustViking CLI `kv put` command SHALL be executed with the key and value, and the operation SHALL succeed with exit code 0

#### Scenario: Retrieve an event via KVGet

- **WHEN** `KVGet(key="42:evt:1710678000:0")` is called
- **THEN** RustViking CLI `kv get` command SHALL return the stored JSON value

#### Scenario: KVGet on non-existent key

- **WHEN** `KVGet(key="42:evt:9999999:0")` is called for a key that was never stored
- **THEN** the operation SHALL return an error indicating key not found

### Requirement: KV prefix scan for segment listing

The system SHALL support prefix-based scanning via `KVScan(prefix, limit)`. This SHALL be used to list all segments in a partition (prefix `{pid}:meta:`), list all events in a segment (prefix `{pid}:evt:{window_ts}:`), and list all tombstones (prefix `{pid}:tomb:`).

#### Scenario: List all segments in a partition

- **WHEN** `KVScan(prefix="42:meta:", limit=100)` is called
- **THEN** all keys matching the prefix SHALL be returned, with their values, up to the limit

#### Scenario: List events within a segment

- **WHEN** `KVScan(prefix="42:evt:1710678000:", limit=1000)` is called
- **THEN** all event entries for that time window SHALL be returned in key order

### Requirement: KV range scan for time-based queries

The system SHALL support range-based scanning via `KVRange(start, end, limit)`. This SHALL be used to query events within a time range, taking advantage of RocksDB's byte-ordered keys where timestamps are embedded in the key.

#### Scenario: Query events in a time range

- **WHEN** `KVRange(start="42:evt:1710678000:", end="42:evt:1710681600:", limit=50)` is called
- **THEN** all event entries with keys in [start, end) SHALL be returned, up to the limit

### Requirement: KV batch operations for compaction

The system SHALL support atomic batch write operations via `KVBatch(ops)`. This SHALL be used during compaction to: write the merged output segment, write the new `.idx` entries, and delete the source segments—all in a single atomic operation.

#### Scenario: Atomic compaction batch

- **WHEN** `KVBatch(ops)` is called with 500 put operations (new segment + index) and 24 delete operations (old segments)
- **THEN** either all operations SHALL succeed atomically, or the batch SHALL fail and no partial changes SHALL be applied

### Requirement: CLI invocation with JSON parsing

The system SHALL invoke RustViking CLI via `exec.Cmd` with the `-o json` flag. The CLI's JSON response (`{"success": true, "data": ...}` or `{"success": false, "error": ...}`) SHALL be parsed into Go structs. Exit code 0 SHALL indicate success; exit code 1 SHALL indicate user error; exit code 2 SHALL indicate system error.

#### Scenario: Successful CLI invocation

- **WHEN** RustViking CLI exits with code 0 and JSON `{"success": true, "data": "..."}`,
- **THEN** the Go client SHALL return the parsed data without error

#### Scenario: CLI invocation failure

- **WHEN** RustViking CLI exits with code 2 and JSON `{"success": false, "error": "RocksDB IO error"}`
- **THEN** the Go client SHALL return a Go error wrapping the CLI error message

#### Scenario: CLI binary not found

- **WHEN** the RustViking binary is not in PATH
- **THEN** the Go client SHALL return a clear error message indicating the binary is missing
