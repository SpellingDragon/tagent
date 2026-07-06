## ADDED Requirements

### Requirement: AgentToolWrapper 自动注入 event_keys

AgentToolWrapper.Call SHALL 在以下条件全部满足时自动注入 event_keys：
1. LLM 未在工具参数中传递 `event_keys`（数组为空或不存在）
2. `AgentToolWrapper` 配置了 `eventParams`（即 `event_keys` 在 eventParams 列表中）
3. `AgentToolWrapper` 持有非 nil 的 `parentProjection`

自动注入时 SHALL 从 `parentProjection.GetAll()` 取最近 N 个（默认 5）EventKey。EventKey 为 0 的引用 SHALL 被跳过。注入的 event_keys SHALL 通过 `serializeExternalContext` 正常序列化到 `RuntimeState["external_context"]`。

#### Scenario: LLM 未传 event_keys 时自动注入

- **WHEN** LLM 调用 knowledge({request: "搜索天气"}) 且未传 event_keys
- **AND** AgentToolWrapper 配置了 event_params=["event_keys"]
- **AND** parentProjection 有 8 个 EventReference
- **THEN** 自动取最近 5 个 EventKey
- **AND** 通过 parentStore.GetEvent 获取对应 FullEvent
- **AND** 序列化到 RuntimeState["external_context"]
- **AND** 子 Agent 收到外部上下文

#### Scenario: LLM 主动传了 event_keys 时不自动注入

- **WHEN** LLM 调用 recall({request: "之前说的", event_keys: [K1, K3]})
- **THEN** 不执行自动注入
- **AND** 使用 LLM 传递的 event_keys

#### Scenario: parentProjection 为空时不注入

- **WHEN** LLM 未传 event_keys 且 parentProjection 为空（0 个引用）
- **THEN** 不注入任何 event_keys
- **AND** 子 Agent 无外部上下文运行

### Requirement: AgentToolWrapper 持有 parentProjection 引用

AgentToolWrapper SHALL 新增 `parentProjection *SessionProjection` 字段。`NewAgentToolWrapper` SHALL 接受 `parentProjection` 参数。如果 `parentProjection` 为 nil，自动注入功能 SHALL 被禁用（静默跳过，不报错）。

#### Scenario: NewAgentToolWrapper 传入 projection

- **WHEN** 创建 AgentToolWrapper 时传入非 nil 的 parentProjection
- **THEN** 自动注入功能可用

#### Scenario: NewAgentToolWrapper 传入 nil projection

- **WHEN** 创建 AgentToolWrapper 时传入 nil parentProjection
- **THEN** 自动注入功能被禁用
- **AND** 不报错，正常工作
