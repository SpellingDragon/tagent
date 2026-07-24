## ADDED Requirements

### Requirement: task_settled 作为一等事件触发回收 turn

`task_settled` SHALL 作为一等事件进入持久事件循环。当一个后台 Task settle 时,其 `task_settled` 事件 SHALL 经 EventBus 进入循环,并像外部输入一样触发一个新 turn(回收 turn);当循环空闲(阻塞在 Pull)时,`task_settled` SHALL 能唤醒它。

若一个 turn 正在进行,`task_settled` SHALL 排队至下一轮被消费,SHALL NOT 打断进行中的 turn。

#### Scenario: 空闲时任务完成唤醒循环

- **WHEN** 事件循环空闲(阻塞在 Pull)且一个后台任务 settle
- **THEN** `task_settled` 事件 SHALL 唤醒循环并触发一个回收 turn

#### Scenario: turn 进行中任务完成则排队

- **WHEN** 一个 turn 正在执行时某后台任务 settle
- **THEN** `task_settled` SHALL 排队,SHALL NOT 打断当前 turn,SHALL 在下一轮被消费
