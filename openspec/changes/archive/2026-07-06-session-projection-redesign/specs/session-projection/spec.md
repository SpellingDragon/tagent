## ADDED Requirements

### Requirement: Session.Events SHALL be a projection of event flow
Session.Events 是 event flow 的投影（有界工作内存），不是完整事件存储。Session 中每个条目是 EventReference（key + type + summary），完整数据存储在 MemoryStore。

#### Scenario: Session stores EventReference not full events
- **WHEN** AgentLoop 追加一个新事件到 Session.Events
- **THEN** Session.Events 中新增一个 EventReference（含 EventKey、EventType、EventSummary），不含完整 Content/ToolCalls/Response

#### Scenario: Session.Events remains bounded
- **WHEN** Session.Events 中的事件数量或对应 token 量超过阈值
- **THEN** Compact 机制触发，清理旧 EventReference 并替换为 summary reference，Session.Events 保持有界

### Requirement: Compact SHALL clean Session.Events projection without touching MemoryStore
Compact 是 Session.Events 投影的清理机制。它移除旧的 EventReference，替换为 summary reference。MemoryStore 中的 FullEvent 永不被 Compact 修改。

#### Scenario: Compact preserves MemoryStore integrity
- **WHEN** Compact 清理 Session.Events 中的旧 EventReference
- **THEN** MemoryStore 中对应的 FullEvent 保持不变，recall 工具仍可通过 EventKey 查询完整数据

#### Scenario: Compact replaces old references with summary
- **WHEN** Compact 触发，保留最近 N 个完整任务的 EventReference
- **THEN** 被 N 个任务之前的旧 EventReference 被替换为一条 summary reference（含压缩的 EventKey 列表和摘要文本）

### Requirement: onEvent SHALL execute five-step pipeline atomically
onEvent 回调在事件进入 Session 之前完成五步协同：①事件提取（ExtractEventType 推断类型）→ ②记忆写入（MemoryStore 持久化 FullEvent）→ ③因果链（RelationStore.SetParent）→ ④StateDelta 填充（event_key/event_type）→ ⑤投影追加（Session.Events 追加 EventReference）。五步在同一个 onEvent 调用中完成。

#### Scenario: Event extraction infers type from Message.Role
- **WHEN** onEvent 收到一个 event.Event
- **THEN** ExtractEventType 从 Message.Role 推断事件类型（RoleUser → external_input，RoleAssistant+ToolCalls → thinking_plan，RoleAssistant → agent_output，RoleTool → action_command）

#### Scenario: MemoryStore persistence happens before projection append
- **WHEN** onEvent 执行到第⑤步（投影追加）
- **THEN** FullEvent 已在第②步持久化到 MemoryStore，EventKey 已在第④步填入 StateDelta——投影追加的是含完整 StateDelta 的 EventReference

#### Scenario: Session and MemoryStore stay consistent
- **WHEN** onEvent 成功完成
- **THEN** MemoryStore 中有对应的 FullEvent，Session.Events 中有对应的 EventReference，两者的 EventKey 一致

#### Scenario: No separate session copy needed
- **WHEN** Preprocessor 读取 Session.Events 构建 messages
- **THEN** Preprocessor 读取的就是 onEvent 追加的同一份 Session.Events，不需要 AgentLoop 维护独立 copy

### Requirement: Preprocessor SHALL build messages from EventReference on demand
Preprocessor 从 Session.Events 的 EventReference 构建 messages 时，按需从 MemoryStore 拉取完整内容——最近的引用拉取 FullEvent.Content，旧的引用直接用 EventSummary。

#### Scenario: Recent references fetch full content from MemoryStore
- **WHEN** Preprocessor 构建 messages，遇到最近 N 个任务的 EventReference
- **THEN** 从 MemoryStore.GetEvent(key) 拉取完整 FullEvent，使用其 Content 字段构建 model.Message

#### Scenario: Old references use summary directly
- **WHEN** Preprocessor 构建 messages，遇到被 Compact 替换的 summary reference
- **THEN** 直接使用 EventSummary 作为 message content，不从 MemoryStore 拉取

### Requirement: Compact and onEvent persistence SHALL have correct temporal relationship
onEvent 在每次事件到来时实时持久化 FullEvent 到 MemoryStore。Compact 在投影满时清理 Session.Events 中的旧引用。两者不是同一时机——onEvent 是实时的、每事件一次；Compact 是批量的、投影满时触发。Compact 清理投影时不丢数据，因为完整数据已在 onEvent 时持久化。

#### Scenario: Compact does not lose data
- **WHEN** Compact 从 Session.Events 移除旧 EventReference
- **THEN** 对应的 FullEvent 仍在 MemoryStore 中（onEvent 时已写入），recall 工具仍可查询

