# compression-user-message Specification

## Purpose
TBD - created by archiving change production-readiness-fix. Update Purpose after archive.
## Requirements
### Requirement: 压缩后保留未完成的 User message

SmartCompressor.Compress 在替换 oldSegments 后，SHALL 检查压缩后的消息列表中是否存在最后一个 agent_output（assistant 无 tool calls）之后的 user message。如果存在，SHALL 保留该 user message 作为 LLM 的驱动输入。

#### Scenario: 有未完成任务时保留 pending user

- **WHEN** 压缩前的消息序列为 [User:"Task1", Assistant:"Done1", User:"Task2(pending)"]
- **AND** SmartCompress 压缩掉 Task1，保留 Task2
- **THEN** 压缩后的消息序列为 [System:"摘要...", User:"Task2(pending)"]
- **AND** 最后一条消息是 user role，内容为 "Task2(pending)"

#### Scenario: 所有任务完成时保留引导消息

- **WHEN** 压缩前的消息序列为 [User:"Task1", Assistant:"Done1", User:"Task2", Assistant:"Done2"]
- **AND** SmartCompress 压缩掉 Task1 和 Task2
- **THEN** 压缩后的消息序列为 [System:"摘要...", User:"（以上是对话历史摘要。如果有新任务，请告诉我。）"]
- **AND** 最后一条消息是 user role

### Requirement: ensureUserPrompt 使用 pending user 策略

ContextIntervention 的 ensureUserPrompt 函数 SHALL 替换为 pending user 策略：先查找压缩后消息中是否存在 user role 消息，如果存在则不添加任何消息；如果不存在，添加引导消息。不再使用硬编码的"继续"。

#### Scenario: 压缩后已有 user message 时不添加

- **WHEN** 压缩后的消息列表中已包含 user role 消息
- **THEN** 不添加任何额外消息
- **AND** 消息列表不变

#### Scenario: 压缩后无 user message 时添加引导消息

- **WHEN** 压缩后的消息列表中不包含 user role 消息
- **THEN** 添加 user role 消息，内容为 "（以上是对话历史摘要。如果有新任务，请告诉我。）"
- **AND** 不添加 "继续" 消息

### Requirement: 摘要使用 System role

SmartCompressor 生成的摘要事件 SHALL 使用 System role（model.RoleSystem），不使用 User role。摘要作为上下文信息，不作为用户输入。

#### Scenario: 摘要事件 role 为 System

- **WHEN** SmartCompressor 生成摘要事件
- **THEN** 摘要事件的 Message.Role == model.RoleSystem
- **AND** 摘要事件的 Message.Content 包含 "对话历史摘要" 前缀

