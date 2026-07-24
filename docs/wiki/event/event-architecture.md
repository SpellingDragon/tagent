# tagent/event 模块架构文档

## 一、模块定位

`tagent/event` 是 tagent 的**事件类型与摘要工具**包，为 `MemoryPlugin` 和 `SummaryPlugin` 提供统一的事件分类和摘要生成能力。

**核心职责**：
- 定义 tagent 专属的事件类型常量（`external_input`、`agent_output` 等）
- 提供事件类型推断函数（`ExtractEventType`）
- 提供摘要生成函数（`GenerateEventSummary`），**严格禁止任何形式的截断**

**设计原则**：
- **严格拒绝非设计折损**：摘要是设计内的信息折损，截断是设计外的双重折损，会破坏压缩质量
- **统一 event type 分类**：所有非 `agent_output` / `action_command` 的角色统一归为 `external_input`
- **零外部依赖**：仅依赖 `trpc-agent-go/model`，不依赖框架其他模块

---

## 二、文件清单

| 文件 | 行数 | 职责 |
|------|------|------|
| `types.go` | 171 | 事件类型常量、类型推断、摘要生成、Token 估算 |

---

## 三、组件关系总览图

```mermaid
graph TB
    subgraph "tagent/event"
        ET["ExtractEventType(msg)\n推断事件类型"]
        IS["IsSpecialEventType(type)\n判断是否特殊事件"]
        GS["GenerateEventSummary(msg, type, opts)\n生成摘要"]
        TC["EstimateTokens(text)\nToken 估算"]
    end

    subgraph "tagent/plugin"
        MP["MemoryPlugin\nOnEvent"]
        SP["SummaryPlugin\nOnEvent"]
    end

    MP --> ET
    MP --> GS
    SP --> ET
    SP --> GS

    GS --> TC
```

---

## 四、事件类型常量

### 4.1 完整常量列表

```go
// event/types.go:18-44
const (
    // 所有外部输入（用户消息、API 调用、系统注入消息、后台任务结算 task_settled）
    TypeExternalInput   = "external_input"

    // Agent 最终回复
    TypeAgentOutput     = "agent_output"

    // 工具/命令执行
    TypeActionCommand   = "action_command"

    // 规划相关
    TypeThinkingPlan    = "thinking_plan"

    // 记忆召回
    TypeThinkingRecall  = "thinking_recall"

    // 知识检索
    TypeThinkingKnowledge = "thinking_knowledge"

    // 上下文压缩
    TypeContextCompress = "context_compress"
)
```

### 4.2 类型分类逻辑

```mermaid
graph LR
    msg["model.Message"] --> role["msg.Role"]

    role -->|"RoleUser"| ext["external_input"]
    role -->|"RoleSystem\n(TmuxMonitor)"| ext
    role -->|"RoleAssistant\n无 ToolCalls"| ao["agent_output"]
    role -->|"RoleAssistant\n有 ToolCalls"| tp["thinking_plan"]
    role -->|"RoleTool"| ac["action_command"]

    ext --> special["IsSpecialEventType = true"]
    ao --> special
    tp --> special
    ac --> notspecial["IsSpecialEventType = false"]
```

### 4.3 RoleSystem 的双重身份

| 场景 | Message.Role | 是否参与事件流 | EventType | 说明 |
|------|-------------|-------------|-----------|------|
| System Prompt | `RoleSystem` | **不参与** | — | 初始化时由 InstructionProcessor 注入，与事件流隔离 |
| TmuxMonitor 注入 | `RoleSystem` | **参与** | `external_input` | 通过 `TagentAgent.InjectMessage` 进入 EventBus，分类为 `external_input` |

---

## 五、ExtractEventType — 类型推断

### 5.1 函数签名

```go
// event/types.go:52-70
func ExtractEventType(msg model.Message) string
```

### 5.2 推断规则

```go
func ExtractEventType(msg model.Message) string {
    switch msg.Role {
    case model.RoleUser:
        return TypeExternalInput
    case model.RoleAssistant:
        if len(msg.ToolCalls) > 0 {
            return TypeThinkingPlan
        }
        return TypeAgentOutput
    case model.RoleTool:
        return TypeActionCommand
    case model.RoleSystem:
        // 仅来自 TmuxMonitor 注入（进入事件流）
        return TypeExternalInput
    default:
        return TypeExternalInput
    }
}
```

