## 能力: archive-recall-anchor

L3 压缩归档时提取决策关键词作为检索锚点，使归档事件可被语义搜索精准命中。

## 需求

### archiveSegment 增强

在 `smart_compress.go` 的 `archiveSegment` 方法中，归档事件时：
1. 从被压缩段的消息中提取锚点文本（≤200 字符）
2. 将锚点文本生成 embedding
3. 存入归档事件的 Metadata["anchor_text"] 和向量存储

### 锚点提取规则

优先级从高到低：
1. 段内用户消息的完整文本（拼接，截断到 200 字）
2. 段内 EventSummary 非空的事件的摘要拼接
3. 如果以上都为空，使用 LLM 生成的摘要文本前 200 字

### 向量存储

- 归档事件的 embedding 使用锚点文本生成（而非完整段内容——因为完整内容太长，embedding 质量反而下降）
- 调用 StoreEventWithEmbedding(summaryKey, summaryEvent, anchorEmbedding)

### 约束

- 锚点提取是纯字符串操作，无 LLM 调用
- embedding 生成走异步队列（与正常事件相同路径）
- 无 EmbeddingProvider 时跳过向量存储步骤，只保留 Metadata["anchor_text"]
