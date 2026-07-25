# tagent/tool 模块架构文档

## 一、模块定位

`tagent/tool` 是 tagent 为 trpc-agent-go Runner 提供的一组 **CallableTool 工具实现**，也是 Agent 与外部世界交互的主要通道。

**核心职责**：
- **KnowledgeAgent**：知识获取与翻译 — 发现/理解/翻译能力（Skill/MCP）为可执行计划，实现为 config-driven TagentAgent + AgentToolWrapper 包装
- **RecallAgent**：智能记忆召回 — 使用内部 LLM React 循环理解查询意图，综合历史事件为连贯回答
- **ActionTool**：命令执行（同步 exec / 异步 tmux_exec），纯执行器，不关心命令来源
- **TmuxMonitor**：后台监控 tmux session 状态，状态变更时通过 `InjectMessage` 触发新的 Agent 迭代
- **File Tools**：封装 trpc-agent-go 内置文件操作工具（read_file、save_file 等）

**设计原则**：
- **职责分离**：理解层（KnowledgeAgent, RecallAgent）和执行层（ActionTool）分离，Agent 负责决策
- **架构统一**：KnowledgeAgent 和 RecallAgent 都是 config-driven TagentAgent 实例 + AgentToolWrapper 包装，复用框架能力
- **按需 React**：KnowledgeAgent 和 RecallAgent 有内部 React 循环；ActionTool 不需要
- **Prompt 文件化**：System prompt 通过 `prompt.Loader` 动态加载
- **配置声明式**：所有 tool 通过 Config + ToolRef 声明，`kind` 区分 agent/tool
- **事件上下文传递**：tool agent 通过父 agent 的 MemStore + `event_keys` 获取完整事件上下文
- **异步任务层（当前）**：`action`（tmux）等长耗时工具经调用上下文注入的 `TaskSpawner` 接入**异步任务层**——settle-or-detach + `task_settled` 回收 turn，ActionTool 本身无状态。详见 `agent-architecture.md` §2.10。（旧的 `MessageInjector` 闭环已废弃，见 §8.6）
- **统一注册路径**：所有内置工具通过 `RegisterBuiltinTools()` 统一注册为 plain tool

---

## 二、文件清单

### 2.1 包结构

```
# 根包 (tagent)
├── tagent.go           # New() 工厂函数：声明式 Config + Option 创建 TagentAgent
├── config.go           # Config / ToolConfig / PromptConfig 声明式配置 + LoadConfig
├── registry.go         # ToolRegistry：统一工具注册/查询/校验（RegisterBuiltinTools）
├── builtin.go          # 内置 plain tool 工厂函数（actionFactory）

# agent 包
├── tagent_agent.go     # TagentAgent 组合根 + runEventLoop
├── tool_agent.go       # ToolAgentFactory / PlainToolFactory 注册接口 + AgentToolWrapper
├── context_manager.go  # ContextManager + TokenCounter + BeforeModel 回调链
├── smart_compress.go   # SmartCompressor
├── task_segmenter.go   # TaskSegmenter + Compactor
├── event_bus.go        # EventBus + AgentEvent
├── projection.go       # SessionProjection（投影存储，写入经 ProjectionSink 由插件管线驱动）
├── meditation.go       # MeditationManager
├── trajectory_recorder.go # LLM 调用轨迹记录
├── http_api.go         # HTTP API（RL/AReaL 集成）
└── output_limit_tool.go # 工具输出截断

# tool 包
tool/
├── accessor.go          # 抽象接口定义
├── action/              # action 子包
│   ├── action_tool.go     # ActionTool 实现 (exec / tmux_exec 双模式)
│   ├── action_executor.go # 命令执行器
│   ├── tmux_executor.go   # Tmux 执行器
│   ├── tmux_monitor.go    # Tmux 监控器
│   └── action_test.go     # ActionTool 测试
├── recall/              # recall 子包
│   ├── recall_agent.go    # RecallAgent 组装
│   └── recall_subtools.go # 子工具实现 + RegisterSubTools()
├── knowledge/           # knowledge 子包
│   ├── knowledge_agent.go   # KnowledgeAgent 组装
│   ├── knowledge_subtools.go# 子工具实现 + RegisterSubTools()
│   └── websearch.go         # Web 搜索工具
├── file/                # file 子包
│   └── file.go              # 封装 trpc-agent-go 内置文件操作工具
├── speak/               # speak 子包 (stub)
│   └── speak_agent.go
└── draw/                # draw 子包 (stub)
    └── draw_agent.go

# prompt 包
prompt/
├── loader.go            # Loader + CompositeConfig
```

### 2.2 详细文件列表

| 文件 | 行数 | 职责 |
|------|------|------|
| `tagent.go` (根) | 677 | New() 工厂函数：声明式 Config + Option，按 ToolRef 列表创建 tool |
| `config.go` (根) | 546 | Config / AgentConfig / ToolRef / PromptConfig + LoadConfig + DefaultConfig |
| `registry.go` (根) | 99 | ToolRegistry：统一工具注册/查询/校验门面 + RegisterBuiltinTools |
| `builtin.go` (根) | 45 | 内置 plain tool 工厂函数：actionFactory（ActionTool） |
| `agent/tool_agent.go` | 459 | ToolAgentFactory / PlainToolFactory 注册接口 + AgentToolWrapper 实现 |
| `accessor.go` | 33 | 抽象接口定义（MemoryStoreAccessor, SkillRepository） |
| `recall/recall_agent.go` | ~190 | RecallAgent 组装 |
| `recall/recall_subtools.go` | 421 | RecallAgent 子工具 + RegisterSubTools |
| `knowledge/knowledge_agent.go` | ~145 | KnowledgeAgent 组装 |
| `knowledge/knowledge_subtools.go` | 423 | KnowledgeAgent 子工具 + RegisterSubTools |
| `knowledge/websearch.go` | ~560 | Web 搜索工具实现 |
| `action/action_tool.go` | 377 | ActionTool：exec / tmux_exec 双模式 |
| `action/action_executor.go` | ~250 | 命令执行器 |
| `action/tmux_monitor.go` | ~440 | Tmux 监控器 |
| `action/tmux_executor.go` | ~383 | Tmux 执行器 |
| `file/file.go` | 116 | 封装 trpc-agent-go 内置 8 个文件操作工具 |
| `prompt/loader.go` | 289 | Loader + CompositeConfig |

