# Tasks: Hybrid Semantic Recall

> 重编自 risk-mitigation-semantic-recall(61 任务,已归档);淘汰过时挂载点,
> 继承其向量方案详述精华。预留确认项 X1-X3 见 design.md。
> 执行顺序:组 1→7;组 8(向量链路可观测)在组 5 之后、组 7 门禁之前执行。
> 交接背景与全局上下文见 roadmap design.md「D6 交接须知」。

## 1. 选型实测与地基

- [ ] 1.1 CONFIRM X1:实测 rustviking CLI 向量命令面(vector upsert/search 是否存在、JSON 契约稳定性);不成熟则确认走 D1-A(KV 序列化)
- [ ] 1.2 memory/ 新增 EmbeddingConfig 结构 + MemoryConfig.Embedding 字段 + YAML 解析(config.go 同步);未配置时全链路零变化的守卫测试
- [ ] 1.3 embedder HTTP 客户端(openai 兼容 /v1/embeddings;zhipu embedding-3;30s 超时;重试 ≤1;批量 ≤16)+ 单测(httptest mock)

## 2. EmbeddingWorker 异步流水线

- [ ] 2.1 Worker 实现:非阻塞 chan 投递、批量 embed、写索引+持久化;chan 满丢弃计数;选择性生成(event_types 过滤 + textForEmbedding 8000 截断/摘要回退)
- [ ] 2.2 MemoryPlugin.OnEvent 接线:StoreEvent 后投递(仅配置开启时);同步路径零额外耗时断言测试
- [ ] 2.3 生命周期:Worker 随 MemoryStore 启停(Closer 接线);resolveMemoryStore 按配置创建
- [ ] 2.4 单测:入库不阻塞、类型过滤、丢弃计数、Close 排空

## 3. 向量存储与 SearchByEmbedding

- [ ] 3.1 内存向量索引(余弦 topK;分区维度组织;条目数指标)
- [ ] 3.2 KV 序列化持久化(独立键前缀)+ 启动 KVRange 异步重建;重建窗口退化行为测试
- [ ] 3.3 InMemoryStore.SearchByEmbedding / FileSegmentStore.SearchByEmbedding 落地 + SupportsVectorSearch 语义;未配置时保持 stub 行为的回归测试
- [ ] 3.4 (可选)历史事件一次性回填命令/工具(KVRange 全量 → 批量 embed)

## 4. recall hybrid 融合

- [ ] 4.1 tool/recall query 路径:向量 topK ∪ 关键词 topK → RRF(k=60)融合 → limit 截断;向量不可用自动退化
- [ ] 4.2 声明恒定断言测试:配置开启前后 recall 工具 Declaration JSON 逐字节一致
- [ ] 4.3 同义改写召回集成测试(mock embedder 固定向量,验证无共同关键词场景命中;真实 embedder 版本进 tests/ 按惯例 Skip 保护)

## 5. 真实验证与接线

- [ ] 5.1 CONFIRM X2:小样本对比 512 vs 1024 维召回质量(zhipu embedding-3 真实调用),定默认维度
- [ ] 5.2 wechat-bot 示例 yaml 增加 embedding 配置段(注释说明成本与开关语义)
- [ ] 5.3 真实集成测试(tests/ 惯例):真实 embedder 下"入库→语义召回→票据取回全文"闭环

## 6. 可选组(主链路验收后裁决)

- [ ] 6.1 CONFIRM X3:主动召回 hint 是否纳入本变更;纳入则实现 meditation 空闲任务(L3 归档摘要语义补召→pitfall/insight 沉淀),否则记录为后续变更候选并关闭本组

## 8. 向量链路可观测(2026-09-05 追加,归属裁决见 LEDGER;turn 级 trace 骨架属 observability-tracing 变更,不在本组)

- [ ] 8.1 embedder 调用产生 span,对齐上游 GenAI 语义约定(trpc-agent-go telemetry/semconv/trace/embedding.go:gen_ai.embeddings.dimension.count 等属性;span 名遵循上游 internal/telemetry/trace_embedding.go 惯例——实现前先读该文件核对命名与属性集)
- [ ] 8.2 丢弃计数(chan 满/API 失败,组 2 已实现)与索引条目数(组 3)接入 otel counter/gauge(未设 OTEL_EXPORTER_OTLP_ENDPOINT 时 noop 零开销,与 telemetrytrace.Start 现状语义一致);重建开始/完成打结构化日志(含条目数与耗时)
- [ ] 8.3 声明区守卫:span/metric 全部位于 Worker 与 store 内部,MCP/recall 工具 Declaration 零触碰;noop tracer 下全链路行为不变的单测

## 7. 门禁与收尾

- [ ] 7.1 三道门禁:build/vet/test -race → 真实集成抽查 → CodeReview sub-agent fresh-eyes(必须修复项清零)
- [ ] 7.2 delta specs 同步主 specs(semantic-search、recall-hybrid-fusion 新增;recall-protocol 若有 MODIFIED 项按全文拷贝规程)
- [ ] 7.3 commit(conventional 风格)+ archive 本变更 + 回写 LEDGER.md 与 roadmap P1 检查点
