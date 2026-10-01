## REMOVED Requirements

### Requirement: InjectBusInputs converts RoleSystem to RoleUser

**Reason**: 中途认领 bus 输入与固定批次的唯一消费边界冲突；主规格中同名重复条款表达同一旧机制，均由新要求取代，不据此恢复已移除的中途消费。

**Migration**: 在冻结批次准备 canonical 消息时复制并归一化 system→user，源事件保持不变；执行中到达的异步结果留到下一轮。仅替换这组角色/消费条款，不修改其余压缩与命令生命周期历史要求。正常归档前在副本验证两处旧重复条款均被替代，不在提案阶段同步主规格。

## ADDED Requirements

### Requirement: 冻结批次消费时归一化外部消息角色

异步系统注入 SHALL 作为外部输入在下一次有限 Pull 中认领；准备 canonical fact 及实际请求时复制 Message，将 system role 转为 user，不提升为模型系统指令。原始 source_event、Content、完整消息及来源 SHALL 保留；原为 user 的消息不改变语义。同步工具结果仍由框架处理当前 ReAct，MUST NOT 因 bus 固定批次改为延后。

#### Scenario: 异步任务结果执行中到达
- **WHEN** 当前 turn 执行中出现 system-role 异步任务结果
- **THEN** 原件留队到下一批，消费时使用 user-role 副本，当前轮不认领它

#### Scenario: 原始消息不变
- **WHEN** system 消息经历准备、提交与重启
- **THEN** 原 source_event role 仍为 system，canonical/模型侧为 user，内容和来源不被原地改写

#### Scenario: 普通用户与同步工具结果
- **WHEN** 处理 user 输入或当前 ReAct 的同步工具结果
- **THEN** user 语义保持，同步工具结果照常参与当前轮，不受异步 bus 边界影响
