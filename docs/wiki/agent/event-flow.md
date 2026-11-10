> **本篇是当期契约**：代码注释 `// 契约: docs/wiki/agent/event-flow.md#<锚>` 指向本页各节。实现面在 `runtime/agent/`（`loop.go`/`consumer.go`/`invocation.go`/`request_assembly.go`/`final_round.go`/`final_round_planner.go`）与 `runtime/state/`（`occupancy.go`/`authority.go`），投影面在 `memory/replay/`（`projection.go`/`replay_budget.go`/`age_segments.go`），装配面在 `assembly/`（`facts.go`/`request.go`/`age_fold.go`/`compress_consumer.go`/`host_delivery.go`）。

# 一条链，一条事件流：从输入到交付

三个概念分清楚，本页每个判据都落在其一：**链**管身份与因果（轻、恒可重放）· **原文仓**管字节（重、按需取、可过期）· **账本**管责任（任务迁移、消费回执、交付七态）；其余一切现算。白话版见 [storage-concept-map.html](../../demos/storage-concept-map.html) 与 [storage-time-travel.html](../../demos/storage-time-travel.html)。

<a id="event-stream-overview"></a>
## 一、事件流总览：四步一个环，四块一条事件
环只有四步（`event-loop` 的四步，见 [docs/EXTENSIONS.md](../../EXTENSIONS.md)）：**Ready**（这批事件是谁的、给谁看）→ **消费**（消费循环拉取一次批次，有驱动事件时一次模型请求）→ **Backflow**（回合进行中读到的终结结算）→ **投递**（按冻结路由交给目标）。一条事件有四个块：identity（事件键）、content（角色与正文）、ownership（分区与实例）、ledger（父键与因果）；`RoleOf` 把投影类型映到模型角色，表外类型按 `ErrProjectionTypeUnmapped` 具名拒绝——不猜、不降级成某一种角色。

<a id="runner-flow"></a>
## 二、一个回合在核内的流转
回合不再由入口启动，而由一个常驻消费循环推进：每个 `(agent, session)` 地址一条 goroutine（`runtime/agent/consumer.go` 的 `Consumer.run`），等 `Wake` → `pullDriving` 拉取已到达的事件 → 按事件类型分派 → 有驱动事件时开一段并调用一次模型 → 回到拉取，不等派发的工具返回。`agent.Agent.Decide` 拿到的是一次装配好的请求，模型只提供词汇。一段（segment）的顺序是：`openSegment`（铸范围身份 `<session>#cN`，把新材料按 `aggregateReady` 合进 transcript）→ 循环 `decide`（`model.Model.GenerateContent` 出流，`model.ReduceChannel` 做**唯一一次**终结归约）→ 决策与工具调用事实落链、派发即返回 → 结果以事件回流，`harvest` 非阻塞配成观察续下一轮 → 无工具调用的最终回复即 `closeSegment`（`commitTranscript` + `commitOutput` 落 `agent_output`）。全部入口收敛常驻消费循环：`Host.Invoke`、`Run.Invoke`、`Run.InjectEvent`（冥想注入）、一次投递落到空闲目标的那一轮与模型可见 send 都只提交事实并唤醒目标地址，同步调用方等待其输入所属闭合段的不可变结果，不存在直驱例外。三处边界写死：归约只发生一次（不二次解释流机制）；工具调用只经适配器的 call 面；本回合的 `Turn{DecisionCount,Terminal,Failure,FinalRound}` 是唯一结果记录，不另立第二份。

**实例面是每个地址一个**：`Consumers.Ensure` 按材料自己声明的完整地址装配并启动实例（`runtime/agent/consumer.go`），入口 agent 之外也挂——委派会址 `<调用方会话>#sub:<原生调用号>` 就靠它得到自己的驱动者。三条硬性质：同一地址重复 Ensure 回已挂的实例，一个地址绝不跑两个驱动者；生命周期就是 run 的，集合已停则一次调用答“无实例”而不是另起驱动者；实例的读游标从链上最后一条 `agent_output` 之后接着读，重启后未答复的输入仍然待办。宿主入口因此只有一条路径：`Host.Invoke` → 提交输入事实 → `Ensure` → `Wake` → `WaitAnswer`（`assembly/host.go` 的 `invokeConsumed`），“调用方阻塞等目标回合跑完”的同步直驱不再是常规驱动路径。

