# Hybrid Semantic Recall

> 本变更是 risk-mitigation-semantic-recall(2026-07-15,0/61,已归档)的重编版:
> 保留其仍然成立的问题定义与向量方案精华,淘汰已被架构演进淘汰的部分
> (ProjectionOrganizer 挂载点、独立语义工具形态),并按 2026-09 的代码现实重编任务。
> 裁决依据见 openspec/changes/LEDGER.md。
>
> ⚠️ 并存设计(开工前必读):docs/.dev/tagent下一步迭代设计报告.md 的 **D2 记忆与召回增强**是同主题更详细方案(VectorStore 装饰器 + 异步嵌入队列 + RRF + VectorDelete 三层对策 + 风险 R1/R2),与本变更存储选型(内存索引 + RustViking KV 序列化)存在分歧。两者并存作参考、未定权威(见 roadmap design D6「并存规划」);apply 前 MUST 先对照 D2 讨论调和存储方案,避免返工。

## Why

`SearchByEmbedding` 至今为 stub(memory/in_memory_store.go:306、memory/segment_store.go:762 返回 ErrVectorSearchNotSupported),召回只有分词关键词 + 时间范围 + 票据路径——"表达不同但含义相同"的事件无法召回,长程记忆的检索天花板被锁死。这是两篇外部评审、旧变更风险评估与 roadmap P1 三方一致确认的仍存在问题。同时,2026-08-27 的关键词召回增强(term-split matching + first-class time-range)已落地,为"向量 ∪ 关键词 → RRF 融合"的 hybrid 形态提供了现成的另一半。

## What Changes

- 新增 EmbeddingWorker:事件入库后异步生成向量(非阻塞 chan 投递,选择性生成——仅 external_input/agent_output,文本 8000 字符截断),承接旧变更已论证的数据流设计。
- 向量存储与检索:首版走"内存向量索引 + 序列化持久化到既有 RustViking KV(启动重建)"路径,零新增外部依赖;rustviking 原生向量插件命令面实测后决定是否迁移(预留确认项)。
- `SearchByEmbedding` 从 stub 落地为可用实现(InMemoryStore 与 FileSegmentStore 双实现)。
- recall 统一入口的 query 模式内部升级为 hybrid:向量召回 ∪ 分词关键词召回 → RRF 融合 → 票据精确取回(两段式不变;recall 工具声明零变化,prefix-cache 不受影响)。
- 主动召回 hint(旧变更阶段 3)重设计:挂载点从已退役的 ProjectionOrganizer 改为 meditation 空闲期("压缩老化事件的语义补召"作为冥想任务之一);首版可选项,不阻塞主链路。
- embedder:zhipu embedding-3(openai 兼容 /v1/embeddings,key 复用现有 provider 体系);经配置声明,缺省关闭(无 key/未配置时 recall 行为与现状完全一致,优雅降级)。
- 向量链路可观测(2026-09-05 追加):embedder span 对齐上游 GenAI 语义约定、丢弃计数/索引条目数接 otel metric(noop 默认零开销)。turn 级 trace 骨架与轨迹互链属独立变更 observability-tracing(归属裁决见 openspec/changes/LEDGER.md)。

## Capabilities

### New Capabilities

- `semantic-search`: 事件向量的生成、存储、检索——EmbeddingWorker 异步流水线、向量持久化与启动重建、SearchByEmbedding 双实现、embedder 配置与降级。
- `recall-hybrid-fusion`: recall query 模式的向量∪关键词 RRF 融合——融合排序、两段式(语义发现→票据精确取回)保持、工具声明恒定不变。

### Modified Capabilities

<!-- recall-protocol(openspec/specs/recall-protocol/)的 query 模式行为增强:内部检索从纯关键词升级为 hybrid 融合;工具入口/参数/声明不变,仅结果质量语义变化。delta spec 随本变更 specs/ 提交 -->

## Impact

- 代码:memory/(EmbeddingWorker、向量索引、两个 store 的 SearchByEmbedding)、tool/recall/(query 路径融合)、config(MemoryConfig 增 embedding 配置段)、agent/meditation(可选 hint 任务)。
- 依赖:零新增外部服务(embedder 走 HTTP API;向量存 RustViking KV/内存)。
- 不变量:recall 工具声明恒定(prefix-cache);FullEvent 不可变(向量是旁路索引不是事件字段变更——StoreEventWithEmbedding 语义为"附加索引");无 embedder 配置时行为与现状逐字节一致。
- 成本:选择性生成下约 40 次 embedding API 调用/天(100 events/日场景,旧变更已估算)。
