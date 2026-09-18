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

