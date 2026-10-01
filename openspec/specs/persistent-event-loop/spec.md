# persistent-event-loop Specification

## Purpose

持久事件循环:一个常驻 goroutine 反应式地 drain 事件邮箱、合并为一条消息、调用 `RunFlow` 执行一个 turn,并将 `task_settled` 等一等事件作为新 turn 的触发源。循环在 `Flow.Run` 返回时不退出,仅在显式 `StopLoop`(loopCtx 取消)时退出。
## Requirements
### Requirement: Loop goroutine 持续运行不退出

Loop goroutine SHALL 循环执行：drain mailbox（阻塞等第一个事件 + non-blocking drain 剩余）→ mergeBatch 合并为一条 model.Message → 调用 `FrameworkFlowAdapter.RunFlow` → 转发事件到 outputCh → 回到 drain。Loop SHALL NOT 在 Flow.Run 返回（单轮 ReAct 结束）时退出。Loop SHALL 仅在 loopCtx 被取消（StopLoop）时退出。

Flow.Run 内部 SHALL 由 `trpc-agent-go` 框架处理 ReAct 循环、工具执行和迭代控制。`AgentLoop` 不再自建 `callModel`、`handleResponse` 或 `dispatchToolUse`。

Loop SHALL NOT 执行任何 trajectory 采集、reward 计算或 trajectory 存储逻辑。Loop 仅负责事件转发、日志记录和 OTLP span 属性设置。

#### Scenario: Run 结束后继续 drain

- **WHEN** `FrameworkFlowAdapter.RunFlow` 返回的 event channel 关闭（Flow 在 final response 时结束）
- **THEN** Loop 不退出
- **AND** Loop 回到 drain mailbox 等待下一批事件

#### Scenario: StopLoop 终止 Loop

- **WHEN** 调用 StopLoop()
- **THEN** loopCtx 被取消
- **AND** Loop goroutine 退出
- **AND** outputCh 被关闭

#### Scenario: Loop 不采集 trajectory

- **WHEN** Loop 处理完一个 batch 的事件
- **THEN** 不创建 Trajectory 记录
- **AND** 不调用任何 RewardFunc
- **AND** 不调用任何 TrajectoryStore.Add
- **AND** 仅记录日志（batch 完成、duration、events、tokens）和 OTLP span 属性

### Requirement: 批量 drain mailbox

Loop SHALL 阻塞等待 mailbox 的第一个事件，然后 non-blocking drain 所有后续 pending 事件。DrainAll 返回 `[]model.Message`，保证至少包含 1 条消息。

#### Scenario: 单事件 drain

- **WHEN** mailbox 中只有 1 条 pending 消息
- **THEN** drain 返回 `[]model.Message{msg1}`
- **AND** mailbox 为空

#### Scenario: 批量 drain 多事件

- **WHEN** Loop drain 时 mailbox 中有 msg1、msg2、msg3
- **THEN** 返回 `[]model.Message{msg1, msg2, msg3}`
- **AND** mailbox 为空

#### Scenario: 等待第一个事件

- **WHEN** mailbox 为空
- **THEN** drain 阻塞直到有消息到达

### Requirement: mergeBatch 合并批量消息

Loop SHALL 将 drain 到的多条 model.Message 合并为一条 model.Message。合并规则：提取每条消息的 Content，用 "\n\n---\n\n" 连接。合并后消息 Role 为 RoleUser。单条消息时直接返回，不处理。

#### Scenario: 多消息合并

- **WHEN** drain 到 [system "tmux completed", user "构建结果如何？"]
- **THEN** 合并为 `model.Message{Role: RoleUser, Content: "tmux completed\n\n---\n\n构建结果如何？"}`

#### Scenario: 单消息不合并

- **WHEN** drain 到 [user "你好"]
- **THEN** 直接返回 `model.Message{Role: RoleUser, Content: "你好"}`

### Requirement: InjectMessage 双模式

