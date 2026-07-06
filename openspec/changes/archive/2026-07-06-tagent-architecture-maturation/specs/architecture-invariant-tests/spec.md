## ADDED Requirements

### Requirement: 不变量 1 测试 — SessionProjection 只含 EventReference

tests/invariants_test.go SHALL 验证：在 runEventLoop 执行一轮后，SessionProjection 中的所有条目都是 `memory.EventReference` 类型，不包含 `FullEvent` 或完整 `Content`。EventReference 的 EventKey、EventType、EventSummary、Timestamp 字段 SHALL 非空（除 EventSummary 可为空字符串）。

#### Scenario: 运行一轮后检查 Projection 内容

- **WHEN** 使用 InMemoryStore + mock model 执行一轮 runEventLoop（InjectMessage → Pull → RunFlow → onEvent）
- **THEN** SessionProjection.GetAll() 返回的每个 EventReference 包含 EventKey > 0
- **AND** EventType 非空
- **AND** Timestamp > 0
- **AND** 不存在 Content 字段（EventReference 结构体无此字段）

### Requirement: 不变量 2 测试 — Compactor 不修改 MemoryStore

tests/invariants_test.go SHALL 验证：Compactor.Compact 执行前后，MemoryStore 中的 FullEvent 数量和内容完全不变。Compactor 只修改 SessionProjection（替换旧引用为 summary reference），不删除、不修改 MemoryStore 中的任何 FullEvent。

#### Scenario: Compact 前后 MemoryStore 不变

- **WHEN** 向 SessionProjection 追加 10 个 EventReference（对应 MemoryStore 中 10 个 FullEvent）
- **AND** 执行 Compactor.Compact（keepRecentTasks=2）
- **THEN** SessionProjection 引用数减少（旧引用被替换为 summary reference）
- **AND** MemoryStore 仍包含全部 10 个 FullEvent
- **AND** 每个 FullEvent 的 Content、EventType、EventSummary 与 Compact 前完全一致

### Requirement: 不变量 3 测试 — 工具结果经 onEvent 回流到 SessionProjection

tests/invariants_test.go SHALL 验证：框架 Runner 执行工具后产生的事件，经 onEvent 回调追加到 SessionProjection。工具结果事件的 EventType SHALL 为 `action_command`，且 EventKey SHALL 非零（由 MemoryPlugin 生成）。

#### Scenario: 工具执行后 Projection 包含 action_command

- **WHEN** mock model 返回带 tool_calls 的 response，mock tool 返回结果
- **AND** runEventLoop 执行一轮（包含工具调用）
- **THEN** SessionProjection 中存在 EventType == "action_command" 的 EventReference
- **AND** 该 EventReference 的 EventKey > 0

#### Scenario: 工具结果 Publish 到 EventBus

- **WHEN** 工具执行后 onEvent 回调触发
- **AND** 事件类型为 action_command
- **THEN** EventBus 中存在 Source == "tool_result" 的 AgentEvent
- **AND** 该 AgentEvent 的 Message.Content 包含工具结果摘要
