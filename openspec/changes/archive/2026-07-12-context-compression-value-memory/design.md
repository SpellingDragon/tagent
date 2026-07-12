## Context

`SmartCompressor` 当前把对话按 user input 切分为 segment，然后按"从旧到新"的顺序做贪婪压缩。压缩等级（L0-L3）和 `selectiveSplit` 中的关键消息判定都依赖硬编码规则：短消息保留、含 Error 保留、异步结果保留等。这种规则型方案有两个根本问题：

1. **信息效率低**：没有度量每条消息对当前任务的边际价值，可能把高价值旧消息压掉，却保留低价值新消息。
2. **执行效率低**：压缩后的摘要只是文本注入上下文，没有被 `MemoryStore` 索引；LLM 在摘要不足时只能重复执行工具或重新读取文件来获取信息。

本次设计在现有架构内（不引入新模型、不修改 MemoryStore schema）把压缩重新建模为三步：

1. **评估（Valuate）**：用现有 summary model 批量给事件打分并推荐处理方式。
2. **规划（Plan）**：按价值密度排序，在 token 预算内选择最优表示策略。
3. **归档（Archive）**：将摘要/关键事实写入 `MemoryStore`，原事件在上下文中替换为轻量引用。

## Goals / Non-Goals

**Goals：**

- 对同一 batch 内的事件做价值评估，输出 `value_score`、`processing` 策略和 `key_facts`。
- 压缩规划从"按年龄贪婪"改为"按价值密度排序 + 预算约束策略选择"。
- 把摘要作为 RAG 索引写入 `MemoryStore`，运行时用 `recall` 按需注入。
- 事件过多时自动分批，不预设模型调用批次上限，依赖 token/超时/预算控制收敛。
- 保持单 segment 内评估，不引入跨 segment 相关性；跨 segment 信息由后续再次压缩自然重组。

**Non-Goals：**

- 不引入新的嵌入模型、向量数据库或语义相似度模型。
- 不修改 `MemoryStore` / RustViking 的持久化 schema（复用现有 `EventSummary` 和 `StoreEvent` 机制）。
- 不实现跨 segment 的语义关联或因果链重排（这些由已有 `relation-store-provider` 和 `recall` 覆盖）。
- 不实现实时压缩质量反馈闭环（可作为后续 change）。

## Decisions

### 1. 用现有 summary model 同时承担"评估"和"摘要"职责

**选择**：不再新建 valuation model，而是让 `SmartCompressor.summaryModel` 在需要时输出结构化的评估结果。

**理由**：
- 避免新增 provider/model 配置复杂度。
- summary model 本身就是为压缩场景准备的，对长上下文和结构化输出要求一致。
- 通过 prompt 工程即可让同一次 LLM 调用同时返回评估元数据和摘要，减少 round-trip。

**替代方案**：单独 valuation model —— 配置更重，且两者输入输出高度重叠。

### 2. `EventValuator` 作为独立接口

**选择**：定义 `EventValuator` 接口，接收 `[]model.Message` 或 `[]*TaskSegment`，返回 `[]EventValue`。

```go
type ProcessingStrategy string

const (
    Keep      ProcessingStrategy = "keep"
    Truncate  ProcessingStrategy = "truncate"
    KeyFacts  ProcessingStrategy = "keyfacts"
    Summary   ProcessingStrategy = "summary"
    Reference ProcessingStrategy = "reference"
    Drop      ProcessingStrategy = "drop"
)

type EventValue struct {
    EventKey    int64
    ValueScore  float64          // 0.0 ~ 1.0
    Processing  ProcessingStrategy
    KeyFacts    string           // 结构化关键事实
    Reason      string           // 评估理由（可选，用于调试）
}
```

**理由**：
- 解耦评估策略与压缩规划，便于单元测试和后续替换为更轻量的评估器。
- `SmartCompressor` 只依赖接口，不依赖具体 LLM 实现。

### 3. 批量评估与摘要合并为单次 LLM 调用

**选择**：对每批 segment 只调用一次 summary model，prompt 要求同时返回：
1. 该批中每个事件的评估结果（JSON array）。
2. 该批的整体摘要（text）。

