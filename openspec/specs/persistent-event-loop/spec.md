## MODIFIED Requirements

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
