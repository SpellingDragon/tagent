# Design: Hybrid Semantic Recall

## Context

- 前身变更 risk-mitigation-semantic-recall(已归档)完成了问题定义与向量方案论证,本设计继承其仍成立的部分并修正过时挂载点。
- 代码现实(2026-09-05 核验):SearchByEmbedding 双 stub;StoreEventWithEmbedding 已存在但忽略 embedding;RustVikingClient 已预留 VectorSearch/Embed 方法与 KVRange(:189);recall 已收敛为统一入口(recall_query/get/recent/trace);关键词召回已有 term-split 增强(2026-08-27);ProjectionOrganizer 已退役(2026-07-24),meditation 是现存的空闲期机制。
- roadmap 调研输入:trpc-agent-go knowledge/vectorstore 有 6 后端与 SearchModeHybrid;rustviking 有 VectorStore/EmbeddingProvider 插件(memory/rocks 后端、mock/openai embedding)。

## Goals / Non-Goals

**Goals:**

- 打通 embedding 生成 → 存储 → 检索 → hybrid 融合召回的完整链路,消灭 SearchByEmbedding stub。
- recall query 模式对"同义改写/跨措辞"场景可召回;两段式(语义发现→票据取回)哲学不变。
- 无配置时零行为变化(优雅降级),配置后成本可控(选择性生成)。

**Non-Goals:**

- 不做完整 IR 系统(BM25 引擎/re-ranker/独立向量数据库)——继承前身变更的辩证结论:万~十万级事件量用轻量向量索引 + 现有关键词即可。
- 不改 recall 工具的 Declaration(声明区恒定是全局不变量)。
- 不做 workspace 代码/文档索引(前身变更 9.2 提及的方向,超出记忆域,留给未来)。
- 首版不做主动 recall hint 的自动化触发(列为可选任务,挂载 meditation,不阻塞主链路)。

## Decisions

### D1 向量存储:内存索引 + RustViking KV 序列化持久化(首版)

| 备选 | 评估 | 结论 |
|---|---|---|
| A. 内存索引 + KV 序列化持久化(启动重建) | 复用既有 RustVikingClient KV 面(零新命令);10 万事件 × 1024 维 × 4B ≈ 400MB 内存——需降维或分片评估 | **首选**,但 D2 维度决策联动 |
| B. rustviking 原生向量插件(vector_store/embedding CLI) | 同库一致性最佳;但命令面/JSON 契约未实测,且 rustviking 需发版配合 | 实测后若成熟则迁移(预留确认项 X1) |
| C. trpc-agent-go knowledge/vectorstore(sqlitevec 等) | 现成 Hybrid 模式;但引入独立存储,与事件库分裂,违背"事实源唯一"原则 | 拒绝(仅当 A/B 均不可行时重议) |

重建策略:启动时经 KVRange 扫描向量键空间异步重建索引;重建完成前 recall 退化为纯关键词(现状行为),不阻塞启动。

### D2 embedder 与维度:zhipu embedding-3,维度取 512(可配)

- 前身变更推荐 1024 维;按 D1-A 的内存估算(400MB@10万事件)下调默认至 512 维(≈200MB),检索精度损失在万级事件下可忽略;维度进配置。
- 端点:openai 兼容 /v1/embeddings(zhipu embedding-3),api_key_env 复用现有 provider 体系;HTTP 客户端 30s 超时,失败重试 1 次后丢弃该事件的向量(关键词路径兜底,向量缺失不报错)。

### D3 EmbeddingWorker:异步旁路,不触碰事件主链路

- 继承前身变更数据流:MemoryPlugin.OnEvent 同步 StoreEvent 后,非阻塞 chan 投递 (key, text);Worker goroutine 批量(≤16 条/批)调 embedder,写向量索引 + KV 持久化。
- 选择性生成:仅 external_input/agent_output 事件类型;textForEmbedding 沿用前身设计(Content ≤8000 字符,回退 EventSummary)。
- chan 满时丢弃并计数(指标),不背压事件循环——向量是增强索引,丢一条只影响该条的语义可召回性。
- Engine/Policy 分离:Worker 属 Engine 侧常驻组件,随 MemoryStore 生命周期启停(Closer 接线)。

### D4 hybrid 融合:RRF,在 recall query 路径内完成

- 融合点:tool/recall 的 query 模式实现内部——向量 topK(≈20)与关键词 topK(≈20)各取后按 RRF(rank 倒数和,k=60)重排,截断至现有 limit。
- 两段式保持:融合结果仍是 EventReference(票据),全文取回走既有 GetEvent 路径——语义检索只回答"recall 之前的那个问题"(前身变更原话,继承)。
- 无向量可用时(未配置/索引重建中/该分区无向量)自动退化为纯关键词,行为与现状一致。

### D5 主动召回 hint(可选,不阻塞):挂载 meditation

- 前身变更阶段 3 挂 ProjectionOrganizer(已退役)——重设计为 meditation 空闲任务:"扫描近期压缩归档的 L3 摘要,对高价值主题做一次语义补召,命中则以 pitfall/insight 类事件沉淀"。
- 列为 tasks 可选组,主链路(D1-D4)验收后再做;若首版不做,记录为后续变更候选。

### D6 配置面

```yaml
memory:
  type: localfile
  path: .wechat-config/data
  embedding:                 # 缺省整段不配置 = 功能关闭,行为与现状一致
    provider: zhipu          # openai 兼容 /v1/embeddings
    model: embedding-3
    api_key_env: ZAI_API_KEY
    dimensions: 512
    event_types: [external_input, agent_output]
```

MemoryConfig 增 EmbeddingConfig 子结构;resolveMemoryStore 按配置决定是否创建 EmbeddingWorker。

### 预留确认项(执行时核对)

| # | 决策点 | 默认降级路径 |
|---|---|---|
| X1 | rustviking 原生向量命令面实测结果(是否有 vector upsert/search CLI 与稳定 JSON 契约) | 不成熟→维持 D1-A(KV 序列化),不阻塞 |
| X2 | 512 维在真实事件分布下的召回质量(需小样本对比 1024) | 质量不足→维度上调至 1024,内存超预算时按分区懒加载 |
| X3 | D5 主动 hint 是否纳入首版 | 不纳入→记录为后续变更候选 |

## Risks / Trade-offs

- [内存占用:向量索引常驻] → 512 维默认 + 分区懒加载(仅活跃分区索引常驻,KVRange 按需补载);指标埋点索引条目数。
- [embedding API 抖动/成本] → 失败丢弃不重试超过 1 次;选择性生成已把量级压到 ~40 次/天;chan 满丢弃计数可观测。
- [启动重建窗口内语义召回缺失] → 优雅退化为关键词(现状),重建异步完成;不引入启动阻塞。
- [RRF 融合改变 query 模式结果排序,既有 real-LLM 测试断言可能受扰] → 融合仅在 embedding 配置开启时生效;CI 默认不开启,行为逐字节不变。
- [前身变更 61 任务的重编遗漏] → 本设计显式继承清单:D3(方案详述)/D4(两段式)/D5(阶段3重设计);specs 四个能力目录中 semantic-search、recall-semantic-tool 由新 specs 覆盖,archive-recall-anchor、proactive-recall-hint 并入 D5 可选项。

## Migration Plan

- 纯增量:未配置 embedding 段时零行为变化;配置后旧事件无向量(仅新事件生成)——提供一次性回填命令(读 KVRange 全量事件 → 批量 embed)作为可选任务。
- 回滚:删除配置段即回到现状;向量 KV 键空间独立前缀,可整段清除。

## Open Questions

- 全部转化为 X1-X3 预留确认项。
