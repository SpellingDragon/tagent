## ADDED Requirements

### Requirement: AgentToolWrapper 的 Declaration 声明正确的 InputSchema
AgentToolWrapper.Declaration() SHALL 返回包含 event_keys 参数的 InputSchema，当 eventParams 包含 "event_key" 时。

#### Scenario: 声明 event_keys 参数
- **WHEN** `eventParams` 包含 `"event_key"`
- **THEN** Declaration 返回的 InputSchema.Properties SHALL 包含 `event_keys` 字段
- **AND** `event_keys` 类型为 array，元素类型为 integer

#### Scenario: 未声明 event_keys 时不包含该字段
- **WHEN** `eventParams` 为空或 nil
- **THEN** Declaration 返回的 InputSchema.Properties SHALL NOT 包含 `event_keys` 字段

### Requirement: AgentToolWrapper.Call 正确解析 event_key 并注入外部上下文
AgentToolWrapper.Call() SHALL 从 args 中提取 event_keys，通过 parentStore 解析为 FullEvent，并调用子 agent 的 IngestExternalEvents。

#### Scenario: 解析 event_key 并注入
- **WHEN** Call 参数包含 `event_keys: [key1, key2]` 且 parentStore 包含对应 FullEvent
- **THEN** 子 agent 的 pendingExternalEvents SHALL 包含 key1 和 key2 对应的 FullEvent

#### Scenario: event_key 在 parentStore 中不存在
- **WHEN** Call 参数包含不存在的 event_key
- **THEN** 跳过该 key，不注入无效事件，不返回错误

#### Scenario: 无 event_key 时正常执行
- **WHEN** Call 参数不包含 event_keys 字段
- **THEN** 子 agent SHALL 正常执行，不注入外部上下文
