## MODIFIED Requirements

### Requirement: LLM 任务操作工具

系统 SHALL 向 agent 暴露任务操作工具:`list_tasks`(列出活跃/近期任务)、`cancel(id)`、`relaunch(id)`。这些工具 SHALL 是即时返回的同步工具(不进入 dense 窗口)。`get_task_result` SHALL NOT 作为框架注册工具提供——结算结果已随 task_settled 事件本体全量持久化，全量召回由 recall 协议凭票据（事件 key）承接，与任务层 TTL 解耦。框架注入的文案（截断提示、看板、去重提示）SHALL NOT 引用任何任务工具名，工具的装配与否属于 agent 配置层决策。

#### Scenario: LLM 列出与取消任务

- **WHEN** agent 装配了 `list_tasks`/`cancel_task` 并调用
- **THEN** 工具 SHALL 即时返回任务清单/取消结果（不进入 dense 窗口）

#### Scenario: 结果消费不走专用工具

- **WHEN** 一个 task_settled 事件携带结果（小结果内联全文 / 超大结果尾部+文件路径票据）被持久化，稍后模型需要内容
- **THEN** 小结果 SHALL 直接从通知/召回可见；超大结果 SHALL 经 `read_file` 分页读取转储文件
- **AND** SHALL NOT 依赖 `get_task_result` 工具或任务层 TTL 窗口
- **AND** 召回路径 SHALL NOT 返回超大全文（事件本体有界，复发不可能）
