## REMOVED Requirements

### Requirement: RelationStore uses LRU hot cache backed by RocksDB

**Reason**: The RocksDB-backed LRU relation cache is absent from the codebase and is superseded by direct chain derivation, which needs no second authoritative store or hot cache.

**Migration**: Read the durable segment index directly; no RocksDB WAL or LRU replacement is introduced.

### Requirement: GetChildren uses reverse index with range scan + limit

**Reason**: There is no reverse relation index or GetChildren implementation. Current recall only asks for the predecessor of one key.

**Migration**: Use GetParent on the durable query source. A child-enumeration API is not reintroduced without a real consumer.

### Requirement: RelationStore drops journal/snapshot methods

**Reason**: The whole RelationStore, including its lifecycle methods, is retired rather than merely having journal methods removed.

**Migration**: No interface remains to inspect; use tierQuerySource.GetParent.

### Requirement: RelationStore Close flushes dirty LRU entries

**Reason**: There is no dirty LRU or RocksDB WAL to flush because parent reads maintain no mutable cache.

**Migration**: Close the storage tier through its existing lifecycle; no relation-specific close remains.
