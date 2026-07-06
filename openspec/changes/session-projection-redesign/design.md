## Context

tagent 的核心设计思想体现于 prototype/agent.go（126 行抽象实现）。原型是设计哲学的代码体现，用可替换的函数字段（`OnEvents`、`Compact`、`Run`、`ModelCompletion`）定义了一个可扩展的框架骨架。

### 原型的三个不变量

**不变量 1：inputs 是 event flow 的投影（有界，读写同一份数据）**
- 原型：`inputs []string` — OnEvents 追加、ModelCompletion 读取、Compact 清理，操作同一份数据
- 映射：EventBus = 事件流（真相来源），Session.Events = 投影（有界工作内存），MemoryStore = 永久存储

**不变量 2：Compact 修改投影不修改事件流**
- 原型：`DefaultCompact() { inputs = inputs[:0] }` — 清空投影，事件已在 bus 上流过
- 映射：Compact 清理 Session.Events（移除旧引用），不碰 MemoryStore（永久存储），不碰 EventBus（已消费）

**不变量 3：工具结果回写 bus 不直接操作 inputs**
- 原型：`go func() { output := tool(...); eventBus <- Event{3, output} }()` — 工具结果回到 bus，下一轮 OnEvents 才追加到 inputs
- 映射：dispatchToolUse goroutine → bus.Publish(external_input, tool_result) → 下一轮 Pull 消费

### 原型隐含的设计要点

- **所有输出回写 bus**：OnEvents 的返回值（`output EventType != 0`）也被回写到 eventBus。这意味着 LLM 的响应（tool_use 或 final output）也回到 bus，与工具结果走同一路径。生产中 handleResponse 的 `bus.Publish(tool_use)` + `emitEvent` 体现了这一点。
- **model 作为工具**：`agent.tools["model"] = agent.ModelCompletion` — model 和其他工具注册在同一个 map 中，走同一个调用路径。生产中 model 独立为 `model.Model.GenerateContent`，因为 trpc-agent-go 的 `model.Model` 接口与 `tool.CallableTool` 不同（GenerateContent 返回 streaming channel，Call 返回同步结果）。文档需说明这个映射——model 在原型中是工具的一种，在生产中因框架接口差异而独立。
- **批量处理**：DefaultRun Pull 第一个事件后，非阻塞取出所有剩余事件组成 batch。确保同一轮内的多个事件按顺序追加到 inputs，LLM 一次性看到所有新事件，减少循环迭代次数。

### 一条事件的完整生命周期

