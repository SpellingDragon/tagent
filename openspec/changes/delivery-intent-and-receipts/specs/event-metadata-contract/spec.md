## ADDED Requirements

### Requirement: 结算事件自身血统落盘

`persistBusEvent` 持久化 `SourceTask`（结算）事件时，SHALL 将事件自带的 `Metadata[trigger_source]`（由 SettleSignal.Lineage 于结算信号级盖入）以独立键 `settle_trigger_source` 写入事实链 FullEvent.Metadata；该键 SHALL NOT 覆写或复用回合级 `trigger_source` 字段——两键并存使"消费回合血统 vs 事件派生血统"可直接对账。fresh 构造路径与 durable prepared-fact 路径 SHALL 一致覆盖。

#### Scenario: 事实链可直接溯源结算血统

- **WHEN** 一个携带派生血统的任务结算被持久化
- **THEN** `QueryEvents` 返回的该事件 Metadata 含 `settle_trigger_source` 且值等于结算信号血统，与回合级 `trigger_source` 并存

#### Scenario: prepared-fact 路径同样落盘

- **WHEN** 结算事件经 durable claim 的 prepared_fact 冻结后提交
- **THEN** 事实链该事件的 `settle_trigger_source` 与 fresh 路径结果一致

## MODIFIED Requirements

### Requirement: 元数据 key 统一定义

事件元数据（存储标识 `event_key`/`partition_id`/`event_type`/`event_summary`、路由来源 `trigger_source`、结算血统 `settle_trigger_source`、透传 `meta_` 前缀）的 key SHALL 在框架（agent 包）中以常量单点定义；所有注入点与解析点 SHALL 引用该唯一来源，SHALL NOT 各自硬编码字符串。

#### Scenario: 全仓键引用同源

- **WHEN** 审计框架内所有写入或读取上述元数据 key 的位置
- **THEN** 它们 SHALL 全部引用统一定义的常量
