# relation-store-lru-rocks Specification

## Purpose

本规范定义 relation-store-lru-rocks 能力。`RelationStore` SHALL maintain an in-memory LRU cache (default 1M entries) for parent lookups, with RocksDB as the authoritative persistent store.

## Requirements

### Requirement: RelationStore uses LRU hot cache backed by RocksDB

`RelationStore` SHALL maintain an in-memory LRU cache (default 1M entries) for parent lookups, with RocksDB as the authoritative persistent store. Independent journal files and snapshot files SHALL NOT be used; RocksDB WAL SHALL provide crash durability.

#### Scenario: SetParent writes through to RocksDB before updating LRU

- **WHEN** `SetParent(child=100, parent=50)` is called
- **THEN** the write SHALL first commit to RocksDB via a single WriteBatch containing `{pid}:rel:100` → entry and `{pid}:revrel:50:100` → empty marker, then update the LRU hot cache
- **AND** if the RocksDB write fails, the LRU SHALL NOT be updated

#### Scenario: GetParent cache hit avoids KV roundtrip

- **WHEN** `GetParent(100)` is called and child `100` is in the LRU
- **THEN** the parent SHALL be returned without any KV get

#### Scenario: GetParent cache miss falls back to RocksDB

- **WHEN** `GetParent(100)` is called and child `100` is not in the LRU
- **THEN** the LRU SHALL issue a KV get for `{pid}:rel:100`, decode the entry, insert it into the LRU, and return the parent

### Requirement: GetChildren uses reverse index with range scan + limit

`RelationStore.GetChildren(parent int64, limit int)` SHALL return `([]int64, hasMore bool, error)`. Implementation SHALL use RocksDB range scan over `{pid}:revrel:{parent}:` prefix, reading at most `limit+1` entries to determine `hasMore`.

#### Scenario: GetChildren returns all when count <= limit

- **WHEN** parent `50` has 3 children (100, 101, 102) and `GetChildren(50, 10)` is called
- **THEN** the result SHALL be `([100, 101, 102], hasMore=false, nil)`

#### Scenario: GetChildren returns bounded with hasMore=true

- **WHEN** parent `50` has 1000 children and `GetChildren(50, 100)` is called
- **THEN** the result SHALL be `(first 100 children, hasMore=true, nil)`

### Requirement: RelationStore drops journal/snapshot methods

`RelationStore` interface SHALL NOT expose `Snapshot()`, `LoadSnapshot()`, or `ReplayJournal()`. Existing callers of these methods SHALL be removed.

#### Scenario: Interface definition excludes journal methods

- **WHEN** the `RelationStore` interface is examined
- **THEN** no method named `Snapshot`, `LoadSnapshot`, or `ReplayJournal` SHALL be present

### Requirement: RelationStore Close flushes dirty LRU entries

`RelationStore.Close()` SHALL ensure any LRU entries with unpersisted updates (should not occur under write-through semantics, but defensive) are flushed to RocksDB before returning.

#### Scenario: Close completes after WAL sync

- **WHEN** `Close()` is called
- **THEN** the underlying RocksDB WAL SHALL be synced before the method returns
