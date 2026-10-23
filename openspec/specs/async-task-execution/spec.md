# async-task-execution Specification

## Purpose

Let long-running tools (tmux commands, sub-agents) execute without blocking the
persistent event loop, by running each call as a Task in a task layer. A call
either settles within the dense phase and returns inline, or crosses the
dense→sparse boundary (detach) and returns an honest ack while the Task is
tracked in the background (see also `adaptive-poll-scheduling`).
## Requirements
### Requirement: 工具调用经 spawn + dense 窗口执行

每个纳入任务层的 tool / 子 agent 调用 SHALL 经注册 wrapper 走**一次受理**协议：wrapper 收到调用即 SHALL **立即返回一次受理回执**（含 task id 与 running 状态），受理**不等结果**——快任务也 MUST NOT 另走「同步等待窗口内内联最终结果」分支；受理失败 SHALL 产生一次原生工具错误响应。任务本身 SHALL 经 spawn 进入任务层跟踪。

原「`Call()` 等待第一个 settle 直到 dense 阶段结束 / 窗口内内联返回 / 越窗返回 ack」的**等待式窗口语义 SHALL 被本条取代**：`Call()` 返回值语义是**受理回执**（任务已受理、结果将随后台结果回流），而**不是**该调用的最终结果；`dense`／`sync-wait` 窗口 MUST NOT 再作为 `Call()` 返回值或结算判据。回执 MUST NOT 是撒谎式占位符（声称已完成但实际未完成）。

**回执防轮询约束**：回执文案 SHALL NOT 引导轮询式查询或等待（如「可用任务工具查询状态/结果」类表述——实机已证明其诱发模型以 sleep 命令自旋等待）。回执携带的「完成后回写/自动通知」语义 SHALL 完整可依，模型凭回执即可安心结束回合。

#### Scenario: 快任务受理即回执
- **WHEN** 一个 tool 调用对应的 Task 很快（可能在原 dense 窗口内即 settle）
- **THEN** `Call()` SHALL 已返回一次受理回执（task id + running），MUST NOT 因任务快而改走同步内联最终结果分支
- **AND** 其最终结果 SHALL 作为后台结果独立回流（驱动下一步 decision），不回填受理回执

#### Scenario: 慢任务不受等待窗口约束
- **WHEN** 一个 tool 调用的 Task 耗时超出任何窗口
- **THEN** `Call()` SHALL 早已返回受理回执，调用方 MUST NOT 因等待其结算而阻塞

#### Scenario: 受理失败产生原生错误响应
- **WHEN** 注册 wrapper 受理失败（如超限/拒绝）
- **THEN** SHALL 产生一次原生工具错误响应，MUST NOT 返回「已受理」回执

#### Scenario: 回执不引导轮询
- **WHEN** `Call()` 返回受理回执
- **THEN** 回执文案 SHALL NOT 出现「查询状态/结果」等引导模型轮询或以命令等待的表述
- **AND** 回执 SHALL 保留「结算后回写/通知」语义

### Requirement: 并行 dense 窗口

当一个 turn 内 LLM 并行发起多个纳入任务层的调用时，各调用的**受理** SHALL 并行、互不阻塞事件循环；事件循环被受理占用的总时长 SHALL NOT 随调用数串行累加。（原以「并行 dense 计时窗口」表达的等待式语义不再作为 `Call()` 返回或结算依据，见「工具调用经 spawn + dense 窗口执行」。）

#### Scenario: 多个长任务并行受理
- **WHEN** 一个 turn 内并行发起两个长任务调用
- **THEN** 两次受理 SHALL 并行完成、各自立即返回回执
- **AND** 事件循环被受理阻塞的总时长 SHALL 不随调用数串行累加

### Requirement: 按任务类型的 settle 探测器与分档

任务层 SHALL 为不同任务类型提供各自的 settle 探测器：tmux 命令使用 `TmuxMonitor`（周期 poll + 输出稳定计时 + 主动 heartbeat 探针 + 进程退出检测）；通用任务使用 goroutine 返回。

settle 信号 SHALL 携带分档 `kind`：`completed`（进程退出，确定完成）、`stable`（输出稳定，可用但未必完成）、`suspect`（静默超过 `fake_dead_duration`，疑似挂死）。探测器 SHALL 只做确定性探测与分类，SHALL NOT 对「是否真正完成」做语义判断；语义解读 SHALL 由 LLM 在被触发的 decision 读取输出后完成。

子 agent 调用的 settle SHALL 以**本调用义务明确结清**为准：MUST NOT 以「模型最终 response 结束」或「`RunFlow` 返回」直接判定为 `completed`（见 `subagent-turn-execution`「子 agent 调用可作为异步任务执行」）。

