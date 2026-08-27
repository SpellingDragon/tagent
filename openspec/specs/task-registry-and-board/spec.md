# task-registry-and-board Specification

## Purpose

Provide a deterministic (non-LLM) TaskManager registry that tracks every Task,
projects a live task board into context each turn (decoupled from compaction),
emits self-contained `task_settled` events, and exposes LLM task-operation tools
(list/get/cancel/relaunch) so the agent can observe and act on background work.
## Requirements
### Requirement: TaskManager 内存 registry 与幂等 spawn

系统 SHALL 提供一个确定性的 TaskManager 组件(非 LLM agent),维护一个**内存 registry**,记录每个 Task 的 `{id, desc, status, spawn-spec, resultRef, startedAt}`。status SHALL 覆盖 `running` / `stable` / `alive-detached` / `completed` / `failed` / `suspect` / `dead` / `cancelled`。

spawn SHALL 幂等:当一个语义等价的任务已在运行时,再次 spawn SHALL 返回既有 Task 句柄而非新建,以防止 LLM 重复发起。

registry SHALL 为纯内存(ephemeral),SHALL NOT 跨进程重启持久化。

#### Scenario: 幂等 spawn 去重

- **WHEN** 一个语义等价的任务已处于 running,LLM 再次发起同一任务
- **THEN** TaskManager SHALL 返回既有 Task,SHALL NOT 新建重复任务

#### Scenario: 死任务保留 spec 以便进程内 relaunch

- **WHEN** 一个任务转为 `dead` / `suspect`
- **THEN** 其记录 SHALL 保留 spawn-spec
- **AND** LLM SHALL 可通过 `relaunch(id)` 用原 spec 在**同一进程内**重新发起

### Requirement: live 任务看板投影

系统 SHALL 在每次 LLM 调用的 `BeforeModel` 阶段从 TaskManager registry **重新渲染**一个紧凑的任务看板注入上下文。看板 SHALL **追加在消息列表末尾**（当前输入与任何待答工具结果之后）——看板字节随任务年龄/状态逐次变化，置于末尾使前缀缓存的损失严格限于看板自身；插入在输入之前会在看板处截断缓存前缀，导致回合内每次 LLM 调用重付整个活跃回合（2026-08-27 修复）。

任务看板 SHALL NOT 参与上下文压缩——它是 live 状态快照而非历史事件。已被 LLM 处理(acknowledged)的 settled 任务 SHALL 在短 TTL 后从看板 age-out,以保持看板有界。

**等待指引行**：看板存在活跃任务时,尾部 SHALL 追加一行固定文本的等待指引（语义：等待后台任务时直接给出简短回复并结束回合,结算会自动唤醒;不要用 sleep 等命令等待）。指引行 SHALL 为固定文本（不随任务数/年龄变化）,SHALL NOT 引入随 turn 变化的内容。看板作为模型读到的最后一条消息，同时强化反自旋教学（结束回合即合法等待）。

#### Scenario: 看板每 turn 重新渲染且不被压缩

- **WHEN** 上下文历史被压缩
- **THEN** 任务看板 SHALL 仍由 registry 重新渲染出当前活跃任务
- **AND** 看板内容 SHALL NOT 被压缩器改写或丢弃

#### Scenario: 看板位于消息列表末尾（缓存稳定位）

- **WHEN** 构建发给 LLM 的消息序列（存在新输入或待答工具结果）
- **THEN** 任务看板 SHALL 是最后一条消息
- **AND** 其前全部消息 SHALL 原样保留（含工具调用配对），使前缀缓存仅损失看板自身

#### Scenario: 已处理任务 age-out

- **WHEN** 一个 settled 任务已被 LLM 在回收 turn 中处理并超过 age-out TTL
- **THEN** 该任务 SHALL 从活跃看板移除

#### Scenario: 活跃任务存在时尾部出现等待指引

- **GIVEN** 看板渲染时存在 1 个活跃后台任务
- **WHEN** `RenderBoard` 输出
- **THEN** 末尾 SHALL 有一行含"结束回合/自动唤醒/不要 sleep 等待"语义的固定指引
- **AND** 无活跃任务时看板为空,SHALL NOT 出现指引行

### Requirement: 自包含的 task_settled 事件

当一个后台 Task settle 时,TaskManager SHALL 发出一个**自包含**的 `task_settled` 事件,携带 `task_id`、任务 `desc`、settle `kind` 与结果摘要或 `event_key` ref。该事件 SHALL 不依赖原始 spawn 的 tool_call 仍存在于历史中即可被 LLM 正确关联(与压缩解耦)。

**紧凑单行轨迹形态**：事件正文 SHALL 组装为单行轨迹骨架（内部换行转义），形如：

```
[task settled] <✓|✗|∞|⚠> <desc 截断> (id=<短id>) → 结果: <内联截断 | 转储路径+尾部预览>
```

- 状态标记 SHALL 区分 completed / failed（附错误截断）/ alive-detached / suspect
- 轨迹骨架 SHALL 保留"做了什么（desc）→ 结果如何（状态+结果摘要）"与短 id,使 LLM 无需回取原文即可决策（继续等待 / read_file 全文 / recall 票据回补）
- **内联上限 SHALL 为编译期命名常量（不设配置旋钮、不设回退开关）**：结果 ≤ 常量（初始 600 字符）SHALL 单行内联；> 常量 SHALL 转储文件（复用 tool-output 落盘管线）并在通知中携带路径与尾部预览
- 组装点瘦身 SHALL 信息无损：task_id / desc / settle kind / 错误 / 结果或转储指引缺一不可；丢弃的仅是排版冗余

大结果 SHALL 按转储文件路径回写,事件正文仅放摘要/预览,LLM SHALL 可按需经 `read_file` 拉取全量（本要求使实现与该既有约束收敛——旧实现内联上限派生自 token 预算公式,在 128K 配置下达 ~256K 字符,与"事件仅放摘要"相悖）。

#### Scenario: 压缩后仍可关联结果

- **WHEN** 原始 spawn 的 tool_call/thinking 已被压缩丢弃,而任务随后 settle
- **THEN** `task_settled` 事件 SHALL 凭自身携带的 desc + 结果使 LLM 正确关联,无需原始 tool_call 存在

#### Scenario: trivial 结果单行内联

- **GIVEN** 一个完成任务,结果为 `"wait longer\n"`（12 字符）
- **WHEN** 组装 task_settled 事件
- **THEN** 事件正文 SHALL 为单行,含状态标记、desc 截断、短 id 与内联结果
- **AND** 正文中 SHALL NOT 出现连续空行或独立成行的 UUID/状态标签

#### Scenario: 超常量结果转储+预览

- **GIVEN** 一个完成任务,结果 5000 字符,内联常量 600
- **WHEN** 组装 task_settled 事件
- **THEN** 结果 SHALL 写入 tool-output 转储文件
- **AND** 事件正文 SHALL 携带文件路径与尾部预览,并提示可经 read_file 分页读取

#### Scenario: 失败任务轨迹完整

- **GIVEN** 一个失败任务,错误信息 200 字符
- **WHEN** 组装 task_settled 事件
- **THEN** 单行形态 SHALL 含失败标记与错误摘要截断,LLM 可据此决定 relaunch 或放弃

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

