# platform-review Specification

## ADDED Requirements

### Requirement: 六维评审完整性

D5 域报告 SHALL 覆盖六维，每维含评分与置信度，含"本域最尖锐的三个问题"小节（契约同 D1 spec）。

#### Scenario: 章节锚点齐备

- **WHEN** 编排者 grep 报告锚点
- **THEN** 六维章节与尖锐三问全部命中

### Requirement: 维护成本账专判

报告 SHALL 对五个默认关闭子系统（治理/自进化/可靠性/org 热更/资源所有权）逐个给出：文档页数与代码面量级、声明消费者、实际启用证据（示例/测试/文档交代），并给出"保留/简化/合并候选"的独立判断。

#### Scenario: 成本账可引用

- **WHEN** W2 汇总过度设计清单
- **THEN** 五子系统在 D5 报告中各有独立结论行

### Requirement: 关键机制佐证

报告 SHALL 对以下至少 2 项给出 `文件:符号` 佐证：org 候选事务、租约最后引用清理、漂移审计默认开、子系统 enabled 默认值。漂移 SHALL 明示。

#### Scenario: 漂移明示

- **WHEN** 抽查发现文档与代码不符
- **THEN** 报告明示冲突双方证据