InjectMessage SHALL 检查 loopActive 标志。Loop 运行时（loopActive=true）SHALL 将消息写入 mailbox（非阻塞，mailbox 满时阻塞作为背压）。Loop 未运行时（loopActive=false）SHALL 保持现有行为（启动新 Run + drain goroutine）。InjectMessage 签名不变。

#### Scenario: Loop 模式写入 mailbox

- **WHEN** loopActive=true，调用 InjectMessage(system_msg)
- **THEN** system_msg 写入 mailbox
- **AND** 不直接调用 Flow.Run
- **AND** Loop 在下次 drain 时收到此消息

#### Scenario: One-shot 模式保持现有行为

- **WHEN** loopActive=false，调用 InjectMessage(system_msg)
- **THEN** 执行现有逻辑（Run + drain goroutine）
- **AND** 行为与变更前完全一致

### Requirement: task_settled 作为一等事件触发回收 turn

`task_settled` SHALL 作为一等事件进入持久事件循环。当一个后台 Task settle 时,其 `task_settled` 事件 SHALL 经 EventBus 进入循环,并像外部输入一样触发一个新 turn(回收 turn);当循环空闲(阻塞在 Pull)时,`task_settled` SHALL 能唤醒它。

若一个 turn 正在进行,`task_settled` SHALL 排队至下一轮被消费,SHALL NOT 打断进行中的 turn。

#### Scenario: 空闲时任务完成唤醒循环

- **WHEN** 事件循环空闲(阻塞在 Pull)且一个后台任务 settle
- **THEN** `task_settled` 事件 SHALL 唤醒循环并触发一个回收 turn

#### Scenario: turn 进行中任务完成则排队

- **WHEN** 一个 turn 正在执行时某后台任务 settle
- **THEN** `task_settled` SHALL 排队,SHALL NOT 打断当前 turn,SHALL 在下一轮被消费

### Requirement: 循环不依赖 bus echo 自触发

