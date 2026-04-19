# tagent/agent 模块架构文档

## 一、模块定位

`tagent/agent` 是 tagent 项目的**顶层组装层（Composition Root）**，也是 tagent 与 trpc-agent-go 框架的**集成粘合层**。

**核心职责**：将 trpc-agent-go 的通用能力（LLMAgent / Runner / Plugin）与 tagent 的差异化逻辑（SmartCompress / MemoryPlugin / TmuxMonitor 事件注入）组装为统一入口 `TagentAgent`。

**设计原则**：
- **复用而非重写**：tagent 不重复实现 React Loop，改用 LLMAgent 作为骨架
- **注入而非继承**：tagent 的能力通过 callback（BeforeModel）和 plugin（OnEvent）注入到框架
- **视图转换原则**：压缩发生在 BeforeModel，仅修改发给 LLM 的 messages 视图，不修改 Session 原始数据

---

## 二、组件关系总览图

```mermaid
graph TB
    subgraph "调用方"
        User([用户 / API])
        Tmux([TmuxMonitor 异步事件])
    end

    subgraph "tagent/agent"
        TA["TagentAgent\n(组合根)"]
        CI["ContextIntervention\n(BeforeModel 拦截器)"]
        SC["SmartCompressor\n(两阶段压缩)"]
        TC["TokenCounter\n(Token 估算)"]
    end

    subgraph "trpc-agent-go 框架"
        Runner["Runner\n(编排引擎)"]
        LLMAgent["LLMAgent\n(React Loop Flow)"]
        ModelCallbacks["model.Callbacks\n(BeforeModel/AfterModel 回调链)"]
        PluginManager["plugin.Manager\n(事件钩子链)"]
        Session["session.Session\n(会话状态)"]
    end

    subgraph "tagent/plugin"
        MP["MemoryPlugin\n(OnEvent: 持久化+因果链)"]
        SP["SummaryPlugin\n(OnEvent: Tag 注入)"]
    end

    subgraph "tagent/memory"
        MS["memory.MemoryStore\n(记忆存储)"]
    end

    subgraph "外部依赖"
        LLM["model.Model\n(LLM: GLM-4 / OpenAI ...)"]
        Tools["[]tool.Tool\n(CallableTools)"]
    end

    User --> TA
    Tmux --> TA

    TA --> Runner
    TA --> LLMAgent
    TA --> CI
    TA --> MS

    Runner --> Session
    Runner --> PluginManager
    Runner --> LLMAgent

    LLMAgent --> ModelCallbacks
    LLMAgent --> LLM
    LLMAgent --> Tools

    ModelCallbacks --> CI
    CI --> SC
    CI --> TC

    PluginManager --> MP
    PluginManager --> SP

    MP --> MS

    style TA fill:#e1f5ff,stroke:#0277bd,stroke-width:3px
    style Runner fill:#f3e5f5,stroke:#7b1fa2,stroke-width:2px
    style LLMAgent fill:#e8f5e9,stroke:#2e7d32,stroke-width:2px
    style CI fill:#fff3e0,stroke:#ef6c00,stroke-width:2px
    style SC fill:#fff8e1,stroke:#f9a825,stroke-width:2px
```

---

## 三、文件清单与职责

| 文件 | 行数 | 职责 |
|------|------|------|
| `tagent_agent.go` | 205 | 组合根：初始化 + 对外 API |
| `context_intervention.go` | 85 | BeforeModel 拦截器：token 预算检查 + 触发压缩 |
| `smart_compress.go` | 226 | 两阶段压缩引擎：按任务边界切分 + LLM 摘要 |
| `token_counter.go` | 42 | Token 估算器：启发式字符计数 |

---

## 四、核心数据结构

### 4.1 TagentAgent — 组合根

```go
// tagent_agent.go:29-39
type TagentAgent struct {
    llmAgent   *llmagent.LLMAgent  // 框架 React Loop
    runner     runner.Runner        // 框架编排引擎
    memStore   memory.MemoryStore  // 记忆存储（内存 or 文件）
    config     *TagentConfig

    // 异步事件注入的会话上下文（首次 Run 时缓存）
    lastUserID    string
    lastSessionID string
}
```

**为什么需要 lastUserID / lastSessionID？**

