# event-metadata-contract Specification

## Purpose

事件元数据契约：MetaKey* 常量单点定义、注入点职责归一、ParseEventMeta 统一解析、EventKey 的 canonical hex 字符串形态（FormatEventKey/ParseEventKey）。
## Requirements
### Requirement: 元数据 key 统一定义

事件元数据（存储标识 `event_key`/`partition_id`/`event_type`/`event_summary`、路由来源 `trigger_source`、结算血统 `settle_trigger_source`、透传 `meta_` 前缀）的 key SHALL 在框架（agent 包）中以常量单点定义；所有注入点与解析点 SHALL 引用该唯一来源，SHALL NOT 各自硬编码字符串。

#### Scenario: 全仓键引用同源

- **WHEN** 审计框架内所有写入或读取上述元数据 key 的位置
- **THEN** 它们 SHALL 全部引用统一定义的常量

### Requirement: 注入点职责归一

框架 SHALL 在固定注入点写入元数据：存储标识由事件插件管线在存储时写入；`trigger_source` 由 RunFlow 入口按 invocation 设置并传播到该 invocation 的全部派生事件；`meta_*` 透传元数据由框架在事件投递时统一传播。每个投递到消费端的事件 SHALL 携带完整的存储标识与路由来源。

#### Scenario: 投递事件元数据完整

- **WHEN** 消费端从 outputCh 收到任意携带 Response 的事件
- **THEN** 该事件 SHALL 携带 `trigger_source`
- **AND** 若该事件已被存储，则 SHALL 同时携带 `event_key`/`partition_id`/`event_type`

### Requirement: 元数据解析 API

框架 SHALL 提供类型化解析 API（如 `ParseEventMeta` 与路由助手），消费端 SHALL 通过该 API 解析元数据，SHALL NOT 依赖未在契约中定义的字符串键。

#### Scenario: 消费端经 API 取路由与标识

- **WHEN** example/消费端需要获取事件的 trigger_source、chat_id、event_key
- **THEN** 其 SHALL 通过框架解析 API 获得，且解析结果与注入值一致

### Requirement: bundle_id 归因键

Attribution 载体 MUST 补 BundleID；RunFlow 组装时读取当前 active bundle（evolution 未启用为空串、不写键）；MemoryPlugin 构造期与 persistBusEvent 双路径盖章；evolution Evidence 归因优先 bundle_id 精确 join，缺章回退时间窗。

#### Scenario: 自进化产出可归因到版本
- **WHEN** 某 bundle active 期间产生事件
- **THEN** 事件 Metadata 携带该 bundle_id；guardrail/feedback 聚合可按版本精确分组

### Requirement: 结算血统一级键可查询

`buildBusFact` 持久化 `SourceTask`（结算）事件时，SHALL 将事件自带的 `Metadata[trigger_source]`（由 SettleSignal.Lineage 于结算信号级盖入）以独立键 `settle_trigger_source` 提升为事实链一级可读键；`source_snapshot` SHALL 原样保留（无损快照不因提升而移除）。一级键与回合级 `trigger_source` 两键并存，使"消费回合血统 vs 事件派生血统"免解码直接对账。

#### Scenario: 一级键免解码可读

- **WHEN** 一个携带派生血统的任务结算被持久化
- **THEN** `QueryEvents` 返回的该事件 Metadata 一级键 `settle_trigger_source` 等于结算信号血统，且 `source_snapshot` 内原值仍在

#### Scenario: 两键并存可对账

- **WHEN** 用户血统结算件被非用户回合消费并持久化
- **THEN** 一级 `settle_trigger_source` 为用户血统，回合级 `trigger_source` 为消费回合血统，两值并存可辨

### Requirement: 空血统不写空串一级键

事实链一级键 SHALL NOT 以空串冒充存在：`buildBusFact` 在回合级 `trigger_source` 为空时 SHALL NOT 写入该一级键（与事件属性构造的既有空值保护同形）；读方 SHALL 能以"键缺席"判定"无回合级盖章"。

#### Scenario: 空回合血统不落空串键

- **WHEN** `cm.triggerSource` 为空的 ContextManager 持久化事件
- **THEN** 事实链 Metadata 不含 `trigger_source` 键（而非空串值）

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
