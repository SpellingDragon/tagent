## Context

tagent 的 InjectMessage 通过 `runner.Run()` 启动全新 Run 处理 Tmux 通知。新 Run 的事件被 drain goroutine 丢弃（只计数不消费），且与主 Run 并发导致 Session 竞争。

框架分析结论：Flow 的 `IsFinalResponse → break` 是正确的——它表示"LLM 对这批输入的回应完成了"。问题不在 break，而在**没有持久循环在 Run 结束后继续处理下一批事件**。

trpc-agent-go 的 Runner.Run() 管道完整覆盖：Session 管理、消息追加、Plugin.OnEvent（MemoryPlugin + SummaryPlugin）、BeforeModel（SmartCompressor + injectEventKeyPrefixes）、AppendEventHook（Response.Clone 防御）。全部可以复用。

## Goals / Non-Goals

**Goals:**
- 持久 Loop：Run 结束后自动 drain mailbox 下一批事件，不退出
- 单消费者：只有 Loop goroutine 调用 runner.Run()，杜绝并发 Run
- 批量 drain：一次 drain 所有 pending 事件，合并为一条消息送入 runner.Run()
- 向后兼容：RunSimple 和 InjectMessage 签名不变，Loop 未启动时行为不变

**Non-Goals:**
- 不修改 trpc-agent-go 框架源码
- 不引入新事件类型（mailbox 就是 `chan model.Message`）
- 不实现异步 tool dispatch（Flow 内 tool.Call() 同步是框架设计）
- 不改动 CommandTool、AgentToolWrapper、Plugin 等现有组件
- 不实现跨 Session 事件触发

## Decisions

### D1: Loop of Runner.Run() — 拥抱 Break

Loop goroutine 循环调用 `runner.Run()`。每次 Run 的 break 表示"这批事件处理完了"，Loop 回到 drain mailbox 等待下一批。相同 userID/sessionID 确保跨 Run 的 Session 连续性。

```go
for {
    batch := drainMailbox(ta.mailbox)       // 阻塞等第一个 + non-blocking drain 剩余
    msg := mergeBatch(batch)                 // 合并为一条 model.Message
    eventCh, err := ta.runner.Run(ta.loopCtx, userID, sessionID, msg)
    if err != nil { continue }
    for evt := range eventCh {               // 转发到持久 outputCh
        ta.outputCh <- evt
    }
    if ta.loopCtx.Err() != nil { return }    // StopLoop 退出
}
```

**放弃的方案**：自建 PersistentAgentLoop 直接调用 `model.GenerateContent()`，手动复现 BeforeModel + Plugin.OnEvent + SessionService.AppendEvent。放弃原因：Runner.Run() 已完整覆盖这些管道，手动复现是重复造轮子且容易遗漏。

### D2: mailbox 就是 chan model.Message

不引入 MailboxEvent struct、MailboxEventType 枚举、EventSubmitter 接口。mailbox 就是 `chan model.Message`（cap=256）。DrainAll 是 10 行 inline 代码。事件来源区分由 `model.Message.Role` 表达（RoleUser = 用户输入，RoleSystem = Tmux 通知）。

### D3: InjectMessage 双模式（签名不变）

```go
func (ta *TagentAgent) InjectMessage(msg model.Message) {
    if ta.loopActive.Load() {
        ta.mailbox <- msg      // Loop 模式：写入 mailbox
        return
    }
    // One-shot 模式：现有逻辑（runner.Run + drain goroutine）
    // ... 不变 ...
}
```

CommandTool 调用 `injector.InjectMessage()` 的代码零改动。tagent.go 的 `cmdTool.SetMessageInjector(ta)` 零改动。

### D4: StartLoop/StopLoop — 唯一新增 API

```go
func (ta *TagentAgent) StartLoop(userID, sessionID string) (<-chan *event.Event, error)
func (ta *TagentAgent) StopLoop()
```