TmuxMonitor 在后台异步检测 tmux 会话状态变更，需要注入 `RoleSystem` 消息触发新一轮 Agent 迭代。但 `handleTmuxStateChange` 没有传入 userID/sessionID 参数，只能依赖缓存的最近一次调用上下文。

### 4.2 TagentConfig — 配置参数

```go
// tagent_agent.go:41-51
type TagentConfig struct {
    Model             model.Model        // ✅ 必填：LLM 模型
    MemoryStore       memory.MemoryStore // 可选：默认 InMemoryStore
    SystemPrompt      string             // 可选：系统提示词
    Tools             []tool.Tool        // 可选：CallableTools
    MaxToolIterations int                // 默认 200
    MaxTokens         int                // Token 预算，默认 8000
    CompressThreshold float64            // 触发压缩阈值，默认 0.8
    SummaryModel      model.Model        // 可选：Stage 2 摘要模型
}
```

### 4.3 ContextIntervention — 调用前拦截器

```go
// context_intervention.go:10-20
type ContextIntervention struct {
    compressor   *SmartCompressor  // 实际执行压缩
    tokenCounter TokenCounter      // Token 估算
    maxTokens    int               // 最大 token 预算
    thresholdPct float64          // 触发压缩阈值比例
}
```

### 4.4 SmartCompressor — 两阶段压缩

```go
// smart_compress.go:19-22
type SmartCompressor struct {
    summaryModel    model.Model // Stage 2 LLM 摘要模型（可选）
    keepRecentTasks int         // 保留最近 N 个完整任务（默认 2）
}

// TaskSegment 是任务边界切分的最小单元
type TaskSegment struct {
    Messages   []model.Message
    IsComplete bool // 是否为完整任务（assistant 无 tool_calls）
}
```

### 4.5 TokenCounter — Token 估算接口

```go
// token_counter.go:8-10
type TokenCounter interface {
    Estimate(messages []model.Message) int
}

// 默认实现：启发式字符计数
type DefaultTokenCounter struct {
    CharsPerToken float64  // 默认 2.0（中英混合）
}
```

---

## 五、初始化流程 — NewTagentAgent

```go
// tagent_agent.go:61-130
func NewTagentAgent(cfg *TagentConfig) (*TagentAgent, error) {
    // Step 1: MemoryStore（外部注入 or 内存默认）
    var memStore memory.MemoryStore
    if cfg.MemoryStore != nil {
        memStore = cfg.MemoryStore
    } else {
        memStore = memory.NewInMemoryStore()
    }

    // Step 2: MemoryPlugin — 注册 OnEvent 钩子（持久化 + 因果链）
    memPlugin := tagentplugin.NewMemoryPlugin(memStore)

    // Step 3: SmartCompressor（带可选 Stage 2 模型）
    compressorOpts := []SmartCompressorOption{}
    if cfg.SummaryModel != nil {
        compressorOpts = append(compressorOpts, WithSummaryModel(cfg.SummaryModel))
    }
    compressor := NewSmartCompressor(compressorOpts...)

    // Step 4: ContextIntervention — 包装为 BeforeModel 回调
    tokenCounter := NewDefaultTokenCounter()
    ci := NewContextIntervention(compressor, tokenCounter, cfg.MaxTokens, cfg.CompressThreshold)

    // Step 5: model.Callbacks — 注册 BeforeModel 链
    modelCB := model.NewCallbacks()
    modelCB.RegisterBeforeModel(ci.BeforeModel)

    // Step 6: LLMAgent — 注入 ModelCallbacks（包含 CI.BeforeModel）
    llmAgentOpts := []llmagent.Option{
        llmagent.WithModel(cfg.Model),
        llmagent.WithInstruction(cfg.SystemPrompt),
        llmagent.WithMaxToolIterations(cfg.MaxToolIterations),
        llmagent.WithModelCallbacks(modelCB),  // 关键：BeforeModel 回调注入点
    }
    if len(cfg.Tools) > 0 {
        llmAgentOpts = append(llmAgentOpts, llmagent.WithTools(cfg.Tools))
    }
    llmAgent := llmagent.New("tagent", llmAgentOpts...)

    // Step 7: Runner — 组装 LLMAgent + Plugins
    r := runner.NewRunner("tagent", llmAgent, runner.WithPlugins(
        tagentplugin.NewSummaryPlugin(),  // 事件 Tag 注入
        memPlugin,                         // 事件持久化
    ))

    return &TagentAgent{...}, nil
}
```

