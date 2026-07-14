## ADDED Requirements

### Requirement: EventBus.TryPull 非阻塞批量读取

EventBus SHALL 提供非阻塞的 `TryPull() []*AgentEvent` 方法，读取所有 pending 事件但不阻塞等待。与 `Pull` 的批量 drain 逻辑一致，但如果 channel 为空立即返回空 slice。

#### Scenario: 有 pending 事件时批量读取

- **WHEN** EventBus 中有 3 个 pending 事件
- **THEN** TryPull 返回 3 个事件
- **AND** EventBus 为空

#### Scenario: 无 pending 事件时返回空

- **WHEN** EventBus 中无 pending 事件
- **THEN** TryPull 返回空 slice（非 nil）
- **AND** 不阻塞

### Requirement: InjectBusInputs BeforeModel 回调

ContextManager SHALL 在 BeforeModel 回调链最前面注册 `InjectBusInputs` 回调。该回调在每次 LLM 调用前 TryPull EventBus 中的新用户消息，追加到 `args.Request.Messages`。

过滤规则：只处理 `Type == external_input` 且 `Source` 不是 `agent_output`/`error`/`tool_result` 的事件。

#### Scenario: ReAct 循环中注入新用户消息

- **WHEN** RunFlow 执行期间用户通过 InjectMessage 发送新消息
- **AND** 框架 ReAct 循环在下一次 LLM 调用前触发 BeforeModel
- **THEN** InjectBusInputs TryPull 到用户消息
- **AND** 追加到 args.Request.Messages
- **AND** LLM 在当前 ReAct 迭代中看到新用户消息

#### Scenario: 无新消息时不影响 LLM 请求

- **WHEN** BeforeModel 触发但 EventBus 中无新用户消息
- **THEN** TryPull 返回空
- **AND** args.Request.Messages 不变

#### Scenario: 过滤非用户消息

- **WHEN** EventBus 中有 agent_output echo 或 tool_result 桥接事件
- **THEN** InjectBusInputs 跳过这些事件
- **AND** 只注入真正的用户消息

### Requirement: 注入顺序在 InjectEventKeys 之前

InjectBusInputs SHALL 在 InjectEventKeys 回调之前注册执行，确保注入的用户消息也被注入 event_key 前缀。

#### Scenario: 注入的消息获得 event_key 前缀

- **WHEN** InjectBusInputs 追加用户消息到 args.Request.Messages
- **AND** InjectEventKeys 回调随后执行
- **THEN** 注入的用户消息也获得 [evt_KEY|type] 前缀（如果 SessionProjection 有对应引用）