StartLoop 创建 mailbox + outputCh + loopCtx，启动 loop goroutine，返回持久 outputCh。调用方通过 outputCh 接收 agent 响应，通过 `evt.IsFinalResponse()` 判断单次响应完成。outputCh 在 StopLoop 后关闭。

RunSimple 保持不变——one-shot 模式下直接调用 runner.Run()，不经过 mailbox。

### D5: mergeBatch — 简单拼接

```go
func mergeBatch(msgs []model.Message) model.Message {
    if len(msgs) == 1 { return msgs[0] }
    var parts []string
    for _, m := range msgs {
        if m.Content != "" { parts = append(parts, m.Content) }
    }
    return model.Message{Role: model.RoleUser, Content: strings.Join(parts, "\n\n---\n\n")}
}
```

System 消息（Tmux 通知）和 User 消息混合时，简单拼接为一条 user 消息。LLM 在一次调用中看到全部事件的上下文。

## Risks / Trade-offs

**[R1] RunSimple 与 Loop 不能同时使用**
 接受：Loop 模式下调用方应使用 InjectMessage 提交事件 + 从 outputCh 读取响应。RunSimple 是 one-shot 模式的 API。两种模式互斥，StartLoop 后不应再调 RunSimple。

**[R2] Loop 模式下 outputCh 不关闭，调用方需用 IsFinalResponse 判断响应完成**
 接受：这是持久 Agent 的正确语义。outputCh 是事件流，IsFinalResponse 标记单次响应边界。

**[R3] mailbox 满时 Submit 阻塞**
 缓解：mailbox cap=256，正常场景不会满。若满则阻塞 Submit 调用方（背压），比丢弃事件更安全。

**[R4] Tmux 轮询延迟（30s）不变**
 接受：轮询是 async 命令检测的 pragmatic 例外。变更解决的是"检测到之后事件如何流动"，不是"如何检测"。
## Context

tagent 的 InjectMessage 通过 `runner.Run()` 启动全新 Run 处理 Tmux 通知。新 Run 的事件被 drain goroutine 丢弃（只计数不消费），且与主 Run 并发导致 Session 竞争。

框架分析结论：Flow 的 `IsFinalResponse → break` 是正确的——它表示"LLM 对这批输入的回应完成了"。问题不在 break，而在**没有持久循环在 Run 结束后继续处理下一批事件**。

trpc-agent-go 的 Runner.Run() 管道完整覆盖：Session 管理、消息追加、Plugin.OnEvent（MemoryPlugin + SummaryPlugin）、BeforeModel（SmartCompressor + injectEventKeyPrefixes）、AppendEventHook（Response.Clone 防御）。全部可以复用。

## Goals / Non-Goals

**Goals:**
- 持久 Loop：Run 结束后自动 drain mailbox 下一批事件，不退出
- 单消费者：只有 Loop goroutine 调用 runner.Run()，杜绝并发 Run
- 批量 drain：一次 drain 所有 pending 事件，合并为一条消息送入 runner.Run()
- 向后兼容：RunSimple 和 InjectMessage 签名不变，Loop 未启动时行为不变

**Non-Goals:**
- 不修改 trpc-agent-go 框架源码
- 不引入新事件类型（mailbox 就是 `chan model.Message`）
- 不实现异步 tool dispatch（Flow 内 tool.Call() 同步是框架设计）
- 不改动 CommandTool、AgentToolWrapper、Plugin 等现有组件
- 不实现跨 Session 事件触发

## Decisions

### D1: Loop of Runner.Run() — 拥抱 Break

Loop goroutine 循环调用 `runner.Run()`。每次 Run 的 break 表示"这批事件处理完了"，Loop 回到 drain mailbox 等待下一批。相同 userID/sessionID 确保跨 Run 的 Session 连续性。

```go
for {
    batch := drainMailbox(ta.mailbox)       // 阻塞等第一个 + non-blocking drain 剩余
    msg := mergeBatch(batch)                 // 合并为一条 model.Message
    eventCh, err := ta.runner.Run(ta.loopCtx, userID, sessionID, msg)
    if err != nil { continue }
    for evt := range eventCh {               // 转发到持久 outputCh
        ta.outputCh <- evt
    }
    if ta.loopCtx.Err() != nil { return }    // StopLoop 退出
}
```

