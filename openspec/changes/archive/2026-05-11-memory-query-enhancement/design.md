## Context

当前 recall agent 的三个子工具（`memory_query`、`memory_get`、`memory_recent`）消费了 MemoryStore 接口能力的很小一部分。`QueryOptions` 已有 `StartTime`/`EndTime` 字段但从未被使用；`FullEvent.ParentKey` 在每个事件中都被正确设置，但 recall 没有任何工具能沿链回溯。此次变更在 recall 工具层补齐这两个缺口。

约束条件：不修改 `MemoryStore` 接口、不修改任何存储实现、不触动 `MemoryPlugin` 写入链路。所有变更限定在 `tool/recall/` 包。

## Goals / Non-Goals

**Goals:**
- `memory_query` / `memory_recent` 支持时间范围过滤，映射到已有的 `QueryOptions.StartTime/EndTime`
- 新增 `memory_trace` 工具，沿 `ParentKey` 链回溯事件历史
- `memory_get` 可选附带父事件摘要
- 所有新参数向后兼容（不传时行为不变）

**Non-Goals:**
- 不修改 `MemoryStore` 接口定义
- 不新增存储层方法
- 不引入外部依赖（如向量数据库、全文索引）
- 不修改 SmartCompress、AgentToolWrapper、MemoryPlugin
- 不引入新的事件类型或数据模型
- 不实现 Layer 3 RAG（向量搜索）

## Decisions

### Decision 1: 时间范围参数使用 Unix 毫秒时间戳

**选择**: `since` / `until` 参数定义为 `int64` Unix 毫秒时间戳，直接映射到 `QueryOptions.StartTime/EndTime`。

**替代方案**: 接受相对时间字符串（如 "1h"、"30m"），在 handler 内解析后转换为毫秒时间戳。

**理由**: Unix 毫秒时间戳与 `FullEvent.Timestamp` 和 `QueryOptions.StartTime/EndTime` 格式一致，零转换成本。LLM 需要做的时间推理（"1小时前" → `time.Now().UnixMilli() - 3600000`）对于现代 LLM 是基础能力。保持 handler 简单，不引入时间解析复杂度。如果未来需要相对时间快捷方式，可以在工具描述中建议 LLM 自行换算。

### Decision 2: memory_trace 使用逐次 GetEvent 遍历

**选择**: `memory_trace` 内部循环调用 `accessor.GetEvent(parentKey)` 逐步回溯，而非新增批量查询方法。

**替代方案**: 在 MemoryStore 接口新增 `GetCausalChain(key, maxSteps)` 方法。

**理由**: 
- 不修改 MemoryStore 接口（Non-Goal）
- 因果链回溯不是高频操作（LLM 调用 tool 的频率远低于 MemoryPlugin 写入），逐次调用的 IO 开销可忽略
- `GetEvent` 已按 key 精确定位（InMemoryStore O(1)，FileBackend 直接定位文件），性能可接受
- maxSteps 上限 20 确保最坏情况下只有 20 次 `GetEvent` 调用

### Decision 3: memory_get 的 include_parent 只返回摘要

**选择**: `include_parent=true` 时，只返回父事件的 EventType、EventSummary、Timestamp、EventKey，不返回完整 Content。

**理由**: 
- 减少 LLM 上下文消耗（父事件的完整 Content 可能很长）
- LLM 看到摘要后如需完整内容，可再次调用 `memory_get` 精确获取
- 保持 EventReference 的"先摘要后详情"读取模式一致

### Decision 4: memory_trace 作为独立工具，不合并到 memory_get

**选择**: 新增独立的 `memory_trace` 工具，不将其作为 `memory_get` 的参数（如 `memory_get(key=123, trace=true)`）。

**理由**:
- 语义清晰：`memory_get` = 取单个事件，`memory_trace` = 遍历链
- LLM 工具选择时能明确区分两种操作
- 避免 `memory_get` 的返回结构过度膨胀（trace 返回数组，get 返回单个对象）
- 遵循单一职责原则，降低每个工具的复杂度

### Decision 5: 不将时间过滤加入 memory_get

**选择**: `memory_get` 按 key 精确查询，不受时间过滤。

**理由**: `memory_get` 的语义是"根据 key 取事件"，key 自身编码了时间戳（Snowflake），按时间再过滤一次是多余的。时间过滤属于 `memory_query` / `memory_recent` 这类列表查询的职责。

## Risks / Trade-offs

- **[风险] memory_trace 在长链上多次 GetEvent 调用增加延迟** → 缓解：maxSteps 上限 20，最坏情况 20 次 `GetEvent`。FileBackend 每次是一次 `os.ReadFile`，20 次约 1-5ms，对 LLM tool call 的 round-trip 时间不构成瓶颈

- **[风险] LLM 可能传入不合理的 since/until 范围（如覆盖整个历史）** → 缓解：handler 层不加限制——LLM 请求全量数据时返回全量是本应有的行为。如需保护，可在 QueryOptions 的 Limit 已有默认值（10）

- **[权衡] `include_parent` 只返回摘要而非完整内容** → 接受此权衡：如需完整父事件内容，LLM 可再调用一次 `memory_get`。单次 tool call 的信息密度优先

## Open Questions

- recall agent 的 prompt 更新程度：是否需要详细描述每个新参数的用法？当前方案是在工具的 `Declaration().Description` 中说明，由 LLM 自行理解。如果实践中 LLM 未能有效使用新参数，再考虑在 system prompt 中添加示例。
