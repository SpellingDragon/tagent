## ADDED Requirements

### Requirement: SmartCompressor and Compact SHALL have clearly separated responsibilities
SmartCompressor 压缩 `[]model.Message`（发给 LLM 的视图），不修改 Session.Events。Compact 清理 Session.Events（移除旧 EventReference，替换为 summary reference），不修改 MemoryStore。两者职责分离，不可混淆。

#### Scenario: SmartCompressor modifies messages only
- **WHEN** SmartCompressor 执行压缩
- **THEN** 仅修改局部 `messages` 变量（发给 LLM 的视图），Session.Events 和 MemoryStore 均不被修改

#### Scenario: Compact modifies Session.Events only
- **WHEN** Compact 执行清理
- **THEN** 修改 Session.Events（移除旧 EventReference，替换为 summary reference），MemoryStore 不被修改，Session.Events 保持有界

#### Scenario: Compression and Compact are distinct operations
- **WHEN** 文档描述上下文管理机制
- **THEN** 文档 MUST 使用对比表区分 SmartCompressor（作用对象=messages 视图，触发=token 超阈值，效果=压缩 LLM 看到的消息列表）和 Compact（作用对象=Session.Events 投影，触发=Session 有界性维护，效果=清理旧引用替换为 summary）

### Requirement: View transformation principle SHALL be corrected
"视图转换原则"修正为：SmartCompressor 仅修改 messages 视图（不变）；MemoryStore 永不被运行时操作修改（不变）；Session.Events 可被 Compact 清理（修正）。当前文档 §12.2 错误地把 Session.Events 和 MemoryStore 同时保护。

#### Scenario: Corrected view transformation principle documented
- **WHEN** 文档描述视图转换原则
- **THEN** 文档 MUST 明确区分三者的可变性：messages（SmartCompressor 可修改）、Session.Events（Compact 可清理）、MemoryStore（永不可变）

### Requirement: Compact trigger and strategy SHALL be documented
文档 MUST 定义 Compact 的触发条件、清理策略、与 SmartCompressor 的协作顺序、与 onEvent 持久化的时序关系。

#### Scenario: Compact collaboration with SmartCompressor documented
- **WHEN** 文档描述 token 超阈值时的处理
- **THEN** 文档 MUST 说明：先执行 SmartCompress（压缩 messages 视图），如果仍超限则执行 Compact（清理 Session.Events 投影）

#### Scenario: Compact temporal relationship with onEvent documented
- **WHEN** 文档描述 Compact 与 onEvent 的时序关系
- **THEN** 文档 MUST 说明：onEvent 在每次事件到来时实时持久化 FullEvent 到 MemoryStore；Compact 在投影满时清理 Session.Events 中的旧引用；两者不是同一时机——onEvent 是实时的每事件一次，Compact 是批量的投影满时触发；Compact 清理投影时不丢数据

#### Scenario: Compact strategy documented
- **WHEN** 文档描述 Compact 的清理策略
- **THEN** 文档 MUST 说明：按任务边界切分 Session.Events，保留最近 N 个完整任务的 EventReference，旧引用替换为 summary reference（含压缩的 EventKey 列表）

### Requirement: Compact SHALL be primary control valve, MaxToolIterations SHALL be fallback
Compact 是主要控制阀门（Session 投影有界性），MaxToolIterations 是兜底（Compact 后 LLM 仍无法收敛时强制中断）。

#### Scenario: StartLoop mode uses Compact as sole control
- **WHEN** 文档描述 StartLoop 持久循环模式的控制策略
- **THEN** 文档 MUST 说明：Compact 是唯一控制阀门，Session.Events 超限时 Compact 清理投影，MaxToolIterations 不需要或设很大

#### Scenario: Run() mode uses Compact + MaxToolIterations together
- **WHEN** 文档描述 Run() 子 agent 模式的控制策略
- **THEN** 文档 MUST 说明：Compact 保持 Session 有界，MaxToolIterations 作为兜底中断（默认 10），复用框架 Invocation.MaxToolIterations 字段

#### Scenario: Current implementation deviation documented
- **WHEN** 文档描述当前实现的迭代控制
- **THEN** 文档 MUST 标注：当前无 Compact，MaxToolIterations=200 被当成唯一控制阀门，这是实现偏差
## ADDED Requirements

### Requirement: SmartCompressor and Compact SHALL have clearly separated responsibilities
SmartCompressor 压缩 `[]model.Message`（发给 LLM 的视图），不修改 Session.Events。Compact 清理 Session.Events（移除旧 EventReference，替换为 summary reference），不修改 MemoryStore。两者职责分离，不可混淆。

#### Scenario: SmartCompressor modifies messages only
- **WHEN** SmartCompressor 执行压缩
- **THEN** 仅修改局部 `messages` 变量（发给 LLM 的视图），Session.Events 和 MemoryStore 均不被修改

#### Scenario: Compact modifies Session.Events only
- **WHEN** Compact 执行清理
- **THEN** 修改 Session.Events（移除旧 EventReference，替换为 summary reference），MemoryStore 不被修改，Session.Events 保持有界

#### Scenario: Compression and Compact are distinct operations
- **WHEN** 文档描述上下文管理机制
- **THEN** 文档 MUST 使用对比表区分 SmartCompressor（作用对象=messages 视图，触发=token 超阈值，效果=压缩 LLM 看到的消息列表）和 Compact（作用对象=Session.Events 投影，触发=Session 有界性维护，效果=清理旧引用替换为 summary）

### Requirement: View transformation principle SHALL be corrected
"视图转换原则"修正为：SmartCompressor 仅修改 messages 视图（不变）；MemoryStore 永不被运行时操作修改（不变）；Session.Events 可被 Compact 清理（修正）。当前文档 §12.2 错误地把 Session.Events 和 MemoryStore 同时保护。

#### Scenario: Corrected view transformation principle documented
- **WHEN** 文档描述视图转换原则
- **THEN** 文档 MUST 明确区分三者的可变性：messages（SmartCompressor 可修改）、Session.Events（Compact 可清理）、MemoryStore（永不可变）

### Requirement: Compact trigger and strategy SHALL be documented
文档 MUST 定义 Compact 的触发条件和清理策略。

#### Scenario: Compact trigger condition documented
- **WHEN** 文档描述 Compact 的触发时机
- **THEN** 文档 MUST 说明：token(Session.Events 对应的 messages) > threshold 时，先执行 SmartCompress（压缩视图），如果仍超限则执行 Compact（清理投影）

#### Scenario: Compact strategy documented
- **WHEN** 文档描述 Compact 的清理策略
- **THEN** 文档 MUST 说明：按任务边界切分 Session.Events，保留最近 N 个完整任务的 EventReference，旧引用替换为 summary reference（含压缩的 EventKey 列表）
