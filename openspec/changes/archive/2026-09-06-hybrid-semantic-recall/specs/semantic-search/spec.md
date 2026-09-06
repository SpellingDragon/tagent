# semantic-search Specification (Delta)

## ADDED Requirements

### Requirement: 异步向量生成流水线
系统 SHALL 提供 EmbeddingWorker:事件经 MemoryPlugin 入库后以非阻塞方式投递 (EventKey, text),Worker 异步批量(≤16/批)调用 embedder 生成向量并写入索引与持久层。投递 MUST NOT 阻塞事件循环;队列满时 SHALL 丢弃并计数,不背压。向量生成 SHALL 仅覆盖配置声明的事件类型(默认 external_input/agent_output),文本取 Content(≤8000 字符截断,回退 EventSummary)。

#### Scenario: 事件入库不等待向量
- **WHEN** 开启 embedding 配置且事件入库时 embedder 响应缓慢(>1s)
- **THEN** StoreEvent 同步路径耗时不受影响,向量在事件可查之后异步就绪

#### Scenario: 非配置类型不生成向量
- **WHEN** thinking_plan/action_command 类型事件入库
- **THEN** 不产生 embedding 调用

#### Scenario: 队列满丢弃可观测
- **WHEN** 投递队列满
- **THEN** 该事件向量被丢弃,丢弃计数递增,事件本身正常入库

### Requirement: 向量持久化与启动重建
向量 SHALL 以独立键空间前缀持久化到既有 RustViking KV(localfile/file 存储);进程启动时 SHALL 经 KVRange 扫描异步重建内存索引。重建完成前,语义检索 SHALL 返回空结果集(触发 recall 的关键词退化路径),MUST NOT 阻塞启动或报错。

#### Scenario: 重启后语义召回恢复
- **WHEN** 进程重启且索引重建完成后执行语义查询
- **THEN** 重启前入库事件的向量可被检索

#### Scenario: 重建窗口内优雅退化
- **WHEN** 索引重建尚未完成时 recall query 到达
- **THEN** 返回纯关键词结果,无错误

### Requirement: SearchByEmbedding 落地
`InMemoryStore` 与 `FileSegmentStore` 的 `SearchByEmbedding(query, topK)` SHALL 从 stub(ErrVectorSearchNotSupported)变为可用实现:余弦相似度 topK,返回 EventReference 列表(票据形态);`SupportsVectorSearch()` SHALL 在 embedding 配置开启且索引可用时返回 true。未配置 embedding 时 SHALL 保持现有 stub 行为(错误返回),确保零行为变化。

#### Scenario: 配置开启后 stub 消失
- **WHEN** embedding 配置开启且索引就绪,调用 SearchByEmbedding
- **THEN** 返回按相似度排序的 EventReference 列表而非 ErrVectorSearchNotSupported

#### Scenario: 未配置时行为与现状一致
- **WHEN** 未配置 embedding 段
- **THEN** SearchByEmbedding 返回 ErrVectorSearchNotSupported,SupportsVectorSearch 为 false

### Requirement: embedder 配置与降级
embedder SHALL 经 MemoryConfig 的 embedding 段声明(provider/model/api_key_env/dimensions/event_types);API 调用失败 SHALL 重试至多 1 次后放弃该事件向量(不报错、不中断);api_key_env 缺失时 SHALL 视同未配置(功能关闭)并记录一次警告。

#### Scenario: embedder 故障不影响主链路
- **WHEN** embedding API 持续超时
- **THEN** 事件入库与关键词召回完全正常,仅向量缺失且有丢弃计数

### Requirement: 向量链路可观测
EmbeddingWorker 的批量 embed 调用 SHALL 产生符合上游 GenAI 语义约定的 span(含 gen_ai.embeddings.dimension.count 属性);丢弃计数与索引条目数 SHALL 以 otel metric 暴露;未配置 OTLP 导出时 MUST 为 noop(零开销)。span/metric MUST 全部位于 Worker/store 内部,不触碰任何工具 Declaration。

#### Scenario: OTLP 启用时向量链路可见
- **WHEN** 设置 OTEL_EXPORTER_OTLP_ENDPOINT 且有事件触发向量生成
- **THEN** 导出的 trace 含 embedder 批处理 span 与维度属性

#### Scenario: 未启用导出时零开销
- **WHEN** 未设置 OTLP endpoint
- **THEN** tracer/meter 为 noop,向量链路行为与无观测时一致