**放弃的方案**：自建 PersistentAgentLoop 直接调用 `model.GenerateContent()`，手动复现 BeforeModel + Plugin.OnEvent + SessionService.AppendEvent。放弃原因：Runner.Run() 已完整覆盖这些管道，手动复现是重复造轮子且容易遗漏。

### D2: mailbox 就是 chan model.Message

不引入 MailboxEvent struct、MailboxEventType 枚举、EventSubmitter 接口。mailbox 就是 `chan model.Message`（cap=256）。DrainAll 是 10 行 inline 代码。事件来源区分由 `model.Message.Role` 表达（RoleUser = 用户输入，RoleSystem = Tmux 通知）。

### D3: InjectMessage 双模式（签名不变）

```go
func (ta *TagentAgent) InjectMessage(msg model.Message) {
    if ta.loopActive.Load() {
        ta.mailbox <- msg      // Loop 模式：写入 mailbox
        return
    }
    // One-shot 模式：现有逻辑（runner.Run + drain goroutine）
    // ... 不变 ...
}
```

CommandTool 调用 `injector.InjectMessage()` 的代码零改动。tagent.go 的 `cmdTool.SetMessageInjector(ta)` 零改动。

### D4: StartLoop/StopLoop — 唯一新增 API

```go
func (ta *TagentAgent) StartLoop(userID, sessionID string) (<-chan *event.Event, error)
func (ta *TagentAgent) StopLoop()
```

StartLoop 创建 mailbox + outputCh + loopCtx，启动 loop goroutine，返回持久 outputCh。调用方通过 outputCh 接收 agent 响应，通过 `evt.IsFinalResponse()` 判断单次响应完成。outputCh 在 StopLoop 后关闭。

RunSimple 保持不变——one-shot 模式下直接调用 runner.Run()，不经过 mailbox。

### D5: mergeBatch — 简单拼接

```go
func mergeBatch(msgs []model.Message) model.Message {
    if len(msgs) == 1 { return msgs[0] }
    var parts []string
    for _, m := range msgs {
        if m.Content != "" { parts = append(parts, m.Content) }
    }
    return model.Message{Role: model.RoleUser, Content: strings.Join(parts, "\n\n---\n\n")}
}
```

System 消息（Tmux 通知）和 User 消息混合时，简单拼接为一条 user 消息。LLM 在一次调用中看到全部事件的上下文。

## Risks / Trade-offs

**[R1] RunSimple 与 Loop 不能同时使用**
→ 接受：Loop 模式下调用方应使用 InjectMessage 提交事件 + 从 outputCh 读取响应。RunSimple 是 one-shot 模式的 API。两种模式互斥，StartLoop 后不应再调 RunSimple。

**[R2] Loop 模式下 outputCh 不关闭，调用方需用 IsFinalResponse 判断响应完成**
→ 接受：这是持久 Agent 的正确语义。outputCh 是事件流，IsFinalResponse 标记单次响应边界。

**[R3] mailbox 满时 Submit 阻塞**
→ 缓解：mailbox cap=256，正常场景不会满。若满则阻塞 Submit 调用方（背压），比丢弃事件更安全。

**[R4] Tmux 轮询延迟（30s）不变**
→ 接受：轮询是 async 命令检测的 pragmatic 例外。变更解决的是"检测到之后事件如何流动"，不是"如何检测"。
## Context

tagent 当前依赖 trpc-agent-go 的 Runner → Flow → LLMAgent 管道驱动 Agent。Flow 的 React Loop 在 `IsFinalResponse()` 时 break 退出，导致每次用户输入和 TmuxMonitor 注入都启动全新 Run。InjectMessage 的事件被 drain 丢弃，同一 Session 上的并发 Run 导致因果链竞争。

