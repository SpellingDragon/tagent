## MODIFIED Requirements

### Requirement: 工具调用经 spawn + dense 窗口执行

每个受管工具或子 agent 调用 SHALL 经 TaskManager 的去重和准入后执行。调用 SHALL 在 dense 窗口等待本轮声明的条件 `exit`、`ready`、`input` 或 `signal`，与 producer 寿命分开；普通 stable/watch/probe 观察 MUST NOT 冒充条件满足。`completed`、`failed` 与带 Final 的观察 SHALL 结束本轮等待；Final 仅表示观察已结束，MUST NOT 把 suspect 等状态伪造为业务成功。

满足条件先到 SHALL 内联返回真实结果；detach 先到 SHALL 返回含 task id 的诚实 ACK 并继续后台跟踪。ACK MUST NOT 声称已完成或引导 sleep/轮询。旧版直接调用未声明 Mode/WaitFor 的首观察返回兼容，不得使生产命令默认 exit 条件被稳定输出提前满足。

#### Scenario: 静默长命令仍在 exit 等待
- **WHEN** oneshot 进程仍活着且 dense 内出现 stable/watch 观察
- **THEN** 观察可提交为事实，exit 等待不解除；进程退出或观察器 Final 到达才返回相应结果

#### Scenario: Final suspect 结束等待但不完成工作
- **WHEN** 本轮观察器以 Final suspect 结束
- **THEN** 调用等待解除，任务保持 suspect，结果交给模型判断而非记为 completed

#### Scenario: detach 与条件信号同时到达
- **WHEN** 条件信号恰在窗口交接边界到达
- **THEN** 同一信号恰走内联或后台一条路径，不被覆盖或丢失

### Requirement: 按任务类型的 settle 探测器与分档

探测器 SHALL 只报告确定性证据：命令 native pane 退出及退出码、显式 readiness/input 匹配或成功探针、稳定/可疑/watch/probe 边沿；子 agent 以逻辑调用结果作为完成信号。稳定输出 MUST NOT 证明进程退出，探针失败 MUST NOT 等同死亡。信号 SHALL 区分 Kind、Observation、Ready、WaitSatisfied 与 Final，并冻结产生时的 WaitFor、round、owner、seq、ObservedAt。

#### Scenario: native 非零退出
- **WHEN** tmux pane 原生状态证明进程非零退出
- **THEN** 结算包含失败极性及原生退出码，稳定时长不参与成功判定

#### Scenario: probe 失效后恢复
- **WHEN** 活进程的健康探针先失败后恢复
- **THEN** 系统记录相应边沿观察；仅明确成功证据可满足 ready，不把探针失败记成进程死亡

### Requirement: 服务型任务转 alive-detached

服务的前台等待条件 SHALL 与 producer 存活分别跟踪。生产 resident 服务 SHALL 由显式 ready pattern 或成功 probe 证明就绪，interactive SHALL 由 input pattern 证明等待输入；首次满足 ready 后可进入 alive_detached，重复非终局稳定/可疑观察可被抑制。job 的 stable/suspect MUST NOT 冒充 ready 或 completed，非终局稳定观察不解除默认 exit 等待。进程终态及 Final 观察仍 SHALL 通知。旧版无 Mode/WaitFor 的直接 service 声明可保留 stable 兼容语义，但 MUST 只抑制同producer round、同kind、同payload的重复观察；changed载荷仍进入管线。

detachedAt SHALL 持久化并沿原值恢复，MUST NOT 以恢复时刻替代。

#### Scenario: 服务已就绪但仍存活
- **WHEN** resident ready 条件先满足，进程稍后才退出
- **THEN** ready 只解除对应等待，producer 继续受跟踪与 TTL 约束，之后退出以新的 seq 通知

#### Scenario: job 稳定观察
- **WHEN** 包安装进程仅输出稳定而仍存活
- **THEN** 其 stable 作为观察记录，任务不进入 alive_detached，默认 exit 等待继续

### Requirement: task_settled 为通知类 input 事件

