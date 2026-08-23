# task-registry-and-board Delta

## MODIFIED Requirements

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

### Requirement: live 任务看板投影

系统 SHALL 在每个 turn 的 `BeforeModel` 阶段从 TaskManager registry **重新渲染**一个紧凑的任务看板注入上下文。看板 SHALL 放置在**最后一个 agent_output 之后、当前输入之前**(recency 锚点)。

任务看板 SHALL NOT 参与上下文压缩——它是 live 状态快照而非历史事件。已被 LLM 处理(acknowledged)的 settled 任务 SHALL 在短 TTL 后从看板 age-out,以保持看板有界。

**等待指引行**：看板存在活跃任务时,尾部 SHALL 追加一行固定文本的等待指引（语义：等待后台任务时直接给出简短回复并结束回合,结算会自动唤醒;不要用 sleep 等命令等待）。指引行 SHALL 为固定文本（不随任务数/年龄变化）,SHALL NOT 引入随 turn 变化的内容。

#### Scenario: 看板每 turn 重新渲染且不被压缩

- **WHEN** 上下文历史被压缩
- **THEN** 任务看板 SHALL 仍由 registry 重新渲染出当前活跃任务
- **AND** 看板内容 SHALL NOT 被压缩器改写或丢弃

#### Scenario: 看板位于 recency 锚点

- **WHEN** 构建发给 LLM 的消息序列
- **THEN** 任务看板 SHALL 位于最后一个 agent_output 之后、当前输入(或 settle 事件)之前

#### Scenario: 已处理任务 age-out

- **WHEN** 一个 settled 任务已被 LLM 在回收 turn 中处理并超过 age-out TTL
- **THEN** 该任务 SHALL 从活跃看板移除

#### Scenario: 活跃任务存在时尾部出现等待指引

- **GIVEN** 看板渲染时存在 1 个活跃后台任务
- **WHEN** `RenderBoard` 输出
- **THEN** 末尾 SHALL 有一行含"结束回合/自动唤醒/不要 sleep 等待"语义的固定指引
- **AND** 无活跃任务时看板为空,SHALL NOT 出现指引行
