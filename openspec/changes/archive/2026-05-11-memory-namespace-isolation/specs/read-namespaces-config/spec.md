## ADDED Requirements

### Requirement: MemoryConfig supports ReadNamespaces

`MemoryConfig` SHALL support a `ReadNamespaces` field of type `[]string`. When non-empty, each element SHALL be an agent name whose partition this agent is allowed to read. The system SHALL resolve these agent names to PartitionIDs at build time and make them available to the agent's memory access layer.

#### Scenario: ReadNamespaces declared in YAML config

- **WHEN** a YAML config specifies `memory: {type: file, path: /data/events, read_namespaces: [tagent]}` for the recall agent
- **THEN** the system parses `ReadNamespaces` as `["tagent"]` and later resolves `"tagent"` to its PartitionID

#### Scenario: ReadNamespaces empty (default)

- **WHEN** `MemoryConfig.ReadNamespaces` is nil or empty
- **THEN** the agent's store reads only its own namespace (default isolation)

#### Scenario: ReadNamespaces with multiple entries

- **WHEN** `ReadNamespaces` is `["tagent", "knowledge"]`
- **THEN** the system resolves both agent names to their respective PartitionIDs

### Requirement: Build pipeline resolves ReadNamespaces to PartitionIDs

During `buildAgent()`, the system SHALL resolve each entry in `ReadNamespaces` to a PartitionID using `PartitionIDFromName()`. The resulting `[]int` SHALL be injected into the `ToolAgentFactoryConfig` for use by the agent's sub-tool wiring.

#### Scenario: recall agent with read_namespaces: [tagent]

- **WHEN** building recall agent with `ReadNamespaces: ["tagent"]`
- **THEN** `buildAgent` computes `PartitionIDFromName("tagent")` and passes the result to recall's `ToolAgentFactoryConfig.ReadPartitionIDs`

### Requirement: No instance-level dedup

The system SHALL NOT maintain a global registry that maps storage paths or names to singleton MemoryStore instances. Each agent SHALL own its own MemoryStore instance. Cross-namespace data access SHALL be achieved through the filesystem (for FileBackend) or namespace-level configuration, not through instance sharing.

#### Scenario: Two agents with same file path create separate instances

- **WHEN** tagent and recall both configure `memory: {type: file, path: /data/events}`
- **THEN** each gets its own `*FileBackend` instance (not the same pointer)