---

## 三、组件关系总览图

```mermaid
graph TB
    subgraph "tagent (root)"
        KA["tagent.go\nbuildAgent() + RegisterBuiltinTools()"]
    end

    subgraph "tagent/agent"
        TA["TagentAgent\nInjectMessage()\nStartLoop()/StopLoop()\nrunEventLoop"]
    end

    subgraph "tagent/tool"
        subgraph "recall/"
            RA["RecallAgent\nconfig-driven + AgentToolWrapper\nrecall_query/get/recent/trace"]
        end
        subgraph "knowledge/"
            KT["KnowledgeAgent\nconfig-driven + AgentToolWrapper\nskill_search/load, mcp_discover"]
        end
        subgraph "action/"
            CT["ActionTool\nCallableTool"]
            CE["ActionExecutor"]
            TE["TmuxExecutor"]
            TM["TmuxMonitor"]
        end
        subgraph "file/"
            FT["File Tools\nread_file/save_file/..."]
        end
        AC["accessor.go\n抽象接口"]
    end

    subgraph "tagent/memory"
        MS["MemoryStore"]
    end

    subgraph "Agent 决策层"
        LLMA["LLMAgent\n(React Loop)"]
    end

    LLMA --> RA
    LLMA --> KT
    LLMA --> CT
    LLMA --> FT

    RA --> MS
    KT -->|Skill/MCP/Web| SRC["知识源"]
    KT -->|ExecutionPlan| CT
    CT --> CE
    CT -->|tmux_exec| TE
    TM -->|检查状态| TE
    TM -->|状态变化回调| CT
    CT -->|InjectMessage\n→ EventBus| TA
    TA -->|runEventLoop| LLMA
    KA -->|创建| TA
    KA -->|assembles| RA
    KA -->|assembles| KT
```

---

## 四、事件上下文传递机制（EventKeys 注入 + 数据隔离）

### 4.0 核心设计

顶层 Agent 直接送 LLM 的 context 是一条**事件组成的记录流**。tool 与顶层 Agent 交互时，需要依赖顶层 Agent 传入的关键 `event_keys` 从 MemStore 中获取完整上下文。

**问题**：tool agent 被调用时，LLM 只能传递文本参数（如 `request`），但 tool agent 需要访问触发其调用的完整事件上下文（因果链、完整事件详情），这需要 `event_keys`。

**设计决策**：tool agent 的 Declaration InputSchema 中声明 `event_keys` 参数（数组），由 `AgentToolWrapper` 从调用参数中解析，通过父级 MemoryStore 获取上下文。

### 4.1 EventKey Snowflake 设计

EventKey 为 Snowflake int64（详见 [memory-architecture.md](../memory/memory-architecture.md) §4.1）。

```
┌──────────────────────────────────────────────────────────────────┐
│ 63       53 │ 52            22 │ 21       12 │ 11             0 │
│  PartitionID│   Timestamp      │  Sequence   │   Reserved     │
│  (11 bits)  │   (31 bits)      │  (10 bits)  │   (12 bits)    │
└──────────────────────────────────────────────────────────────────┘
```

| 字段 | 位数 | 说明 |
|------|------|------|
| PartitionID | 11 bits | 存储分区键（0-2047），由 FNV-1a(AgentName) 派生 |
| Timestamp | 31 bits | 秒级时间戳偏移（相对 epoch），可用 ~68 年 |
| Sequence | 10 bits | 同秒内序列号（0-1023），单分区每秒可产生 1024 个事件 |
| Reserved | 12 bits | 预留位 |

### 4.2 Memory 数据隔离设计

**核心原则：Memory 不感知 agent，但从存储角度实现数据隔离。**

- FilterKey 是 trpc-agent-go 框架的概念，属于 LLM context 层面的隔离
- Memory 从**存储分区**角度思考隔离，使用 **PartitionID** 作为分区键
- 框架已有的 **AgentName** 是稳定的 agent 身份标识
- **PartitionID = FNV-1a(AgentName) & 0x7FF**，由 MemoryPlugin 在 tagent 层计算

```
框架层 (AgentName/FilterKey)     tagent 层 (MemoryPlugin)          Memory 层 (PartitionID)
┌──────────────────────┐      ┌───────────────────────┐      ┌─────────────────┐
│ AgentName = "tagent" │──────→│ FNV-1a("tagent")=42  │──────→│ partition=42    │
│ FilterKey = "tagent" │      │ FNV-1a("knowledge")=85│──────→│ partition=85    │
├──────────────────────┤      │ FNV-1a("recall")=123 │──────→│ partition=123   │
│ AgentName = "know"   │      │                       │      │                 │
└──────────────────────┘      │ AgentName → PartitionID│      │ 纯整数分区键    │
                              │ Memory 不感知 agent   │      │ 无 agent 语义   │
└──────────────────────┘      └───────────────────────┘      └─────────────────┘
```

### 4.3 EventKeys 注入流程

```mermaid
sequenceDiagram
    participant LLM as LLM Model
    participant Flow as Flow (框架)
    participant MP as MemoryPlugin
    participant Tool as Tool Agent
    participant MS as MemStore

    LLM->>Flow: tool_calls: knowledge({request: "..."})
    Flow->>MP: OnEvent(assistant message + tool_calls)
    MP->>MP: 生成 EventKey (Snowflake: PartitionID=42, ts, seq)
    MP->>MP: 写入 StateDelta["event_key"]
    MP->>MS: StoreEvent(key, FullEvent{PartitionID: 42})
    MP-->>Flow: 返回带 StateDelta 的事件

    Note over Flow: AgentToolWrapper 拦截调用<br/>从 InputSchema 解析 event_keys<br/>通过 parentStore.GetEvent 获取上下文

    Flow->>Tool: Call(ctx, {"request": "...", "event_keys": [K1]})
    Tool->>MS: GetEvent(K1)
    MS-->>Tool: FullEvent
    Tool->>MS: QueryEvents({PartitionID: 42, ...})
    MS-->>Tool: 顶层 agent 的事件流
    Tool-->>Flow: Tool Result
```

