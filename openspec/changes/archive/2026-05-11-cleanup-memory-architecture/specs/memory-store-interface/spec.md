## REMOVED Requirements

### Requirement: MemoryStore SetParent compatibility method

**Reason**: `SetParent` was added to the MemoryStore interface as a backward-compatibility bridge during `event-storage-layered-architecture`. Parent-child relationships are the sole responsibility of RelationStore. Exposing `SetParent` on MemoryStore creates a confusing dual path for relationship management.

**Migration**: Callers should use `RelationStore.SetParent(childKey, parentKey)` directly. In the plugin layer, access the RelationStore via `FileSegmentStore`'s internal `rel` field through a public getter method.

#### Scenario: Plugin stores parent relationship via RelationStore

- **WHEN** a new event is stored and has a parent
- **THEN** the plugin SHALL call `store.RelationStore().SetParent(eventKey, parentKey)` instead of `store.SetParent(eventKey, parentKey)`

### Requirement: emptyRelationStore no-op implementation

**Reason**: `emptyRelationStore` was a no-op RelationStore for scenarios that explicitly did not need relationship tracking. With `simpleInMemRelationStore` serving as the default lightweight implementation, this type is redundant.

**Migration**: All callers using `emptyRelationStore{}` should use `newSimpleInMemRelationStore()` or pass `nil` to `NewFileSegmentStore` (which auto-creates a simpleInMemRelationStore).

## ADDED Requirements

### Requirement: MemoryStore interface is pure CRUD + Query

The MemoryStore interface SHALL define only storage-level operations: store events, retrieve events, query events, and delete events. Parent-child relationship management SHALL be handled exclusively by RelationStore.

#### Scenario: MemoryStore does not include relationship methods

- **WHEN** the MemoryStore interface is inspected
- **THEN** it SHALL NOT contain `SetParent`, `GetParent`, or `GetChildren` methods

#### Scenario: MemoryStore provides core operations

- **WHEN** the MemoryStore interface is inspected
- **THEN** it SHALL define `StoreEvent`, `StoreEvents`, `GetEvent`, `GetEvents`, `QueryEvents`, `DeleteEvent`, `GetStats`, `StoreEventWithEmbedding`, `SealCurrent`, `GetSegmentMeta`, `ListSegments`

### Requirement: RelationStore is accessed independently

The system SHALL provide a public getter method on concrete MemoryStore implementations to access the RelationStore for parent-child operations. This decouples storage from relationship concerns.

#### Scenario: Accessing RelationStore from FileSegmentStore

- **WHEN** a caller needs to set or get parent relationships
- **THEN** it SHALL use `store.RelationStore().SetParent(child, parent)` and `store.RelationStore().GetParent(child)`

### Requirement: MemoryStoreAccessor is minimal

The MemoryStoreAccessor interface in the tool package SHALL expose only the methods required by tool implementations: `QueryEvents` and `GetEvent`.

#### Scenario: MemoryStoreAccessor excludes relationship methods

- **WHEN** the MemoryStoreAccessor interface is inspected
- **THEN** it SHALL NOT contain `GetParent` or any relationship-related methods

#### Scenario: Recall tools use direct RelationStore access

- **WHEN** a recall tool needs to trace parent chains
- **THEN** it SHALL access RelationStore through the concrete store implementation rather than through the accessor interface
