## ADDED Requirements

### Requirement: 退化空 final 响应不产出输出

一个 turn 以 final assistant 响应（`Role=assistant` 且无 `tool_calls`）结束时，若其 `content` 为空，该响应 SHALL 被视为**退化响应**：SHALL NOT 被 echo 成 `agent_output` 事件发回 EventBus，消费端 SHALL NOT 因此向用户投递任何内容（包括占位串如 `"(empty response)"`）。持久循环 SHALL 在此 turn 结束后照常回到 `Pull` 阻塞，依赖下一个外部/monitor 事件（如 `task_settled`）恢复。

框架 SHALL NOT 为"退化空响应且无进行中后台任务"的情形注入任何自动 nudge、重试或占位输出——该情形视为模型/prompt 质量问题，不由循环掩盖。

#### Scenario: 空 final 响应不 echo 回 bus

- **WHEN** RunFlow 收到一个 final assistant 响应且其 `content` 为空
- **THEN** SHALL NOT 向 EventBus 发布 `agent_output` echo
- **AND** 循环回到 `Pull` 阻塞等待下一个事件

#### Scenario: 空+挂起靠 monitor 事件恢复

- **WHEN** 一个 turn 以空 final 响应结束，且此时存在进行中的后台任务
- **THEN** 循环 SHALL 静默等待，直到该任务的 `task_settled` 事件到达并触发回收 turn

#### Scenario: 非空 final 响应正常产出

- **WHEN** RunFlow 收到一个 final assistant 响应且 `content` 非空
- **THEN** SHALL 照常 echo 成 `agent_output` 并经 `outputCh` 交给消费端投递

#### Scenario: 空+空闲不注入兜底

- **WHEN** 一个 turn 以空 final 响应结束，且无任何进行中的后台任务
- **THEN** 框架 SHALL NOT 注入自动 nudge、重试或占位输出
