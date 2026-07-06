## ADDED Requirements

### Requirement: AgentToolWrapper.Call 设置超时

AgentToolWrapper.Call SHALL 使用 `context.WithTimeout` 包装子 Agent 调用。默认超时为 120 秒。超时后 SHALL 返回 error。超时时间 SHALL 可通过 `AgentToolWrapper` 的可选配置覆盖。

#### Scenario: 子 Agent 正常完成

- **WHEN** 子 Agent 在超时前完成
- **THEN** Call 正常返回结果

#### Scenario: 子 Agent 超时

- **WHEN** 子 Agent 运行超过超时时间
- **THEN** Call 返回超时 error
- **AND** 子 Agent 的 context 被取消

### Requirement: 远程 A2A 调用失败后重试 1 次

AgentToolWrapper.Call 在远程 A2A 调用失败时 SHALL 重试 1 次。本地调用失败不重试。重试前 SHALL 等待 500ms。

#### Scenario: 远程调用首次失败重试成功

- **WHEN** A2AAgent.Run 首次返回网络错误
- **THEN** 等待 500ms 后重试
- **AND** 第二次调用成功

#### Scenario: 本地调用失败不重试

- **WHEN** TagentAgent.Run 返回错误
- **THEN** 不重试，直接返回错误

## MODIFIED Requirements

### Requirement: Context passed via RuntimeState

AgentToolWrapper.Call SHALL serialize resolved external events into `Invocation.RunOptions.RuntimeState["external_context"]` as JSON. Call SHALL use `context.WithTimeout` to wrap the sub-agent invocation. For remote A2A agents, Call SHALL retry once on network failure with 500ms backoff. For local TagentAgent, Call SHALL NOT retry.

#### Scenario: EventKey resolution to RuntimeState

- **WHEN** LLM passes event_keys in tool arguments
- **THEN** the wrapper resolves each key via `parentStore.GetEvent(key)` to obtain FullEvent
- **AND** serializes only EventKey, EventType, EventSummary into `[]ExternalContextEntry` JSON
- **AND** stores the JSON in `inv.RunOptions.RuntimeState["external_context"]`

#### Scenario: No event_keys provided

- **WHEN** LLM does not pass event_keys in tool arguments
- **THEN** the wrapper constructs Invocation without "external_context" in RuntimeState

#### Scenario: Remote A2A timeout and retry

- **WHEN** A2AAgent.Run fails with network error after 120s timeout
- **THEN** Call waits 500ms and retries once
- **AND** if retry succeeds, returns result normally

#### Scenario: Local TagentAgent failure no retry

- **WHEN** TagentAgent.Run returns error
- **THEN** Call returns error immediately without retry
