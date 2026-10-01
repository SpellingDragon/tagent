## REMOVED Requirements

### Requirement: InjectBusInputs converts RoleSystem to RoleUser

**Reason**: 常驻输入改为 turn 开始时一次冻结批次，旧 InjectBusInputs/TryPull 的中途注入条款与该边界冲突；角色安全语义继续保留，不恢复已删除的 InjectBusInputs 函数。

**Migration**: 由事件循环消费冻结批次，在规范化事实/请求副本上进行 RoleSystem→RoleUser 转换，原始消息保持不变；执行中新到事件等待下一批。主规格中该旧标题的重复块归档后均不得残留，其他无关历史条款不在本变更改写。

## ADDED Requirements

### Requirement: 批次输入的角色归一化

当事件循环消费冻结批次时，系统 SHALL 将源消息 RoleSystem 在规范化事实和模型输入副本上转换为 RoleUser，使 action_tool_result 等外部注入内容不被视为系统指令。原始源事件的 role、正文及业务 Metadata MUST 保持不变，durable 往返 SHALL 保留原始载荷；RoleUser 不变。执行中新到的外部任务消息 SHALL 留到下一 turn，不经 BeforeModel 中途拉取。

#### Scenario: action_tool_result 在下一批消费
- **WHEN** 当前 turn 执行时收到 RoleSystem 的 action_tool_result
- **THEN** 当前请求不包含该事件；下一批消费时模型以 RoleUser 看到原正文

#### Scenario: User message not affected
- **WHEN** 冻结批次含 RoleUser 消息
- **THEN** 其规范化 role 和正文不变，按批次规则合并输入

#### Scenario: Original event not mutated
- **WHEN** 源 RoleSystem 消息经历持久化、claim、规范化与模型装配
- **THEN** 源事件仍为 RoleSystem，正文和 Metadata 不被修改；模型输入副本为 RoleUser