### 5.3 推断规则表

| msg.Role | ToolCalls | EventType | 说明 |
|----------|-----------|-----------|------|
| `RoleUser` | — | `external_input` | 用户输入 |
| `RoleSystem` | — | `external_input` | TmuxMonitor 注入 |
| `RoleAssistant` | `len > 0` | `thinking_plan` | Agent 思考/计划（带工具调用） |
| `RoleAssistant` | `len == 0` | `agent_output` | Agent 最终回复 |
| `RoleTool` | — | `action_command` | 工具执行结果 |
| 其他 | — | `external_input` | Fallback |

---

## 六、IsSpecialEventType — 特殊事件判断

### 6.1 函数签名

```go
// event/types.go:75-82
func IsSpecialEventType(eventType string) bool
```

### 6.2 特殊事件 vs 普通事件

| 事件类型 | IsSpecialEventType | 摘要策略 | 原因 |
|---------|-------------------|---------|------|
| `external_input` | **true** | 原文全文 | 用户意图需完整保留 |
| `agent_output` | **true** | 原文全文 | Agent 回复需完整保留 |
| `thinking_plan` | **true** | 原文全文 | Agent 思考过程含工具调用决策 |
| `action_command` | false | 工具调用摘要 | 工具调用信息密度高 |
| `thinking_recall` | false | 原文全文 | 记忆召回内容需完整 |
| `thinking_knowledge` | false | 原文全文 | 知识检索内容需完整 |
| `context_compress` | false | 原文全文 | 压缩通知内容需完整 |
| 其他 | false | 原文全文 | Fallback |

**核心原则**：`thinking_plan` 和 `external_input`/`agent_output` 一样使用原文全文摘要策略，`action_command` 使用工具调用摘要。

---

## 七、GenerateEventSummary — 摘要生成

### 7.1 函数签名

```go
// event/types.go:113-126
func GenerateEventSummary(msg model.Message, eventType string, opts EventSummaryOptions) string
```

### 7.2 摘要策略

```go
func GenerateEventSummary(msg model.Message, eventType string, opts EventSummaryOptions) string {
    // 特殊事件：摘要 = 原文全文（无截断）
    if IsSpecialEventType(eventType) {
        return msg.Content
    }

    // 普通事件：action_command 使用工具调用摘要
    switch eventType {
    case TypeActionCommand:
        return formatToolCallSummary(msg, opts)
    default:
        return msg.Content
    }
}
```

### 7.3 formatToolCallSummary — 工具调用摘要

```go
// event/types.go:155-171
func formatToolCallSummary(msg model.Message, opts EventSummaryOptions) string {
    if len(msg.ToolCalls) == 0 {
        if msg.Role == model.RoleTool {
            return msg.Content  // Tool 结果用原文
        }
        return "命令执行"
    }

    toolName := msg.ToolCalls[0].Function.Name
    args := string(msg.ToolCalls[0].Function.Arguments)

    if opts.StructuredFormat {
        return fmt.Sprintf("调用工具: %s\n  参数: %s", toolName, args)
    }
    return fmt.Sprintf("调用工具: %s(%s)", toolName, args)
}
```

**输出示例**：

```
# StructuredFormat = false（单行，节省 token）
调用工具: echo(hello world)

# StructuredFormat = true（多行，清晰）
调用工具: echo
  参数: hello world
```

### 7.4 EventSummaryOptions — 配置项

```go
// event/types.go:88-91
type EventSummaryOptions struct {
    StructuredFormat bool  // true: 多行格式; false: 单行格式（节省 token）
}
```

**预设配置**：

| 工厂函数 | StructuredFormat | 适用场景 | 调用方 |
|---------|-----------------|---------|-------|
| `DefaultOptionsForLLMContext()` | false | LLM 消息上下文（高频调用） | SummaryPlugin |
| `DefaultOptionsForCompression()` | true | SmartCompress 压缩（低频调用） | agent/SmartCompressor |

---

## 八、严格拒绝非设计折损

### 8.1 截断已被完全移除

以下内容已在重构中删除（`types.go`）：

```
已删除的代码：
  - DefaultMaxContentLength = 500      （截断阈值）
  - DefaultMaxArgsLength = 200           （参数截断阈值）
  - MaxContentLength int                 （EventSummaryOptions 字段）
  - MaxArgsLength int                   （EventSummaryOptions 字段）
  - formatContent() 函数                 （截断实现）
```