```
Event 到达 EventBus
  │
  ▼
AgentLoop.Pull → 批量取出事件
  │
  ▼
onEvent 回调 (对应原型 case 1/3 的 append):
  ① 事件提取: ExtractEventType(evt) — 从 Message.Role 推断类型
     原型: EventType 直接给定 (1/2/3)
     生产: RoleUser → external_input, RoleAssistant+ToolCalls → thinking_plan, ...
  ② 记忆写入: MemoryStore.StoreEvent(FullEvent) — 永久持久化完整事件
     原型: 无 (inputs 就是全部记忆)
     生产: durability 保证, 进程崩溃不丢数据
  ③ 因果链: RelationStore.SetParent(key, parentKey) — 记录事件前驱
     原型: 无
     生产: recall 可沿因果链回溯
  ④ StateDelta 填充: event_key, event_type → evt.StateDelta
     原型: 无
     生产: 供 Preprocessor 前缀注入使用
  ⑤ 投影追加: Session.Events.append(EventReference) — 轻量引用
     原型: inputs = append(inputs, data)
     生产: 追加 key+type+summary (不存完整内容)
     关键: 读写同一份 Session.Events, 不维护 copy

Preprocessor.Process (对应原型的 "inputs 非空 → 调模型"):
  ① 从 Session.Events 构建 messages
     原型: 直接用 inputs (字符串数组)
     生产: 从 EventReference 构建
       最近的引用 → 从 MemoryStore 拉取完整 Content
       旧的引用 → 直接用 EventSummary
  ② event_key 前缀注入: [evt_K|type] content
     原型: 无 (inputs 就是字符串)
     生产: 让 LLM 看到事件 key, 可传递给子 agent
  ③ shouldCallModel 判断
     原型: len(inputs) > 0
     生产: batch 中有 external_input → true (tool_use 只 dispatch)
  ④ token 预算检查 + 压缩
     原型: inputs 太大 → Compact (清空一切)
     生产: 两阶段
       Stage A: SmartCompress (压缩 messages 视图) — 不碰 Session.Events
       Stage B: Compact (清理 Session.Events 投影) — 不碰 MemoryStore
     关键: SmartCompress 先, Compact 后; 两者作用于不同对象
     时序: onEvent 实时持久化到 MemoryStore, Compact 清理投影时不丢数据

Model + handleResponse (对应原型的 ModelCompletion):
  ① Model.GenerateContent(messages)
     原型: ModelCompletion(inputs) → ModelOutput
     生产: model.Model.GenerateContent + TrajectoryRecorder wrapper
  ② handleResponse
     原型: 返回 Event{3, output} → 回写 bus
     生产: tool_calls → emitEvent (onEvent + Session 追加) + bus.Publish(tool_use) + dispatch
           final → emitEvent (onEvent + Session 追加) + outputCh
     关键: handleResponse 的输出也回写 bus (不变量: 所有输出回写 bus)
           onEvent 再次执行五步协同 (记忆写入 + 投影追加)
```

### 从抽象到具体的扩展映射

| prototype 组件 | 正确扩展 | 偏差 |
|---------------|---------|------|
| `Event{EventType, EventData}` | `Event{Key, ParentKey, Type, Data, Summary}` + StateDelta | — |
| `inputs []string` | `Session.Events []EventReference`（轻量引用） | 当前存 `[]event.Event`（完整事件） |
| `DefaultCompact()` | Compact：保留最近 N 个任务，旧引用替换为 summary | 当前无 Compact，SmartCompressor 只压缩 messages 视图 |
| `OnEvents` (追加+调模型) | onEvent（持久化+追加）+ Preprocessor（构建+压缩）+ Model | onEvent 和 AgentLoop append 分离（维护 copy） |
| `tool goroutine → bus` | `dispatchToolUse → bus.Publish(external_input)` | — |
| `tools["model"]` | `model.Model.GenerateContent`（框架接口差异导致独立） | — |
| `Model.ModelCompletion` | `model.Model.GenerateContent` + TrajectoryRecorder wrapper | — |
| `RegisterTool` | `AgentToolWrapper`（子 agent 作为工具） | MaxToolIterations=200 形同虚设 |
| `OnEvents 返回值回写 bus` | `handleResponse → bus.Publish + emitEvent` | — |
| `Run/OnEvents/Compact 可替换` | `AgentLoop.Run / onEvent callback / Compact func` | — |

生产代码在原型基础上叠加了 SessionService、MemoryPlugin、SmartCompressor、AgentToolWrapper 等多层级扩展，总代码量增长到 5000+ 行。在这个扩展过程中，文档与实现偏离了原型的不变量，产生了三处自相矛盾：

1. **Session.Events 身份矛盾**（违反不变量 1）：memory-architecture.md §4.3 设计 Session 存 `EventReference`（轻量引用，投影），agent-architecture.md §4 描述 Session 存 `[]event.Event`（完整事件含 `*model.Response`，非投影）。生产实现遵循后者，导致 Session 无限增长——投影不应该无限增长。
2. **"视图转换原则"过度保护**（违反不变量 2）：agent-architecture.md §12.2 规定 SmartCompressor 不修改 Session.Events 和 MemoryStore。MemoryStore 的保护是对的（永久存储），但 Session.Events 是投影，应该可被 Compact 清理——Compact 修改投影不违反任何原则。
3. **AgentLoop 维护独立 session copy**（违反不变量 1）：因为 SessionService 返回 clone，AgentLoop 不得不额外 append 到自己的 session copy。读写不再操作同一份数据——onEvent 写 service 内部对象，AgentLoop 写自己的 copy，Preprocessor 读 copy。如果 Session 存 EventReference（轻量），不需要 copy。

