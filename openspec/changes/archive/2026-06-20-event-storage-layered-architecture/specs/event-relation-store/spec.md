## ADDED Requirements

### Requirement: Set and retrieve parent-child event relationships

The system SHALL maintain a mutable directed graph of event causal relationships, independent of event content storage. `SetParent(childKey, parentKey)` SHALL establish or update a causal link. `GetParent(childKey)` SHALL return the parent EventKey (0 if no parent). `GetChildren(parentKey)` SHALL return all direct child EventKeys.

#### Scenario: Establish a parent relationship

- **WHEN** `SetParent(childKey=200, parentKey=100)` is called
- **THEN** `GetParent(200)` SHALL return 100, and `GetChildren(100)` SHALL include 200 in the result

#### Scenario: Update an existing parent relationship

- **WHEN** a compress operation changes a causal chain by calling `SetParent(childKey=500, parentKey=100)` (previously parent was 400)
- **THEN** `GetParent(500)` SHALL return 100, `GetChildren(100)` SHALL include 500, and `GetChildren(400)` SHALL no longer include 500

#### Scenario: Get parent of root event

- **WHEN** `GetParent(key=100)` is called for an event with no parent
- **THEN** the result SHALL be 0 (no error)

#### Scenario: Get children of leaf event

- **WHEN** `GetChildren(key=500)` is called for an event with no children
- **THEN** an empty slice SHALL be returned (no error)

#### Scenario: Batch get parents for trace

- **WHEN** `GetParents([]int64{500, 400, 300})` is called
- **THEN** a map SHALL be returned with each key mapped to its parent EventKey, all within a single mutex lock acquisition

### Requirement: Remove all relationships for an event

The system SHALL support removing all relationships for a given EventKey (used when an event is tombstoned). `RemoveRelations(key)` SHALL delete the key's entry from both the child→parent and parent→children maps.

#### Scenario: Remove relations on tombstone

- **WHEN** `RemoveRelations(key=200)` is called for an event that had `parent=100` and `children=[300, 400]`
- **THEN** `GetParent(200)` SHALL return 0, `GetChildren(100)` SHALL no longer include 200, and `GetChildren(200)` SHALL return empty

### Requirement: Persist relations via append-only WAL journal

The system SHALL persist every relationship mutation to an append-only WAL journal file (`relations.journal`). Each entry SHALL be a single line encoding the operation type and parameters. The journal SHALL be fsync'd after each write to guarantee durability.

#### Scenario: Journal records SetParent

- **WHEN** `SetParent(childKey=200, parentKey=100)` is called
- **THEN** a line in format `+1:200:100` SHALL be appended and fsync'd to `relations.journal`

#### Scenario: Journal records RemoveRelations

- **WHEN** `RemoveRelations(key=200)` is called
- **THEN** a line in format `-1:200` SHALL be appended and fsync'd to `relations.journal`

### Requirement: Recover relation graph from snapshot and journal on restart

The system SHALL recover the full relation graph on restart by: (1) loading the most recent snapshot file (`relations.snap`), (2) replaying all journal entries written after the snapshot. After recovery, the in-memory maps SHALL reflect the exact state at crash time.

#### Scenario: Full recovery from snapshot and journal

- **WHEN** tagent restarts with a snapshot containing {child→parent} pairs and a journal with 5 additional entries
- **THEN** after `ReplayJournal()`, the in-memory `childToParent` and `parentToChildren` maps SHALL contain all relationships from both snapshot and journal entries

#### Scenario: Recovery with corrupted last journal line

- **WHEN** tagent restarts and the last journal line is incomplete (no trailing `\n`)
- **THEN** the incomplete line SHALL be discarded, and all preceding complete entries SHALL be successfully replayed

### Requirement: Create and load snapshots

The system SHALL support creating a full snapshot of the current relation graph (`Snapshot()`). The snapshot SHALL be writable to a file (`relations.snap`) for fast restart recovery. After a snapshot is written, the journal file SHALL be truncated (since all mutations up to that point are captured in the snapshot).

#### Scenario: Snapshot creation and journal truncation

- **WHEN** `Snapshot()` is called and written to `relations.snap`
- **THEN** the journal file SHALL be truncated to zero length, and subsequent mutations SHALL start fresh in the journal

### Requirement: RelationStore is in-memory with bounded size

The system SHALL maintain all relationships entirely in memory using two Go maps: `childToParent map[int64]int64` and `parentToChildren map[int64][]int64`. For 1 million events, total memory SHALL not exceed 50MB.

#### Scenario: Memory usage at scale

- **WHEN** the system has 1 million events with parent relationships
- **THEN** the total memory used by RelationStore maps SHALL be less than 50MB