### 4.4 EventKeys 注入机制

**注入位置**：MemoryPlugin.OnEvent 处理 assistant 的 tool_call 消息时生成 Snowflake EventKey 并写入 `StateDelta`。

**调用流程**：
1. LLM 输出 tool_calls，选择相关 `event_keys`
2. `AgentToolWrapper.Call` 从参数中解析 `event_keys`（数组）和兼容单数的 `event_key`
3. 通过 `parentStore.GetEvent(key)` 逐个取出完整 `FullEvent`
4. 序列化为 `RuntimeState["external_context"]`
5. 调用 `agent.Run(ctx, inv)`，子 Agent 从 RuntimeState 读取上下文

**Tool Declaration 约束**：
- Agent-kind tool 的声明自动包含 `event_keys` 参数（当 `ToolRef.EventParams` 包含 `event_keys` 时）
- 纯执行器 tool（如 ActionTool、File Tools）不声明此参数

```go
// AgentToolWrapper.Declaration 中 event_keys 的声明
"event_keys": {
    Type:        "array",
    Description: "[LLM-selected] Array of Snowflake EventKeys for related events ...",
    Items: &tool.Schema{Type: "integer"},
}
```

### 4.5 Tool Agent 使用 EventKeys 获取上下文

Tool agent 收到 `event_keys` 后，可通过 MemoryStore 执行以下操作：

| 操作 | 方法 | 用途 |
|------|------|------|
| 获取触发事件详情 | `GetEvent(eventKey)` | 获取完整的 tool_call 事件内容 |
| 提取分区归属 | `PartitionIDFromEventKey(eventKey)` | 从 EventKey 反推 PartitionID |
| 按分区查询 | `QueryEvents({PartitionID: id})` | 查询同分区的事件流 |
| 追溯因果链 | `RelationStore.GetParent(event.EventKey)` | 获取前驱事件 |
| 跨分区查询 | `QueryEvents({PartitionIDs: [42, 85]})` | 查询顶层+子 agent 事件（PartitionIDs 由 ReadNamespaces 注入） |

### 4.6 远程上下文传递路径

当子 agent 部署为远程 tagent 服务时（`ToolRef.Remote` 配置），上下文传递链路通过 trpc 框架原生的 RuntimeState 机制自动完成：

```
AgentToolWrapper.Call
  → 解析 event_keys → parentStore.GetEvent → FullEvents
  → serializeExternalContext → ExternalContextEntry[] JSON (仅 EventKey/EventType/EventSummary)
  → Invocation.RunOptions.RuntimeState["external_context"] = JSON
  → agent.Run(ctx, inv)
      │
      ├── 本地: TagentAgent.Run 直接读取 RuntimeState
      └── 远程: A2AAgent.Run
            → WithTransferStateKey("external_context")
            → RuntimeState → A2A message.Metadata
            → HTTP 传输
            → A2A Server
            → agent.WithRuntimeState(message.Metadata)
            → RuntimeState → TagentAgent.Run
```

**序列化格式选择**：仅 EventKey + EventType + EventSummary，不含 Content。原因：
1. `injectExternalContext` 只用 EventSummary
2. A2A metadata 有大小限制
3. 远程子 agent 如需完整事件，可通过自身 MemoryStore 查询

---

## 五、工具的 trpc-agent-go 集成

### 5.1 CallableTool 接口

所有 tagent 工具都实现了 `trpc-agent-go/tool.CallableTool` 接口：

```go
// action/action_tool.go
var _ tool.CallableTool = (*ActionTool)(nil)
```

KnowledgeAgent 和 RecallAgent 不是直接的 CallableTool，而是通过 `AgentToolWrapper` 包装：

```go
// agent/tool_agent.go
wrapper := agent.NewAgentToolWrapper(subAgent, desc, tr.EventParams, parentMemStore)
// wrapper 实现 CallableTool 接口
```

接口定义（`trpc-agent-go/tool`）：

```go
type CallableTool interface {
    Declaration() *Declaration   // 返回工具声明
    Call(ctx context.Context, jsonArgs []byte) (any, error)  // 执行工具
}
```

### 5.2 工具注册机制（三阶段生命周期）

tagent 采用**三阶段工具生命周期**：实现层指定 → 注册层注册 → 配置层组织。

**阶段一：实现层**（各子包内）

每个工具包导出工厂函数，实现 `PlainToolFactory` 接口：

```go
// tool/knowledge/knowledge_subtools.go
func skillSearchFactory(cfg agent.PlainToolFactoryConfig) (tool.CallableTool, error) {
    return NewSkillSearchTool(cfg.SkillRepo), nil
}

// tool/recall/recall_subtools.go
func recallQueryFactory(cfg agent.PlainToolFactoryConfig) (tool.CallableTool, error) {
    return NewRecallQueryTool(accessor, cfg.ReadPartitionIDs), nil
}
```

**阶段二：注册层**（registry.go + builtin.go）

`RegisterBuiltinTools()` 统一注册所有内置 plain tool：

```go
// registry.go
func RegisterBuiltinTools() error {
    registerOnce.Do(func() {
        agent.RegisterPlainTool("exec", actionFactory)
        file.RegisterTools()
        knowledge.RegisterSubTools()
        recall.RegisterSubTools()
    })
    return nil
}
```

注册后 ToolRegistry 中可查询的 plain tool：

