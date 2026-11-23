# ci-throughput 规格

## ADDED Requirements

### Requirement: 缓存只加速不判定

编译缓存（module/build）SHALL 只影响构建耗时；任何门/锚的判定 SHALL 只看命令退出码。冷缓存与热缓存两态 SHALL 各取一轮证据且均绿；缓存键 SHALL 绑定 `go.sum`/锁文件。

#### Scenario: 冷热两态同判

- **WHEN** 同一提交在冷缓存与热缓存各跑一轮
- **THEN** 全部门的结论 SHALL 相同
- **AND** 两轮的缓存命中读数 SHALL 分别入档

### Requirement: race 全覆盖由分片并集保真

race 门 SHALL 可分散在多个 shard 执行；shard 包集定义 SHALL 单点维护（`scripts/gates/race_check.sh`），且 SHALL 由脚本静态断言「全部分片并集 ≡ 原全包集」（缺包/重包即红）。聚合门 SHALL fail-closed：任一分片非 success（含 skipped/cancelled）⇒ 聚合红。分片 MUST NOT 改变 race_check 的退出码保真与 waiver 分类器语义。

#### Scenario: 分片缺包

- **WHEN** 从 shard 定义删去一个包
- **THEN** 并集断言 SHALL 红
- **AND** 聚合门 MUST NOT 报绿

### Requirement: 证明面不因吞吐优化而收缩

重复轮次矩阵（event-short 10 轮、event-long 6 分片）与变异矩阵 SHALL 保持原样；MUST NOT 以删测试、缩包集、跳过或放宽阈值换取墙钟。触发策略（push/PR 双跑）SHALL NOT 在本能力内改变。

#### Scenario: 优化越界

- **WHEN** 任一优化提议需要减少轮次、缩小 race 包集或放宽阈值
- **THEN** 该提议 SHALL 被拒绝并回设计门

### Requirement: 吞吐读数前后对比

吞吐验收 SHALL 以真 CI 为权威面，记录变更前后同一工作流的 job 分解与墙钟：关键路径（race）目标不超过原单 job 墙钟的 60%，run 墙钟 SHALL 同步下降；读数原文 SHALL 入证据。

#### Scenario: 关键路径读数

- **WHEN** 分片与缓存上线后的第一轮真 CI 完成
- **THEN** job 分解表 SHALL 显示 race 分片墙钟与聚合门状态
- **AND** 前后对比 SHALL 按同一口径列出
