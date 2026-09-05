## Why

基于独立风险评估报告（`synthesis_tagent_risk.md`）的结论，tagent 框架当前面临两个互相叠加的高优先级风险：

1. **语义检索缺失（风险A）**：`SearchByEmbedding` 为 stub，底层检索仅支持关键词子串匹配 + 时间范围 + 事件类型。长程记忆中"表达不同但含义相同"的事件无法被召回，直接限制了 recall 工具的天花板。
2. **压缩后可靠召回依赖 LLM 主动性（风险B）**：L3 归档后信息虽在 MemoryStore，但取回需要 Agent 主动调用 recall。在实测中已观察到长程检索精度退化——recall 返回旧片段而非高相关事件。

两个风险叠加形成文章02所述的"级联失效"：压缩老化 → 关键信息离开上下文 → 召回不足 → 决策基于不完整信息 → 输出质量退化。

### 辩证评估

**报告中正确的部分**：
- 语义检索确实为 stub，这是客观事实
- L3 压缩 5x 比率下细节抹平是真实风险
- "取回依赖 LLM 主动"是设计缺陷而非功能缺失

**需要辩证修正的部分**：
- 报告建议"引入向量库 + BM25 + re-ranker 混合架构"——**过度设计**。tagent 的事件量级（万~十万级）不需要完整的 IR 系统。轻量级向量索引 + 现有关键词检索已能满足需求
- 报告建议"L3 归档后自动 recall 兜底"——方向正确但实现需审慎。不应在每次 BeforeModel 时触发 recall（太贵），而应在**空闲整理阶段**（ProjectionOrganizer）判断是否需要从归档中恢复关键信息
- 报告中的"代码级熔断"建议有价值，但应渐进实现（先连续失败计数 → 再考虑 schema 校验）

### 本变更的定位

聚焦 **P0 + P1 的核心子集**：为 tagent 补上语义检索能力，并让压缩后的信息能被**可靠**召回。不做完整的 IR 系统，但要打通从 embedding 生成 → 存储 → 检索 → 注入上下文的完整链路。

## What Changes

### Phase 1: 语义检索基础能力

- **接入 embedding 模型**：在 `memory/` 层新增 `EmbeddingProvider` 接口，支持将事件文本转为向量。初期实现对接 OpenAI-compatible embedding API（如 `text-embedding-3-small`）
- **实现真实 `SearchByEmbedding`**：替换 `InMemoryStore` 和 `FileSegmentStore` 中的 stub，使用余弦相似度 + brute-force 搜索（万级事件量不需要 HNSW）
- **事件入库时自动生成 embedding**：在 `MemoryPlugin.OnEvent` 中，对 `external_input` 和 `agent_output` 类型事件自动调用 embedding 生成并存储

### Phase 2: recall 工具增强

- **新增 `memory_semantic` 子工具**：对 recall agent 新增语义搜索能力，接受自然语言查询，返回语义最相关的事件
- **混合检索策略**：当 recall agent 执行 `memory_query` 时，同时做关键词匹配和语义搜索，合并结果（不需要独立 re-ranker，recall agent 的 LLM 本身就是 re-ranker）

### Phase 3: 压缩后可靠召回

- **L3 归档时记录"决策关键词"**：压缩段被归档时，提取段中的关键实体/决策点作为检索锚点（存入归档事件的 Metadata）
- **ProjectionOrganizer 增强**：空闲整理时，检查是否有归档事件与当前 Projection 中的活跃上下文高度相关。如果有，将归档摘要 key 注入一条系统提示（"以下归档信息可能与当前任务相关：recall key=X"）
- **连续失败计数**：在主循环中增加简单的连续工具失败计数器，连续 3 次同类失败时注入 `[warning]` 提示而非依赖 LLM 自行判断

## Capabilities

### New Capabilities

- `semantic-search`: 真实的向量检索能力，替代 SearchByEmbedding stub，支持 embedding 生成 + 存储 + 余弦相似度搜索
- `recall-semantic-tool`: recall agent 的语义搜索子工具 `memory_semantic`，支持自然语言查询历史事件
- `archive-recall-anchor`: L3 归档时提取决策关键词作为检索锚点，使归档事件可被语义搜索命中
- `proactive-recall-hint`: ProjectionOrganizer 在空闲时检测归档事件与活跃上下文的相关性，主动注入 recall 提示

### Modified Capabilities

- `value-driven-compression`: L3 archiveSegment 增加关键词提取，存入归档事件 Metadata
- `projection-organize`: 增加归档相关性检测 + recall hint 注入逻辑

## Impact

### 代码变更

- **新增文件**：`memory/embedding.go`（EmbeddingProvider 接口 + OpenAI 实现）、`memory/vector_index.go`（brute-force 向量索引）、`tool/recall/semantic_tool.go`（语义搜索子工具）
- **修改文件**：`memory/in_memory_store.go`（实现真实 SearchByEmbedding）、`memory/segment_store.go`（同上）、`plugin/memory_plugin.go`（事件入库时生成 embedding）、`agent/smart_compress.go`（archiveSegment 提取关键词）、`agent/projection_organizer.go`（归档相关性检测）、`tool/recall/recall_subtools.go`（注册 memory_semantic）
- **配置变更**：`config.go` 新增 `EmbeddingConfig`（model、provider、api_key_env）

### 风险

- **embedding API 延迟**：每个事件入库增加一次 API 调用。缓解：异步生成，事件先入库再补 embedding，搜索时跳过无 embedding 的事件
- **向量索引内存占用**：1000 维 × 10 万事件 ≈ 400MB。缓解：初期只对 `external_input` 和 `agent_output` 生成 embedding（过滤 tool 结果和 thinking_plan），且支持按时间窗淘汰旧向量
- **brute-force 搜索性能**：10 万事件余弦计算 ~10ms（Go 单线程），可接受。超 100 万时需切换为 HNSW

### 依赖

- 需要 embedding 模型 API（OpenAI-compatible，如 ZhiPu 的 embedding-3）
- 不引入外部向量库依赖（初期 brute-force 足够）
