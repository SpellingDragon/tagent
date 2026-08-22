## MODIFIED Requirements

### Requirement: LLM 任务操作工具

系统 SHALL 向 agent 暴露任务操作工具:`list_tasks`(列出活跃/近期任务)、`cancel(id)`、`relaunch(id)`。这些工具 SHALL 是即时返回的同步工具(不进入 dense 窗口)。`get_task_result` SHALL NOT 作为框架注册工具提供——结算结果已随 task_settled 事件本体全量持久化，全量召回由 recall 协议凭票据（事件 key）承接，与任务层 TTL 解耦。框架注入的文案（截断提示、看板、去重提示）SHALL NOT 引用任何任务工具名，工具的装配与否属于 agent 配置层决策。

#### Scenario: LLM 列出与取消任务

- **WHEN** agent 装配了 `list_tasks`/`cancel_task` 并调用
- **THEN** 工具 SHALL 即时返回任务清单/取消结果（不进入 dense 窗口）

#### Scenario: 大结果全量经事件召回

- **WHEN** 一个 task_settled 事件携带全量结果被持久化，稍后模型需要完整内容
- **THEN** 模型 SHALL 经 recall 协议（memory_recall，票据=该事件的 evt key）获取全文
- **AND** SHALL NOT 依赖 `get_task_result` 工具或任务层 TTL 窗口
