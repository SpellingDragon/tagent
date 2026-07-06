## 0. 前置

- [ ] 0.1 等待 `harden-event-storage-for-scale` change 合并并上线观察一周
- [ ] 0.2 收集真实 L3 归档数据（事件类型分布、平均 content 长度、周压缩数据量）
- [ ] 0.3 评估 LLM 预算（按预估事件量 × 平均 token 数）

## 1. LLMSummarizer 实现

- [ ] 1.1 在 `memory/summarizer.go` 新增 `LLMSummarizer struct`：字段包括 `model`、`batchSize`、`timeout`、`fallback SummaryGenerator`
- [ ] 1.2 实现 `Generate(event FullEvent) (string, error)` 单事件路径
- [ ] 1.3 新增 `GenerateBatch(events []FullEvent) ([]string, error)` 批处理路径
- [ ] 1.4 新增 `memory/summarizer_prompt.go`：事件类型 → prompt 模板映射（`assistant_response`、`tool_result`、`thinking_plan` 等）

## 2. 批处理接入

- [ ] 2.1 修改 `memory/compaction.go` 的 L2→L3 合并路径：按 `eventType` 分桶；每桶批量调 `GenerateBatch`
- [ ] 2.2 批失败降级：单桶整体失败 → 回退到 `PassthroughSummarizer.Generate` 逐事件调用
- [ ] 2.3 并发控制：桶间串行（单一 Compaction 任务内），避免对 LLM 并发过高

## 3. 失败处理与监控

- [ ] 3.1 失败降级 metric：`summarizer_fallback_total{reason}` 计数
- [ ] 3.2 超时熔断：连续 N 次超时后切换为降级模式 M 分钟
- [ ] 3.3 摘要质量校验：空摘要 / 过长摘要 / 语言不一致时记录 warning 并降级

## 4. 配置

- [ ] 4.1 `Config.ArchiveSummarizer`：`Model / MaxBatchSize / TimeoutMs / Enabled`
- [ ] 4.2 `tagent.go` `resolveMemoryStore` 根据 `ArchiveSummarizer.Enabled` 选择 `PassthroughSummarizer` / `LLMSummarizer`

## 5. 测试 & 验证

- [ ] 5.1 单元测试：LLMSummarizer happy path（mock LLM 客户端）
- [ ] 5.2 批处理测试：10 个事件分 3 桶分别处理
- [ ] 5.3 降级测试：LLM 超时 → 回退 PassthroughSummarizer
- [ ] 5.4 端到端：写满一周事件 → 触发 L2→L3 → 验证 `EventSummary` 字段真实填充
