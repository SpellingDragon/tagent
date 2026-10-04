## ADDED Requirements

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

## MODIFIED Requirements

### Requirement: 元数据 key 统一定义

事件元数据（存储标识 `event_key`/`partition_id`/`event_type`/`event_summary`、路由来源 `trigger_source`、结算血统 `settle_trigger_source`、透传 `meta_` 前缀）的 key SHALL 在框架（agent 包）中以常量单点定义；所有注入点与解析点 SHALL 引用该唯一来源，SHALL NOT 各自硬编码字符串。

#### Scenario: 全仓键引用同源

- **WHEN** 审计框架内所有写入或读取上述元数据 key 的位置
- **THEN** 它们 SHALL 全部引用统一定义的常量