| Tool ID | 工厂位置 | 说明 |
|---------|---------|------|
| `exec` | builtin.go | ActionTool（shell/tmux 执行） |
| `read_file` | file/file.go | 读取文件 |
| `save_file` | file/file.go | 保存文件 |
| `list_file` | file/file.go | 列出目录 |
| `search_file` | file/file.go | 搜索文件 |
| `search_content` | file/file.go | 搜索内容 |
| `read_multiple_files` | file/file.go | 批量读取 |
| `replace_content` | file/file.go | 替换内容 |
| `skill_search` | knowledge/knowledge_subtools.go | 搜索技能库 |
| `skill_load` | knowledge/knowledge_subtools.go | 加载技能内容 |
| `mcp_discover` | knowledge/knowledge_subtools.go | 发现 MCP 工具 |
| `web_search` | knowledge/knowledge_subtools.go | 搜索通用网页 |
| `duckduckgo_search` | knowledge/knowledge_subtools.go | DuckDuckGo 事实搜索 |
| `memory_query` | knowledge/knowledge_subtools.go | 查询历史知识记录 |
| `recall_query` | recall/recall_subtools.go | 按条件检索事件 |
| `recall_get` | recall/recall_subtools.go | 获取完整事件详情 |
| `recall_recent` | recall/recall_subtools.go | 快速获取最近事件 |
| `recall_trace` | recall/recall_subtools.go | 因果链回溯 |

**阶段三：配置层**（YAML AgentConfig.Tools）

每个 agent 通过 `Tools []ToolRef` 声明使用哪些工具：

```yaml
agents:
  tagent:
    tools:
      - agent: knowledge
        description_file: knowledge_tool_desc.md
        event_params: [event_keys]
      - agent: recall
        description_file: recall_tool_desc.md
        event_params: [event_keys]
      - kind: tool
        id: exec
        description_file: action_tool_desc.md
      - kind: tool
        id: read_file
        description_file: read_file_tool_desc.md
        properties:
          base_dir: "./workspace"
  knowledge:
    tools:
      - kind: tool
        id: skill_search
      - kind: tool
        id: skill_load
      # ... 共 6 个 plain tools
  recall:
    tools:
      - kind: tool
        id: recall_query
      # ... 共 4 个 plain tools
```

**构建路径**（`tagent.go:buildToolFromRef`）：

```
ToolRef (kind=agent) → buildAgentToolRef → buildAgent() 递归创建子 Agent → AgentToolWrapper 包装
ToolRef (kind=tool)  → buildPlainToolRef → ToolRegistry.GetPlainToolFactory(id) → factory(PlainToolFactoryConfig) → CallableTool
```

`PlainToolFactoryConfig` 携带运行时依赖（MemStore、SkillRepo、MCPToolSets、ReadPartitionIDs、Properties），由 `buildPlainToolRef` 从当前 agent 的上下文注入。

---

## 六、召回体系：memory_recall（协议入口）+ RecallAgent（多跳编排）

### 6.0 memory_recall — 召回标准协议（纯函数，主 agent 直持）

**文件**：`tool/recall/memory_recall.go`。索引卡即召回票据，按输入形态分流：

| 输入形态 | 路径 | 特性 |
|---|---|---|
| `items=[{key,hint?}]` | 批量 `GetEvent` 精确回补（原序） | 零幻觉；未命中显式 `miss`；hint 回显对账；items 优先 |
| `query`(+since/until/event_types) | `QueryOptions` 关键词检索 | 检索层可独立演进（→向量），入口协议不变 |

确定性路径上无 LLM 中间层；滚动压缩摘要卡片行里的 `[hex]` key 可直接抠出构造 items。

### 6.1 RecallAgent — 复杂检索与多跳编排（定位收窄）

RecallAgent 使用内部 LLM React 循环理解查询意图，综合历史事件为连贯回答——适用于多跳因果追溯（trace）、跨轮收窄等复杂场景；简单精确/关键词召回请走 memory_recall。

**设计决策**：RecallAgent 使用 config-driven TagentAgent + AgentToolWrapper 包装架构（与 KnowledgeAgent 统一），而非简单的 CallableTool。理由：需要 LLM 理解查询意图、综合多个子工具结果、提供结构化的记忆摘要。

### 6.2 配置结构

```go
// recall/recall_agent.go
type Config struct {
    Model             model.Model
    MemStore          tagentpkg.MemoryStoreAccessor
    ReadPartitionIDs  []int
    PromptDir         string
    Prompt            PromptConfig
    Description       string
    DescriptionFile   string
    MaxToolIterations int   // 默认：5
    MaxTokens         int   // 默认：4096
}
```

### 6.3 子工具注册

子工具通过 `RegisterSubTools()` 统一注册为 plain tool：

| 子工具 | 工厂函数 | 说明 |
|--------|---------|------|
| `recall_query` | `recallQueryFactory(cfg)` | 按查询条件检索事件列表，支持时间范围过滤，自动注入 `ReadPartitionIDs` |
| `recall_get` | `recallGetFactory(cfg)` | 根据 event_key 获取完整事件详情，支持 `include_parent` |
| `recall_recent` | `recallRecentFactory(cfg)` | 快速获取最近的 N 条事件，自动注入 `ReadPartitionIDs` |
| `recall_trace` | `recallTraceFactory(cfg)` | 沿 RelationStore 因果链回溯，最多 20 步 |

### 6.4 构建路径

RecallAgent 与 KnowledgeAgent 一致走 config-driven 路径：

```
tagent.New()
  → buildAgent("recall", recallCfg, ...)
    → 从 recallCfg.Tools 构建 4 个 plain tool
    → 每个 plain tool 从 ToolRegistry.GetPlainToolFactory(id) 创建
    → PlainToolFactoryConfig 携带 MemStore + ReadPartitionIDs
  → buildAgentToolRef() 用 AgentToolWrapper 包装为 CallableTool
```

---

## 七、KnowledgeAgent — 知识获取与翻译

### 7.1 核心职责

KnowledgeAgent 发现和加载外部技能文件（skills 目录中的 .md 等文件），并将能力描述翻译为 ExecutionPlan。

**设计原则**：
- **理解层，非执行层**：KnowledgeAgent 负责"理解"技能，执行由 ActionTool 负责
- **架构统一**：TagentAgent 实例 + AgentToolWrapper 包装