14 小时生产日志证实了这些偏差的后果：Session.Events 从 1 条增长到 130+ 条（22000+ tokens），action 子 agent 14 次重复调用同一命令直到 10 分钟超时。

## Goals / Non-Goals

**Goals:**

- 确立 Session.Events 作为"event flow 投影"的设计身份，统一所有文档中的描述
- 修正"视图转换原则"，区分 SmartCompressor（压缩 LLM 视图）与 Compact（清理 Session 投影）的职责
- 消除文档中 Session 存完整事件 vs EventReference 的矛盾，确立 EventReference 为设计目标
- 消除"AgentLoop 维护独立 session copy"的文档描述，回归"读写同一份投影"的原型哲学
- 明确 onEvent 五步协同作为事件到来时的标准处理流程
- 明确 Preprocessor 从 EventReference 按需拉取完整内容构建 messages 的方式
- 明确 Compact 与 onEvent 持久化的时序关系（onEvent 实时持久化 → Compact 安全清理投影）
- 明确 Compact 与 MaxToolIterations 的主次关系（Compact 为主，MaxToolIterations 为辅）
- 明确 model 与 tool 的映射关系（原型 model 作为工具 → 生产 model 独立因框架接口差异）
- 在所有数据流图、序列图、伪代码中统一三层模型表示
- 标注当前实现偏差为已知技术债，为后续代码重构提供契约

**Non-Goals:**

- 不修改任何 .go 代码文件
- 不设计 EventReference 的具体 Go 结构体实现（那是后续代码 change 的工作）
- 不设计 Compact 的具体算法实现
- 不设计 MemoryStore 的 compaction/lifecycle 策略
- 不修改 openspec 之外的工具链或构建系统

## Decisions

### D1: Session.Events = EventReference[]（设计目标），当前实现 = []event.Event（已知偏差）

**决策**：文档统一采用 memory-architecture.md §4.3 的设计——Session 存 EventReference（key + type + summary），不存完整事件。当前实现存 `[]event.Event` 标注为"实现偏差"。

**理由**：原型 `inputs []string` 是投影，轻量，有界。EventReference 是它的自然扩展——从存字符串到存引用。完整数据在 MemoryStore（onEvent 时持久化），Session 只需引用。

**备选方案**：维持当前设计（Session 存完整事件），用 SmartCompressor 做更激进的压缩。否决——这无法解决 Session.Events 无限增长的根因，压缩只作用于 messages 视图，Session 永不清理。

### D2: Compact 修改 Session.Events（投影），SmartCompressor 修改 messages（视图）

**决策**：两个操作分离——
- SmartCompressor：压缩 `[]model.Message`（发给 LLM 的视图），不修改 Session.Events。已有实现不变。
- Compact：清理 Session.Events（移除旧 EventReference，替换为 summary reference），不修改 MemoryStore。新增设计。

**协作顺序**：token(Session.Events 对应的 messages) > threshold 时，先 SmartCompress（压缩视图），如果仍超限则 Compact（清理投影）。

**与 onEvent 持久化的时序关系**：onEvent 在每次事件到来时实时持久化 FullEvent 到 MemoryStore（五步协同的第②步）。Compact 在投影满时清理 Session.Events 中的旧引用——此时完整数据已在 MemoryStore 中，Compact 不丢数据。两者不是同一时机：onEvent 是实时的、每事件一次；Compact 是批量的、投影满时触发。

**理由**：原型 `DefaultCompact()` 清空 inputs。扩展为"保留最近 N 个任务 + 旧引用替换为 summary"更精细，但遵循同样原则——Compact 修改投影。MemoryStore 在 onEvent 时已持久化，Compact 不需要再持久化。