**空闲回收只收没有活的实例**：常驻实例不是无限期缓存（`runtime/agent/recycle.go`）。集合按固定节奏扫一次静止地址，一条地址连续静到空闲窗（缺省十分钟，`SetIdleTTL` 可改；负值=不回收，零值=回缺省）才回收，回收后该地址的下一次输入按新挂载从链上重建，语义与首次启动一致。动工中的地址与**仍欠投递的地址**一律不回收：`SetDeliveryPending` 注册的谓词说明该地址还有未结清的投递义务（held 或 owed 的投递事实），有它就留多久都不收。回收是可观测的：`RecycleCount` 计数 + 一行带地址与原因的日志，不靠猜实例为什么重建。

<a id="event-pipeline-atomic"></a>
## 三、事实提交：存储与投影同点，幂等按身份
事实落链与投影追加是**同一次提交**（`assembly/facts.go:recordMapped`）：先 Append 事实，再按**身份**移动滚动边界。四条判据：同一条事实重投不产第二条（`sameFactOf` 按身份判等）；同一身份带不同内容按 `storedFactConflict` 拒，不覆盖；提交失败即整条不进（不先投影后存储）；边界移动与事实同点，读者永远看不到「已可见但未投影」。

<a id="projection-lifecycle"></a>
## 四、投影生命周期：边界按身份记，预算是每次现算
投影是 `ReplayProjection`（entries/seen/originals/consumed/fold/boundary/recovery），`Append` 按事件键幂等。滚动边界记的是**身份**（`replayEntry.endedAt`），不是位置——折叠会重写序列，位置边界会在重写后指错对象。边界**只在两种时刻移动**：段闭合时移向那条 `agent_output`（`endedPrefix` 解读时含自己），无飞行段时移向新开范围的输入（不含自己，它归该段携带）——段飞行中落链的输入不动边界。做法在 `assembly/facts.go` 的 `recordMapped`（`rangeOpen`），解析在 `assembly/request.go` 的 `endedPrefix`：于是已闭合的段整段呈现为前缀、正在飞的段由 transcript 携带，同一条事实不会被读成两条。边界内的判定 `admitsRange`：`TypeExternalInput`，或 `TypeFeedback` 且触发源为 `task`（`TypeExternalInput` 中带 `delegated_part=instruction|material` 的两类不算：委派批次随它自己的 task 进段，不另开范围）；换言之能被拿来回放的只有外部输入与本 agent 自己的随动反馈。呈现预算是**每次装配现算**（`AssembleBudget`/`AssembleBudgetPrefix`），绝不读回上一次的形态：每一条目按所剩 room 取第一个装得下的形态，`Full → Ticket → Summary → notice card`；notice 类（feedback 族）**可缩不可丢**，4 字符/令牌是唯一换算常数。