---

## 六、trpc-agent-go 关键集成机制

### 6.1 BeforeModel 回调链

`BeforeModel` 是 **LLM 调用前** 的拦截点，tagent 通过它实现上下文压缩。

**框架调用路径**：

```
Runner.Run()
  → LLMAgent.Run()
    → Flow.Run()
      → runOneStep()
        → buildRequest()
          → runBeforeModelCallbacks()
            → runBeforeModelCallbacksWith(invocation.Plugins.ModelCallbacks())
            → runBeforeModelCallbacksWith(flow.modelCallbacks)
```

源码位置：`trpc-agent-go/internal/flow/llmflow/llmflow.go:873-901`

```go
func (f *Flow) runBeforeModelCallbacks(ctx context.Context, invocation *agent.Invocation, llmRequest *model.Request) (context.Context, *model.Response, error) {
    // 1. 先执行 Plugin 注册的 BeforeModel（通过 invocation.Plugins）
    pluginCallbacks := invocation.Plugins?.ModelCallbacks()
    if pluginCallbacks != nil {
        ctx, resp, err := runBeforeModelCallbacksWith(callbackCtx, invocation, llmRequest, pluginCallbacks)
        if resp != nil { return ctx, resp, nil } // CustomResponse 可短路 LLM 调用
        if err != nil { return ctx, nil, err }
    }
    // 2. 再执行 LLMAgent 注册的 BeforeModel
    newCtx, resp, err := runBeforeModelCallbacksWith(ctx, invocation, llmRequest, f.modelCallbacks)
    return newCtx, resp, err
}
```

**关键特性**：
- **CustomResponse 短路**：如果 BeforeModel 返回了非空 `CustomResponse`，框架跳过 LLM 调用直接返回
- **Context 传递**：每个回调可以返回新的 `context.Context`，后续回调使用新的 context
- **panic 恢复**：`model.Callbacks.runBeforeModelCallback` 中有 `defer recoverModelCallbackPanic`，防止单个回调 panic 破坏整个流程
- **continueOnError / continueOnResponse**：通过 `model.Callbacks` 的选项控制错误和响应的处理策略

**tagent 的注入方式**：

```
NewTagentAgent
  → model.NewCallbacks()
  → modelCB.RegisterBeforeModel(ci.BeforeModel)
  → llmagent.New(..., llmagent.WithModelCallbacks(modelCB))
  → Runner(llmAgent)
```

tagent 将 `ContextIntervention.BeforeModel` 注册到 `model.Callbacks`，通过 `llmagent.WithModelCallbacks` 注入到 LLMAgent，最终在 Flow 层每次 LLM 调用前执行。

### 6.2 Plugin OnEvent 钩子链

`Plugin.OnEvent` 是**每个事件流经 Runner 时**的钩子点，tagent 通过它实现事件持久化和 Tag 注入。

**框架调用路径**：

```
Runner.processSingleAgentEvent()
  → Runner.applyEventPlugins(ctx, invocation, agentEvent)
    → invocation.Plugins.OnEvent(ctx, invocation, e)
      → Manager.OnEvent()
        → 依次执行所有已注册的 EventHook
```

源码位置：`trpc-agent-go/runner/runner.go:807-828`

```go
func (r *runner) applyEventPlugins(ctx context.Context, invocation *agent.Invocation, e *event.Event) *event.Event {
    if invocation == nil || invocation.Plugins == nil {
        return e
    }
    updated, err := invocation.Plugins.OnEvent(ctx, invocation, e)
    if err != nil {
        log.ErrorfContext(ctx, "plugin OnEvent failed: %v", err)
        return e  // 出错时回退原始事件
    }
    if updated == nil {
        return e  // 钩子可选择不修改事件
    }
    copyEventInvocationFields(updated, e)  // 保留原始事件的元数据
    return updated
}
```

**Plugin Manager 的实现**（`trpc-agent-go/plugin/manager.go:275-295`）：