#### Scenario: 进程退出判定为 completed
- **WHEN** 一个 tmux 命令的进程退出或 pane dead
- **THEN** 探测器 SHALL 发出 `completed` settle 信号并附带捕获的输出

#### Scenario: 静默过久判定为 suspect 而非 completed
- **WHEN** 一个存活进程的输出静默超过 `fake_dead_duration` 且 heartbeat 探针无响应
- **THEN** 探测器 SHALL 发出 `suspect` settle 信号（而非误判为 completed）
- **AND** LLM SHALL 在被触发的 decision 中据输出决定重试/取消/继续等待

#### Scenario: 子 agent 义务结清才 settle
- **WHEN** 一个纳入任务层的子 agent 调用其模型已给出最终 response（`RunFlow` 返回），但该调用的后台结果/义务尚未全部结清
- **THEN** 该 Task MUST NOT 被判为 `completed`；SHALL 待义务明确结清后方 settle，结果作为独立 observation 回流

### Requirement: 服务型任务转 alive-detached

服务型任务（`LifetimeOf(spec) == service`）的**运行中 / 就绪 / 终局**三类观测能力 SHALL 按已批准新合同**分维度保留**：运行中（alive）与就绪（ready）属**非终局**观测，MUST NOT 被记为 `completed`；**终局结算 SHALL 只由统一终态归约产生**。

就绪通知 SHALL 以独立**通知类别（Kind）**与**稳定 Seq** 身份表达，其身份 MUST NOT 与终态通知或其他类别冲突；同一通知的重复运输 MUST NOT 生成第二份事实，任何先前**已 delivered** 的重复 MUST NOT 被冒充为成功终局。

**服务就绪 MUST NOT 按时间推移推断为成功**：系统 MUST NOT 因「经过某时长而未见终局」把服务判为成功/完成。`detachedAt` MUST NOT 作为生命周期事实持久化；历史已落库的 `detachedAt` 在读取时按未知/忽略处理，MUST NOT 因缺失而拒绝恢复。任务生命周期类（`TaskSpec.Lifetime`：job/service）声明与年龄约束语义不受影响（见「生命周期类只决定编排语义，不决定是否受年龄约束」）。

> 迁移说明：本条**取代**主 spec 中「`alive-detached` 过渡态 + `detachedAt` 持久化」的旧表述，但不删除服务能力：运行/就绪/终局按维度保留，就绪以独立 Kind+Seq 表达且**非 completed**、不按时间推成功。因此本域**不设 REMOVED**（原 REMOVED 块因 Reason 引用的 `settle-signal-matrix` 等旧变更不在本计划证据面内而撤回）。

#### Scenario: job 型静默任务不产生就绪终局
- **WHEN** job 型任务（如 `sleep 180`、包安装命令）输出静默
- **THEN** 任务 MUST NOT 因此产生「就绪即成功」的终局；其终态结算照常按统一归约通知

#### Scenario: service 型就绪为非终局
- **WHEN** service 型任务（如常驻服务）达到就绪条件
- **THEN** SHALL 产生独立类别（Kind）与稳定 Seq 的就绪通知，且该通知 MUST NOT 被记为 `completed`；终局仍待统一归约

#### Scenario: 就绪不按时间推成功
- **WHEN** 一个 service 任务长时间无终局
- **THEN** 系统 MUST NOT 仅因时间推移将其判为成功/完成

#### Scenario: 重复通知不冒充成功
- **WHEN** 一条先前已 delivered 的就绪通知被重复运输
- **THEN** 重复 SHALL 被幂等去重（同一 Kind+Seq），MUST NOT 生成第二份事实，MUST NOT 把任何副本冒充为成功终局

#### Scenario: 历史 detachedAt 不阻断恢复
- **WHEN** 恢复时读到历史遗留的 `detachedAt` 字段
- **THEN** 该字段 SHALL 按未知/忽略处理，MUST NOT 因缺失或未知而拒绝恢复

### Requirement: task_settled 为通知类 input 事件

后台任务的结算结果（task_settled）SHALL 被视为「通知」类外部输入事件：它 SHALL NOT 被视为对某次「等待中」tool 调用的协议应答（该调用的**同步应答**是受理时返回的一次受理回执，见「工具调用经 spawn + dense 窗口执行」）；它 SHALL 作为新的驱动事件进入时间线并触发**下一步 decision**。该 decision 仍属**原 invocation**（沿用原调用身份；CallID 按 decision 作用域保真，见 `event-metadata-contract`），MUST NOT 偷偷创建新 invocation——仅应用**显式 Handoff** 时改投新目标/实例（见 `persistent-event-loop` ADDED「Handoff 未来通知的目标、实例与稳定序号」）。通知内容 SHALL 携带文本级关联标识（task id 与任务简述），使模型能在内容上将通知与先前的调用关联。