tagent 的设计目标是成为持久长程的 Agent 运行时（类似操作系统进程），Session 持续接收事件、压缩、运行，直到 tagent 关闭。这需要一个不退出的 Agent Loop，配合单消费者事件邮箱模型。

trpc-agent-go 提供的可复用组件：SessionService（会话管理 + AppendEvent）、Plugin Manager（OnEvent 钩子链）、model.Callbacks（BeforeModel 拦截）、model.Model（LLM 接口）。不复用的组件：Runner（每消息一个 Run）、Flow（IsFinalResponse break）、LLMAgent（封装 Flow）。

## Goals / Non-Goals

**Goals:**
- Agent Loop 持续运行不退出，agent_output 是"本轮输出"而非终止信号
- Event Mailbox 支持并发写入（用户、tool 回调、Tmux 轮询）、单 goroutine 消费、批量 drain
- Tool 调用异步分发，多个 agent-kind tool 可并行执行，结果作为 callback 事件写回 mailbox
- 同步工具（command exec）保持同步执行，不改变现有行为
- 最大化复用 trpc-agent-go 的 Session、Plugin、BeforeModel、MemoryStore 机制
- TmuxMonitor 轮询保留（async 命令检测的 pragmatic 例外），但 callback 目标改为 mailbox

**Non-Goals:**
- 不修改 trpc-agent-go 框架源码
- 不实现跨 Session / 跨 Agent 事件触发（tagent 运行时是独立的）
- 不引入事件总线 / 发布订阅模式（单 Session 单 Loop 足够）
- 不改变 MemoryStore / RelationStore / SmartCompressor 的内部实现
- 不改变声明式配置结构（Config / AgentConfig / ToolRef）
- 不实现 RAG 向量搜索（独立 change 范围）

## Decisions

### D1: 自建 PersistentAgentLoop，不复用 Flow

**选择**：在 tagent agent 包中新建 `PersistentAgentLoop`，直接调用 `model.Model.GenerateContent()`，手动构建 `model.Request`。

**理由**：Flow 的 `IsFinalResponse → break` 是其核心语义，无法配置关闭。持久 Loop 需要"agent_output 后继续等待事件"，与 Flow 的退出模型根本冲突。自建 Loop 允许精确控制 LLM 调用时机（仅在 drain 到事件时调用）。

**放弃的方案**：在 Flow 外层包一个 for 循环，Flow break 后重新调用 `runner.Run()`。放弃原因：(1) 每次 Run 创建新 Invocation 和新 event channel，状态不连续；(2) Flow 内部的 tool 调用是同步的，无法改为异步；(3) "批量 drain 事件"模型不兼容 Flow 的"单消息驱动"模型。

### D2: Event Mailbox 使用 Go channel + 批量 drain 模式

**选择**：`mailbox chan MailboxEvent`（buffered, cap=256），单 goroutine 消费。drain 模式：阻塞等待第一个事件，然后 non-blocking drain 所有后续 pending 事件。

```go
// drain pattern
events := []MailboxEvent{<-mailbox}  // 阻塞等第一个
draining:
    for {
        select {
        case evt := <-mailbox:
            events = append(events, evt)
        default:
            break draining
        }
    }
```

**理由**：Go channel 天然支持多写入者单读取者。批量 drain 确保同时到达的多个事件（如多个 tool result 同时返回）在同一次 LLM 调用中处理。

**MailboxEvent 类型**：
```go
type MailboxEvent struct {
    Type      MailboxEventType  // user_input | tool_result | tmux_notification
    Message   model.Message      // 事件内容（user/system 消息或 tool result）
    Source    string             // 事件来源标识（"user" | "knowledge" | "recall" | "tmux"）
    Timestamp time.Time
}
```

### D3: 异步 Tool Dispatch — agent tool 走 goroutine，sync tool 保持同步

**选择**：
- **同步工具**（command exec）：Loop goroutine 内直接调用 `tool.Call()`，结果立即作为 tool result 消息追加到 Session
- **异步工具**（knowledge, recall）：启动独立 goroutine 执行 `TagentAgent.Run()`（子 Agent 自己的 React Loop），完成后将结果作为 `MailboxEvent{Type: tool_result}` 写回 mailbox

