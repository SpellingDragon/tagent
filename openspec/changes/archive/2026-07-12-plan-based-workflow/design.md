## Context

OpenSpec 是项目已有的 spec-driven 开发工具，工作流为：
```
openspec new change "<name>" → 创建 proposal.md + tasks.md
执行中: tasks.md checkbox - [ ] → - [x]
openspec archive "<name>" → 归档
```

### 原型的可扩展执行逻辑

原型中 `BaseTAgent` 的 `Run` 和 `OnEvents` 是**可替换的函数字段**：

```go
type BaseTAgent struct {
    Run             func()
    OnEvents        func(event []Event) Event
    ModelCompletion func(inputs []string) string
    // ...
}

func (agent *BaseTAgent) New() {
    agent.Run = agent.DefaultRun        // 可替换
    agent.OnEvents = agent.DefaultOnEvents  // 可替换
}
```

生产实现中 `TagentAgent.Run` 是方法。但 plan agent 可以**重写 Run 方法**，在进入 ReAct 循环前拦截特定请求，交由工程逻辑处理：

```mermaid
graph TB
    A["AgentToolWrapper.Call(ctx, args)"] --> B{args.action == "query_progress"?}
    B -->|"是"| C["工程逻辑: 读 tasks.md<br/>解析 checkbox<br/>返回进度摘要"]
    B -->|"否"| D["标准 Run: 进入 ReAct 循环<br/>LLM 推理 → exec/save_file"]
    C --> E["返回 tool result"]
    D --> E
```

### plan agent 的两种操作模式

plan agent 通过**自定义 Run 方法**实现双模式。`AgentToolWrapper.Call` 解析 `action` 参数，决定走工程直读还是标准 ReAct：

| action 参数 | 执行路径 | 原因 |
|------------|---------|------|
| `create` | 标准 ReAct | LLM 分析任务目标、生成 proposal + tasks |
| `update` | 标准 ReAct | LLM 理解描述、决定更新哪个 checkbox |
| `archive` | 标准 ReAct | LLM 确认完成、调用 openspec archive |
| `progress` | **工程直读** | 直接读 tasks.md → 解析 checkbox → 返回摘要 |

**工程直读不过 model 的价值**：节省一次 LLM 调用，降低延迟和 token 消耗。tagent 在 ReAct 循环中可能频繁查询进度（尤其上下文压缩后恢复时），每次都过 model 是浪费。

### 为什么删除 PlanProgressTracker BeforeModel 回调

之前的设计在 tagent 每次 LLM 调用前自动注入进度摘要。问题：
1. tagent 不一定每次都需要进度信息——简单任务不需要
2. 自动注入浪费 token（即使 tagent 不关注进度）
3. 进度追踪应该是 plan agent 的职责，不应侵入 tagent 的回调链

**正确做法**：tagent 需要时主动调用 `tool_call(plan, {action: "progress"})`。plan agent 的自定义 Run 拦截此请求，直接读文件返回。

### 原型不变量对齐

```
不变量 1: inputs 是投影（有界）
  → 进度查询结果作为 tool result 进入 tagent 的 messages，不额外注入

不变量 2: Compact 只修改投影
  → 无 BeforeModel 回调，计划数据完全在文件系统

不变量 3: 工具结果回写 bus
  → plan agent 的返回值（无论是 model 生成还是工程直读）都作为 tool result 走正常事件流
```

## Goals / Non-Goals

**Goals:**
- plan agent 通过自定义 Run 方法实现双模式：工程直读（查询进度）和标准 ReAct（创建/更新/归档）
- plan_tool_desc.md 使用结构化参数（`action` 字段）而非纯自然语言，让 tagent LLM 明确指定操作类型
- 删除 PlanProgressTracker 及所有相关代码
- FrameworkPrompt 保持通用——不硬编码 plan 工具名

**Non-Goals:**
- 不修改 AgentToolWrapper 的核心调用机制（plan agent 的自定义 Run 在 wrapper 内部生效）
- 不修改 openspec CLI
- 不修改原型设计——利用原型的"Run 可替换"设计精神

