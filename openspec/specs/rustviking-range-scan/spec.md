# rustviking-range-scan Specification

## Purpose

本规范定义 rustviking-range-scan 能力。RustViking CLI SHALL expose a `kv range` subcommand that performs a native RocksDB range scan using byte-ordered key iteration.

## Requirements

### Requirement: RustViking CLI exposes native range scan

RustViking CLI SHALL expose a `kv range` subcommand that performs a native RocksDB range scan using byte-ordered key iteration. The command SHALL accept `--start` (inclusive), `--end` (exclusive), and `--limit` parameters, and return matching key-value pairs in key order.

#### Scenario: Range scan across time windows

- **WHEN** `rustviking kv range -s "1:evt:1710678000:" -e "1:evt:1710681600:" -l 50` is executed
- **THEN** all KV entries with keys in `[1:evt:1710678000:, 1:evt:1710681600:)` SHALL be returned, up to 50 entries, in RocksDB key order

#### Scenario: Range scan with empty result

- **WHEN** `rustviking kv range -s "42:evt:9999999:" -e "42:evt:9999999;"` is executed for a range with no keys
- **THEN** the response SHALL contain `"count": 0` and `"entries": []`

#### Scenario: Range scan output matches JSON format

- **WHEN** range scan returns results
- **THEN** the output SHALL follow the unified JSON format: `{"success":true,"data":{"start":"...","end":"...","count":N,"entries":[{"key":"...","value":"..."}]}}`

### Requirement: Go client uses range command instead of client-side filtering

`RustVikingClient.KVRange()` SHALL invoke `rustviking kv range` with the exact start/end parameters instead of computing a common prefix and filtering on the client side.

#### Scenario: KVRange calls kv range CLI command

- **WHEN** `KVRange("42:evt:1710678000:", "42:evt:1710681600:", 50)` is called
- **THEN** the CLI arguments SHALL be `["kv", "range", "-s", "42:evt:1710678000:", "-e", "42:evt:1710681600:", "-l", "50"]`

#### Scenario: KVRange with no common prefix does not error

- **WHEN** `KVRange("1:evt:1000:", "2:evt:2000:", 10)` is called (different partitions)
- **THEN** the call SHALL succeed via `kv range` command instead of returning an error about empty common prefix
