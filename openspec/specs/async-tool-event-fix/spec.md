# async-tool-event-fix Specification

## Purpose

定义异步输入消费的角色归一化与命令native退出证据边界。输入原件、canonical事实和模型请求分别保持可核对身份；输出稳定、等待条件、进程死亡不再互相替代，防止长任务被提前完成或失去后续结算。
## Requirements
### Requirement: 冻结批次消费时归一化外部消息角色

系统注入的外部输入 SHALL 在下一次有限拉取中被认领（非唤醒式排空＋durable 认领，不阻塞事件循环）。准备 canonical fact 与实际请求消息时 MUST 复制 Message 再把 `model.RoleSystem` 转为 `model.RoleUser`，不得原地改写原件，也不得把系统注入提升为模型系统指令。原始 source_event、Content、完整消息与来源 SHALL 保持不变；本就是 user 的消息语义不变。同步工具结果仍由框架在当前 ReAct 中处理，MUST NOT 因总线的批次边界而延后。

> 承载坐标（现行码面）：消费侧 `agent/context_manager.go` 的 BeforeModel 回调（`msg := *evt.Message` 后按角色转换，日志串仍作 `[InjectBusInputs]`）；canonical 侧同文件的 `buildBusFact`（注释明示"durable fact 的 canonical 形态只决定一次，重放不重推"）；注入源头 `agent/inject.go` 的 `EmitSystemAlert` 仍以 `RoleSystem` 经 `NewExternalInputEvent` 入总线；行为钉测 `tests/inject_bus_inputs_test.go`。**名为 `InjectBusInputs` 的函数已不存在，但本命题的行为仍在——不得据函数名消失判定能力消失。**

#### Scenario: 异步任务结果在当前轮执行中到达

- **WHEN** 当前 turn 执行期间到达一条 system-role 的异步任务结果
- **THEN** 原件留队至下一次拉取，消费时使用 user-role 副本，当前轮不认领它

#### Scenario: 原始消息不被改写

- **WHEN** 一条 system 消息经历准备、提交与重启
- **THEN** 原 source_event 的角色仍为 system，canonical 与模型侧为 user，内容与来源不被原地修改

#### Scenario: 普通用户输入与同步工具结果不受影响

- **WHEN** 处理 user 输入，或当前 ReAct 的同步工具结果
- **THEN** user 语义保持；同步工具结果照常参与当前轮，不受异步总线边界影响

### Requirement: Output stability does not prove process completion

For every command mode, the monitor MUST distinguish quiet output from native process death. A live pane reaching its stability threshold SHALL emit an observation and MUST NOT become completed solely because output stopped. Exit polarity SHALL derive from pane_dead and native exit status. Resident readiness and interactive input readiness SHALL require their explicit pattern or successful probe evidence.

#### Scenario: Quiet oneshot remains live
- **WHEN** a oneshot command is quiet beyond stable_duration while its native pane is alive
- **THEN** it remains active and its exit wait is not completed by stability

#### Scenario: Native exit determines completion
- **WHEN** the native pane exits with status zero or nonzero
- **THEN** the result reports completed or failed with the captured native exit evidence