```go
func (m *Manager) OnEvent(ctx context.Context, invocation *agent.Invocation, e *event.Event) (*event.Event, error) {
    curr := e
    for _, h := range m.eventHooks {  // 依次执行每个钩子
        next, err := h.hook(ctx, invocation, curr)
        if err != nil {
            return nil, fmt.Errorf("plugin %q: %w", h.name, err)
        }
        if next != nil {
            curr = next  // 链式传递：上一个钩子的输出是下一个钩子的输入
        }
    }
    return curr, nil
}
```

**tagent 的两个 Plugin**：

| Plugin | 钩子 | 作用 |
|--------|------|------|
| `MemoryPlugin` | `OnEvent` | 推断事件类型、生成 EventKey、构建因果链、持久化到 MemoryStore、写回 StateDelta |
| `SummaryPlugin` | `OnEvent` | 给事件设置 Tag（`agent_output` / `action_command` 等） |

### 6.3 Runner 的编排职责

`Runner` 是 trpc-agent-go 的**编排引擎**，tagent 通过它获得：
- Session 管理（会话创建、消息追加）
- Plugin 管理（OnEvent 钩子注入）
- 请求/响应事件流处理
- 并发运行管理（Cancel / RunStatus）

`Runner.Run()` 的关键步骤（`trpc-agent-go/runner/runner.go:329-495`）：

```
1. 生成 RequestID (uuid)
2. 创建执行上下文（Timeout / DetachedCancel）
3. 获取或创建 Session（userID + sessionID → session.Key）
4. 选择 Agent（注册表 or AgentFactory）
5. 创建 Invocation（封装所有上下文）
6. 注册 RunHandle（用于 Cancel / RunStatus）
7. 追加用户消息到 Session
8. 调用 Agent.RunWithPlugins() 触发 Flow
9. 启动 processAgentEvents goroutine 消费事件流
   → 对每个事件 applyEventPlugins()
   → 持久化到 Session
   → 发送到输出 channel
```

### 6.4 LLMAgent 的 Flow 机制

`LLMAgent` 的核心是一个由**请求处理器链**和**响应处理器链**组成的 Flow。

**请求处理器链**（`trpc-agent-go/agent/llmagent/llm_agent.go:191-331`）：

```
buildRequestProcessorsWithAgent()
  1. BasicRequestProcessor        — 生成配置
  2. PlanningRequestProcessor    — 规划指令
  3. InstructionRequestProcessor — 系统提示词 / 指令
  4. IdentityRequestProcessor    — Agent 身份
  5. SkillsRequestProcessor      — Skill 内容注入
  6. ContentRequestProcessor     — 对话历史注入 ← BeforeModel 拦截点
  7. PostToolRequestProcessor    — Tool 后提示词
  8. SkillsToolResultProcessor   — Skill 结果处理
  9. TimeRequestProcessor        — 时间信息
```

**ContentRequestProcessor** 构建最终发给 LLM 的 `Request.Messages`。tagent 的 `BeforeModel` 在这个 Processor 执行后**修改** messages，实现压缩而不破坏 Session 原始数据。

**Flow.Run 的执行循环**（`trpc-agent-go/internal/flow/llmflow/llmflow.go:88-181`）：

```go
func (f *Flow) Run(ctx context.Context, invocation *agent.Invocation) (<-chan *event.Event, error) {
    eventChan := make(chan *event.Event, ...)
    go func(ctx context.Context) {
        for {
            f.emitStartEventAndWait(ctx, invocation, eventChan)   // 等待 barrier
            f.maybeSyncSummaryIntraRun(ctx, invocation)           // 迭代间摘要
            lastEvent, err := f.runOneStep(ctx, invocation, eventChan)  // 一次 LLM 迭代
            if lastEvent == nil || invocation.EndInvocation || lastEvent.IsFinalResponse() {
                break  // 结束条件
            }
        }
    }(runCtx)
    return eventChan, nil
}
```

---

## 七、请求处理流程

### 7.1 标准同步请求（用户输入）

