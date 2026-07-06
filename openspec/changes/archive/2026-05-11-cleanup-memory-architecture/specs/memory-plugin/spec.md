## REMOVED Requirements

### Requirement: FileBackend as memory storage backend

**Reason**: `FileBackend` was the original file-based storage implementation (JSONL files on disk). It has been superseded by `FileSegmentStore` (RustViking KV-based segmented storage). tagent is in incubation, no backward compatibility needed.

**Migration**: All code referencing `FileBackend` should use `FileSegmentStore` instead. The `FileBackend` type, its test file, and all configuration paths referencing it SHALL be deleted.

#### Scenario: FileBackend no longer exists

- **WHEN** the codebase is searched for FileBackend
- **THEN** no references to the `FileBackend` type SHALL exist in production code or tests

### Requirement: Dual-mode MemoryPlugin (FileBackend + FileSegmentStore)

**Reason**: The plugin supported both `FileBackend` (for in-memory/testing) and `FileSegmentStore` (for production) paths. With FileBackend removed, the plugin SHALL use FileSegmentStore as the single implementation.

**Migration**: Remove the backend selection logic in `memory_plugin.go`. `NewMemoryPlugin` SHALL always create a `FileSegmentStore`.

## ADDED Requirements

### Requirement: MemoryPlugin uses FileSegmentStore exclusively

The MemoryPlugin SHALL use `FileSegmentStore` as the sole MemoryStore implementation. There SHALL be no alternative backend selection or fallback logic.

#### Scenario: Plugin initialization creates FileSegmentStore

- **WHEN** `NewMemoryPlugin` is called with configuration
- **THEN** it SHALL create a `FileSegmentStore` instance using the provided KV store and data directory

#### Scenario: Plugin stores events via FileSegmentStore

- **WHEN** an event is triggered and the plugin's `OnEvent` handler processes it
- **THEN** the event SHALL be stored using `FileSegmentStore.StoreEvent`

#### Scenario: Plugin sets parent relationship after storing

- **WHEN** an event with a parent key is stored successfully
- **THEN** the plugin SHALL call `store.RelationStore().SetParent(eventKey, parentKey)`
