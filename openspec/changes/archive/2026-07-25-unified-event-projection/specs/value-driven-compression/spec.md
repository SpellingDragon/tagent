## ADDED Requirements

### Requirement: 摘要保留关联标识文本

压缩生成摘要（LLM 摘要或规则截断）时，SHALL 保留被压缩事件中的关联标识文本（task id、工具名），使"通知 ↔ 调用"等跨事件关联在压缩后仍可于内容上建立。压缩边界 SHALL NOT 因工具配对而被特殊处理（事件彼此独立，无配对约束）。

#### Scenario: 压缩后通知仍可关联到调用

- **WHEN** 含 ack（带 task id）的窗口被压缩为摘要，后续 turn 收到同 id 的 task_settled 通知
- **THEN** 摘要文本 SHALL 保留该 task id
- **AND** 模型能从摘要与通知中建立内容级关联

#### Scenario: 压缩边界不做配对特殊处理

- **WHEN** 压缩规划选择压缩窗口
- **THEN** 窗口边界 SHALL 仅由值密度/预算/保留窗口决定
- **AND** SHALL NOT 因"保持 tool_call 与 result 相邻"而调整边界
