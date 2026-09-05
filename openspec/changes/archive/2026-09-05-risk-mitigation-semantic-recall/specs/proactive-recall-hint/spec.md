## 能力: proactive-recall-hint

ProjectionOrganizer 在空闲期检测归档事件与活跃上下文的相关性，主动注入 recall 提示。

## 需求

### 相关性检测逻辑

在 ProjectionOrganizer.OrganizeOnce 中，新增"归档相关性检测"步骤：

1. 查询所有 `context_compress_summary` 类型的归档事件（有 embedding 的）
2. 取当前 Projection 最近 3 个 refs 的 EventSummary 拼接为"当前上下文摘要"
3. 将"当前上下文摘要"转为 embedding
4. 对每个归档事件的 embedding 计算余弦相似度
5. 如果最高相似度 > 阈值（默认 0.75）且该归档 key 未在当前 Projection 中出现 → 注入 hint

### Hint 注入方式

通过 `projection.Append(hintRef)` 注入一条特殊的 EventReference：
```go
EventReference{
    EventKey:     archiveKey, // 负数，表示归档引用
    EventType:    "recall_hint",
    EventSummary: fmt.Sprintf("[recall_hint] 以下归档信息可能与当前任务相关: key=%d, 摘要: %s", archiveKey, archiveSummary[:100]),
    Timestamp:    time.Now().UnixMilli(),
}
```

ContextCompressor.resolveRef 对 `recall_hint` 类型特殊处理：直接使用 EventSummary 作为 system message 内容。

### 频率控制

- 每轮整理最多检测 10 个归档事件
- 每轮最多注入 1 条 hint（选择相似度最高的）
- 同一 archiveKey 在 24 小时内不重复注入（通过内存 set 记录已注入的 key + 时间）
- 无 EmbeddingProvider 时跳过此步骤

### 连续失败计数（附加能力）

在 `agent/context_manager.go` 的 RunFlow 方法中：
- 新增 `failCounts map[string]int` 字段（工具名 → 连续失败次数）
- 每次工具调用返回结果后，检查结果内容是否含 "error"/"Error"/"failed"
- 含错误：`failCounts[toolName]++`；不含：`failCounts[toolName] = 0`
- 当 `failCounts[toolName] >= 3` 时，在下次 BeforeModel 注入 warning 消息

### 约束

- hint 注入不触发新的 RunFlow（它只是 Projection 中的一个 ref）
- 连续失败计数在 RunFlow 结束时不重置（跨轮累计）
- 不依赖 LLM 判断是否注入——纯代码逻辑
