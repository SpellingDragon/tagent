## 1. Core Data Structures

- [x] 1.1 新建 `agent/event_value.go`
  - 定义 `ProcessingStrategy` 为 string 类型，常量：`Keep`、`Truncate`、`KeyFacts`、`Summary`、`Reference`、`Drop`
  - 定义 `EventValue` struct，字段：`EventKey int64`、`ValueScore float64`、`Processing ProcessingStrategy`、`KeyFacts string`、`Reason string`
  - 定义 `EventValuator` 接口：`Evaluate(ctx context.Context, segments []*TaskSegment) ([]EventValue, string, error)`，第三个返回值为该 batch 的整体摘要
  - 定义 `ValuationConfig` struct：`ValueFloors map[string]float64`、`PromptVersion string`、`Timeout time.Duration`

- [x] 1.2 修改 `agent/tagent_agent.go` 中的 `CompressConfig`
  - 新增字段：`ValueFloors map[string]float64`、`ValuationTimeoutMs int`、`SummaryPromptVersion string`
  - 删除与旧规则相关的冗余字段（如后续确认不再需要）

- [x] 1.3 修改 `agent/smart_compress.go` 中的 `SmartCompressor` struct
  - 新增字段：`eventValuator EventValuator`、`archiveCache map[string]archiveCacheEntry`、`valuationConfig ValuationConfig`
  - 新增 `archiveCacheEntry` struct：`summaryKey int64`、`summary string`
  - 新增 option：`WithEventValuator(ev EventValuator)`、`WithValuationConfig(cfg ValuationConfig)`

## 2. LLM 事件评估器实现

- [x] 2.1 在 `agent/event_value.go` 实现 `LLMEventValuator`
  - struct 字段：`model model.Model`、`config ValuationConfig`
  - 构造函数：`NewLLMEventValuator(model model.Model, cfg ValuationConfig) EventValuator`
  - 实现 `Evaluate`：按 batch 构造 prompt，调用 `model.GenerateContent`，返回 `[]EventValue` + batch summary

- [x] 2.2 设计 valuation prompt
  - prompt 要求 LLM 对输入的每个 segment 输出 JSON array，每个元素含 `event_key`、`value_score`、`processing`、`key_facts`、`reason`
  - prompt 同时要求输出一段整体 batch summary（纯文本，放在 JSON 之后，用 `--- BATCH SUMMARY ---` 分隔）
  - 明确 value_score 含义：0=可丢弃，1=必须保留；processing 含义；key_facts 要求不超过 200 字符

- [x] 2.3 实现解析与兜底
  - 用 `strings.Split` 分离 JSON 部分与 batch summary 部分
  - 用 `encoding/json` 解析 JSON array 到 `[]EventValue`
  - 解析失败时直接返回 error，由调用方降级为 rule-based（仅保留最内层兜底，不兼容旧逻辑）
  - 对缺失字段应用默认值：`processing=Summary`，`key_facts=""`

- [x] 2.4 实现 `ValueFloors`
  - 在 `LLMEventValuator.Evaluate` 返回前遍历每个 `EventValue`
  - 根据 `EventValue` 对应的 event type 查 `config.ValueFloors`，取 `max(value_score, floor)`
  - 默认 floor：`external_input=0.5`，`agent_output=0.4`，其余 0.0

- [x] 2.5 单测 `agent/event_value_test.go`
  - 测试正常 JSON + batch summary 解析
  - 测试缺失字段默认值
  - 测试 value floor clamp
  - 测试 malformed JSON 返回 error

## 3. 价值驱动的压缩规划

- [x] 3.1 重写 `SmartCompressor.Compress` 的规划阶段
  - 在 `SegmentMessages` 之后，对可压缩 segment（非最近 `KeepRecentTasks`、非最后未完成）调用 `eventValuator.Evaluate`
  - 对无法解析 segment 内 event key 的情况，给该 segment 一个默认 `EventValue{ValueScore: 0.5, Processing: Summary}`
  - 计算每个 segment 的 `valueDensity = valueScore / max(tokens, 1)`
  - 按 `valueDensity` 升序排序，得到压缩顺序

