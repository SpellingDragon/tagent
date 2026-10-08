# tool-task-review Specification

## ADDED Requirements

### Requirement: 六维评审完整性

D3 域报告 SHALL 覆盖六维，每维含评分与置信度，含"本域最尖锐的三个问题"小节（契约同 D1 spec）。

#### Scenario: 章节锚点齐备

- **WHEN** 编排者 grep 报告锚点
- **THEN** 六维章节与尖锐三问全部命中

### Requirement: 关键机制断言佐证

报告 SHALL 对以下至少 2 项给出 `文件:符号` 佐证：tmux 会话重挂/TaskID 桥、任务终态 TTL 与重入窗口、压缩前缀稳定实现、recall 子工具路由。漂移 SHALL 明示。

#### Scenario: 漂移明示

- **WHEN** 抽查发现文档与代码不符
- **THEN** 报告明示冲突双方证据

### Requirement: README 特性兑现判定

报告 SHALL 对 README 场景一异步任务层（后台执行→task_settled 通知）与场景二会话重挂给出兑现判定与证据。

#### Scenario: 判定可溯源

- **WHEN** W2 引用 D3 兑现判定
- **THEN** 判定可回溯到报告内证据行
