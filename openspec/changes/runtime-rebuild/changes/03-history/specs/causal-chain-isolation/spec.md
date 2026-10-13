# causal-chain-isolation delta（03-history）

> 复用登记（非规范，不改本条语义）：因果链父子关系经 `RelationStore.SetParent` 维护（`FullEvent.ParentKey` 已移除）；按 `(PartitionID, SessionID)` 隔离。
> 本 delta 只修改「Causal chain is isolated by session」；本能力其余内容由主 spec `openspec/specs/causal-chain-isolation/spec.md` 继承（**证据映射，不改**）。

## MODIFIED Requirements

### Requirement: Causal chain is isolated by session

MemoryPlugin.lastEventKeys SHALL be keyed by a composite of PartitionID and SessionID. Each (PartitionID, SessionID) pair SHALL maintain an independent causal chain. Events from different sessions SHALL NOT be linked as parent-child. Parent relationships are maintained via RelationStore.SetParent (not FullEvent.ParentKey, which has been removed).

投影回放与召回 SHALL 尊重该隔离：冷启动重建与 `recall(turn_key=...)` 沿 `GetParent` 回走时，MUST NOT 跨 `(PartitionID, SessionID)` 把不同会话的事件连成父子；回走以 `external_input` 为停止条件，仍限于本会话链。隔离键 SHALL 由 session 决定，MUST NOT 退化为按 agent 名或全局单调链。

#### Scenario: Two users with same agent name

- **WHEN** user A and user B both use the "tagent" agent (same PartitionID) in different sessions
- **THEN** user A's event parent chain only contains user A's events; user B's events are not linked to user A's chain

#### Scenario: Same session multiple events

- **WHEN** a single session produces events E1, E2, E3 in order
- **THEN** E2's parent is E1, E3's parent is E2 (normal causal chain within a session), maintained via RelationStore.SetParent

#### Scenario: Session ID unavailable

- **WHEN** inv.Session is nil or Session.ID is empty
- **THEN** events are keyed by PartitionID only (degraded to current behavior, no crash)

#### Scenario: 回放不跨会话连链

- **WHEN** 冷启动重建投影并按 `GetParent` 回走因果链
- **THEN** 回走 SHALL 限于同一 `(PartitionID, SessionID)`，MUST NOT 把另一会话的事件连为父子

#### Scenario: 隔离键不按 agent 名

- **WHEN** 审查因果链隔离键
- **THEN** 键 SHALL 含 session 维度，MUST NOT 仅按 agent 名或全局单调序
