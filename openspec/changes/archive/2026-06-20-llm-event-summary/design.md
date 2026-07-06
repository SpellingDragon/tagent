## Context

**PLACEHOLDER change**。本 change 将在 `harden-event-storage-for-scale` 合并、上线运行一段时间、收集真实 L3 归档数据后启动。详细设计决策（LLM 模型选择、prompt 工程、批处理大小、降级阈值、敏感信息过滤策略）在启动时补充。

## Deferred Design Topics

- **模型选型**：小模型（gpt-4o-mini / Claude Haiku）vs 本地模型（Qwen 7B / Llama 3.1 8B）trade-off
- **Prompt 工程**：
  - `assistant_response` 类 → "保留核心决策、行动、结论，去除过程推理"
  - `tool_result` 类 → "保留成功/失败、关键返回字段、错误类别"
  - `thinking_plan` 类 → "保留最终计划，去除中间推理"
- **批处理 vs 单事件**：RTT 节省 vs 失败隔离粒度
- **降级链**：`LLM → 截断 content 500 字 → 空摘要（pass-through）`
- **敏感信息过滤**：信用卡号、密钥、私人信息的正则预过滤 + 事后 check
- **摘要长度预算**：每类事件最多 N 字符；超长触发再压缩

## Upstream Dependencies

- `harden-event-storage-for-scale` 的 `l3-archive-summarization` capability 提供：
  - `SummaryGenerator` 接口
  - `FullEvent.EventSummary` 字段
  - `archive_summary_types[type]` 配置加载
  - `PassthroughSummarizer` 默认实现（本 change 的降级 fallback）

## Open Questions (to be answered on kickoff)

- L3 归档是否需要"可解码回原文"的保证？（否 → 语义压缩；是 → 需要独立保留原文的冷存储）
- 同一事件是否允许 re-summarize？（多次压缩是否稳定）
- 多语种一致性：原事件中文 → 摘要中文；混合语种如何处理？
- LLM 调用预算封顶：超额后自动降级到 pass-through？
