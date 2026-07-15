## 能力: recall-semantic-tool

为 recall agent 新增 `memory_semantic` 子工具，支持自然语言语义搜索历史事件。

## 需求

### memory_semantic 工具声明

```go
Declaration{
    Name: "memory_semantic",
    Description: "按语义相似度搜索历史事件。输入自然语言查询，返回含义最相关的事件。",
    InputSchema: {
        Type: "object",
        Properties: {
            "query": {Type: "string", Description: "自然语言搜索查询"},
            "top_k": {Type: "integer", Description: "返回结果数量，默认10"},
        },
        Required: ["query"],
    },
}
```

### 执行逻辑

1. 将 `query` 文本通过 EmbeddingProvider 转为向量
2. 调用 MemoryStore.SearchByEmbedding(embedding, topK)
3. 对每个结果，从 MemoryStore.GetEvent(key) 获取 EventSummary
4. 如果 MemoryStore 不支持向量搜索（SupportsVectorSearch() == false），降级为 memory_query 的关键词搜索
5. 返回格式化结果：`[{event_key, event_type, timestamp, summary}]`

### 注册

- 在 `tool/recall/recall_subtools.go` 的 `BuildRecallSubTools` 中注册
- 仅在 EmbeddingProvider 配置可用时注册（否则不暴露此工具）

### 约束

- 单次搜索超时 ≤ 15s（含 embedding 生成 + 向量搜索）
- top_k 最大 50，超过则截断
- 降级时不报错，透明切换为关键词搜索
