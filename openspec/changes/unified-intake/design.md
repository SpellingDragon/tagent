# 设计决策（D1–D9）

## D2 子域表

| # | 子域 | spec 能力 | 一句话边界 | 外部依赖 |
|---|---|---|---|---|
| 01 | decision-boundary | `persistent-event-loop`（ADDED） | 在途 turn 的每个 decision 边界重新聚合链上就绪事件（含用户/外部输入）；host Invoke 受理语义与 gate 降级 | `persistent-event-loop` 现条款（:94/:105/:123 保留不动） |
| 02 | meditation-activity | `meditation-idle-gating`（MODIFIED） | 节奏门加用户事件新鲜度闸与决策席在途闸；自管回合不重置 | `event-metadata-contract` 的 trigger_source 读径（相同，不新建） |

## D1 决策边界消费＝既有 ready 面的时序参数化（零新概念）

**不新增任何面/类型/哨兵。全部复用：** `Invocation.Ready`（注释本就自称「aggregated for the next decision」）、`observeReady`/`aggregateReady`（既有）、`SessionFacts`（既有读面）、`intake` 的 transcript 累积（既有）、`projection.endedAt` 界标（既有）。

- **落链归属**：在途到达的外部输入走**既有 `Run.CommitToSession`**（它本就是「只落链、不驱动」的既有入口），事实 `InvocationID` 记为**在途 range 的 invocation**（Host 在既有地址门内记录在途 inv——地址门既有对象的字段扩展）；**它随带的 After 段由随后的 `observeReady` 承担，在途落链不再多跑一次 After**（防重复观测）。同 invocation 的事实不触发 `admitsRange` 的「新 range 首条输入」界标前移——界标不动的保证来自既有判据本身。
- **消费**：`Drive` 每轮 `decide` 前重跑既有 `observeReady`+`aggregateReady`；读面走**既有 `HistoryFace` 接口增加的方法**（第 3 方法，签名与生产实现 `*Run.SessionFacts` 逐字一致——揭示既有能力，非新概念；`check_concept_budget` 名义上限 3 方法正好到顶）。输入=`SessionFacts(addr)` 中 `InvocationID==本 range` 且 `admitsRange` 放行的事实；**去重=本 range 已消费键集合**（即「哪些事实已进 transcript」的键形表达——既有 transcript 语义的最小必要形态，非新账本）；`skeleton_assert.go` 同步（D2 白名单已含）。
- **退出判据扩展（唯一改动点）**：既有退出条件 `len(receipts)==0 && len(observations)==0` 增加一项——`aggregateReady` 增量非空则继续。**位置在既有 commit-transcript/commit-output 之前**（与 grill P2-2 修正一致：并入退出判定，不在 return 前另插 re-check）。
- 红线条目照旧：progress notice 不进任何 decision 输入（既有红线，不因本变更松动）。

## D2 host Invoke 受理语义（零新概念版）

- **在途受理**：`Host.Invoke` 在既有地址门内发现已有在途 turn 时——在途落链走既有 `Run.CommitToSession` 的内部落链段（**不可走 `Run.Invoke`：它会起第二个 Drive**；`CommitToSession` 公面签名不变，内部拆出落链段供在途路径复用、其 After 段仍归公面路径），事实 `InvocationID` 记为在途 range 的 invocation，返回 **既有形态** `Turn{Terminal: model.KindUnknown}` 且 `err=nil`。**不新增哨兵**：受理语义=`Turn` 自身注释的「or KindUnknown while no decision reached one」+ `err=nil`+0 决策；**不得引用 `model.Kind` 层面的「状态不可判定」**（其契约是 MUST 报警，两层不同域，规格 delta 写明「无 err 且 0 决策=已受理」防下游误报警）。
- **无在途**：保持现状——拿门、起 turn、阻塞返回完整 Turn。
- **末决策窗口**：由 D1 的退出判据扩展自然覆盖（消息在末决策期间落链→退出判定看到增量→继续而非退出）；门释放前若判据新增了增量（微窗口），持有方重跑一次同一判据——**同一判据重跑，不是新机制**。
- **Host 组合缝**：无在途走既有 `Run.Invoke`（落链+驱动一体）；在途走既有 `Run.CommitToSession`（只落链，不驱动、不重复 After）。**不抽新函数、不改任何公共面**。
- follow-up 路径（fireNoticeTurn/driveSeat）**一字不动**。