任务信号 SHALL 形成带 task id、producer round、round owner、seq、观察时间和冻结等待条件的自包含输入事实，不依赖原 tool_call 仍在历史中。生产端 SHALL 标记 `settle_wake=observe|decision`；普通观察 SHALL 提交、投影并写输入处置凭据，MUST NOT 单独调用模型；满足等待、终局、Final 或需裁决的输入 SHALL 经原调用总线驱动模型。混合用户/任务批次 SHALL 保留每条原始事实，MUST NOT 因部分输入已提交而跳过其余事实。

通知内容 SHALL 有界；大结果使用可读取的正文文件和尾部预览。存储失败 MUST 保留同一信号及其身份重试，MUST NOT 假装交付。取消 SHALL 产生原 producer 归属的可靠终局事实，不依赖旧 void OnCancel 的最佳努力记录。

#### Scenario: 纯观察批次
- **WHEN** 只有稳定、watch 或 probe 边沿观察到达
- **THEN** 系统提交原事实和精确 input_disposition，不启动模型且不丢失后续终局

#### Scenario: 用户与 volatile 任务结果混批
- **WHEN** durable 用户输入与未启用 inbox 的任务结果同批消费
- **THEN** 每条输入独立提交并保持结构化身份，Runner 合并回显不重复入库

#### Scenario: 大结果写入失败
- **WHEN** 终局输出正文无法持久保存
- **THEN** 信号拒绝受理并保留原 receipt 重试，MUST NOT 用不存在的路径票据确认成功

### Requirement: 迟到信号不得复活终态

信号 SHALL 在同一任务锁内按原 producer round 与观察窗口身份校验后再改状态或交付。旧 detector、迟到窗口交接、重复 seq MUST NOT 修改新轮状态或释放新轮等待。终态后仅允许原 receipt 的 ACK 清理，MUST NOT 重新打开业务状态或重复通知。

#### Scenario: 旧调用晚交接窗口
- **WHEN** 旧 Spawn 尚未交接其内联窗口，而 Resume 已绑定新轮
- **THEN** 旧调用只关闭自己捕获的窗口，不能让新轮提前返回

#### Scenario: 重复终局仅确认清理
- **WHEN** 已记录终态的原 receipt 重现
- **THEN** 系统仅按原身份确认清理，不产生新业务结算

## ADDED Requirements

### Requirement: 受理意图与资源绑定先于业务启动

生产 Spawn SHALL 依次完成去重/门控、OnSpawnAccepted 意图提交、TaskSpec.Start 资源准备、OnStartAccepted 绑定提交及 ActivatableDetector.Start 业务激活。被拒绝或 dedup 的 lazy 调用 MUST NOT 准备或启动资源；绑定未提交 MUST NOT 激活。已准备资源拒绑时 SHALL 清理本次新资源，MUST NOT 杀死被复用的旧 producer。旧 void 回调仅提供兼容，MUST NOT 使生产拒收被误记为成功。

#### Scenario: 意图提交失败
- **WHEN** OnSpawnAccepted 返回 false
- **THEN** lazy factory 不执行，业务零副作用

#### Scenario: 资源绑定提交失败
- **WHEN** 准备完成但 OnStartAccepted 返回 false
- **THEN** 新资源被取消且不激活，原有共享 producer 保持

### Requirement: 精确信号受理与 producer ACK

每个信号 SHALL 使用 task id、原 round 与 seq 构成稳定 SourceID，冻结 ObservedAt、WaitFor、owner、完整输出和结果极性。OnSettleAccepted/OnInlineSettleAccepted 及批量/补发的 accepted 回调返回 true 后才 SHALL 清除 pending、确认通知并调用原 detector 的 AcknowledgeSignal。无接收者 SHALL 视为拒收。拒收 SHALL 由独立 worker 以 400ms 至 4s 退避重试，不依赖 List、输入或冥想；重试 MUST NOT 换身份或时间戳。

#### Scenario: transport 拒绝后无新输入
- **WHEN** receiver 第一次拒绝某终局，之后没有用户消息或看板读取
- **THEN** 原信号自动重试，成功前 producer 证据不被 ACK 清理

#### Scenario: 批量通知拒绝
- **WHEN** 批量退役或终局补发受理失败
- **THEN** 每条原始信号保持 pending，并以原 detector 和身份进入可靠重试，不仅以 bus Publish 返回作为完成凭据
