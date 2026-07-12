## Why

当前 `SmartCompressor` 的压缩决策基于固定规则（年龄、消息长度、错误关键字）和硬编码的收益假设（4/5 压缩比），没有真正衡量每条消息对当前任务的边际价值，也没有把压缩后的摘要与 `MemoryStore` 的召回能力打通。结果导致两类典型低效：高价值旧消息被过度压缩，LLM 不得不重复执行工具来恢复信息；低价值长消息被原样保留，浪费上下文预算。本次改动将压缩建模为"事件价值评估 + 表示策略选择 + RAG 式摘要存储"，在仅使用现有 LLM 的前提下，让上下文在信息密度和执行开销上都接近最优。

## What Changes

- 引入**事件价值评估器**（`EventValuator`）：对同一批次内的消息/事件调用现有 summary model，让 LLM 输出每条事件的 `value_score`（0-1）、`processing`（keep/truncate/keyfacts/summary/reference/drop）和 `key_facts`，不引入新的嵌入模型或外部依赖。
- 重构 `SmartCompressor` 的压缩规划：从"按年龄贪婪"改为"按价值密度排序 + 预算约束下的策略选择"；**不引入跨 segment 相关性**，跨 segment 的信息重组由后续再次压缩自然完成。
- 新增**摘要-记忆 RAG 闭环**：被压缩且选择 `summary`/`reference` 策略的事件，其摘要/关键事实写入 `MemoryStore` 并替换为轻量引用；`recall` 工具按需注入完整内容，使事件一旦归档就不再常驻 session 运行内存。
- 优化批量处理：事件过多时自动分批评估与摘要，**不预设模型调用上限**，依赖 token/预算与超时控制来收敛。
- 扩展压缩提示（compress notice）：不仅列出被压缩的 event key，还包含价值分数、推荐处理方式以及如何通过 `recall`/`search_content` 找回信息。

## Capabilities

### New Capabilities

- `value-driven-compression`: 基于事件价值密度进行压缩策略选择，替代当前按年龄贪婪的规则。
- `llm-event-valuation`: 利用现有 summary model 批量输出每条事件的 value_score、processing 策略和 key_facts，不引入新模型。
- `summary-memory-rag`: 将压缩摘要写入 MemoryStore 作为 RAG 索引，运行时用 recall 注入，释放 session 运行内存。

### Modified Capabilities

- `compress-event-enrichment`: 扩展 context_compress 通知内容，增加价值分数、处理策略和召回指引。
- `batched-summarization`: 扩展分批逻辑，支持按价值密度分组、批量事件评估，并取消预设批次上限。

## Impact

- 主要修改 `agent/smart_compress.go`、`agent/context_manager.go`、`agent/task_segmenter.go`。
- 新增 `EventValuator` 接口与实现，复用现有 `model.Model`（summary model）作为评估模型。
- `MemoryStore` 新增轻量摘要写入路径；`recall` 工具的命中率和使用场景增加。
- 配置文件（`tagent.yaml`）可能新增 `compression.value_driven` 开关与评估提示配置。
- 测试覆盖：事件评估批处理、价值排序、摘要-记忆闭环、压缩通知新格式。
