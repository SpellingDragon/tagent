## ADDED Requirements

### Requirement: spawn 时捕获发起轮来源上下文为不透明 baggage

当一个 tool/子 agent 在某个 turn 内 spawn 一个后台 Task 时，框架 SHALL 在 spawn 时刻捕获发起轮 invocation metadata 的快照（副本）并作为**不透明 baggage** 绑定到该 Task。任务执行层 SHALL 忠实透传该 baggage（spawn 收下、settle 交还），SHALL NOT 读取或解释其内容，SHALL NOT 依据其内容改变执行或结算行为。捕获 SHALL 发生在 RunFlow 注入 task spawner 的边界；工具 SHALL NOT 需要感知或传递它。快照 SHALL 使用副本，SHALL NOT 与后续 turn 的 invocation metadata 相互覆盖。

#### Scenario: 用户轮内 spawn 携带 chat_id

- **WHEN** 一个由用户消息（携带 `chat_id`）触发的 turn 内 spawn 了一个后台 Task
- **THEN** 该 Task SHALL 绑定该 turn 来源上下文的不透明快照（含 `chat_id`）

#### Scenario: 工具无需感知 baggage

- **WHEN** 一个工具调用 `Spawn` 且未显式提供任何来源上下文
- **THEN** 框架 SHALL 自动填入发起轮的 baggage 快照
- **AND** 工具代码 SHALL NOT 因此改变

#### Scenario: 任务层不解释 baggage

- **WHEN** 任务执行层处理一个携带 baggage 的 Task（执行、探测 settle、relaunch）
- **THEN** SHALL NOT 读取 baggage 内容
- **AND** 其执行/结算行为 SHALL 与 baggage 内容无关

### Requirement: 结算事件携带路由 metadata 至回收 turn

当一个绑定了路由 metadata 的 Task settle 时，其 `task_settled` 事件 SHALL 携带该路由 metadata；由该事件驱动的回收 turn 产生的所有输出事件 SHALL 携带同一路由 metadata（`meta_chat_id` 等），使发起会话身份从 spawn 一路存活到输出事件。该传播 SHALL 复用既有 metadata 管线，SHALL NOT 引入并行的独立管线。

#### Scenario: settle 事件带出 chat_id

- **WHEN** 一个绑定了 `chat_id` 的后台 Task settle
- **THEN** 其 `task_settled` 事件 SHALL 携带该 `chat_id`
- **AND** 回收 turn 的输出事件 SHALL 带有 `meta_chat_id`

#### Scenario: 无 metadata 的任务不受影响

- **WHEN** 一个未绑定路由 metadata 的 Task settle（如无发起会话的内部任务）
- **THEN** `task_settled` 事件 SHALL 正常产生，仅不含路由 metadata

### Requirement: task 作为一等可路由触发源

消费端 SHALL 将 `trigger_source=task` 的非空 final 响应作为一等可投递输出，路由回其携带的发起会话（`meta_chat_id`），投递语义与 `trigger_source=user` 一致。`task` 触发源 SHALL NOT 落入"未知触发源"分支而被丢弃。

当 `trigger_source=task` 的非空 final 响应缺少 `meta_chat_id` 时，消费端 SHALL 记录告警并跳过投递（无法路由），SHALL NOT 静默假装成功。

#### Scenario: 后台任务结果投递回原会话

- **WHEN** 消费端收到 `trigger_source=task` 且带 `meta_chat_id` 的非空 final 响应
- **THEN** SHALL 投递到该 `chat_id`（与 `user` 路径一致）

#### Scenario: task 结果缺 chat_id 时告警跳过

- **WHEN** `trigger_source=task` 的非空 final 响应缺少 `meta_chat_id`
- **THEN** SHALL 记录告警并跳过投递
- **AND** SHALL NOT 落入"未知触发源"分支
