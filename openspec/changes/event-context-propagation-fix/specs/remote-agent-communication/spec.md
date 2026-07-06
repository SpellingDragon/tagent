## ADDED Requirements

### Requirement: AgentToolWrapper 自动注入 event_keys

AgentToolWrapper.Call SHALL 在 LLM 未传 event_keys 且配置了 event_params 时，自动从 parentProjection 取最近 5 个 EventKey 注入。EventKey 为 0 的引用 SHALL 被跳过。注入的 event_keys 通过 serializeExternalContext 序列化到 RuntimeState["external_context"]，与 LLM 主动传递的 event_keys 走相同路径。

#### Scenario: LLM 未传 event_keys 时自动注入最近 5 个

- **WHEN** LLM 调用 knowledge({request: "搜索天气"}) 未传 event_keys
- **AND** parentProjection 有 8 个 EventReference
- **THEN** 自动取最近 5 个 EventKey
- **AND** 通过 parentStore.GetEvent 获取 FullEvent
- **AND** 序列化到 RuntimeState["external_context"]

#### Scenario: LLM 主动传了 event_keys 时不自动注入

- **WHEN** LLM 调用 recall({request: "之前说的", event_keys: [K1, K3]})
- **THEN** 使用 LLM 传递的 event_keys，不执行自动注入

## MODIFIED Requirements

### Requirement: Context passed via RuntimeState

AgentToolWrapper.Call SHALL serialize resolved external events into `Invocation.RunOptions.RuntimeState["external_context"]` as JSON. Call SHALL use `context.WithTimeout` to wrap the sub-agent invocation. For remote A2A agents, Call SHALL retry once on network failure with 500ms backoff. For local TagentAgent, Call SHALL NOT retry. When LLM does not pass event_keys and AgentToolWrapper has a non-nil parentProjection, Call SHALL automatically inject the most recent 5 EventKeys from parentProjection as fallback context.

#### Scenario: EventKey resolution to RuntimeState

- **WHEN** LLM passes event_keys in tool arguments
- **THEN** the wrapper resolves each key via `parentStore.GetEvent(key)` to obtain FullEvent
- **AND** serializes only EventKey, EventType, EventSummary into `[]ExternalContextEntry` JSON
- **AND** stores the JSON in `inv.RunOptions.RuntimeState["external_context"]`

#### Scenario: Auto-injection when LLM omits event_keys

- **WHEN** LLM does not pass event_keys in tool arguments
- **AND** AgentToolWrapper has eventParams configured for "event_keys"
- **AND** parentProjection is non-nil with at least 1 EventReference
- **THEN** the wrapper takes the most recent 5 EventKeys from parentProjection
- **AND** resolves them via parentStore.GetEvent
- **AND** serializes to RuntimeState["external_context"]

#### Scenario: No event_keys and empty projection

- **WHEN** LLM does not pass event_keys and parentProjection is empty
- **THEN** the wrapper constructs Invocation without "external_context" in RuntimeState
- **AND** the sub-agent runs without external context

#### Scenario: Remote A2A timeout and retry

- **WHEN** A2AAgent.Run fails with network error after 120s timeout
- **THEN** Call waits 500ms and retries once
- **AND** if retry succeeds, returns result normally
