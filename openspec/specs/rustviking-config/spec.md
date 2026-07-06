## ADDED Requirements

### Requirement: MemoryConfig supports RustViking binary path

The `MemoryConfig` struct SHALL include an optional `RustVikingBinary` field to specify the path to the rustviking CLI binary. When empty, the system SHALL default to `"rustviking"` (looked up via PATH).

#### Scenario: Binary path explicitly configured
- **WHEN** `rustviking_binary` is set to `/usr/local/bin/rustviking` in config
- **THEN** the system SHALL use `/usr/local/bin/rustviking` for CLI calls

#### Scenario: Binary path not configured
- **WHEN** `rustviking_binary` is empty or not set
- **THEN** the system SHALL default to `"rustviking"` and look it up via PATH

### Requirement: Production MemoryStore wiring uses real RustVikingClient

When `MemoryConfig.Type` is `"file"`, `resolveMemoryStore` SHALL create a `FileSegmentStore` backed by `RustVikingClient` (real CLI) and `InMemRelationStore` (WAL + snapshot persistence), NOT `MockRustVikingClient` or `simpleInMemRelationStore`.

#### Scenario: File type creates real client
- **WHEN** `type: file` with `path: /data/events` and `rustviking_binary` set
- **THEN** the system SHALL create `NewRustVikingClient(binaryPath, dataDir)` and `NewInMemRelationStore(dataDir)`
- **AND** events SHALL be persisted to RocksDB via rustviking CLI

#### Scenario: File type without binary path
- **WHEN** `type: file` with `rustviking_binary` empty
- **THEN** the system SHALL create `NewRustVikingClient("", dataDir)` using default binary name