### Requirement: Session.Events design identity SHALL be documented consistently across all wiki and README
所有文档中 Session.Events 的描述 MUST 统一为"投影（有界工作内存）"，不得出现"完整未压缩的对话历史"等矛盾表述。

#### Scenario: No contradictory Session.Events descriptions
- **WHEN** 审查任何 wiki 或 README 文档中关于 Session.Events 的描述
- **THEN** 描述 MUST 使用"投影"、"有界工作内存"、"EventReference"等术语，不得使用"完整事件"、"完整未压缩"等表述

### Requirement: Current implementation deviation SHALL be documented as known technical debt
当前生产代码中 Session.Events 存储 `[]event.Event`（完整事件含 `*model.Response`），而非 EventReference。文档 MUST 明确标注此为实现偏差，设计目标是 EventReference。

#### Scenario: Implementation deviation explicitly documented
- **WHEN** 文档描述 Session.Events 的存储类型
- **THEN** 文档 MUST 同时标注"设计目标：EventReference[]"和"当前实现：[]event.Event（已知偏差）"
## ADDED Requirements

### Requirement: Session.Events SHALL be a projection of event flow
Session.Events 是 event flow 的投影（有界工作内存），不是完整事件存储。Session 中每个条目是 EventReference（key + type + summary），完整数据存储在 MemoryStore。

#### Scenario: Session stores EventReference not full events
- **WHEN** AgentLoop 追加一个新事件到 Session.Events
- **THEN** Session.Events 中新增一个 EventReference（含 EventKey、EventType、EventSummary），不含完整 Content/ToolCalls/Response

#### Scenario: Session.Events remains bounded
- **WHEN** Session.Events 中的事件数量或对应 token 量超过阈值
- **THEN** Compact 机制触发，清理旧 EventReference 并替换为 summary reference，Session.Events 保持有界

### Requirement: Compact SHALL clean Session.Events projection without touching MemoryStore
Compact 是 Session.Events 投影的清理机制。它移除旧的 EventReference，替换为 summary reference。MemoryStore 中的 FullEvent 永不被 Compact 修改。

#### Scenario: Compact preserves MemoryStore integrity
- **WHEN** Compact 清理 Session.Events 中的旧 EventReference
- **THEN** MemoryStore 中对应的 FullEvent 保持不变，recall 工具仍可通过 EventKey 查询完整数据

#### Scenario: Compact replaces old references with summary
- **WHEN** Compact 触发，保留最近 N 个完整任务的 EventReference
- **THEN** 被 N 个任务之前的旧 EventReference 被替换为一条 summary reference（含压缩的 EventKey 列表和摘要文本）

### Requirement: onEvent SHALL write EventReference to Session and FullEvent to MemoryStore atomically
onEvent 回调在事件进入 Session 之前完成两件事：持久化 FullEvent 到 MemoryStore（durability），追加 EventReference 到 Session.Events（投影更新）。这两步在同一个 onEvent 调用中完成，保证 MemoryStore 与 Session 的一致性。

#### Scenario: Session and MemoryStore stay consistent
- **WHEN** onEvent 成功完成
- **THEN** MemoryStore 中有对应的 FullEvent，Session.Events 中有对应的 EventReference，两者的 EventKey 一致

#### Scenario: No separate session copy needed
- **WHEN** Preprocessor 读取 Session.Events 构建 messages
- **THEN** Preprocessor 读取的就是 onEvent 追加的同一份 Session.Events，不需要 AgentLoop 维护独立 copy

### Requirement: Session.Events design identity SHALL be documented consistently across all wiki and README
所有文档（README.md、README_EN.md、docs/wiki/agent/、docs/wiki/event/、docs/wiki/memory/）中 Session.Events 的描述 MUST 统一为"投影（有界工作内存）"，不得出现"完整未压缩的对话历史"等矛盾表述。

#### Scenario: No contradictory Session.Events descriptions
- **WHEN** 审查任何 wiki 或 README 文档中关于 Session.Events 的描述
- **THEN** 描述 MUST 使用"投影"、"有界工作内存"、"EventReference"等术语，不得使用"完整事件"、"完整未压缩"等表述

### Requirement: Current implementation deviation SHALL be documented as known technical debt
当前生产代码中 Session.Events 存储 `[]event.Event`（完整事件含 `*model.Response`），而非 EventReference。文档 MUST 明确标注此为实现偏差，设计目标是 EventReference。

#### Scenario: Implementation deviation explicitly documented
- **WHEN** 文档描述 Session.Events 的存储类型
- **THEN** 文档 MUST 同时标注"设计目标：EventReference[]"和"当前实现：[]event.Event（已知偏差）"
