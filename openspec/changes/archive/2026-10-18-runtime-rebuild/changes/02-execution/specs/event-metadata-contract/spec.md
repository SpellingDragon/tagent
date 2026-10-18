# event-metadata-contract delta（02-execution）

> 复用登记（非规范，不改本条语义）：原生消息直用框架 `model.Message`（`Role`/`ContentParts`/`ToolCalls`/原生 `ToolCallID` 保真），不建「影子消息」第二表示；事件身份去重作用域含所属实例/调用/决策。
> 本 delta 只新增「事件身份与原生载荷保真」一处；本能力其余 Requirement（元数据 key 统一定义、注入点职责归一、元数据解析 API、bundle_id 归因键、结算血统一级键可查询、空血统不写空串一级键）由主 spec `openspec/specs/event-metadata-contract/spec.md` 继承（**证据映射，不改**）。

## ADDED Requirements

### Requirement: 事件身份与原生载荷保真

事件 SHALL 以**原生载荷**承载消息：`model.Message` 的 `Role`（原生角色）、`ContentParts`（多模态）、`ToolCalls` 与原生 `ToolCallID`（工具调用 ID，保真，不做重写/归一）SHALL 原样保留。系统 MUST NOT 重建一层「影子消息」与原生 `model.Message` 并存。

事件身份去重作用域 SHALL 至少包含 `(InstanceID, InvocationID, DecisionID, CallID)` 四元；因 CallID **非全局唯一**，同一 CallID 在不同实例/调用/决策下 MUST NOT 被误判为重复事实，去重 MUST NOT 仅以 CallID 为键。

消息保真 SHALL 覆盖 assistant 同时携带 `ToolCalls` 与 `Content` 的并存形态；MUST NOT 为分类、压缩或存储而剥离其中任一字段。

#### Scenario: assistant 并存 tool_calls 与 content 保真

- **WHEN** 一条 assistant 消息同时携带非空 `Content` 与 `ToolCalls`
- **THEN** 事件载荷 SHALL 原样保留两者，MUST NOT 因分类或持久化丢弃任一字段

#### Scenario: 同 CallID 不同实例不误去重

- **WHEN** 两个不同实例/调用/决策各自产生相同的 CallID
- **THEN** 二者 SHALL 均被作为独立事实受理，MUST NOT 因 CallID 相同被去重为一条

#### Scenario: 多模态 ContentParts 不被压成纯文本

- **WHEN** 消息携带 `ContentParts` 多模态部件
- **THEN** 事件载荷 SHALL 保留 `ContentParts`，MUST NOT 降级为纯文本 `Content`

#### Scenario: 原生 ToolCallID 不被重写

- **WHEN** 事件被构造与持久化后再读取
- **THEN** `ToolCallID` SHALL 与原生值逐字节一致，MUST NOT 被重新生成或归一