**AgentToolWrapper.Call() 实现要点**：
1. 从 `args` 中解析 `event_keys` 参数（`[]int64`）
2. 通过 `parentStore.GetEvent(key)` 逐个获取完整 `FullEvent`
3. 序列化为 `RuntimeState["external_context"]`，通过 `agent.Run(ctx, inv)` 传递给子 Agent
4. `Response.Clone()` 防御层：确保子 Agent 读取的 Response 与 Session 存储的 Response 不共享指针
5. 提取 `finalOutput`（子 Agent 最后一个 `agent_output` 事件的内容）作为 tool result 返回给顶层 LLM

### 7.2 构建路径（config-driven）

```
tagent.New()
  → buildAgent("knowledge", knowledgeCfg, ...)
    → 从 knowledgeCfg.Tools 列表构建 6 个 plain tool
    → 每个 plain tool 从 ToolRegistry.GetPlainToolFactory(id) 创建
    → 创建 TagentAgent + 6 个 plain tool
  → buildAgentToolRef() 用 AgentToolWrapper 包装为 CallableTool
```

**子工具声明**（`DefaultConfig()` 中 knowledge agent 的 Tools）：

```go
"knowledge": {
    Tools: []ToolRef{
        {Kind: ToolKindTool, ID: "skill_search"},
        {Kind: ToolKindTool, ID: "skill_load"},
        {Kind: ToolKindTool, ID: "mcp_discover"},
        {Kind: ToolKindTool, ID: "web_search"},
        {Kind: ToolKindTool, ID: "duckduckgo_search"},
        {Kind: ToolKindTool, ID: "memory_query"},
    },
}
```

### 7.3 子工具注册

| 子工具 | 工厂函数 | 说明 |
|--------|---------|------|
| `skill_search` | `skillSearchFactory(cfg)` | 搜索本地技能库 |
| `skill_load` | `skillLoadFactory(cfg)` | 加载技能完整内容 |
| `mcp_discover` | `mcpDiscoverFactory(cfg)` | 发现 MCP 工具 |
| `duckduckgo_search` | `duckDuckGoSearchFactory(cfg)` | 搜索事实性知识 |
| `web_search` | `webSearchFactory(cfg)` | 搜索通用网页内容 |
| `memory_query` | `memoryQueryFactory(cfg)` | 查询历史知识记录 |

### 7.4 Prompt 文件化

System prompt 存储在 `resources/prompts/knowledge_agent.md`：
- 通过 `prompt.Loader` 动态加载
- 包含工具使用指南、exec-plan 规范、执行原则
- 支持运行时更新

---

## 八、ActionTool — 命令执行

### 8.1 双模式设计

> ℹ️ **当前架构**：ActionTool 已**无状态重写**并接入异步任务层——`Call` 经 `TaskSpawnerFromContext` 取 spawner，以 `TmuxSettleDetector` spawn 一个 Task（dense 内 settle → 内联；越界 detach → ack + `task_settled` 回收 turn）。本节部分代码块（`injector`/`MessageInjector`/`handleStateChange`，见 §8.6）为**重写前的历史留存**，当前不再持有 injector/waiter。端到端模型见 `agent-architecture.md` §2.10。

ActionTool 支持两种执行模式：

| 模式 | 执行方式 | 返回时机 | 适用场景 |
|------|---------|---------|---------|
| `exec` | 优先 tmux 异步；tmux 不可用时同步回退 | 立即返回 session ID（tmux）或命令结束（sync） | 所有命令 |
| `tmux_exec` | 同 exec（当前实现统一走 tmux 异步） | 立即返回 session ID | 长期交互命令 |

> **当前实现**：所有 ActionTool 调用优先走 tmux 异步路径。若 tmux 不可用，才回退到同步 exec。

### 8.1.1 Properties 配置

`exec`（ActionTool）通过 `ToolRef.Properties` 接收以下配置：

| 字段 | 类型 | 说明 |
|------|------|------|
| `workspace` | string | 命令工作目录；**默认继承进程运行目录**（与 file tools 的相对路径基准一致，避免路径分裂诱发模型幻觉） |
| `run_as_user` | string | 通过 `sudo -u` 执行命令时使用的用户 |
| `run_as_group` | string | 通过 `sudo -g` 执行命令时使用的用户组 |

```yaml
tools:
  - kind: tool
    id: exec
    description_file: action_tool_desc.md
    properties:
      run_as_user: tagent-runner
      run_as_group: tagent-runner
```

> 另：大输出经 `WithActionOutputDir` 落盘 `tool-output/`；启动时 `CleanupOrphanSessions` 按前缀清扫上代孤儿会话（可经 `WithOrphanCleanupDisabled` 关闭，多实例共用 tmux server 时用独立前缀）。

### 8.2 ActionTool 的组合结构

```go
// action/action_tool.go:37-50
type ActionTool struct {
    workspace    string
    runAsUser    string
    runAsGroup   string
    description  string
    executor     *ActionExecutor
    tmuxExecutor *TmuxExecutor
    tmuxMonitor  *TmuxMonitor
    injector     MessageInjector
    closeOnce    sync.Once
}
```

### 8.3 Declaration

```go
// action/action_tool.go:138-166
func (ct *ActionTool) Declaration() *tool.Declaration {
    return &tool.Declaration{
        Name:        "action",
        Description: ct.description,
        InputSchema: &tool.Schema{
            Type: "object",
            Properties: map[string]*tool.Schema{
                "command": {Type: "string", Description: "..."},
                "work_dir": {Type: "string", Description: "..."},
                "env": {Type: "object", AdditionalProperties: true},
                "is_tui": {Type: "boolean", Description: "..."},
            },
            Required: []string{"command"},
        },
    }
}
```

> **注意**：工具在注册表中 ID 为 `exec`，但 LLM 看到的工具名是 `action`。

### 8.4 exec 模式 — 同步执行（tmux 不可用时的回退）

```go
// action/action_tool.go:199-227
func (ct *ActionTool) executeSync(ctx context.Context, args ActionArgs) (any, error) {
    timeout := args.Timeout
    if timeout <= 0 {
        timeout = 60
    }

    spec := ActionSpec{
        Command:    "sh",
        Args:       []string{"-c", args.Command},
        Env:        args.Env,
        Dir:        args.WorkDir,
        Workspace:  ct.workspace,
        Timeout:    time.Duration(timeout) * time.Second,
        RunAsUser:  ct.runAsUser,
        RunAsGroup: ct.runAsGroup,
    }

    result, err := ct.executor.Execute(ctx, spec)
    if err != nil {
        return nil, fmt.Errorf("action: execution failed: %w", err)
    }

    return &ActionExecResult{
        ExitCode: result.ExitCode,
        Stdout:   result.Stdout,
        Stderr:   result.Stderr,
    }, nil
}
```

