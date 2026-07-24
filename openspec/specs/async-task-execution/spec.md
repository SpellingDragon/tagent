# async-task-execution Specification

## Purpose

Let long-running tools (tmux commands, sub-agents) execute without blocking the
persistent event loop, by running each call as a Task in a task layer. A call
either settles within the dense phase and returns inline, or crosses the
dense→sparse boundary (detach) and returns an honest ack while the Task is
tracked in the background (see also `adaptive-poll-scheduling`).

## Requirements

### Requirement: 工具调用经 spawn + dense 窗口执行

每个纳入任务层的 tool / 子 agent 调用 SHALL 通过 spawn 一个 Task 执行。`Call()` SHALL 等待该 Task 的**第一个 settle 信号**直到调度的 **dense 阶段**结束（dense→sparse 边界，即 detach；见 `adaptive-poll-scheduling`）:

- 若在 dense 阶段内 settle,`Call()` SHALL 返回**内联的最终结果**(与同步调用等价的手感)。
- 若越过 dense 阶段仍未 settle(detach 先到),`Call()` SHALL 返回一个**诚实的 ack**(说明任务已启动、处于 running、完成后将通过事件通知),并将该 Task 转交 TaskManager 后台跟踪。

`Call()` SHALL NOT 返回撒谎式占位符(声称已完成但实际未完成),ack 与 inline 两种返回 SHALL 都是各自闭合、语义诚实的 tool 结果。

#### Scenario: 快任务在 dense 阶段内 settle 内联返回

- **WHEN** 一个 tool 调用对应的 Task 在 dense 阶段内产生 settle 信号
- **THEN** `Call()` SHALL 返回内联最终结果
- **AND** 该任务 SHALL NOT 进入后台跟踪、SHALL NOT 触发额外的回收 turn

#### Scenario: 慢任务越过 dense 阶段转后台

- **WHEN** 一个 tool 调用对应的 Task 越过 dense 阶段仍未 settle(detach 先到)
- **THEN** `Call()` SHALL 返回启动 ack（含 task id 与 running 状态）
- **AND** 该 Task SHALL 转入 TaskManager 后台跟踪

#### Scenario: dense_duration 不小于稳定判定时长

- **WHEN** 配置 `dense_duration` 与 tmux `stable_duration`
- **THEN** `dense_duration` SHALL ≳ `stable_duration`，以保证能在 dense 阶段内捕获快命令的稳定 settle

### Requirement: 并行 dense 窗口

当一个 turn 内 LLM 并行发起多个纳入任务层的调用时,各调用的 dense 窗口 SHALL 并行计时,SHALL NOT 串行累加阻塞事件循环。

#### Scenario: 多个长任务并行等窗口

- **WHEN** 一个 turn 内并行发起两个长任务调用
- **THEN** 两个 dense 窗口 SHALL 并行计时
- **AND** 事件循环被阻塞的总时长 SHALL 约等于单个 dense 窗口（而非其两倍）

### Requirement: 按任务类型的 settle 探测器与分档

任务层 SHALL 为不同任务类型提供各自的 settle 探测器:tmux 命令使用 `TmuxMonitor`(周期 poll + 输出稳定计时 + 主动 heartbeat 探针 + 进程退出检测);子 agent 使用 `RunFlow` 返回;通用任务使用 goroutine 返回。

settle 信号 SHALL 携带分档 `kind`:`completed`(进程退出,确定完成)、`stable`(输出稳定,可用但未必完成)、`suspect`(静默超过 `fake_dead_duration`,疑似挂死)。探测器 SHALL 只做确定性探测与分类,SHALL NOT 对"是否真正完成"做语义判断;语义解读 SHALL 由 LLM 在被触发的 turn 读取输出后完成。

#### Scenario: 进程退出判定为 completed

- **WHEN** 一个 tmux 命令的进程退出或 pane dead
- **THEN** 探测器 SHALL 发出 `completed` settle 信号并附带捕获的输出

#### Scenario: 静默过久判定为 suspect 而非 completed

- **WHEN** 一个存活进程的输出静默超过 `fake_dead_duration` 且 heartbeat 探针无响应
- **THEN** 探测器 SHALL 发出 `suspect` settle 信号(而非误判为 completed)
- **AND** LLM SHALL 在回收 turn 中据输出决定重试/取消/继续等待

#### Scenario: 子 agent 以 RunFlow 返回为 settle

- **WHEN** 一个纳入任务层的子 agent 调用其 `RunFlow` 返回
- **THEN** 该 Task SHALL 被判定为 `completed` 并以子 agent 最终输出作为结果

### Requirement: 服务型任务转 alive-detached

对于输出已稳定但进程仍存活的服务型任务(如启动一个常驻服务),任务层 SHALL 在首次 `stable` settle 时发出一次"就绪"通知,随后将该 Task 转为 `alive-detached` 状态。处于 `alive-detached` 的任务 SHALL NOT 因后续输出变化重复触发回收 turn,SHALL NOT 自动完成,其结束 SHALL 仅由显式 `cancel` 或进程死亡触发。

#### Scenario: 服务就绪后不再刷屏

- **WHEN** 一个服务型任务输出稳定并发出"就绪"通知后仍持续产生输出
- **THEN** 该任务 SHALL 处于 `alive-detached`，后续输出变化 SHALL NOT 触发新的回收 turn

#### Scenario: alive-detached 任务经 cancel 结束

- **WHEN** LLM 对一个 `alive-detached` 任务调用 `cancel`
- **THEN** 该任务 SHALL 终止并从活跃看板移除
