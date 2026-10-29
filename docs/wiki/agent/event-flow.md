> **本篇是当期契约**：代码注释 `// 契约: docs/wiki/agent/event-flow.md#<锚>` 指向本页各节。实现面在 `runtime/agent/`（`loop.go`/`invocation.go`/`request_assembly.go`/`final_round.go`）与 `runtime/state/`（`occupancy.go`/`authority.go`），投影面在 `memory/replay/`（`projection.go`/`replay_budget.go`/`age_segments.go`），装配面在 `assembly/`（`facts.go`/`request.go`/`age_fold.go`/`host_delivery.go`）。

# 一条链，一条事件流：从输入到交付

三个概念分清楚，本页每个判据都落在其一：**链**管身份与因果（轻、恒可重放）· **原文仓**管字节（重、按需取、可过期）· **账本**管责任（任务迁移、消费回执、交付七态）；其余一切现算。白话版见 [storage-concept-map.html](../../demos/storage-concept-map.html) 与 [storage-time-travel.html](../../demos/storage-time-travel.html)。

<a id="event-stream-overview"></a>
## 一、事件流总览：四步一个环，四块一条事件
环只有四步（`event-loop` 的四步，见 [docs/EXTENSIONS.md](../../EXTENSIONS.md)）：**Ready**（这批事件是谁的、给谁看）→ **消费**（本调用一次 drive）→ **Backflow**（回合进行中读到的终结结算）→ **投递**（按冻结路由交给目标）。一条事件有四个块：identity（事件键）、content（角色与正文）、ownership（分区与实例）、ledger（父键与因果）；`RoleOf` 把投影类型映到模型角色，表外类型按 `ErrProjectionTypeUnmapped` 具名拒绝——不猜、不降级成某一种角色。

<a id="runner-flow"></a>
## 二、一个回合在核内的流转
回合由 `Loop.Drive` 自己驱动，不再借框架 Runner 消费一张外部消息表：`agent.Agent.Decide` 拿到的是一次装配好的请求，模型只提供词汇。一次 drive 的顺序是：`observeReady`（ready 里的每一条事件按下标成事实，空键跳过）→ 会话历史 + ready 合成 transcript → 循环 `decide`（`model.Model.GenerateContent` 出流，`model.ReduceChannel` 做**唯一一次**终结归约）→ 决策落事实并提交通知 → 终结即停。三处边界写死：归约只发生一次（不二次解释流机制）；工具调用只经适配器的 call 面；本回合的 `Turn{Decisions,Terminal,Failure,FinalRound}` 是唯一结果记录，不另立第二份。

<a id="event-pipeline-atomic"></a>
## 三、事实提交：存储与投影同点，幂等按身份
事实落链与投影追加是**同一次提交**（`assembly/facts.go:recordMapped`）：先 Append 事实，再按**身份**移动滚动边界。四条判据：同一条事实重投不产第二条（`sameFactOf` 按身份判等）；同一身份带不同内容按 `storedFactConflict` 拒，不覆盖；提交失败即整条不进（不先投影后存储）；边界移动与事实同点，读者永远看不到「已可见但未投影」。

<a id="projection-lifecycle"></a>
## 四、投影生命周期：边界按身份记，预算是每次现算
投影是 `ReplayProjection`（entries/seen/originals/consumed/fold/boundary/recovery），`Append` 按事件键幂等。滚动边界记的是**身份**（`replayEntry.endedAt`），不是位置——折叠会重写序列，位置边界会在重写后指错对象。边界内的判定 `admitsRange`：`TypeExternalInput`，或 `TypeFeedback` 且触发源为 `task`；换言之能被拿来回放的只有外部输入与本 agent 自己的随动反馈。呈现预算是**每次装配现算**（`AssembleBudget`/`AssembleBudgetPrefix`），绝不读回上一次的形态：每一条目按所剩 room 取第一个装得下的形态，`Full → Ticket → Summary → notice card`；notice 类（feedback 族）**可缩不可丢**，4 字符/令牌是唯一换算常数。