### 8.5 tmux_exec 模式 — 异步执行

```go
// action/action_tool.go:229-267
func (ct *ActionTool) executeAsync(ctx context.Context, args ActionArgs) (any, error) {
    session, err := ct.tmuxExecutor.CreateSession(ctx, TmuxCreateOptions{
        Command: args.Command,
        WorkDir: args.WorkDir,
        Env:     args.Env,
    })
    if err != nil {
        return nil, fmt.Errorf("action: failed to create tmux session: %w", err)
    }

    if ct.tmuxMonitor != nil {
        ct.tmuxMonitor.AddSession(&TmuxSession{
            ID:        session.ID,
            Name:      session.Name,
            Command:   args.Command,
            WorkDir:   args.WorkDir,
            Status:    SessionRunning,
            CreatedAt: time.Now(),
            IsTUI:     args.IsTUI,
        })
        if !ct.tmuxMonitor.IsRunning() {
            ct.tmuxMonitor.Start()
        }
    }

    return &TmuxExecResponse{
        SessionID: session.ID,
        Status:    "running",
    }, nil
}
```

### 8.6 ActionTool 的 MessageInjector 机制（已废弃）

> ⚠️ **已过时**：此 `MessageInjector`/`handleStateChange` 机制已随 ActionTool 的**无状态重写**移除。当前 ActionTool 不再持有 injector/waiter，而是经调用上下文的 `TaskSpawner`（`TaskSpawnerFromContext`）接入**异步任务层**：状态变更由 `TmuxMonitor` 的按会话回调驱动 `TmuxSettleDetector`，dense 阶段内 settle → 内联返回、越界 → ack 并经 `task_settled` 事件回收 turn。详见 `agent-architecture.md` §2.10 任务层。以下代码块仅为历史留存。

ActionTool 通过 `MessageInjector` 接口闭环处理 tmux 状态变更通知：

```go
// action/action_tool.go:18-23
type MessageInjector interface {
    InjectMessage(msg model.Message)
}
```

```go
// action/action_tool.go:269-
func (ct *ActionTool) handleStateChange(sessionID, oldStatus, newStatus, output string) {
    if ct.injector == nil {
        return
    }
    content := fmt.Sprintf("[system] tmux session %s state changed: %s -> %s", ...)
    if output != "" {
        if len(output) > 2000 {
            output = "...(truncated)" + output[len(output)-2000:]
        }
        content += fmt.Sprintf("\nOutput:\n%s", output)
    }
    ct.injector.InjectMessage(model.Message{Role: model.RoleSystem, Content: content})
}
```

`TagentAgent` 实现了 `MessageInjector` 接口，`buildAgent()` 中通过 `cmdTool.SetMessageInjector(ta)` 完成接线。`InjectMessage` 将消息发布到 active EventBus，由 `runEventLoop` 下一轮消费。

---

## 九、ActionExecutor — 安全命令执行

### 9.1 Execute 流程

```go
// action/action_executor.go
func (ce *ActionExecutor) Execute(ctx context.Context, spec ActionSpec) (ActionResult, error) {
    if timeout > 0 {
        ctx, cancel = context.WithTimeout(ctx, timeout)
        defer cancel()
    }

    cmd := ce.buildCommand(spec)
    cmd.Start()
    doneCh := make(chan error, 1)
    go func() { doneCh <- cmd.Wait() }()

    select {
    case err = <-doneCh:
        // 正常结束
    case <-ctx.Done():
        syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
    }

    return ActionResult{ExitCode, Stdout, Stderr, Duration}, nil
}
```

### 9.2 buildCommand — 用户隔离

```go
// action/action_executor.go
func (ce *ActionExecutor) buildCommand(spec ActionSpec) (*exec.Cmd, error) {
    if spec.RunAsUser != "" {
        args := []string{"-n", "-u", spec.RunAsUser}
        if spec.RunAsGroup != "" {
            args = append(args, "-g", spec.RunAsGroup)
        }
        args = append(args, spec.Command)
        args = append(args, spec.Args...)
        cmd = exec.Command("sudo", args...)
    } else {
        cmd = exec.Command(spec.Command, spec.Args...)
    }

    cmd.Dir = spec.Dir || spec.Workspace || ce.workspace
    cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

    return cmd, nil
}
```

**安全隔离**：通过 `sudo -u` 实现用户隔离，通过 `Setpgid` 实现进程组管理（超时清理）。

---

## 十、TmuxMonitor — 状态监控

### 10.1 监控状态机

```mermaid
stateDiagram-v2
    [*] --> Running
    Running --> Running: 有输出变化
    Running --> Stable: 输出稳定 N 次检测
    Running --> FakeAlive: 进程存在但无响应
    FakeAlive --> Running: 重启成功
    FakeAlive --> FakeDead: 重启失败
    FakeDead --> [*]: 强制清理
    Stable --> Completed: pane 已死或进程退出
    Stable --> [*]: 清理
    Completed --> [*]
    Error --> [*]
```

### 10.2 状态常量

```go
// action/tmux_executor.go
const (
    SessionRunning   SessionStatus = "running"
    SessionStable    SessionStatus = "stable"
    SessionCompleted SessionStatus = "completed"
    SessionError     SessionStatus = "error"
    SessionFakeDead  SessionStatus = "fake_dead"
    SessionFakeAlive SessionStatus = "fake_alive"
)
```

### 10.3 detectSessionState — 状态检测逻辑

