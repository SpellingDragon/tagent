# 架构减法收紧 · 独立拆解报告（核实表 + 尖锐问题清单）

> 角色：独立问题拆解者。只读代码 + 本报告，不改代码、不答问题。
> 基线：`8afa6cf+`（工作区 HEAD `5f78bdf`，fidelity-and-recovery 实施在途）。
> 材料：①架构评估（四项保留/四个结构不足/四条不变量）②减法收紧（六项删并 + 删呈现决策 + 三批顺序 + 验收三问）③现有变更 fidelity-and-recovery（proposal/design/tasks/findings）。
> 核实方法：逐项读源码定位现状证据，对照 fidelity-and-recovery tasks/findings 判覆盖，再出决策点问题。

---

## 一、核实表（六项删/并 + 附加呈现决策删除）

### (a) commitFact 与 commitFactChecked 双入口 → 单入口返回错误

| 项 | 内容 |
|---|---|
| 现状证据 | `facts.go:57-63` `commitFact(evt)` 只是 `commitFactChecked(context.Background(), evt)` 的包装，唯一附加行为是失败时 `assemblyWarn`；`commitFactChecked`（`facts.go:77-105`）是真正语义体（键幂等/冲突 + persistFact + 映射登记）。调用点分裂：无 ctx 的 `commitFact` 仅 `age_fold.go:217`、`tools.go:111`；带 ctx 的 `commitFactChecked` 在 `run.go:980/1020`、`facts.go:565`、`invoke.go:59`。 |
| 覆盖判定 | **部分覆盖**：2.2 已完成"返回 error 不再吞错"（G3 红转绿，findings 已记）；但**双入口本体仍在**，(a) 的残余面 = ctx-less wrapper + assemblyWarn 观测归属。 |
| 删并依赖前提 | ①两个无 ctx 调用点能拿到 ctx（age_fold 折叠回调、tools.go 事实提交处）或明确改 `context.Background()` 语义；②`assemblyWarn` 的失败观测由谁接管——loop 侧已有 `consumerWarn` 分类（findings 2.2 记录的 commitSettled/commitProgressFact 路径），需避免双报或漏报。 |
| 最大风险 | 低。真正风险是**观测语义漂移**：wrapper 的 warn 是"commit 失败必有日志"的兜底，合并后若调用点各报各的，失败可观测性变成调用方自觉——与材料一不变量3（责任须有完成证据）擦边。 |

### (b) history 与 replays 双历史路径 + max_tokens 决定走哪条 → 统一恢复/读取路径

| 项 | 内容 |
|---|---|
| 现状证据 | 双路径实体：`r.history map[event.Address][]model.Message`（`facts.go:664-684` SessionHistory/CommitHistory，全地址键，纯内存）与 `r.replays map[event.SessionID]replayEntry`（`request.go:227-240`，**仅 SessionID 键**，挂 `replay.ReplayProjection`）。分叉开关是预算：`attachDeclaredReplay`（`facts.go:289-320`）`window.MaxTokens <= 0` 即不注册映射序列；`seedReplay`（`run.go:1240`）`MaxTokens<=0` 的种子直接 `continue` 丢弃；`CommitHistory`（`facts.go:680-683`）发现 session 已映射就不写 transcript。G7 症状即由此产生：max_tokens=0 → 事实落盘但重装配无旧历史。 |
| 覆盖判定 | **未覆盖（仅挂账）**：fidelity 4.1 G7 只写"历史恢复与预算解耦（max_tokens=0 仍恢复旧历史）"，是修症状；(b) 要删一条权威路径，是删结构。 |
| 删并依赖前提 | ①(c) 完整事件成为恢复源（否则统一路径读回的还是轻字段反造物）；②地址键语义先定（replays 单键 SessionID vs history 全地址，正是 2.1 G4 未完成面）；③"预算只影响呈现"要求 AssembleBudgetPrefix 成为唯一预算消费点（现状 `window.MaxTokens` 同时是注册门、fold 阈值、预算上限三重身份，`age_fold.go:40-47`）。 |
| 最大风险 | **高**。统一后"恢复"与"呈现"分离，但现状 MaxTokens 兼任"这条链有没有恢复资格"的门——删掉门后，历史恢复对无窗口声明的 session 也成立，会改变既有测试冻结的"无窗口=无注册"语义（`request.go:283` AttachReplay 明文拒绝 MaxTokens<=0）。 |

