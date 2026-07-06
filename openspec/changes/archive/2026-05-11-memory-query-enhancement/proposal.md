## Why

memory 模块当前有一个显著的能力断层：存储层（MemoryStore）提供了丰富的数据（QueryOptions 的 7 个过滤维度、每个事件的 ParentKey 因果链），但 recall agent 的子工具只消费了其中最基础的子集（`memory_query` 只用 Limit+EventTypes，`memory_get` 返回了 ParentKey 但无处可用）。这导致 Agent 面对"最近一小时发生了什么"或"这件事的前因是什么"等自然查询时，只能依赖 LLM 在已有 event_keys 中猜测——检索能力严重受限。

## What Changes

- **`memory_query` 增加时间范围过滤**：新增可选 `since` / `until` 参数（Unix 毫秒时间戳），映射到已有的 `QueryOptions.StartTime` / `EndTime`。LLM 可以查询指定时间段的历史事件。
- **新增 `memory_trace` 子工具**：给定一个 event_key，沿 ParentKey 因果链回溯 N 步，返回完整事件链。让 LLM 能从任意事件切入追溯前因后果。
- **`memory_get` 增强**：可选 `include_parent` 参数，取事件时自动附带其父事件摘要，省去 LLM 额外调用的往返。
- **recall agent prompt 更新**：引导 LLM 知晓新参数用途。

所有变更限定在 `tool/recall/` 包内，不修改 MemoryStore 接口、不新增存储方法、不触动写入链路。

## Capabilities

### New Capabilities

- `recall-time-filter`: `memory_query` 和 `memory_recent` 工具支持时间范围过滤参数 `since` / `until`
- `recall-causal-trace`: 新增 `memory_trace` 工具，沿 ParentKey 因果链回溯事件历史

### Modified Capabilities

<!-- No existing specs to modify -->

## Impact

- **修改文件**：`tool/recall/recall_subtools.go`（新增 memory_trace 工具、memory_query/memory_get 参数扩展）、`tool/recall/recall_agent.go`（注册新工具、更新 prompt）
- **测试文件**：`tool/recall/recall_subtools_test.go`（如有）或 memory 包测试
- **不修改**：MemoryStore 接口、InMemoryStore、FileBackend、MemoryPlugin、AgentToolWrapper、SmartCompress
- **向后兼容**：所有新参数均为可选，不传时行为与当前完全一致
