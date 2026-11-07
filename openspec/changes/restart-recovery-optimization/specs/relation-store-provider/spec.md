## REMOVED Requirements

### Requirement: RelationStoreProvider interface defined

**Reason**: RelationStoreProvider exposed a mutable relation graph that no longer exists. FileSegmentStore has no such graph to return.

**Migration**: Use the stateless durable query and parent reader instead of a provider lookup.

### Requirement: Callers use RelationStoreProvider instead of anonymous interface

**Reason**: There is no RelationStoreProvider implementation or caller left to assert. Parent reads are direct chain operations, not provider dispatch.

**Migration**: Bind RecallPort to the durable tierQuerySource; no type assertion is needed.