持久事件循环 SHALL 仅通过 `bus.Pull` 等待外部/任务事件驱动下一轮；框架 SHALL NOT 将 agent_output(final 响应）回灌到 EventBus 作为自触发脉冲。final 响应的投递 SHALL 仅经 outputCh。

#### Scenario: turn 结束后循环静默等待

- **WHEN** 一个 turn 完成（final 响应已投递）且 bus 上无其他事件
- **THEN** 循环 SHALL 阻塞于 `Pull`，直到新的外部输入或任务事件到达
- **AND** SHALL NOT 出现由 agent_output echo 引发的空转唤醒

#### Scenario: 后台任务结算唤醒循环

- **WHEN** 循环阻塞于 `Pull` 时一个后台任务 settle 产生 task_settled 事件
- **THEN** 循环 SHALL 被该事件唤醒并开启回收 turn

### Requirement: outputCh 宽限与溢出落盘

RunFlow 向 outputCh 发送事件时 MUST 设 2s 宽限；超限将事件全文落盘至 `<workspace>/tool-output/output-overflow/` 并投递摘要票据事件（路径+首尾片段），不阻塞主循环、不静默丢弃；SessionHook default 丢弃分支同步计数可观测。

#### Scenario: 慢消费者不卡死主循环
- **WHEN** 消费者停止读取 outputCh 超过 2s
- **THEN** 主循环继续运行，事件落盘可经票据找回，SessionHook 丢弃计数增加

### Requirement: 循环停止为终结态

同一个 TagentAgent 的 StopLoop SHALL 为终结操作：再次 StartLoop SHALL 显式返回错误，不返回关闭通道，不创建第二个循环。需要重新运行时 SHALL 创建新 agent 并从事实链恢复。重复 Stop/Close SHALL 幂等；Start/Stop/Close 的并发状态转换 MUST 受同一生命周期同步机制保护。每次成功启动的输出通道 SHALL 恰关闭一次，循环异常退出也 SHALL 更新真实 active/terminated 状态。

#### Scenario: Stop 后 Start 的往返
- **WHEN** 同实例 StopLoop 返回后再次 StartLoop
- **THEN** 返回明确终结态错误；新实例可正常恢复并启动

#### Scenario: 两轮完整往返后停止
- **WHEN** 分别创建两个实例执行 Start→Stop→Close
- **THEN** 两个实例各关闭自己的通道一次，重复停止不 panic

#### Scenario: 并发启动停止
- **WHEN** Start/Stop/Close 并发执行或 loop 异常退出
- **THEN** active 状态与真实消费者一致，无 WaitGroup 误用、双关闭或后台孤儿

### Requirement: 可判定的接收结果

系统 SHALL 提供带 context 和稳定请求 ID 的接收入口，返回 receipt 或明确错误，区分 volatile/durable accepted。旧 void 入口 SHALL 保留兼容并记录拒绝计数，随载 HTTP/宿主 SHALL 使用新入口。关闭、满额、超时和存储失败 SHALL NOT 被表示为 accepted。202 SHALL 只表示接收，不表示任务处理/消息送达完成。

#### Scenario: volatile 模式满队列
- **WHEN** 未配置可靠 inbox 且队列满至超时
- **THEN** 返回明确背压错误，计数可见，不承诺持久成功

### Requirement: 可靠输入全序持久化

显式可靠模式 SHALL 将所有输入先写入有界 inbox-v1，文件和目录屏障完成后才返回 durable。发布序列 SHALL 由单一串行化点分配；消费者按该序处理，channel 只作唤醒。默认未确认上限 2560，满额/不可写/初始化失败 SHALL 拒绝，不回退 volatile。一次批量提交 SHALL 以单 envelope 接收，保留各消息的来源和稳定身份。

#### Scenario: 低负载可靠输入仍可恢复
- **WHEN** 只有一个输入收到 durable receipt 后进程终止
- **THEN** 新进程可恢复该输入，不依赖先填满 channel

#### Scenario: 并发与背压不超车
- **WHEN** 多生产者在积压临界点并发提交
- **THEN** 所有成功 receipt 有全序；超额明确拒绝，新事件不绕过旧持久项

### Requirement: 输入处理确认与幂等

claim SHALL 不删除原件。事实提交使用固定 EventKey 与源 ID 幂等，投影不重复。只有 turn 结束且处理结果 receipt 已写入事实链并耐久后，系统 SHALL ack inbox。崩溃后未完成 claim MUST 可重试，已确认 receipt MUST 不重复处理。处理 receipt SHALL 注册为非投影事件；对应 inbox 尚未确认清理时 SHALL 免于 TTL/容量淘汰，ack 删除及目录同步后按 30 天保留窗口处理。重启去重索引 SHALL 只覆盖 outstanding IDs；超过 30 天的客户端重复提交不承诺幂等。工具副作用 SHALL 明确为至少一次边界，不宣称 exactly-once。

#### Scenario: 取出后入库前崩溃
- **WHEN** claim 完成但事实提交未完成即终止
- **THEN** 原输入仍存在，恢复后可重试且不丢

#### Scenario: 入库后执行前崩溃
- **WHEN** 原始事件已提交但尚无处理完成 receipt
- **THEN** 恢复后继续/重试处理，原始事件与投影不重复追加

#### Scenario: receipt 后 ack 失败
- **WHEN** 处理完成 receipt 已耐久但删除 inbox 失败
- **THEN** 恢复只重试确认清理，不再次执行已确认输入

#### Scenario: 旧格式或损坏项
- **WHEN** 存在未迁移旧 spill 或不可读取的新 inbox 项
- **THEN** 旧格式未排空时拒绝启动并给迁移提示；损坏项保留隔离及告警，不静默删除

### Requirement: LLM 端点重定向逐跳受 allowlist 约束

动态端点重定向启用时，LLM HTTP client SHALL 安装 CheckRedirect 钩子：30x 跳转的每一跳目标 host MUST ∈ endpoint allowlist，越界跳转 SHALL 被拒绝且请求以明确错误终止；allowlist 未配置（重定向禁用语义）时任何跳转 SHALL 被拒绝。初始 URL 的既有校验（scheme/userinfo/fragment/host）不变。

#### Scenario: 端点 302 跳出 allowlist 被拒

- **WHEN** allowlist 为 `proxy.allowed.example` 且该端点 302 跳转到 `internal.metadata.host`
- **THEN** 第二跳被 CheckRedirect 拒绝，请求失败并返回明确的越界 host 信息，不发生任何对越界主机的请求

#### Scenario: 未启用重定向时跳转全拒

- **WHEN** 部署未启用动态端点（allowlist 空）且某响应携带 30x
- **THEN** 所有跳转被拒绝，行为与重定向禁用语义一致

### Requirement: 业务 turn 开始执行时绑定编排版本

常驻循环 SHALL 在冻结批次确定后、进入业务 turn 且首次读取编排绑定之前完成一次版本获取，并置于传输重试循环之外；一次业务 turn 的多次模型迭代与工具轮次使用同一绑定。正在排队的输入不提前绑定；task_settled 等一等事件触发的新顶层 turn 按开始执行时选择当前有效版本，其来源标记仅作诊断，不用于恢复旧执行。

由在途执行派生、进入另一 agent 事件管线的输入 SHALL 随入队携带派生引用并在消费时沿用发起版本，不因排队改读最新 effective；同一管线内不相容输入（不同父请求或不同继承代）不得混入同一批次。

#### Scenario: 排队的派生输入保持发起版本

- **WHEN** G1 执行中向 B 投递的委派输入仍在 B 的队列中等待，期间发布 G2
- **THEN** B 消费该输入时沿用 G1 的派生绑定；B 随后开始的独立新输入按 G2 处理，两者不混入同一批次

#### Scenario: 排队输入使用执行时版本

- **WHEN** 输入在 G1 期间入队，G2 发布后才开始执行
- **THEN** 该 turn 使用 G2，接收与冻结协议不变

#### Scenario: 重试继承同一版本

- **WHEN** 某 turn 的 RunFlow 传输失败进入重试，期间发布新版本
- **THEN** 重试仍使用该 turn 开始时获取的绑定，不中途切换

#### Scenario: task_settled 回流选当前代

- **WHEN** G1 期间派生的后台任务在 G2 发布后 settle 并回流为新 turn
- **THEN** 新 turn 使用 G2；原后台执行沿用其启动时绑定直至实际停止

### Requirement: 瞬时提交失败的重排覆盖完整冻结接收集

持久循环在瞬时提交失败熬尽退避后重排 claim 时，SHALL 以本批的完整冻结接收集（received，含混批让位的 meditation 事件）为口径释放 claim，MUST NOT 只重排 selected 子集——让位事件的 durable envelope 若不被释放，将永久滞留 claimed 态（进程内无人再领取）直到重启。

#### Scenario: 混批中 meditation 让位后瞬时失败熬尽退避

- **WHEN** 一个批次同时包含用户输入与 meditation 事件（meditation 让位、仅用户输入被 selected），durable 提交遭遇瞬时故障并熬尽退避预算
- **THEN** 批内全部 durable claim（含让位 meditation 的 envelope）回到 pending，下一轮 Pull 按序重新领取，无 claim 滞留 claimed 态

### Requirement: 不可解码 slot 触发整封隔离

durable inbox 消费侧（claimDurable）恢复 envelope 的 source_event slot 失败时，SHALL 对该 envelope 整封执行隔离（QuarantineEnvelope，保留原始字节），MUST NOT 保留 claimed 等待重试（全坏形态跨重启死循环），也 MUST NOT 让同封可解码 slot 的 completion 导致整封 Ack（半坏形态静默销毁未执行输入）。

#### Scenario: envelope 全部 slot 不可解码

- **WHEN** 某 envelope 的每个 source_event 反序列化均失败
- **THEN** 该 envelope 被移入隔离区并携带原因，不留在 inbox 的 claimed/pending 态循环重试，隔离区内可查原始字节

#### Scenario: envelope 部分 slot 不可解码

- **WHEN** 某 envelope 的一个 slot 解码失败而其余 slot 可解码
- **THEN** 该 envelope 整封隔离（同封好 slot 不进入本批、不形成 completion、不触发 Ack），未执行输入不被销毁

### Requirement: per-turn echo 凭据随 turn 清理统一回收

事件循环安装的批次 echo spec（turnEcho）的作用域 SHALL 在 turn 的每一条退路上终结——其清理 MUST 与 turn 租约释放共享同一 per-turn 清理闭包（endTurn），MUST NOT 依赖成功路径的末尾清理点单独执行。

#### Scenario: 重试循环内早退路径

- **WHEN** turn 在重试循环内因 ctx 取消或执行代关闭而 turnStop 早退
- **THEN** turnEcho 已被置空，同一 ContextManager 上后续任何批次不会命中陈旧 echo spec 而跳过本应存储的用户输入

### Requirement: 可靠模式的恢复能力与格式准入

可靠总线（有 durable inbox）在构造 agent 时 SHALL 以**硬准入**核验后端的显式重放能力：内层（含包装链）未实现 `EventReplayer` 即**拒绝构造**并指明是哪个 store 类型缺失该能力，MUST NOT 静默降级为"有写路径就算 durable"。材料保留能力（`RetentionGuard`）按可用注入、不可用时不强制扩张后端主接口——未启用可靠模式的自定义后端不因缺该接口而被拒。

格式准入：信封槽位编号不连续、source_event 非法、`prepared_fact` 携带非当前 `prepared_version`、completion 携带非当前 `completion_version` 时 MUST 报错拒绝，MUST NOT 猜测读取或自动迁移过渡数据。无准备材料的 pending 是当前合法的初始状态（不报错）。已有冻结材料但代际不符的数据不被自动读取——读错误与当前格式损坏 MUST NOT 被当作"旧数据"自动清空。

#### Scenario: 后端不具备重放能力

- **WHEN** 配置启用了可靠总线而所选 store 未实现显式重放接口
- **THEN** agent 构造失败并指明该 store 类型，不以待降级形态启动

#### Scenario: 冻结材料代际不符

- **WHEN** 已有 prepared_fact 的 `prepared_version` 与当前版本不一致
- **THEN** 读取以错误失败、材料保留原样，不被自动迁移也不被静默丢弃

### Requirement: 启动直接核对未确认完成证据

启动 SHALL 清点原始 outstanding 信封，对每一份以**其自身固定的 receipt key** 直接核对事实链，不依赖投影快照或尾部扫描顺带发现。核对内容含：request id、预留 receipt key、每个已处理槽位的 fact key 与写入前冻结的准备身份逐一对齐——任一漂移即为矛盾。

处置按四支分路，且彼此独立（一份坏信封不得阻塞其余健康者）：材料已准备而无 completion → 继续输入；completion 在而链上 receipt 缺 → 校验凭据后补投回执；两者匹配 → 直接清理；链上 receipt 与冻结的 receipt fact 不符、或状态自称 receipted 却无 durable completion、或 completion 不可解码 → **隔离**（保留字节，不重盖、不静默消费原件）。单次读取 I/O 失败计为 blocked 并保留材料，MUST NOT 当作可清理；列举（inventory）整体失败 SHALL 中止本轮核对——绝不凭残缺视图核对。

本核对 MUST 在恢复登记与保护租约武装**之后**、消费循环开始喂给可能遗忘的生产者**之前**执行。

#### Scenario: 回执 key 早于压缩边界

- **WHEN** 某 outstanding 项的 receipt key 落在已压缩的窗口内
- **THEN** 仍按其固定 key 直接核对事实链，不因投影侧已折叠而误判为缺失

#### Scenario: 身份漂移只隔离不改写

- **WHEN** completion 的某槽位 fact key 与信封内冻结的准备身份不一致
- **THEN** 该信封被隔离并报告原因，其余信封照常收敛，原件字节保留供检视

#### Scenario: 列举失败不核对残缺视图

- **WHEN** outstanding 目录列举本身返回错误
- **THEN** 整轮核对中止并上抛，不基于部分清单做任何清理决定

