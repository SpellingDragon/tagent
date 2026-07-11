## Context

tagent 的三层信息架构（MemoryStore / SessionProjection / messages）设计意图是：完整数据在 Layer 3，轻量引用在 Layer 2，压缩视图在 Layer 1。

原型三个不变量：
1. inputs 是投影（有界）
2. Compact 只修改投影，不碰 MemoryStore 或 EventBus
3. 工具结果回写 bus

SmartCompressor 的设计职责是**视图变换**——只修改发给 LLM 的 `[]model.Message`，不碰 MemoryStore 和 SessionProjection。Compactor 的职责是**投影变换**——只修改 SessionProjection，不碰 MemoryStore。

当前问题：
- `buildCompressEvent` 只输出裸 key 列表 `[1234, 1235, 1236]`，LLM 不知道每个 key 对应什么内容
- `extractExecutionState` 是独立函数，截断参数硬编码为包级常量
- TmuxMonitor 检测周期硬编码为 `DefaultMonitorConfig()`

### 原始 chunking 方案的反思

原始设计让 SmartCompressor 在压缩时写 MemoryStore + 修改 SessionProjection。这违反了不变量 2（SmartCompressor 是纯视图变换）。经分析，原始事件已存在于 MemoryStore 中，创建 chunk 事件是冗余的——LLM 只需知道每个 EventKey 对应什么内容即可 recall 原始事件。

### 方案 B：改进 compress message（最终选择）

从 oldSegments 消息的 `[evt_KEY|type]` 前缀中提取每个被压缩事件的 key + type，再从消息内容截取摘要。零副作用，纯视图变换。

## Goals / Non-Goals

**Goals:**
- `buildCompressEvent` 输出 key + type + summary 列表，LLM 据此判断该 recall 哪个 key
- `extractExecutionState` 迁移为 SmartCompressor 方法，截断参数可配置
- 压缩参数和 TmuxMonitor 检测周期通过 YAML 配置化
- ChunkSplitter 作为 extractExecutionState 的内部工具，提供语义截断能力

**Non-Goals:**
- 不创建新 MemoryStore 事件（不违反不变量 2）
- 不修改 SessionProjection（不侵入 Compactor 领域）
- 不改变框架 ContentRequestProcessor 的消息构建逻辑
- 不实现向量检索/embedding-based RAG
- 不改变 ActionTool 的异步执行模型

## Decisions

### Decision 1: buildCompressEvent 从 oldSegments 提取 key + type + summary

**选择**: 改进 `buildCompressEvent`，从 oldSegments 消息中解析 `[evt_KEY|type]` 前缀和内容摘要。

**理由**: 消息内容已由 `InjectEventKeys` (Callback 0) 注入了 `[evt_KEY|type]` 前缀。只需解析前缀获取 key 和 type，再截取消息内容前 N 字符作为摘要。无需访问 MemoryStore 或 SessionProjection。

**输出格式**:
```
[context_compress] 压缩了 3 个对话片段:
- evt_1234 [external_input]: 用户请求获取文章
- evt_1235 [thinking_plan]: 好的，我来帮你获取...
- evt_1236 [action_command]: 调用工具: action({"command":"curl ..."})

使用 recall 工具检索对应 key 获取完整内容。
```

**替代方案（已否决）**: chunking 方案让 SmartCompressor 写 MemoryStore + Projection，违反不变量 2。

### Decision 2: extractExecutionState 迁移 + ChunkSplitter 用于语义截断

**选择**: `extractExecutionState` 迁移为 `(sc *SmartCompressor)` 方法。截断长工具输出时使用 ChunkSplitter 按语义边界截取，而非机械按字符数截断。

**理由**: 截断参数作为 SmartCompressor 字段，通过 SmartCompressorOption 配置。ChunkSplitter 已实现，复用其语义切分能力避免在标题/段落中间截断。

### Decision 3: 配置化 — Agent 级别 + Tool 级别

**选择**: 压缩参数在 AgentConfig（agent 级别），TmuxMonitor 参数在 ActionProperties（tool 级别）。

**Agent 级别配置**:
```yaml
agents:
  - name: tagent
    compress:
      max_tool_result_chars: 500
      max_exec_state_chars: 2000
      chunk_size: 1000
      chunk_summary_len: 150
```

**Tool 级别配置**:
```yaml
tools:
  - kind: tool
    id: action
    properties:
      monitor:
        interval: 10s
        stable_duration: 30s
```

## Risks / Trade-offs

- [摘要质量依赖前缀解析] → `[evt_KEY|type]` 前缀由 InjectEventKeys 注入，如果消息没有前缀则回退到裸 key → 缓解：collectCompressedKeys 已有前缀解析逻辑，复用即可
- [摘要截取可能不完整] → 前 N 字符可能截断在语义单元中间 → 缓解：使用 ChunkSplitter 的 truncate 函数，在句号处截断
- [配置复杂度] → 新增配置项 → 缓解：所有参数有合理默认值，不配置时行为与当前一致
## Context

