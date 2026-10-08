# runtime-periphery-review Specification

## ADDED Requirements

### Requirement: 六维评审完整性

D6 域报告 SHALL 覆盖六维，每维含评分与置信度，含"本域最尖锐的三个问题"小节（契约同 D1 spec）。

#### Scenario: 章节锚点齐备

- **WHEN** 编排者 grep 报告锚点
- **THEN** 六维章节与尖锐三问全部命中

### Requirement: 轨迹完备性专判

报告 SHALL 判定 RL 轨迹的 `(state, action, reward)` 完备性：state（上下文是否原样记录）、action（工具调用全量）、reward（成败信号可得性）三者各给结论与代码证据；并 SHALL 判定 SwappableModel 对训练分布的影响是否有文档交代。

#### Scenario: 结论可引用

- **WHEN** W2 汇总训练友好性结论
- **THEN** D6 报告对上述四点各有明确判定（成立/不成立/证据不足）

### Requirement: 关键机制佐证

报告 SHALL 对以下至少 2 项给出 `文件:符号` 佐证：TrajectoryRecorder 字段、HTTPAPI fail-closed、ReliableBus 溢出、allowlist 逐跳校验。漂移 SHALL 明示。

#### Scenario: 漂移明示

- **WHEN** 抽查发现文档与代码不符
- **THEN** 报告明示冲突双方证据