```mermaid
sequenceDiagram
    participant U as 用户
    participant TA as TagentAgent
    participant R as Runner
    participant S as Session
    participant LA as LLMAgent
    participant F as Flow
    participant CI as ContextIntervention
    participant SC as SmartCompressor
    participant LLM as model.Model
    participant MP as MemoryPlugin
    participant SP as SummaryPlugin
    participant MS as MemoryStore

    U->>TA: Run(ctx, msg)
    TA->>R: runner.Run()
    R->>S: 获取/创建 Session
    R->>LA: llmAgent.Run(invocation)
    LA->>F: flow.Run()

    loop 每次 LLM 迭代 (ReAct Loop)
        F->>F: buildRequest() — 构建 messages

        Note over F: ContentRequestProcessor<br/>注入对话历史

        F->>CI: BeforeModel(args)
        CI->>CI: tokenCounter.Estimate()

        alt Token 充足
            CI-->>F: 直接通过（不修改）
        else Token 超限
            CI->>SC: compressor.Compress()
            SC->>SC: Stage 1: 任务边界切分
            alt 有 SummaryModel
                SC->>LLM: Stage 2: LLM 生成摘要
                LLM-->>SC: 摘要文本
            end
            SC-->>CI: compressed messages
            CI->>CI: ensureUserPrompt()
            CI-->>F: args.Request.Messages = compressed
        end

        F->>LLM: model.GenerateContent()

        loop 流式响应
            LLM-->>F: response events
            F->>SP: applyEventPlugins → Tag 注入
            F->>MP: applyEventPlugins → 持久化
            MP->>MS: StoreEvent(eventKey, fullEvent)
            MP->>MP: 更新因果链 lastEventKey
            F->>R: emit event
        end

        F->>F: postprocess — 处理 tool_calls
    end

    R->>S: 追加事件到 Session
    R-->>U: event stream
```

### 7.2 TmuxMonitor 异步事件注入

```mermaid
sequenceDiagram
    participant TM as TmuxMonitor
    participant CT as CommandTool
    participant TA as TagentAgent
    participant R as Runner
    participant CI as ContextIntervention
    participant LLM as model.Model
    participant MS as MemoryStore

    Note over TM: tmux 会话状态变更
    TM->>CT: onStateChange(session, old, new, output)
    CT->>TA: handleTmuxStateChange()
    TA->>TA: 构建 system 消息 (RoleSystem)<br/>截断 output 到 2000 字符

    alt 首次调用（无缓存）
        TA-->>CT: 忽略（lastUserID == ""）
    else 已有调用上下文
        TA->>R: runner.Run(ctx, lastUserID, lastSessionID, system_msg)
        R->>R: 复用已有 Session
        R->>R: 追加 system 消息 (RoleSystem)
        R->>R: 触发新一轮 ReAct Loop
        R->>CI: BeforeModel（可能触发压缩）
        R->>LLM: model.GenerateContent()
        R->>R: 事件流
    end

    Note over TA: 后台 goroutine 消费事件<br/>防止 goroutine 泄漏
```

---

## 八、ContextIntervention.BeforeModel — 详解

**源码位置**：`context_intervention.go:37-64`

```go
func (ci *ContextIntervention) BeforeModel(
    ctx context.Context,
    args *model.BeforeModelArgs,
) (*model.BeforeModelResult, error) {
    if args == nil || args.Request == nil || len(args.Request.Messages) == 0 {
        return nil, nil
    }

    // Step 1: 估算当前 messages 的 token 数
    usedTokens := ci.tokenCounter.Estimate(args.Request.Messages)

    // Step 2: 计算触发阈值
    threshold := int(float64(ci.maxTokens) * ci.thresholdPct)

    // Step 3: 超过阈值则压缩
    if usedTokens > threshold {
        log.Infof("ContextIntervention: token %d > threshold %d, compressing", usedTokens, threshold)

        // Stage 1 + 可选 Stage 2
        compressed := ci.compressor.Compress(ctx, args.Request.Messages)
        // 确保有 user prompt（防止 LLM 只看到 agent_output）
        compressed = ensureUserPrompt(compressed)
        // 替换发送给 LLM 的 messages（不修改 Session！）
        args.Request.Messages = compressed

        newTokens := ci.tokenCounter.Estimate(args.Request.Messages)
        log.Infof("compressed from %d to %d tokens", usedTokens, newTokens)
    }

    // Step 4: 返回 nil 表示不短路流程，LLM 正常调用
    return nil, nil
}
```