tagent 的三层信息架构（MemoryStore / SessionProjection / messages）设计意图是：完整数据在 Layer 3，轻量引用在 Layer 2，压缩视图在 Layer 1。但当前 SmartCompressor 的"丢弃旧段 + 单条 LLM 摘要"策略破坏了这个意图——压缩后 LLM 在 Layer 1 既看不到完整内容，也无法精确检索 Layer 3，因为摘要太粗粒度，LLM 不知道该 recall 哪个 EventKey。

同时，SmartCompressor 的截断参数（maxExecStateChars=2000, maxToolResultChars=500）和 TmuxMonitor 的检测周期（Interval=30s 等）都是硬编码常量，无法通过 YAML 配置调整。不同场景（短对话 vs 长文章检索 vs 大型项目构建日志）需要不同的压缩策略和检测频率。

### 当前压缩流程

```
Compress():
  1. splitSystemMessage → 分离 system prompt
  2. SegmentMessages → 按任务边界切分 (agent_output 为边界)
  3. protectPendingAsyncSegments → 保护未完成异步结果
  4. oldSegments / recentSegments 分割
  5. Stage 2: batchSegmentsByTokenBudget → summarizeBatches (LLM 生成单条摘要)
  6. collectCompressedKeys → 收集被压缩的 EventKey 列表
  7. buildCompressEvent → 生成 [context_compress] system message
  8. extractExecutionState → 提取工具调用/结果精简行 (独立函数, 截断 500→2000)
  9. findPendingUserMessage → 保留未回复的用户消息
  10. 拼接: systemMsg + compressEvent + summaryMsgs + execState + recentSegments + pendingUser
```

问题：
- Step 8 (extractExecutionState) 是独立函数，截断参数硬编码
- Step 5 (summarizeBatches) 只生成一条摘要，无法精确检索
- 被压缩的 EventKey 列表虽然放在 [context_compress] 消息中，但 LLM 不知道每个 key 对应什么内容

## Goals / Non-Goals

**Goals:**
- SmartCompressor 压缩旧段时，对大段工具输出执行语义感知切分，每个 chunk 独立持久化到 MemoryStore 并生成摘要
- LLM 在压缩后的 messages 中看到 chunk 摘要列表（而非单条粗摘要），能按需 recall 检索完整 chunk
- SmartCompressor 成为历史上下文处理的唯一入口：extractExecutionState 逻辑合并进来，截断参数可配置
- TmuxMonitor 检测周期和压缩参数通过 YAML 配置化

**Non-Goals:**
- 不改变框架 ContentRequestProcessor 的消息构建逻辑（正常路径不变）
- 不改变 MemoryStore 的存储接口和因果链机制
- 不实现向量检索/embedding-based RAG（chunk 检索仍通过 EventKey + recall 工具）
- 不改变 ActionTool 的异步执行模型（tmux 仍异步，结果仍通过 InjectMessage 注入）
- 不改变 SessionProjection 的数据结构（仍是 []EventReference）

## Decisions

### Decision 1: 切分策略 — 语义感知 + 启发式规则

**选择**: 基于内容结构的启发式切分，不使用额外 LLM 调用。

**理由**: 使用 LLM 切分会增加每次压缩的延迟和成本。大多数工具输出有可识别的结构：
- HTML/Markdown 文章：按标题（`#`, `<h1>`）切分
- JSON：按顶层 key 切分
- 日志输出：按时间戳或分隔符切分
- 纯文本：按段落（双换行）切分，超过 chunk_size 时在句号/分号处断开

**切分流程**:
```
输入: tool result content (如 curl 返回的 5000 字文章)
  │
  ├── 1. 检测内容类型 (markdown/json/log/plain)
  │
  ├── 2. 按对应策略切分
  │    ├── markdown: 按 # 标题边界
  │    ├── json: 按顶层 key
  │    ├── log: 按 \n + 时间戳模式
  │    └── plain: 按段落 + chunk_size 上限
  │
  ├── 3. 每个 chunk 生成摘要
  │    ├── 截取前 N 字符作为摘要 (N=150, 可配置)
  │    └── 不调用 LLM (避免延迟)
  │
  └── 4. 每个 chunk 持久化到 MemoryStore
       └── 返回 EventKey
```

**替代方案考虑**:
- LLM 语义切分：精度高但延迟和成本不可接受
- 纯字符数机械切分：简单但可能截断语义单元（如截断在标题中间）
- **选择启发式**：在精度和性能之间平衡

**配置参数**:
```yaml
compress:
  chunk_size: 1000        # 单 chunk 最大字符数 (默认 1000)
  chunk_summary_len: 150  # chunk 摘要长度 (默认 150)
  max_tool_result_chars: 500  # extractExecutionState 中单工具结果截断
  max_exec_state_chars: 2000  # 执行状态总截断
```

