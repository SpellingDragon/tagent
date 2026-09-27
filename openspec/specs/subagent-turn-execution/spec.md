# subagent-turn-execution Specification

## Purpose

定义 sub-agent 工具调用的执行语义:一次调用 = 一次 `RunFlow` = 一个完整 turn。turn 边界由 `RunFlow` 的自然返回定义,而非对输出事件流的内容探测。sub-agent 与顶层持久循环共享同一个 turn 原语 `RunFlow`,仅循环包裹方式不同(顶层 `for { Pull; RunFlow }` 反应式守护;sub-agent 直调一次)。
## Requirements
### Requirement: Sub-agent 调用执行恰好一个 turn

Sub-agent 工具调用(经 `AgentToolWrapper.Call` → `agent.Run()`)SHALL 通过**直接调用一次 `RunFlow`** 执行,而非运行持久事件循环 `runEventLoop` 后再探测事件流。一次 `RunFlow` = 一个完整 turn(单一输入 → 完整 ReAct 工具循环 → 最终响应)。turn 边界 SHALL 由 `RunFlow` 的返回定义,而非对输出事件的内容探测。

`Run()` SHALL NOT 使用 500ms drain 定时器、事件探测停止条件(`len(ToolCalls) == 0` 判断)或 `runCancel` 强制取消来确定 turn 结束。

#### Scenario: 多轮工具调用完整执行

- **WHEN** sub-agent 收到需要多次工具调用的请求(如 plan create 需依次执行 `openspec init`、`openspec new change`、写 tasks.md)
- **THEN** `RunFlow` 在单次调用内跑完所有工具轮次直到最终 assistant 响应
- **AND** sub-agent 不在任一中间工具结果处提前返回
- **AND** 输出 channel 在 `RunFlow` 返回后关闭

#### Scenario: 慢 LLM 不触发提前返回

- **WHEN** sub-agent 的 LLM 每轮响应耗时远超 500ms(如 glm-5.2 每轮约 16s)
- **THEN** sub-agent 仍执行到最终响应,不被任何定时器截断
- **AND** 首个工具调用后的后续轮次正常执行

#### Scenario: turn 边界由 RunFlow 返回定义

- **WHEN** `RunFlow` 内部的 event channel 关闭(Flow 在最终响应处结束)
- **THEN** 承载 `RunFlow` 的 goroutine 关闭 `invOutputCh`
- **AND** 调用方对输出 channel 的 `range` 循环自然结束
- **AND** 无需探测事件内容判断结束

### Requirement: 最终响应识别区分 assistant 响应与工具结果

判断一个事件是否为最终响应(`isFinalResponse`)SHALL 同时要求消息角色为 `assistant` **且** 无 tool_calls。工具结果事件(`Role=tool`)即使无 tool_calls,也 SHALL NOT 被识别为最终响应。

#### Scenario: 工具结果不被误判为最终响应

- **WHEN** 一个事件的消息 `Role=tool`(工具执行结果),无 tool_calls
- **THEN** `isFinalResponse` 返回 false
- **AND** `RunFlow` 不因此向 EventBus 回发 `agent_output` 事件

#### Scenario: assistant 最终消息被正确识别

- **WHEN** 一个事件的消息 `Role=assistant` 且无 tool_calls
- **THEN** `isFinalResponse` 返回 true
- **AND** `RunFlow` 向 EventBus 回发一个 `agent_output` 事件

#### Scenario: 带 tool_calls 的 assistant 响应不是最终响应

- **WHEN** 一个事件的消息 `Role=assistant` 且包含至少一个 tool_call
- **THEN** `isFinalResponse` 返回 false

### Requirement: Sub-agent 保持并发隔离与调用契约不变

每次 sub-agent 调用 SHALL 创建独立的 EventBus、SessionProjection 与 ContextManager(SmartCompressor 具有可变状态,不可跨并发调用共享)。`AgentToolWrapper.Call` 的输入参数与返回值契约 SHALL 保持不变。

#### Scenario: 每次调用组件隔离

- **WHEN** 同一 sub-agent 实例被并发或连续多次调用
- **THEN** 每次调用使用各自独立的 invBus / invProjection / invCM
- **AND** 调用之间不共享可变的压缩器状态

#### Scenario: 调用完成后恢复持久 bus

- **WHEN** 一次 sub-agent 调用的 `RunFlow` 返回
- **THEN** agent 的 activeBus 恢复为 persistentBus
- **AND** 后续 `InjectMessage` 路由回持久事件循环(若其处于活动状态)

