## MODIFIED Requirements

### Requirement: 同名计划任务单飞

`AgentToolWrapper` 为 plan 类子 agent spawn 任务时，若调用携带非空 `name`，幂等 Key SHALL 为 `agentName + ":" + name`（relaunch 轮次 SHALL 透传同一 Key）；同名 change 的并发 spawn SHALL 被任务层去重短路（返回既有任务与 Deduped 标记），后到调用方 SHALL 收到含既有 task id 的**事实性提示**（票据化：同名任务已在运行、等待其 task_settled 结果、勿重复发起同名调用）。提示 SHALL NOT 教学具体操作工具（如 `get_task_result` 查询、`resume_task` 续行的调用示例）——生命周期操作指引属于 plan 工具描述，与实际装配一致。未携带 `name` 的调用维持按 `request` 去重。

#### Scenario: 同名并发 spawn 去重

- **WHEN** 两次携带相同 `name` 的 plan 调用并发发生
- **THEN** 恰有一个任务被跟踪
- **AND** 后到者 SHALL 收到含既有 task id 与"等待 task_settled、勿重复发起"的事实性指引，SHALL NOT 产生第二个并发 Run
- **AND** 提示文本 SHALL NOT 引用 `get_task_result` 或 `resume_task` 等工具名

#### Scenario: relaunch 保持 name 键

- **WHEN** 同名任务的 relaunch 轮次再次 spawn
- **THEN** 幂等 Key SHALL 透传同一 `agentName + ":" + name`，不产生重复任务