### 8.2 设计折损 vs 非设计折损

| 类别 | 示例 | 是否允许 |
|------|------|---------|
| **设计内折损** | EventSummary 生成时从完整内容到摘要 | 允许（压缩质量由 SmartCompress 保证） |
| **设计内折损** | SmartCompress Stage 2 LLM 生成摘要 | 允许（两阶段机制保真） |
| **设计外折损** | 截断超出限制的文本 | **禁止**（双重折损，破坏压缩质量） |
| **设计外折损** | MaxArgsLength 截断工具参数 | **禁止**（参数信息丢失） |

### 8.3 溢出处理路径

```
内容超限
    ↓
ContextManager BeforeModel 检测到 Token 超阈值
    ↓
SmartCompress.Compress 触发压缩
    ↓
Stage 1: 按 task boundary 切分，丢弃旧 segment
Stage 2: LLM 生成摘要（"对话历史摘要: ...")
    ↓
压缩后的消息列表重新发给 LLM
    ↓
Token 消耗降低 ✅
```

---

## 九、辅助函数

### 9.1 FormatEventDescription

为 SmartCompress 生成结构化的事件描述（完整信息，不截断）：

```go
// event/types.go:128-147
func FormatEventDescription(index int, msg model.Message) string

// 输出示例：
// [0] user: 你好
//   → ToolCalls:
//     - echo(hello)
// [1] assistant: 好的
```

### 9.2 EstimateTokens

简单的 Token 估算（启发式，约每 3 个字符 1 个 token）：

```go
// event/types.go:149-153
func EstimateTokens(text string) int {
    return len([]rune(text)) / 3
}
```

**注意**：这是估算，不是精确计算（中英文混合场景约每 2.5~4 个字符 1 个 token）。`agent/context_manager.go` 中的 `DefaultTokenCounter` 使用 `CharsPerToken = 2.0`，是更保守的估算。

---

## 十、与其他模块的关系

### 10.1 依赖关系

```
tagent/event（基础工具层）
    ↑
    │  提供类型推断和摘要生成
    │
tagent/plugin
    ├── MemoryPlugin.OnEvent → ExtractEventType + GenerateEventSummary
    └── SummaryPlugin.OnEvent → ExtractEventType + GenerateEventSummary

tagent/agent
    └── SmartCompress → FormatEventDescription + EstimateTokens
```

### 10.2 数据流

```mermaid
sequenceDiagram
    participant MP as MemoryPlugin.OnEvent
    participant SP as SummaryPlugin.OnEvent
    participant ET as ExtractEventType
    participant IS as IsSpecialEventType
    participant GS as GenerateEventSummary

    MP->>ET: model.Message
    ET-->>MP: eventType
    MP->>GS: msg, eventType, opts
    GS->>IS: eventType
    IS-->>GS: isSpecial
    alt isSpecial == true
        GS-->>MP: msg.Content（原文全文）
    else isSpecial == false
        GS-->>MP: formatToolCallSummary(...)（工具调用摘要）
    end

    SP->>ET: model.Message
    ET-->>SP: eventType
    SP->>GS: msg, eventType, opts
    GS-->>SP: summary
    Note over SP: Tag = eventType + ":" + summary
```

### 10.3 信息隔离

`tagent/event` 是纯工具层，**不持有任何状态**。所有函数都是纯函数（给定相同输入，总是产生相同输出），可并发安全调用。

---

## 十一、EventBus 与 AgentEvent（事件驱动架构）

在事件驱动架构中，`tagent/agent` 包新增了 `EventBus` 和 `AgentEvent` 类型（定义在 `agent/event_bus.go`），与 `tagent/event` 包的事件类型常量配合使用。

### 11.1 AgentEvent 结构

```go
type AgentEvent struct {
    ID        string           // UUID 唯一标识
    Type      string           // "external_input" | "tool_use"
    Source    string           // "user" | "tmux" | "meditation" | "subagent" | "agent_loop" | "inject"
    Timestamp time.Time
    Message   *model.Message   // external_input 载荷
    ToolCall  *model.ToolCall  // tool_use 载荷
    Metadata  map[string]any   // 扩展数据
}
```

### 11.2 事件流全貌