```go
// action/tmux_monitor.go
func (tm *TmuxMonitor) detectSessionState(session *TmuxSession) SessionStatus {
    processExists := tm.executor.ProcessExists(session.ID)
    isPaneDead := tm.executor.IsPaneDead(session.ID)
    currentMD5 := md5.Sum([]byte(currentOutput))

    if processExists && !isPaneDead {
        if currentMD5 == session.LastOutputMD5 {
            session.StableCount++
        } else {
            session.StableCount = 0
        }
    }

    if !processExists || isPaneDead {
        return SessionCompleted
    }
    if session.StableCount >= threshold {
        return SessionStable
    }
    return SessionRunning
}
```

### 10.4 FakeAlive / FakeDead 处理

| 状态 | 触发条件 | 处理方式 |
|------|---------|---------|
| `fake_alive` | 进程存在、pane 存活、输出稳定超过阈值，但心跳有响应 | 重启 session |
| `fake_dead` | 进程存在、pane 存活、输出稳定超过阈值，心跳也无响应 | 强制 kill session |

### 10.5 配置参数

```go
// action/tmux_monitor.go
func DefaultMonitorConfig() MonitorConfig {
    return MonitorConfig{
        Interval:             30 * time.Second,
        StableThreshold:      2,
        InteractiveThreshold: 3,
        FakeDeadThreshold:    5,
        HeartbeatCommand:    "echo ping",
        HeartbeatTimeout:     5 * time.Second,
    }
}
```

---

## 十一、TmuxExecutor — Tmux Session 管理

### 11.1 核心操作

| 方法 | 说明 |
|------|------|
| `CreateSession(opts)` | 创建 detached tmux session |
| `KillSession(id)` | 终止 session |
| `SessionExists(id)` | 检查 session 是否存在 |
| `GetSessionOutput(id)` | 捕获 pane 内容 |
| `IsPaneDead(id)` | 检查 pane 是否已死 |
| `ProcessExists(id)` | 检查主进程是否存活 |
| `SendHeartbeat(id)` | 发送心跳检测 |
| `RestartSession(id, opts)` | 重启 session |
| `SendKeys(id, keys)` | 向 session 发送按键 |

### 11.2 Session 唯一命名

```go
// action/tmux_executor.go
func (te *TmuxExecutor) CreateSession(...) (*TmuxSession, error) {
    sessionName := fmt.Sprintf("%s-%d", te.prefix, time.Now().UnixNano())
    // prefix 默认值："tagent"
}
```

---

## 十二、File Tools — 文件操作工具

### 12.1 定位

`tool/file` 封装 trpc-agent-go 内置文件操作工具，作为 plain tool 注册到 ToolRegistry，可直接在 YAML 中引用。

### 12.2 注册的 8 个工具

| Tool ID | 说明 |
|---------|------|
| `read_file` | 读取文件内容 |
| `save_file` | 保存文件 |
| `list_file` | 列出目录内容 |
| `search_file` | 按文件名搜索 |
| `search_content` | 按内容搜索 |
| `read_multiple_files` | 批量读取文件 |
| `replace_content` | 替换文件内容 |

### 12.3 Properties 配置

| 字段 | 类型 | 说明 |
|------|------|------|
| `base_dir` | string | 文件操作的根目录（默认当前工作目录 `.`） |

```yaml
tools:
  - kind: tool
    id: read_file
    description_file: read_file_tool_desc.md
    properties:
      base_dir: "./workspace"
```

### 12.4 实现方式

`file.NewToolSet(baseDir)` 创建 trpc-agent-go 内置 file toolset，`makeFileToolFactory` 根据工具名从 toolset 中取出对应的 `CallableTool`。

---

## 十三、完整数据流

### 13.1 RecallTool 完整数据流

```mermaid
sequenceDiagram
    participant LLM as LLM (父 Agent)
    participant ATW as AgentToolWrapper
    participant RA as RecallAgent (内部 TagentAgent)
    participant RL as Recall LLM (内部 LLM)
    participant MS as MemoryStore

    LLM->>ATW: tool_calls: recall({query: "部署", event_keys: [E1,E3]})
    ATW->>MS: GetEvent(E1), GetEvent(E3)
    MS-->>ATW: FullEvents
    ATW->>RA: Run(invocation with external_context)
    RA->>RL: BeforeModel → 注入 system prompt
    RL->>RL: 理解查询意图

    Note over RL: 内部 React Loop<br/>决定使用 recall_query
    RL->>MS: recall_query({query, limit})
    MS-->>RL: []EventReference

    alt 需要更多细节
        RL->>MS: recall_get(key)
        MS-->>RL: FullEvent
    end

    RL->>RL: 综合检索结果为连贯回答
    RL-->>RA: 最终回答
    RA-->>ATW: event stream
    ATW-->>LLM: Tool Result
```

### 13.2 ActionTool tmux_exec 完整数据流

```mermaid
sequenceDiagram
    participant LLM as LLM
    participant CT as ActionTool
    participant TE as TmuxExecutor
    participant TM as TmuxMonitor
    participant TA as TagentAgent

    LLM->>CT: command({command: "make build"})
    CT->>TE: CreateSession(command="make build")
    TE-->>CT: session{id: "tagent-xxx"}
    CT->>TM: AddSession(session)
    CT->>TM: Start()（后台 goroutine）
    CT-->>LLM: TmuxExecResponse{session_id: "tagent-xxx", status: "running"}

    loop 每 30 秒
        TM->>TM: checkSession()
        alt 输出稳定
            TM->>CT: StateChangeCallback(sid, running→stable, output)
            CT->>TA: InjectMessage(RoleSystem, tmux state change)
            TA->>TA: runEventLoop Pull 新事件
            Note over TA: LLM 读取 tmux 输出
        end
    end
```

---

## 十四、关键设计决策

### 14.1 为什么 RecallAgent 和 KnowledgeAgent 都需要内部 LLM React 循环？

| 工具 | 内部 React | 实现方式 | 理由 |
|------|-----------|---------|------|
| **RecallAgent** | 需要 | config-driven TagentAgent + AgentToolWrapper | 4 种 plain tool 协作，需要 LLM 理解查询意图、综合结果 |
| **KnowledgeAgent** | 需要 | config-driven TagentAgent + AgentToolWrapper | 6 种 plain tool 协作，LLM 翻译能力为 ExecutionPlan |
| **ActionTool** | 不需要 | CallableTool (PlainToolFactory) | 纯执行器，无决策需求 |
| **File Tools** | 不需要 | CallableTool (PlainToolFactory) | 纯执行器 |