**Pending 跟踪**：Loop 维护 `pendingToolCalls map[string]bool`（tool_call_id → pending）。当 LLM 输出包含 tool_calls 时：
1. 同步 tool：立即执行，结果追加到 messages，清除 pending
2. 异步 tool：dispatch goroutine，保持 pending
3. 如果所有 pending 已清除（同步 tool 已完成 + 无异步 tool 待返回）→ 继续下一次 LLM 调用
4. 如果有异步 pending → 回到 mailbox drain 等待 tool_result 事件

**理由**：同步 tool（exec 命令）通常快速完成，异步分发增加复杂度无收益。异步 tool（子 Agent React Loop）可能耗时数秒到数十秒，并行执行显著降低延迟。

**替代方案考虑**：所有 tool 都异步。放弃原因：command exec 通常 < 1s，异步化的调度开销不值得。

### D4: 手动构建 model.Request，复用 BeforeModel + Plugin

**选择**：PersistentAgentLoop 手动构建 `model.Request`：
1. 从 `Session.Events` 提取消息列表（复用框架的 `ContentRequestProcessor` 逻辑或简化版）
2. 注入 system prompt（从 TagentConfig.SystemPrompt）
3. 注入 tool 定义（从注册的 tools）
4. 调用 `BeforeModel` 回调（触发 SmartCompressor 压缩）
5. 调用 `model.GenerateContent()`
6. 对每个产出事件调用 `Plugin.OnEvent()`（触发 MemoryPlugin 持久化 + SummaryPlugin Tag 注入）
7. 调用 `SessionService.AppendEvent()` 持久化到 Session

**理由**：这是 Flow 内部做的事情，我们需要在 Loop 中手动复现。关键复用点：
- `BeforeModel`：ContextIntervention.BeforeModel（含 injectEventKeyPrefixes + SmartCompressor）
- `Plugin.OnEvent`：MemoryPlugin（事件持久化 + 因果链）+ SummaryPlugin（Tag 注入）
- `SessionService.AppendEvent`：含 AppendEventHook（Response.Clone 数据竞争防护）

**放弃的方案**：尝试复用 LLMAgent 的 request processor chain。放弃原因：processor chain 依赖 Flow 内部状态（Invocation、barrier 等），脱离 Flow 无法独立使用。

### D5: TmuxMonitor callback 改为写入 mailbox

**选择**：`CommandTool.handleStateChange()` 构造 system 消息后，调用 `mailbox.Submit(MailboxEvent{Type: tmux_notification, Message: msg})` 而非 `InjectMessage(msg)`。

**理由**：InjectMessage 启动新 Run 的根本问题是事件被 drain 丢弃。写入 mailbox 后，Loop 在下一轮 drain 中自然消费此事件，经过完整的 LLM → Plugin → Session 管道，事件不丢失。

**TmuxMonitor 轮询不变**：30s 间隔的状态检测逻辑保持不变。轮询是 async 命令检测的 pragmatic 例外——真正的难点不在于"如何感知命令完成"（轮询够用），而在于"感知到之后事件如何流动"（当前是启动新 Run 丢弃事件，改为写入 mailbox 让 Loop 消费）。

### D6: TagentAgent API 变更

**选择**：
```go
// 新 API
func (ta *TagentAgent) SubmitEvent(evt MailboxEvent)    // 提交事件到 mailbox
func (ta *TagentAgent) StartLoop() (<-chan *event.Event, error)  // 启动持久 Loop
func (ta *TagentAgent) StopLoop()                        // 停止 Loop

// 保留
func (ta *TagentAgent) MemStore() memory.MemoryStore
func (ta *TagentAgent) Close() error

// 移除
// RunSimple() — 替换为 SubmitEvent + StartLoop
// InjectMessage() — 替换为 SubmitEvent
// Run() — 内部由 Loop 驱动
```

