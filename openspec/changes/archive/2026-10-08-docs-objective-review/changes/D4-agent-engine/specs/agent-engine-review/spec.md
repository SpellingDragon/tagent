# agent-engine-review Specification

## ADDED Requirements

### Requirement: 六维评审完整性

D4 域报告 SHALL 覆盖六维，每维含评分与置信度，含"本域最尖锐的三个问题"小节（契约同 D1 spec）。

#### Scenario: 章节锚点齐备

- **WHEN** 编排者 grep 报告锚点
- **THEN** 六维章节与尖锐三问全部命中

### Requirement: 事件驱动收益账专判

报告 SHALL 给出"事件驱动引擎 vs 同步 ReAct 循环"的复杂度-收益分析：列出换来的能力（异步任务回收、多入口统一、重启连续等）与付出的复杂度（管线层数、调试面、时序推理负担），结论须落在第一性判准上而非习惯。

#### Scenario: 收益账可引用

- **WHEN** W2 汇总架构总评
- **THEN** D4 报告的收益账被引用且双向（收益+代价）齐备

### Requirement: 关键机制佐证

报告 SHALL 对以下至少 2 项给出 `文件:符号` 佐证：runEventLoop 主循环、turn 原语统一壳、冥想门控、治理装饰器插入点。漂移 SHALL 明示。

#### Scenario: 漂移明示

- **WHEN** 抽查发现文档与代码不符
- **THEN** 报告明示冲突双方证据
