## Why

tagent 的核心设计思想体现于 prototype/agent.go（126 行抽象实现）。原型是设计哲学的代码体现，用可替换的函数字段（`OnEvents`、`Compact`、`Run`、`ModelCompletion`）定义了一个可扩展的框架骨架。核心哲学：inputs 是 event flow 的投影，event bus 承载 event flow，inputs 满则触发 Compact 和 memory 持久化。

这一哲学用三个不变量编码：
- **不变量 1**：inputs 是投影（有界，读写同一份数据）——OnEvents 追加、ModelCompletion 读取、Compact 清理，操作同一份数据
- **不变量 2**：Compact 修改投影不修改事件流——清空投影，事件已在 bus 上流过
- **不变量 3**：工具结果回写 bus 不直接操作 inputs——goroutine 执行工具，结果回到 bus，下一轮 OnEvents 才追加

原型还隐含了三个设计要点：
- **所有输出回写 bus**：OnEvents 的返回值也被回写到 eventBus，与工具结果走同一路径
- **model 作为工具**：`tools["model"] = ModelCompletion`，model 和其他工具走同一个调用路径
- **批量处理**：DefaultRun Pull 第一个事件后非阻塞取出所有剩余事件组成 batch，一轮处理多个事件

生产代码在原型基础上扩展了 SessionService、MemoryPlugin、SmartCompressor、AgentToolWrapper 等多层级能力。在这个从抽象到具体的扩展过程中，文档与实现偏离了原型的不变量和设计要点，产生了多处矛盾。14 小时生产日志证实了后果：Session.Events 从 1 条增长到 130+ 条（22000+ tokens），action 子 agent 14 次重复调用同一命令直到 10 分钟超时。

需要一次性对齐所有 wiki 核心文档和 README，以原型哲学为校验标尺，确立设计目标、标注实现偏差，为后续代码重构提供契约。

## What Changes