```
Producers                        EventBus                     Consumer
───────────                     ────────                     ────────
InjectMessage ──┐
TmuxMonitor ────┤
MeditationMgr ──┼──→ Publish ──→ [chan *AgentEvent] ──→ Pull ──→ runEventLoop
SubAgent result ┤    (cap=256)   (有序队列)              (batch drain)
Tool result ────┤
```

`runEventLoop` 是 EventBus 的**唯一消费者**。它批量拉取事件，调用 `ContextManager.BuildInvocation` 合并，再调用 `ContextManager.RunFlow` 执行框架 Flow。

### 11.3 事件类型与 Bus 触发器

| Bus 事件类型 | 进入 Bus 的生产者 | 在 runEventLoop 中的处理 |
|---------|------------|---------|
| `external_input` | InjectMessage、TmuxMonitor、MeditationManager、A2A Server、HTTPAPI | `BuildInvocation` 合并为一条 user message，触发 `RunFlow` |
| `agent_output`（echo） | `RunFlow` 在最终响应时 echo 回 bus | `BuildInvocation` 识别 `Source == agent_output` 并跳过，避免自触发 |
| `tool_use` | 当前实现中**不实际产生**到 Bus | `BuildInvocation` 只处理 `TypeExternalInput`，tool_use 会被忽略 |

> **注意**：`TypeToolUse = "tool_use"` 常量定义在 `agent/event_bus.go` 而非 `tagent/event` 包中，因为它是 Bus 内部的潜在触发器类型，不属于持久化事件类型体系。当前 `runEventLoop` 不消费 `tool_use` 事件——工具执行由框架 Runner 在 `RunFlow` 内部完成。

### 11.4 onEvent 回调与 Session 投影维护

框架 Runner 在 `RunFlow` 执行期间产生事件流，通过 `onEvent` 回调追加到 `SessionProjection`：

```
RunFlow:
  eventCh := runner.Run(...)
  for fwEvt := range eventCh:
      onEvent(fwEvt)      → projection.Append(EventReference)
      outputCh <- fwEvt   → 对外输出
```

`TagentAgent.makeOnEventCallback` 仅做一件事：从 `event.Event.StateDelta` 读取 `MemoryPlugin` 写入的 `event_key`、`partition_id`、`event_type`，构建 `EventReference` 并追加到 `SessionProjection`。

框架 Runner 已完成 `sessionService.AppendEvent` 和 `MemoryPlugin.OnEvent`。tagent 的 `onEvent` 不重复持久化。

### 11.5 三层数据表示与流转

```
层1: EventBus AgentEvent (事件流, 临时)
    │
    ├── runEventLoop.Pull → BuildInvocation → RunFlow
    │
    └── RunFlow 内部：runner.Run 产生 event.Event 流
            │
            ├── onEvent → 追加 EventReference 到 SessionProjection ──→ 层2: SessionProjection (投影, 有界)
            │
            └── MemoryPlugin.OnEvent → StoreEvent ───────────────────→ 层3: MemoryStore FullEvent (永久存储, 不可变)

层2 (SessionProjection) → BuildMessages → 按需从 MemoryStore 拉取完整 Content
                       → InjectEventKeys → [evt_KEY|type] 前缀注入
                       → 发给 LLM 的 []model.Message

层3 (MemoryStore) → recall/memory_query 工具查询 → 跨 Session 检索
                  → RelationStore → 因果链回溯
```

**关键约束**：
- `SessionProjection` 只保存轻量 `EventReference`（key + type + summary），不保存完整内容
- 完整事件内容由 `MemoryPlugin.OnEvent` 持久化到 `MemoryStore`
- `SmartCompressor` 只修改发往 LLM 的 `[]model.Message`，不修改 `SessionProjection`
- `Compactor` 只清理 `SessionProjection` 中的旧引用，不删除 `MemoryStore`
- `MemoryStore` 是唯一完整事件链，Agent 和 Tool 通过 `EventKey` 按需访问

### 11.6 与 tagent/event 的关系

- `tagent/event` 包提供**事件类型常量**和**摘要生成**工具（纯函数，无状态）
- `agent/event_bus.go` 提供**事件传输机制**（EventBus + AgentEvent 结构体）
- `agent/context_manager.go` 使用 `tagent/event` 的类型常量进行事件过滤（`ShouldCallModel`、`BuildInvocation`）
- `agent/event_bus.go` 使用 `NewExternalInputEvent` / `NewToolUseEvent` 构造事件
