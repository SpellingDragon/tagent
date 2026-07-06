## ADDED Requirements

### Requirement: StartLoop 启动持久事件循环

TagentAgent SHALL 提供 StartLoop(userID, sessionID) 方法，创建 mailbox（chan model.Message, cap=256）、outputCh（chan *event.Event）、loopCtx，并启动后台 loop goroutine。StartLoop SHALL 返回 outputCh 供调用方接收 agent 响应。outputCh 在 StopLoop 后关闭。StartLoop SHALL 设置 loopActive 标志为 true。

#### Scenario: StartLoop 返回持久 output channel

- **WHEN** 调用 StartLoop("user-1", "session-1")
- **THEN** 后台 loop goroutine 启动
- **THEN** 返回 `<-chan *event.Event`（outputCh）
- **AND** loopActive 为 true
- **AND** mailbox 已创建（cap=256）

#### Scenario: 重复调用 StartLoop

- **WHEN** loopActive 已为 true 时调用 StartLoop
- **THEN** 返回已存在的 outputCh
- **AND** 不创建新的 loop goroutine

### Requirement: Loop goroutine 持续运行不退出

Loop goroutine SHALL 循环执行：drain mailbox（阻塞等第一个事件 + non-blocking drain 剩余）→ mergeBatch 合并为一条 model.Message → runner.Run() → 转发事件到 outputCh → 回到 drain。Loop SHALL NOT 在 Run 结束（Flow break）时退出。Loop SHALL 仅在 loopCtx 被取消（StopLoop）时退出。

#### Scenario: Run 结束后继续 drain

- **WHEN** runner.Run() 返回的 event channel 关闭（Flow 在 IsFinalResponse 时 break）
- **THEN** Loop 不退出
- **AND** Loop 回到 drain mailbox 等待下一批事件

#### Scenario: StopLoop 终止 Loop

- **WHEN** 调用 StopLoop()
- **THEN** loopCtx 被取消
- **AND** Loop goroutine 退出
- **AND** outputCh 被关闭

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
- **AND** 不调用 runner.Run()
- **AND** Loop 在下次 drain 时收到此消息

#### Scenario: One-shot 模式保持现有行为

- **WHEN** loopActive=false，调用 InjectMessage(system_msg)
- **THEN** 执行现有逻辑（runner.Run + drain goroutine）
- **AND** 行为与变更前完全一致

### Requirement: 复用 Runner 全部管道

Loop 调用 runner.Run() 时 SHALL 复用 trpc-agent-go 的全部管道：Session 管理（相同 userID/sessionID 跨 Run 连续）、消息追加（Runner 自动将 message 追加到 Session.Events）、Plugin.OnEvent（MemoryPlugin 持久化 + SummaryPlugin Tag 注入）、BeforeModel（SmartCompressor 压缩 + injectEventKeyPrefixes）、AppendEventHook（Response.Clone 防御）。

#### Scenario: 跨 Run Session 连续性

- **WHEN** Loop 第一批调用 runner.Run("user-1", "session-1", msg1)，第二批调用 runner.Run("user-1", "session-1", msg2)
- **THEN** 两批 Run 使用同一个 Session
- **AND** 第二批 Run 的 LLM 能看到第一批 Run 的事件历史
- **AND** SmartCompressor 在 token 超阈值时压缩历史

#### Scenario: Plugin 管道正常工作

- **WHEN** Loop 调用 runner.Run()，Runner 内部产生事件
- **THEN** MemoryPlugin.OnEvent 被调用（事件写入 MemoryStore + 因果链）
- **AND** SummaryPlugin.OnEvent 被调用（Tag 注入）
- **AND** AppendEventHook 执行 Response.Clone()
