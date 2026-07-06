## Why

目前 tagent 的 persistent event loop 采用"批量 drain mailbox → mergeBatch → runner.Run()"的同步批处理模型，React Loop 过程中工具调用是 inline 同步执行的，新到达的外部事件必须等整轮 Run 结束才能被消费。这导致事件管线流动受阻，agent 无法在工具执行期间响应新输入。理想情况下，react loop 产出的 tool_use 事件应重新进入事件管线，工具执行完成后以 external_input 事件回写管线，agent loop 按 类型消费事件并决定何时调用 model。当前实现是"一阶段一阶段批量消费"（类似 Spark），而非事件自然流转。

## What Changes

- **新增 EventBus**：per-agent 的统一事件队列，替代现有 `mailbox chan model.Message`。事件类型为 `external_input` 和 `tool_use`（`agent_output` 不进 bus，直接输出 + 写 session）。Producers 包括：用户消息、TmuxMonitor、MeditationManager、子 agent 完成、agent loop 自身产出的 tool_use。
- **新增 AgentLoop**：纯引擎，替代 framework 的 `LLMAgent.flow.Run` / graph 执行模型。职责：pull 事件 batch → 调用 Preprocessor → 按需调用 `model.Model.GenerateContent` → 解析 response（tool_use 发布到 bus，agent_output 直接 emit + 写 session）。AgentLoop 不包含业务语义。
- **升级 ContextIntervention → Preprocessor**：从 model callback 升级为显式的预处理阶段。承担全部业务判断：事件筛选（external_input 需要进 LLM context，tool_use 不需要）、shouldCallModel 判断、token 预算检查、SmartCompress 触发。输出 `[]model.Message + bool`。
- **统一异步 Tool Dispatch**：agent loop 发现 tool_use 后异步分发，不等待结果。**BREAKING**：shell 类工具（action）全部改为 tmux async 模式，移除 exec 同步路径，`tool.Call()` 立即返回 session_id，TmuxMonitor 异步感知状态变化后回写 external_input。子 agent 类工具（knowledge/recall）启动 goroutine 调用 `子agent.Run()`，eventCh 关闭后回写 external_input。
- **移除 LLMAgent / flow / graph 依赖**：tagent 不再使用 framework 的 graph 执行模型，改为自己的 AgentLoop。**BREAKING**：`TagentAgent.Run()` 内部不再委托 `runner.Run()`，但 `TagentAgent` 仍实现 `agent.Agent` 接口，仍被 runner 外壳管理（session/plugin/trace 复用）。
- **保留 framework 复用层**：`model.Model`、`tool.Tool`、`event.Event`、`session.Session`、`plugin.Plugin`（MemoryPlugin/SummaryPlugin）、`agent.Invocation`、`agent.RunWithPlugins` 均保留复用。

## Capabilities

### New Capabilities
- `event-driven-agent-loop`: EventBus + AgentLoop + Preprocessor + 异步 Tool Dispatch 的事件驱动执行模型，替代 framework graph 的同步 React Loop

### Modified Capabilities
- `persistent-event-loop`: 从 mailbox 批量 drain + runner.Run 改为 EventBus 事件驱动 + AgentLoop 消费
- `tool-output-interception`: tool 执行从同步 inline 改为异步 dispatch，结果以 external_input 事件回写 bus

## Impact

- **核心代码变更**：
  - `agent/tagent_agent.go`：重写 `loop()`、`StartLoop()`、`Run()`，新增 EventBus、AgentLoop
  - `agent/context_intervention.go`：升级为 Preprocessor，新增事件筛选 + shouldCallModel 逻辑
  - `agent/tool_agent.go`：AgentToolWrapper.Call 从同步改异步（goroutine + 回写 bus）
  - `tool/action/action_tool.go`：移除 exec 同步路径，全走 tmux async
  - `tool/action/tmux_monitor.go`：状态变化回写从 InjectMessage 改为 publish EventBus
  - `agent/meditation_manager.go`：注入方式从 mailbox 改为 EventBus
- **移除的依赖**：`LLMAgent`、`llmflow`、`graph` 包（仅在 tagent 内不再引用，framework 包保留）
- **保留复用**：`runner.Runner`（外壳）、`session.Service`、`plugin.PluginManager`、`model.Model`、`tool.Tool`、`event.Event`、`agent.Invocation`
- **测试重写**：依赖 `runner.Run()` / `mockRunner` 的测试需要适配 AgentLoop
- **A2A 兼容**：`a2a_server.go` 入口 message 转为 bus 首个事件，agent loop 接管
