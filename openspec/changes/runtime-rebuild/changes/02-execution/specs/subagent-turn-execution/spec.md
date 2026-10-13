# subagent-turn-execution delta（02-execution）

> 复用登记（非规范，不改本条语义）：子 agent 调用沿用框架 `AgentToolWrapper.Call` → 直接调用一次 `RunFlow`（非运行持久事件循环）；一次 `RunFlow` = 一个 turn（单一输入 → 完整 ReAct → 最终 assistant 响应）。**模型最终 response 结束 ≠ invocation 完成**：子 agent 若自身派生后台义务，其 Task 只在**本调用义务明确结清**后 settle。不引入 LLMAgent 第二工具循环。
> 本 delta 修改「子 agent 调用可作为异步任务执行」，并**新 MODIFIED** 与主 spec 语义相反的三条：`subagent-turn-execution`「Sub-agent 调用执行恰好一个 turn」（turn 边界不等于 invocation 完成）、「Sub-agent 保持并发隔离与调用契约不变」（不每次创建私有可写真源）与「投递对账屏障的 booking 与投递期望一一配对」（撤销表单以「受理回执」而非「同步等待窗口内 settle」为准）；本能力其余 Requirement（最终响应识别区分 assistant 响应与工具结果、Sub-agent 调用超时对多轮工作宽容、子 agent 委派使用同代目标绑定、真实委派上下文穿透工具装饰层）由主 spec `openspec/specs/subagent-turn-execution/spec.md` 继承（**证据映射，不改**）。

## MODIFIED Requirements

### Requirement: 子 agent 调用可作为异步任务执行

`AgentToolWrapper` 对子 agent 的调用 SHALL 纳入任务层作为异步 Task 执行。**受理一次**：wrapper 收到调用即 SHALL 立即返回一次**受理回执**（含 task id 与 running 状态），受理**不等结果**——快任务也 MUST NOT 另走「同步等待窗口内内联最终结果」分支（原「`RunFlow` 返回即 settle / dense 阶段内内联返回 / `asyncDisabled` 回退强制同步」三类旧表述 SHALL 被本条取代）。

**模型 response 结束不等 invocation 完成**：子 agent 的最终 assistant 响应（`RunFlow` 返回）只标志该 turn 的模型决策结束，MUST NOT 被直接判为该调用 `completed`；该调用的后台结果作为**独立 observation** 回流，驱动**下一步 decision**（仍属**原 invocation**，CallID 按 decision 作用域保真，见 `event-metadata-contract`），MUST NOT 冒充该调用的第二次同步应答。该 Task SHALL 只在**本调用义务明确结清**后 settle。

**子 agent 使用应用选择的 session**：子 agent 调用 SHALL 在**应用显式选择的 session**（见 01 `config-state-twinship`「session 身份与授权」）内执行，并借用 state 同代绑定；MUST NOT 为每次调用创建**私有可写事件真源**（第二份私有可写会话历史/投影真源）——并发隔离经 invocation 作用域借用实现（各自决策、各自解析与去重身份），而非另建可写历史。

`AgentToolWrapper.Call` 的输入参数与返回值契约 SHALL 保持不变（返回值为一次受理回执，见上）。不引入 LLMAgent 第二工具循环。

#### Scenario: 快子 agent 亦受理一次

- **WHEN** 一个纳入任务层的子 agent 的 `RunFlow` 在短时间内返回
- **THEN** 父 agent 的 `Call()` SHALL 已返回一次受理回执（task id + running），MUST NOT 因任务快而改走同步内联最终结果分支
- **AND** 其最终输出 SHALL 作为独立 observation 回流，驱动原 invocation 的下一步 decision

#### Scenario: 模型 response 结束不判 completed

- **WHEN** 子 agent 模型已给出最终 response（`RunFlow` 返回），但其派生的后台义务尚未结清
- **THEN** 该 Task MUST NOT 被判 `completed`；SHALL 待义务明确结清后 settle
- **AND** 结清结果 SHALL 以独立 observation 回流，MUST NOT 冒充第二次同步应答

#### Scenario: 不创建私有可写真源

- **WHEN** 一次子 agent 调用执行
- **THEN** 其 SHALL 使用应用选择的 session 与 state 借出的同代绑定，MUST NOT 另建私有可写事件历史/投影真源
- **AND** 与同 session 其它 invocation 的并发隔离经 invocation 作用域借用实现，互不串写

#### Scenario: 委派终局可关联到原调用

- **WHEN** 一次纳入任务层的子 agent 调用在后台结算
- **THEN** 其结算通知 SHALL 携带原 task id 以关联原调用，MUST NOT 冒充该调用的第二次同步应答

#### Scenario: 委派绑定发起代不读最新表

- **WHEN** 委派调用在 G1 期间发出，期间发布 G2 改变目标
- **THEN** 该委派 SHALL 继续使用 G1 的目标与声明，MUST NOT 中途改读全局最新表

### Requirement: Sub-agent 调用执行恰好一个 turn

