## ADDED Requirements

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
