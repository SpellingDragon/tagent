# causal-chain-isolation Specification

## Purpose

本规范定义 causal-chain-isolation 能力。MemoryPlugin.lastEventKeys SHALL be keyed by a composite of PartitionID and SessionID.

## Requirements

### Requirement: Causal chain is isolated by session

MemoryPlugin.lastEventKeys SHALL be keyed by a composite of PartitionID and SessionID. Each (PartitionID, SessionID) pair SHALL maintain an independent causal chain. Events from different sessions SHALL NOT be linked as parent-child. Parent relationships are maintained via RelationStore.SetParent (not FullEvent.ParentKey, which has been removed).

#### Scenario: Two users with same agent name

- **WHEN** user A and user B both use the "tagent" agent (same PartitionID) in different sessions
- **THEN** user A's event parent chain only contains user A's events; user B's events are not linked to user A's chain

#### Scenario: Same session multiple events

- **WHEN** a single session produces events E1, E2, E3 in order
- **THEN** E2's parent is E1, E3's parent is E2 (normal causal chain within a session), maintained via RelationStore.SetParent

#### Scenario: Session ID unavailable

- **WHEN** inv.Session is nil or Session.ID is empty
- **THEN** events are keyed by PartitionID only (degraded to current behavior, no crash)
