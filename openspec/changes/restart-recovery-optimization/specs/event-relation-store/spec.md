## REMOVED Requirements

### Requirement: Set and retrieve parent-child event relationships

**Reason**: The mutable RelationStore graph no longer exists. Recall parent reads derive the predecessor directly from the durable chain in (PartitionID, Seq) order.

**Migration**: Use recall trace/get-parent over tierQuerySource; no SetParent/GetChildren replacement API is provided.

### Requirement: Remove all relationships for an event

**Reason**: There is no mutable parent map to tombstone or clean up. The event chain is immutable and parent derivation needs no lifecycle operation.

**Migration**: No migration; historical events remain immutable and queryable.

### Requirement: Persist relations via append-only WAL journal

**Reason**: The relation journal is retired. A second durable parent graph created ordering and recovery duplication without adding truth to the chain.

**Migration**: Parent derivation uses the durable segment index and body fallback only when the index lacks a session.

### Requirement: Recover relation graph from snapshot and journal on restart

**Reason**: Restart no longer loads a separate relation snapshot or journal. The same commit-ordered chain that restores facts also answers parent reads.

**Migration**: Use tierQuerySource.GetParent after opening the tier; no relation recovery step remains.

### Requirement: Create and load snapshots

**Reason**: Relation snapshots are retired with the graph they captured. No second snapshot family is needed.

**Migration**: No migration; recovery uses the existing event chain and its checkpoint mechanisms.

### Requirement: RelationStore is in-memory with bounded size

**Reason**: The bounded in-memory RelationStore and its two maps are gone. Query-time derivation retains no parent map at all.

**Migration**: Use stateless tierQuerySource reads; no capacity contract for a parent map remains.
