## ADDED Requirements

### Requirement: EventBus 统一事件队列

每个 TagentAgent 实例 SHALL 拥有一个 per-agent 的 EventBus。EventBus 是一个有序事件队列，agent loop 是其唯一消费者。EventBus SHALL 支持两种事件类型作为触发器：`external_input` 和 `tool_use`。`agent_output` 不进入 bus。

EventBus SHALL 提供 `Publish(event *AgentEvent)` 和 `Pull(ctx context.Context) ([]*AgentEvent, error)` 方法。Pull SHALL 阻塞直到至少一个事件可用，然后 non-blocking 地取出所有剩余 pending 事件。

#### Scenario: 外部输入发布到 bus

- **WHEN** 用户调用 InjectMessage 或 TmuxMonitor 检测到状态变化
- **THEN** 一个 `external_input` 类型的 AgentEvent 被发布到 bus
- **AND** 该事件包含 model.Message 载荷和 source 标识

#### Scenario: tool_use 发布到 bus

- **WHEN** agent loop 从 model response 解析出 tool_calls
- **THEN** 对每个 tool_call，一个 `tool_use` 类型的 AgentEvent 被发布到 bus
- **AND** 该事件包含 model.ToolCall 载荷

#### Scenario: agent_output 不进入 bus

- **WHEN** agent loop 从 model response 解析出最终响应（无 tool_calls）
- **THEN** 该响应直接 emit 到 outputCh
- **AND** 该响应写入 session（通过 MemoryPlugin.OnEvent）
- **AND** 该响应不作为 AgentEvent 发布到 bus

#### Scenario: Pull 批量获取事件

- **WHEN** bus 中有多个 pending 事件，agent loop 调用 Pull
- **THEN** 返回所有 pending 事件的切片
- **AND** bus 被清空

#### Scenario: Pull 阻塞等待首个事件

- **WHEN** bus 为空，agent loop 调用 Pull
- **THEN** Pull 阻塞直到至少一个事件到达或 ctx 被取消

### Requirement: AgentLoop 纯引擎循环

AgentLoop SHALL 是一个不含业务语义的纯引擎循环。每轮迭代执行：Pull 事件 batch → 调用 Preprocessor → 按 shouldCallModel 决定是否调用 model → 解析 response → 发布 tool_use 或 emit agent_output → 异步分发 tool_use → 继续下一轮。

AgentLoop SHALL NOT 包含事件筛选逻辑、shouldCallModel 判断逻辑、token 预算检查或压缩逻辑。这些职责属于 Preprocessor。

#### Scenario: 有 external_input 触发 model 调用

- **WHEN** agent loop pull 到包含 external_input 的事件 batch
- **AND** Preprocessor 返回 shouldCallModel=true
- **THEN** agent loop 调用 model.GenerateContent
- **AND** 解析 response

#### Scenario: 只有 tool_use 不触发 model 调用

- **WHEN** agent loop pull 到只包含 tool_use 的事件 batch
- **AND** Preprocessor 返回 shouldCallModel=false
- **THEN** agent loop 不调用 model
- **AND** agent loop 异步分发 tool_use
- **AND** 回到 idle 等待下一轮

#### Scenario: model 返回 tool_calls

- **WHEN** model.GenerateContent 返回包含 tool_calls 的 response
- **THEN** agent loop 对每个 tool_call 发布 tool_use 事件到 bus
- **AND** 异步分发 tool_use（不等待工具执行完成）
- **AND** 回到 idle 等待工具结果以 external_input 回写

#### Scenario: model 返回最终响应

- **WHEN** model.GenerateContent 返回不包含 tool_calls 的 response
- **THEN** agent loop 将响应 emit 到 outputCh
- **AND** 通过 MemoryPlugin.OnEvent 写入 session
- **AND** 不发布 agent_output 到 bus
- **AND** 回到 idle 等待下一个 external_input

### Requirement: Preprocessor 显式预处理阶段

Preprocessor SHALL 在 agent loop 调用 model 之前被显式调用。Preprocessor 接收 `[]*AgentEvent` 输入，输出 `([]model.Message, bool)`，其中 bool 表示是否应该调用 model。

Preprocessor SHALL 执行以下步骤：事件筛选（external_input 进入 LLM context，tool_use 不进入）、shouldCallModel 判断（有 external_input → true，只有 tool_use → false）、构造 model.Message 数组、token 预算检查、SmartCompress 触发。

#### Scenario: external_input 事件触发 model 调用

- **WHEN** Preprocessor 接收到包含 external_input 的事件 batch
- **THEN** 返回 shouldCallModel=true
- **AND** 返回构造好的 []model.Message 包含 external_input 内容

#### Scenario: 只有 tool_use 不触发 model 调用

- **WHEN** Preprocessor 接收到只包含 tool_use 的事件 batch
- **THEN** 返回 shouldCallModel=false
- **AND** 返回空的 []model.Message

#### Scenario: token 超限触发压缩

- **WHEN** Preprocessor 构造的 messages token 数超过阈值
- **THEN** 触发 SmartCompress 压缩旧消息
- **AND** 返回压缩后的 []model.Message

### Requirement: 异步 Tool Dispatch

Agent loop 发现 tool_use 后 SHALL 异步分发，不等待工具执行完成。分发方式按工具类型 if-else 判断：

Shell 类工具（action）SHALL 调用 `tool.Call()` 立即返回 session_id，TmuxMonitor 异步感知状态变化后回写 external_input 到 bus。

子 agent 类工具（knowledge/recall）SHALL 启动 goroutine 调用子 agent 的 Run 方法，goroutine 等待 eventCh 关闭后回写 external_input 到 bus。

两种路径的回写方式统一：都构造 external_input 类型的 AgentEvent 发布到 bus。

#### Scenario: shell 工具异步执行

- **WHEN** agent loop 分发一个 action tool_use
- **THEN** 调用 tool.Call() 立即返回 session_id + status:running
- **AND** agent loop 不等待，回到 idle
- **AND** TmuxMonitor 异步监控状态变化
- **AND** 命令完成后回写 external_input（含 stdout/exit_code）到 bus

#### Scenario: 子 agent 异步执行

- **WHEN** agent loop 分发一个 knowledge tool_use
- **THEN** 启动 goroutine 调用子 agent.Run()
- **AND** agent loop 不等待，回到 idle
- **AND** goroutine 等待 eventCh 关闭
- **AND** 收集最终输出后回写 external_input 到 bus

#### Scenario: 工具执行结果触发下一轮

- **WHEN** 工具执行完成后 external_input 被发布到 bus
- **THEN** 触发 agent loop 下一轮 Pull
- **AND** Preprocessor 将该 external_input 纳入 LLM context
- **AND** model 在下一轮看到工具执行结果

### Requirement: 子 agent 事件管线隔离

子 agent SHALL 拥有自己独立的 EventBus、AgentLoop 和 Preprocessor，与父 agent 的事件管线完全隔离。父 agent 只看到 Tool Dispatch goroutine 返回的最终 external_input。子 agent 内部的事件不会泄漏到父 agent 的 bus。

#### Scenario: 子 agent 内部事件不泄漏

- **WHEN** 子 agent 在执行过程中产生 thinking_plan、thinking_recall 等内部事件
- **THEN** 这些事件只存在于子 agent 自己的 bus 中
- **AND** 父 agent 的 bus 不收到这些事件

#### Scenario: 父 agent 只收到子 agent 最终结果

- **WHEN** 子 agent 执行完成
- **THEN** 父 agent 的 bus 收到一个 external_input 事件
- **AND** 该事件包含子 agent 的最终输出
- **AND** 不包含子 agent 的中间过程