- [x] 3.2 根据 processing 策略直接分配压缩等级
  - 在排序后的 segment 上依次处理，维护 `remainingExcess`
  - `Keep` → level 0，不释放 token
  - `Truncate`/`KeyFacts` → level 1，释放 `nonKeyTokens*4/5`
  - `Summary` → level 2 或 level 3（若 remainingExcess 仍大于该 segment 预估释放量则升 level 3）
  - `Reference`/`Drop` → level 3，释放 `segTokens*4/5`
  - 一旦 `remainingExcess <= 0` 停止；剩余未处理 segment 保持 level 0

- [x] 3.3 删除旧的年龄贪婪代码
  - 删除 `smart_compress.go` 中按 `for i range plans` 从旧到新依次判断的旧逻辑块
  - 保留 `KeepRecentTasks` 和最后未完成 segment 的保护逻辑，但改为在排序前标记不可 L3

- [x] 3.4 单测 `agent/smart_compress_plan_test.go`
  - 测试低价值密度 segment 先被压缩
  - 测试 `Keep` 策略阻止压缩
  - 测试 `KeepRecentTasks` 硬边界
  - 测试 budget 满足后剩余 segment 不被压缩

## 4. 摘要-记忆 RAG 闭环

- [x] 4.1 实现 `SmartCompressor.archiveSegment`
  - 输入：`seg *TaskSegment`、`summary string`、`value EventValue`
  - 计算 segment 内容 hash（用 `fnv` 或 `sha256` 对 messages 序列化后的字节）
  - 查 `archiveCache`，命中则直接返回 `summaryKey`
  - 未命中时：
    - 生成新的 `summaryKey`，使用 `memory.NewSnowflakeEventKey(partitionID, 0)`
    - partitionID 优先从 segment 内 event key 提取（`memory.PartitionIDFromEventKey`），否则用 `memory.NewPartitionID()`
    - 构造 `memory.FullEvent{EventType: "context_compress_summary", EventSummary: summary, Content: value.KeyFacts, Metadata: map[string]string{"original_key": fmt.Sprintf("%d", value.EventKey)}}`
    - 调用 `sc.memStore.StoreEvent(summaryKey, event)`
    - 写入 `archiveCache`
    - 返回 `summaryKey`

- [x] 4.2 实现 `SmartCompressor.buildReferenceMessage`
  - 输入：`originalKey int64`、`summaryKey int64`、`value EventValue`
  - 输出 system message，内容格式：
    ```
    [context_archive] evt_<originalKey> 已归档
    处理方式: <processing>
    价值分: <value_score>
    摘要 key: <summaryKey>
    关键事实: <key_facts>
    如需完整信息: recall({"event_keys": [<summaryKey>]})
    ```

- [x] 4.3 在 `Compress` 的 level 3 分支中调用归档
  - 当 segment 被分配 level 3 且 processing 为 `Summary`/`Reference`/`Drop` 时：
    - 生成 batch summary（已在 valuation 阶段得到）
    - 调用 `archiveSegment` 得到 `summaryKey`
    - 在结果中插入 `buildReferenceMessage` 替代原 segment 内容
  - `Drop` 策略直接丢弃，不插入引用消息（信息价值为 0 时不值得占用 token）

- [x] 4.4 验证 recall 可检索 summary key
  - 无需修改 recall 工具本身（`recall_get` 已支持按 key 取事件）
  - 在 `agent/context_manager.go` 的 `resolveReferenceToMessage` 中增加对 `context_compress_summary` 类型的处理：直接返回 `model.NewSystemMessage(ref.EventSummary)`

- [x] 4.5 单测与集成测试
  - `TestArchiveSegment_WritesMemoryStore`：验证 summary event 被写入 `InMemoryStore`
  - `TestArchiveSegment_CacheReuse`：相同内容第二次归档返回相同 key
  - `TestRecallSummaryKey`：通过 recall_get 取回 summary event

## 5. 批量优化：评估与摘要合并

- [x] 5.1 重构 `SmartCompressor.generateSummary`
  - 改为返回 `(*ValuationBatchResult, error)`，其中 `ValuationBatchResult` 包含 `Summary string` 和 `Values []EventValue`
  - 内部调用 `eventValuator.Evaluate` 而非直接构造 summary prompt
  - 删除旧的 standalone summary prompt 构造代码

