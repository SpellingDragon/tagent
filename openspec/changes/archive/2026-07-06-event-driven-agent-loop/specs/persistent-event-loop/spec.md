## MODIFIED Requirements

### Requirement: StartLoop 启动持久事件循环

TagentAgent SHALL 提供 StartLoop(userID, sessionID) 方法，创建 EventBus、outputCh（chan *event.Event, cap=100）、loopCtx，并启动后台 AgentLoop goroutine。StartLoop SHALL 返回 outputCh 供调用方接收 agent 响应。outputCh 在 StopLoop 后关闭。StartLoop SHALL 设置 loopActive 标志为 true。

#### Scenario: StartLoop 返回持久 output channel

- **WHEN** 调用 StartLoop("user-1", "session-1")
- **THEN** 后台 AgentLoop goroutine 启动
- **THEN** 返回 `<-chan *event.Event`（outputCh）
- **AND** loopActive 为 true
- **AND** EventBus 已创建

#### Scenario: 重复调用 StartLoop

- **WHEN** loopActive 已为 true 时调用 StartLoop
- **THEN** 返回已存在的 outputCh
- **AND** 不创建新的 loop goroutine

### Requirement: Loop goroutine 持续运行不退出

AgentLoop goroutine SHALL 循环执行：Pull 事件 batch from EventBus → 调用 Preprocessor → 按需调用 model.GenerateContent → 解析 response（发布 tool_use 或 emit agent_output）→ 异步分发 tool_use → 回到 Pull。Loop SHALL NOT 在单轮 model 调用结束后退出。Loop SHALL 仅在 loopCtx 被取消（StopLoop）时退出。

Loop SHALL NOT 执行任何 trajectory 采集、reward 计算或 trajectory 存储逻辑。Loop 仅负责事件转发、日志记录和 OTLP span 属性设置。

#### Scenario: model 调用结束后继续 Pull

- **WHEN** agent loop 完成一轮 model 调用 + tool_use 分发
- **THEN** Loop 不退出
- **AND** Loop 回到 Pull 等待下一批事件

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

### Requirement: InjectMessage 发布事件到 bus

InjectMessage SHALL 检查 loopActive 标志。Loop 运行时（loopActive=true）SHALL 将消息构造为 `external_input` 类型的 AgentEvent 并发布到 EventBus。Loop 未运行时（loopActive=false）SHALL 丢弃消息并记录警告。InjectMessage 签名不变。

#### Scenario: Loop 模式发布到 bus

- **WHEN** loopActive=true，调用 InjectMessage(system_msg)
- **THEN** 构造 external_input 类型的 AgentEvent（source 标识来源）
- **AND** 发布到 EventBus
- **AND** 不直接调用 model
- **AND** AgentLoop 在下次 Pull 时收到此事件

#### Scenario: Loop 未运行时丢弃消息

- **WHEN** loopActive=false，调用 InjectMessage(system_msg)
- **THEN** 消息被丢弃
- **AND** 记录警告日志
