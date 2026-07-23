## ADDED Requirements

### Requirement: Sub-agent 调用超时对多轮工作宽容

`AgentToolWrapper` 对 sub-agent 调用施加的超时（`defaultSubAgentTimeout`）SHALL 足够宽容，以容纳 sub-agent 正常的多轮 ReAct 工作（如 plan create 依次执行自检、init、new change、写多个 artifact、validate）。超时 SHALL NOT 在 sub-agent 正常推进多轮工具调用时将其截断；其真实工作上界由各 agent 的 `max_tool_iterations` 决定，超时仅作为对真正失控（runaway/挂死）调用的兜底。

`defaultSubAgentTimeout` SHALL 不小于 600 秒。

#### Scenario: 多轮创建流程不被超时截断

- **WHEN** plan agent 执行需要多轮工具调用的 create 流程（自检 → init → new change → 写 proposal.md → 写 tasks.md → validate）
- **AND** 使用较慢的 LLM（如 glm-5.2 每轮约 15–25s）
- **THEN** sub-agent 调用 SHALL 在超时内完成，不被 `defaultSubAgentTimeout` 中途取消

#### Scenario: 超时仅作 runaway 兜底

- **WHEN** sub-agent 达到其 `max_tool_iterations` 上界
- **THEN** sub-agent SHALL 因迭代上界正常结束，而非依赖超时
- **AND** `defaultSubAgentTimeout` 仅在调用真正挂死时兜底触发
