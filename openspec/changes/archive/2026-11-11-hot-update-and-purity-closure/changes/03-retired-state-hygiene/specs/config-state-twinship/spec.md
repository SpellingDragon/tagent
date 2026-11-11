# config-state-twinship Specification（MODIFIED delta）

## Purpose

清除退役残留的独立写权与影子状态：凡无消费者或可从链/声明重算的状态不得独立写；事实编码单一 wire。

## MODIFIED Requirements

### Requirement: 无影子只写状态

只写不读的影子副本 SHALL 删除；状态 SHALL 有真实读取方或从链派生。

#### Scenario: 影子副本删除

- WHEN 审查 publicationRecords
- THEN 不存在只写不读的 records 副本

### Requirement: 事实编码单一 wire

同一 Publication 数据 SHALL 仅一种 wire 编码；旧事实 SHALL 仍可读。

#### Scenario: 单一编码

- WHEN 提交 configFact
- THEN 顶层与嵌套不重复编码同一数据

#### Scenario: 旧事实可读

- WHEN 读迁移前旧编码事实
- THEN 正确解码不丢字段

### Requirement: 撤键不留下死参

退役配置键 SHALL 不留死参/死分支；独立写权 tuning SHALL 改派生或常量。

#### Scenario: 死参清零

- WHEN 审查已撤预算键与检索 tuning 的独立写权
- THEN 零残留