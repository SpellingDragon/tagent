## Context

OpenSpec 是项目已有的 spec-driven 开发工具，工作流为：

```
① openspec new change "<name>" → 创建 change 目录
   openspec/changes/<name>/
     proposal.md   ← 做什么、为什么
     design.md      ← 怎么做（可选）
     specs/**/*.md ← 需求规格（可选）
     tasks.md       ← 任务清单 (- [ ] / - [x])

② 按 tasks.md 逐步执行，每完成一个 task:
   - [ ] → - [x]

③ openspec archive "<name>" → 归档
   openspec/changes/<name>/ → openspec/changes/archive/YYYY-MM-DD-<name>/
```

### tagent 的子 agent 模式

tagent 已有 knowledge、recall、action 三个子 agent，通过 `AgentToolWrapper` 包装为 CallableTool。调用模式：

```
tagent LLM → tool_call(plan, {request: "创建获取文章的计划"})
  → AgentToolWrapper.Call():
    ① 解析 event_keys（可选）
    ② 从 parentStore 获取外部上下文
    ③ plan 子 agent Run():
       → plan LLM 分析请求
       → plan LLM 调用 exec("openspec new change ...")
       → plan LLM 调用 save_file("proposal.md", ...)
       → plan LLM 调用 save_file("tasks.md", ...)
       → plan LLM 生成 final response: "计划已创建，3个任务..."
    ④ AgentToolWrapper 返回 finalOutput
  → tagent LLM 看到 "计划已创建，3个任务..."
```

**关键设计**：tagent 不直接操作 openspec 文件。plan agent 封装了 openspec 的操作细节，tagent 只需要告诉 plan "做什么"。

### 原型不变量对齐

```
不变量 1: inputs 是投影（有界）
  → PlanProgressTracker 注入的是 tasks.md 的进度摘要（轻量），不是完整文件

不变量 2: Compact 只修改投影
  → 计划数据在文件系统（openspec/changes/），不在 MemoryStore
  → PlanProgressTracker 只读取并注入摘要，不修改任何状态

不变量 3: 工具结果回写 bus
  → plan agent 的 finalOutput 作为 tool result 走正常事件流
  → plan agent 内部的 exec/save_file 结果留在 plan agent 的 session 中
```

## Goals / Non-Goals

**Goals:**
- plan 子 agent 通过 exec/read_file/save_file 操作 openspec CLI 创建、更新、归档计划
- tagent 通过 AgentToolWrapper 调用 plan agent，不直接操作 openspec 文件
- PlanProgressTracker BeforeModel 回调自动读取活跃 change 的 tasks.md，注入进度摘要到 tagent 上下文
- FrameworkPrompt 说明何时调用 plan agent

**Non-Goals:**
- 不新建 plan 事件类型（openspec change 的生命周期通过文件系统管理）
- 不修改 openspec CLI 本身
- 不让 tagent 直接读写 openspec 文件（通过 plan agent 封装）
- 不强制所有任务都走 plan（简单任务直接 ReAct 执行）

## Decisions

### Decision 1: plan 作为子 agent，不作为普通工具

**选择**: plan 是一个 `TagentAgent` 子 agent（和 knowledge、recall 一样），有自己的 LLM、system prompt 和工具（exec、read_file、save_file）。通过 `AgentToolWrapper` 包装为 tagent 的工具。

**理由**: plan 需要多步推理——分析任务、决定 openspec change 名称、生成 proposal 内容、编写 tasks.md。这是 LLM 的工作，不是简单的函数调用。子 agent 模式让 plan 有独立的 ReAct 循环完成这些步骤。

**替代方案**: 普通工具封装 openspec 命令（不推荐——无法生成结构化的 proposal/tasks 内容，只是 exec 的 thin wrapper）。

### Decision 2: tagent 不直接操作 openspec 文件

**选择**: tagent 的工具列表中不包含 openspec 相关的文件操作。plan agent 独占 openspec 操作权。tagent 需要创建/更新/查询计划时，通过 `tool_call(plan, {request: "..."})` 调用 plan agent。

**理由**: 职责分离。tagent 负责高层决策（做什么），plan agent 负责计划文档的产出和维护（怎么做计划）。避免 tagent 直接操作文件导致的状态不一致。

### Decision 3: PlanProgressTracker 直接读文件系统，不走 plan agent

**选择**: PlanProgressTracker BeforeModel 回调直接读取 `openspec/changes/` 目录和 tasks.md 文件，不通过 plan agent。只在 tagent 的 LLM 调用前注入进度摘要。

**理由**: 进度注入是高频操作（每次 LLM 调用前），不应每次都启动 plan 子 agent。直接读文件系统开销极低。PlanProgressTracker 只读取不修改，不破坏 plan agent 的操作权。

### Decision 4: FrameworkPrompt 说明 plan agent 的使用时机

**选择**: 在 FrameworkPrompt 中增加段落：

```
## 工作计划（Plan Agent）

面对复杂多步骤任务时，调用 plan 工具创建结构化计划：
- plan agent 会通过 openspec 创建 proposal 和 tasks.md
- 每完成一个任务后，调用 plan 工具更新进度
- 所有任务完成后，调用 plan 工具归档
- 框架会在每次思考前自动注入当前计划进度
简单单步任务不需要创建计划。
```

**理由**: tagent LLM 需要知道 plan agent 的存在和使用方式。

## Risks / Trade-offs

| 风险 | 缓解 |
|------|------|
| LLM 不主动调用 plan agent | FrameworkPrompt 明确说明；简单任务不强制 |
| plan agent 生成低质量 tasks.md | plan agent 的 system prompt 提供格式指引和示例 |
| tasks.md 读写竞争 | tagent 单 agent 执行，plan agent 同步调用（Run 阻塞），无并发写 |
| PlanProgressTracker 和 plan agent 读同一文件 | Tracker 只读不写，无竞争 |
| openspec CLI 不在 PATH | plan agent 的 exec 执行失败时 LLM 可感知错误并反馈给 tagent |
| 进度摘要占用 token | 摘要只包含 task 标题和状态标记，不含详细内容 |