### (c) 轻量 EventBody 反造完整事件 → 完整事件为恢复源，轻量只做索引

| 项 | 内容 |
|---|---|
| 现状证据 | `memory/event_body.go:32-78`：EventBody 双份字段并存——轻字段（Key/Type/Summary/Content/Timestamp）与全字段（Meta/Message/九 ID/Source/Target）。反造点在 `Event()`（`:235-267`）：Message 空且 Content 非空→`model.Message{Content}` 兜底；Summary 回填 meta。另 `DecodeEventBody`（`:145-175`）legacy 路径把 `event_key/event_type/event_summary` 译回。`canonical()`（`:303-314`）经 `Event()→BodyFromEvent` 往返算幂等判据，反造逻辑是幂等判定的一环。 |
| 覆盖判定 | **大部分已覆盖**：2.2 已做全字段编解码 + `Event()` 重建 + SameFact（findings 2.2）。(c) 的残余面 = Summary/Content 与 Meta/Message 的**重复权威**（写入时 `BodyFromEvent` 把两者各存一份，`event_body.go:110-111`）及 Event() 的兜底合成。 |
| 删并依赖前提 | ①legacy 数据的读路径必须保留某种反造（盘上旧轻 body 无法变出未存字段）——"轻量只做索引"对 legacy 不成立，除非判 legacy 为兼容例外；②索引字段（`Record()` `:208-218` 供查询/向量用 Summary/Content）改为从 Meta/Message 派生，写入侧派生点唯一化；③SameFact/canonical 的比较面随之改形，须重审排除清单（见 Q17 新发现）。 |
| 最大风险 | **中高**。删重复字段=改持久格式，与 findings 2.2 已声明的兼容性边界（legacy 轻 body 与全字段同键重发=冲突）直接互锁：先删重复权威会让"同键重发"的判定结果变化，可能把既有幂等测试判红。 |

### (d) SettleDriver 的 retained/firing/rearm → mailbox 统一待消费责任

| 项 | 内容 |
|---|---|
| 现状证据 | `settle_turn.go:92-99`：`firing bool` + `retained []settleNotice` 纯内存；`:228-239` rearm 出队/清空；`:250-264` Stop 清 retained 并 cancel 在飞回合。另一侧 mailbox 体系：`runtime/state/mailbox.go`（AcceptNotice/Claim/Consume/TrackDelivery，`MemoryStore` + durable 面 `consume:` 回执 `runtime/state/durable.go:52`），`facts.go:594-644` bookNotice/sink 是"通知待消费责任"的正统登记路径。 |
| 覆盖判定 | **方向冲突**：fidelity 3.2 G6b 计划是"retained 队列**持久待办**+回调重试"（加状态）；材料二 (d) 是删 retained 并入 mailbox（减状态）。两案并存会双写。 |
| 删并依赖前提 | ①语义等价性证明：mailbox 条目=通知在站点待消费（AcceptSeq/Claim/Consume 三态），retained 条目=**欠一轮 follow-up turn**（含目标用户地址+触发事实），两者消费动作不同（handover vs 起回合），需定义 mailbox 之上的"turn 义务"承载物；②retained 保存的是完整 `event.Event`（settle_turn.go:64-69 注释明言"保留事实而非重建"），mailbox 只存 `task.Notice`——合并后事实从哪读（lookup 已有，`settle_turn.go:79`）；③持久面：MemoryStore 的 durable 面目前只覆盖 task/consume 族，settle 义务入 durable 需新回执族。 |
| 最大风险 | **高**。mailbox 有 Claim/Consume/Reclaim 的并发与重启语义（`mailbox.go:184-249`），把"起回合"责任挂进去会把回合生命周期（firing 单飞串行，settle_turn.go:200-207）与通知消费生命周期耦合——双跑/重复 fire 的风险回来了（材料一保留语义"通知恰好消费一次"的反面）。 |

