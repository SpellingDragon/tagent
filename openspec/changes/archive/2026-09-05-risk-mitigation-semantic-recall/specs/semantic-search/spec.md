## 能力: semantic-search

替换 MemoryStore 中 SearchByEmbedding stub，实现真实的向量语义检索。

## 需求

### EmbeddingProvider 接口

```go
type EmbeddingProvider interface {
    Embed(ctx context.Context, text string) ([]float32, error)
    Dimensions() int
}
```

- 支持 OpenAI-compatible API（`/v1/embeddings`）
- 配置：model name、endpoint、api_key_env
- 超时：单次调用 ≤ 10s

### SearchByEmbedding 实现

- 输入：query embedding ([]float32) + topK (int)
- 输出：[]EventReference（按余弦相似度降序）
- 实现：brute-force 遍历所有有 embedding 的事件，计算余弦相似度
- 跳过无 embedding 的事件（不报错）

### StoreEventWithEmbedding 实现

- 输入：EventKey + FullEvent + embedding
- 行为：存储事件同时存储其向量表示
- 向量存储位置：InMemoryStore 内部的 `embeddings map[int64][]float32`

### 异步 Embedding 生成

- MemoryPlugin.OnEvent 存储事件后，将 (EventKey, Content) 推入异步队列
- 独立 goroutine 消费队列，调用 EmbeddingProvider.Embed，写入 StoreEventWithEmbedding
- 仅对 `external_input` 和 `agent_output` 类型事件生成 embedding
- 队列满或 API 失败时跳过（日志记录），不影响事件正常入库

### 约束

- 不引入外部向量库依赖
- 不修改 MemoryStore 接口签名
- embedding API 不可用时整个语义搜索功能降级（返回空结果），不影响其他功能
- SupportsVectorSearch() 在配置了 EmbeddingProvider 后返回 true