**理由**：SubmitEvent 是非阻塞的（写入 channel），StartLoop 启动后台 goroutine 持续消费。调用方通过 event channel 接收 agent_output。这符合"并发写入、单消费"模型。

## Risks / Trade-offs

**[R1] 手动构建 model.Request 可能遗漏 LLMAgent processor chain 的功能**
→ 缓解：初期只复现 ContentRequestProcessor（消息历史）+ InstructionProcessor（system prompt）+ tool 定义注入。其他 processor（Planning、Identity、Skills、Time）按需逐步添加。编写集成测试验证 LLM 请求格式正确。

**[R2] 异步 tool dispatch 的错误处理复杂**
→ 缓解：tool agent goroutine 中 panic recover，错误转为 `MailboxEvent{Type: tool_result, Message: error_message}`。Loop 不区分成功/失败的 tool result，统一送入 LLM 上下文。

**[R3] BeforeModel 的 injectEventKeyPrefixes 依赖 Session.Events 的位置匹配，手动构建 Request 可能改变消息顺序**
→ 缓解：PersistentAgentLoop 严格按 Session.Events 顺序构建 messages，不重排。添加断言：`len(messages) ≈ len(events)`（允许 system/tool 消息偏差）。

**[R4] 单 goroutine 消费可能成为吞吐瓶颈**
→ 缓解：LLM 调用是主要耗时（秒级），channel drain 是微秒级。单消费者模型下，事件积累在 mailbox 中等待，不会丢失。实测如果 LLM 调用 < 30s，mailbox 不会溢出（cap=256）。

**[R5] 子 Agent 的 React Loop 仍然是同步的（子 Agent 内部用 Runner/Flow）**
→ 接受：子 Agent（knowledge, recall）内部可以继续用 Runner/Flow，因为子 Agent 的 Loop 是独立的、短生命周期的。只有顶层 Agent 需要持久 Loop。子 Agent 完成后通过 callback 事件返回结果。

**[R6] 不使用 Runner 意味着失去 Runner 的 Cancel/RunStatus 管理能力**
→ 缓解：PersistentAgentLoop 实现 `context.Context` 取消机制。StopLoop() 取消 context，Loop goroutine 退出。RunStatus 可通过 atomic 状态实现。

## Migration Plan

1. **Phase 1**: 新建 `agent/persistent_loop.go` + `agent/event_mailbox.go`，不修改现有文件。编写单元测试验证 mailbox drain + loop 迭代。
2. **Phase 2**: 新建 `agent/tool_dispatcher.go`，实现异步 tool dispatch。编写测试验证 knowledge/recall 异步执行 + callback。
3. **Phase 3**: 修改 `tagent.go` New() 组装逻辑，切换到 PersistentAgentLoop。修改 `command_tool.go` handleStateChange 写入 mailbox。
4. **Phase 4**: 移除旧代码（RunSimple, InjectMessage, Runner 依赖）。更新 wiki 文档。
5. **回滚策略**: Phase 3 之前，新旧 API 可以共存（TagentAgent 同时暴露 RunSimple 和 SubmitEvent/StartLoop）。如果新 Loop 有问题，可以回退到 Runner 模式。

## Open Questions

1. **子 Agent 是否也需要 PersistentAgentLoop？** 当前设计：子 Agent 保持用 Runner/Flow（短生命周期，同步调用）。如果未来子 Agent 也需要持久运行，可以递归应用此模式。
2. **多个异步 tool 的结果顺序**：当前设计不保证顺序——先完成的先写入 mailbox。LLM 在 drain 时看到所有已完成的 tool results。这是否影响 LLM 推理质量？需要实测。
3. **Session.Events 压缩时机**：BeforeModel 中的 SmartCompressor 压缩 `Request.Messages`（LLM 视图），不压缩 `Session.Events`。随着持久运行，Session.Events 会无限增长。是否需要定期裁剪 Session.Events？（当前由 trpc-agent-go 框架管理，可能已有机制）
