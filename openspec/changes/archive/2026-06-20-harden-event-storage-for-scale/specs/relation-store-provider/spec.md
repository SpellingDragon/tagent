## MODIFIED Requirements

### Requirement: RelationStoreProvider interface defined

The `memory` package SHALL define a `RelationStoreProvider` interface with a single method `RelationStore() RelationStore`, providing a named, discoverable way to access the underlying `RelationStore` from any `MemoryStore` implementation that supports it. Callers SHALL use this interface for ALL relationship operations (SetParent, GetParent, GetChildren), rather than calling methods directly on MemoryStore.

#### Scenario: FileSegmentStore implements RelationStoreProvider

- **WHEN** a `*FileSegmentStore` is type-asserted to `RelationStoreProvider`
- **THEN** the assertion SHALL succeed
- **AND** `RelationStore()` SHALL return the store's underlying `RelationStore`

#### Scenario: InMemoryStore implements RelationStoreProvider

- **WHEN** an `*InMemoryStore` is type-asserted to `RelationStoreProvider`
- **THEN** the assertion SHALL succeed
- **AND** `RelationStore()` SHALL return the store's underlying `RelationStore`

### Requirement: Callers use RelationStoreProvider instead of anonymous interface

All call sites that currently use anonymous interface type assertions (e.g., `interface{ RelationStore() memory.RelationStore }`) SHALL be replaced with the named `memory.RelationStoreProvider` interface.

#### Scenario: MemoryPlugin sets parent via RelationStoreProvider

- **WHEN** `MemoryPlugin.onEvent` needs to call `SetParent(eventKey, parentKey)`
- **THEN** it SHALL type-assert `p.memStore` to `memory.RelationStoreProvider`
- **AND** call `rsp.RelationStore().SetParent(eventKey, parentKey)` on success

#### Scenario: RecallGetTool reads parent via RelationStoreProvider

- **WHEN** `NewRecallGetTool` handler needs to read the parent key
- **THEN** it SHALL type-assert `accessor` to `memory.RelationStoreProvider`
- **AND** call `rsp.RelationStore().GetParent(key)` on success

#### Scenario: RecallTraceTool traces via RelationStoreProvider

- **WHEN** `NewRecallTraceTool` handler traces the causal chain
- **THEN** it SHALL type-assert `accessor` to `memory.RelationStoreProvider`
- **AND** call `rsp.RelationStore().GetParent(key)` for each step

## REMOVED Requirements

### Requirement: GetParent/GetChildren on MemoryStore interface

**Reason**: Relationship operations (GetParent, GetChildren) are semantically distinct from event CRUD operations. They belong on RelationStore, accessed through RelationStoreProvider. Having them on MemoryStore conflates two orthogonal concerns.

**Migration**: Replace `store.GetParent(key)` with type assertion to `RelationStoreProvider` and call `rsp.RelationStore().GetParent(key)`. Replace `store.GetChildren(key)` with `rsp.RelationStore().GetChildren(key, limit)` — note the new mandatory `limit` parameter.

### Requirement: Snapshot / LoadSnapshot / ReplayJournal on RelationStore interface

**Reason**: The RelationStore v2 architecture uses LRU + RocksDB write-through persistence (see capability `relation-store-lru-rocks`). RocksDB's WAL provides the crash-durability guarantees that the legacy independent journal file offered. The `Snapshot`, `LoadSnapshot`, and `ReplayJournal` methods are therefore unused and SHALL be removed from the interface.

**Migration**: Delete all calls to these methods. Crash recovery is now handled by RustViking (RocksDB WAL) automatically; no explicit replay is required.

## ADDED Requirements

### Requirement: GetChildren takes a mandatory limit and returns hasMore

`RelationStore.GetChildren` SHALL have the signature `GetChildren(parent int64, limit int) (children []int64, hasMore bool, err error)`. A positive `limit` is mandatory; a value of 0 or negative SHALL return an error. `hasMore` SHALL be `true` when more children exist beyond `limit`, enabling callers to paginate.

#### Scenario: GetChildren returns bounded result

- **WHEN** `rsp.RelationStore().GetChildren(50, 100)` is called and parent `50` has 500 children
- **THEN** the call SHALL return the first 100 children in key order, `hasMore=true`, and no error

#### Scenario: GetChildren rejects invalid limit

- **WHEN** `GetChildren(50, 0)` or `GetChildren(50, -1)` is called
- **THEN** the call SHALL return `(nil, false, ErrInvalidLimit)`

#### Scenario: GetChildren on leaf parent

- **WHEN** parent `99` has no children
- **THEN** the call SHALL return `([], false, nil)`

### Requirement: RelationStore implementation uses LRU + RocksDB

The concrete `RelationStore` implementation SHALL follow the LRU-hot + RocksDB-cold architecture specified in capability `relation-store-lru-rocks`. Legacy journal-based implementations SHALL be removed.