**返回值语义**：
- `return nil, nil` — 不短路，继续正常 LLM 调用
- `return &BeforeModelResult{CustomResponse: resp}, nil` — 短路，跳过 LLM 调用
- `return nil, err` — 中断，返回错误

---

## 九、SmartCompressor — 两阶段压缩详解

### 9.1 Stage 1：按任务边界切分

**源码位置**：`smart_compress.go:114-145`

**任务边界定义**：`IsComplete = msg.Role == assistant && len(msg.ToolCalls) == 0`

```
messages[0]: user "task 1"
messages[1]: assistant "result 1"
           ↑ 任务边界（完整任务 → IsComplete=true）

messages[2]: user "task 2"
messages[3]: assistant [tool_calls]
           ↑ 非边界（tool_call 周期内）
messages[4]: tool "result"
messages[5]: assistant "done"
           ↑ 任务边界（完整任务 → IsComplete=true）
```

**切分逻辑**：
- 从头开始遍历消息
- 遇到 `IsComplete=true` 的 assistant 消息 → 切分段
- 未完成的任务段（当前在进行的 tool_call 周期）不切分，保留在最后

### 9.2 Stage 2：LLM 生成摘要

**源码位置**：`smart_compress.go:147-220`

**触发条件**：`summaryModel != nil` 且有被丢弃的旧片段

**摘要提示词**：
```
你是一个对话摘要助手。请为历史对话生成简洁但完整的摘要。

--- 片段 1 ---
user: ...
assistant: ...
[tool_calls: func1(...), func2(...)]

--- 片段 2 ---
...

--- 摘要 ---
```

**调用方式**：直接调用 `summaryModel.GenerateContent()`，消费流式响应，返回纯文本。

**失败回退**：如果 LLM 调用失败或返回空，返回 `compressNotice(n)` — 简单提示「N 个任务片段已省略」。

### 9.3 压缩输出结构

```
输入: [system] + [task1全部] + [task2全部] + [task3全部] + [task4全部] + [task5全部]
      假设 keepRecentTasks=2，丢弃 task1-task3，保留 task4-task5

Stage 1 输出: system + [task1-task3通知] + [task4全部] + [task5全部]
Stage 2 输出: system + [LLM摘要] + [task4全部] + [task5全部]
```

---

## 十、TokenCounter — 估算公式

**源码位置**：`token_counter.go:26-41`

```go
func (c *DefaultTokenCounter) Estimate(messages []model.Message) int {
    total := 0
    for _, msg := range messages {
        total += len([]rune(msg.Content)) / int(c.CharsPerToken)
        total += 10                                    // 每条消息 overhead
        total += 20 * len(msg.ToolCalls)               // tool_calls overhead
    }
    if total < 1 {
        total = 1
    }
    return total
}
```

**估算公式**：

```
estimatedTokens = Σ( Content长度 / CharsPerToken + 10 + 20×ToolCalls数 )
```

中英混合场景：`CharsPerToken = 2.0`（2 中文字符 ≈ 1 token；4 英文字符 ≈ 1 token）

---

## 十一、MemoryPlugin — OnEvent 钩子详解

**源码位置**：`tagent/plugin/memory_plugin.go:50-113`

```go
func (p *MemoryPlugin) onEvent(ctx context.Context, inv *agent.Invocation, evt *event.Event) (*event.Event, error) {
    // 1. 推断事件类型
    eventType := inferEventType(evt)  // 根据 Message.Role

    // 2. 生成事件 Key（时间戳 + 计数器）
    eventKey := memory.NewEventKey(time.Now().UnixMilli(), 0)

    // 3. 提取摘要
    eventSummary := extractSummary(evt)

    // 4. 获取前驱事件 Key（因果链）
    parentKey := p.lastEventKey

    // 5. 构建 FullEvent
    fullEvent := memory.FullEvent{
        EventKey:     eventKey,
        ParentKey:    parentKey,
        EventType:    eventType,
        EventSummary: eventSummary,
        Timestamp:    timestamp,
        Content:      ...,
        ToolCalls:    ...,
        Response:     ...,
    }

    // 6. 持久化到 MemoryStore
    p.memStore.StoreEvent(eventKey, fullEvent)

    // 7. 写回 EventKey/EventType 到 StateDelta
    evt.StateDelta["event_key"] = []byte(eventKey)
    evt.StateDelta["event_type"] = []byte(eventType)

    // 8. 更新因果链（线程安全）
    p.lastEventKey = eventKey

    return evt, nil
}
```

