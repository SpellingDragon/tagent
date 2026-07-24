## ADDED Requirements

### Requirement: 子 agent 调用可作为异步任务执行

`AgentToolWrapper` 对子 agent 的调用 SHALL 可纳入任务层作为异步 Task 执行:其 settle 信号为 `RunFlow` 返回,结果为子 agent 的最终输出。窗口内完成则内联返回(等价于既有同步行为);超窗则返回 ack 并在 `RunFlow` 返回时发出 `task_settled`。

此能力 SHALL 分阶段、可开关地启用:在任务层基础能力(tmux 任务)验证通过前,子 agent SHALL 保持既有同步行为;启用后 SHALL 保留回退开关。每次子 agent Task 的并发隔离契约(独立 EventBus / SessionProjection / ContextManager)SHALL 保持不变。

#### Scenario: 快子 agent 窗口内内联返回

- **WHEN** 一个纳入任务层的子 agent 在 `sync_wait` 窗口内 `RunFlow` 返回
- **THEN** 父 agent 的 `Call()` SHALL 内联返回子 agent 最终输出(与既有同步行为等价)

#### Scenario: 慢子 agent 超窗异步回收

- **WHEN** 一个子 agent 的 `RunFlow` 超过 `sync_wait` 仍未返回
- **THEN** `Call()` SHALL 返回 ack,并在 `RunFlow` 最终返回时发出自包含的 `task_settled` 事件

#### Scenario: 未启用时保持同步

- **WHEN** 子 agent 异步开关未启用
- **THEN** 子 agent 调用 SHALL 保持既有同步执行行为
