## MODIFIED Requirements

### Requirement: Causal chain is isolated by session

Parent relationships SHALL be derived from the durable event chain in (PartitionID, Seq) order, scoped by (PartitionID, SessionID), and MUST NOT be maintained through RelationStore.SetParent or a second relation journal. Recall replay and GetParent MUST NOT join events from different sessions. An index entry without a session SHALL fall back to a partition-scoped body walk so legacy or empty-session data cannot silently merge chains.

#### Scenario: Two users with same agent name

- **WHEN** two sessions produce events under the same agent partition
- **THEN** each session's parent chain contains only its own events, derived directly from the chain

#### Scenario: Same session multiple events with out-of-order timestamps

- **WHEN** one session commits E1 with a later timestamp and then E2 with an earlier timestamp
- **THEN** E2's parent is E1 because the sequence slot, not the timestamp, defines causality

#### Scenario: Session metadata unavailable in the index

- **WHEN** an index entry names no session
- **THEN** parent derivation decodes only that key's partition and derives the session from the body instead of falling back to a partition-only chain

#### Scenario: No online relation writer

- **WHEN** the host assembles recall tools or restarts over durable history
- **THEN** no SetParent path or relation journal is opened; RecallPort forwards the durable parent reader after Bind