**失败极性表达**：命令非零退出（含信号死）、探测不可辨超限、kill 失败强拆三类路径的结算 SHALL 以失败极性呈现（✗/failed），通知文本 SHALL 携带 `exit_code=N`（判定与透传契约见 failure-polarity 能力规格）；零退出的成功极性（✓/completed）语义不变。

通知结果 SHALL 以**原文持久引用**承载并**有界呈现**：超大结果 SHALL 先按**原文持久存储**（引用含 owner/format，按 owner/format 准确取回与分段读取，见 `recall-protocol`「原文引用与分段读取保真」与 `tool-output-interception`「工作区副本物化与引用稳定」）；通知本体 SHALL 只携带**有界视图**与凭据（票据），SHALL NOT 持全文；**临时副本可被清理且 MUST NOT 影响原文**。凭票据召回 SHALL 返回有界视图与票据，MUST NOT 把大结果原文回灌上下文；全文消费 SHALL 经分段读取（`start_line`/`num_lines` 行级分页）。纯空白载荷（去除首尾空白后为空且无 Err）SHALL 降级为单行票据，不投递空白正文。

> 迁移说明：本条**取代**主 spec 中「通知结果按字符数阈值（`MaxTokens/2×4`）全文内联 / 超限转储到 workspace 唯一 tool-output 目录 / 大结果永不经召回回流」的旧表述——原文持久引用与有界视图的边界改按 `recall-protocol`/`tool-output-interception` 新合同，字符数阈值不再作为「全文是否回流上下文」的判定，也不作为唯一存放位置的理由。

#### Scenario: 慢命令的受理-通知二段式
- **WHEN** 一个命令调用被受理转后台，稍后在后台 settle
- **THEN** 原调用处 SHALL 已返回一次受理回执（含 task id)——这是该调用的同步应答
- **AND** settle 结果 SHALL 以 task_settled 通知（含同一 task id）驱动**下一步 decision**，该 decision 仍属原 invocation

#### Scenario: 后台结果不新建 invocation
- **WHEN** 一次后台任务结算并回流
- **THEN** 回流 SHALL 落在原 invocation 的下一步 decision 上，MUST NOT 偷偷创建新 invocation；仅应用显式 Handoff 时才改投新目标/实例

#### Scenario: 失败结算以失败极性通知
- **WHEN** 后台命令以非零退出码结束并结算
- **THEN** task_settled 通知 SHALL 为失败极性（✗/failed），文本 SHALL 含 `exit_code=N` 与 task id

#### Scenario: 通知携带可关联标识
- **WHEN** 渲染含 task_settled 通知的历史
- **THEN** 通知文本 SHALL 含 task id 与任务简述
- **AND** 同时间线中先前受理回执文本 SHALL 含同一 task id

#### Scenario: 大结果按原文持久引用有界呈现
- **GIVEN** 一个 settle 输出远超展示阈值（如数万字符）
- **WHEN** task_settled 事件被构造
- **THEN** 全文 SHALL 已按**原文持久存储**（引用含 owner/format），通知 SHALL 含**有界视图**与凭据（票据）
- **AND** 通知本体 SHALL NOT 持全文；凭票据召回 SHALL 返回有界视图与票据，全文经分段读取获取
- **AND** 原文持久引用的取回 SHALL 按其 owner/format 准确进行，MUST NOT 因临时副本被清理而失效

#### Scenario: 空白载荷降级票据
- **GIVEN** 一个 settle 输出去除首尾空白后为空且无 Err
- **WHEN** task_settled 通知被构造
- **THEN** 通知 SHALL 为单行票据（id、简述、极性），SHALL NOT 投递空白正文

#### Scenario: 一次受理仅一次回执
- **WHEN** 注册 wrapper 收到一次工具调用
- **THEN** 其 SHALL 恰好返回一次受理回执；快任务在窗口内 settle 亦走同一受理协议，MUST NOT 另走同步最终结果分支

#### Scenario: 后台结果作为独立 observation
- **WHEN** 后台任务结算
- **THEN** 结果 SHALL 作为独立 observation 驱动新的下一步 decision，MUST NOT 冒充原调用同一 CallID 的第二次同步 response

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

### Requirement: 生命周期类只决定编排语义，不决定是否受年龄约束

任务生命周期 MUST 显式声明（`TaskSpec.Lifetime`：job／service），缺省 SHALL 按 Kind 推断（command／subagent→job、generic→service），并作为任务声明的一部分持久化以便回放复原。该类别只界定编排语义，MUST NOT 界定任务是否受年龄墙约束：超龄治理的唯一来源是统一 TTL 回收器，它覆盖全部 active 态与全部寿命类，无常驻豁免，也无与 TTL 并存的观测墙或第二条 deadline 墙（回收、续期、呈现与通知形态见 `async-task-lifetime` 能力）。

