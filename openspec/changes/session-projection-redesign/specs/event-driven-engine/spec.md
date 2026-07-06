## ADDED Requirements

### Requirement: AgentLoop SHALL read and write the same Session.Events projection
AgentLoop 不得维护独立的 session copy。onEvent 追加 EventReference 到 Session.Events，Preprocessor 从同一份 Session.Events 读取。读写操作同一份数据，消除一致性风险。

#### Scenario: No duplicate session copies
- **WHEN** 文档描述 AgentLoop 的事件处理流程
- **THEN** 文档不得出现"AgentLoop 维护自己持有的 session copy"、"al.session.Events 额外追加"等描述，当前实现中的 copy 维护 MUST 标注为实现偏差

### Requirement: AgentLoop event processing order SHALL be clearly documented
AgentLoop.Run 的事件处理步骤 MUST 在文档中清晰描述。设计目标中，tool_use 的 dispatch 在 handleResponse 中发生（与原型 OnEvents 一致：模型返回 tool_calls → dispatch → 结果回写 bus → 下一轮处理）。当前实现中 dispatch 在 Step 1（Pull 后立即执行），文档 MUST 标注此为实现偏差。

#### Scenario: Event processing steps documented without ambiguity
- **WHEN** 文档描述 AgentLoop.Run 的设计目标伪代码
- **THEN** 文档 MUST 说明：Step 1 处理 external_input（onEvent 持久化 + Session 追加 EventReference），Step 2 调用 Preprocessor（从 Session.Events 构建 messages），Step 3 调用 Model，Step 4 处理响应（tool_use → dispatch，final → emit）

#### Scenario: Current implementation deviation documented
- **WHEN** 文档描述 AgentLoop.Run 的当前实现
- **THEN** 文档 MUST 标注：当前实现在 Pull 后立即 dispatch tool_use（Step 1），然后处理 external_input（Step 2），这是实现偏差——设计目标中 dispatch 应在 handleResponse 中发生

### Requirement: All outputs SHALL be written back to EventBus
原型中 OnEvents 的返回值被回写到 eventBus（`if output.EventType != 0 { agent.eventBus <- output }`）。生产中 handleResponse 的输出（tool_use 事件、final response）也回写 bus——tool_use 通过 `bus.Publish`，final 通过 `emitEvent → outputCh`。这确保所有输出走同一路径，与工具结果回写 bus 一致。

#### Scenario: LLM response written back to bus
- **WHEN** 文档描述 handleResponse 的行为
- **THEN** 文档 MUST 说明：tool_calls 响应通过 bus.Publish(tool_use) 回写 bus + dispatch；final 响应通过 emitEvent 发送到 outputCh。两者都经过 onEvent（五步协同）追加到 Session.Events

### Requirement: Batch processing SHALL be documented
AgentLoop.Pull 批量取出所有待处理事件，一轮处理多个事件。原型 DefaultRun 中 `eventLen := len(agent.eventBus)` 后非阻塞取出所有剩余事件。这确保同一轮内的多个事件按顺序追加到 Session.Events，LLM 一次性看到所有新事件。

#### Scenario: Batch processing documented
- **WHEN** 文档描述 Pull 的行为
- **THEN** 文档 MUST 说明：Pull 取出第一个事件后，非阻塞取出所有剩余事件组成 batch，一轮处理多个事件

### Requirement: shouldCallModel SHALL be based on external_input in batch
Preprocessor 的 shouldCallModel 判断只检查当前 batch 中是否包含 external_input 事件。tool_use 事件不触发模型调用。

#### Scenario: tool_use only batch does not trigger model call
- **WHEN** AgentLoop Pull 到一批事件只包含 tool_use（LLM 的工具调用请求）
- **THEN** shouldCallModel 返回 false，AgentLoop 不调用 LLM，只 dispatch 工具

#### Scenario: external_input triggers model call
- **WHEN** AgentLoop Pull 到一批事件包含至少一个 external_input
- **THEN** shouldCallModel 返回 true，AgentLoop 调用 Preprocessor 构建 messages 并调用 LLM

### Requirement: Three-layer model SHALL be documented consistently
所有文档 MUST 统一使用三层模型表述：层1 EventBus AgentEvent（事件流，临时）、层2 Session.Events EventReference[]（投影，有界工作内存）、层3 MemoryStore FullEvent（永久存储，不可变）。

#### Scenario: Layer descriptions consistent across docs
- **WHEN** 审查任何 wiki 或 README 文档中的三层模型描述
- **THEN** 层2 的描述 MUST 包含"投影"和"有界"关键词，不得使用"完整未压缩"
