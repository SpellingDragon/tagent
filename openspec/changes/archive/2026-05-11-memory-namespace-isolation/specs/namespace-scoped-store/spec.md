## ADDED Requirements

### Requirement: Recall sub-tools automatically inject ReadPartitionIDs

Recall agent's sub-tools (`memory_query`, `memory_recent`) SHALL automatically include the agent's configured `ReadPartitionIDs` in every `QueryOptions` call. The LLM SHALL NOT be required to pass `partition_ids` as a tool argument. The injection SHALL happen at the sub-tool creation layer, transparent to the LLM.

#### Scenario: recall queries automatically include tagent's partition

- **WHEN** recall agent is configured with `ReadPartitionIDs: [201]` (tagent's partition)
- **THEN** every `memory_query` call by recall automatically queries across its own partition AND partition 201

#### Scenario: recall without ReadPartitionIDs

- **WHEN** recall agent has empty `ReadPartitionIDs`
- **THEN** `memory_query` queries across all partitions (same behavior as before the change)

#### Scenario: LLM tool call does not specify partition_ids

- **WHEN** LLM calls `memory_query(query="deployment")` without `partition_ids`
- **THEN** the query still includes ReadPartitionIDs (if configured) — the LLM never needs to know about partition mechanics

### Requirement: ToolAgentFactoryConfig carries ReadPartitionIDs

`ToolAgentFactoryConfig` SHALL include a `ReadPartitionIDs []int` field. When building a tool agent via its factory, this field SHALL be non-nil and contain the resolved PartitionIDs from `MemoryConfig.ReadNamespaces`.

#### Scenario: recall factory receives ReadPartitionIDs

- **WHEN** `buildAgent("recall", ...)` constructs `ToolAgentFactoryConfig`
- **THEN** `factoryCfg.ReadPartitionIDs` contains `[PartitionIDFromName("tagent")]`

### Requirement: Sub-tool arg structs are simplified

Recall sub-tool argument structs (`recallQueryArgs`, `recallRecentArgs`) SHALL NOT expose `PartitionIDs` as a JSON-tagged field. The partition filtering SHALL be internal to the implementation, using the configured `ReadPartitionIDs` injected at tool creation time.

#### Scenario: recallQueryArgs struct

- **WHEN** inspecting `recallQueryArgs` struct definition
- **THEN** it contains `Query`, `EventTypes`, `Limit` fields only — no `PartitionIDs` field