### Decision 2: 切分块写入 SessionProjection + MemoryStore (方案 Y)

**选择**: 压缩时从 MemoryStore 拉取完整内容，切分后写入新 EventReference 到 SessionProjection，原始内容写入 MemoryStore。正常路径下 LLM 通过框架 ContentRequestProcessor 看到原始消息，不受影响。

**数据流**:
```
压缩触发时:
  oldSegments 中的 tool result (如 EventKey=1234, 5000字)
    │
    ├── 从 MemoryStore.GetEvent(1234) 拉取完整内容
    │
    ├── ChunkSplitter.Split(content) → [chunk_1, chunk_2, chunk_3, chunk_4]
    │
    ├── 每个 chunk:
    │    ├── MemoryStore.StoreEvent(chunk_key, FullEvent{
    │    │     type: "tool_result_chunk",
    │    │     summary: chunk[:150],
    │    │     content: chunk,
    │    │     parent: 1234  ← 因果链关联到原始事件
    │    │   })
    │    └── SessionProjection.Append(EventReference{
    │          key: chunk_key, type: "tool_result_chunk",
    │          summary: chunk[:150]
    │        })
    │
    └── 发给 LLM 的 messages 中替换原始 tool result 为:
         "[压缩] 工具结果已切分为 4 个块:
          - chunk_5678: 文章标题+引言段落...
          - chunk_5679: 正文第一部分...
          - chunk_5680: 正文第二部分...
          - chunk_5681: 结论+参考文献...
          使用 recall 工具检索对应 key 获取完整内容"
```

**替代方案考虑**:
- 方案 X (写入 session.Events)：改变框架内部结构，风险高
- 方案 Z (注入 EventBus)：产生额外事件循环，复杂度高
- **方案 Y**：不侵入框架，只在压缩时操作 tagent 自己的数据结构

### Decision 3: SmartCompressor 作为唯一入口

**选择**: extractExecutionState 从独立函数迁移为 SmartCompressor 的内部方法，截断参数作为 SmartCompressor 字段。

**理由**: 当前 extractExecutionState 在 Compress 方法中调用但参数硬编码为常量。迁移后：
- 截断参数通过 SmartCompressorOption 配置
- 切分逻辑和执行状态提取共享同一套配置
- 压缩策略在一处维护，职责清晰

**迁移内容**:
```
从: agent/smart_compress.go 常量区 + extractExecutionState() 独立函数
到: SmartCompressor struct {
      ...
      maxExecStateChars  int      // 默认 2000
      maxToolResultChars int      // 默认 500
      maxToolArgsChars   int      // 默认 80
      chunkSize          int      // 默认 1000
      chunkSummaryLen    int      // 默认 150
      memStore           memory.MemoryStore  // 切分块持久化
      projection         *SessionProjection  // chunk EventReference 追加
    }
```

### Decision 4: 配置化 — Agent 级别 + Tool 级别

**选择**: 压缩参数在 AgentConfig（agent 级别），TmuxMonitor 参数在 ActionProperties（tool 级别）。

**Agent 级别配置**:
```yaml
agents:
  - name: tagent
    compress:
      max_tool_result_chars: 500
      max_exec_state_chars: 2000
      chunk_size: 1000
      chunk_summary_len: 150
```

**Tool 级别配置**:
```yaml
tools:
  - kind: tool
    id: action
    properties:
      workspace: /tmp/tagent-workspace
      monitor:
        interval: 10s
        stable_duration: 30s
        interactive_stable_duration: 60s
        fake_dead_duration: 120s
```

**理由**: 压缩策略是 agent 的上下文管理行为，应在 agent 级别配置。TmuxMonitor 检测周期是工具的运行时行为，应在 tool 级别配置。不同 agent 可以有不同的压缩策略，不同部署环境可以有不同的检测频率。

## Risks / Trade-offs

- [切分质量不稳定] → 启发式切分可能在不规则内容上效果不佳 → 缓解：chunk 摘要包含前 150 字，LLM 能从摘要判断是否需要 recall；chunk_size 上限保证单个 chunk 不会太大
- [压缩延迟增加] → 切分 + MemoryStore 写入增加压缩时间 → 缓解：切分是纯计算（无 LLM 调用），MemoryStore 写入是顺序 IO；对于 < chunk_size 的输出不切分（快速路径）
- [SessionProjection 增长] → chunk EventReference 增加 projection 长度 → 缓解：Compactor 仍会折叠旧 chunk 引用；chunk 引用比原始完整消息轻量得多
- [配置复杂度] → 新增配置项增加用户认知负担 → 缓解：所有参数有合理默认值，不配置时行为与当前一致
- [方案 Y 的时序问题] → Compactor 重建 messages 时会从包含 chunk 引用的 projection 构建 → 缓解：chunk 引用的 summary 已包含内容摘要，BuildMessages 自然处理