#### Scenario: 生命周期类按 Kind 推断并随声明复原

- **WHEN** 派生声明未显式给出生命周期类
- **THEN** 依 Kind 解析为 job 或 service，该类别随声明持久化并在重建时保持同一值

#### Scenario: 类别不改变年龄约束

- **WHEN** 一个常驻 service 任务与一个一次性 job 任务各自越过生效 TTL
- **THEN** 二者经同一回收器、同一退役通路被回收，service 不因类别获得豁免

### Requirement: 后台执行保留编排引用至实际停止

纳入任务层的子 agent／工具调用 SHALL 在派生执行启动前获取并持有其编排执行绑定引用，直到实际调用、响应流排空或执行 owner 确认停止后恰好释放一次。顶层 ACK、任务状态被置为终态、流早停或取消请求本身 MUST NOT 单独作为释放依据；spawn 被拒、dedup 命中与取消确认路径同样 MUST NOT 泄漏或提前释放派生引用。

结算与通知 SHALL 经任务所属 tagent 自己的 TaskManager、记录 sink 与 EventBus 完成；来源 metadata（含诊断用 generation 标记）随原管道保真传递，不改变既有任务状态机。A 跟踪一次对 B 的委派，不代表 A 接管 B 内部 C／exec 的任务。B 内部任务结算先回 B.bus，由 B 处理后再产生向 A 的输出；当前回复结束不关闭 B 或其无关任务。

#### Scenario: ACK 后热更不关旧资源

- **WHEN** G1 请求返回任务 ACK 后发布 G2，G1 的后台调用仍在运行
- **THEN** G1 所需绑定与资源保持，实际停止后恰好释放一次；task_settled 触发的新 turn 使用 G2

#### Scenario: 拒绝与去重不泄漏引用

- **WHEN** 派生调用因 spawn gate 拒绝或同名任务 dedup 而未实际启动
- **THEN** 该次尝试获取的执行引用被释放，不产生无主持有；既有任务继续沿用其原绑定

#### Scenario: 拒绝时 detector 已提前启动

- **WHEN** detector 的后台函数已启动，而 Spawn 随后被 gate 拒绝或 dedup 返回既有任务
- **THEN** 该次尝试先取消并等待其真实完成确认再释放派生引用；既有任务的执行、来源 metadata 与租约不受影响

#### Scenario: 后台不受父取消影响但仍保有绑定

- **WHEN** G1 任务已经 ACK 并脱离父取消，父 turn 结束且 G2 已发布
- **THEN** 后台继续使用 G1 的声明与目标，结果经原任务通知链返回；只有真实后台及其流收尾后才释放 G1，通知触发的新顶层 turn 使用当前代

### Requirement: spawnGate 的审计触发源与豁免放行

spawnGate 判定链 SHALL 支持行为审计触发源（self-telemetry-audit L3 冻结）：语义与 disk 退化 block 完全同构——进行中任务不受影响、新 spawn 返回 Blocked 并携带审计 reason。冻结期间，声明为保护性类别的任务（retention guard / mem_spill 重放 / 重试修复）MUST 被豁免放行；豁免依据任务声明（构造时确定），运行时不可自行标注。

#### Scenario: 审计源与 disk 源并存

- **WHEN** disk 退化与审计冻结同时生效
- **THEN** 两者 reason 分别如实呈现；豁免类任务仅在 disk 源下受检（审计源不拦截保护性任务）

### Requirement: 审批 Hold 与 Denied 分离

等待类结果 SHALL 与终局拒绝**分开建模**：审批等待 SHALL 登记为一个**未执行的等待任务**（`Hold`，属等待状态、非终局），并返回等待状态；**拒绝**（`Denied`）SHALL 为终局事实且工具**零执行**，并产生具名配对结果供模型读取。系统 MUST NOT 把 `Hold`（等待）伪装成 `Denied`（终局），也 MUST NOT 让被拒调用执行。

#### Scenario: 审批等待登记为未执行任务

- **WHEN** 一次调用进入审批等待
- **THEN** 其 SHALL 登记为等待任务并返回等待状态（`Hold`），工具 SHALL NOT 执行，MUST NOT 记为终局拒绝

#### Scenario: 拒绝零执行且产生具名配对结果

- **WHEN** 一次调用被拒绝
- **THEN** 工具函数 SHALL 零执行，模型上下文 SHALL 出现具名配对结果，该状态 SHALL 为终局（`Denied`）

#### Scenario: 等待态可观测

- **WHEN** 一个调用处于审批等待
- **THEN** 系统 SHALL 能观测到其等待状态，重复查询 MUST NOT 使其变为终局