判断标准：需要"思考-行动-观察"循环 → TagentAgent + AgentToolWrapper；单一功能/执行器 → 简单 CallableTool。

### 14.2 为什么 KnowledgeAgent 和 RecallAgent 是 config-driven？

| 维度 | 旧架构（ToolAgentFactory） | 新架构（config-driven） |
|------|---------------------------|------------------------|
| knowledge/recall 创建 | 注册 `RegisterToolAgent("knowledge", factory)` | `buildAgent()` 通用路径 |
| 子工具注册 | 在 factory 内部硬编码组装 | `RegisterSubTools()` 注册为 plain tool |
| 子工具配置 | 不可配置 | YAML 声明式 |
| 扩展性 | 需修改 factory 代码 | 只需在 YAML 中添加 tool ref |

### 14.3 为什么 tool 参数必须包含 event_keys？

| 对比项 | 无 event_keys | 有 event_keys |
|--------|--------------|---------------|
| **上下文获取** | 只能依赖 LLM 传的文本 | 可从 MemStore 获取完整事件上下文 |
| **因果链追溯** | 无法追溯 | 通过 RelationStore 追溯事件脉络 |
| **LLM 依赖** | 完全依赖 LLM 传参 | AgentToolWrapper 自动解析 |

**注入时机**：
1. MemoryPlugin.OnEvent 生成 `event_key` 并写入 StateDelta
2. ContextManager.InjectEventKeys 为消息添加 `[evt_<KEY>|<type>]` 前缀
3. LLM 选择相关 `event_keys` 作为 tool 参数传递
4. AgentToolWrapper.Call 解析 `event_keys`，通过 `parentStore.GetEvent` 获取完整事件数据

### 14.4 为什么 TmuxMonitor 用 callback 而不是 channel？

**决策**：callback 让 TagentAgent 完全控制如何触发新迭代（通过 `InjectMessage`）。

| 方案 | 优点 | 缺点 |
|------|------|------|
| **callback（tagent 选型）** | TagentAgent 完全控制触发逻辑 | 调用方需保存引用 |
| channel | 解耦更彻底 | 需要额外的 goroutine 消费 channel |

TagentAgent 需要在 callback 中注入 `RoleSystem` 消息到 EventBus，使用 callback 比 channel 更直接。

### 14.5 为什么用 RuntimeState 而非 struct 字段传递上下文？

**设计决策**：AgentToolWrapper 通过 `Invocation.RunOptions.RuntimeState["external_context"]` 传递外部事件上下文。

| 对比项 | struct 字段 | RuntimeState |
|--------|------------|-------------|
| **本地调用** | 进程内有效 | 直接读取 |
| **远程调用** | 无法跨越 A2A 边界 | 自动映射到 A2A metadata |
| **额外代码** | 需要 ProcessMessageHook | 零额外代码 |

### 14.6 为什么 AgentToolWrapper 持有 agent.Agent 接口而非 *TagentAgent？

**设计决策**：`AgentToolWrapper.agent` 字段类型为 `agent.Agent`（接口）。

**理由**：
1. `TagentAgent` 已实现 `agent.Agent` 接口
2. `a2aagent.A2AAgent` 也实现 `agent.Agent` 接口
3. Wrapper 只需调用 `agent.Run(ctx, inv)`，不关心是本地还是远程
4. 统一接口消除了本地/远程的代码分支

### 14.7 为什么 exec 工具注册 ID 是 exec，但 Declaration Name 是 action？

**设计决策**：
- 注册表 ID `exec`：标识这是一个执行器工具，在 YAML 配置中使用 `id: exec`
- LLM 看到的工具名 `action`：语义上表示"执行行为动作"，与执行器职责一致

```yaml
tools:
  - kind: tool
    id: exec                 # 注册表 ID
    description_file: action_tool_desc.md
```

LLM 调用时使用 `action` 作为 tool name：

```json
{"name": "action", "arguments": {"command": "ls -la"}}
```

## 十五、任务重入（resume_task）与会话回收

### resume 状态机

```mermaid
stateDiagram-v2
    [*] --> running: spawn
    running --> stable: SettleStable(窗口内)
    running --> alive_detached: 后台 stable
    running --> completed: SettleCompleted
    running --> failed: SettleCompleted+Err
    stable --> running: resume(input)
    alive_detached --> running: resume(input)
    completed --> running: resume(input,round 型)
    failed --> running: resume(input,round 型)
    running --> cancelled: cancel
```

合法源状态按**轮次边界**定义（存活类=会话重入；完成态=round 型执行器自然续行点），running/suspect/cancelled 拒绝并引导。并发 resume 占坑单胜（task.mu 内置 running 再调 ResumeFn，失败回滚）。

### 特异出入口

| 执行器 | resume 实现 | 关键机制 |
|---|---|---|
| tmux | 同一 detector `Rearm(baseline)` + `SendKeys` | detector 绑会话而非轮次——回调/watch 永不换手，零换绑零竞态；输出=基线后增量；TouchSession 重回 dense 轮询；TUI 拒绝 |
| subagent | 新 Run + 任务链还原器 | 本任务前序轮次链（上次 settle 结果为首，`resume_context_rounds` 封顶）注入 external_context；只含本任务内容；无进程复活 |

任务层 `detector != task.detector` 一行区分两形态：同 detector 不退役 watch；新 detector 走 watchDone 退役（防泄漏与陈旧信号串轮）。

### 会话回收闭环

| 时机 | 机制 |
|---|---|
| 运行时 | completed/error → 自动 kill session |
| 优雅退出 | `ActionTool.Close()` 收编 monitor 内全部存活 session |
| 崩溃/强杀后 | 下次启动 `CleanupOrphanSessions()` 按前缀清扫孤儿（每个孤儿占一个 pty，实机曾因此耗尽系统 pty 池）；多实例场景 `WithOrphanCleanupDisabled` 或独立前缀 |
