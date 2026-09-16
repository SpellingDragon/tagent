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
输出稳定的服务型任务进入 alive-detached 观测语义时，其脱离时间戳（detachedAt）MUST 作为任务生命周期事实持久化（写入 Declarative 并随 task_spawned 链恢复）；恢复后的脱离时间 MUST 沿用原值，MUST NOT 以恢复时间替代。

#### Scenario: detached 任务跨重启
- **WHEN** alive-detached 任务跨重启恢复
- **THEN** 恢复后的 detachedAt MUST 等于死亡前的原值，超龄观测/终止判定沿用真实时长

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

### Requirement: 终态事实四端一致
任务终态 MUST 经由唯一转换入口产生：内存状态、Settle 信号 Kind、WAL settle_status、反馈正负性四端 MUST 一致——失败终态的结算信号 MUST 携带失败语义（SettleFailed 或 SettleCompleted+Err），事件映射 MUST 将其记为 failed 并产生负面反馈；未知 Settle Kind MUST 映射为 unknown 并告警，MUST NOT 默认记为 completed。

#### Scenario: 僵死回收不产生成功事实
- **WHEN** zombie / orphan / stale 回收路径终结一个任务
- **THEN** 看板状态、task_settled 事件、WAL settle_status、反馈四端 MUST 同为失败语义，MUST NOT 出现「内存 failed、事件 completed」

### Requirement: 迟到信号不得复活终态
任务进入终态后，后续到达的 detector 信号 MUST 被丢弃并记录（含被丢弃的 Kind），MUST NOT 修改终态、MUST NOT 触发重复结算。

#### Scenario: 终态后迟到 Stable 信号
- **WHEN** 任务已 finalize 为 failed，其旧 detector 随后发出 SettleStable
- **THEN** 该信号 MUST 被丢弃并记 Warn，任务状态与结算事实 MUST 保持不变

### Requirement: 任务超龄治理为观测优先、终止显式
超龄 detached 任务默认 MUST 仅标记 stale 观测态（非终态、进程不动、一次性告警通知）；仅当任务生命周期为 job 且宿主显式配置 job deadline 时，超 deadline 才由 owner（detector.Cancel）执行终止并一次结算为 failed。service 型任务 MUST NEVER 因年龄被终止；任务生命周期 MUST 显式声明（TaskSpec.Lifetime，默认按 Kind 推断：command/subagent→job、generic→service）。

#### Scenario: 默认配置下超龄
- **WHEN** job 型任务 detached 超过 task_stale_after 且未配置 job deadline
- **THEN** 任务 MUST 转为 stale 观测态并发出一次性告警，进程与任务 MUST NOT 被强制终结

#### Scenario: 配置 deadline 后超龄
- **WHEN** job 型任务配置了 task_job_deadline 且 detached 超过 deadline
- **THEN** owner MUST 执行 Cancel 确认退出，任务一次结算为 failed，终态事实四端一致

