# production-wiring Specification

## Purpose

本规范定义 production-wiring 能力。`resolveMemoryStore("file")` in `tagent.go` SHALL create and start the complete lifecycle management stack in addition to the base FileSegmentStore.

## Requirements

### Requirement: Production wiring creates and starts all lifecycle subsystems

`resolveMemoryStore("file")` in `tagent.go` SHALL create and start the complete lifecycle management stack in addition to the base FileSegmentStore. This includes: calling `Init()` to recover seqCounter, initializing per-partition TombstoneSets, and starting LifecycleManager and Compactor background goroutines.

#### Scenario: FileSegmentStore is initialized before any writes

- **WHEN** `resolveMemoryStore("file")` creates a FileSegmentStore
- **THEN** `store.Init()` SHALL be called before the store is returned to any caller
- **AND** Init SHALL recover seqCounter and currentWindow via `global:active_partitions` bitmap + `{pid}:cursor` (see capability `startup-active-partition-bitmap`)

#### Scenario: TombstoneSet is available for lifecycle operations

- **WHEN** a FileSegmentStore is created for production use
- **THEN** each partition's TombstoneSet SHALL be lazily initialized with correct `pid`, `kv`, and `rel` references
- **AND** `RecoverFromKV()` SHALL be called automatically on first TombstoneSet access

#### Scenario: LifecycleManager starts scanning after wiring

- **WHEN** `resolveMemoryStore("file")` completes
- **THEN** a LifecycleManager SHALL be created with the store's TombstoneSet and default LifecycleConfig
- **AND** `LifecycleManager.Start()` SHALL be called, beginning the TTL/capacity scanner loop

#### Scenario: Compactor starts scheduling after wiring

- **WHEN** `resolveMemoryStore("file")` completes
- **THEN** a Compactor SHALL be created with the store's KV client, RelationStore, and default CompactionConfig
- **AND** `Compactor.Start()` SHALL be called, beginning the compaction scheduler loop

#### Scenario: LifecycleManager and Compactor are stoppable via Close

- **WHEN** `FileSegmentStore.Close()` is called
- **THEN** LifecycleManager SHALL be stopped gracefully
- **AND** Compactor SHALL be stopped gracefully
- **AND** all TombstoneSet dirty state SHALL be flushed to KV

### Requirement: Subsystem references injected via setter methods

FileSegmentStore SHALL provide `SetLifecycleManager(*LifecycleManager)` and `SetCompactor(*Compactor)` setter methods to receive subsystem references injected by the production wiring code, avoiding circular import dependencies.

#### Scenario: SetLifecycleManager stores reference for Close

- **WHEN** `store.SetLifecycleManager(lm)` is called
- **THEN** the store SHALL retain the reference for later use in `Close()`

#### Scenario: SetCompactor stores reference for Close

- **WHEN** `store.SetCompactor(c)` is called
- **THEN** the store SHALL retain the reference for later use in `Close()`

### Requirement: Close follows strict shutdown order

`FileSegmentStore.Close()` SHALL shut down subsystems in the following order, waiting for each step before proceeding:

1. `Compactor.Stop()` — wait for any in-flight compaction task to drain; no new compaction SHALL start
2. `LifecycleManager.Stop()` — stop TTL/capacity scanner goroutines
3. Flush all dirty `TombstoneSet` state to KV via an atomic batch per partition
4. `RelationStore.Close()` — flush LRU dirty entries (if any) and sync RocksDB WAL (see capability `relation-store-lru-rocks`)

A failure at any step SHALL be logged but SHALL NOT abort the remaining steps; the method returns a joined error describing all failures.

#### Scenario: Close blocks on in-flight compaction

- **WHEN** `Close()` is invoked while a compaction task is running
- **THEN** `Close()` SHALL wait until the compaction task returns before proceeding to stop the LifecycleManager

#### Scenario: Close is idempotent

- **WHEN** `Close()` is invoked a second time
- **THEN** it SHALL return the same outcome as the first call (typically `nil`) without re-executing the shutdown sequence