### Requirement: Sub-agent 调用超时对多轮工作宽容

`AgentToolWrapper` 对 sub-agent 调用施加的超时（`defaultSubAgentTimeout`）SHALL 足够宽容，以容纳 sub-agent 正常的多轮 ReAct 工作（如 plan create 依次执行自检、init、new change、写多个 artifact、validate）。超时 SHALL NOT 在 sub-agent 正常推进多轮工具调用时将其截断；其真实工作上界由各 agent 的 `max_tool_iterations` 决定，超时仅作为对真正失控（runaway/挂死）调用的兜底。

`defaultSubAgentTimeout` SHALL 不小于 600 秒。

#### Scenario: 多轮创建流程不被超时截断

- **WHEN** plan agent 执行需要多轮工具调用的 create 流程（自检 → init → new change → 写 proposal.md → 写 tasks.md → validate）
- **AND** 使用较慢的 LLM（如 glm-5.2 每轮约 15–25s）
- **THEN** sub-agent 调用 SHALL 在超时内完成，不被 `defaultSubAgentTimeout` 中途取消

#### Scenario: 超时仅作 runaway 兜底

- **WHEN** sub-agent 达到其 `max_tool_iterations` 上界
- **THEN** sub-agent SHALL 因迭代上界正常结束，而非依赖超时
- **AND** `defaultSubAgentTimeout` 仅在调用真正挂死时兜底触发

### Requirement: 子 agent 调用可作为异步任务执行

`AgentToolWrapper` 对子 agent 的调用 SHALL 可纳入任务层作为异步 Task 执行:其 settle 信号为 `RunFlow` 返回,结果为子 agent 的最终输出。dense 阶段内完成则内联返回(等价于既有同步行为);越过 dense 阶段(detach)则返回 ack 并在 `RunFlow` 返回时发出 `task_settled`。

子 agent SHALL **默认异步**,并保留回退开关(可强制同步)。每次子 agent Task 的并发隔离契约(独立 EventBus / SessionProjection / ContextManager)SHALL 保持不变。

#### Scenario: 快子 agent 窗口内内联返回

- **WHEN** 一个纳入任务层的子 agent 在 dense 阶段内 `RunFlow` 返回
- **THEN** 父 agent 的 `Call()` SHALL 内联返回子 agent 最终输出(与既有同步行为等价)

#### Scenario: 慢子 agent 越窗异步回收

- **WHEN** 一个子 agent 的 `RunFlow` 越过 dense 阶段仍未返回
- **THEN** `Call()` SHALL 返回 ack,并在 `RunFlow` 最终返回时发出自包含的 `task_settled` 事件

#### Scenario: 回退开关强制同步

- **WHEN** 子 agent 异步回退开关被启用(asyncDisabled)
- **THEN** 子 agent 调用 SHALL 保持既有同步执行行为

### Requirement: 子 agent 委派使用同代目标绑定

`AgentToolWrapper` 的同步调用、传输重试、受管异步与本地/A2A 适配 SHALL 使用发起请求所持有版本中的子调用目标与声明，MUST NOT 在调用中途读取全局最新 agent 表或可变 agent 配置。本地目标 SHALL 借用唯一 resident owner；`Run` 是协作边界适配：输入经统一事件入口进入该 owner 的处理管线，请求级上下文由公共处理器创建并接该 agent 自己的服务（含其 taskController），不能按陈旧 owner.config 先造上下文后补 lease，也不得为被调方另造单轮专用执行架构或对已存在 agent 再造完整壳。当前关联的同步结果按原语义返回发起 turn，不作为新顶层输入。

调用的 message/session 标识、metadata、外部上下文和投影 SHALL 保持在本次调用作用域；不得通过共享 activeBus、lastSessionID 或 pendingExternalEvents 临时传递而污染并发调用。原直接 Ingest API 的单次交接语义须同步保留，不新增持久消息路由协议。

后台派生调用脱离父取消上下文时 SHALL 保留同代执行绑定与来源 metadata；已结束任务的 Resume/Relaunch 若创建新的 Run，有发起者则继承其版本，无发起者则获取当前版本，再从任务所属 owner 的执行面解析目标。所选版无目标明确拒绝，不复活旧执行器；仍持 G1 的发起者不因 G2 删除目标而丢失合法 G1 绑定。

