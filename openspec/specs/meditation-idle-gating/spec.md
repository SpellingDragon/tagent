# meditation-idle-gating Specification

## Purpose

定义冥想心跳的双闸门触发判据：以“真相便宜的位置”拆分空闲闸门（血统无关，任意 turn 结束算忙）与新颖性闸门（锚定输入侧 `source==user`），使冥想不依赖输出侧血统追踪即可免疫“冥想→派生任务→task_settled→再冥想”的自触发永动机。
## Requirements
### Requirement: 冥想触发采用双闸门判定

触发一次反思 SHALL 同时满足：① interval 自查节奏到期；② 自身空闲门：距本 agent 最近一次回合结束（**任何谱系**，含投递触发的回合与失败回合）≥ min_gap；③ novelty 门：观察面内存在水位之后的**非自管谱系**事件。观察面缺省为**[自身分区]**（不配置 `observed_namespaces` 时，任何配了 `meditation.enabled` 的 agent 反思自己的分区——与旧 in-loop 行为等效且零迁移）；显式声明可含自身与他人（他人须 ⊆ `read_namespaces` 授权）。任一门不过 SHALL 以 debug 级具名记录。反思动作恒为向**本 agent 循环的 session** 注入冥想输入事件——形态差异只是"哪个 agent 的 session、观察谁"。

#### Scenario: 三门齐备触发

- **WHEN** interval 到期、本 agent 空闲 ≥ min_gap、观察面水位后有非自管事件
- **THEN** 注入 `source="meditation"` 反思输入到本 agent 的 session 并推进水位

#### Scenario: 缺省观察面等效 in-loop

- **WHEN** 入口 agent 仅配 `meditation.enabled`（无 observed_namespaces），其业务 session 收到用户消息并入库
- **THEN** 下一个空闲窗口 novelty 门开、反思注入业务 session（共享会话上下文，自体维护）

#### Scenario: 投递风暴期只推迟

- **WHEN** 大量投递使本 agent 持续繁忙且观察面无新非自管事件
- **THEN** 空闲门与 novelty 门均不过，至多推迟反思

### Requirement: 空闲锚点血统无关

空闲锚点 `lastTurnEnd` SHALL 在**每个** turn 结束时无条件更新（含冥想触发的 turn、task_settled 回收 turn、失败/重试耗尽的 turn），不依据 trigger_source 过滤。冥想衍生活动对空闲锚点的影响 SHALL 仅表现为推迟下一次冥想，SHALL NOT 使其重新武装新颖性闸门。

#### Scenario: 冥想衍生任务 settle 不再武装冥想（永动机防护）

- **WHEN** 一次冥想 turn 派生的后台任务 settle 并完成其回收 turn，期间无任何新用户输入
- **THEN** `lastTurnEnd` 前移但 `lastUserInput` 不变
- **AND** 此后无论经过多少个 `MinGap`，冥想 SHALL NOT 再次触发，直至新用户输入到达

#### Scenario: 失败 turn 同样刷新空闲锚点

- **WHEN** 一个 turn 以 RunFlow 错误（含重试耗尽）结束
- **THEN** `lastTurnEnd` SHALL 更新为该 turn 结束时刻

### Requirement: 门控不依赖输出侧血统追踪

冥想门控 SHALL NOT 依赖输出事件、任务层 `Origin` 行李或 task_settled 的血统标记；事件回调（`makeOnEventCallback`）SHALL NOT 更新冥想锚点。novelty 判据读取的 `trigger_source` 是**提交时盖章在事实链上的持久归因**（入库路径的既有部分），属"输入侧事实"而非输出侧追踪；除此之外门控不引入任何输出侧读取。

#### Scenario: 事件回调与锚点解耦

- **WHEN** 任意 trigger_source 的 final response 经过事件回调
- **THEN** 冥想锚点不因该回调而变化

#### Scenario: 唯一 novelty 读径是事实链

- **WHEN** 审查 novelty 判定路径
- **THEN** 其数据来源仅 QueryEvents/GetEvent 的持久归因，无输出事件或回调读取

### Requirement: 混合批次中丢弃冥想事件

事件循环从总线批量拉取后，若批次中同时存在 `source="meditation"` 事件与任何非 meditation 的 external_input 事件，SHALL 在构建 invocation 前移除冥想事件并记录日志。被移除的冥想 SHALL NOT 补偿性重新注入。纯冥想批次 SHALL 正常处理。

#### Scenario: 冥想与任务结果同批时让位

- **WHEN** 同一批次包含 task_settled 事件与冥想事件（任意顺序）
- **THEN** 冥想事件 SHALL 被移除，该 turn 仅处理任务结果且 trigger_source 为 `task`
- **AND** 任务结果的输出 SHALL 携带其原有路由元数据正常投递

#### Scenario: 冥想与用户消息同批时让位

- **WHEN** 同一批次包含用户消息与冥想事件
- **THEN** 冥想事件 SHALL 被移除，该 turn 的 trigger_source 为 `user`

#### Scenario: 纯冥想批次正常执行

- **WHEN** 批次中仅有冥想事件
- **THEN** 该 turn SHALL 正常执行且 trigger_source 为 `meditation`