## Decisions

### Decision 1: 删除 PlanProgressTracker，进度查询由 tagent 主动调用 plan agent

**选择**: 删除 `agent/plan_progress_tracker.go`、回调注册、`OpenSpecDir` 配置。tagent 需要进度时调用 `tool_call(plan, {action: "progress"})`。

**理由**: 进度追踪是 plan agent 的职责，不应侵入 tagent 的 BeforeModel 回调链。tagent 自行决定何时需要进度信息。

### Decision 2: plan agent 自定义 Run 方法实现工程直读

**选择**: plan agent 不是标准 `TagentAgent`，而是继承或包装 `TagentAgent` 并重写 `Run` 方法。在 `Run` 入口处检查 `inv.Message.Content` 或 `RuntimeState` 中的 `action` 字段：
- `action == "progress"`：直接读 `openspec/changes/` 下的 tasks.md，解析 checkbox，构建 final response event 返回。**不创建 EventBus、不启动 runEventLoop、不调用 LLM**。
- 其他 action：调用 `ta.TagentAgent.Run(ctx, inv)` 走标准 ReAct 循环。

**实现方式**：

```go
// PlanAgent wraps TagentAgent with custom Run for dual-mode operation.
type PlanAgent struct {
    *TagentAgent
    openSpecDir string
}

func (pa *PlanAgent) Run(ctx context.Context, inv *agent.Invocation) (<-chan *event.Event, error) {
    // Check if this is a progress query
    action := extractActionFromInvocation(inv)
    if action == "progress" {
        return pa.runProgressQuery(ctx, inv)
    }
    // Standard ReAct path
    return pa.TagentAgent.Run(ctx, inv)
}

func (pa *PlanAgent) runProgressQuery(ctx context.Context, inv *agent.Invocation) (<-chan *event.Event, error) {
    // 1. Scan openspec/changes/ for active change
    // 2. Read tasks.md
    // 3. Parse checkboxes
    // 4. Build progress summary
    // 5. Return as final response event — no LLM call
    ch := make(chan *event.Event, 1)
    ch <- buildProgressEvent(summary)
    close(ch)
    return ch, nil
}
```

**理由**: 遵循原型的"Run 可替换"设计精神。plan agent 在 Run 入口拦截特定请求，避免不必要的 LLM 调用。对 AgentToolWrapper 透明——它只看到 Run 返回的 event channel。

### Decision 3: plan_tool_desc.md 使用结构化参数

**选择**: 工具描述中使用 `action` 参数明确指定操作类型：

```
plan({action: "create", request: "为获取网站内容创建计划"})
plan({action: "update", request: "任务1.1完成，更新进度"})
plan({action: "archive", request: "归档计划"})
plan({action: "progress"})
```

**理由**: 结构化参数让 plan agent 的自定义 Run 能可靠地路由请求。纯自然语言需要 LLM 解析，违背了工程直读"不过 model"的初衷。

### Decision 4: FrameworkPrompt 不硬编码 plan 工具名

**选择**: FrameworkPrompt 保持通用描述（异步工具、事件标识、上下文压缩），不增加 plan 相关段落。plan 的使用说明完全由 `plan_tool_desc.md` 承载。

**理由**: FrameworkPrompt 是框架层面的运行时说明，不应依赖具体工具是否存在。plan 是可选工具，不是所有部署都会启用。

## Risks / Trade-offs

| 风险 | 缓解 |
|------|------|
| tagent 不主动查询进度 | LLM 通过工具描述理解何时该查询；用户也可以直接问"进度如何"触发查询 |
| PlanAgent 自定义 Run 需要处理 event channel | 直接构造 event.Event 返回，参考 AgentToolWrapper 的 finalOutput 收集逻辑 |
| action 参数解析失败 | 默认走标准 ReAct 路径，安全降级 |
| 删除 PlanProgressTracker 后已有测试失败 | 同步删除测试文件和相关配置 |