### (e) Run.snapshots 与 Capture 重复快照 + 空 Seal 适配 → 采集一次 + 创建者封存

| 项 | 内容 |
|---|---|
| 现状证据 | 同一请求两份快照：`Run.snapshots []RequestSnapshotRecord`（`run.go:200`，`request.go:45-58` snapshotRequest 写入，`rl.SnapshotRequest` 取安全副本）与 `Capture.Snapshot`（`rl/capture.go:28-45`，`capture_wiring.go:285-311` CaptureAdapter→scopedCapture→owner.keep 也留一份）。调用链上两者都挂在请求边界（`run.go:332` SetRequestSnapshotFace + mountCapture）。空 Seal 适配是**双层**：`hostRunCapture.Seal` 返回空 manifest 不关账（`host_capture.go:98-101`）、`hostScopedCapture.Seal` 同样透传空账；真正封存在宿主关闭（`sealHostCapture` `host_capture.go:54-63`）。loop 侧每 drive 封一次（`runtime/agent/loop.go:765-773` sealCapture），`CaptureAdapter.Seal`（capture_wiring.go:325-329）带超时等待。 |
| 覆盖判定 | **未覆盖**：fidelity 无此项。findings W0 断点 3 旁证"请求快照仅内存、RL capture 可选且无读回面"。 |
| 删并依赖前提 | ①明确快照的唯一所有者：capture 在场时归 capture，**capture 不在场时归谁**（`loop.go:737-739` capture==nil 直接 return，什么都不记）；②"创建者封存"要求把 Seal 从执行环接口拿掉或改为 no-op 契约，涉及 `rl.Capture` 接口（capture.go:43）、`runtime/agent/loop.go:171` 窄面、`examples/contract/rl/Capture_example.go` 契约示例、`rl/skeleton_assert.go:27` 冻结断言——破坏面横跨三包；③`Run.RequestSnapshots` 有生产消费者（`host_face.go:306` 状态面计数）与测试消费者（tests/integration/learning/formal_request_snapshot_test.go），删哪份要迁移读方。 |
| 最大风险 | **中**。空 Seal 适配是"每回合封账 vs 全程一账"两套生命周期的补丁（host_capture.go:6-13 注释自认）；直接删适配而不给 loop 换掉 Seal 职责，会把 run-wide sink 每回合封死（CaptureSink.Seal 第二次返回首账，rl/sink.go:345-356）——轨迹数据静默截断。 |

### (f) 插件注册 Phase/Material/Product/Port 多层 → 窄接口或单一端口枚举

| 项 | 内容 |
|---|---|
| 现状证据 | `api/plugin/material.go`：Material(3) × Product(6) → Port 对（`:58-71`），`RouteOf` 闭表再映射 Phase(2)（`:86-109`，`phases.go:40-49`），注释自认"the phase and the narrow interface are both readings of it"（material.go:60）。窄接口族：PostCommitObserver/Presentation/TransformCall/Governor/CompressTrigger/RiskAssessor/EnvironmentInject/FinalPresenter 等（api/plugin + examples/contract/api_plugin 11 个示例）。装配校验 `NewFlow`（material.go:135-144）+ 细粒度红线（Instance 3 条、Port 2 条）。 |
| 覆盖判定 | **未覆盖**：fidelity 无此项。 |
| 删并依赖前提 | ①定删哪层：Phase 是纯派生（RouteOf 表）可删，Port 两字段枚举化=6 个具名常量（现 switch 已是闭表，改形收益主要是词汇量）；②"单一端口枚举"与窄接口二选一：窄接口是编译期约束（`phases.go:249` 类型断言分发），单枚举=运行时分发，材料二自己列了两个方向——先裁方向才能动手；③门控/日志键位：`observePhase`（phases.go:322-383、443-452）以 Phase+Port 为维度，telemetry 依赖这些键。 |
| 最大风险 | **中**。词汇精简是纯机械改形但触面最宽（api/plugin 是共享词汇面，settle_turn/age_fold/lease_observer/telemetry 全是其实例），与材料一①（PostCommitObserver 混纯观察与关键义务）在同一面上动刀——两件事不排序会互踩。 |