### D3: 消除"AgentLoop 维护独立 session copy"

**决策**：文档删除"AgentLoop 在 onEvent 之外额外 append 到 al.session.Events"的描述。设计目标中，onEvent 追加 EventReference 到 Session（轻量，几乎不会失败），Preprocessor 从同一份 Session.Events 读取。读写同一份数据。

**理由**：当前 AgentLoop 维护 copy 的根因是 SessionService 返回 clone + Session 存完整事件。如果 Session 存 EventReference（几十字节），append 操作的失败概率极低，不需要维护 copy。

**当前实现偏差标注**：生产代码因 Session 存完整 `event.Event` 且 SessionService 返回 clone，确实需要维护 copy。文档标注为"实现偏差，待后续 change 修复"。

### D4: Compact 为主，MaxToolIterations 为辅

**决策**：文档明确 Compact 是主要控制阀门（Session 投影有界性），MaxToolIterations 是兜底（Compact 后 LLM 仍无法收敛时强制中断）。

**两种模式的控制策略**：
- **StartLoop 模式**（持久循环，对应原型 DefaultRun）：Compact 是唯一控制阀门。Session.Events 超限时 Compact 清理投影，LLM 看到干净的有界上下文。MaxToolIterations 不需要（或设很大）。
- **Run() 模式**（子 agent，有边界）：Compact + MaxToolIterations 协同。Compact 保持 Session 有界，MaxToolIterations 作为"Compact 后 LLM 仍无法收敛"的兜底中断。默认值 10。

**MaxToolIterations 复用框架**：trpc-agent-go 的 `agent.Invocation` 已有 `MaxToolIterations` 字段（`invocation.go:144`）。tagent 复用此字段，不自建。

**当前实现偏差标注**：生产代码 MaxToolIterations 默认 200，且无 Compact，MaxToolIterations 被当成唯一控制阀门。文档标注为实现偏差。

### D5: model 与 tool 的映射

**决策**：文档说明原型中 `tools["model"] = ModelCompletion`（model 是工具的一种），生产中 model 独立为 `model.Model.GenerateContent`，因为 trpc-agent-go 的 `model.Model` 接口（`GenerateContent → <-chan *model.Response`）与 `tool.CallableTool` 接口（`Call → (json.RawMessage, error)`）不同——前者返回 streaming channel，后者返回同步结果。

**理由**：这个映射关系说明"model 独立"不是偏离原型——原型的统一调用路径在原型中通过"同一 map"实现，在生产中通过"AgentLoop 统一调用 model + 统一 dispatch tool"实现。形式不同，本质一致：AgentLoop 是唯一的调用者。

### D6: 三层模型统一表述

**决策**：所有文档统一使用以下三层模型表述：

```
层1: EventBus AgentEvent — 事件流（真相来源，临时，Publish→Pull 后丢弃）
层2: Session.Events EventReference[] — 投影（有界工作内存，可 Compact）
层3: MemoryStore FullEvent — 永久存储（不可变，onEvent 写入，recall 查询）
```

**替代当前文档中不一致的表述**：agent-arch §4 "Session Events（工作内存，完整未压缩）" → "Session Events（投影，有界工作内存）"。

### D7: tagent 与 trpc-agent-go 的结合边界

**决策**：tagent 基于原型哲学扩展，复用 trpc-agent-go 的接口原语，但替换其同步执行模型。文档 MUST 明确记录"该做什么、不该做什么"作为扩展指导。

**该做（复用框架，自然结合）**：

