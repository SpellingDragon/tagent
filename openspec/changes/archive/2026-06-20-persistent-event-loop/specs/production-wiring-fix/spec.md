## MODIFIED Requirements

### Requirement: 优雅关闭流程

FileSegmentStore SHALL 提供 Close() 方法，按顺序停止 Compactor → 停止 LifecycleManager → flush TombstoneSet 持久化 → 关闭 RelationStore。Close() MUST 幂等（多次调用不 panic）。TagentAgent.Close() SHALL 先调用 StopLoop()（如果 Loop 正在运行），再关闭 registered closers，再关闭 MemoryStore，最后关闭 Runner。

#### Scenario: Close 停止所有后台组件

- **WHEN** 调用 store.Close()
- **THEN** Compactor.Stop() 被调用（停止压实调度）
- **AND** LifecycleManager.Stop() 被调用（停止 TTL 扫描）
- **AND** TombstoneSet 持久化到 KV 存储
- **AND** RelationStore.Close() 被调用

#### Scenario: Close 幂等

- **WHEN** 连续调用 store.Close() 两次
- **THEN** 第二次调用不 panic、不 error
- **AND** 后台组件不会被重复停止

#### Scenario: TagentAgent.Close 先停 Loop

- **WHEN** Loop 正在运行（loopActive=true），调用 TagentAgent.Close()
- **THEN** StopLoop() 先被调用（Loop goroutine 退出，outputCh 关闭）
- **AND** 然后关闭 registered closers（如 CommandTool）
- **AND** 然后关闭 MemoryStore
- **AND** 最后关闭 Runner

#### Scenario: Loop 未运行时 Close 正常

- **WHEN** Loop 未运行（loopActive=false），调用 TagentAgent.Close()
- **THEN** 不调用 StopLoop()（或调用但无副作用）
- **AND** 关闭 registered closers
- **AND** 关闭 MemoryStore
- **AND** 关闭 Runner
## MODIFIED Requirements

### Requirement: 优雅关闭流程

FileSegmentStore SHALL 提供 Close() 方法，按顺序停止 Compactor → 停止 LifecycleManager → flush TombstoneSet 持久化 → 关闭 RelationStore。Close() MUST 幂等（多次调用不 panic）。TagentAgent.Close() SHALL 先调用 StopLoop() 停止 PersistentAgentLoop，再关闭 CommandTool，最后关闭 MemoryStore。

#### Scenario: Close 停止所有后台组件

- **WHEN** 调用 store.Close()
- **THEN** Compactor.Stop() 被调用（停止压实调度）
- **AND** LifecycleManager.Stop() 被调用（停止 TTL 扫描）
- **AND** TombstoneSet 持久化到 KV 存储
- **AND** RelationStore.Close() 被调用

#### Scenario: Close 幂等

- **WHEN** 连续调用 store.Close() 两次
- **THEN** 第二次调用不 panic、不 error
- **AND** 后台组件不会被重复停止

#### Scenario: TagentAgent.Close 停止 Loop 后关闭组件

- **WHEN** 调用 TagentAgent.Close()
- **THEN** StopLoop() 被调用（PersistentAgentLoop goroutine 退出）
- **AND** 所有 CommandTool.Close() 被调用
- **AND** MemoryStore.Close() 被调用（触发 FileSegmentStore.Close 流程）
- **AND** 关闭顺序为 StopLoop → CommandTool → MemoryStore

## ADDED Requirements

### Requirement: TagentAgent 初始化创建 EventMailbox 和 PersistentAgentLoop

TagentAgent.New() SHALL 创建 EventMailbox 实例并暴露 SubmitEvent() 方法供外部提交事件。New() SHALL 创建 PersistentAgentLoop 实例（但不自动启动），通过 StartLoop() 显式启动。New() SHALL NOT 创建 Runner 实例——PersistentAgentLoop 直接使用 model.Model、SessionService、Plugin Manager。

#### Scenario: New 创建 Mailbox 和 Loop

- **WHEN** 调用 tagent.New(config) 创建 TagentAgent
- **THEN** TagentAgent 包含 EventMailbox 实例（buffered channel, cap=256）
- **AND** TagentAgent 包含 PersistentAgentLoop 实例（未启动状态）
- **AND** TagentAgent 包含 ToolDispatcher 实例（注册了 sync/async tool 分类）
- **AND** TagentAgent 不包含 Runner 实例

#### Scenario: StartLoop 启动后台 goroutine

- **WHEN** 调用 StartLoop()
- **THEN** PersistentAgentLoop goroutine 启动
- **AND** 返回 `<-chan *event.Event` 供消费者接收 agent_output
- **AND** Loop 进入 DrainAll() 阻塞等待第一个事件

### Requirement: TmuxMonitor callback 写入 EventMailbox

CommandTool.handleStateChange() SHALL 将 tmux 状态变更消息构造为 MailboxEvent{Type: "tmux_notification"} 并写入 EventMailbox，而非调用 InjectMessage()。CommandTool SHALL 通过 EventSubmitter 接口（Submit(evt MailboxEvent)）与 EventMailbox 交互，不直接持有 mailbox 引用。

#### Scenario: 状态变更写入 mailbox

- **WHEN** TmuxMonitor 检测到 session "build-42" 从 running → completed
- **THEN** handleStateChange 构造 MailboxEvent{Type: "tmux_notification", Source: "tmux", Message: system_message}
- **AND** 调用 EventSubmitter.Submit(evt) 写入 mailbox
- **AND** 不调用 InjectMessage()

#### Scenario: CommandTool 通过接口注入

- **WHEN** TagentAgent 创建 CommandTool
- **THEN** CommandTool 接收 EventSubmitter 接口（而非 TagentAgent 引用）
- **AND** CommandTool 不直接调用 TagentAgent.InjectMessage()
- **AND** CommandTool.handleStateChange 通过 EventSubmitter.Submit() 提交事件

### Requirement: 用户输入通过 SubmitEvent 提交

外部调用方（如 wechat-bot、CLI）SHALL 通过 TagentAgent.SubmitEvent() 提交用户消息到 EventMailbox。SubmitEvent() SHALL 构造 MailboxEvent{Type: "user_input", Source: "user", Message: user_message} 并调用 mailbox.Submit()。SubmitEvent() MUST 非阻塞。

#### Scenario: 用户消息提交到 mailbox

- **WHEN** 调用 SubmitEvent(model.Message{Role: RoleUser, Content: "你好"})
- **THEN** MailboxEvent{Type: "user_input", Source: "user"} 被构造
- **AND** 事件写入 mailbox channel
- **AND** SubmitEvent 立即返回（不等待 Loop 消费）
- **AND** Loop 在下次 DrainAll 时收到此事件