#### Scenario: 多级调用使用同版本不同 owner 视图

- **WHEN** A→B→C 正在 G1 调用，期间 G2 改变 B 的工具或 C 的模型
- **THEN** 三层保持 G1，并分别使用 G1 中 A/B/C 的执行描述；B 不误用 A 的直接工具表，后续独立调用才用 G2

#### Scenario: 去壳后同 owner 并发调用隔离

- **WHEN** 同一 resident B 接收两路不同 invocation/session/external_context 的并发调用
- **THEN** 各自投影、注入 keys、metadata 和输出只属于自身请求，收尾不改写另一调用的 bus／session；两路都使用 B 的正确资源与原 RunFlow

#### Scenario: 被调方仍是完整 tagent

- **WHEN** B 作为 A 的被调方处理输入，期间 B 以自己的任务管理器启动 C 或命令并越过首答
- **THEN** C 的结算先进入 B 的事件总线并由 B 处理，随后按原请求关联产生向 A 的输出；B 的 bus、任务域与资源不因首答结束被关闭或并入 A

#### Scenario: 进行中热更不改变子调用目标

- **WHEN** 父请求在 G1 开始（A 可调用 B），执行期间发布 G2（A 改为可调用 C）
- **THEN** 该请求的工具声明与实际子调用仍为 B；下一个开始的新请求调用 C

#### Scenario: 移除目标后的新调用被拒

- **WHEN** G2 移除了 agent B 后，模型依据新声明发起了对 B 的陈旧调用或显式重投
- **THEN** 调用被明确拒绝并说明目标不存在，不静默改投其他 agent 或复用旧执行器

### Requirement: 真实委派上下文穿透工具装饰层

冷启动、候选、回滚和子调用接线 SHALL 使用现有透明工具解包能力，将委派绑定到实际父调用的 projection；MUST NOT 因 OutputLimitTool 包裹而跳过，也不得把候选壳空投影或其他调用投影作为父上下文。构造候选或调用私有 wrapper 不得原地修改已发布共享 wrapper。

#### Scenario: 省略 event_keys 自动注入真实父上下文

- **WHEN** 配置了 event_keys 参数的委派工具经生产包装链被调用，模型未传 keys，实际父投影有可注入事件
- **THEN** 子 agent 实际收到按既有规则选出的上下文，不因外层 OutputLimitTool 而缺失；显式 keys 仍优先，空投影及不支持该参数的普通工具不被强行注入

#### Scenario: 热候选与并发子调用不混用投影

- **WHEN** G1 调用持有其父上下文，同时构建并发布 G2，且不同子调用各自有私有投影
- **THEN** G1 的实际上下文不被重绑，G2 使用正确常驻父上下文，各子调用的下一层委派读取其本调用投影，不读取候选壳或其他调用投影

### Requirement: 投递对账屏障的 booking 与投递期望一一配对

子调用环的投递对账屏障（delivery-accounting barrier）SHALL 保证每个 invocation 的 pending 计数与真实投递期望一一配对：包装 spawner 在内层 spawn 返回 inline settle、dedup 命中或被 gate 拒绝（Blocked）三种「本次调用不再拥有 settle 期望」的形态时，MUST 撤销（void）本次预登记的 booking；booking 的登记 MUST 保持先于内层 spawn（期望先于任务可能极快的 settle 而存在）。

#### Scenario: 同 key 任务在飞时重复发起（dedup single-flight）

- **WHEN** 子调用的某 turn 内对同 key 任务第二次发起调用，任务层 dedup 命中返回既有 active 任务
- **THEN** 本次调用的 booking 被撤销，屏障 pending 不因 dedup 而净增；既有任务 settle 时按其原发起调用的 booking 配对递减
- **THEN** 子调用环在该调用最后一个真实 settle 投递并排空总线后静默退出，不被无人消耗的 booking 钉住

#### Scenario: spawn gate 拒绝纳管（disk 退化）

- **WHEN** 子调用的某 turn 内 spawn 被 disk 退化闸拒绝（Blocked，工作已被取消跟踪）
- **THEN** 本次调用的 booking 被撤销，屏障不泄漏；调用环按正常静默条件退出

#### Scenario: inline settle 保持既有配对（回归对照）

- **WHEN** 内层 spawn 在同步等待窗口内结算（Settled=true）
- **THEN** booking 照旧被撤销（既有行为不变），屏障与环退出行为与本变更前逐字节一致