- **明确 Session.Events 作为投影的身份** — Session.Events 是 event flow 的投影（有界工作内存），不是完整事件存储。完整数据在 MemoryStore。统一 memory-arch §4.3（EventReference 轻量引用）与 agent-arch §4（完整 event.Event）的矛盾，确立 Session = EventReference[] 为设计目标。
- **修正"视图转换原则"** — 当前 §12.2 错误地把 Session.Events 和 MemoryStore 同时保护。修正为：SmartCompressor 修改 messages 视图（不变）；Compact 修改 Session.Events 投影（新增能力，清理旧引用替换为 summary）；MemoryStore 永不被运行时操作修改（不变）。
- **明确 Compact 机制设计** — 定义 Compact 的触发条件、清理策略、与 onEvent 持久化的时序关系、与 SmartCompressor 的协作顺序。Compact 不是 SmartCompressor 的同义词——SmartCompressor 压缩 LLM 视图，Compact 清理 Session 投影。onEvent 在每次事件到来时实时持久化到 MemoryStore，Compact 在投影满时清理投影——两者不是同一时机。
- **明确 onEvent 五步协同** — 事件到来时 onEvent 执行：①事件提取（ExtractEventType 推断类型）→ ②记忆写入（MemoryStore 持久化 FullEvent）→ ③因果链（RelationStore.SetParent）→ ④StateDelta 填充（event_key/event_type）→ ⑤投影追加（Session.Events 追加 EventReference）。这五步在同一个 onEvent 调用中完成，保证 MemoryStore 与 Session 的一致性。
- **明确 Preprocessor 从 EventReference 构建 messages 的方式** — 最近的引用从 MemoryStore 拉取完整 Content，旧的引用直接用 EventSummary。这是 EventReference 投影的自然结果——投影不存完整内容，需要时从 MemoryStore 按需拉取。
- **明确 AgentLoop 与 SessionService 的读写关系** — 消除"AgentLoop 维护独立 session copy"的设计偏差。Session 存 EventReference（轻量），onEvent 追加引用，Preprocessor 读取同一份引用——读写同一份数据，不维护 copy。
- **明确 Compact 与 MaxToolIterations 的主次关系** — Compact 是主要控制阀门（Session 投影有界性），MaxToolIterations 是兜底（Compact 后 LLM 仍无法收敛时强制中断）。StartLoop 模式下 Compact 是唯一控制阀门；Run() 模式下 Compact + MaxToolIterations 协同。MaxToolIterations 复用框架 Invocation 的字段。
- **修正 MaxToolIterations 默认值** — 子 agent 默认 10（兜底），当前 200 标注为偏差。
- **明确 model 与 tool 的关系** — 原型中 `tools["model"] = ModelCompletion`，model 是工具的一种。生产中 model 独立为 `model.Model.GenerateContent`，因为 trpc-agent-go 的 model.Model 接口与 tool.CallableTool 不同。文档需说明这个映射。
- **明确 tagent 与 trpc-agent-go 的结合边界** — 深入框架内部发现，框架的 `LLMAgent` + `Flow` 已实现了 tagent AgentLoop 的大部分功能（ContentRequestProcessor 从 session 构建 messages、FunctionCallResponseProcessor 执行工具+迭代控制、BeforeModel 压缩 hook、Flow.Run ReAct 循环）。tagent 重复造了这些轮子。深层结合方向：tagent = `StartLoop` + `EventBus` + `LLMAgent.Run(Flow.Run)` + `MemoryStore` + `Compact` + `MeditationManager`。tagent 独有的只有持久循环和异步注入层面，ReAct 循环本身应复用框架 Flow。文档需记录"该做什么（复用 Flow/ContentRequestProcessor/FunctionCallResponseProcessor/BeforeModel）、不该做什么（不用 Runner）、重复造了什么轮子（AgentLoop/Preprocessor/dispatchToolUse）、深层结合的收益（~1000 行）和挑战（tmux 异步模型变化）"作为后续代码重构的指导。
- **统一原型哲学描述** — README 和 wiki 需阐述核心设计哲学和三个不变量，作为所有扩展的校验标尺。
- **同步所有文档中的数据流图、序列图、伪代码** — 确保三层模型（EventBus 事件流 / Session.Events 投影 / MemoryStore 永久存储）在所有文档中一致。
- **记录生产实现偏差作为已知技术债** — 当前实现偏差（Session 存完整事件、AgentLoop 维护 copy、MaxToolIterations=200、无 Compact）在文档中明确标注。

## Capabilities

### New Capabilities
- `session-projection`: Session.Events 作为 event flow 投影的设计规范——EventReference 轻量引用、有界性保证、Compact 清理机制、onEvent 五步协同、Preprocessor 按需拉取、与 MemoryStore 的读写一致性

### Modified Capabilities
- `event-driven-engine`: AgentLoop 的事件处理流程修正——消除"独立 session copy"、修正事件处理步骤描述（区分设计目标和实现偏差）、明确 shouldCallModel 与 Compact 的关系、批量处理的意义
- `context-compression`: SmartCompressor 与 Compact 的职责分离——SmartCompressor 压缩 LLM 视图(messages)，Compact 清理 Session 投影(EventReference[])，两者协作顺序，Compact 与 onEvent 持久化的时序关系
- `tool-agent-pipeline`: 子 agent 的 session 管理与迭代控制——Compact 为主 MaxToolIterations 为辅、子 agent session 有界性、model 与 tool 的映射关系

## Impact

- **文档变更范围**：README.md、README_EN.md、docs/wiki/agent/agent-architecture.md、docs/wiki/event/event-architecture.md、docs/wiki/memory/memory-architecture.md、docs/wiki/plugin/plugin-architecture.md
- **文档不涉及代码变更**：本变更仅修订文档，不修改任何 .go 文件。所有实现偏差在文档中标注为已知技术债。
- **后续代码变更的依据**：本文档确立的设计目标和原型不变量是后续代码重构的契约。实现偏差将在后续 change 中修复。
