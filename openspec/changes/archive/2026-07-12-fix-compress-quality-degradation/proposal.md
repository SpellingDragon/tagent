## Why

运行日志暴露 SmartCompressor 和 Compactor 的多个缺陷导致 LLM 上下文质量严重退化、陷入死循环：LLM 摘要无视长度约束导致压缩后 token 反而增加；Compactor 重建消息后 role 丢失为 `unknown`；`findPendingUserMessage` 重复追加已在 recentSegments 中的用户消息。这些问题叠加导致 LLM 无法区分 user/assistant/tool，反复执行相同动作。

## What Changes

- **摘要超阈值时切分重新摘要**：`generateSummary` 的 LLM 返回结果超过 `targetChars` 时，将原始输入按段切分为更小的子批次，对每个子批次独立生成摘要后拼接。如果切分后单段摘要仍超限，对该段摘要硬截断。摘要消息的 Role 固定为 `assistant`。
- **Compactor role 降级修复**：`resolveReferenceToMessage` 在 `full.Response == nil` 时，使用 `ref.EventType` 推断正确的 Role（external_input→user, agent_output→assistant, action_command→tool），而非使用可能为空的 `ref.Role`
- **EventReference.Role 空值兜底**：`BuildEventReference` 在 `evt.Response == nil` 时，使用 `evt.StateDelta["event_type"]` 推断 Role
- **findPendingUserMessage 基于 event key 去重**：追加 pending user 消息前检查其 event key 是否已存在于 recentSegments 的消息前缀中，避免重复

## Capabilities

### New Capabilities
- `compress-quality-fix`: 修复 SmartCompressor 摘要膨胀、Compactor role 丢失、用户消息重复

### Modified Capabilities
（无——不修改已有 spec 的需求）

## Impact

- 修改 `agent/smart_compress.go`：`generateSummary` 增加超限切分重新摘要逻辑；`findPendingUserMessage` 增加 event key 去重检查
- 修改 `agent/context_manager.go`：`resolveReferenceToMessage` 增加 EventType→Role 推断降级
- 修改 `agent/projection.go`：`BuildEventReference` 在 Response 为 nil 时从 EventType 推断 Role
## Why

运行日志暴露 SmartCompressor 和 Compactor 的多个缺陷导致 LLM 上下文质量严重退化、陷入死循环：LLM 摘要无视长度约束导致压缩后 token 反而增加；Compactor 重建消息后 role 丢失为 `unknown`；`findPendingUserMessage` 重复追加已在 recentSegments 中的用户消息。这些问题叠加导致 LLM 无法区分 user/assistant/tool，反复执行相同动作。

## What Changes

- **摘要长度强制截断**：`summarizeBatch` 的 LLM 返回结果超过 `targetChars` 时，截断为目标长度 + 省略标记，不让摘要膨胀
- **Compactor role 降级修复**：`resolveReferenceToMessage` 在 `full.Response == nil` 时，使用 `ref.EventType` 推断正确的 Role（external_input→user, agent_output→assistant, action_command→tool），而非使用可能为空的 `ref.Role`
- **EventReference.Role 空值兜底**：`BuildEventReference` 在 `evt.Response == nil` 时，使用 `evt.StateDelta["event_type"]` 推断 Role
- **findPendingUserMessage 去重**：追加 pending user 消息前检查它是否已存在于 recentSegments 中，避免重复

## Capabilities

### New Capabilities
- `compress-quality-fix`: 修复 SmartCompressor 摘要膨胀、Compactor role 丢失、用户消息重复

### Modified Capabilities
（无——不修改已有 spec 的需求）

## Impact

- 修改 `agent/smart_compress.go`：`summarizeBatch` 增加结果截断；`findPendingUserMessage` 增加去重检查
- 修改 `agent/context_manager.go`：`resolveReferenceToMessage` 增加 EventType→Role 推断降级
- 修改 `agent/projection.go`：`BuildEventReference` 在 Response 为 nil 时从 EventType 推断 Role