<a id="unified-compression"></a>
## 五、三龄段折叠：折叠的是呈现，不是链
旧的分级压缩器已整体退场，现在是**三龄段折叠**，而段的归属由**世代递推**决定、不由绝对段龄决定：`bandOfSegment` 按「这一段是否已持早前折叠的产物」定档——窗口内（`DefaultAgeKeepRecent=2`）或仍在进行的段＝`BandNew`，离开窗口且仍无产物＝`BandMid`，已持 `segment_fold`/`history_summary` 产物＝`BandOld`；`LevelOfAge`/`BandOfAge`/`BandOfLevel`/`Segment.Level` 降为年龄阶梯的诊断读，不参与分派。`FoldByAge`/`PlanByAge` 只在体积达阈（`DefaultAgeVolumeThreshold=4096` 字节）时动手，按档分职：新档原样保留、中档的过程细节（`thinking_plan` + `action_command`）折成一条 `segment_fold`（丢过程、留 input/output 骨架）、老档折成一条 `history_summary` 置顶。于是第一次折叠只把材料从新晋到中（`fold²≠fold¹`），第二次才把中晋到老并收成「一条摘要＋最近一档」，此后稳定：不变量是**体积全程有界＋迭代收敛到不动点**，不是即时的 `fold(fold(x))==fold(x)`。产物用**内容寻址负键**（`-seg-<hex>`/`-hist-<hex>`），同内容不产第二件，因此重复触发是幂等的。每次折叠判定按它将要铸的每个产物落一条 `compress.request` 事实（`AgeProducerSegmentAge`）；门内（体积未达阈，或新档之外没有可折的档）不落判定，提交照常。判定与执行分成两步：`assembly/age_fold.go` 的 `AgeFoldPlugin` 只判定——按体积与龄段调 `PlanByAge` 并提交 `compress.request`（折什么由投影自己的规则定）；`assembly/compress_consumer.go` 的 `CompressionConsumer` 读 request 执行折叠并提交 `compress.product`（`Key`/`Covered`/`Text`/`Producer`），失败只记具名 gap——不回滚、不重跑。两个都是 `fact->observation` 的 PostCommitObserver，**事实提交之后**才动作。重启时 `assembly/run.go` 的 `seedReplay` 重建投影：checkpoint 已覆盖的链从切片幸存者水合、只回放偏移后事实，链上的 `compress.product` 重建合成条目（`memory/replay/age_plan.go` 的 `RestoreProducts`），不重折：产物身份落链后稳定（恢复载体见[平台篇 §六](../platform/platform-subsystems.md#framework-checkpoint)）。原文仍在原文仓，票据与 recall 可达，折叠只改这一次要发出去的那张脸。渲染侧另有一道单点**双向**的配对校验（`assembly/request.go:restoreCallPairing`）：配不上调用的结果降级成 user 侧输入注记（内容与关联 id 都留），结果不在序列里的 assistant tool_calls 被剥离——**系统永不生成文本调用语法**，因为任何文本调用语法都会被模型模仿成伪调用。老档摘要的正文由模型生成、由事实定案：`assembly/summary_gist.go` 的 `bandSummaries` 面把老档的机械读（每行已带 `[MM-DD HH:MM|gen:…]` 锚）交给该会话 agent 的模型，回答即摘要正文；`assembly/compress_consumer.go` 的 `summarizeProduct` 在折叠这一批产物时取这段 gist、按 `DefaultAgeSummaryTextLimit`（2048 字节，登记 `golden/runtime/window_bounds.json`）裁到界内，再随 `compress.product` 的 `Text` 落链，幸存者检查点也从这段正文算起。三条降级都不阻塞折叠：会话的 agent 没有模型（`ErrNoSummaryModel`）、请求失败或终态不是 completed、回答为空——一律保留折叠自己铸的**机械有界型**（最新行照留、更老的行退化为 `+N earlier entries covered by key`）。模型访问只在 assembly 一侧发生，`memory/replay` 仍是纯读：回放与恢复读的是链上那条 `compress.product` 的正文，所以重启后的摘要与在线时逐字一致，也不会为恢复再问一次模型（重启期 parity 见[平台篇 §六](../platform/platform-subsystems.md#framework-checkpoint)）。

<a id="e2e-turn-sequence"></a>
## 六、一次完整回合的端到端时序
宿主端点收请求（`assembly/host_face.go`：**先鉴权后读体**，未认证即 `ErrHostUnauthenticated`）→（user 地址）提交输入事实 → `Wake` 该地址的消费循环 → `WaitAnswer` 等消费了自己输入的那段闭合（`assembly/host.go` 的 `invokeConsumed`）；消费循环 `openSegment` 铸范围身份 `<session>#cN`，每次决策前 `Occupy` 占决策席（`ErrSlotHeld` 写面单飞互斥仍在；冥想让路与 Sites 查询等读面改读 `Loop.InFlightModel`）→ `RequestFace` 装配请求（布局三段：**稳定前缀**=声明 prompt 一条 system、声明技能正文一条 system、压缩摘要作 user 级注记、assistant 恒等于模型真实产出；**transcript**；**动态段**=运行时加载的技能正文加 `## Environment` 块，一条 system，是请求最后一条消息，收尾指令在前）。稳定内容前置、可变内容收尾，为的是服务端前缀缓存只截断末尾一段；工具定义走 `Request.Tools`，其顺序由 `ToolOrder` 表达（发布代工具在前名序，运行期后加载工具在后注册序），adapter 的缓存重排关闭，家族特殊 token 由 adapter 按惯例渲染→ `Decide` → tool 调用经适配器执行、结果作为事实提交 → 决策与通知提交 → 终结即 `Vacate` 席位 → 段闭合时 `closeSegment` 回调 `SegmentDelivery`（`assembly/host.go` 的 `deliverClosedSegment`），由 `assembly/host_delivery.go` 的 `deliverFresh` 按冻结路由投递，未接受的保留义务。`Backflow` 是回合进行中读终结结算的那条缝：`progress` 落成事实，`settled`/`failed`/`cancelled` 才进入终结处理。

<a id="trace-anchor"></a>
## 七、trace 锚点：span 与轨迹两条投影必须互链，且不得造假
事实带归因章（rollout_id / trace_id / span_id），读者能从一条事实跳到对应 span。三条底线：noop span（没有真实 SpanContext）一律**降级为空值**而不是伪造 ID；无效 SpanContext 不得产出可用 ID；link 为空就**不建 link**——用假链掩盖断链，比断链本身更难查。

<a id="multi-user-isolation"></a>
## 八、多用户并发：读权按对授权，调用元数据随输入流动
一个常驻会话会同时收到多个主体的输入，隔离是两条独立判据。**读权**：`state.Authority` 只记「某主体可读某目标地址」这一对（`GrantRead`），检查只读已记授权、**从不顺手记一条**（问过不等于有权），空白授权拒绝一切读（`ErrReadNotAllowed`）；授权只覆盖那一个 subject×target 对，不扩散到邻会话。**归属**：产出只能携带**发起它的那条输入**的会话标识，调用元数据只能随自己的输入流动，不得暂存在共享实例状态上。症状决定了它必须单独存在：投递目标按会话标识回退选择，一旦串了用户，后果是「给 A 的回信送进 B 的会话」——不报错、不可撤销、事后无法从日志还原。

<a id="watchdog-final-round"></a>
## 九、看门狗收尾轮：预算到期的下一次决策就是收尾轮
`FinalRoundPolicy{MaxRounds,Budget,Prompt,Timeout}` 装在 `Loop.SetFinalRound` 上，四条红线：两个预算都为 0 时 drive 恒无界；预算只在**回合边界**读，绝不切断已在途的决策；**至多跑一轮**收尾轮，它返回即结束 drive；收尾轮发出的工具调用**一律不受理**——每个调用以配对回执如实拒绝（`finalRoundRefusal`），因为"交付"不该开新义务。收尾轮有三处标记：请求边界 `RequestBoundary.FinalRound`、尾部携带收尾指令（动态段在其后，仍是最后一条）、按自己的短超时（缺省 30s）运行；收尾轮读链得到的在途输入不进本回合（直驱路径），留作下一回合的历史；`Turn.FinalRound` 报出原因是 `rounds` 还是 `budget`（自终则空）。收尾之后照旧走 `Host.Quiesce`／`Run.Close` 的真实终结判定（`CloseOutcome` 四值：零值 `CloseUnreached` ＋ `Quiesced`/`Cut`/`Broken`，`Cut` 是正常返回）——上限生效与优雅收尾两全。判定本身是链上事实：`runtime/agent/final_round_planner.go` 的 `FinalRoundPlanner`（PostCommit 观察者）按 policy 把「本 range 的预算已耗尽」提交成 `final_round.request`（`FinalRoundDue` 与消费循环同源判定、内容寻址，一 range 至多一条）；消费循环在每次决策前按本段 range 读这条事实（`runtime/agent/consumer.go` 的 `closingRoundOf`），读到即本次为收尾轮，读不到才回落本地判定兜底——兜底不补事实，判定与执行各归一处。

## 已知缺口与演进方向
| 缺口 | 现状与防线 | 候选方向 |
|---|---|---|
| 折叠阈是字节数，不是模型视角的价值 | 体积阈能挡住无界膨胀，但「哪一段值得留」仍只看年龄与体积 | 以消费回执与票据命中率作第三判据 |
| 呈现降级对读者不可见 | `Full→Ticket→Summary` 的降级只体现在发出文本里，没有一条「这一条被降级了」的旁路记录 | 降级计数进 telemetry 的消耗面 |
| 循环按地址分片 | 每个 `(agent, session)` 一个常驻消费循环；同一会话的多主体仍靠授权与单飞席位隔离，跨实例仍只有一个写者 | 跨实例的循环归属与迁移 |