- [x] 5.2 删除批次数量上限
  - 在 `batchSegmentsByTokenBudget` 中移除任何 `maxBatches` 或硬编码循环次数限制
  - 保留 `maxInputTokens = maxTokens/2` 的预算控制

- [x] 5.3 单 batch 单次 LLM 调用
  - 在 `summarizeBatches` 中，每个 batch 调用一次 `generateSummary`，该调用内部同时完成评估和摘要
  - 不再为 level 2/level 1 的 per-segment summary 单独调用 LLM；统一用 batch summary 覆盖

- [x] 5.4 超时保护
  - 在 `SmartCompressor.Compress` 入口记录 `startTime`
  - 在每次 LLM 调用前检查 `time.Since(startTime) < sc.valuationConfig.Timeout`
  - 超时则停止继续评估/摘要，已处理部分直接返回当前结果，未处理 segment 原样保留

- [x] 5.5 单测
  - `TestSummarizeBatches_SingleCallPerBatch`：mock model 验证每个 batch 只调用一次
  - `TestCompress_TimeoutStopsGracefully`：mock model 阻塞，验证超时后结果仍合法

## 6. 压缩通知增强

- [x] 6.1 更新 `SmartCompressor.buildSegmentCompressNotice`
  - 输入增加 `values []EventValue`
  - 输出中每条事件格式改为：`- evt_<KEY> [<type>] score=0.XX proc=<processing>: <summary>`
  - 保留通用警告：`**不要重复执行已被压缩的操作来获取相同内容**`
  - 保留召回选项：`recall`、`search_content`、`read_file` with line range

- [x] 6.2 删除或合并 `buildCompressEvent`
  - 当前 `buildCompressEvent` 生成全局 compress 消息，与新的 per-segment inline notice 重复
  - 删除 `buildCompressEvent` 方法及其调用点
  - 将 batch summary 信息追加到最后一个 level-3 segment 的 inline notice 中

- [x] 6.3 更新测试
  - 修改 `smart_compress_test.go` 中断言 compress notice 格式的用例
  - 删除针对 `buildCompressEvent` 的测试（如有）

## 7. 配置与 wiring

- [x] 7.1 修改 `agent/tagent_agent.go` 的 `buildCompressorOpts`
  - 如果 `cfg.SummaryModel != nil`，构造 `NewLLMEventValuator(cfg.SummaryModel, valuationCfg)` 并通过 `WithEventValuator` 注入
  - 注入 `WithValuationConfig`

- [x] 7.2 修改 `newContextManagerFromConfig`
  - 确保 `compressor.memStore` 和 `compressor.projection` 被正确注入（已有逻辑，保持不变）
  - 无需额外 wiring，因为 SmartCompressor 内部使用 memStore

- [x] 7.3 更新 `examples/wechat-bot/tagent.yaml`
  - 在 agent 配置下新增可选 `compress` 块示例：
    ```yaml
    compress:
      value_floors:
        external_input: 0.5
        agent_output: 0.4
      valuation_timeout_ms: 30000
      summary_prompt_version: "v1"
    ```

## 8. 测试、度量和清理

- [x] 8.1 新增/修改 `agent/smart_compress_test.go`
  - 覆盖价值排序、策略映射、归档写入、缓存复用、超时、通知格式
  - 使用 mock model 替代真实 LLM 调用

- [x] 8.2 新增 metrics
  - `smart_compress_valuation_cache_hits_total`
  - `smart_compress_valuation_cache_misses_total`
  - `smart_compress_batches_total`
  - `smart_compress_value_density_min` / `value_density_max`
  - 用 `atomic` 计数器或 `expvar` 暴露，避免引入新依赖

- [x] 8.3 清理死代码
  - 删除 `extractFilePath`（如仍存在）
  - 删除 `collectCompressedEventInfo` 中不再使用的逻辑（或改为供 metrics 使用）
  - 删除旧 `buildCompressEvent` 后检查是否有未引用符号

- [x] 8.4 运行测试与示例
  - `go test ./agent/...` 全绿
  - `go vet ./agent/...`
  - 在 `examples/wechat-bot` 下做一次手动 smoke run，观察 compress log 中价值分与归档 key