## D3 gate 语义（域 A，零新概念）

`addressGate` 既有的「同 session 单槽」语义**不改**；变化只在持有者行为：在途期间到达的输入**落链后即返受理**（D2），不再由调用方在门外排队。`state.Occupancy`/`ErrSlotHeld`/`ErrShutdownRefused` 一字不动。**新增的只有门内一个既有对象的字段**（记录在途 InvocationID，供 D1 落链归属），非新概念。

## D4 外部输入规格条款（域 A delta）

`persistent-event-loop` ADDED Requirement「在途 turn 的外部输入在 decision 边界消费」：外部输入（`source==user`/渠道）SHALL 先成事实；在途 turn SHALL 在下一 decision 边界聚合并消费，SHALL NOT 排队至 turn 结束另起 turn，SHALL NOT 被执行中的 decision 打断消费。Scenario 两条（在途消费/无在途起 turn）。**不改** :94/:105/:123 的 task_settled 条款（两者同构，条款并列）。

## D5 冥想活跃闸＝既有 novelty 判据的时间参数化（零新概念）

**不新增读面/计时器/计数器。全部复用：** 既有 novelty 门的扫描结果（`ScannedFact{Timestamp, SelfManaged, Attribution}` 已有「非自管新鲜事实」的全部判据）、既有 `retained`/`rearm` 语义、既有 verdict 通道。

- **判据①（用户事件新鲜度）**：novelty 门已在扫描「Timestamp > lastMeditation 且非自管」的事实——**把「存在即 novel」的时间参数化**：该批事实中最近一条的 Timestamp 距 now 不足 `min_gap` → 本 tick 不 fire（冷却自最后非自管事实重置）。**限定（W2 如实）**：判据只看已盖章谱系（`hostFacingStamp` 既有词表）；未盖章输入既有判据本就归自管，不纳入——与既有实现一致，不新增归一化。
- **判据②（决策在途，落地形态）**：构建时注入既有 `*state.Occupancy` 与 `event.InstanceID`（既有 `NewDriver` 构造加两参，非新类型）；fire 前取 `ScanVerdict.Recent.Identity.Key` → 既有 `Reflection.CommittedFact` 读回事实 → 其 `Owner` 地址查既有 `Slot(Owner, instance)` → 占用不 fire。**收窄说明**：Occupancy 只在模型决策在飞时占用（工具执行不占席），本判据管决策窗口；慢工具轮不抑制（反思跑独立会话，不与工具争模型槽）。`Recent` 置于 novelty 检查之后取用（`Recent==nil ⇔ 无 novelty`，天然规避空面）。**不造 SlotQuery 面**：直接持有既有对象。
- **抑制语义（C3 裁定：零时钟源）**：抑制=水位不动+`pending` 保持为空+报告走既有 `SetObserver` 的 `TickOutcome`（`TickRhythm`+Reason 文字，不新增常量；deferred 回执只在注入后产生，抑制期不产生）；**重评=下一次 host-facing 提交触发既有 `ObserveCommit` 时重新评估**（既有机制，无 ticker、无新计时源）。**已知行为边界（如实入 gaps）**：会话安静下来且无新提交时，被抑制的冥想不自动恢复——「推迟」的上界是下一封消息；这是零新机制的诚实代价，规格 Scenario 以「下一次评估机会」表述。
- **不造任何新读面/新计时器/新计数器/新同步入口**；测试沿用既有测试设施的注入方式（既有 fake 时间设施或既有构造的可选参数）。

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

每域改动独立可回退：A2/A3 回退=还原退出判据与门内时序（无配置面，代码级可逆）；B2 以活跃闸参数零值=现行为（构造零值=既有全部语义）。**无新时钟 seam**：重评沿用既有 rearm/interval 节拍；测试沿用既有 `NewDriver` 构造与既有测试设施（禁真实 sleep）。回退后与基线零 diff（F1 逐条命令见 tasks）。
