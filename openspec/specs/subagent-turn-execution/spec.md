# subagent-turn-execution Specification

## Purpose

定义 sub-agent 工具调用的执行语义:一次调用 = 一次 `RunFlow` = 一个完整 turn。turn 边界由 `RunFlow` 的自然返回定义,而非对输出事件流的内容探测。sub-agent 与顶层持久循环共享同一个 turn 原语 `RunFlow`,仅循环包裹方式不同(顶层 `for { Pull; RunFlow }` 反应式守护;sub-agent 直调一次)。
## Requirements
### Requirement: Sub-agent 调用执行恰好一个 turn

Sub-agent 工具调用 SHALL 经**统一事件调度**执行：同一 invocation SHALL 由一个调度面驱动，**可包含多个 decision**（工具结果/后台观察回流驱动下一步 decision），MUST NOT 引入 LLMAgent 或任何**第二工具迭代循环**。本条**不冻结**历史类名/方法，亦**不以 500ms drain 定时器**或等价时间推定定义 turn 结束；**受理 ≠ 运行即成功**（受理只发一次受理回执，见「子 agent 调用可作为异步任务执行」）。

**turn 边界 ≠ invocation 完成**：一个 turn 的结束不构成该调用义务已结清——若子 agent 自身派生后台任务/通知义务，其 Task SHALL 待**本调用义务明确结清**后方 settle（见「子 agent 调用可作为异步任务执行」）。无工具调用的模型回复不能证明后台义务已完成。

#### Scenario: 多轮工具调用完整执行

- **WHEN** sub-agent 收到需要多次工具调用的请求（如 plan create 需依次执行 `openspec init`、`openspec new change`、写 tasks.md）
- **THEN** 统一事件调度在同一 invocation 内跑完所有工具轮次直到最终 assistant 响应
- **AND** sub-agent 不在任一中间工具结果处提前返回
- **AND** 单次 response 流结束 MUST NOT 关闭**仍有未结义务**的 invocation 输出/接收路径

#### Scenario: 慢 LLM 不触发提前返回

- **WHEN** sub-agent 的 LLM 每轮响应耗时远超任何固定 drain 窗口（如旧 500ms 推定，实际每轮约 16s）
- **THEN** sub-agent 仍执行到最终响应，不被任何时间推定/定时器截断
- **AND** 首个工具调用后的后续轮次正常执行

#### Scenario: turn 边界由最终响应结束定义但不作义务完成判据

- **WHEN** 一次 turn 的事件流在最终响应处结束
- **THEN** 该 turn 的驱动在最终响应处停止产出，**不冻结**承载执行体的 goroutine 与输出/接收通道实现形态
- **AND** 该 turn 结束 MUST NOT 单独作为「调用义务已结清」的判据；仍有未结义务时其输出/接收路径 MUST NOT 被关闭

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

每次 sub-agent 调用 SHALL 保持并发隔离与**原生 `CallableTool` 接口兼容**（`Call(ctx, jsonArgs)` 形态；业务受理回执属已明定变更）；**MUST NOT** 承诺「旧历史 API 返回值保持不变」。本条**不冻结**历史类名/方法。

隔离 SHALL 经 **invocation 作用域借用**实现——同一 sub-agent 实例被并发或连续多次调用时，各自决策、各自持有解析与去重身份，MUST NOT 共享可变游标/压缩器状态而互相污染；session SHALL 由应用显式选择并在本次调用作用域内使用。MUST NOT 每次调用创建**私有可写事件历史/投影真源**（第二份可写会话历史）；调用结束 MUST NOT 关闭或并入父的 state 会话真源。

#### Scenario: 每次调用按 invocation 作用域隔离

- **WHEN** 同一 sub-agent 实例被并发或连续多次调用
- **THEN** 各调用 SHALL 各自决策、互不串写；调用之间不共享可变的压缩器状态
- **AND** MUST NOT 通过为每次调用另建私有可写事件历史/投影真源来实现隔离

#### Scenario: 复用应用选择的 session 不新建真源

- **WHEN** 一次 sub-agent 调用执行
- **THEN** 其 SHALL 在应用显式选择的 session 内借用 state 同代绑定执行，session 历史归 state 持有
- **AND** MUST NOT 另建第二份可写会话历史或在调用结束时关闭/并入父的 state 会话真源

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

**子 agent 调用**（历史实现经 `AgentToolWrapper`；具体类名/方法名**不冻结**，见「Sub-agent 调用执行恰好一个 turn」）SHALL 纳入任务层作为异步 Task 执行。**受理一次**：调用方收到调用即 SHALL 立即返回一次**受理回执**（含 task id 与**接纳/等待**状态——**不是**「运行中」），受理**不等结果**——快任务也 MUST NOT 另走「同步等待窗口内内联最终结果」分支（原「最终响应返回即 settle / dense 阶段内内联返回 / `asyncDisabled` 回退强制同步」三类旧表述 SHALL 被本条取代）。