### (g) 删长期保留的呈现决策（Full/Ticket/Summary）→ 过去看请求快照、当前由事实+配置+预算计算

| 项 | 内容 |
|---|---|
| 现状证据 | `memory/replay/replay_budget.go`：`assembleBudgetLocked`（`:153-224`）每次按预算落 Full/Ticket/Summary 决策并 `recordLocked`（`:266-286`）**first-time-wins 永久记录**，且原地改写 `p.entries[i].Content` 为呈现文（`:278-283`），原文另存 `p.originals`；`viewOf`（`:288-300`）之后一切读取走记录的决策。决策还被铸成 replay-settle 事实（replay_settle.go），但 `Settles()` **无生产消费者**（全仓 grep 仅测试/骨架）。 |
| 覆盖判定 | **方向冲突**：fidelity 4.3 G9 计划是"呈现决定**条件重用**+完整请求硬约束"（修复重用）；材料 (g) 是删掉长期决策本身（删除重用的载体）。G9 直译测试（"200 字节内容在预算缩至 4 字节后仍完整保留"）在两案下的红转绿语义不同。 |
| 删并依赖前提 | ①"过去看到什么"的事实源必须先存在且可读：请求快照现状仅内存（`request.go:62-77`，findings 断点 3），无持久读回面——(g) 的一半前提押在 (e) 的快照归属上；②ProjectionEntry.Content 双角色拆分（见 Q18 新发现）；③ReplaySettle/`Settles()`/`RecordDecision` 的对外面（replay_budget.go:260、385-405）删除涉及 memory/replay 冻结面。 |
| 最大风险 | **高（验收面冲突）**。删除决策后"同一事实在不同请求里呈现不同"成为合法行为，G9 的症状直译测试断言要重写——验收面后改即触 R29（追认缩水）红线，必须先裁归属。 |

### 与 fidelity-and-recovery 重叠归属一览

| 材料二项 | fidelity 对应叶 | 状态 | 关系 |
|---|---|---|---|
| (a) | 2.2 G2+G3 | ✅ 完成 | 部分覆盖（error 返回已做，双入口残余） |
| (b) | 4.1 G7 + 2.1 G4 | 未做 / 未做 | G7 是症状修复，(b) 是结构删除；G4 地址键是 (b) 前置 |
| (c) | 2.2（EventBody 全字段）+ 1.0 ③ | ✅ / 停车待裁 | 核心已覆盖；残余=重复权威字段 + assistant 调用事实缺位 |
| (d) | 3.2 G6b | 未做 | **方向冲突**（持久化 retained vs 删 retained） |
| (e) | 无（findings 断点 3 旁证） | — | 纯新增 |
| (f) | 无 | — | 纯新增 |
| (g) | 4.3 G9 | 未做 | **方向冲突**（条件重用 vs 删决策） |

---

## 二、尖锐问题清单（18 条，供逐条回答）

> 每条格式：类别 | 问题 | 代码证据 | 为什么这是决策点

### Q1 · 删并边界（a）
**问题**：commitFact/commitFactChecked 合并后，"commit 失败必有一条具名观测"归谁——调用点各自的 consumerWarn，还是单入口保留 assemblyWarn？若归调用点，`age_fold.go:217`（折叠产物提交）与 `tools.go:111`（工具结果提交）这两条无返回通道的路径失败时，责任记在谁的账上？
**证据**：`facts.go:57-63`（wrapper 唯一附加行为=assemblyWarn）vs `facts.go:77-105`；findings 2.2 已把无返回通道路径定为 `consumerWarn`——两套 warn 规则并存。
**决策点**：这决定"单入口"是纯机械合并还是顺带重新划分失败观测责任（材料一不变量3"完成证据"的落点）；选错=失败可观测性静默丢失，恰是 G3 类缺陷的复发形态。

