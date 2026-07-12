## 1. 摘要切分重新摘要

- [x] 1.1 在 `agent/smart_compress.go` 的 `generateSummary` 中，流式接收 LLM 响应后检查 `len(result)` 是否超过 `targetChars * 1.5`
- [x] 1.2 超限时执行切分重新摘要：将原始 segments 按 `len(segments)/2` 分成两个子批次，对每个子批次独立调用 `generateSummary`，targetChars 减半，递归深度限制为 2
- [x] 1.3 递归到底仍超限时，硬截断为 `result[:targetChars] + "...(摘要已截断)"` 作为最终兜底
- [x] 1.4 确认 `summarizeBatches` 中摘要消息使用 `model.Message{Role: model.RoleAssistant, Content: ...}`（替代当前的 `model.NewSystemMessage`）
- [x] 1.5 新增测试：模拟 LLM 返回超长摘要，验证切分重新摘要行为；模拟递归后仍超限，验证硬截断

## 2. EventType → Role 推断

- [x] 2.1 在 `agent/context_manager.go` 的 `resolveReferenceToMessage` 中，当 `full.Response == nil` 时使用 `ref.EventType` 推断 Role
- [x] 2.2 新增 `eventTypeToRole(eventType string) model.Role` 辅助函数：external_input→user, agent_output→assistant, action_command→tool, thinking_plan→assistant, 默认→user
- [x] 2.3 修正 warn 日志：区分 "GetEvent failed" 和 "no Response, falling back to EventType inference"
- [x] 2.4 新增测试：验证各种 EventType 的 Role 推断

## 3. BuildEventReference Role 兜底

- [x] 3.1 在 `agent/projection.go` 的 `BuildEventReference` 中，当 `evt.Response == nil` 时从 `evt.StateDelta["event_type"]` 推断 Role
- [x] 3.2 新增测试：验证无 Response 事件的 EventReference.Role 不为空

## 4. findPendingUserMessage 基于 event key 去重

- [x] 4.1 在 `agent/smart_compress.go` 的 `Compress` 方法中，`findPendingUserMessage` 返回后从消息 Content 中解析 event key（`[evt_KEY|type]` 前缀）
- [x] 4.2 扫描 recentSegments 中的 user 消息，检查是否有相同 event key 前缀
- [x] 4.3 已存在则不追加，记录 Debug 日志 "pending user message evt_KEY=N already in recent segments, skipping"
- [x] 4.4 无 event key 前缀的消息正常追加（无 key 可去重）
- [x] 4.5 新增测试：验证基于 event key 的去重逻辑

## 5. 验证

- [x] 5.1 `go build ./...` 通过
- [x] 5.2 `go test ./agent/...` 全部通过
- [x] 5.3 确认 `resolveReferenceToMessage` 不再产生 `unknown` role
- [x] 5.4 确认 `generateSummary` 超限时切分重新摘要，最终 token 减少
## 1. 摘要长度强制截断

- [x] 1.1 在 `agent/smart_compress.go` 的 `summarizeBatch` 中，流式接收 LLM 响应后检查 `len(result)` 是否超过 `targetChars * 1.5`
- [x] 1.2 超限时截断为 `result[:targetChars] + fmt.Sprintf("...(摘要已截断，原始长度 %d 字符)", len(result))`
- [x] 1.3 新增测试：模拟 LLM 返回超长摘要，验证截断行为

## 2. EventType → Role 推断

- [x] 2.1 在 `agent/context_manager.go` 的 `resolveReferenceToMessage` 中，当 `full.Response == nil` 时使用 `ref.EventType` 推断 Role
- [x] 2.2 新增 `eventTypeToRole(eventType string) model.Role` 辅助函数：external_input→user, agent_output→assistant, action_command→tool, thinking_plan→assistant, 默认→user
- [x] 2.3 修正 warn 日志：区分 "GetEvent failed" 和 "no Response, falling back"
- [x] 2.4 新增测试：验证各种 EventType 的 Role 推断

## 3. BuildEventReference Role 兜底

- [x] 3.1 在 `agent/projection.go` 的 `BuildEventReference` 中，当 `evt.Response == nil` 时从 `evt.StateDelta["event_type"]` 推断 Role
- [x] 3.2 新增测试：验证无 Response 事件的 EventReference.Role 不为空

## 4. findPendingUserMessage 去重

- [x] 4.1 在 `agent/smart_compress.go` 的 `Compress` 方法中，`findPendingUserMessage` 返回后检查 recentSegments 中是否已有相同 Content 的 user 消息
- [x] 4.2 已存在则不追加，记录 Debug 日志 "pending user message already in recent segments, skipping"
- [x] 4.3 新增测试：验证去重逻辑

## 5. 验证

- [x] 5.1 `go build ./...` 通过
- [x] 5.2 `go test ./agent/...` 全部通过
- [x] 5.3 确认 `resolveReferenceToMessage` 不再产生 `unknown` role
- [x] 5.4 确认 `summarizeBatch` 截断后 token 减少
