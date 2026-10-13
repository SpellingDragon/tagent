# event-type-unification delta（02-execution）

> 复用登记（非规范，不改本条语义）：事件类型常量单一来源在 `event` 包（`event.Type*`）；本域受理回执/审批等待/拒绝终局/后台结果观察/转交关联等**若**确需新增的类型，亦只在该包定义，不落 `task`/`agent`/`plugin` 平行常量。
> 本 delta 只修改「事件类型常量单一来源」；其余 Requirement（memory 包可引用 event 包无循环依赖、feedback 类型注册、governance 事件非骨架）由主 spec `openspec/specs/event-type-unification/spec.md` 继承（**证据映射，不改**）。

## MODIFIED Requirements

### Requirement: 事件类型常量单一来源

事件类型常量 SHALL 只在 `event` 包中定义（`event.TypeExternalInput` 等）。`memory/types.go` 中的 `EventType*` 常量 SHALL 被删除。所有引用 `memory.EventType*` 的代码 SHALL 改为引用 `event.Type*`。

本域新增的事件类型常量（如受理回执、审批等待、拒绝终局、后台结果观察、转交关联）——**若**确需新增——SHALL 同样只在 `event` 包单点定义，SHALL NOT 在 `task`/`agent`/`plugin` 包各持平行字面量。事件类型分类 SHALL 依据原生 `Role`/`ToolCalls`，MUST NOT 依据被重构或归一后的字段推导。

#### Scenario: memory 包不再定义事件类型常量

- **WHEN** 在 memory 包中搜索 `EventType` 常量定义
- **THEN** 不存在 `EventTypeExternalInput`、`EventTypeAgentOutput`、`EventTypeActionCommand`、`EventTypeThinkingPlan`、`EventTypeContextCompress` 等常量定义

#### Scenario: 所有引用使用 event.Type*

- **WHEN** 在整个代码库中搜索 `memory.EventType`
- **THEN** 返回零结果
- **AND** 所有原来引用 `memory.EventTypeExternalInput` 的代码已改为 `event.TypeExternalInput`

#### Scenario: event 包常量值不变

- **WHEN** 对比 event.TypeExternalInput 的值
- **THEN** 值仍为 "external_input"（与原 memory.EventTypeExternalInput 相同）
- **AND** 所有事件类型的字符串值保持不变（向后兼容已存储的事件）

#### Scenario: 新增事件类型单点定义

- **WHEN** 在 `task`/`agent`/`plugin` 包中搜索本域新增事件类型的字面量或常量定义
- **THEN** 返回零结果，全部引用 `event.Type*`

#### Scenario: 分类基于原生角色

- **WHEN** 对同一事件重复分类
- **THEN** 分类结果 SHALL 只依赖原生 `Role`/`ToolCalls`，与被后续变换的视图/摘要无关