### Q2 · 删并边界（b）
**问题**：统一历史路径后，"无窗口声明（max_tokens<=0）的 session"是恢复旧历史还是永久不恢复？现状三处 `MaxTokens<=0` 各有一票否决（注册/种子/折叠），删掉分叉后 G7 的验收语义（"max_tokens=0 仍恢复旧历史"）是终点态还是过渡态？
**证据**：`facts.go:310`、`run.go:1240`、`run.go:1264`、`request.go:283`（AttachReplay 明文拒绝 MaxTokens<=0）。
**决策点**：预算"只影响呈现"意味着恢复资格与预算彻底解耦，但现状的"无窗口=无映射序列"被 `age_fold.go:40-47`（fold 需要窗口测体积）依赖——fold 没有窗口怎么折？这是删并的真边界，不裁则 (b) 只能做成"又一条第三路径"。

### Q3 · 顺序依赖（b ← 2.1 G4）
**问题**：统一历史读取要求先统一地址键——replays 是 `map[event.SessionID]`（`facts.go:681`、`request.go:287`），history 是 `map[event.Address]`。2.1 G4（AgentID+SessionID 完整地址）在 fidelity 尚未做且与 1.0 同文件串行。材料二批次 1 是否把 2.1 认作前置？还是 (b) 自己顺带把键改了（与 2.1 合单）？
**证据**：`facts.go:680-683`（`r.replays[addr.SessionID]` 判映射跳写 transcript）；tasks.md 2.1 未勾且标注与 1.0 串行。
**决策点**：两个变更改同一个键语义而无归属，必然一方重做或互相判红；这也是"改现有计划 vs 另立"的第一个分叉。

### Q4 · 删并边界（c）
**问题**：盘上已有的 legacy 轻 body（`event_key/event_type/event_summary` 三字段）永久保留反造读，还是给定期限迁移后删除 `DecodeEventBody` legacy 分支？"轻量只做索引"对 legacy 是否是具名例外？
**证据**：`memory/event_body.go:145-175`（legacy 分支）+ findings 2.2 兼容性边界（legacy 与全字段同键重发=冲突）。
**决策点**：例外是否具名决定"完整事件为恢复源"是不是完整约束（材料一②正题）；不定则每处恢复代码都要永远带两套读路径，(c) 的减法只减一半。

### Q5 · 归属（c / 1.0 ③）
**问题**："完整事件为恢复源"的前提是 assistant 工具调用事实被提交入链——findings W0 断点 3 已证实它**从未提交**（`commitOutput` 只在回合终点提交终局输出，真参数仅存于内存请求快照），1.0 ③ 因此结构性停车待裁（三选项 a/b/c）。这块是 (c) 批次 1 的前置工作项，还是 fidelity 1.0 的裁决遗留？谁改 `runtime/agent/loop.go`（两案白名单外）？
**证据**：findings.md 断点 3/4；`request.go:45-58`（快照仅内存）；tasks.md 1.0 未勾。
**决策点**：冷启动协议合法（不变量2）在调用事实入链前**结构上不可证**——(c) 若不含它，批次 1 的验收三问里"原有恢复验收仍成立"对 ③ 是空话。

### Q6 · 删并边界（d）
**问题**：retained 条目=欠一轮 follow-up turn（带用户地址+完整事实），mailbox 条目=通知在站点待消费（AcceptSeq/Claim/Consume）。"mailbox 统一待消费责任"是把 turn 义务降解为通知再消费一次，还是 mailbox 之上新增义务条目类型？后者不就是把 retained 换个家？
**证据**：`settle_turn.go:64-69`（retained 保留完整 event.Event）vs `runtime/state/mailbox.go:67-249`（Notice+AcceptSeq 三态）。
**决策点**：若只是搬家，(d) 的"删掉哪份状态"答不出（mailbox 与 retained 是两种责任，合并需先证明一种）；若真降解，"通知恰好消费一次"（材料一保留语义）与"回合恰好起一次"如何在同一账上各自可判？

### Q7 · 不变量判定（d）
**问题**：不变量3"每项任务交付责任有完成证据或可恢复未结状态"如何对上 `Stop()` 清空 retained（欠轮义务无证据、不可恢复、直接丢）？这是现存违例要顺带修，还是 Stop=关停豁免？豁免的话"实际停止证据"（材料一保留语义）在哪？
**证据**：`settle_turn.go:228-239`（rearm 停机清队）、`:259-260`（`d.retained = nil`）。
**决策点**：不变量若不能对存量违例给出判定（红/绿/豁免），它就不是不变量而是愿景；(d) 合并后这个丢弃点会藏进 mailbox 的哪个操作里更难看见。

