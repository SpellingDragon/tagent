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

### Requirement: 通知身份、事实引用与消费身份分层

系统 SHALL 将责任身份、通知身份、事实引用、逻辑消费身份与外部发送动作身份分层记录，MUST NOT 以一个身份替代另一个：

- 责任身份：沿用本能力既有「事件身份与原生载荷保真」的 `(InstanceID, InvocationID, DecisionID, CallID)` 四元，本要求不重定义其去重作用域；
- 通知身份：`(TaskID, Round, Kind, Seq)`（`Round` 即既有 `ExecutionRound`，`Kind` 即既有 `Category`），其稳定序号与重复运输幂等沿用 `persistent-event-loop`「Handoff 未来通知的目标、实例与稳定序号」既有义务，本要求不另建同义条款；
- 事实引用：`EventID`，指向已提交事实，MUST NOT 取代通知身份；
- 逻辑消费身份：消费方实例/调用/决策 + 所消费事实键 + 消费位置；
- 外部发送动作身份：一次显式 `send`、最终答复、文件或渠道分片各自的动作身份。

同一通知身份携带不同正文、任务或类别时 MUST 被判为身份冲突并显式拒绝或隔离，MUST NOT 静默保留首份，也 MUST NOT 执行两份。不同责任身份的通知 SHALL 各自保留责任归属与事实键；同一 session 不同 invocation 不合批的既有义务见 `persistent-event-loop`「业务 turn 开始执行时绑定编排版本」，本要求不重复定义。多个合法消费者读取同一已提交事实时各自的消费归属由 `committed-event-attribution` 规定，本要求不重复定义。

#### Scenario: 同身份异正文被判冲突

- **WHEN** 两个通知携带相同 `(TaskID, Round, Kind, Seq)` 但正文或任务内容不同
- **THEN** 系统 MUST 以具名冲突拒绝或隔离后到者
- **AND** MUST NOT 静默保留首份，也 MUST NOT 执行两份

#### Scenario: 不同责任身份的通知不合并

- **WHEN** 两个通知携带相同 `TaskID` 或相同正文，但责任 `(InstanceID, InvocationID, DecisionID)` 不同
- **THEN** 二者 SHALL 各自保留责任身份与事实键
- **AND** 不合批沿用既有条款，MUST NOT 以任务号或正文相同为由改写任一方的责任身份

#### Scenario: 事实引用不代替通知身份

- **WHEN** 两个事件各自携带不同的 `EventID`，但 `(TaskID, Round, Kind, Seq)` 相同
- **THEN** 系统 SHALL 按通知身份而非 `EventID` 判定二者为同一通知
- **AND** MUST NOT 以 `EventID` 不同为由执行两次

#### Scenario: 外部发送动作身份独立

- **WHEN** 同一回合既产生最终答复又执行一次显式 `send`
- **THEN** 两个动作 SHALL 各有独立动作身份
- **AND** MUST NOT 以最终答复身份、`EventID` 或正文哈希代替显式发送身份

### Requirement: 派生视图有来源且不改写源事实

摘要、卡片、投影和遥测视图 SHALL 作为源事实的投影视图携带来源事实键或等价稳定引用，MUST NOT 改写源事实的角色、`ToolCallID`、`ContentParts` 或正文身份。系统 MUST NOT 以文本相似度、正文前缀或摘要内容作为去重、归属或消费判定依据；源事实与派生视图 MUST 在事实链中可区分。

#### Scenario: 卡片携带来源事实键

- **WHEN** 一条通知被渲染为遥测卡片或摘要
- **THEN** 卡片 SHALL 携带其来源通知事实键
- **AND** MUST NOT 只保留渲染文本而丢失来源引用

#### Scenario: 相似正文不同事实不按文本去重

- **WHEN** 两条源事实正文相同或高度相似但身份不同
- **THEN** 二者 SHALL 作为独立事实保留
- **AND** MUST NOT 因文本相似被合并或去重

#### Scenario: 派生视图不回写源事实

- **WHEN** 投影或摘要被消费
- **THEN** 源事实的 `Role`、`ToolCallID`、`ContentParts` 与正文 SHALL 保持原样
- **AND** MUST NOT 被派生视图覆盖
