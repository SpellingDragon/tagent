## ADDED Requirements

### Requirement: 循环不依赖 bus echo 自触发

持久事件循环 SHALL 仅通过 `bus.Pull` 等待外部/任务事件驱动下一轮；框架 SHALL NOT 将 agent_output(final 响应）回灌到 EventBus 作为自触发脉冲。final 响应的投递 SHALL 仅经 outputCh。

#### Scenario: turn 结束后循环静默等待

- **WHEN** 一个 turn 完成（final 响应已投递）且 bus 上无其他事件
- **THEN** 循环 SHALL 阻塞于 `Pull`，直到新的外部输入或任务事件到达
- **AND** SHALL NOT 出现由 agent_output echo 引发的空转唤醒

#### Scenario: 后台任务结算唤醒循环

- **WHEN** 循环阻塞于 `Pull` 时一个后台任务 settle 产生 task_settled 事件
- **THEN** 循环 SHALL 被该事件唤醒并开启回收 turn