### Q8 · 归属（d vs 3.2 G6b）
**问题**：tasks.md 3.2 G6b = "retained 队列持久待办+回调重试"（加持久状态），材料二 (d) = 删 retained（减状态）。两案方向相反：是改 3.2 的任务行（改验收面=tasks/spec 修订，谁批准），还是 (d) 降格为 3.2 之后的后继变更？
**证据**：tasks.md W2 3.2（未勾，前置 2.2 已解锁）；settle_turn.go retained 字段。
**决策点**：3.2 已在就绪队列上（前置已完）随时会被派发实施——不先裁归属，实施者会把 retained 持久化写出来等着被 (d) 删掉，两笔工都进 main。

### Q9 · 删并边界（e）
**问题**："采集一次+创建者封存"要删的空 Seal 适配是双层的（`hostRunCapture.Seal` 与 `hostScopedCapture.Seal` 都回空账），但 loop 的每 drive Seal（loop.go:765-773）是 `rl.Capture` 接口义务（capture.go:43 + loop 窄面 loop.go:171 + 契约示例 + skeleton 冻结）。封存责任上移是改接口（三包破坏面）还是保留接口由创建者实现真封？另：capture==nil 时 `fixCapture` 直接 return（loop.go:737-739），快照唯一来源被 (e) 删掉后**无 capture 的装配永远没有请求快照**——(g) 的"过去看请求快照"在此场景看什么？
**证据**：`host_capture.go:98-101/105-130`；`rl/capture.go:28-45`；`runtime/agent/loop.go:735-773`；`run.go:332`+`request.go:45-58`。
**决策点**：删重复快照的判据是"同一请求两个 keep 点"，但两份的消费者不同（r.snapshots 供 host_face 状态面与 G1 呈现层回收，capture 供轨迹）——合并必须先答消费者迁移，否则是把 G1 修复（restoreCallPairing 依赖 r.facts/r.snapshots）拆掉。

### Q10 · 删并边界（f）
**问题**：删 Phase 后 `observePhase` 的门控/日志键位（telemetry 依赖）以什么为维度？而"单一端口枚举"与"窄接口"是材料二自己并列的两个方向——选单枚举会把 `phases.go:249` 的编译期类型断言分发换成运行时 switch，这与"窄接口"方向相反。先裁哪一个？
**证据**：`api/plugin/material.go:86-109`（RouteOf 闭表）、`phases.go:40-49/322-383/443-452`、`phases.go:249`（`element.impl.(PostCommitObserver)` 分发）。
**决策点**：两个方向的删除面不同（Phase 可零风险删；Port 枚举化牵动 Instance 红线与 11 个契约示例），不裁则 (f) 无法定义"删掉哪份状态"（验收三问第一问直接落空）。

### Q11 · 顺序依赖（批次2/3 vs 材料一①）
**问题**：材料一①（PostCommitObserver 混纯观察与关键义务：`present.go:28-48` 观察失败只记 Gap，而 SettleDriver 的失败=欠用户一轮）是改插件词汇面（责任分层进接口）——它排在批次 2（通知消费）还是批次 3（插件词汇）？(d) 合并 retained 进 mailbox 若先做，义务责任分层的接口形就先被 mailbox 决定了。
**证据**：`api/plugin/present.go:28-48`（After 无错误返回，失败=Gaps）；`settle_turn.go:103-105`（SettleDriver 即 PostCommitObserver）；`telemetry/commit_observer.go`（纯观察同接口）。
**决策点**：这是三批顺序表唯一的跨批次耦合点（材料二没写）；次序定错，批次 3 的"窄接口"会把批次 2 刚定的责任边界再翻一遍。

