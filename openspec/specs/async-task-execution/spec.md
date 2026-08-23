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

### Requirement: task_settled 为通知类 input 事件

后台任务的结算结果（task_settled)SHALL 被视为"通知"类外部输入事件：它 SHALL NOT 被视为对某次"等待中" tool 调用的协议应答（同步应答已在 spawn 的 sync-wait 窗口内以 ack/内联结果完成）；它 SHALL 作为新的驱动事件进入时间线并触发回收 turn。通知内容 SHALL 携带文本级关联标识（task id 与任务简述），使模型能在内容上将通知与先前的调用关联。

通知结果 SHALL **有界化**（对齐同步路径的输出转储模式）：结果不超过转储阈值（与 OutputLimitTool 同公式，`MaxTokens/2×4` 字符）时全文内联；超过时全文 SHALL 转储到 workspace 的 tool-output 目录（受 Cleaner 周期清理），通知 Content SHALL 携带尾部摘录（对齐 ActionTool 的 2000 字符）与文件路径票据，事件本体 SHALL NOT 持有全文——凭票据召回该事件返回的是有界版+票据，大结果永不经召回回流上下文。全文消费 SHALL 经 `read_file(start_line, num_lines)` 行级分页。

#### Scenario: 慢命令的应答-通知二段式

- **WHEN** 一个命令越过 dense 窗口转后台，稍后在后台 settle
- **THEN** 原调用处 SHALL 已返回 ack（含 task id)——这是该调用的同步应答
- **AND** settle 结果 SHALL 以 task_settled 通知（含同一 task id）驱动一个**新的** turn

#### Scenario: 通知携带可关联标识

- **WHEN** 渲染含 task_settled 通知的历史
- **THEN** 通知文本 SHALL 含 task id 与任务简述
- **AND** 同时间线中先前 ack 文本 SHALL 含同一 task id

#### Scenario: 小结果全文内联

- **GIVEN** 一个 settle 输出低于转储阈值（如 800 字符）
- **WHEN** task_settled 事件被构造并持久化
- **THEN** 事件 Content SHALL 为结果全文，SHALL NOT 含截断标记或文件票据

#### Scenario: 大结果转储文件且事件有界

- **GIVEN** 一个 settle 输出远超转储阈值（如数万字符）
- **WHEN** task_settled 事件被构造
- **THEN** 全文 SHALL 已写入 tool-output 目录文件，通知 SHALL 含尾部摘录与文件路径票据
- **AND** 事件 Content SHALL 有界（不含全文），凭 evt key 召回 SHALL 返回有界版+票据，不复发大结果
- **AND** 模型 SHALL 可经 `read_file` 分页读取全文

### Requirement: 任务状态机 resume 边

任务状态机 SHALL 新增 resume 边:合法源状态 {alive-detached, stable, completed, failed} --resume(input)--> running(dense)（存活类=会话重入;完成态=round 型执行器的自然续行点）;running/suspect/cancelled SHALL 拒绝并引导。resume 后的结算复用既有 settle 三档分类与 task_settled 通知路径,通知 SHALL 携带原 task id。任务工具族 SHALL 加入 `resume_task`（与 list/get/cancel/relaunch 并列）。

#### Scenario: resume 后的结算走既有通知路径

- **WHEN** resume 的命令在 dense 窗口外完成
- **THEN** SHALL 发布 task_settled 通知（同一 task id）,持久循环按既有规则回收 turn

