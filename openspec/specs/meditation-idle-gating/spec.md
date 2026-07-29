# meditation-idle-gating Specification

## Purpose
TBD - created by archiving change meditation-gate-split. Update Purpose after archive.
## Requirements
### Requirement: 冥想触发采用双闸门判定

冥想触发 SHALL 同时满足两道独立闸门：(a) **空闲闸门**——距最近一次 turn 结束（`lastTurnEnd`）的间隔 ≥ `MinGap`；(b) **新颖性闸门**——最近一次用户输入时间（`lastUserInput`）晚于最近一次冥想时间（`lastMeditation`）。任一闸门不满足 SHALL 跳过本次检查。`lastUserInput` 为零值（从未有用户输入）时 SHALL 不触发冥想。

#### Scenario: 双闸门同时满足才触发

- **WHEN** 距最近 turn 结束已超过 `MinGap`，且上次冥想之后有过用户输入
- **THEN** 冥想 SHALL 触发，注入 `source="meditation"` 的 external_input 事件

#### Scenario: 空闲不足时跳过

- **WHEN** 距最近 turn 结束的间隔小于 `MinGap`
- **THEN** 本次检查 SHALL 跳过，不注入冥想事件

#### Scenario: 无新用户输入时跳过

- **WHEN** 上次冥想之后没有新的用户输入（`lastUserInput <= lastMeditation`）
- **THEN** 本次检查 SHALL 跳过，即使空闲时长已远超 `MinGap`

### Requirement: 空闲锚点血统无关

空闲锚点 `lastTurnEnd` SHALL 在**每个** turn 结束时无条件更新（含冥想触发的 turn、task_settled 回收 turn、失败/重试耗尽的 turn），不依据 trigger_source 过滤。冥想衍生活动对空闲锚点的影响 SHALL 仅表现为推迟下一次冥想，SHALL NOT 使其重新武装新颖性闸门。

#### Scenario: 冥想衍生任务 settle 不再武装冥想（永动机防护）

- **WHEN** 一次冥想 turn 派生的后台任务 settle 并完成其回收 turn，期间无任何新用户输入
- **THEN** `lastTurnEnd` 前移但 `lastUserInput` 不变
- **AND** 此后无论经过多少个 `MinGap`，冥想 SHALL NOT 再次触发，直至新用户输入到达

#### Scenario: 失败 turn 同样刷新空闲锚点

- **WHEN** 一个 turn 以 RunFlow 错误（含重试耗尽）结束
- **THEN** `lastTurnEnd` SHALL 更新为该 turn 结束时刻

### Requirement: 新颖性锚点锚定输入侧

`lastUserInput` SHALL 仅在消息注入点（`InjectMessageWithSource` / `InjectMessageWithMetadata`）且 `source == "user"` 时更新。其他 source（`meditation`、`task`、`tmux` 等）的注入 SHALL NOT 更新该锚点。SHALL NOT 依据任何输出侧事件（final response、trigger_source 标注）更新新颖性锚点。

#### Scenario: 用户注入更新新颖性锚点

- **WHEN** 以 `source="user"` 注入消息
- **THEN** `lastUserInput` SHALL 更新为注入时刻

#### Scenario: 非用户 source 注入不更新

- **WHEN** 以 `source="meditation"` 或 `source="task"` 注入消息
- **THEN** `lastUserInput` SHALL 保持不变

### Requirement: 门控不依赖输出侧血统追踪

冥想门控 SHALL NOT 要求输出事件、任务层 `Origin` 行李或 task_settled 事件携带冥想血统标记。事件回调（`makeOnEventCallback`）SHALL NOT 包含冥想锚点更新逻辑；`checkAndMeditate` SHALL NOT 在触发时重置空闲锚点（新颖性闸门在 `lastMeditation` 推进后自锁，无需额外重置）。

#### Scenario: 事件回调与冥想状态解耦

- **WHEN** 任意 trigger_source 的 final response 经过事件回调
- **THEN** 冥想管理器的任何锚点 SHALL NOT 因该回调而变化

#### Scenario: 触发后无需重置即自锁

- **WHEN** 冥想触发且随后无新用户输入
- **THEN** 后续每次检查 SHALL 被新颖性闸门拦截，不依赖任何触发时的锚点重置

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

