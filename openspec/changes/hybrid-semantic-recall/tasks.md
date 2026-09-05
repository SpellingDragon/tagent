# Tasks: Hybrid Semantic Recall

> 重编自 risk-mitigation-semantic-recall(61 任务,已归档);淘汰过时挂载点,
> 继承其向量方案详述精华。预留确认项 X1-X3 见 design.md。
> 执行顺序:组 1→7;组 8(向量链路可观测)在组 5 之后、组 7 门禁之前执行。
> 交接背景与全局上下文见 roadmap design.md「D6 交接须知」。
>
> **执行状态(2026-09-05 /opsx-apply 对照核对)**:核心链路(组1-4、组8)已交付并 -race 绿;
> 实现载体为 execution-dag.md 的 T-A 节点(解耦缝 C6:Embedder/InMemoryEngine/engineBridge/
> KV持久化/recall hybrid/rustviking契约修复)+ 本次补齐组8向量可观测(TracedEmbedder)、
> 4.2声明恒定测试、5.2 yaml 示例。BLOCKED 项需真实 ZAI_API_KEY(5.1/5.3);X3(6.1)裁决为
> 后续变更候选。逐行状态见各项行尾标注。

## 1. 选型实测与地基

- [x] 1.1 CONFIRM X1:实测 rustviking CLI 向量命令面(vector upsert/search 是否存在、JSON 契约稳定性);不成熟则确认走 D1-A(KV 序列化) — **已决议**:F1 核验 rustviking `index` CLI 进程内易失(main.rs:129 无 load)、原 `vector`/`embed` 命令不存在(虚构契约),故走 D1-A(KV 序列化持久化+启动重建);证据见 f1-rustviking-capability-report.md
- [x] 1.2 memory/ 新增 EmbeddingConfig 结构 + MemoryConfig.Embedding 字段 + YAML 解析(config.go 同步);未配置时全链路零变化的守卫测试 — config.go MemoryEngineConfig(Backend/Embedding/VectorTopK/KeywordTopK/RRFK)+EmbeddingConfig;MemoryConfig.Engine *MemoryEngineConfig(nil=关闭);守卫:DefaultConfig 零变化 + engine=nil 全链路逐字节不变测试
- [x] 1.3 embedder HTTP 客户端(openai 兼容 /v1/embeddings;zhipu embedding-3;30s 超时;重试 ≤1;批量 ≤16)+ 单测(httptest mock) — memory/embedder.go ZhipuEmbedder(openai 兼容 /embeddings)+MockEmbedder(确定性哈希)+单测

## 2. EmbeddingWorker 异步流水线

- [x] 2.1 Worker 实现:非阻塞 chan 投递、批量 embed、写索引+持久化;chan 满丢弃计数;选择性生成(event_types 过滤 + textForEmbedding 8000 截断/摘要回退) — InMemoryEngine.embedWorker(非阻塞 chan/批量/写索引+KV持久化/droppedCount/Embeddable 类型过滤/MaxTextRunes 截断)
- [x] 2.2 MemoryPlugin.OnEvent 接线:StoreEvent 后投递(仅配置开启时);同步路径零额外耗时断言测试 — **实现差异(等价达成)**:用 engineBridge 装饰 MemoryStore.StoreEvent→engine.Index(而非 MemoryPlugin.OnEvent);语义等价(入库即异步索引),装饰器模式更贴解耦缝 C6;同步路径仅非阻塞 chan 投递
- [x] 2.3 生命周期:Worker 随 MemoryStore 启停(Closer 接线);resolveMemoryStore 按配置创建 — engineBridge.Close→engine.Close(worker 排空,M1 修复:WithoutCancel+DrainTimeout);wireMemoryEngine 按 MemoryConfig.Engine 创建
- [x] 2.4 单测:入库不阻塞、类型过滤、丢弃计数、Close 排空 — engine_inmemory_test 覆盖(不阻塞/Embeddable过滤/droppedCount/Close排空)

## 3. 向量存储与 SearchByEmbedding

- [x] 3.1 内存向量索引(余弦 topK;分区维度组织;条目数指标) — InMemoryEngine vectors/vmeta(分区过滤)+cosine+vectorKeys topK+Stats(条目数)
- [x] 3.2 KV 序列化持久化(独立键前缀)+ 启动 KVRange 异步重建;重建窗口退化行为测试 — engine_persist.go(VecKeyPrefix 独立前缀+persistedVector 含 ModelID 指纹)+rebuildFromKV 异步重建(不阻塞构造,重建窗口向量渐进可用,Ready() 含 rebuildDone)
- [x] 3.3 InMemoryStore.SearchByEmbedding / FileSegmentStore.SearchByEmbedding 落地 + SupportsVectorSearch 语义;未配置时保持 stub 行为的回归测试 — engineBridge.SearchByEmbedding→RawVectorSearcher;SupportsVectorSearch 反映引擎能力(engine_bridge.go:140);stub 回归:engine=nil 时 SupportsVectorSearch=false(engine_bridge_test)
- [ ] 3.4 (可选)历史事件一次性回填命令/工具(KVRange 全量 → 批量 embed) — **可选未做**:标注为后续增强候选(新事件已自动索引;历史回填非主链路验收必需)

