# recall-hybrid-fusion Specification

## Purpose

本 delta 要求混合融合只在**授权检索面**上生效：融合不得越过授权过滤，且保持票据召回哲学与工具声明恒定。

## ADDED Requirements

### Requirement: 融合在授权面上生效且保持票据形态

query 模式的 hybrid 融合（RRF, k=60）SHALL 仅在调用方**已授权**的分区/时间/类型/关系面上执行；授权过滤 MUST 先于/并列于融合；融合结果 SHALL 保持 `EventReference` 票据形态，全文经既有取回路径。

#### Scenario: 授权内融合命中

- **WHEN** 查询在授权分区内且 embedding 已配置
- **THEN** 融合结果包含匹配事件的 `EventReference`
- **AND** 不含未授权分区的任何事件

#### Scenario: 未授权不因融合越权（失败）

- **WHEN** 向量路命中的事件落在未授权分区
- **THEN** 该事件 MUST NOT 出现在结果中

#### Scenario: 声明恒定（并发）

- **WHEN** 对比 embedding 配置开启前后 recall 工具的 `Declaration` JSON
- **THEN** 完全一致（前缀缓存不变量）
