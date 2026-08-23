# async-task-execution Delta

## MODIFIED Requirements

### Requirement: 工具调用经 spawn + dense 窗口执行

每个纳入任务层的 tool / 子 agent 调用 SHALL 通过 spawn 一个 Task 执行。`Call()` SHALL 等待该 Task 的**第一个 settle 信号**直到调度的 **dense 阶段**结束（dense→sparse 边界，即 detach；见 `adaptive-poll-scheduling`）:

- 若在 dense 阶段内 settle,`Call()` SHALL 返回**内联的最终结果**(与同步调用等价的手感)。
- 若越过 dense 阶段仍未 settle(detach 先到),`Call()` SHALL 返回一个**诚实的 ack**(说明任务已启动、处于 running、完成后将通过事件通知),并将该 Task 转交 TaskManager 后台跟踪。

`Call()` SHALL NOT 返回撒谎式占位符(声称已完成但实际未完成),ack 与 inline 两种返回 SHALL 都是各自闭合、语义诚实的 tool 结果。

**ack 防轮询约束**：后台 ack 文案 SHALL NOT 引导轮询式查询或等待（如"可用任务工具查询状态/结果"类表述——实机已证明其诱发模型以 sleep 命令自旋等待）。ack 携带的"完成后回写/自动通知"语义 SHALL 完整可依，模型凭 ack 即可安心结束回合。

#### Scenario: 快任务在 dense 阶段内 settle 内联返回

- **WHEN** 一个 tool 调用对应的 Task 在 dense 阶段内产生 settle 信号
- **THEN** `Call()` SHALL 返回内联最终结果
- **AND** 该任务 SHALL NOT 进入后台跟踪、SHALL NOT 触发额外的回收 turn

#### Scenario: 慢任务越过 dense 阶段转后台

- **WHEN** 一个 tool 调用对应的 Task 越过 dense 阶段仍未 settle(detach 先到)
- **THEN** `Call()` SHALL 返回启动 ack（含 task id 与 running 状态）
- **AND** 该 Task SHALL 转入 TaskManager 后台跟踪

#### Scenario: 后台 ack 不引导轮询

- **WHEN** `Call()` 返回后台 ack
- **THEN** ack 文案 SHALL NOT 出现"查询状态/结果"等引导模型轮询或以命令等待的表述
- **AND** ack 文案 SHALL 保留"结算后回写/通知"语义

#### Scenario: dense_duration 不小于稳定判定时长

- **WHEN** 配置 `dense_duration` 与 tmux `stable_duration`
- **THEN** `dense_duration` SHALL ≳ `stable_duration`，以保证能在 dense 阶段内捕获快命令的稳定 settle
