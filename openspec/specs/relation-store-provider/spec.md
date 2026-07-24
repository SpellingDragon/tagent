# relation-store-provider Specification

## Purpose

本规范定义 relation-store-provider 能力。The `memory` package SHALL define a `RelationStoreProvider` interface with a single method `RelationStore() RelationStore`, providing a named, discoverable way

## Requirements

### Requirement: RelationStoreProvider interface defined

The `memory` package SHALL define a `RelationStoreProvider` interface with a single method `RelationStore() RelationStore`, providing a named, discoverable way to access the underlying `RelationStore` from any `MemoryStore` implementation that supports it.

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