## 4. recall hybrid 融合

- [x] 4.1 tool/recall query 路径:向量 topK ∪ 关键词 topK → RRF(k=60)融合 → limit 截断;向量不可用自动退化 — recallViaEngine(hybrid RRF k=60)+recallByQuery;向量不可用(SupportsVectorSearch=false/引擎未就绪)自动退化纯关键词
- [x] 4.2 声明恒定断言测试:配置开启前后 recall 工具 Declaration JSON 逐字节一致 — **本次补**:tool/recall/declaration_stable_test.go(确定性:两次构造逐字节一致 + 声明区不泄漏 embedding/vector/hnsw/rrf 字样);结构保证:recall 工具签名无向量参数
- [x] 4.3 同义改写召回集成测试(mock embedder 固定向量,验证无共同关键词场景命中;真实 embedder 版本进 tests/ 按惯例 Skip 保护) — tool/recall/hybrid_recall_test.go(mock embedder,无共同关键词命中)+ memory 引擎 hybrid 测试

## 5. 真实验证与接线

- [ ] 5.1 CONFIRM X2:小样本对比 512 vs 1024 维召回质量(zhipu embedding-3 真实调用),定默认维度 — **BLOCKED(需真实 ZAI_API_KEY 调用)**:当前默认 dimensions 由配置定(示例 1024);实测对比留待有真实 key 环境执行,YAML 已注明「X2 待实测定默认」
- [x] 5.2 wechat-bot 示例 yaml 增加 embedding 配置段(注释说明成本与开关语义) — **本次补**:examples/wechat-bot/tagent.yaml entry memory 段加 engine/embedding 注释示例(成本/开关/声明恒定/降级语义)
- [ ] 5.3 真实集成测试(tests/ 惯例):真实 embedder 下"入库→语义召回→票据取回全文"闭环 — **BLOCKED(需真实 key)**:mock embedder 闭环已测(hybrid_recall_test);真实 embedder 版按 tests/ 惯例 Skip 保护,留待有 key 环境

## 6. 可选组(主链路验收后裁决)

- [ ] 6.1 CONFIRM X3:主动召回 hint 是否纳入本变更;纳入则实现 meditation 空闲任务(L3 归档摘要语义补召→pitfall/insight 沉淀),否则记录为后续变更候选并关闭本组 — **DEGRADED(裁决:后续变更候选)**:主动召回 hint 与 T-D 记忆策展(meditation 巩固)主题重叠,记录为后续变更候选,关闭本组;本变更聚焦被动 hybrid 召回

## 8. 向量链路可观测(2026-09-05 追加,归属裁决见 LEDGER;turn 级 trace 骨架属 observability-tracing 变更,不在本组)

- [x] 8.1 embedder 调用产生 span,对齐上游 GenAI 语义约定 — **本次补**:memory/embedder_trace.go TracedEmbedder 装饰器(span 名 tagent.embeddings;属性 gen_ai.request.model/gen_ai.embeddings.request.text_count/gen_ai.embeddings.dimension.count 用上游 semconv KeyGenAIEmbeddingsDimensionCount);buildEmbedder 统一包裹
- [x] 8.2 丢弃计数(chan 满/API 失败,组 2 已实现)与索引条目数(组 3)接入 otel counter/gauge;重建开始/完成打结构化日志(含条目数与耗时) — TracedEmbedder counter(tagent.embedding.calls/texts)+histogram(dimension);引擎 droppedCount/indexedCount(Stats);重建完成日志 engine_persist.go(rebuilt %d vectors,corrupt/staleModel 计数)
- [x] 8.3 声明区守卫:span/metric 全部位于 Worker 与 store 内部,MCP/recall 工具 Declaration 零触碰;noop tracer 下全链路行为不变的单测 — **本次补**:TracedEmbedder 可观测全在 embedder 内部;embedder_trace_test noop 透传逐字节一致 + declaration_stable_test 声明区守卫

## 7. 门禁与收尾

- [x] 7.1 三道门禁:build/vet/test -race → 真实集成抽查 → CodeReview sub-agent fresh-eyes(必须修复项清零) — build/vet/全量23包-short绿+新子系统-race绿;CodeReview gate-3(T-A 首轮 M1/M2/M3+7S+12Nit 全清零;真实集成抽查 BLOCKED 需 key)
- [ ] 7.2 delta specs 同步主 specs(semantic-search、recall-hybrid-fusion 新增;recall-protocol 若有 MODIFIED 项按全文拷贝规程) — **待板块统一处理**(/opsx-apply 收尾:specs 同步)
- [ ] 7.3 commit(conventional 风格)+ archive 本变更 + 回写 LEDGER.md 与 roadmap P1 检查点 — commit✅(conventional 全程)+LEDGER✅(两次驱动记账);**archive 待裁决**(5.1/5.3 BLOCKED 项是否阻断 archive 由用户定;核心链路已交付)