**模型 response 结束不等 invocation 完成**：子 agent 的最终 assistant 响应（历史实现即 `RunFlow` 返回；符号不冻结）只标志该 turn 的模型决策结束，MUST NOT 被直接判为该调用 `completed`；该调用的后台结果作为**独立 observation** 回流，驱动**下一步 decision**（仍属**原 invocation**，CallID 按 decision 作用域保真，见 `event-metadata-contract`），MUST NOT 冒充该调用的第二次同步应答。该 Task SHALL 只在**本调用义务明确结清**后 settle。

**子 agent 使用应用选择的 session**：子 agent 调用 SHALL 在**应用显式选择的 session**（见 01 `config-state-twinship`「session 身份与授权」）内执行，并借用 state 同代绑定；MUST NOT 为每次调用创建**私有可写事件真源**（第二份私有可写会话历史/投影真源）——并发隔离经 invocation 作用域借用实现（各自决策、各自解析与去重身份），而非另建可写历史。

子 agent 工具 SHALL 保持**原生 `CallableTool` 接口兼容**（`Call(ctx, jsonArgs)` 形态，见 `trpc-reuse.md` §2）；返回值为一次**受理回执**（属已明定的业务面变更，见上）；**MUST NOT** 承诺「旧历史 API 返回值保持不变」。本条**不冻结**历史类名/方法。不引入 LLMAgent 第二工具循环。

#### Scenario: 快子 agent 亦受理一次

- **WHEN** 一个纳入任务层的子 agent 的最终 assistant 响应在短时间内给出
- **THEN** 父 agent 的 `Call()` SHALL 已返回一次受理回执（task id + 接纳/等待状态），MUST NOT 因任务快而改走同步内联最终结果分支
- **AND** 其最终输出 SHALL 作为独立 observation 回流，驱动原 invocation 的下一步 decision

#### Scenario: 模型 response 结束不判 completed

- **WHEN** 子 agent 模型已给出最终 response，但其派生的后台义务尚未结清
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

子调用环的投递对账屏障（delivery-accounting barrier）SHALL 保证每个 invocation 的 pending 计数与真实投递期望一一配对。**配对以「本次调用是否为本次 invocation 新增了义务」为准**：内层 spawn 返回**成功受理回执**时，本次调用**新增了义务**（结果将随后台 observation 回流），其 booking SHALL **保持至该义务明确结清**，MUST NOT 因「返回受理回执」即撤销；仅当本次调用**未新增义务**——dedup 命中（复用既有 active 任务）或 gate 拒绝（Blocked，工作已被取消跟踪）——时，MUST 撤销（void）本次**预登记** booking；**既有任务原有的 booking/义务 MUST NOT 被本次 dedup/rejected 影响**。booking 的登记 MUST 保持先于内层 spawn（期望先于任务可能极快的结算而存在）。

**极快完成与注册竞态**（任务在 booking 登记与受理返回之间即已 settle，或注册与结算竞争）：受理与结算**各自是一次独立 mutation**，两次 mutation MUST NOT 被伪称同一次事务；SHALL 各自按**共同提交协议**有序、**幂等**对账，MUST NOT 依赖受理返回的顺序判定是否结清，不产生「先撤后结」的双重配对或遗漏，屏障 pending MUST 最终归零。

#### Scenario: 同 key 任务在飞时重复发起（dedup single-flight）

- **WHEN** 子调用的某 turn 内对同 key 任务第二次发起调用，任务层 dedup 命中返回既有 active 任务（本次调用**未新增义务**）
- **THEN** 本次调用的**预登记** booking 被撤销，屏障 pending 不因 dedup 而净增；**既有任务**结算时按其**原发起调用**的 booking 配对递减（既有义务不受本次 dedup 影响）
- **AND** 子调用环在该调用最后一个真实结算投递并排空总线后静默退出，不被无人消耗的 booking 钉住

#### Scenario: spawn gate 拒绝纳管（disk 退化）

- **WHEN** 子调用的某 turn 内 spawn 被 disk 退化闸拒绝（Blocked，工作已被取消跟踪，本次调用**未新增义务**）
- **THEN** 本次调用的**预登记** booking 被撤销，屏障不泄漏；调用环按正常静默条件退出

#### Scenario: 成功受理保持义务（取代旧「受理即撤销 booking」）

- **WHEN** 内层 spawn 返回**受理回执**（本次调用已受理，结果将随后台 observation 回流）
- **THEN** 本次预登记 booking SHALL **保持至义务明确结清**，MUST NOT 因受理即撤销
- **AND** 结清时按该 booking 配对递减；仅有未新增义务的 dedup 命中/被 gate 拒绝才撤销本次预登记

#### Scenario: 极快完成与注册竞态按共同提交协议幂等对账

- **WHEN** 任务在 booking 登记与受理返回之间即已 settle（极快完成），或注册与结算发生竞争
- **THEN** 受理与结算各自作为独立 mutation 按共同提交协议**有序、幂等**对账，MUST NOT 依赖受理返回顺序判结清、MUST NOT 伪称单次事务，不产生「先撤后结」的双重配对或遗漏
- **AND** 屏障 pending MUST 最终归零，不泄漏、不钉住