| 框架能力 | 原型对应 | tagent 使用方式 | 理由 |
|---------|---------|----------------|------|
| `agent.Agent` 接口 | `BaseTAgent`（有 Run, RegisterTool） | TagentAgent 实现 agent.Agent | 让 tagent 可被框架组件（A2A, Runner）消费 |
| `model.Model` 接口 | `Model.Completion` | 直接用框架 model + TrajectoryRecorder wrapper | model 的 streaming 是框架核心设计，不重新实现 |
| `tool.Tool / CallableTool` 接口 | `tools map[string]func` | AgentToolWrapper 实现 CallableTool | 让 tagent 工具可被框架的 agenttool 发现和调用 |
| `plugin.Plugin + OnEvent` 钩子 | 无（OnEvents 直接操作 inputs） | MemoryPlugin/SummaryPlugin 注册为框架 Plugin | 框架的 OnEvent 钩子是 onEvent 五步协同的载体 |
| `session.Service`（部分） | 无（inputs 是唯一记忆） | 用 sessionSvc 管理 session 生命周期 | 框架的 session 持久化能力有价值 |
| `event.Event` 结构体 | `Event{EventType, EventData}` | 直接用框架 event.Event | StateDelta 是 event_key 注入载体，IsFinalResponse 是 outputCh 判断依据 |
| `Invocation.MaxToolIterations` | 无（不需要） | 子 agent Run() 模式复用此字段 | 框架已提供迭代控制，不自建 |

**不该做（偏离原型，重复造轮子）**：

深入 trpc-agent-go 内部发现，框架的 `LLMAgent` + `Flow` 已经实现了 tagent AgentLoop 的大部分功能。以下"不该做"项分为两类：**因同步模型不适用而不用的**（但深层结合后可能重新使用）和**重复造轮子应该删除的**。

| 框架已有能力 | tagent 自建替代 | 判定 |
|------------|---------------|------|
| `ContentRequestProcessor` (从 session.Events 构建 messages, 已有 MaxHistoryRuns/Summary) | `Preprocessor.Process` | **重复造轮子** — 框架已从 session 构建 messages, tagent 应复用 |
| `FunctionCallResponseProcessor` (执行工具 + MaxToolIterations + IncToolIteration) | `dispatchToolUse` | **重复造轮子** — 框架已执行工具+迭代控制, tagent 应复用 |
| `BeforeModel callbacks` (压缩 hook, 在 Flow.preprocess 中自动调) | `Preprocessor` 内调 `SmartCompressor.Compress` | **重复造轮子** — 框架已有压缩 hook 点, SmartCompressor 应注册为 BeforeModel |
| `Flow.Run` (for 循环: preprocess→callLLM→processResponse→检查退出) | `AgentLoop.Run` (Pull→Process→Model→Dispatch) | **部分重复** — 框架的 ReAct 循环可复用, 但缺少 EventBus 异步注入 |
| `Runner` | `StartLoop` | 不用 — Runner 是同步入口, StartLoop 是持久循环 |
| `Flow / runOneStep` | `AgentLoop.Run` 内部循环 | **深层结合后可复用** — 见下方 D8 |
| `BeforeModel / AfterModel` 回调 | `Preprocessor` 显式调用 | **深层结合后可复用** — 见下方 D8 |

**深层结合方向（D8）**：

框架的 `Flow.Run` 已经是 ReAct 循环（preprocess→callLLM→processResponse→工具执行→迭代控制→退出检查）。tagent 的 `AgentLoop.Run` 也做同样的事，但多了 EventBus。深层结合的方式：

```
StartLoop (tagent 独有, 持久循环):
  for {
    events = EventBus.Pull(ctx)          ← tagent 独有: 异步事件批量取出
    
    for evt in events where external_input:
      onEvent(evt)                        ← 框架 Plugin.OnEvent: 持久化+StateDelta
      Session.append(EventReference)      ← 投影追加
    
    if hasExternalInput:
      invocation = NewInvocation(session, message, ...)
      llmAgent.Run(ctx, invocation)       ← 框架! Flow.Run: ReAct 循环
        └── Flow.runOneStep:
              preprocess:
                ContentRequestProcessor   ← 框架! 从 session.Events 构建 messages
                BeforeModel               ← 框架! SmartCompressor 注册于此
              callLLM                     ← 框架! model.GenerateContent
              processStreamingResponses:
                AfterModel                 ← 框架!
                FunctionCallResponseProcessor ← 框架! 执行工具+MaxToolIterations
              检查 EndInvocation / IsFinalResponse
    
    → Flow.Run 结束后回到 StartLoop, 等下一个事件
  }
```

