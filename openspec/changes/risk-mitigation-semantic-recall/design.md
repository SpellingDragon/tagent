## Context

tagent 当前的记忆检索仅支持结构化查询（事件类型 + 时间范围 + 关键词子串），无法按语义相似度检索"表达不同但含义相同"的历史事件。`SearchByEmbedding` 在 InMemoryStore 和 FileSegmentStore 中均为 stub。

压缩后的信息虽然通过 L3 归档存入 MemoryStore，但取回完全依赖 Agent 主动调用 recall 工具——这是一个"希望 LLM 记得去做某事"的软约束，在长程多轮对话中存在可靠性退化。

本设计在保持 tagent 轻量级原则下，以最小代码量打通语义检索闭环并增强压缩后的信息可达性。

## Goals / Non-Goals

**Goals:**

- 为 MemoryStore 实现真实的向量检索能力（替换 stub）
- 事件入库时异步生成 embedding（不阻塞事件循环）
- recall agent 获得 `memory_semantic` 子工具（自然语言查询历史事件）
- 压缩归档时提取关键词锚点，使归档事件可被语义搜索命中
- ProjectionOrganizer 在空闲期检测归档-上下文相关性，主动注入 recall hint
- 增加简单的连续失败计数熔断（非 prompt 软约束）

**Non-Goals:**

- 不引入外部向量库（Milvus/Pinecone/Qdrant 等）
- 不实现 HNSW/IVF 等近似索引（brute-force 足够 10 万级事件）
- 不实现独立的 re-ranker（recall agent 的 LLM 充当 re-ranker）
- 不修改 MemoryStore 接口签名（接口已预留 embedding 方法）
- 不修改事件类型系统或 EventKey 格式

## Decisions

### D1: Embedding 生成异步化

事件入库（MemoryPlugin.OnEvent）是热路径，不能被 embedding API 延迟阻塞。

方案：MemoryPlugin.OnEvent 正常写入事件后，将 EventKey 投入一个异步队列。独立 goroutine 消费队列、调用 embedding API、写入向量存储。搜索时跳过无 embedding 的事件（新事件有短暂的"不可语义搜索"窗口，通常 < 1s）。

### D2: 向量存储与事件存储同构

不引入独立的向量数据库。向量直接存入 InMemoryStore/FileSegmentStore 的事件结构中（FullEvent 已有 Embedding 字段预留位）。搜索时从内存加载所有向量做 brute-force 余弦计算。

10 万事件 × 1024 维 × 4 bytes = ~400MB，在 tagent 的单进程长运行场景下可接受。

### D3: recall 混合检索策略

`memory_semantic` 子工具：
1. 将用户查询文本转为 embedding
2. 调 SearchByEmbedding 取 topK=20 结果
3. 与 memory_query 的关键词结果合并去重
4. 返回 LLM（recall agent 自身作为 re-ranker 选择最相关的）

### D4: 归档锚点与主动 recall hint

L3 archiveSegment 时，从被压缩段中提取关键实体（用简单启发式：提取带 `[evt_` 前缀的 key、出现频率最高的名词短语前5个，或直接用 EventSummary 的前 100 字符作为锚点文本）。锚点文本生成 embedding 存入归档事件。

ProjectionOrganizer 空闲时：
1. 取归档事件的 embedding
2. 与当前 Projection 最近 N 个 refs 的 embedding 计算相似度
3. 如果相似度 > 阈值（0.7），注入 hint 消息到 Projection

### D5: 连续失败计数

在 ContextManager.RunFlow 中：
- 记录工具调用的连续失败次数（按工具名分组）
- 连续 3 次同名工具失败 → 注入 `[warning] 工具 X 连续失败 3 次，建议更换策略` 到下次 BeforeModel
- 成功则重置计数

## Risks / Trade-offs

- **[R1] Embedding API 可用性**：如果 embedding API 不可用，事件仍正常入库但无法被语义搜索。缓解：搜索时自动降级为关键词搜索。
- **[R2] 内存占用增长**：向量索引全量加载内存。缓解：只对 external_input/agent_output 生成 embedding；超过阈值（如 50 万事件）时可按时间窗淘汰旧向量。
- **[R3] Recall hint 噪音**：如果阈值设置不当，可能注入大量低相关 hint。缓解：每轮整理最多 1 条 hint；相似度阈值默认 0.75（较保守）。