### Q12 · 归属（g vs 4.3 G9）
**问题**：G9 直译测试（"200 字节内容在预算缩至 4 字节后仍完整保留"，红着）在 (g) 删除长期呈现决策后断言什么？"条件重用"（4.3）与"删决策"（(g)）是同一批先后两个动作，还是二选一？若二选一，fidelity 4.3 的任务行与 G9 症状锚怎么改（改验收面=R29 红线，须停并上报）？
**证据**：tasks.md 4.3（未勾）；`replay_budget.go:266-286`（first-time-wins + entries 原地改写）；proposal G9 行。
**决策点**：症状直译锚是验收矩阵第一行，其语义被另一个变更改写而不申报=完成度五查的谎报面；这是两个变更合并前必须回答的归属问题。

### Q13 · 顺序依赖（g）
**问题**：(g) 把"过去看到什么"押在请求快照记录上，但请求快照现状仅内存、无读回面（findings 断点 3），其归属又在 (e) 的裁决里（Q9）。(g) 是否显式以 (e) 完成为前置？若 (e) 裁出"capture 可不在场"，(g) 的过去视图在无 capture 装配下由什么承载？
**证据**：`request.go:62-77`（RequestSnapshots 仅内存拷贝）；`replay_budget.go:278-283`（决策记录是唯一的"过去呈现"载体，删后即失）。
**决策点**：删状态的前提是替代读源存在；(g) 现在的依赖图里 (e) 不在三批顺序的同一支上，实际是隐藏前置（R1 形态的跨批启动门缺失）。

### Q14 · 不变量判定（不变量1/2）
**问题**："重启前后事实同语义"与"压缩恢复后请求协议合法"的判定锚各自是哪条具名测试？现状 TestFactFidelity 覆盖重装配全字段（不变量1 的正面），但 SameFact 的"同语义"含一份手工排除清单（Timestamp/InvocationID/InstanceID/Format/Retention，event_body.go:297-314）——清单本身在不变量管辖内吗？不变量2 侧，冷启动 ③ 是具名结构性 Skip（`TAGENT_FIDELITY_COLD_BOOT=1` 红证在），"压缩恢复"绿 ≠ "冷启动恢复"成立，不变量2 的字面覆盖哪条？
**证据**：findings 2.2 键语义节 + findings W0 ③ 停车节；`event_body.go:303-314`。
**决策点**：不变量若不落到"哪条测试红了就是违例"，(c)/(b) 删并批无法用它做回归判据（验收三问第三问没有判据源）。

### Q15 · 不变量判定（不变量4）
**问题**："每份可变状态有唯一所有者"的判定标准是字段归属（在哪个 struct 上）还是读写点唯一？现状六份内存态散落：`Run.facts/factOrder`、`Run.history`、`Run.replays`、`Run.snapshots`、`Run.presented`、`Host.pendingDeliveries`（host.go:184-187/1163-1183）、`SettleDriver.retained`——其中 `restoreCallPairing` 跨读 r.facts+r.snapshots 修投影（request.go:641+），呈现层对快照是**跨所有者读**。
**证据**：run.go:195-206 字段簇；host.go:184-187；settle_turn.go:92-99；request.go restoreCallPairing 注释。
**决策点**：六份里 (d)(e)(g) 各删一份，但跨所有者读的存在说明现状就没有唯一所有者——不变量4 若按"字段归属"判则现在就绿，按"读写点"判则 (d)(e)(g) 只是开始；两种判定给出完全不同的删并完成线。

### Q16 · 验收（三问机械化）
**问题**："删掉哪份状态 / 剩余责任归谁 / 原有失败与恢复验收仍成立"三问能否进机械门？第一问可 skeleton 断言（字段/接口删除）；第二问"责任归谁"如何机器判（具名责任人类型 + 完成证据锚？）；第三问与**有意改语义**的测试（G9 条件重用、无窗口恢复资格）冲突时，改测试的批准权在谁——编排者、用户、还是 plan 修订流程？另："预算只影响呈现"的机械判定在 noticeCard 路径下不可判（feedback 卡片无上限检查照常塞入，replay_budget.go:213-224）——预算硬约束 vs 永不丢弃谁赢，是预算语义的未定义角。
**证据**：replay_budget.go:213-224（`if entry.Type != event.TypeFeedback` 分支无 `<=limit` 检查）；tasks.md D5（孙任务三锚）。
**决策点**：三问若只能人判，每批收尾都要人工评审卡节奏（R31 断点滥用）；第三问的批准权不裁，会出现"改断言=掩盖缺陷"（findings W0 明令）与"合法语义变更"两种改测试混在一起。

