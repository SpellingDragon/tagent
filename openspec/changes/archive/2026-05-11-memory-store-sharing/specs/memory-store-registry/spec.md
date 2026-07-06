## ADDED Requirements

### Requirement: FileBackend instance deduplication

The system SHALL ensure that creating a FileBackend with the same `path` returns the same instance. Two agents configured with identical `path` values in their MemoryConfig SHALL share exactly one FileBackend instance, including its internal `sync.RWMutex`, guaranteeing thread-safe concurrent read/write to the same file tree.

#### Scenario: Same path returns same instance

- **WHEN** `resolveMemoryStore(MemoryConfig{Type: "file", Path: "/data/events"})` is called twice
- **THEN** both calls return the same `*FileBackend` pointer

#### Scenario: Different paths return different instances

- **WHEN** `resolveMemoryStore({Type: "file", Path: "/data/a"})` and `resolveMemoryStore({Type: "file", Path: "/data/b"})` are called
- **THEN** the two returned instances are distinct `*FileBackend` objects

#### Scenario: Concurrent writes are safe

- **WHEN** two goroutines call `StoreEvent()` on FileBackend instances obtained from the same path
- **THEN** all writes succeed without data corruption due to the shared `sync.RWMutex`

### Requirement: InMemoryStore named sharing

The system SHALL support optional named sharing of InMemoryStore instances. When `MemoryConfig.Name` is a non-empty string, `resolveMemoryStore` SHALL return the same InMemoryStore instance across all calls with the same name. When `Name` is empty (default), each call SHALL return an isolated new instance, preserving backward compatibility.

#### Scenario: Named stores are shared

- **WHEN** `resolveMemoryStore({Type: "memory", Name: "shared"})` is called twice
- **THEN** both calls return the same `*InMemoryStore` pointer

#### Scenario: Unnamed stores are isolated

- **WHEN** `resolveMemoryStore({Type: "memory"})` is called twice with no `Name`
- **THEN** the two calls return different `*InMemoryStore` instances

#### Scenario: Different names isolate

- **WHEN** `resolveMemoryStore({Type: "memory", Name: "a"})` and `resolveMemoryStore({Type: "memory", Name: "b"})` are called
- **THEN** the two instances are distinct

### Requirement: Thread-safe registry

The instance registry SHALL be thread-safe. Concurrent calls to `resolveMemoryStore` with identical parameters SHALL not create duplicate instances, and all registry operations SHALL be protected by a mutex.

#### Scenario: Concurrent same-name resolution

- **WHEN** 10 goroutines concurrently call `resolveMemoryStore({Type: "memory", Name: "x"})`
- **THEN** all return the same `*InMemoryStore` instance, and exactly one instance is created
