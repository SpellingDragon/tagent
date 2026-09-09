# compress-event-enrichment Specification

## Purpose

本规范定义 compress-event-enrichment 能力。When `SmartCompressor` discards old task segments, `buildCompressEvent` SHALL extract each compressed event's EventKey, EventType, a short content summary, `val

> **状态核验（2026-09-09）**：本规范描述的 `buildCompressEvent` 富集注记（逐事件 `score=<分数>` / `proc=<处理策略>` 元数据，依赖 `EventValuator` 估值产物）**已被骨架卡片行替代**——压缩产物现为滚动摘要 ref（负 key）内的卡片行序列（`[hex] type 摘要`，票据恒在），无分数/策略元数据。Req2 的原则（注记不含工具名广告、只携事实与票据）**仍有效**并由现渲染路径遵守。本文其余 SHALL 条款不再约束当前实现，保留作为设计决策记录；现状形态见 [task-skeleton-compression](../task-skeleton-compression/spec.md) 与 [event-timeline-rendering](../event-timeline-rendering/spec.md)。
## Requirements
### Requirement: buildCompressEvent outputs key, type, summary, value score, and processing strategy

When `SmartCompressor` discards old task segments, `buildCompressEvent` SHALL extract each compressed event's EventKey, EventType, a short content summary, `value_score`, and `processing` recommendation from the `[evt_KEY|type]` prefix and the `EventValue` produced by `EventValuator`. The output message SHALL list each event as `- evt_<KEY> [<type>] score=<score> proc=<processing>: <summary>` followed by a recall hint. This enables the LLM to understand not only what was compressed, but also how valuable each compressed event is and how to recover it.

#### Scenario: Compressed events listed with value metadata

- **WHEN** `SmartCompressor` compresses 3 segments containing events with keys 1234, 1235, 1236 and value scores 0.9, 0.3, 0.5
- **THEN** the compress message SHALL contain:
  ```
  [context_compress] 压缩了 3 个对话片段:
  - evt_1234 [external_input] score=0.90 proc=keep: 用户请求获取文章
  - evt_1235 [thinking_plan] score=0.30 proc=summary: 好的，我来帮你获取...
  - evt_1236 [action_command] score=0.50 proc=keyfacts: 调用工具: action({"command":"curl ..."})

  被压缩的内容不要重复执行。如需找回，请使用 recall/search_content。
  ```

#### Scenario: Events without prefix fall back to key-only listing

- **WHEN** a compressed message has no `[evt_KEY|type]` prefix (e.g., system message)
- **THEN** it SHALL be listed as `- evt_<KEY> [unknown] score=0.50 proc=summary: <content[:summary_len]>`

#### Scenario: No memory store or projection side effects

- **WHEN** `SmartCompressor` compresses old segments
- **THEN** no new events SHALL be written to MemoryStore as a side effect of building the compress notice
- **AND** no new EventReferences SHALL be appended to SessionProjection
- **AND** only `args.Request.Messages` (the view) is modified
- **AND** any explicit `summary`/`reference` archive step SHALL be performed separately from notice construction

### Requirement: Compress notice warns against repeating archived operations

The compress notice SHALL include a generic warning that the agent SHALL NOT re-execute operations whose outputs were compressed, regardless of whether the source was file reads, model outputs, search results, or API calls. The notice SHALL NOT enumerate specific tool names or call syntax (e.g. `recall({...})`, `search_content`, `read_file` with parameters) — tool availability is a per-agent assembly decision, and framework-generated text SHALL only carry facts and tickets (event keys), never advertise capabilities. Recovery guidance lives in tool declarations, which are always consistent with the actual assembly.

#### Scenario: Generic warning present

- **WHEN** `buildCompressEvent` is generated
- **THEN** the notice SHALL contain text equivalent to `**不要重复执行已被压缩的操作来获取相同内容**`
- **AND** it SHALL list the compressed events with their `evt_` key tickets (existing key/type/summary listing)
- **AND** it SHALL NOT contain tool-name references such as `search_content` or parameterized call examples

