# 设计决策（D1–D9）

## D2 子域表

| # | 子域 | spec 能力 | 一句话边界 | 外部依赖 |
|---|---|---|---|---|
| 01 | decision-boundary | `persistent-event-loop`（ADDED） | 在途 turn 的每个 decision 边界重新聚合链上就绪事件（含用户/外部输入）；host Invoke 受理语义与 gate 降级 | `persistent-event-loop` 现条款（:94/:105/:123 保留不动） |
| 02 | meditation-activity | `meditation-idle-gating`（MODIFIED） | 节奏门加用户事件新鲜度闸与决策席在途闸；自管回合不重置 | `event-metadata-contract` 的 trigger_source 读径（相同，不新建） |

## D1 决策边界就绪增量（域 A 核心；grill P0-1/P1-3/P2-14 修订）

- `Loop` 新增窄读面 `ReadyFeed`（消费方定义）：`func(ctx context.Context, addr event.Address, after event.EventID) []event.Event`——按会话读 `after` 之后的**可入 ready 事实**；装配注入；nil=现行为（兼容）。
- **可入判据=复用 `admitsRange`**（assembly/facts.go:242：`TypeExternalInput` + task 章的 notice），并显式排除本 range 已消费键。**红色约束：progress notice（TypeFeedback）绝不进入任何 decision 输入**（既有红线 loop.go:221）；drive 自己落的 thinking_plan/agent_output 不入 ready（同判据自然排除）。
- `Drive` 每轮 `decide` **之前**做增量：拉 `admitsRange` 增量→并入 transcript（按下标成事实位点）；`observeReady` 的 After 趟对**增量**重跑且按 EventKey 去重（同事实不重复观察）。
- **不变量：在途消费不移动 projection 界标 `endedAt`**——`admitsRange` 对「range 在途且 InvocationID 不同」的事实 SHALL 只记入 ready 水位、不移动已结束材料界标（否则 prefix 与 transcript 双份携带；见 P0-1）。可重放边界由 `replayEntry.endedAt` 承担（`Event` 无 inputs 字段，Decision 只活在内存——P2-14 修正原论证）。
- 首个 decision 语义不变（ready=Invocation.Ready 聚合同样适用）。
- **消费水位具名面（P1-4）**：`lastConsumedReady event.EventID`——由 `Loop.intake` 持有；初值=首轮 `Invocation.Ready` 末键；推进点=每次增量并入 transcript 之后；驱动以错误结束**不推进**；`ReadyFeed(after)` 只负责范围，排除已消费由该游标负责。**账本分层**：受理账本复用既有 mailbox（`AdmitInbound`/claim/consume）；`lastConsumedReady` 只是**呈现**账本（决定 transcript 并入），不与 mailbox seq 竞争。
- **在途位具名面**：`replayEntry` 增加 `rangeInFlight bool`（Drive 起赋值、返回前清除）；`admitsRange` 的「在途不移动 endedAt」判据读它。

## D2 host Invoke 受理语义（域 A；grill P0-2/P1-4/P1-10 修订）

- **顺序不变量：落链先于 gate**——`Run.LandInput(ctx, addr, msgs) ([]event.Event, error)`（从 Run.Invoke 抽出；`assembly/invoke.go` 进 D3 白名单）在 `lockAddressContext` **之前**执行；受理事实以在途 range 的 `InvocationID` 落链（或同 range 水位归属），保证投影界标不移动（D1 不变量）。重复落链幂等：同内容同 session 未消费水位内的重复提交 SHALL 归并（不重复入 transcript）。
- **受理返回值确定化**：在途受理返回 `Turn{InvocationID: 受理 id, Terminal: model.KindUnknown}` 并携带新哨兵 `agent.ErrAcceptedInFlight`（调用方可辨）；**仅对用户入口生效**——follow-up 路径（fireNoticeTurn/driveSeat）仍经 gate 阻塞拿完整 Turn，`ErrNoticeTurnIncomplete` 语义一字不变。D2 旧分支③（挂 gate 等起 turn）删除：无在途但 gate 被占的微窗口=以「在途」同径受理（消费方是 gate 持有者的收尾 re-check）。
- **末决策兜底（P0-2）与驱动归属（P1-1）**：`Drive` 返回前做增量 re-check（未消费增量→续跑）；驱动归属用 **handoff latch**（每地址 `gateSeat{mu, held, closing, nextDrive}`，与 gate 同锁）替代二次裁定的 check-then-act：受理方落链后持锁——held/closing→`nextDrive=true`（由持有者承接）；否则 `held=true` 自驱。持有方收回后持锁——`nextDrive`→承接续驱；否则关闭。**保证分工：至少一次消费由 latch，至多一次由 `lastConsumedReady` 水位**。
- **Host 唯一组合缝**：`Run.InvokeLanded(ctx, req, ready []event.Event)`——LandInput 已落链后的驱动入口；Host SHALL NOT 再调 `Run.Invoke`（防二次落链+另起驱动）。

## D3 gate 降级为 turn 发起权门（域 A）

