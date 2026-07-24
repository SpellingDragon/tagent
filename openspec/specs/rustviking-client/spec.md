# rustviking-client Specification

## Purpose

本规范定义 rustviking-client 能力。The system SHALL support range-based scanning via `KVRange(start, end, limit)`.

## Requirements

### Requirement: KV range scan for time-based queries

The system SHALL support range-based scanning via `KVRange(start, end, limit)`. This SHALL invoke the RustViking CLI `kv range` subcommand (native RocksDB range iteration), which returns only keys within [start, end). The client SHALL NOT perform client-side common-prefix filtering.

#### Scenario: Query events in a time range

- **WHEN** `KVRange(start="42:evt:1710678000:", end="42:evt:1710681600:", limit=50)` is called
- **THEN** RustViking CLI `kv range -s "42:evt:1710678000:" -e "42:evt:1710681600:" -l 50` SHALL be invoked, returning only keys in the specified range

#### Scenario: KVRange across partition boundaries

- **WHEN** `KVRange(start="1:evt:1000:", end="2:evt:2000:", limit=10)` is called
- **THEN** the call SHALL succeed via native range scan instead of failing due to empty common prefix

### Requirement: KV batch operations for compaction

The system SHALL support atomic batch write operations via `KVBatch(ops)`. The RustViking CLI `kv batch` command SHALL use RocksDB `WriteBatch` to ensure all operations in the batch are committed atomically. This SHALL be used during compaction to write the merged output segment, index entries, and delete source segments in a single atomic operation.

#### Scenario: Atomic compaction batch

- **WHEN** `KVBatch(ops)` is called with 500 put operations (new segment + index) and 24 delete operations (old segments)
- **THEN** either all operations SHALL succeed atomically via RocksDB WriteBatch, or the batch SHALL fail and no partial changes SHALL be applied


### Requirement: RustViking CLI exposes kv range subcommand

The RustViking CLI SHALL expose a `kv range` subcommand accepting `--start` (inclusive), `--end` (exclusive), and `--limit` parameters, performing a native RocksDB range scan and returning matching entries in key order with the unified JSON response format.

#### Scenario: kv range returns matching entries

- **WHEN** `rustviking kv range -s "1:evt:100:" -e "1:evt:200:" -l 10` is executed
- **THEN** only KV entries with keys in [1:evt:100:, 1:evt:200:) SHALL be returned, up to 10 entries

### Requirement: RustVikingClient is CLI-only

`RustVikingClient` SHALL integrate with RustViking **exclusively via local CLI** (`exec.Cmd`). The client SHALL NOT expose a `mode` configuration, a `server` / `gRPC` / `HTTP` branch, or any `ErrServerNotImplemented` placeholder. All KV operations SHALL dispatch directly to `rustviking <subcommand>` subprocess.

#### Scenario: Client construction accepts only CLI config

- **WHEN** `NewRustVikingClient(cfg)` is called
- **THEN** `cfg` SHALL expose only `BinaryPath` and `ConfigPath`; no `Mode` / `ServerAddr` / `Endpoint` fields SHALL exist

#### Scenario: All KV operations fork CLI subprocess

- **WHEN** any `KVPut` / `KVGet` / `KVDelete` / `KVScan` / `KVRange` / `KVBatch` is invoked
- **THEN** the call SHALL `exec.Cmd` the `rustviking` binary with the corresponding subcommand and parse the unified JSON envelope response

### Requirement: Batch operations are atomic

All `KVBatch(ops)` invocations SHALL translate to a single `rustviking kv batch` CLI call, which on the RustViking side SHALL use `KvStore::batch().commit()` (WriteBatch). Partial success SHALL NOT be observable: either every put/delete is persisted, or none.

#### Scenario: Batch failure rolls back all operations

- **WHEN** a batch of 100 puts encounters an IO error on operation 50
- **THEN** operations 0..49 SHALL NOT be observable in subsequent reads; the call SHALL return a non-nil error
