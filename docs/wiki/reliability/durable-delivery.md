# 持久投递与消费账：先登记后销账

> **活契约页**：逐条对齐当前码面（`runtime/state/`、`runtime/task/`、`assembly/host_delivery.go`），代码内 `// 契约:` 注释指向本页锚点。口径统一于[存储概念地图](../../demos/storage-concept-map.html)：链管身份与因果、原文仓管字节、**账本管责任**、其余现算；可恢复状态的持久载体统一为[框架 checkpoint 事实族](../platform/platform-subsystems.md#framework-checkpoint)。历史快照见 [PRE-REWRITE-MANIFEST](../../archive/wiki/PRE-REWRITE-MANIFEST.md)。

本页承载两件事：**一条通知被谁认领、是否被消费**（通知消费账），以及**一条已生成的正文有没有送出去**（交付账本）。两者的失效表现都不是报错——而是重复驱动、重复投递或静默漏送。

<a id="envelope-states"></a>
## 一、两本账的状态机

### 1.1 通知消费账（`runtime/state/mailbox.go`）

一个席位（mailbox entry）只走三态：**未认领 → 已认领 → 已消费**。

| 动作 | 前置 | 结果与具名拒绝 |
|---|---|---|
| `AcceptNotice` | 目标地址完整、投递已登记 | 依 `noticeKey{task, round, kind, seq}` 幂等；同身份不同内容 → `ErrNoticeConflict`；**无投递登记 → `ErrDeliveryNotAllowed`**（fail-closed：没路由就不收） |
| `Claim` | 席位未认领 | 认领成功记下实例与调用帧；已被认领 → `ErrClaimConflict` |
| `Consume` | 席位已被本实例认领 | **先把消费回执落盘，再把席位标记为已消费**；重复消费 → `ErrClaimSettled`；未认领 → `ErrNotClaimed` |
| `Reclaim` / `Release` | 席位属于本实例 | 退回可认领 / 交还，供崩溃后重新驱动 |

两条承重细节：①`Consumed` 水位只推进**连续已消费前缀**，中间留一个空洞就不前进——否则一次跳号会把没消费的通知记成已消费；②`ErrClaimSettled` 是"恰好一次驱动"的唯一执行面：回执落地之后，同一通知再也不会被驱动第二次。

### 1.2 交付账本（`assembly/host_delivery.go`）

一个正文义务在链上声明：带正文的输出在**同一笔 `agent_output` 事实**里登记 owed（目标＋血统元数据），义务 ID 是 **`delivery-fresh-<factKey>`**——以承载该正文的事实的身份命名；此后每次状态迁移作为 `delivery.settlement` 族事实以「身份＋状态」为键**追加式**落链（`writeDeliveryState` 是唯一记录入口）。因此**登记、补交接、重启恢复读的是同一套身份**：同一事实只有一个义务，重启与实时面不会各自造一份。

码面是**四个结算终态常量**（`sent`/`refused`/`headless-retired`＋带 reason 的 `held`，reason ∈ `approval`/`retrying`/`retry-exhausted`/`unknown`），外加迁移专用的 `owed` 态——旧 KV 六态记录只在 `migrateRetiredDeliveries` 里一次性映射成结算事实后只读留存，不再是活账。

| 状态 | 含义 | 进入条件 | 出口 |
|---|---|---|---|
| `owed`（迁移专用） | 旧账留下的未发送义务 | 旧 KV 记录迁移 | 恢复时按仍欠处理：入口地址重投，非入口具名销账 |
| `held.<reason>` | 等待：审批挂起或重投退避 | 裁决挂起（approval）／通道拒收（retrying）／重试耗尽（retry-exhausted）／结果未知（unknown） | `ApproveDelivery`→`sent`；`RefuseDelivery`→`refused`；retrying 续跑重投 |
| `sent` | 已送达的回执 | 发送成功后落结算事实 | 终止态（**回执落链是唯一阻止重投的凭据**） |
| `refused` | 已放弃并留审计 | 显式拒绝 | 终止态 |
| `headless-retired` | 具名销账，未发送 | 恢复时发现义务目标是**非入口地址** | 终止态，**记录保留正文与目标供审计** |

入口门禁是硬判据：`recordDeliveryOwed` 在 `addr.AgentID != entry` 时**什么都不登记**；恢复读面遇到非入口的 owed 义务时调 `retireHeadlessDelivery` 具名销账而不是投递——非入口地址的答复归驱动它的调用方，永不进消息渠道。

### 1.3 顺序：先交接，后销账

| 时机 | 动作 | 为什么 |
|---|---|---|
| 生成后、发送前 | `recordDeliveryOwed`：owed 声明随输出事实同笔落链，返回**整套仍欠的正文**（本次登记的 + 早先登记未销的） | 崩溃点落在"已登记未发送"时，重启读得回这条义务 |
| 通知终态落地时 | `handoffSettledBodies` **在消费邮箱条目之前**跑：登记本批义务，并补投整套仍欠的正文 | 消费回执一旦落地，该通知永不再被唤醒；补投必须发生在销账之前 |
| 启动 | 框架 checkpoint 的 `delivery` 切片先恢复整本账（结算态＋开账清单＋重试 attempt），`restorePendingDeliveries` 再叠加偏移后事实：仍欠的入口义务按实时同一裁决发送，held 回填待裁表，retrying 从已有 attempt 续跑退避；不可解析或缺身份 → **具名拒启** | 不容许"带着看不懂的义务启动"；已确认 sent 的义务不重发 |
| 每次状态迁移 | 结算事实经 `commitFactChecked` 落链成功后才发布内存态 | 失败即具名返回，内存态不动（**状态提交 checked**） |

### 1.4 语义：at-least-once，窗口显式声明

- **回执落链是唯一凭据**：`sent` 结算事实落链之后，该义务再也不被投递。发送与"发送已落"是两个崩溃点，任何实现只能选一个顺序；这里选"先发送、后回执"，代价是**恰好一次多投**被容忍——重投写的是同一条回执，正是它阻止第三次。
- **六条 checked 条款**（`openspec/changes/archive/2026-11-08-success-semantics/specs/orchestration/spec.md`）：先交接后销账｜只登记入口地址的正文｜未销义务进程内补投｜状态提交 checked｜旧格式具名拒绝｜残余窗口显式声明。本页的每条状态迁移都能对回其中一条。
- **残余窗口不可恢复，故不承诺**：用户轮"`invoke` 返回 → 交付登记"是同一调用栈内的微窗口；结果在 `invoke` 返回之后才存在，登记只能在拿到结果之后写，进程在该窗口内被杀时账上还没有这条义务、也没有可读的持久正文。契约承诺的是**已登记义务的恢复**，不是窗口本身。另有两个"登记完成、发送未发生"的窗口（消费回执已落而发送未发生；用户轮登记后发送前），其恢复面是**重启**，不是进程内补投。

<a id="canonical-fact-resolution"></a>
## 二、重放取哪份事实：账本键就是身份

账本不重建状态，它**记住身份**；状态是重放的纯读数。四类记录同在一份事实面上（`FactLog`：`Put`/`Get`/`Scan`/`Sync`，四条红线要求"暂存不声称持久、屏障之后才报成功、不得把暂存当已存、前缀扫描不得给子集"）。账本自身的可恢复状态（邮箱欠账、任务账户、送达账、未应答输入、投影幸存者）由框架 checkpoint 事实族承载（见[平台篇 §六](../platform/platform-subsystems.md#framework-checkpoint)）：boot 读最新 checkpoint 载入切片、只回放偏移后事实，不再对这些前缀做全史扫描；无 checkpoint 的年轻链才全量回放，坏 checkpoint 具名拒 boot。

| 记录 | 键 | 值 | 身份从哪来 |
|---|---|---|---|
| 提交事实 | `tagent/task/v1/fact:<taskID>\x00<mutation>` | 一次状态迁移的完整记录 | `mutation` = `admit:<taskID>` / `settle:<taskID>:<round>` / `handoff:…` 等 |
| 受理材料 | `tagent/task/v1/spec:<taskID>` | 任务的准入声明 | 同键异内容 → `ErrSpecConflict` |
| 消费回执 | `tagent/task/v1/consume:<taskID>\x00<round>\x00<kind>\x00<seq>` | 该通知确认的消费 | 通知身份四段（与邮箱席位同源） |
| 尝试记录 | `tagent/task/v1/attempt:<taskID>\x00<round>\x00<kind>\x00<seq>` | follow-up 回合的终态（在飞时为空） | 同上四段，与回执**读作一本账** |

四条解析规则：

1. **一条迁移一条独立键，永不覆盖**：同一条任务的多条迁移各占一键，所以"任务历史"天然可回放；只留终态的形态会让时光机失效。
2. **同一键第二次提交同内容 = 幂等成功**：返回已存的那条事实，不写第二条；同键不同内容 = 冲突，具名拒绝。**身份就是事实**，不是"最后一次写入"。
3. **重放不重新盖章**：重放只读到已冻结的记录，不改时间、不换身份、不写第二份；因此一条通知在重放路径上永远只对应一份事实。
4. **扫描失败不得降级成部分视图**：`Scan` 要么给全量、要么报错——部分清单会让"已消费"被误读成"未消费"，进而重复驱动。

任务交接（`runtime/task/handoff.go`）走同一套身份：`handoff:` 前缀的迁移把未投递的义务改判为 `ObligationHandedOff`，随事实链持久；`Retained`/`RecoveryMaterial` 只在实例关闭时交出，不产生第二套状态。

<a id="lineage-visibility"></a>
## 三、血统与可见性：记录字段 + 入口门禁

血统（lineage）在本形态里是**记录字段**，不是独立白名单：交付记录带 `lineage`（来自该回合的 `trigger_source`，任务面固定为 `task`），供审计读出"这条正文是谁的产物"。**能否出宿主**由两道完全不同的判据决定，不得混为一谈：

| 判据 | 位置 | 规则 |
|---|---|---|
| **入口地址门禁**（渠道可见性的执行面） | `assembly/host_delivery.go` | 只有 entry 地址的正文登记义务；其余地址 headless，恢复时具名销账 |
| **域内可见性策略**（该产出的性质） | `features/evolution/meditation/judgment.go` | 宿主面印花集合 `{user, task, reincarnation, system_alert}`；`meditation` 是反思回合自己的印花，可外发；**未声明的印花一律按自管处理**（fail-closed，不投递、不计新鲜度） |

三条由此固定的判据：①`delegation` 印花（`api/event/meta.go` `LineageDelegation`）是自管流量，不开宿主面、不计新鲜度；②未知/空印花既不投递也不构成新鲜材料——"不认识"永远不等于"可以发"；③headless 销账**保留正文与目标**，所以"给了谁、为什么没发"事后可问，而不是删痕了事。

**进程内投递的账目是同一套身份的读面**：`Run.DeliveryLedger()` 把本 run 提交过的投递事实按源事件、目标与投递身份回读（`Records`／`BySource`／`ByTarget`）。两族投递——agent 主动发出的正文落 `external_input`，框架的结算通知落 `task_settled`——盖同一套五键（含 `delivery.denial`），所以"一个源事件到达了哪些目标、各自站在什么回执"不按来路分两种读法。账目不解析正文，也不松上面两道判据：它是回读面，不是第三条门。

<a id="gaps"></a>
## 四、已知缺口与演进方向

| 缺口 | 现状与防线 | 候选方向 |
|---|---|---|
| **残余微窗口不可恢复** | `invoke` 返回→登记之间的崩溃没有可恢复源；契约只承诺已登记义务 | 接受为声明性边界（已写入 spec）；若要收窄，必须让结果先生成成一条已登记事实 |
| **至少一次容许一次多投** | `sent` 回执未落地时重启会再发一次，两份回执同键覆盖 | 与 `delivery-dispatch-receipts` 的动作身份对齐后可做到"复用原动作结论"，但发送与回执的顺序不可能两全 |
| **无进程内孤儿清扫** | `fact:`/`consume:` 前缀只增不改，没有压实面 | 以链为真源的清理器（当前无） |
| **交付账本的批次原子性靠提交入口** | 结算事实经 `commitFactChecked` 单条落链，一批多键之间无事务 | 顺序不变量补偿（先登记后发送、先回执后发布） |
