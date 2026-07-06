## ADDED Requirements

### Requirement: Sub-agent SHALL have Compact as primary control and MaxToolIterations as fallback
子 agent 的迭代控制以 Compact 为主（Session 投影有界性），MaxToolIterations 为辅（Compact 后 LLM 仍无法收敛时的兜底中断）。默认值 10，复用框架 Invocation.MaxToolIterations 字段。当前生产代码默认 200 且无 Compact，标注为实现偏差。

#### Scenario: Sub-agent iteration limit prevents infinite loops
- **WHEN** 子 agent 的工具调用次数达到 MaxToolIterations（默认 10）
- **THEN** AgentLoop 强制返回最终响应，中断工具调用循环

#### Scenario: MaxToolIterations configurable per agent
- **WHEN** 用户在 YAML 配置中为子 agent 指定 max_tool_iterations
- **THEN** 该值覆盖默认值 10，允许复杂场景提高限制

### Requirement: Sub-agent Session.Events SHALL also be bounded and Compactable
子 agent 在执行期间创建独立的 Session，其 Session.Events 也有界。Compact 同样适用于子 agent 的 Session。

#### Scenario: Sub-agent session does not grow unbounded
- **WHEN** 子 agent 执行期间 Session.Events 的 token 量超过子 agent 的 MaxTokens * CompressThreshold
- **THEN** Compact 触发，清理子 agent 的旧 EventReference，子 agent Session 保持有界

### Requirement: Sub-agent tool dispatch SHALL be unified
所有工具——CallableTool（file、speak）和 AgentToolWrapper（knowledge、recall、action）——通过统一的 `dispatchToolUse` 分发：一个 goroutine、一个 recover、一个超时。文档不得区分 callable/subagent 两条路径。

#### Scenario: No type-assertion dispatch branch documented
- **WHEN** 文档描述工具分发机制
- **THEN** 文档 MUST 说明 AgentToolWrapper 实现了 CallableTool 接口，不需要类型判断分支，统一走 dispatchToolUse 单一路径

### Requirement: Sub-agent stop condition SHALL only check tool_calls
子 agent 的停止条件只检查 `len(choice.Message.ToolCalls) == 0`，不检查 Content 是否为空。空内容的 final response 仍然是 final response。

#### Scenario: Empty final response stops sub-agent
- **WHEN** 子 agent 的 LLM 返回无 tool_calls 的响应（即使 Content 为空）
- **THEN** 子 agent 停止，返回最终输出（可能为空或 reasoning_content fallback 结果）

### Requirement: model and tool mapping SHALL be documented
原型中 `tools["model"] = ModelCompletion`——model 是工具的一种，走同一个调用路径。生产中 model 独立为 `model.Model.GenerateContent`，因为 trpc-agent-go 的 `model.Model` 接口（GenerateContent 返回 streaming channel）与 `tool.CallableTool` 接口（Call 返回同步结果）不同。文档 MUST 说明这个映射——model 独立不是偏离原型，而是框架接口差异导致的形态变化。本质一致：AgentLoop 是唯一的调用者。

#### Scenario: model-to-tool mapping documented
- **WHEN** 文档描述 model 与 tool 的关系
- **THEN** 文档 MUST 说明：原型中 model 注册为 tool（`tools["model"]`），生产中 model 独立因框架接口差异，但 AgentLoop 统一调用 model 和 dispatch tool 的本质与原型一致

### Requirement: Sub-agent execution pipeline SHALL be documented for knowledge and action
文档 MUST 包含 knowledge 和 action 两种子 agent 的完整执行管线图，展示从父 agent dispatch 到子 agent 返回的完整流程。

#### Scenario: Knowledge pipeline documented
- **WHEN** 文档描述 knowledge 子 agent 的执行
- **THEN** 文档 MUST 展示：父 dispatch → AgentToolWrapper.Call → agent.Run → invBus + invAgentLoop → skill_search → skill_load → final response → 回写父 bus

#### Scenario: Action pipeline with tmux async documented
- **WHEN** 文档描述 action 子 agent 的执行（含 tmux 异步）
- **THEN** 文档 MUST 展示：activeBus 切换到 invBus → TmuxMonitor 回调通过 InjectMessage 路由到 invBus → 子 agent 收到 tmux 结果 → LLM 处理
## ADDED Requirements

### Requirement: Sub-agent SHALL have bounded MaxToolIterations
子 agent（knowledge、recall、action）的 MaxToolIterations 默认值 MUST 为 10，而非 200。主 agent 的默认值为 50。文档 MUST 标注当前生产代码默认 200 为实现偏差。

#### Scenario: Sub-agent iteration limit prevents infinite loops
- **WHEN** 子 agent 的工具调用次数达到 MaxToolIterations（默认 10）
- **THEN** AgentLoop 强制返回最终响应，中断工具调用循环

#### Scenario: MaxToolIterations configurable per agent
- **WHEN** 用户在 YAML 配置中为子 agent 指定 max_tool_iterations
- **THEN** 该值覆盖默认值 10，允许复杂场景提高限制

### Requirement: Sub-agent Session.Events SHALL also be bounded and Compactable
子 agent 在执行期间创建独立的 Session，其 Session.Events 也有界。Compact 同样适用于子 agent 的 Session。

#### Scenario: Sub-agent session does not grow unbounded
- **WHEN** 子 agent 执行期间 Session.Events 的 token 量超过子 agent 的 MaxTokens * CompressThreshold
- **THEN** Compact 触发，清理子 agent 的旧 EventReference，子 agent Session 保持有界

### Requirement: Sub-agent tool dispatch SHALL be unified
所有工具——CallableTool（file、speak）和 AgentToolWrapper（knowledge、recall、action）——通过统一的 `dispatchToolUse` 分发：一个 goroutine、一个 recover、一个超时。文档不得区分 callable/subagent 两条路径。

#### Scenario: No type-assertion dispatch branch documented
- **WHEN** 文档描述工具分发机制
- **THEN** 文档 MUST 说明 AgentToolWrapper 实现了 CallableTool 接口，不需要类型判断分支，统一走 dispatchToolUse 单一路径

### Requirement: Sub-agent stop condition SHALL only check tool_calls
子 agent 的停止条件只检查 `len(choice.Message.ToolCalls) == 0`，不检查 Content 是否为空。空内容的 final response 仍然是 final response。

#### Scenario: Empty final response stops sub-agent
- **WHEN** 子 agent 的 LLM 返回无 tool_calls 的响应（即使 Content 为空）
- **THEN** 子 agent 停止，返回最终输出（可能为空或 reasoning_content fallback 结果）

### Requirement: Sub-agent execution pipeline SHALL be documented for knowledge and action
文档 MUST 包含 knowledge 和 action 两种子 agent 的完整执行管线图，展示从父 agent dispatch 到子 agent 返回的完整流程。

#### Scenario: Knowledge pipeline documented
- **WHEN** 文档描述 knowledge 子 agent 的执行
- **THEN** 文档 MUST 展示：父 dispatch → AgentToolWrapper.Call → agent.Run → invBus + invAgentLoop → skill_search → skill_load → final response → 回写父 bus

#### Scenario: Action pipeline with tmux async documented
- **WHEN** 文档描述 action 子 agent 的执行（含 tmux 异步）
- **THEN** 文档 MUST 展示：activeBus 切换到 invBus → TmuxMonitor 回调通过 InjectMessage 路由到 invBus → 子 agent 收到 tmux 结果 → LLM 处理