**事件类型推断规则**：

| Message.Role | EventType | 说明 |
|-------------|-----------|------|
| `RoleUser` | `external_input` | 用户输入 |
| `RoleSystem` | — | 不参与事件流（初始化时注入 system prompt） |
| `RoleAssistant + ToolCalls` | `thinking_plan` | Agent 思考/计划 |
| `RoleAssistant` | `agent_output` | Agent 输出 |
| `RoleTool` | `action_command` | Tool 调用结果 |

---

## 十二、关键设计决策

### 12.1 BeforeModel vs AfterModel

| 对比项 | BeforeModel（tagent 选型） | AfterModel |
|--------|--------------------------|-----------|
| **修改对象** | `Request.Messages` | `Response.Choices` |
| **修改位置** | LLM 调用前 | LLM 调用后 |
| **信息损失** | 低（可选 LLM 摘要） | 高（直接截断） |
| **LLM 感知** | 可能看到摘要 | 不知道被截断 |
| **实现复杂度** | 中等（两阶段） | 低（直接切） |

**选 BeforeModel 的原因**：
- Session 中仍保存完整历史，可供 RecallTool 全文搜索
- 压缩后 LLM 仍能通过摘要理解历史
- 不破坏 Session 完整性，支持多 Agent 共享

### 12.2 "视图转换"原则

`ContextIntervention` 和 `SmartCompressor` **仅修改 `args.Request.Messages`**（发给 LLM 的视图），**不修改 Session 原始数据**。

好处：
1. **记忆完整**：MemoryStore 中保存所有事件的原始内容
2. **检索无损**：RecallTool 可以对完整历史做语义搜索
3. **多 Agent 隔离**：多个 Agent 共享同一 Session 时，各自可独立决定压缩策略，互不干扰

### 12.3 TmuxMonitor 事件注入的幂等性

`handleTmuxStateChange` 在以下情况**忽略**事件注入：
- 首次调用前（`lastUserID == ""`）
- Session 已被关闭

注入的 system 消息使用 `RoleSystem`，分类为 `external_input`。LLM 将其视为系统级提示，tagent 将其与其他外部输入统一处理。

**严格拒绝非设计折损**：截断（truncation）在 `event/types.go` 中已完全移除。超过上下文限制的内容通过多次 SmartCompress 循环处理，而非在摘要阶段截断。

### 12.4 Plugin 钩子的链式传递

当多个 Plugin 注册 OnEvent 钩子时，**链式传递**：每个钩子的输出事件作为下一个钩子的输入。

```go
curr := e
for _, h := range m.eventHooks {
    next, err := h.hook(ctx, invocation, curr)
    if next != nil {
        curr = next  // 链式传递
    }
}
return curr
```

这意味着 `MemoryPlugin` 持久化的事件可以进一步被其他 Plugin 修改。

---

## 十三、TagentAgent API 参考

### 13.1 创建

```go
agent, err := tagentagent.NewTagentAgent(&tagentagent.TagentConfig{
    Model:             myModel,
    MemoryStore:       memory.NewFileBackend("/path/to/events"),
    SystemPrompt:      systemPrompt,
    Tools:             []tool.Tool{echoTool, commandTool},
    MaxToolIterations: 200,
    MaxTokens:         8000,
    CompressThreshold: 0.8,
    SummaryModel:      myModel, // Stage 2 使用相同模型
})
defer agent.Close()
```

### 13.2 运行

```go
eventCh, err := agent.Run(ctx, "user-1", "session-1", userMessage)
for evt := range eventCh {
    // 处理事件
}
```

### 13.3 TmuxMonitor 集成

```go
cmdTool := tagenttool.NewCommandTool(...)
agent.WireCommandTool(cmdTool)  // 自动设置 onStateChange 回调
```

### 13.4 直接访问 MemoryStore

```go
store := agent.MemStore()
events, _ := store.RecallEvent("user-1", "session-1", query, 10)
```