Sub-agent 工具调用（经 `AgentToolWrapper.Call` → `agent.Run()`）SHALL 通过**直接调用一次 `RunFlow`** 执行（单一输入 → 完整 ReAct 工具循环 → 最终 assistant 响应），而非运行持久事件循环 `runEventLoop` 后再探测事件流；MUST NOT 引入 LLMAgent 或任何第二工具迭代循环。`Run()` SHALL NOT 使用 500ms drain 定时器、事件探测停止条件（`len(ToolCalls) == 0` 判断）或 `runCancel` 强制取消来确定 turn 结束。

**turn 边界 ≠ invocation 完成**：一次 `RunFlow` 的返回定义该 turn 的边界，但 MUST NOT 被直接当作该调用义务已结清——若子 agent 自身派生后台任务/通知义务，其 Task SHALL 待**本调用义务明确结清**后方 settle（见「子 agent 调用可作为异步任务执行」）。无工具调用的模型回复不能证明后台义务已完成。

#### Scenario: 多轮工具调用完整执行

- **WHEN** sub-agent 收到需要多次工具调用的请求（如 plan create 需依次执行 `openspec init`、`openspec new change`、写 tasks.md）
- **THEN** `RunFlow` 在单次调用内跑完所有工具轮次直到最终 assistant 响应
- **AND** sub-agent 不在任一中间工具结果处提前返回
- **AND** 输出 channel 在 `RunFlow` 返回后关闭

#### Scenario: 慢 LLM 不触发提前返回

- **WHEN** sub-agent 的 LLM 每轮响应耗时远超 500ms（如 glm-5.2 每轮约 16s）
- **THEN** sub-agent 仍执行到最终响应，不被任何定时器截断
- **AND** 首个工具调用后的后续轮次正常执行

#### Scenario: turn 边界由 RunFlow 返回定义但不作义务完成判据

- **WHEN** `RunFlow` 内部的 event channel 关闭（Flow 在最终响应处结束）
- **THEN** 承载 `RunFlow` 的 goroutine 关闭 `invOutputCh`
- **AND** 调用方对输出 channel 的 `range` 循环自然结束，无需探测事件内容判断结束
- **AND** 该返回 MUST NOT 单独作为「调用义务已结清」的判据

### Requirement: Sub-agent 保持并发隔离与调用契约不变

每次 sub-agent 调用 SHALL 保持并发隔离与调用契约：`AgentToolWrapper.Call` 的输入参数与返回值契约 SHALL 保持不变。

隔离 SHALL 经 **invocation 作用域借用**实现——同一 sub-agent 实例被并发或连续多次调用时，各自决策、各自持有解析与去重身份，MUST NOT 共享可变游标/压缩器状态而互相污染；session SHALL 由应用显式选择并在本次调用作用域内使用。MUST NOT 每次调用创建**私有可写事件历史/投影真源**（第二份可写会话历史）；调用结束 MUST NOT 关闭或并入父的 state 会话真源。

#### Scenario: 每次调用按 invocation 作用域隔离

- **WHEN** 同一 sub-agent 实例被并发或连续多次调用
- **THEN** 各调用 SHALL 各自决策、互不串写；调用之间不共享可变的压缩器状态
- **AND** MUST NOT 通过为每次调用另建私有可写事件历史/投影真源来实现隔离

#### Scenario: 复用应用选择的 session 不新建真源

- **WHEN** 一次 sub-agent 调用执行
- **THEN** 其 SHALL 在应用显式选择的 session 内借用 state 同代绑定执行，session 历史归 state 持有
- **AND** MUST NOT 另建第二份可写会话历史或在调用结束时关闭/并入父的 state 会话真源

### Requirement: 投递对账屏障的 booking 与投递期望一一配对

子调用环的投递对账屏障（delivery-accounting barrier）SHALL 保证每个 invocation 的 pending 计数与真实投递期望一一配对：包装 spawner 在内层 spawn 返回**受理回执**、dedup 命中或被 gate 拒绝（Blocked）三种「本次调用不再拥有 settle 期望」的形态时，MUST 撤销（void）本次预登记的 booking；booking 的登记 MUST 保持先于内层 spawn（期望先于任务可能极快的结算而存在）。

#### Scenario: 同 key 任务在飞时重复发起（dedup single-flight）

- **WHEN** 子调用的某 turn 内对同 key 任务第二次发起调用，任务层 dedup 命中返回既有 active 任务
- **THEN** 本次调用的 booking 被撤销，屏障 pending 不因 dedup 而净增；既有任务结算时按其原发起调用的 booking 配对递减
- **AND** 子调用环在该调用最后一个真实结算投递并排空总线后静默退出，不被无人消耗的 booking 钉住

#### Scenario: spawn gate 拒绝纳管（disk 退化）

- **WHEN** 子调用的某 turn 内 spawn 被 disk 退化闸拒绝（Blocked，工作已被取消跟踪）
- **THEN** 本次调用的 booking 被撤销，屏障不泄漏；调用环按正常静默条件退出

#### Scenario: 受理即撤销 booking（取代旧「同步窗口内 settle」）

- **WHEN** 内层 spawn 返回**受理回执**（本次调用已受理、结果将随后台 observation 回流，不再拥有同步 settle 期望）
- **THEN** booking SHALL 被撤销，屏障不泄漏
- **AND** MUST NOT 依赖「同步等待窗口内 settle」这一旧形态作为配对依据