**tagent 独有（不自建, 框架没有）**：
- `EventBus` + `StartLoop`：持久事件循环 + 异步事件注入
- `MemoryStore`：FullEvent + 因果链（框架的 memory.Service 是 KV, 不是事件链）
- `InjectMessage`：TmuxMonitor/Meditation 回调入口
- `Compact`：Session 投影清理（框架没有, 因为框架 Session 存完整事件不需要清理）
- `MeditationManager`：定时冥想
- `TrajectoryRecorder`：LLM 调用轨迹记录

**深层结合的收益**：
1. 删除 `AgentLoop.Run`（500 行）→ 用 `LLMAgent.Run(Flow.Run)`
2. 删除 `Preprocessor.Process`（250 行）→ 用 `ContentRequestProcessor` + `BeforeModel`
3. 删除 `dispatchToolUse`（100 行）→ 用 `FunctionCallResponseProcessor`
4. 删除 `handleResponse`（80 行）→ 用 `processStreamingResponses`
5. 删除 `callModel`（40 行）→ 用 `Flow.callLLM`
6. 总计可减少 ~1000 行代码，同时获得框架的 tracing/telemetry/jsonrepair 等能力

**深层结合的挑战**：
1. 框架的 `Flow.Run` 是同步的——一次调用处理一条消息到 final response。tagent 需要在 `StartLoop` 中循环调用 `LLMAgent.Run`，每次调用处理一条消息
2. TmuxMonitor 的异步回调通过 `InjectMessage` 进入 bus，成为下一个 `external_input`，下一轮 `StartLoop` Pull 处理。这改变了 tmux 异步的模型——不再是"AgentLoop 在 Pull 中等待 tmux 结果"，而是"Flow.Run 结束后回到 StartLoop, tmux 结果作为下一条消息进入"
3. Compact 需要在 `BeforeModel` 回调中实现（而非 Preprocessor 中），因为 `ContentRequestProcessor` 已经构建了 messages

**关键原则**：tagent 不是在框架之上叠加，而是用框架的积木搭建一个不同的执行引擎——但"不同"只在持久循环和异步注入层面，ReAct 循环本身应该复用框架的 Flow。tagent = `StartLoop` + `EventBus` + `LLMAgent.Run(Flow.Run)` + `MemoryStore` + `Compact` + `MeditationManager`。

## Risks / Trade-offs

- **[文档与实现差距扩大]** → 文档确立 EventReference 为设计目标，但实现仍是完整事件。差距通过"已知技术债"标注明确，不隐藏。
- **[Compact 概念新增可能引起混淆]** → 文档中严格区分 SmartCompressor（压缩视图）和 Compact（清理投影），用对比表和流程图消歧。
- **[onEvent 五步协同增加理解复杂度]** → 用一条事件的完整生命周期图展示五步的顺序和依赖关系，而非抽象描述。
- **[子 agent MaxToolIterations=10 可能过小]** → 文档说明可配置，10 是默认值不是硬限制。Compact 生效后 10 次足够兜底。
- **[消除 session copy 描述后，当前代码注释不准确]** → 代码注释在后续 code change 中修复，本变更只修改文档。
- **[偏离框架 Session 设计]** → tagent 的 EventReference 设计偏离框架的 `Session.Events []event.Event`。文档 MUST 说明这是有意识的设计决策（tagent 有 MemoryStore 作为完整事件存储，Session 不需要重复存储），而非误用框架。
- **[深层结合 D8 改变 tmux 异步模型]** → 当前 tmux 异步通过 AgentLoop 在 Pull 中等待结果实现。深层结合后变为 Flow.Run 结束后回到 StartLoop，tmux 结果作为下一条消息进入。文档 MUST 标注此为后续代码重构的设计方向，当前文档修订不改变现有行为描述。