**理由**：
- 相比"先评估所有事件，再摘要部分事件"的两轮调用，减少 30%-50% 的 LLM 调用次数。
- 模型在生成摘要时已经见过所有事件，摘要质量更高。

### 4. 按价值密度排序，而非严格按年龄排序

**选择**：压缩规划时计算每个 segment 的 `value_density = total_value / total_tokens`，按价值密度从低到高排序后依次压缩。

**理由**：
- 低价值密度 segment 是最佳压缩对象，能在相同 token 释放下保留更多信息。
- 保留最近 segment 的约束仍作为硬边界（最后 `KeepRecentTasks` 个 segment 不进入 L3）。

**替代方案**：纯年龄排序 —— 实现简单但信息效率差；纯价值排序忽略最近性 —— 可能把刚发生的关键操作压掉。价值密度是两者的折中。

### 5. 摘要写入 MemoryStore，上下文只保留引用

**选择**：
- 对选择 `summary` 或 `reference` 策略的事件，生成摘要后调用 `MemoryPlugin` / `MemoryStore` 的 `StoreEvent` 写入一个类型为 `context_compress_summary` 的轻量事件。
- 原事件在压缩后的消息列表中替换为 system 消息：`<evt_KEY> 已归档，摘要 key=<summary_key>，可用 recall 找回`。

**理由**：
- 真正释放 session 运行内存，摘要不再占用每次 LLM 调用的上下文。
- `recall` 工具已经存在，只需扩展支持按摘要 key 检索。
- 与现有 `event-compaction` / `l3-archive-summarization` 的"EventSummary 作为归档摘要"理念一致。

### 6. 不预设模型调用批次上限，用 token 预算和超时收敛

**选择**：
- `batchSegmentsByTokenBudget` 继续按 `maxTokens/2` 分批。
- 不限制批次数量；如果事件极多，通过配置的总超时（如 `compression_timeout_ms`）和 token 预算控制。

**理由**：
- 用户明确不要求上限，而是要求"信息效率做到极致"。
- 上限会人为限制可处理的历史深度；预算和超时是更自然的收敛条件。

## Risks / Trade-offs

| Risk | Mitigation |
|---|---|
| LLM 评估增加单次压缩延迟 | 评估与摘要合并为单次调用；对缓存命中的 segment 跳过评估；提供 `value_driven: false` 开关回退到旧规则。 |
| LLM 输出 JSON 解析失败 | 使用宽松解析 + fallback：解析失败时降级为当前基于规则的压缩，记录 warning 指标。 |
| 过度归档导致上下文"碎片化" | compress notice 明确列出被归档的事件 key 和摘要 key；保留 L0/L1/L2 的完整消息作为缓冲。 |
| 摘要丢失关键信息，LLM 重复执行工具 | compress notice 增加"不要重复执行已被压缩操作"的通用警告；summary key 支持 recall 完整内容。 |
| 同一事件被重复评估/摘要 | 引入评估缓存：key = hash(segment messages + prompt version)；相同输入直接复用结果。 |
| MemoryStore 写入增加 I/O | 摘要事件很小（仅 metadata + summary text），且只在真正压缩时写入；批量写入。 |

## Migration Plan

1. **Phase 1（可回滚）**：新增 `EventValuator` 接口与 LLM 实现，默认关闭 `value_driven` 开关，旧逻辑不变。
2. **Phase 2**：开启 `value_driven` 后，压缩规划改为价值密度排序；同时保留旧路径作为 fallback。
3. **Phase 3**：接入 MemoryStore 归档，把 summary/reference 事件写入 store，上下文替换为引用。
4. **Rollback**：设置 `compression.value_driven: false` 并重启，恢复当前基于规则的压缩。

## Open Questions

1. `value_score` 的阈值是否需要按 event type 配置？例如 `external_input` 默认最低分不应低于 0.5。
2. 评估缓存的失效策略：summary model prompt 版本变化时是否清空缓存？
3. `recall` 工具是否需要支持"按摘要 key 检索"的新参数，还是复用现有 `event_keys`？
4. 是否需要在 metrics 中新增 `valuation_cache_hit_rate` 和 `compression_value_density` 指标？