<a id="unified-compression"></a>
## 五、三龄段折叠：折叠的是呈现，不是链
旧的分级压缩器已整体退场，现在是**三龄段折叠**：`LevelOfAge(age, keepRecent)` 给段定级（`DefaultAgeKeepRecent=2`），`BandOfLevel` 把它们合成三档——`BandNew`（L0 原样）、`BandMid`（L1/L2）、`BandOld`（L3）。`FoldByAge` 只在体积达阈（`DefaultAgeVolumeThreshold=4096` 字节）时动手：新段的过程细节（`thinking_plan` + `action_command`）折成一条 `segment_fold`；中段与老段折成一条 `history_summary` 置顶。产物用**内容寻址负键**（`-seg-<hex>`/`-hist-<hex>`），同内容不产第二件，因此重复触发是幂等的。每次折叠判定落一条 `compress.request` 事实（`AgeProducerSegmentAge`），折没折都留痕。执行者是 `assembly/age_fold.go` 的 `AgeFoldPlugin`：一个 `fact->observation` 的 PostCommitObserver，**事实提交之后**才折，失败只记具名 gap——不回滚、不重跑、不写事实。原文仍在原文仓，票据与 recall 可达，折叠只改这一次要发出去的那张脸。渲染侧另有一道单点**双向**的配对校验（`assembly/request.go:restoreCallPairing`）：配不上调用的结果降级成 user 侧输入注记（内容与关联 id 都留），结果不在序列里的 assistant tool_calls 被剥离——**系统永不生成文本调用语法**，因为任何文本调用语法都会被模型模仿成伪调用。

<a id="e2e-turn-sequence"></a>
## 六、一次完整回合的端到端时序
宿主端点收请求（`assembly/host_face.go`：**先鉴权后读体**，未认证即 `ErrHostUnauthenticated`）→ 装配 `Invocation{InvocationID,Address,Instance,Ready}` → `Loop.Drive`：ready 逐条成事实 → 每轮决策前 `Occupy` 会话决策席（同址已有在途即 `ErrSlotHeld`，不并行不插队）→ `RequestFace` 装配请求（固定布局：system 单条恒首位、压缩摘要作 user 级注记、assistant 恒等于模型真实产出）→ `Decide` → tool 调用经适配器执行、结果作为事实提交 → 决策与通知提交 → 终结即 `Vacate` 席位 → 通知由 `assembly/host_delivery.go` 按冻结路由投递，未接受的保留义务。`Backflow` 是回合进行中读终结结算的那条缝：`progress` 落成事实，`settled`/`failed`/`cancelled` 才进入终结处理。

<a id="trace-anchor"></a>
## 七、trace 锚点：span 与轨迹两条投影必须互链，且不得造假
事实带归因章（rollout_id / trace_id / span_id），读者能从一条事实跳到对应 span。三条底线：noop span（没有真实 SpanContext）一律**降级为空值**而不是伪造 ID；无效 SpanContext 不得产出可用 ID；link 为空就**不建 link**——用假链掩盖断链，比断链本身更难查。

<a id="multi-user-isolation"></a>
## 八、多用户并发：读权按对授权，调用元数据随输入流动
一个常驻会话会同时收到多个主体的输入，隔离是两条独立判据。**读权**：`state.Authority` 只记「某主体可读某目标地址」这一对（`GrantRead`），检查只读已记授权、**从不顺手记一条**（问过不等于有权），空白授权拒绝一切读（`ErrReadNotAllowed`）；授权只覆盖那一个 subject×target 对，不扩散到邻会话。**归属**：产出只能携带**发起它的那条输入**的会话标识，调用元数据只能随自己的输入流动，不得暂存在共享实例状态上。症状决定了它必须单独存在：投递目标按会话标识回退选择，一旦串了用户，后果是「给 A 的回信送进 B 的会话」——不报错、不可撤销、事后无法从日志还原。

<a id="watchdog-final-round"></a>
## 九、看门狗收尾轮：预算到期的下一次决策就是收尾轮
`FinalRoundPolicy{MaxRounds,Budget,Prompt,Timeout}` 装在 `Loop.SetFinalRound` 上，四条红线：两个预算都为 0 时 drive 恒无界；预算只在**回合边界**读，绝不切断已在途的决策；**至多跑一轮**收尾轮，它返回即结束 drive；收尾轮发出的工具调用**一律不受理**——每个调用以配对回执如实拒绝（`finalRoundRefusal`），因为"交付"不该开新义务。收尾轮有三处标记：请求边界 `RequestBoundary.FinalRound`、请求尾部携带收尾指令、按自己的短超时（缺省 30s）运行；`Turn.FinalRound` 报出原因是 `rounds` 还是 `budget`（自终则空）。收尾之后照旧走 `Shutdown.Close` 的真实终结判定——上限生效与优雅收尾两全。

## 已知缺口与演进方向
| 缺口 | 现状与防线 | 候选方向 |
|---|---|---|
| 折叠阈是字节数，不是模型视角的价值 | 体积阈能挡住无界膨胀，但「哪一段值得留」仍只看年龄与体积 | 以消费回执与票据命中率作第三判据 |
| 呈现降级对读者不可见 | `Full→Ticket→Summary` 的降级只体现在发出文本里，没有一条「这一条被降级了」的旁路记录 | 降级计数进 telemetry 的消耗面 |
| 单会话循环 | 一次 drive 服务一个地址；多主体靠授权与席位隔离，不是多循环 | 会话路由层（多循环共享链与原文仓） |