### Q17 · 新发现（SameFact 戳记边界无机制定义）
**问题**：幂等判定"哪些字段是事实、哪些是提交戳记"靠 `canonical()` 手工剔除 5 个字段（Timestamp/InvocationID/InstanceID/Format/Retention，event_body.go:303-313），但 Source/Target/TaskID 等仍在比较面内——而**提交路径会重填它们**：InjectEvent 对零值 Source/Target 回填 Owner（run.go:968-973），CommitToSession 每次 mint `#delivery-` 新 InvocationID 并覆写 Owner/Source/Target（run.go:1005-1015）。同一事实重试两次、一次调用方带 Source 一次不带 → 判为冲突（ErrFactKeyConflict）而非幂等。材料二 (a)(c) 都在动这条提交链，谁来给"戳记 vs 事实"定机制性边界（例如提交路径禁止覆写调用方给定字段 / 排除面由提交协议生成而非手工列举）？
**证据**：event_body.go:297-314 + run.go:968-973、1005-1015 + findings 2.2 "SameFact 排除戳记字段"（排除清单=人工裁定）。
**为什么是决策点**：幂等身份是材料二"保留语义：幂等身份+完整会话地址"的核心，但其判定面含可被调用路径改写的字段——(a) 单入口化会把回填逻辑集中到一处，是定边界的机会窗口；错过则幂等语义继续靠手工排除清单维持，每个新字段都要人工追加裁定（G2 类"同键双状态"缺陷的复发土壤）。材料一/二均未点名此边界。

### Q18 · 新发现（ProjectionEntry.Content 双角色 + 呈现层三处手工转换的根）
**问题**：`ProjectionEntry.Content` 一个字段兼任两种身份——"事实文本"（mappedEntryOf 产出时）与"当前呈现文本"（`recordLocked` 原地改写 `p.entries[i].Content`，replay_budget.go:278-283），原文另存 `p.originals` map，读回经 `originalOf`/`viewOf` 分流。材料一②说"EventBody/投影/模型消息多处手工转换"，真正根因是这个字段双身份 + EventBody 的 Summary/Content 双份（Q4）+ 呈现层 restoreCallPairing 第三处修复（request.go:641+）——三处转换都是在补"字段携带的信息不完整"。(c) 与 (g) 都会动这一处：谁拥有 ProjectionEntry 的形态（memory/replay 包外不可改写？），删除原地改写后"当前呈现"存哪（无状态计算=每次 assemble 重算，原值必须在）？
**证据**：replay_budget.go:266-300；`memory/event_body.go:110-111`（Summary/Content 各存一份）；findings W0 投影字段链四断点。
**为什么是决策点**：这是材料一四个结构不足里②的**具体根**，而材料二的 (c)(g) 各删一半（(c) 删 EventBody 重复、(g) 删决策记录）却没人认领字段双身份的拆解——不拆则删并后两处修复逻辑（Event() 兜底、restoreCallPairing）仍需长期存在，"事实唯一真源"依旧不是实现约束。材料一/二均未点名。

---

## 三、覆盖核对（对用户任务书的六项覆盖）

| 任务书要求 | 落点 |
|---|---|
| 六项删并边界 | Q1(a) Q2(b) Q4/Q5(c) Q6(d) Q9(e) Q10(f) |
| 四条不变量可判定性 | Q7(不变量3) Q14(不变量1/2) Q15(不变量4) |
| 三批顺序依赖 | Q3(批1←2.1) Q11(批2/3↔材料一①) Q13(批内隐藏前置) |
| 与 fidelity-and-recovery 归属 | Q3 Q5 Q8 Q12（＋一节重叠表） |
| 每批验收三问可机械化 | Q16 |
| 新发现（≥2 条） | Q17（SameFact 戳记边界）Q18（ProjectionEntry.Content 双角色）；附带发现：noticeCard 预算无下限（Q16 附注）、Settles() 无生产消费者（核实表 (g)） |
