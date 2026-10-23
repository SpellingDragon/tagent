# causal-chain-isolation Specification

## Purpose

本规范定义 causal-chain-isolation 能力。MemoryPlugin.lastEventKeys SHALL be keyed by a composite of PartitionID and SessionID.

## Requirements

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

- **WHEN** inv.Session is nil or Session.ID is empty（正常入口）
- **THEN** 具名拒绝建立因果链（fail-closed），MUST NOT 降级为 PartitionID-only 键接进普通多用户链（报告响应裁决 report-response.md §三.2）
- **AND** 旧数据（历史 partition-only 键）仅可经 09.2 迁移隔离域显式导入并标记隔离来源，不进入新链

#### Scenario: 回放不跨会话连链

- **WHEN** 冷启动重建投影并按 `GetParent` 回走因果链
- **THEN** 回走 SHALL 限于同一 `(PartitionID, SessionID)`，MUST NOT 把另一会话的事件连为父子

#### Scenario: 隔离键不按 agent 名

- **WHEN** 审查因果链隔离键
- **THEN** 键 SHALL 含 session 维度，MUST NOT 仅按 agent 名或全局单调序