`addressGate` 语义从「消息门」改为「**turn 发起权门**」：同 session 仍至多一个在途 turn（决策席串行不变）；gate 不再阻塞消息入链，只仲裁「谁起 turn」。`ErrSlotHeld`（决策席）与 `ErrShutdownRefused` 语义不变。

## D4 外部输入规格条款（域 A delta）

`persistent-event-loop` ADDED Requirement「在途 turn 的外部输入在 decision 边界消费」：外部输入（`source==user`/渠道）SHALL 先成事实；在途 turn SHALL 在下一 decision 边界聚合并消费，SHALL NOT 排队至 turn 结束另起 turn，SHALL NOT 被执行中的 decision 打断消费。Scenario 两条（在途消费/无在途起 turn）。**不改** :94/:105/:123 的 task_settled 条款（两者同构，条款并列）。

## D5 冥想活跃闸（域 B）

- 判据数据源与 novelty 门同源但**归一化**（P1-9）：「用户事件」= `TypeExternalInput` 且 (`hostFacingStamp(trigger_source)` **或** `trigger_source=="" && owner==宿主用户地址`的兼容项)——与 judgment.go 的 host-facing 词表 `{user,task,reincarnation,system_alert}` 同源、`hostFacingStamp` 直接复用；**`trigger_source==meditation` 恒不计入活跃**（冥想自身注入显式排除，不再依赖 session 不同这一巧合）；driver 记录 per-observation-namespace `lastUserEventAt`；fire 前判：`now - lastUserEventAt < min_gap` → **不 fire**；自管谱系不更新（家务不阻塞保留）。
- 决策席在途（P1-5 主体钉死）：判据=「观察面内最新用户事件所属 session 的决策席被占用」——多 session 聚合规则=**任一被测 session 占用即抑制，全空闲才 fire**；装配注入窄读面 **`SlotQuery func(addr event.Address) bool`**（语义=该地址 addressGate 被持有=**turn 门**，与 D3 的 gate 同面；`state.Occupancy` 的 instance 维度显式不参与本读面；原因：D3 后 turn 粒度串行由 gate 承担，decision 席是另一层）。多 session 聚合规则随 delta 场景同步（任一被测 session 占用即抑制）。
- 抑制语义（P1-6/P0-1 修正）：抑制=**gateDeferred 计数+水位不动+保留观察面覆盖**（新增独立计数器，不混入既有 pending）；**重评由一次性 rearm timer 承担**（复用既有 rearm 语义，非周期 ticker）：闸未开时 retained=true 且重评点=冷却到期时刻（`now + min_gap - lastUserEventAt`），到期即评一次；**禁止**在抑制期创建 pending（pending 既有语义=已注入待 outcome，闸在注入之前，两类不得混用）。
- 与 `meditation-idle-gating` 现条款关系：MODIFIED 节奏门 Requirement——保留「时间门槛禁令」原文（反时间阈值），新增「用户事件新鲜度」与「决策席在途」两侧判据（结构性判据，非时间门槛）；其余条款（novelty/让位/水位执行语义）一字不动。

## D6 被否方案

| 方案 | 否决理由 |
|---|---|
| 立即打断（interrupt 语义：取消在途工具/丢弃未归约流） | 破坏决策因果重放（每个 decision 输入须=链上事实集）；用户已裁定取决策边界消费 |
| 双轨开关（配置选忙时保留/即时消费） | 双轨维护成本高；规格已是 decision 边界语义，实现对齐即可，无需模式 |
| 规格推翻忙时保留条款 | 考古证明规格本就是「下一轮 decision 消费」；推翻无对象，用户推翻的是实现现状 |
| 冥想用纯时间门槛判忙闲 | 规格明文禁止（回合时长无上界）；改用结构性判据（用户事件新鲜度+席位占用） |

## D3'（波次表）与 D5'（三级验收）

- 波次：W1=01 域 A（A1 红锚→A2 loop 增量→A3 装配注入→A4 host 语义→A5 规格 delta+锚迁移）；W2=02 域 B（B1 红锚→B2 driver 闸→B3 规格 delta）；W3=F 收尾。域 A 是核心循环改动，单独一波做完再动 B（B2 读决策席面依赖 A4 的席位语义稳定，标接口常数依赖）。
- 三级验收：孙任务 exit 0（正锚+负锚+消费者锚）→ 子域 Scenario 逐条映射测试 → 变更级 DoD（两症状锚绿+回滚即红+全量门禁）。

## D8 合并规则

- 01 域内 A2/A3 同触 loop 装配面→合单；A1 红锚先行独立单（必须第一天红）。
- 02 域单波单代理。
- 共享面（`assembly/host.go` 的 Invoke 路径）归 A4 单写；B2 不碰 host。

## D9 回退

每域改动独立可回退：A2/A3 以 `ReadyFeed` nil=现行为为回退开关（无配置面，代码级并存）；B2 以活跃闸参数零值=现行为。时钟 seam（P1-5）：新增 **`Driver.PassAt(ctx, now) TickOutcome`** 同步直调入口（经过闸门全路径，锚与宿主观察面共用）；B1 锚用它，禁真实 sleep、禁绕过闸门直调 Reflection。回退后与基线零 diff（F1 逐条命令见 tasks）。
