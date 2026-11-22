> **历史输入（2026-11-20）**：本目录不再作为当前派发或自动规格同步输入；下文旧勾选、方案与限制按原时点保留，不授权后续实施。当前只按[编排入口](../../ORCHESTRATION.md)派发三个语义单元，全部旧要求由[承接表](../../CARRYOVER.md)逐项接收，不因转历史而取消。

# 01 任务托管收编 · 设计

> 静态决策见一级 [design.md](../../design.md) D1（P2 单一 g(x)／P4 有界恢复）、**D6.4**（读切片非全扫）、§五共享面冻结签名、§六 **S2**（共享症状锚，04 定义真量）。本域只列实施要点与实证触碰集，不改冻结签名。

## 技术要点（承 D6.4）

1. **切片扩携三族收据**：`StatedTask`（runtime/task/checkpoint.go:40-75）在 Mutations/Obligations 之外追加每任务的 consumed（已消费收据）／attempts（结算轮尝试）／rejected（具名拒绝）。三族是 settle 幂等的全部依据：`recordConsumption` 删欠账（durable.go:466）、`recordRejection` 删欠账（:508）、`attemptOf` 判是否已驱动结算轮（:418，消费方 memory.go:383）。**三族字段不用 `omitempty`**：空集显式落 `[]`，使「扩携前落链的旧切片无键」与「新切片陈述无收据」可区分（反事实：删掉该区分 → 升级后首次 boot 把已消费通知重新欠账，二次投递）。
2. **收据的陈述者与搬运方向**：三族的真相在提交账户（`DurableCommit`），不在 registry 的链折叠里。切片生产者 `tasksStateSlice`（assembly/task_checkpoint.go:21-44）同时持 registry 与提交账户，`SnapshotState` 把账户的三族按任务填进切片；`runtime/state` 单向依赖 `runtime/task`（durable.go:16），故三族的 wire 类型声明在 `runtime/task`（只含 task/api 内置类型），state 侧映射入 `consumed/attempts/rejected` 表——是搬运不是第二派生。
3. **单一记录应用路径（g(x)）**：抽出 `Commit` 已在用的入表段（durable.go:600-609：revisions/facts/mutations/owed）为一个记录应用函数，运行期提交、切片覆盖段入表、尾段入表三处共用；删 load 自己的派生循环（:317-341）。欠账过滤（consumed/rejected）由 `unowedLocked`（:768）与删欠账两函数统一维护，恢复期与运行期读到的欠账恒等。
4. **load 改读切片＋尾段按身份读**：覆盖段零任务事实族前缀读；尾段按任务身份前缀 `factPrefix+taskID+"\x00"`（键形 durable.go:814-816）读该链，成本＝存活链长＋尾，**不随历史任务总数增长**。五前缀无条件全扫只保留为兜底：无 checkpoint（Offset=0，尾＝全史）或旧切片未携三族键（该任务按未覆盖处理）。`FactLog` 四方法不变，不新增键枚举端口。
5. **时序后移**：`NewDurableCommit`（durable.go:226-234）只构造空表（`newDurableCommit` :237）；账户入表移到 `restoreCheckpointState`（run.go:690）读到最新 checkpoint 之后、`r.states.RestoreState`（assembly/checkpoint.go:798）之前——即 `restoreCheckpoint`（checkpoint.go:780）解出 `checkpoint.Slices` 后先喂账户，再按既有顺序恢复各切片。`restoreChains`（run.go:1073）随后只读合成表（`Records` :749），不再触发 log 读。
6. **mailbox 跨切片顺序耦合（必做，否则静默丢欠账）**：`mailboxState.RestoreState` 的 `owedOfWork` 读账户欠账（mailbox_checkpoint.go:89），而切片恢复顺序＝checkpoint 陈述顺序＝注册顺序（state/checkpoint.go:104-106/361），mailbox 先于 tasks 注册（run.go:638/641）。今天靠构造期全扫掩盖了该依赖。第 5 点把账户入表提到所有切片恢复之前，**覆盖段与尾段一并入表**，故 mailbox 读到的欠账与今天同集；注册顺序不动（checkpoint 字节序不变，golden 不受本域影响）。反事实：只后移 load 而不提前入表 → mailbox 恢复时账户为空，切面后提交的通知不入邮箱，重启后不投递。
7. **年轻链发现依赖 02**：任务事实是 KV 族（`TierFacts` 走 `tier.Segments.kv`，memory/ledger/tier_facts.go:25-31），事件索引只收 `<partition>:index:` 键（segment_store.go:301-336），故 `eachFactPastCut`（checkpoint.go:983）发现不了年轻任务链。年轻链身份由 **02 的尾增量**供给：`journalKV.Commit` 一帧携 Puts 键值（journal_kv.go:101-118 ✅），切面之后的帧即含新任务的事实与收据。本域消费该尾键集，**不自建索引全扫**（W1 合单的理由之一）。
8. **既有测试归位（行为变了，测试随之变）**：`TestLoadScanCounts`（runtime/state/durable_test.go:1113）钉「恰五条前缀读」——改为钉新读面（覆盖段零前缀读＋尾段按身份读＋兜底全读）；`sameChainState`（assembly/task_checkpoint_test.go:363-372）比「链折叠可算回的字段」，三族收据链折叠算不回（是独立 KV 记录，非链上链接），须如运动 stamps 一样剥离并注明理由（与 checkpoint.go:35-39 既有理由同构）；三族的等价由 1.2 的「切片 vs 全扫基线逐项相等」锚钉，不靠链折叠 parity 锚。

## 实证触碰集（✅ 读码/grep，不照搬一级清单）

- `runtime/task/checkpoint.go`：:40-75（StatedTask 扩字段）、:80-89（checkpointObligation 先例）、:93-109（SnapshotCheckpoint）、:118-161（RestoreCheckpoint）、:184-219（checkpointOf）、:222-262（record，身份校验先例 :249-252）
- `runtime/state/durable.go`：:53-57（五前缀）、:194-215（LoadStats/LoadScan，04 真量来源）、:226-249（构造与空表）、:258-343（load 全扫＋独立派生）、:348-362（scan 度量）、:435-510（收据/拒绝入表）、:566-611（Commit 入表段）、:743-787（Records/unowedLocked）
- `assembly/run.go`：:617-624（构造＋load 日志）、:638/641（mailbox/tasks 注册顺序）、:690-696（restoreCheckpointState → restoreInMemoryState）、:1073-1138（restoreChains/coverage/chainPast）
- `assembly/checkpoint.go`：:780-810（restoreCheckpoint，账户入表插入点）、:963-978（restoreNewestCheckpoint）
- `assembly/task_checkpoint.go`：:21-44（切片生产者，扩持提交账户）
- `runtime/state/mailbox_checkpoint.go`：:40-49（SnapshotState 读欠账）、:60-98（RestoreState/owedOfWork，顺序耦合）
- 测试：`runtime/task/checkpoint_test.go`:23/177/234、`runtime/state/durable_test.go`:858/968/1113/1208、`assembly/task_checkpoint_test.go`:107/206/261/308/356-381、`assembly/checkpoint_test.go`:432/932/1231（S2 共享锚）
- **不改**：`memory/ledger/*`（02 域）、`features/evolution/governance/*`（03 域）、`openspec/specs/*` 与 golden（05 域）、`FactLog` 接口、Stateful 三方法签名、mailbox/tasks 注册顺序

## 子步序（每步 `go build ./...` 保持可编译）

1.1 切片扩携（wire＋陈述＋恢复，账户读面）→ 1.2 load 改读切片（抽同一入表函数，删独立派生）→ 1.3 时序后移（构造空表＋入表提前到切片恢复前＋尾段/年轻链兜底）→ 1.4 症状锚与消费者锚（真量计数，W0 红转绿）。门绿是 W1 完成判据，非每子步；build 绿才是每子步。与 02/04 合单串行，02 的尾键集在 1.3 之前就绪。

## 风险与回退

- **settle 幂等**：三族缺一即回归——缺 consumed → 已消费通知重新欠账（二次投递）；缺 attempts → 再驱动第二次结算轮；缺 rejected → 已拒绝通知再投。1.1/1.2 以「切片 vs 全扫基线逐项相等」钉三族，1.3 以真实 boot 钉投递/结算不重复。
- **崩溃一致性**：切片在安静点快照（state/checkpoint.go:28-35，不持框架锁、不 flush），故切片恒不超前于已提交事实；切面之后落库的事实与收据由尾段补齐，二者不重不漏（记录应用按 mutation 身份幂等，收据按通知四段身份幂等）。尾段读失败 SHALL 具名拒绝 boot（沿用 `ErrSegmentBodyMissing` 响亮失败取向，一级 D7），不静默按空账启动。
- **旧切片（扩携前落链）**：无三族键 → 该任务按未覆盖处理、读其链兜底，并记一行可检索日志；SHALL NOT 把缺失键当「无收据」参与求值（注入编造值）。
- **已清扫任务不再复活（语义分叉，⚠️ 推断，须实施期夹具实证）**：今天 `TaskIDs()`（durable.go:725）来自全扫，故终态清扫（`PruneTerminal`）已丢弃的历史任务会被 boot 重新折叠回账；改后链集＝切片点名 ∪ 尾，已清扫任务不再复活。依据：切片语义「账的大小＝registry 保持的工作量，不是已结链的大小」（checkpoint.go:5-11）。实证锚：`TestTaskTailFoldLeavesNoSweepStamp`（assembly/task_checkpoint_test.go:308）＋清扫后重启账与运行期账逐项比对；若夹具证伪则上报编排者，不擅自改冻结裁定。
- **回退**：本域回退＝恢复构造期全史读（症状回红，S2 回滚必红作 F3 证据）；`git revert` 整原子单（01+02+04）。不删已提交事实，不改链上字节；切片扩携是追加键，回退后旧代码读新切片忽略未知键（`encoding/json` 既有行为），无需数据迁移。

---

# 本轮重整（05.R1 / 05.R2）

> 上文为 W1 时的实施要点与触碰集，保留为历史时点记录（其锚多数仍绿，见 tasks.md 复验栏）。本节起为本轮证据、设计门问题与候选设计；**新类型/签名一律候选、未冻结**，冻结由 05.R1 结论 + 编排者单写共享面共同完成。

## 七、本轮证据（✅ 读码；行号只作定位）

| 编号 | 证据 | 含义 | 限制 |
|---|---|---|---|
| E05-1 | `runtime/task/checkpoint.go:82-99` `StatedTask` 三族收据；`:160-181` `StateSettleAccount` 逐任务调 `ReceiptsOf` | 托管面在树内，切面按任务陈述收据 | 未测切面耗时随任务数的实际曲线（R1 补红锚） |
| E05-2 | `runtime/state/durable.go:1089-1155` `ReceiptsOf` 对每个任务遍历三张**全库 map** | 切面陈述成本≈O(任务数 × 全库收据数)，已结历史越多越贵 | 未测字节量；口径须与 04 的真量对齐后再声明 |
| E05-3 | `assembly/task_checkpoint.go:32-50`：`tasksStateSlice` 只实现 `StateName/SnapshotState/RestoreState`，无 `SnapshotSessionState` | 任务账是 registry 全局账，不进会话切面（有意设计） | 反例方向：不得因压缩/投影按会话陈述就要求任务账也按会话 |
| E05-4 | `runtime/state/durable.go:1158` `unowedLocked`、`:756/:798` 收据与拒绝的删欠账；`:887` `Commit`、`:417` `Seat`、`:381` `applyRecordLocked` | 欠账恒等式与单一记录应用路径已成型，清减必须落在同一路径 | `PruneTerminal` 清扫后收据不再陈述的判重后果未证（R1 必答） |
| E05-5 | `runtime/state/mailbox.go:257-299`：`Claim`/`Consume` 与「最长连续已消费前缀」推进（`Consumed++` 循环） | 乱序完成会把位置停在空洞前，但逐条收据仍记录 | 与 05 的欠账恒等式组合后是否等价，未证 |
| E05-6 | `assembly/checkpoint.go:913-932` `seatDurableAccount` 用 `tailFactKeys`（`:948-963`）取尾键集 | 尾段读靠引擎 `TailKeys()` 探针 | 探针缺失时退化为整份读（已记录），清减不得依赖该退化 |

## 八、05.R1 设计门：四个必答题

门不通过就不进 R2 的实现叶（一级 D8 G2）。每题都要给出**判据 + 反例 + 可复跑命令**，不得只给结论：

1. **有效集**：任务账的当前有效态是「registry 现存任务」还是「registry 现存任务 ∪ 未结收据所属任务」？前者会让已清扫任务的迟到收据无处安放，后者要求账能反向发现任务。证据须来自真实清扫路径（`PruneTerminal` 与 `task_terminal_ttl` 的接线现状），不是代码形态。
2. **判重身份保留边界**：一条收据表达的身份要保留到什么时候才不破「至多结算一次」？候选边界按强弱排列——(a) 该任务记录仍在账内；(b) 该通知的原事实仍在热层（水位之前已搬冷则不再保留）；(c) 按身份重放不再可达该身份（冷层按身份仍可读 ⇒ 永不到达）。**禁**：按时间阈值 TTL 删除收据来表示「已完成」。R1 必须选一条并给出跨进程重试的反例。
3. **乱序完成**：后序先行完成、前序留洞时，「最长连续已消费前缀」与「欠账恒等式 outbox−consumed−rejected」是否分叉？若分叉，清减只能以逐条收据为准，不能以前缀水位为准（否则洞被水位抹掉）。
4. **与 11 的衔接**：若 11.R2 把事实提交切成「原文/ref 与索引两笔」，收据的判重身份以哪一笔为准？11 未落地时本叶**不得自造第二判据**，应标 blocked 上报，不硬推。

产出物（全部落在本域 design.md，不新增文件）：判据表（与 07.R1、12.R1 合并成**一张**，三域不得各写一版水位定义）＋两枚红着的锚：
- 正锚（拟新增、尚未建立）：`TestTaskAccountStatedSizeTracksLiveWork`——同一任务数、历史已结收据递增 N 代，断言切面陈述的收据条目数与字节不随已结收据线性增长；今天红。
- 负锚（拟新增、尚未建立）：`TestTaskSliceRefusesToDropAReceiptOfAnOwedNotice`——把一条仍欠账通知的收据从切面清掉，恢复后该通知不得重新欠账、不得二次结算；今天不可表达（无清减面）。

### 八·答（05.R1 本轮取证，源树＝隔离 HEAD `9547faca`）

**源树绑定**：以下全部行号与实测均取自 `git archive 9547faca` 解出的隔离树 `/tmp/recovery-g2task/tree`，不是工作树。工作树当时有另一单在改 `memory/ledger/{segment_store,journal}.go` 与四个测试文件，故主树未跑任何 `go test`/`go build`、未写任何 `.go`。`runtime/state/durable.go` 在主树与 HEAD 逐字节相同，读主树亦安全，但本节行号一律以隔离树为准。design.md 上文 §七 的行号是本轮早期读工作树所得，与 HEAD 有偏移（例：`ReceiptsOf` 在 HEAD 是 :1046-1090，§七 记 :1089-1155），以本节为准。

#### 答 1 · 有效集：切片点名（携链）∪ 尾键集，成员判据是 `facts` 不是 `specs`

**判据**：座后账户的有效集 = `d.facts` 的键集，由 `TaskIDs()` 答（`runtime/state/durable.go:1003-1014`）。它由两个来源合成：切片点名且携 Mutations 的任务（`seatTaskLocked` → `applyRecordLocked`，:459-511 / :365-389），与尾键集点名的任务（`tailTaskIDs` → `readTaskLocked`，:591-610 / :517-585）。欠账集 = 该有效集上 outbox − consumed − rejected，**逐条按 notice 四段身份**判定（`applyRecordLocked` :375-388、`unowedLocked` :1115-1134）。

**两个候选答案都不成立**：
- 不是「registry 现存任务」。清扫是 registry 侧的：`pruneTerminalLocked` 在每次 `Register` 内联调用（`runtime/task/lifecycle.go:214`），TTL 由 `task_terminal_ttl` 经 `assembly/run.go:651` 传入，接线是活的。它只 `delete(r.tasks, taskID)`（lifecycle.go:484），不动 durable 键。切片来自 registry 快照（`runtime/task/checkpoint.go:208-224`），故已清扫任务不进切片、不进座后账户。
- 也不是「registry ∪ 未结收据所属任务」。账户没有反向发现能力：`ReceiptsOf` 只按传入 taskID 过滤（durable.go:1053-1079），`TaskIDs` 只看 facts。收据不能把任务拉回账内。

**迟到收据的安放（实证，非推断）**：已清扫任务的五族 durable 键**永不搬冷**——`keepKeysLocked` 只结算 `<pid>:idx:`、event slot、window meta、`ref:`/`blob:` 五类键（`memory/ledger/segment_store.go:954-1009`，:947-949 注释明写「every key of another family is not settled at all」），`tagent/task/v1/*` 不属任何一类。故按身份读恒可答，而座后账户不含它。

**反例（已应用，隔离树实测）**：`TestEffectiveTaskSetIsTheStatedAccountPlusTheTail` —— 三个任务落库，切片只点名其一、尾只点名其二，第三个既不点名也不在尾。结果：`seated=[task-g2-covered task-g2-young]`，第三个任务 `ReceiptsOf` 在座后账户答 0 条收据、按身份读答 1 条。**PASS ×10**。

**新发现的成员判据分叉（须 05.R2 明确）**：切片点名但 `Mutations` 为空的任务进 `d.specs`（:467）却不进 `d.facts`，故不在 `TaskIDs()` → `restoreChains` 不折它（`assembly/run.go:1115-1124`），而 `RestoreCheckpoint` 已把它放进 registry（checkpoint.go:263）。即「registry 持有」与「账户点名」在零链任务上不一致。本轮未证该形态在生产是否可达（admission 是否恒携首条 mutation），标 ⚠️ 未证。

**命令**：`go test ./runtime/state/ -run '^TestEffectiveTaskSetIsTheStatedAccountPlusTheTail$' -count=10 -v`

#### 答 2 · 判重身份保留边界：选 (c)，且实测 (c) 在任务族上永不到达

**真实路径（三层，逐层读码 + 实证）**。「旧键再来」在三层上各有其合法语义，**不是上游错误**：

| 层 | 入口 | 同身份同内容 | 同身份异内容 |
|---|---|---|---|
| 事件事实 | `commitFactNoCadence` `assembly/facts.go:94-105` | 返回原键、不落第二条 | `memory.ErrFactKeyConflict` 具名拒绝 |
| 任务 mutation | `DurableCommit.Commit` `runtime/state/durable.go:850-856` | `CommitCommitted`、不落第二条、**不推进任何投递态** | `task.ErrMutationConflict`；revision 不符另答 `ErrRevisionConflict`（:860-862） |
| 通知 | `noticeSeat` `runtime/state/mailbox.go:161-173`；`Consume` :281-283 | 已接受、不追加第二条；已消费答 `ErrClaimSettled` | `ErrNoticeConflict` |

`runtime/agent/loop.go:1540-1549` 的稳定 instruction 键走的正是第一层：同一执行 site 的后续轮次重提交同一身份，落不出第二条事实。这是**设计内的合法重提交**，任何把「旧键到达」一律判为上游错误的判据都会杀掉正常委托续轮。三族收据的写入侧同样幂等：`recordConsumption` :723-725、`recordRejection` :766-768、`recordAttempt` :668-670 对已持身份直接返回 nil，不落第二对。

**反例（合法重提交，PASS ×10）**：`TestRepeatCommitOfAStoredMutationReowsNothing` —— 消费一条通知后重提交同一 mutation：owed 保持 2、facts 保持 1；换内容重提交被具名拒绝。

**两条真实缺口（本轮实证，×10 恒红，非推断）**：

- **缺口 A · 运行期提交与恢复路径过滤不一致**。`Commit` 的入表段（durable.go:881-887）对 outbox 每条信封**无条件**写 owed；`applyRecordLocked`（:375-388）跳过已被 consumed/rejected 关闭的。故「一条新 mutation 携一条已消费通知」在运行期重新欠账、在恢复期不欠账。实测 `TestRuntimeCommitMustNotReoweAConsumedNotice`：**runtime owed=3 / recovery owed=2**，同一组记录同一条收据。`MemoryCommit.Commit`（`runtime/task/commit.go:211-218`）同样不过滤，但它不持收据表，故该分叉只在 `DurableCommit` 上成立。这正是「单一 g(x)」未兑现的一处：入表段抽出了，过滤没抽出。
- **缺口 B · 覆盖路径重发已结身份**。`RestoreCheckpoint` 只对**未结义务**调 `seq.Observe`（`runtime/task/checkpoint.go:267-269`），链折叠对**每条**信封调（`runtime/task/restore.go:293`）；`CheckpointRestored` 不做任何 Observe（restore.go:181-197）。故被 checkpoint 覆盖、投递义务已关闭的任务，重启后 `IssueNotice` 重新发出**同一四段身份**。实测 `TestCoveredTaskMustNotReissueASettledNoticeIdentity`：**covered seq=0 / folded seq=1**，重发身份 `task-131edbb2…:1:progress:0`。四段相同而 Event 不同 → 按 `noticeSeat` 是 `ErrNoticeConflict`，按 `Commit` 是新 mutation → 触发缺口 A。两缺口组合即「重启后二次投递」的可达机制；**组合路径本身未端到端实证**（见 §十二）。

**保留边界裁定：选 (c)「按身份重放不再可达该身份」**，并实测该边界在当前存储上**永不到达**：
- 任务五族键永不搬冷（segment_store.go:954-1009）⇒ 按身份读恒可达；
- 通知指向的事件事实即使搬冷，`eventByKey`/`coldEventLocked` 仍按身份透明回冷（segment_store.go:1215 / :793）⇒ 也恒可达。

故 (a)、(b) 均**不是**安全边界：(a) 被清扫路径证伪（收据在账外仍可按身份读到，且缺口 B 会重发身份）；(b) 被「任务族不搬冷」证伪（热层恒持有，水位永不越过它们）。

**代价（显式，不两头宣称）**：选 (c) 意味着**三族收据几乎不可清减**。05.R2 的成本收益只能来自**候选甲（按任务键索引）**——不删任何收据，只把 `ReceiptsOf` 的 O(全库) 降为 O(该任务)。**候选乙（水位＋未结项）在 (c) 下不成立**：它要求已结收据不再被陈述，而身份仍可达，负锚 `TestTaskSliceRefusesToDropAReceiptOfAnOwedNotice` 正是这条的反例（实测 RED ×10：清掉一条收据后座后账户把该通知重新欠账）。

**禁 TTL 的机理**：`PruneTerminal` 的 TTL 是 registry 记录的存活界，不是收据的判重界。把它当收据删除判据，就是缺口 A/B 的放大器。

**命令**：
`go test ./runtime/state/ -run '^(TestRepeatCommitOfAStoredMutationReowsNothing|TestRuntimeCommitMustNotReoweAConsumedNotice)$' -count=10 -v`
`go test ./runtime/task/ -run '^(TestCoveredTaskMustNotReissueASettledNoticeIdentity|TestTaskSliceRefusesToDropAReceiptOfAnOwedNotice)$' -count=10 -v`
> **勘误（R68）**：上行第二枚锚已改名退役，重定后为 `^(TestTaskSliceReadsBackTheReceiptItDoesNotState|TestTaskSliceTrustsAStatedEmptyReceiptSet)$`（`§十七·4 勘误`）。按旧名复跑会零匹配 exit 0，不得据此判绿。

#### 答 3 · 乱序完成：两者不分叉，但前缀水位不是清减边界（它根本不落盘）

**判据**：欠账以**逐条 notice 四段身份**为准。`session.Consumed` 的「最长连续已消费前缀」是**进程内 mailbox 位置**，与欠账恒等式不同层，不得用作清减水位。

**机理**：`Consume` 先写逐条收据（`mailbox.go:287-291` → `recordConsumption` 删 owed，`durable.go:743-744`），再推进前缀（:294-300）。前缀停在洞前不影响任何已落收据。

**关键事实（读码实证）**：`session.Consumed` **不进任何切面**。mailbox 切面只陈述 `Owed`（`runtime/state/mailbox_checkpoint.go:32-34`）；`foldOwed` 经 `AcceptNotice` 重新分配 `AcceptSeq`（`mailbox.go:177-191`，`restoreSeat` 置 `nextSeq: 1`，`memory.go:465-468`）。故前缀水位是**每进程重建的局部量**，跨重启不可比。任何以它表达的清减水位都是把非持久量当持久判据。

**反例（已应用，PASS ×10）**：`TestOwedSetKeepsTheHoleWhenLaterNoticesComplete` —— 接受 seq1/2/3，先消费 3 再消费 2。实测 `mailbox position=0, maxSettledSeq=3, owed=1（洞）, 以 max-seq 作水位会丢 1/1`；同一账户座后四面与运行期逐项相等。

**正确形状**：欠账恒等式 outbox − consumed − rejected 保持不变（它已经是逐条集合运算）。若 05.R2 引入任何「水位」，它只能是**「该身份已不可按身份重放到达」的判据别名**，不得是任何序号前缀；且必须与「未结集合」并列陈述，未结集合恒以逐条身份表达。这与 12.R1 的 J1（水位不覆盖未应用事实）是同一约束在任务域的实例，**水位定义由三域共用一张表，本域不另立一版**。

**命令**：`go test ./runtime/state/ -run '^TestOwedSetKeepsTheHoleWhenLaterNoticesComplete$' -count=10 -v`

#### 答 4 · 与 11 的衔接：**BLOCKED**，不自造第二判据

11.R2 未落地。当前收据的判重身份是 notice 四段（`noticeKey`，`mailbox.go:69-74`），`consumeRecord.Event` 是内容不是身份——`durable.go:68-71` 注释明写「The identity alone decides which notice the receipt is about; the event reference stays content beside it」。

据此可确定的部分：事实提交若切成「原文/ref 与索引两笔」，**只要 notice 四段不变，收据身份不变**，本域判据不需第二套。不可确定的部分：若 11 改变的是「同一逻辑通知可否携不同 Event 重提交」，则缺口 A/B 的判定面随之改变（缺口 B 的重发正是「四段同、Event 异」）。

**处置**：标 blocked 上报编排者，等 11.R2 的事实身份等价定义。本叶不硬推、不预设 11 的签名。

### 八·表 G2 判据表（任务域 portion，供编排者与 07.R1/12.R1 合并成一张）

行 id 前缀 `G2-T` 为本域专有，不与 07（治理）/12（消费者生命周期）的行 id 冲突。**水位定义共用一行 G2-T4，三域不得各写一版。**

| 行 id | 判据（须成立的不变量） | 反例（真的应用过） | 可复跑命令（隔离 HEAD 树） | 本轮状态 |
|---|---|---|---|---|
| G2-T1 | 座后有效集 = 切片点名（携链）∪ 尾键集，成员判据是 `facts`；已清扫任务不在账内但其收据按身份仍可查 | 三任务落库、切片点名一、尾点名一 → 第三者座后 0 收据 / 身份读 1 收据 | `go test ./runtime/state/ -run '^TestEffectiveTaskSetIsTheStatedAccountPlusTheTail$' -count=10 -v` | ✅ 实测 PASS ×10 |
| G2-T2 | 欠账恒等式 outbox − consumed − rejected，逐条按 notice 四段身份；座后态与身份读在 revisions/facts/owed/consumed/attempts/rejected 六面逐项相等 | 三路径（整份读 / 切片座 / 按身份读）对同一账户比对 | `go test ./runtime/state/ -run '^TestSeatedAccountAgreesWithIdentityRead$' -count=10 -v` | ✅ 实测 PASS ×10（覆盖任务 0 次前缀读；身份读 5 次） |
| G2-T3 | 判重身份保留边界 = (c) 按身份重放不再可达；任务五族永不搬冷 ⇒ 该边界永不到达 ⇒ 三族不可清减，成本只能由按任务索引降 | 把族**陈述空集**而链上有收据 → 座后重新欠账 | `go test ./runtime/task/ -run '^TestTaskSliceTrustsAStatedEmptyReceiptSet$' -count=10 -v`（原名按 R68 重定为两枚锚，见 `§十七·4 勘误`） | ✅ 反例成立：旧锚 RED ×10 但判据没牙（夹具可平凡满足）；重定后该方向由负锚钉住，且「省略族」的合法清减由正锚证明可从链读回 |
| G2-T4 | **共用行**：任何水位不得覆盖未结项；`session.Consumed` 前缀是进程内局部量、不落切面，不得作清减边界 | 先消费 seq3 再 seq2 → position=0、 owed 恰剩 seq1；max-seq 水位丢 1/1 | `go test ./runtime/state/ -run '^TestOwedSetKeepsTheHoleWhenLaterNoticesComplete$' -count=10 -v` | ✅ 实测 PASS ×10 |
| G2-T5 | 旧键重提交在三层各自幂等：同身份同内容不落第二条、不推进投递态；异内容具名拒绝。稳定 instruction 键的重提交是合法路径 | 消费后重提交同一 mutation → owed/facts 不变；换内容 → 具名拒绝 | `go test ./runtime/state/ -run '^TestRepeatCommitOfAStoredMutationReowsNothing$' -count=10 -v` | ✅ 实测 PASS ×10 |
| G2-T6 | 运行期 `Commit` 与恢复 `applyRecordLocked` 必须共用同一 owed 过滤（单一 g(x) 的过滤段，不只是入表段） | 新 mutation 携已消费通知 → runtime owed=3 / recovery owed=2 | `go test ./runtime/state/ -run '^TestRuntimeCommitMustNotReoweAConsumedNotice$' -count=10 -v` | ❌ **缺口 A**，RED ×10（HEAD 既有缺陷，非本轮引入） |
| G2-T7 | 覆盖路径与链折叠路径必须对同一账户发出同一 notice 身份序列 | 覆盖路径重发已结四段身份 seq=0，折叠路径答 seq=1 | `go test ./runtime/task/ -run '^TestCoveredTaskMustNotReissueASettledNoticeIdentity$' -count=10 -v` | ❌ **缺口 B**，RED ×10（HEAD 既有缺陷） |
| G2-T8 | 一次切面陈述某任务的收据只读该任务自己的收据面；陈述条目/字节随有效工作计费，不随已结收据线性增长 | 2 个存活任务 × 5 代 × 每代 4 条收据 → 陈述条目 8→40（5×），存活任务恒 2 | `go test ./assembly/ -run '^TestTaskAccountStatedSizeTracksLiveWork$' -count=10 -v` | ❌ RED ×10（症状成立，待 05.R2 候选甲转绿） |
| G2-T9 | 收据判重身份与 11 的事实身份等价须一致；11 未落地时不得自造第二判据 | — | — | ⛔ **BLOCKED**（11.R2 未落地） |

**G2-T8 的实测计数（失败信息原文摘录）**：`stated 40 receipt entries at the last generation against 8 at the first (bound 16), while the live task count stayed 2 and the durable account grew from 8 to 40 receipts`；`measured 2 tasks x 3 families x 40 store receipts = 240 comparisons per cut`；`stated receipt bytes 2379 -> 6943, whole payload 3713 -> 8277`。条目比 40/8 = 5 = 代数，存活任务比 = 1，故「线性增长」的判定**与倍数无关**。`bound = first.entries*2` 是**候选容差、非冻结裁决**（R19 N1：未经校准的 2x 不是冻结值）；墙钟未作断言面（实测 470µs→145µs 反向，噪声大于信号）。

### 八·N11 前置条件（本域供 09.R2 消费，不实现）

R24 N11 的零是**正确的有界行为**，缺的是「这个零意味着什么」的前提。本域给出的可判定前提，全部读**boot 实际座下的账户**，不是库存枚举：

1. `durable.Seated()` 为真（`durable.go:445-452`）——座发生过；
2. `len(durable.TaskIDs()) > 0` 且逐代增长——账户非空且真增长（`judgeBootFixture` 已核此项，`assembly/symptom_anchor_recovery_test.go` HEAD:155-160，须提为 durable 判定的**前提**而非并列检查）；
3. `LoadStats().Scans` **为空**，而不是「五条 keys=0 的扫描」。二者是不同形态：前者是切片座成功（覆盖任务零前缀读，实证 G2-T2），后者是走了整份读兜底（`Seat` :409-416）却读了个空存储。现有 `durableLoadCountOf` 把两者都求和成 0（HEAD:96-100），故必须分辨。

三条同时成立时，`durableLoadCount == 0` 才是「非空账户上托管成功」的有界证据；任一不成立即具名失败，不得恒真。

**与 N6 的关系（本域只供判据，接线归 09.R2/13.R2）**：托管 boot 的真实可观测后果是「它座下了哪些任务、按身份读回了什么四面」，即 `TaskIDs()` + `ReceiptsOf`/`Outbox` 的答案，而不是它扫了多少族。**不得**以补发库存键计数行使 attestation 转绿（R20 N6 已删该形态）：库存计数与「是否真重读了链」无因果关系，一个什么都没托管的 boot 也能发满五行。

## 九、05.R2 候选设计（未冻结）

- **候选甲（推荐）· 按任务键索引**：三张收据 map 的键已含 task 身份（`noticeKey{task,round,kind,seq}`），补一份 `map[event.TaskID]` 的键索引，`ReceiptsOf` 与切面陈述退化为 O(该任务收据)。索引由同一组函数在插入/删除收据处同步维护（`recordConsumption`/`recordRejection`/`recordAttempt` 与 `Seat`/`applyRecordLocked`），仍是单一 g(x)（元规则二：第二使用方是切面生产者，独立于索引存在）。
- **候选乙·切面改陈述**：`RegistryCheckpoint` 追加「收据判重保留水位」，切片不再逐条陈述已结收据，只陈述水位 + 未结项。代价：判重身份必须能按身份重现（依赖链与冷层可读），且 `PruneTerminal` 的清理后果要重新论证。**乙不能单独成立**，须以甲或等价索引为前提，否则「未陈述」无法与「无收据」区分。
  - **【G2 勘误（本轮，取代上一句的「须以甲为前提」）】乙在 G2-T3 选定的 (c) 边界下不成立，不是「前提不足」而是「判据相反」**：任务五族键永不搬冷（`memory/ledger/segment_store.go:954-1009`），事件事实搬冷后仍按身份透明可读（:1215/:793），故「按身份重放不再可达」永不到达，已结收据必须恒可陈述。反例 `TestTaskSliceRefusesToDropAReceiptOfAnOwedNotice` 实测 RED ×10：清掉一条收据即重新欠账。**乙不得实施**；成本收益只由甲取得。若将来存储提供了「身份确已不可达」的条件，乙须重新过 G2，不得以本条历史文本为据。
- **已否**：删 `attempts`/`rejected` 省成本（破幂等，见上文风险节）；把任务账改成会话切面（推翻 E05-3 的有意设计）；用 TTL 删未完义务。
- **恢复一致性要求**：无论甲乙，恢复侧（`Seat`：切片入表 + 尾段；无切片时整份读）与运行期 `Commit` 必须共用同一记录应用路径；清减后「未陈述」与「陈述空」的区别必须在载荷里显式（沿用旧设计要点 1 的反事实：删掉该区分 → 升级后首次 boot 把已消费通知重新欠账）。

## 十、风险与回退（本轮）

- **判重身份过早消失**（最重）：表现为重启后同一通知被再派一次或二次结算。回退＝保留三族全量（回到今天），症状是切面成本回涨而非正确性回退；故正确性优先于成本，判据不清就不清减。
- **前缀水位抹洞**（答 3 若错）：乱序完成时把未完成的前序项跳过。负锚必须钉「洞仍在欠账」。
- **跨进程重试的代价**：判据 (c) 意味着几乎不清减；判据 (a) 依赖清扫语义。R1 须显式写出选了哪条、放弃了什么成本收益，不得两头宣称。
- **回退面**：本域回退＝删按任务索引、切面回到逐条 `ReceiptsOf`（成本回涨、症状 S2 不因此转红，因 S2 量的是 boot 侧 `durableLoadCount`）；不删任何已提交事实；新增字段为追加键，回退后旧代码忽略未知键（`encoding/json` 既有行为）。
- **不越界**：压缩账清减（03）、治理账清减（07）、插件义务交接（12）不在本域；本域只消费 12 的「水位不覆盖未应用事实」边界（J1），不实现它。

## 十一、G2 任务域判决：**PARTIALLY-PROVEN**（不放行 05.R2 全面实施）

**判决**：四问中三问已给出可判定判据并以真的反例实证（答 1/2/3），第四问 BLOCKED（答 4，11.R2 未落地）。判据表 G2-T1…G2-T9 已落 §八·表，其中 **G2-T6/G2-T7 是 HEAD 既有缺陷、本轮首次取得源码级实证**，G2-T8 症状成立。

**为什么不是 PASS**：
1. G2-T9 BLOCKED，判据表不完整；按 §八.4 的冻结要求，11 未落地不得自造第二判据。
2. G2-T6/G2-T7 未修。二者组合是「重启后二次投递」的可达机制，属正确性而非成本；在它们闭合前，任何清减都是把判重面进一步缩小，方向相反。
3. 两枚拟新增锚只在隔离树建立，**主树未落盘**（派发令禁写主树 `.go`）。冻结命令的 `-run` 正则与测试名逐字符一致，但验收面要求它们在主树可复跑，本轮不满足。

**为什么不是 FAIL**：三问的判据均可判定、反例均真的应用且原版通过、消费者锚四枚 ×10 全绿；症状与缺口都取得了可复跑的原始退出码，不是纸面推演。

**放行建议（供编排者裁定，本叶不自裁）**：
- 05.R2 的**候选甲（按任务键索引）可以先行**：它不删任何收据、不改恒等式，与 (c) 边界相容，只降陈述成本。G2-T8 是它的转绿锚。
- **候选乙（水位＋未结项）在 (c) 下不成立**，不得实施（G2-T3 反例已证）。
- G2-T6/G2-T7 须**先于或同批于**任何清减落地。二者写面：G2-T6 在 `runtime/state/durable.go` 的 `Commit` 入表段（05.R2 白名单内）；G2-T7 在 `runtime/task/checkpoint.go` 的 `RestoreCheckpoint`（05.R2 白名单内）。**但 G2-T7 的修法有分叉**（在 `RestoreCheckpoint` 内对已结收据也 Observe，还是在 `CheckpointRestored` 内补，还是让 `StatedTask` 携序列高水位），属设计级分叉，须编排者冻结后再派，不由本叶自选。
- 主树落盘两枚锚须单独授权（派发令禁写主树 `.go`），且 `runtime/task/g2_owed_receipt_test.go` 用外部测试包 `task_test` 引 `runtime/state`（无环：state 引的是库包 task），该形态不在 05.R1 原候选白名单（原列 assembly + runtime/state 两文件）内，须编排者确认改道。

## 十二、本轮实证台账与未验证项

### 十二·1 实测命令与原始退出码（全部在隔离 HEAD 树 `/tmp/recovery-g2task/tree`）

隔离树建立：`git archive 9547faca | tar -x -C /tmp/recovery-g2task/tree`。Go `go1.27.1 darwin/arm64`。主树零 `.go` 写入、零 `go test`/`go build`。

| 命令 | exit | 命中 | 结果 |
|---|---|---|---|
| `go build ./...` | 0 | — | 隔离树可编译 |
| `go vet ./runtime/state/ ./runtime/task/ ./assembly/` | 0 | — | 三个原型包无 vet 红 |
| `go test ./runtime/state/ -run '^(TestDurableLoadReadsTheTaskSlice\|TestDurableLoadRefusesAMismatchedReceiptIdentity)$' -count=10 -v` | 0 | 20 RUN / 20 PASS | 消费者锚（既有）保持绿 |
| `go test ./runtime/task/ -run '^(TestTaskSliceStatesTheSettleAccount\|TestRegistryCheckpointStatesTheAccountNotTheHistory)$' -count=10 -v` | 0 | 20 RUN / 20 PASS | 消费者锚（既有）保持绿 |
| `go test ./assembly/ -run '^TestTaskAccountStatedSizeTracksLiveWork$' -count=10 -v` | 1 | 10 RUN / 10 FAIL | 正锚 RED，10 次量逐字相同（40 vs 8） |
| `go test ./runtime/task/ -run '^TestTaskSliceRefusesToDropAReceiptOfAnOwedNotice$' -count=10 -v`（**历史行：锚已按 R68 退役，复跑零匹配会 exit 0，不得据此判绿；新命令见 `§十七·4 勘误`**） | 1 | 10 RUN / 10 FAIL | 负锚 RED，具名指出重新欠账的通知身份 |
| `go test ./runtime/state/ -run '^(TestOwedSetKeepsTheHoleWhenLaterNoticesComplete\|TestSeatedAccountAgreesWithIdentityRead\|TestEffectiveTaskSetIsTheStatedAccountPlusTheTail\|TestRepeatCommitOfAStoredMutationReowsNothing\|TestRuntimeCommitMustNotReoweAConsumedNotice)$' -count=10 -v` | 1 | 50 RUN / 40 PASS / 10 FAIL | 四枚 PASS，`TestRuntimeCommitMustNotReoweAConsumedNotice` 10 次恒红 |
| `go test ./runtime/task/ -run '^TestCoveredTaskMustNotReissueASettledNoticeIdentity$' -count=10 -v` | 1 | 10 RUN / 10 FAIL | 缺口 B 恒红 |

红项归属：G2-T6/G2-T7/G2-T8 的红**不是本轮原型引入**——三者量的是 HEAD 生产代码（`Commit` 入表段、`RestoreCheckpoint` 的 Observe 面、`ReceiptsOf` 的全库遍历），原型只驱动它们，未改任何生产文件。隔离树内 `git` 不可用（archive 无 `.git`），生产面与 HEAD 的一致性由「archive 直接解出 + 只新增 `_test.go`」保证。

### 十二·2 未验证项（显式登记，不按已核处理）

1. **缺口 A + 缺口 B 的组合路径未端到端实证**。两段各自 RED ×10，但「真实 boot 覆盖一个投递义务已关闭的任务 → 重启 → 续轮发出同一四段身份 → 二次投递」这条完整链路未跑。定它需要：一个跨进程夹具（localfile 根 + 两次 assemble），第一代关闭投递义务并落 checkpoint，第二代续轮并数投递次数。归 05.R2 或编排者窄派。
2. **零链任务（`Mutations` 为空）在生产是否可达**未证（答 1 末）。定它需要：枚举 `Register`/`Admit` 路径是否恒携首条 mutation。
3. **`ReceiptsOf` 的遍历计数是算术推导，不是插桩实测**。240 = 2 任务 × 3 族 × 40 条，其中 40 与 2 是实测量，×3 来自 `durable.go:1053/1062/1071` 三个全 map range 的读码。未在生产插桩计数（禁写生产代码）。墙钟未作证据（噪声反向）。
4. **未跑全量回归**：只跑了三个包的选择器与 `go build ./...`。`assembly` 全包（既有记录 260–334s）、`memory/ledger`、跨会话/跨进程原验收（`TestRestoredTaskKeepsTheRevisionTheTailExpects`）本轮未跑。
5. **未读**：`memory/ledger/segment_store.go` 与四个 mid-edit 测试文件的工作树版本（一律读 HEAD）；07/12 域的 plan 目录；`golden/`；CI 制品原文（N6/N11 的引用取自 ORCHESTRATION 台账，未独立复算 CI 日志）。
6. **G1/G3/G4 未判**，本域不声称它们已定；G2 整体判决由编排者合并三域 portion 后给出，本节只是任务域 portion。

## 十三、A+B 合成裁决（窄派取证，隔离 HEAD 树；本节闭合 §十二·2 第 1 项的「未证」状态，不改写上文）

### 十三·0 源树绑定与写面

全部证据取自 `git archive 9547faca | tar -x -C /tmp/recovery-compose/tree` 解出的隔离树。主树零 `.go` 写入、零 `go test`／`go build`。新增文件只有两个：`assembly/g2_composition_test.go`（本轮四枚探针）与 `runtime/task/g2_sequencer_test.go`（从 `/tmp/recovery-g2task/tree` 复制的既有原型）。归因用的三处生产补丁（`runtime/state/durable.go` 的 `Commit` 过滤、`runtime/task/checkpoint.go` 的 `Observe` 面、`runtime/state/mailbox_checkpoint.go` 的 `owedOfWork` 消费集查询）**只在隔离树临时应用，跑完即还原**；收尾 `diff` 逐字确认三个文件与 `git show 9547faca:<path>` 完全相同。Go `go1.27.1 darwin/arm64`。

### 十三·1 判决：**合成不成立**——A 与 B 是两条各自独立充分的重复投递机制，无一需要对方

R30 登记的「两缺口组合即重启后二次投递的可达机制」这一推测**被实测否定**：不存在需要 A 与 B 同时在场才出现重复投递的路径。2×2 归因（下表）显示两者各自单独充分，且触发入口不同。

同时得到一条比原推测更强的结论：**A 单独经公共入口即可在重启后造成重复投递**，不需要任何手接内部端口；今天挡住它的不是机制，而是「生产的通知铸造面全是终态转移」这一偶然事实。

### 十三·2 真实路径（公共入口逐步，行号以隔离 HEAD 树为准）

夹具形态沿用 `assembly/task_checkpoint_test.go`：`snapshotTestRun`（`assembly/request_test.go:294`）→ `assemble`，两代共用一个 `ledger.StorageJournal` 临时根，第一代 `Run.Checkpoint`（`assembly/checkpoint.go:327`）＋ `Run.Close`（`assembly/invoke.go:102`），第二代重新 `assemble` 走真实托管 boot。

1. 建址：`Run.CommitToSession`（`assembly/run.go:1937`）落一条窗口事实，`establish` 把该地址开成本代实例的 site。
2.  admit：`registry.Register`（`runtime/task/lifecycle.go:211`）。
3. 铸通知：**生产形状**＝`Notice{TaskID, Round: 任务自身 round, Kind, Seq: 0, Event}`，与 `runtime/task/wrapper.go:302`、`runtime/agent/delegate.go:641` 逐字同形（两处都硬写 `Seq: 0`，不经序列器）。
4. 提交：`registry.Advance`（`runtime/task/lifecycle.go:282`）→ `applyCommitLocked`（:379）→ `runCommit.Commit`（`assembly/commit.go:371`）→ `DurableCommit.Commit`（`runtime/state/durable.go:844`，入表段 :881-887）。
5. 投递：`manager.Flush`（`runtime/task/manager.go:149`）→ `Run.deliver`（`assembly/tools.go:50`）→ `store.AcceptNotice`（`runtime/state/mailbox.go:94`）＋ `observeSettleDelivery`（`assembly/tools.go:89`）。
6. 结清：`store.Claim`（`mailbox.go:243`）＋ `store.Consume`（`mailbox.go:270`）→ `recordConsumption`（`durable.go:713`）删 owed、写 consumed 收据。
7. 落切面＋重启：`Run.Checkpoint` → `Run.Close` → 新代 `assemble`；boot 侧 `seatDurableAccount`（`assembly/checkpoint.go:913`）→ `DurableCommit.Seat`（`durable.go:400`），`restoreCheckpointState`（`assembly/run.go:1078`）→ `mailboxState.RestoreState`（`mailbox_checkpoint.go:60`）→ `owedOfWork`（:75）→ `foldOwed`（`memory.go:425`）→ `AcceptNotice`；`restoreChains`（`run.go:1110`）按 `CheckpointCoverage` 只折 cut 之后的链。
8. 量：**「重启后 mailbox 是否又提供一个账户已持消费收据的四元身份的未消费 seat」**（`store.Unconsumed()`，`memory.go:332`，跨全部 site）。不用「mailbox 条目数 > 1」作判据——已消费条目不进任何切面，跨代计数恒为 1，会把真重复投递量成绿。

### 十三·3 四枚探针与原始退出码

| 探针（`assembly/g2_composition_test.go`） | 触发面 | exit | 命中 | 结果 |
|---|---|---|---|---|
| `TestG2ComposeProductionNoticeSurvivesRestart` | 生产形状终态通知＋公共入口 | 0 | 3 RUN / 3 PASS（`-count=3`） | 无重复投递 |
| `TestG2ComposeNonTerminalReoweThroughThePublicEntry` | **非终端任务经 `registry.Advance` 二次提交已消费身份**（全程公共入口，不用序列器） | 1 | 3 RUN / 3 FAIL（`-count=3`） | **A 单独造成重启后重复投递** |
| `TestG2ComposeForcedReoweAtTheCommitPort` | 同一触发绕过 registry 直接压到 commit 端口（终态任务） | 1 | 3 RUN / 3 FAIL（`-count=3`） | A 单独造成重复投递 |
| `TestG2ComposeSequencerEntryAfterRestart` | 经 `registry.IssueNotice` 铸 progress 通知，真实托管 boot 覆盖后续轮 | 1 | 3 RUN / 3 FAIL（`-count=3`） | B 单独造成重复投递 |

单跑（`-count=1 -v`）退出码同向：prod=0、public=1、forced=1、sequencer=1。既有消费者锚同树复跑 `go test ./assembly/ -run '^TestTaskRestore(BoundedByCheckpoint|FoldsTheTailOfACoveredChain|WithoutCheckpointFoldsEveryChain)$' -count=1 -v` → **exit 0，3 RUN / 3 PASS**（探针与既有锚共存，未污染）。

**决定性原文（公共入口 A 探针，`-count=3` 三次逐字同形）**：

```
gen1 after consumption: owedHolds=false consumed=1
gen1 second Advance through the public entry = (1, <nil>); owedHolds=true
gen1 flush of the re-owed notice = <nil>; unconsumed=0
gen2: mailbox entries=1 owedHolds=false consumed=1 unconsumed seats of an already-settled identity = 1 (want 0)
gen2 mailbox entry: identity=task-390c272fcac2128b9923ce90bdad5af6:1:progress:0 event=g2-pub-fact-2 claim=0 acceptSeq=1
```

即：同一四元身份 `task-390c…:1:progress:0` 在第一代已结清（consumed=1），第二代 mailbox 又以 `claim=0`（pending）提供它一次，事件引用换成 `g2-pub-fact-2`。**第一代内不重复**（`flush … unconsumed=0`）：活着的 `m.accepted` 持首条事件，`noticeSeat`（`mailbox.go:161-173`）判 `ErrNoticeConflict` 拒了第二次投递；重启后 `m.accepted` 不被任何切面重建，该守卫消失——**重启是这条路径的承重条件**。

**生产形状探针的拒绝原文**：

```
A trigger through registry.Advance = (2, task: the task already holds its terminal fact); owedHolds=false
Reactivate after the settled round = (round 2, <nil>)
gen2: mailbox entries for task-6590…:1:settled:0 = 0 (want 1), unconsumed seats = 0, owedHolds=false, consumedReceipts=1
```

`lifecycle.go:304-305` 的 `isTerminal(entry.status)` → `ErrTerminalState` 挡住第二次终态提交；`Reactivate`（`reactivate.go:81`）与 `Resume`（`lifecycle.go:366`）都 `round++`，重入换身份。

**B 探针原文**：`gen2 reissued identity=task-575b…:1:progress:0 event=g2-b-fact-2; settled identity=task-575b…:1:progress:0 event=g2-b-fact-1; sameIdentity=true`，随后 `gen2 mailbox entry: identity=task-575b…:1:progress:0 event=g2-b-fact-2 claim=0 acceptSeq=1`、`unconsumed seats of an already-settled identity = 1`。boot 侧 `coverage=map[task-575b…:2] durableRevision=2`、`restore folded chains=0 links=0 covered=1` 证明确实走覆盖路径而非链折叠。

### 十三·4 归因：2×2 隔离实测

| | A 在场（HEAD） | A 中和（`Commit` 入表段按 consumed/rejected 过滤） |
|---|---|---|
| **B 在场（HEAD）** | forced FAIL、sequencer FAIL、public FAIL | forced **PASS**（`owedHolds=false`，gen2 0 seat）、sequencer **FAIL**（逐字同形）、public **PASS**（`owedHolds=false`，gen2 mailbox 0 条目） |
| **B 中和（`RestoreCheckpoint` 对 `entry.mutations` 的每条信封也 `Observe`）** | forced **FAIL** | sequencer **PASS**（`covered path issues seq=1, folded path issues seq=1`），既有原型 `TestCoveredTaskMustNotReissueASettledNoticeIdentity` 亦转 **PASS**（exit 0，1 RUN） |

结论：**A 单独充分**（B 中和后 forced 仍红；B 全程未参与 public 探针）；**B 单独充分**（A 中和后 sequencer 仍红）。两者无合成关系，R30 的「组合即可达机制」应改写为「两条独立机制」。

### 十三·5 生产可达性：A 的触发今天是偶然被挡，B 的入口生产零调用

- **A**：触发条件是「非终端任务提交一条携已消费身份的新 mutation」。公共入口完全允许（实测 `CommitCommitted`）。今天挡住它的是**生产通知铸造面全为终态转移**：全仓 `Outbox:` 生产构造点只有 `runtime/task/cancel.go:92`（cancelled）、`runtime/task/lifecycle.go:294`（`Advance` 透传），而 `Advance` 的生产调用方只有 `wrapper.go:307`（settled/failed）、`delegate.go:651`（settled/failed/cancelled）、`invoke.go:259`（cancelled，nil outbox）；`RecordStop` 亦终态。终态之后 `Advance` 被 `ErrTerminalState` 拒，重入必 `round++`。**这是关于当前铸造面的事实，不是机制保证**：任何新增的非终态通知（progress/accepted）铸造方一接线，A 立刻成为活路径。按「潜在红非活路径」登记，不按「已安全」登记。
- **B**：`r.seq` 的唯一读点是 `IssueNotice`（`runtime/task/handoff.go:279`），而 `IssueNotice` **生产零调用方**——全仓命中只有测试、`runtime/task/skeleton_assert.go:227` 的编译期断言（自证形态）与 `scripts/codetools/event_ownership_test.go:249` 的测试内替身。生产铸通知硬写 `Seq: 0`、round 取自任务/委托条目，不经序列器。故 B 是**潜在红**：序列器一旦接线即成活路径，今天不是。

### 十三·6 `Consumed` 缺席 mailbox 切面是否承重：**字段缺席不承重，缺的是查询**

消费集在恢复期**确实被重建**，两处：
1. 切面座：`seatTaskLocked`（`runtime/state/durable.go:468-474`）从 `StatedTask.Consumed` 填 `d.consumed`；该字段由 `StateSettleAccount`（`runtime/task/checkpoint.go:160-180`）经 `ReceiptsOf`（`durable.go:1047`）陈述。
2. 按身份读／整份读：`readTaskLocked`（`durable.go:531-542`）与 `load`（`durable.go:296-307`）从 `consumePrefix` 收据键填 `d.consumed`，**都在应用记录之前**（:566-583 / :331-349），故 `applyRecordLocked` 的过滤有据。

实测后果：A 探针第二代 `owedHolds=false`（durable 侧过滤正确），**但 mailbox 仍收到该通知**——载体是切面载荷里的 `Owed`（第一代运行时 owed 集被 A 污染后写进载荷），而 `owedOfWork`（`mailbox_checkpoint.go:75-98`）只按身份去重，**不查已重建的消费集**。

**判别实验**：只给 `owedOfWork` 补一条「跳过账户已持消费收据的身份」的查询（A 原样在场），forced 探针即 **PASS**（`gen2: mailbox entries=0 … unconsumed seats of an already-settled identity = 0`，exit 0）；sequencer 探针仍 **FAIL**（B 不经切面恢复）。故承重的是**切面恢复不查消费集**，不是 `mailboxSliceWire` 少一个 `Consumed` 字段；补字段而不补查询无效，补查询而不补字段即可挡住 A 的这条载体。

### 十三·7 本节未验证项

1. **未跑跨进程夹具**：两代是同进程内两次 `assemble`（各自独立 journal 根、独立实例 ID），不是两个 OS 进程。R28 的跨进程写者隔离（丙′）不在本节范围。
2. **未驱动真实 follow-up turn**：重复投递的量是「mailbox 又提供了已结清身份的未消费 seat」，未跑 `SettleDriver.drain`（`assembly/settle_turn.go:466`）真起第二轮模型 turn。`driveSeat`（:522）在 `attempts.Attempt` 持已落终态时会关收据不起 turn（:542-554），该守卫能否挡住本节量到的 seat **未测**——它依赖 attempt 收据存在，而 `Run.sink` 路径（`assembly/facts.go:923`）的消费不写 attempt。
3. **未枚举全部生产铸造面的未来接线**：§十三·5 的「全为终态」是本轮 grep 枚举所得，未含插件/SDK 侧可能的外部调用方。
4. **未跑全量回归**：只跑 `assembly` 与 `runtime/task` 的选择器；`assembly` 全包、`memory/ledger`、`tests/integration/` 未跑。
5. **G2 整体未判**：本节只裁 A+B 合成这一问，不声称 G2 任务域通过，不声称 G1/G3/G4 已定，不声称产品可发布。05.R1 任务框未勾。

## 十四、锚名落盘勘误（主树，2026-11-16 第二轮）

本节只登记命名与落点；§八—§十三 的判定、实测数字与退出码**一字未改**。

- **改名（旧名→新名）**：`TestG2OwedKeepsTheHoleWhenLaterNoticesComplete`→`TestOwedSetKeepsTheHoleWhenLaterNoticesComplete`；`TestG2SeatedAccountAgreesWithIdentityRead`→`TestSeatedAccountAgreesWithIdentityRead`；`TestG2EffectiveSetIsTheStatedAccountPlusTheTail`→`TestEffectiveTaskSetIsTheStatedAccountPlusTheTail`；`TestG2RepeatCommitOfAStoredMutationReowsNothing`→`TestRepeatCommitOfAStoredMutationReowsNothing`；`TestG2RuntimeCommitMustNotReoweAConsumedNotice`→`TestRuntimeCommitMustNotReoweAConsumedNotice`；`TestG2CoveredTaskMustNotReissueASettledNoticeIdentity`→`TestCoveredTaskMustNotReissueASettledNoticeIdentity`。辅助名 `g2Account`/`g2ReadAccount`/`g2SameAccount`/`g2TailOf`/`g2SeedThreeNotices`/`g2OwedNoticeLog`→`accountFaces`/`facesOf`/`sameFaces`/`tailOf`/`seedThreeNoticeTask`/`taskSliceFactLog`。**本文与 `tasks.md` 中的 `-run` 命令已按新名改写**（旧名会让正则零匹配而静默 exit 0，R29）；**仍带 `TestG2Compose*` 的 §十三·3 探针是窄派单的一次性夹具，未落盘**，不得当作主树可跑命令。
- **落点**：五枚任务账锚并入 `runtime/state/durable_seat_test.go`（同锚既有文件，避免 `responsibility-fragmentation`）；缺口 B 并入镜像 `runtime/task/checkpoint_test.go`；负锚 `TestTaskSliceRefusesToDropAReceiptOfAnOwedNotice`（冻结名未改）落 `runtime/task/task_slice_receipt_anchor_test.go`（外部测试包 `task_test`）。
- **落盘后行为与本节判定一致**：四枚任务账锚绿、缺口 A/B 两枚红（`-count=3` 同因）、负锚红。原始命令与退出码见 `tasks.md` §05.R1 锚落盘。
- **退役相关性（R51）**：本域七枚锚均不断言退位或「五族留热」；答 2 的 (c) 论证里「五族永不搬冷」是被删机制提供的前提，删后平凡为真，**判据与负锚的结论不变**。

## 十五、05.R2 半单执行记录：缺口 A 与 A′ 收口（主树，2026-11-16 第三轮）

**本单范围**：只做 `runtime/state/` 这一半。缺口 B（`runtime/task/checkpoint.go:267-269` 只对未结义务 `Observe`）属另一派发单，本单未触 `runtime/task/`，**故 05.R2 框未勾**。

**源树绑定**：证据取自主工作树（HEAD `9547faca` ＋ 未提交候选），不是隔离树；`go build ./...` 与 `go vet ./runtime/state/` 亦在主树。`runtime/state/durable_seat_test.go` 的 +325 行在开工前已存在（上一轮锚落盘），非本单所写。

### 十五·1 修法：一条判据，一处折叠

- **单一判据**：新增 `settledLocked(noticeKey)`＝`consumed ∪ rejected`。它把原先在三处各写一遍的两条 `if _, ok := map[key]; ok { continue }` 收成一条：`oweLocked`、`applyRecordLocked`、`unowedLocked` 现在都读它。
- **单一折叠**：新增 `oweLocked(record)`，`Commit`（运行期）与 `applyRecordLocked`（恢复期）**同调此函数**——这是「单一 g(x)」的过滤段，不只是入表段。**缺口 A 的实质是过滤没抽出，不是入表没抽出。**
- **A′（查询缺席，不是字段缺席）**：`durableConsumption` 增 `settled(notice)`，`DurableCommit` 以 `settledLocked` 实现；`owedOfWork` 折叠前对**两个来源同样**过滤（切面载荷的 `Owed` 与账户的 `OwedNotices`）。**未给 `mailboxSliceWire` 加 `Consumed` 字段**——消费集由 `seatTaskLocked`/`readTaskLocked`/`load` 在应用记录前重建，缺的是查询，与 §十三·6 判定一致。
- 锁序：`settled` 只在不持有 `MemoryStore.mu` 时调用（`owedOfWork` 在 `foldOwed` 之前完成折叠），故未新增 `DurableCommit.mu → MemoryStore.mu` 的反向边，既有的 `AcceptNotice → recordConsumption` 顺序不变。

### 十五·2 红绿对（原始退出码，`go1.27.1 darwin/arm64`）

| 锚 | 命令 | 修前 | 修后 |
|---|---|---|---|
| A | `go test ./runtime/state/ -run '^TestRuntimeCommitMustNotReoweAConsumedNotice$' -count=1 -v` | **exit 1**（`the runtime commit owes 3 notice(s) while the recovery path owes 2`） | **exit 0**（1 RUN／1 PASS） |
| A′ | `go test ./runtime/state/ -run '^TestMailboxSliceRestoreSkipsANoticeTheAccountSettledPastTheCut$' -count=1 -v` | **exit 1**（`restored seats = [seq1 seq2 seq3], want the one obligation the account still owes`） | **exit 0**（1 RUN／1 PASS） |

- **A′ 是本单新增的锚**（原先只有 A 一枚钉此事），走 `StateRegistry.RestoreState` 这一与 `assembly/checkpoint.go:883` 相同的正式入口，不是 helper 级演示：先按切面陈述（3 条 owed），再在切面之后落下**一条消费收据与一条拒绝**，重启恢复必须只座 1 条。
- **变异负锚（A′ 可失败性实测）**：把 `owedOfWork` 的 `if settled(...) { return }` 降级为丢弃求值（编译通过、行为退回修前），同一命令 **exit 1**，原文同 A′ 修前（3 条座位）；复原后 exit 0。故该锚**不是恒真界**，且钉的是行为不是形态。
- **A 的负锚即其自身第二段**：同一记录在 `Commit` 与 `Seat` 两路必须给出同一 owed 数（修前 3 vs 2 即失败原文），两路都不松。

### 十五·3 稳定性与回归面

| 命令 | exit | 命中 |
|---|---|---|
| 两锚 `-count=10 -v` | 0 | 20 RUN／20 PASS／0 FAIL |
| 两锚 `-race -count=10` | 0 | 无 DATA RACE |
| `go test ./runtime/state/ -count=1 -v` | **0** | **69 RUN（29 顶层＋40 子）／全 PASS／0 FAIL** |
| 修前同包 `go test ./runtime/state/ -count=1` | 1 | **仅** `TestRuntimeCommitMustNotReoweAConsumedNotice` 一枚红，无其他既有用例转红 |
| `go build ./...` | 0 | — |
| `go vet ./runtime/state/` | 0 | — |
| `go run ./scripts/comment_policy -no-baseline runtime/state` | 0 | **0 finding**（`examples/wechat-bot/knowledge_base` 那条是被 git 忽略文件提示，不计数） |

- 注释门首轮曾得 1 finding（`mailbox_checkpoint.go:82` free-standing：函数体内说明不在文档槽），已把理由并入 `owedOfWork` 文档并删掉体内注释，复跑归零。
- **缺口 B 现状（如实，不认领）**：`go test ./runtime/task/ -run '^TestCoveredTaskMustNotReissueASettledNoticeIdentity$' -count=1` **exit 1**，失败原文指向 `runtime/task/checkpoint.go:267-269`（覆盖路径只 `Observe` 未结义务）。该面不在本单白名单，**A 的修复不改变它**——B 与 A 不合成（§十三·1），各自独立。

### 十五·4 本单未验证项

1. **未跑本包之外的任何测试**：`assembly/`、`tests/...`、`memory/ledger/`、`features/...` 均按派发令不跑（并发单正在写这些包，其 Go 结果不可信）。故「运行期 `Commit` 不再为已结通知开账」对下游消费者的影响**未经实测**——这是本单最大的未验证面，须由消费 `runtime/state` 的包在写面释放后复跑。
2. **未做跨进程夹具**：两代恢复仍在同进程内（§十三·7 同）。
3. **未验证真实 follow-up turn**（§十三·7 第 2 项同）：重复投递的量仍是「mailbox 又提供了已结清身份的未消费 seat」。
4. **G2 整体仍未判**；本单只闭合任务域的缺口 A/A′ 两条，不声称 G2 通过，不声称树绿，不声称 CI 通过。

## 十六、05.R2 半单执行记录：缺口 B 收口（主树，2026-11-16）

**叶未勾**：本单闭合缺口 B（覆盖路径与链折叠路径身份序列一致）。05.R2 另含候选甲（按任务键索引，`ReceiptsOf` 由 O(全库) 降为 O(该任务)）与清减面，二者未做；冻结负锚 `TestTaskSliceRefusesToDropAReceiptOfAnOwedNotice` 仍红（其红恰是「清减面缺席」的陈述，属本叶剩余面）。故叶不得勾。

### 十六·1 修法：一条判据，一处来源

`RestoreCheckpoint` 的 `seq.Observe` 来源由「未结义务的信封」改为「切片陈述的每条 mutation 的 outbox 信封」，与折叠路径 `foldLocked`（`for _, envelope := range fact.Outbox`，`restore.go:288-293`）**同源同集**。

为什么旧源是子集：携信封的义务只由两处创建——`applyCommitLocked`（`lifecycle.go:387-395`，先入 `entry.mutations` 再开义务）与 `foldLocked`（`restore.go:281-295`，同序）——故每条携信封的义务背后都有一条已提交 mutation，替换后观察集不缩。

**不变量**：恢复交给 sequencer 的身份集＝账户**已提交的全部通知身份**（task/round/kind/seq），与服务端是否已结清该通知无关；两条恢复路径对同一账户发出同一身份序列，重启后 `IssueNotice` 不复用账户用过的身份。

**未做的事（有意）**：未按 `Consumed`/`Attempts`/`Rejected` 三族收据观察。收据的通知恒出自某条 mutation 的 outbox，三族是 outbox 的子集；以收据为源会把这些身份之外的未结信封漏掉（`TestCoveredTaskKeepsTheIdentityOfANoticeItStillOwes` 钉住这一点）。

### 十六·2 红绿对（原始退出码，`go1.27.1 darwin/arm64`）

| 命令 | exit | 命中 |
|---|---|---|
| `go test ./runtime/task/ -run '^TestCoveredTaskMustNotReissueASettledNoticeIdentity$' -count=1 -v`（修前） | **1** | 1 RUN／1 FAIL（`re-issued the four-part identity …:1:progress:0`） |
| 同命令（修后） | **0** | 1 RUN／1 PASS（`covered path issues seq=1, folded path issues seq=1`） |
| 两锚 `-count=10` | 0 | 20 RUN／20 PASS |
| 两锚 `-race -count=10` | 0 | 无 DATA RACE |
| `go test ./runtime/task/ -count=1 -v` | 1 | 30 顶层／93 RUN：28 PASS／2 FAIL（两枚均为既有故意红，见十六·3） |
| `go test ./runtime/state/ -count=1`（下游） | **0** | ok |
| `go build ./...` / `go vet ./runtime/task/` | 0 / 0 | — |
| `go run ./scripts/comment_policy -no-baseline runtime/task` | 0 | **0 finding** |

### 十六·3 包内剩余两枚红：归属与不处置

- `TestConsumptionFailureIsReportedByName`：12 域 G2-C2「消费失败须具名上报」的冻结负锚（`changes/12-consumer-state-lifecycle/tasks.md:85` 记为故意红），修前同红，非本单引入，不处置。
- `TestTaskSliceRefusesToDropAReceiptOfAnOwedNotice`：本域 §八答 2 的冻结负锚，红在「账户无清减面、丢了收据必重新欠账」；清减面属 05.R2 剩余面，不在本单白名单，不处置。

### 十六·4 新增锚及其鉴别力

`TestCoveredTaskKeepsTheIdentityOfANoticeItStillOwes`：覆盖任务**仍欠**一条通知时同样不得复用其身份。两套代码在常态下都绿，故另做变异实测——把观察集改成「mutation outbox 减去未结义务」后，该锚红（`re-issued the four-part identity …:1:progress:0`）、冻结锚仍绿，证明它钉住「已结＋未结同源」，不是冻结锚的重复。

### 十六·5 本单未验证项

1. **未跑 `./assembly/`、`./tests/...`、`./features/...`、`./memory/ledger/`**（并发单写面，其结果不可信）。故「重启后不再重发已结身份」在真实 boot 入口上的后果未实测，须待写面释放后复跑。
2. **一处同源不对称未处置（如实登记）**：`foldLocked` 会用 `envelope.Notice.Round` 抬高 `entry.round`（`restore.go:294-296`），而运行期 `applyCommitLocked` 不以信封轮次调 `round`。故 `StatedTask.Round` 取运行期值、折叠取 max。按 `IssueNotice` 的构造（发出时的 `Notice.Round` 即 `entry.round`），该 max 在良构链上不触发，本轮未证其可达性，未改。
3. **G2 整体未判**；不声称树绿、不声称 CI 通过、未提交。

## 十七、05.R2 候选甲落地＋清减面判据重核（2026-11-16 第四轮）

**本单范围**：`runtime/state/` 的生产与测试面＋本域 `design.md`/`tasks.md`。**未触 `runtime/task/`**（冻结负锚所在包，不在白名单）。**源树＝主工作树**（HEAD `9547faca` ＋ 未提交候选），`./runtime/state/` 与 `./runtime/task/` 在全树 `go build ./...` exit 0 下实跑；`./assembly/`、`./tests/...`、`./memory/ledger/` 与全仓套件按派发令**未跑**。

### 十七·1 成本前提按 R51 反重重核：结论是「与反转无关，前提成立」

**甲的成本前提**（`ReceiptsOf` 逐任务全表走查）**从不经过 ledger**：`ReceiptsOf` 只读 `d.consumed`/`d.attempts`/`d.rejected` 三张内存表，零日志读。实测夹具用 `Seat` 载荷直接构造 16,384 条收据的账户（全程无 `FactLog` 实现参与），故退位删除、S0 席位、可寻址索引对这一面**没有任何影响**。§八答 2 里「五族永不搬冷」只是**乙（删收据）**的前提，不是甲的前提；R51 删退位后它平凡为真，甲的形态与必要性不变。

**实测（同机 `go1.27.1 darwin/arm64`，每任务 64 条已结收据，无日志参与）**：

| 任务数 | 账户收据数 | `ReceiptsOf` 单次（修前） | 一次切面（逐任务过一遍，修前） | `ReceiptsOf` 单次（修后） | 一次切面（修后） |
|---|---|---|---|---|---|
| 2 | 128 | 20.9µs | 63.5µs | 9.8µs | 21.8µs |
| 16 | 1,024 | 50.5µs | 572µs | 9.2µs | 142µs |
| 64 | 4,096 | 74.0µs | 4.27ms | 7.8µs | 893µs |
| 256 | 16,384 | 168µs | **36.3ms** | 8.5µs | **2.79ms** |

- 修前单次随**账户**增长（128× 收据 → 8×），一次切面是 **571× 对 128× 收据＝二次**；修后单次对 128× 账户**平坦**（9.8→8.5µs），一次切面 128× 对 128× 收据＝**线性**。
- 生产消费者是 `task.StateSettleAccount`（`runtime/task/checkpoint.go:175`，每任务一次），故「每任务 O(该任务)」正是把切面的二次项消掉的那一步。

**反转真正改变的是另一面（推导＋读码，非实测）**：`Seat` 的按身份读（`readTaskLocked` 五前缀）今日成本是 `journalKV.Read(Prefix)` → `journalLog.keys`：**全索引走查＋全量排序**（`memory/ledger/journal.go:2083-2093`），与前缀无关 = O(全部键)。可寻址引擎的 `Keys(prefix)`＝`idx/<ns>/` 目录读 = O(该前缀键数)。故「切片省略收据族、开档按身份读回」这条清减形态的代价由 O(任务数×历史) 降到 O(该任务)——**反转让清减更便宜，不是让它作废**。甲与清减是两件独立的事：甲降的是**每次切面**的陈述成本，清减降的是**每个切面的载荷体积**。

### 十七·2 落地：一条判据，一处归档

- 新增 `receiptsByTask map[event.TaskID]map[noticeKey]struct{}`，键是 notice 四段身份（**不按族分表**：同一身份跨族共槽，族由 `ReceiptsOf` 对三张表直查决定）。
- `holdReceiptLocked` 是**唯一归档入口**，12 处收据写入全经它：`load` 3 处、`seatTaskLocked` 3 处、`readTaskLocked` 3 处、`recordAttempt`、`recordConsumption`、`recordRejection`。
- `ReceiptsOf` 只遍历该任务的键集，三族由直查决定；排序（`noticeBefore`）与「答案是副本」的语义逐字不变。
- 未新增锁边：`holdReceiptLocked` 仅在已持 `d.mu` 的处调用，`ReceiptsOf` 同锁。
- 收据表只增不删（`delete` 只出现在 `d.owed`，读码实证），故索引无需删除路径。
- **被否的替代形状（记取舍，非省事）**：把三张表改成分层 `map[TaskID]map[noticeKey]…` 可消除「表＋索引」的冗余，但会改写 15 处读写点、改变 `settledLocked` 等判据的形态，而 §九 已冻结「补一份按任务键索引」；索引侧的漂移风险已由「唯一归档入口」＋变异实测（5 处覆盖四读路径）压住，故按冻结形落地。

### 十七·3 两枚新锚与鉴别力（落 `durable_seat_test.go`，同锚既有文件）

**落位说明**：新建独立文件触发 `comment_policy` 的 `responsibility-fragmentation`（该目录同锚组内已有一枚无生产镜像的测试文件），故按 R54 的同一处置**并入同锚既有文件**，同时把该组清到 `0 finding`。

`TestReceiptsOfAnswersTheReceiptsOfOneTask`：两条任务 × 三族 × **四条读路径**（运行期写入／整份读／切片座／身份读），逐路径与「整表参考读」`reflect.DeepEqual` 相等，并断言从未入账的任务答零（防空转）。**三族各钉一条不同 notice**（consumed 1/2、attempt 3、rejection 4），否则「别的族已归档同一身份」会掩掉漏归档。

**变异实测（每条删一处归档，逐条转红，修前/修后同命令）**：

| 变异 | 结果 |
|---|---|
| `load` 的 consumed 归档删掉 | exit 1（`the whole-log load answers {Consumed:[] …}`） |
| `seatTaskLocked` 的 refused 归档删掉 | exit 1（`the checkpoint seat answers …`） |
| `readTaskLocked` 的 consumed 归档删掉 | exit 1（`the identity read answers {Consumed:[] …}`） |
| `recordConsumption` 的归档删掉 | exit 1（`the runtime account answers {Consumed:[] …}`） |
| `recordAttempt` 的归档删掉 | exit 1（`the runtime account answers …`） |

`TestReceiptsOfCostsOnlyItsOwnTask`：2 任务 vs 256 任务（128 vs 16,384 条收据）对**同一任务**的单次答案成本比 ≤4×；实测 **7.7µs vs 7.3µs**（×10 复跑两次读数 7.708/8.227µs 与 7.333/8.397µs，比值 ≈1）。**变异（把 `ReceiptsOf` 改回整账户走查）→ 1.80ms vs 19.6µs＝92× 红**，同一 log 里并列实测的「整表参考读」为 136–138µs（≈17×）。该锚是本叶唯一以成本为判据的锚，故按 R29 的三要素写成：正锚＝比值上界、负锚＝整表走查在 4× 界外、消费者＝`StateSettleAccount` 的逐任务调用。

### 十七·4 冻结负锚 `TestTaskSliceRefusesToDropAReceiptOfAnOwedNotice`：**判据需重定，本单未改，待编排者裁定**

**修前修后同命令（主树）**：`go test ./runtime/task/ -run '^TestTaskSliceRefusesToDropAReceiptOfAnOwedNotice$' -count=1 -v` → **exit 1**，原文 `the reduced slice re-owed the notice task-owed-receipt:1:progress:1 whose consumption receipt it dropped`。甲的落地**不改变它**（该锚只走 `Seat` 与载荷，不读 `ReceiptsOf` 的成本面）。

**两处隔离树实测（`/tmp/recovery-05rest/tree`，rsync 工作树、排除 `.git`/`examples`/`docs`）**：

1. **只把变异字面量由 `[]` 改成 `nil`，生产代码一字不改 → exit 0（绿）**，并打出 `a task slice that states no settle account … reading its chain instead`。即「切片省略收据族、开档按身份读回」这条清减路径**今日已存在**（`Seat` 的 nil 分支 → `readTaskLocked`），锚红只因它把族**陈述为空集**（`Consumed: []`）。而按 1.1 冻结的设计（`StatedTask.Consumed` 的 `json:"consumed"` 无 omitempty、空集落 `[]`），`[]` 的语义恰是**权威的「本任务没有收据」**，与「未陈述」严格区分——§九的反事实说明去掉这一区分会让升级后首启把已消费通知重新欠账。故该锚实际断言的是「载荷谎报一个权威空集，账户必须识破」，而不是「清减」。
2. **反过来让 `Seat` 忽略载荷三族、对每个覆盖任务按身份读回 → 锚转绿 exit 0，但三枚既有绿锚同时转红**：
   - `TestDurableLoadReadsTheTaskSlice`：`the seat read the prefix "tagent/task/v1/spec:task-seat-covered" of a task the checkpoint covers, want no durable prefix read`
   - `TestDurableLoadRefusesAMismatchedReceiptIdentity`：`the seat of a refusal stated under another task = <nil>, want the mismatch named`
   - `TestSeatedAccountAgreesWithIdentityRead`：`the seat of a covered task read 5 prefix(es), want none`

即两条判据**互斥**：冻结负锚要的「载荷不作权威」，正是 1.1 两枚冻结锚与 G2-T2（R30 已判 PASS）所禁的。**故本单不自行改写该锚**（它在 `runtime/task/`，且改写冻结锚属编排者权限；同 R61 #1 的形态：锚面与其设计自相矛盾）。

**提议的不变量（供编排者裁定，未写入任何 spec）**：
- **A（载荷权威）**：切面对覆盖任务的收据族是权威陈述；`[]`＝「该任务无收据」，MUST NOT 被读作「未陈述」。
- **B（唯一安全的清减形态）**：清减 MUST 省略族（`nil`），MUST NOT 以空集陈述；被省略的任务由自身 durable 前缀按身份读回、成本 O(该任务)，并具名 WARN。**按此重新表达，锚在今日树上即为绿**（实测 1），且与三枚绿锚不冲突。
- **C（成本面）**：`ReceiptsOf`＝O(该任务)，判据取「同一任务答案成本对账户规模的增长轴」，已由本单落地。

若编排者采纳，锚的改法与判据（建议名 `TestTaskSliceReadsBackTheReceiptItDoesNotState`）：变异改为把三族置 `nil`；正锚＝不重欠且 `ReceiptsOf` 答回该收据；负锚＝同夹具下把族陈述为 `[]` 时账户**信任**它（陈述空集与省略不可互换，正是 1.1 冻结的那条区分）。**归属**：该文件不在本单写面。

### 十七·4 勘误（R68 裁定后落地，主树第五轮，2026-11-16）

**编排者裁定**（`ORCHESTRATION.md` R68）：采纳本节提议的不变量并授权改该锚与其夹具文件（`runtime/task/task_slice_receipt_anchor_test.go`，原在本单写面外）。冻结三条——**A 载荷权威**；**B 清减只能「省略」族、不能「陈述为空集」**；**C 成本面已落地**。关键区分跨域一致：**`nil`＝未陈述（须从链派生）；`empty`＝陈述为无（权威）**，与 S0 座位尾谓词「找不到⇒nil，永不为空集」（R66）是同一区分，非本域特设。

**旧判据为何没牙（两向隔离实验，修前主树实测）**：旧锚 `TestTaskSliceRefusesToDropAReceiptOfAnOwedNotice` 断言「把一条已结收据从载荷清掉后，座后不得重欠」，而它的「清掉」写成 `stated.Tasks[0].Consumed = []task.StatedConsumption{}`。
- **向一（夹具可平凡满足）**：只把该字面量 `[]` 改 `nil`、生产零改动 → 锚 **exit 0**。该锚只能被夹具编辑关掉，钉不住任何生产行为（与 R24 N11／R54 的锚病同类）。
- **向二（两判据互斥）**：让 `Seat` 忽略载荷三族 → 该锚 exit 0，但 `TestDurableLoadReadsTheTaskSlice`、`TestDurableLoadRefusesAMismatchedReceiptIdentity`、`TestSeatedAccountAgreesWithIdentityRead` 同时转红（原文 `the seat read the prefix "tagent/task/v1/spec:task-seat-covered" of a task the checkpoint covers, want no durable prefix read`）。
- **根因**：旧锚要求的「陈述空集后仍不得重欠」正是 **A 的反面**。§八·答 2 与 `runtime/task/checkpoint.go:75-86` 冻结的语义里，`[]` 就是「本任务没有收据」这条权威陈述，与「未陈述」严格区分；把这条陈述当缺陷，与三枚绿锚所钉的「覆盖任务零前缀读」直接冲突。

**重定后的判据（两锚同夹具，只差一族→`nil` 或 `[]`）**：
- 正锚 `TestTaskSliceReadsBackTheReceiptItDoesNotState`：生产者陈述 1 条消费收据 → 载荷**省略**该族（`nil`）→ 座后**不得重欠**该通知，且 `ReceiptsOf` 必须答回**同一条**收据身份与事件；**负向控制**＝同一载荷换一份不持有该收据的链 → 该通知**必须**重欠（读回取自链，不取自载荷）。
- 负锚 `TestTaskSliceTrustsAStatedEmptyReceiptSet`：同一夹具把该族**陈述为空集**（`[]`）→ 账户**信任陈述**：零前缀读、零消费收据、该通知重新欠账。陈述空集与省略因此不可互换。
- 消费者锚：`StateSettleAccount`（`runtime/task/checkpoint.go:160`）为生产者，`DurableCommit.Seat`（`runtime/state/durable.go:451`）为恢复；同一生产者调用内先断言「持有收据者必须被陈述为该收据、永不陈述为空族」（B 的生产者侧）。
- **生产改动：零**（`Seat` 的 nil 分支 `durable.go:477-481` 与 `readTaskLocked` 已具备），只改测试文件——与派发令「成本面已落地，倾向零生产改动」一致。

**红绿与门禁（主树 `go1.27.1 darwin/arm64`，原始退出码）**：

| 命令 | exit | 命中 |
|---|---|---|
| 两新锚 `-run '^(TestTaskSliceReadsBackTheReceiptItDoesNotState\|TestTaskSliceTrustsAStatedEmptyReceiptSet)$' -count=1` | **0** | 2 RUN／2 PASS |
| 同上 `-count=10 -v` | **0** | 20 RUN／20 PASS／0 FAIL |
| 三枚既有绿锚 `./runtime/state/` 同名正则 `-count=1` | **0** | 3 PASS |
| `go test ./runtime/state/ -count=1 -v` | **0** | **71 RUN（31 顶层＋40 子）／0 FAIL**，与修前逐项相同 |
| `go test ./runtime/task/ -count=1 -v` | 1 | **94 RUN／30 顶层 PASS＋63 子 PASS／1 FAIL**：仅 `TestConsumptionFailureIsReportedByName`（12 域 G2-C2 既有故意红，`12/tasks.md:85`）；旧锚造成的第二枚红已消除（修前 93 RUN／2 FAIL） |
| `go build ./...` / `go vet ./runtime/state/ ./runtime/task/` | 0 / 0 | — |
| `go run ./scripts/comment_policy -no-baseline runtime/state runtime/task` | 0 | **0 finding**（修前 0，未退化） |

**变异矩阵（隔离树 `/tmp/recovery-05anchor/tree`，每变异只改一处；复原后同命令 exit 0 复核）**：

| 变异 | 改哪 | 正锚 | 负锚 |
|---|---|---|---|
| M1 生产把**省略**当**陈述空**（`Seat` 以空三族入座，不读链） | `durable.go:477-481` | **RED**（`re-owed the notice task-owed-receipt:1:progress:1`） | green |
| M2 生产把**陈述空**当**省略**（判据 `nil` 改 `len()==0`） | `durable.go:477` | green | **RED**（`answers 1 receipt(s), want the 0 the statement makes`） |
| M3 夹具把负锚的 `[]` 改 `nil`（**旧把戏原样**） | 测试夹具 | green | **RED** |
| M4 夹具把正锚的 `nil` 改 `[]` | 测试夹具 | **RED** | green |
| M5 生产 `readTaskLocked` 丢弃扫到的消费收据 | `durable.go:588-601` | **RED** | green |

**结论**：M3／M4 证明**任何单一夹具编辑都只关得掉半边**（旧把戏不再构成满足），M1／M2 证明「省略」与「陈述空」的两种混淆各被一枚锚抓住，M5 证明正锚的读回确实取自链而非载荷。

**零残留自查**：本次改动（测试文件＋本节＋`tasks.md` 记录）`grep 'Retire|retire|cold|Cold|keptHot'` **零命中**；未引入依赖退位或冷层的判据、字段或分支。

**未验证项**：`./assembly/`、`./tests/...`、`./features/...`、`./memory/ledger/` 未跑（并发单写面）；05.R2 任务行仍缺 `./assembly/` 正锚（`TestTaskAccountStatedSizeTracksLiveWork` 未建立）与三个 `runtime/state/` 拟新增锚，故 **05.R2 不勾**；G2 整体未判，树未绿，未提交。

### 十七·5 本单红绿与门禁（原始退出码）

| 命令 | exit | 命中 |
|---|---|---|
| `go build ./...` | **0** | — |
| `go vet ./runtime/state/` | **0** | — |
| `go test ./runtime/state/ -count=1 -v` | **0** | **71 RUN（31 顶层＋40 子）／全 PASS／0 FAIL** |
| 两枚新锚 `-count=10` | **0** | 20 RUN／20 PASS |
| `go test ./runtime/state/ -count=1 -race` | **0** | 无 DATA RACE |
| `go test ./runtime/task/ -count=1 -v`（下游） | 1 | 93 RUN／28 顶层 PASS／**2 FAIL**：`TestConsumptionFailureIsReportedByName`（12 域 G2-C2 既有故意红）、`TestTaskSliceRefusesToDropAReceiptOfAnOwedNotice`（本节·4）——**与本单修前逐条相同，无新增红** |
| `go run ./scripts/comment_policy -no-baseline runtime/state` | 0 | **0 finding**（并入同锚文件后，该组原有 1 finding 一并清零） |

**零残留自查**：本单 diff 内 `Retire|retire|cold|Cold|keptHot` **零命中**；未引入任何依赖退位或冷层的判据、字段或分支。

### 十七·6 本单未验证项

1. **未跑 `./assembly/`、`./tests/...`、`./features/...`、`./memory/ledger/`**（并发单写面），故「切面陈述成本线性化」在真实 boot/检查点的后果未实测；甲的语义等价已在包内四条读路径上实测，但生产接线未跑。
2. **未改 `runtime/task/`**：冻结负锚的判据待裁（·4），故 **05.R2 不勾**。（**R68 后已解**：判据已重定并落地（`§十七·4 勘误`）；05.R2 今日不勾的真实原因是 `./assembly/` 正锚 `TestTaskAccountStatedSizeTracksLiveWork` 与三个 `runtime/state/` 拟新增锚仍未建立，且 `./assembly/` 本窗口禁跑。）
3. **成本锚是墙钟比值**（取多轮最小值，4× 界；修前 8×／变异 92×，修后 ≈1×）。它不是物理界，只是增长轴的判别；机器极慢或极快都不改变比值方向，但并发负载下仍可能抖动。
4. **G2 整体未判**；不声称树绿、不声称 CI 通过、未提交。

## 十八、05.R3 阻塞记录（主树，2026-11-16）：冻结谓词缺「引擎全键序走查」接缝

**判决：本叶在派发令的写面内不可实施；主树零 `.go` 写入，05.R3 不勾。** 缺的不是判据（S0 已冻结谓词、三个负锚已实测），而是**谓词的取数接缝**：按 order 新→旧收集键必须走**引擎全键序**，该能力在 `memory/ledger` 内只有**未导出**形态，而 `memory/ledger` 本窗口由并发单持有、派发令明禁。

### 十八·1 接缝清单（`grep` 逐条实证，主树）

| 符号 | 位置 | 导出 | 是否够用 |
|---|---|---|---|
| `journalAddress.eachNewestFirst(visit func(key string, span) (bool, error))` | journal.go:2095 | **否** | 形正是所需（order 文件逆序、全键、含原文位置），但包外不可达 |
| `journalLog.eachKeyNewestFirst(visit func(key string))` | journal.go:2419 | **否** | 全键序在内存 order 上，够用，但包外不可达 |
| `journalKV.eachCommittedNewestFirst(ctx, visit func(key string))` | journal_kv.go:291 | **否** | 同上，且由内部接口 `commitOrder`（segment_store.go:261）在包内消费 |
| `journalKV.TailKeys()` | journal_kv.go:123 | 是 | 是**退位切面**的尾，08.R3 删除后消失，正是本叶要脱开之物 |
| `FileSegmentStore.EachIndexNewestFirst` | segment_store.go:1565 | 是 | **不够**：只走 store 事件索引 |
| `AtomicStore`（引擎接口） | atomic_store.go:54-65 | 是 | 只有 `Read`/`Keys`/`Namespaces`/`Commit`，**无任何序** |

`EachIndexNewestFirst` 不够的机理（读码实证，非推断）：`s.order` 的每一枚都是**事件键**——只在 `publishEventLocked`（segment_store.go:709）追加，退位趟只按 `keepKeys` 重排既有事件键（:1160-1171），而 `commitOrderOf` 把引擎全键经 `segmentIndexEntry`（:565-577）过滤成事件族；`TierFacts` 走 `tier.Segments.kv.Commit`（tier_facts.go:47）的**raw** 提交，落在引擎 order 与 value 区，**不进 store 索引**。这正是 06/design.md §十九·五「跨分区」一节写下的前提：尾必须在**引擎 order 全键**上定义。

### 十八·2 探针实测（主树，`go test -overlay`，主树零落盘）

探针经 `-overlay` 注入 `assembly`（源码在 `/tmp/recovery-05r3/probe_test.go`，仓内无此文件）：开 `StorageJournal` 档、经 `state.NewEmptyDurableCommit(ledger.TierFactLog(tier))` 提交两枚任务记录，再问两处。

| 命令 | exit | 读数 |
|---|---|---|
| `go test ./assembly/ -overlay /tmp/recovery-05r3/overlay.json -run '^TestProbeDurableKeysAreInvisibleToTheIndexWalk$' -count=1 -v` | **0** | `durable_keys=2 index_walk_entries=0 durable_keys_seen_by_the_walk=0`；两键为 `tagent/task/v1/fact:taskProbeA\x00taskProbeA:0`、`…taskProbeB…` |

即：引擎持有 2 枚持久任务键，而 `EachIndexNewestFirst` 一条都不命名。若按它实现尾，**尾集恒空**——空非 nil 尾正是本叶要防的静默漏座形态（S0 实测水位尾命名 **0/3** 年轻任务）。

### 十八·3 为什么不能在本叶写面内绕开（三条，逐条排除）

1. **不能用 `Keys`+`Read` 补**：二者按**键序**答（journal.go:2515 `sort.Strings`），无提交序；「键集差分」代替「序」在语义上不成立——检查点已陈述的任务其后仍可能有新记录，差分既不命名它，也无从判断是否该读。
2. **不能把空尾改 `nil`**（`tailFactKeys` 内加一层）：连接点在检查点即最新提交的常态下尾**真**为空，改 `nil` 会让每次开档退化为整份读，直接反red S2 的 `durableLoadCount` 有界锚；且违反 R68「`nil`＝未陈述、`empty`＝陈述为无」与 06/design.md:584「不可改为常空」。
3. **不能在 assembly 声明接口让引擎「顺带满足」**：Go 的导出方法集不含未导出方法，`journalKV` 今天**不满足**任何形如 `EachCommittedNewestFirst(ctx, …)` 的导出接口；测试替身能满足、真实引擎不能——那正是 R14「mock 冒充路径」。

### 十八·4 需编排者裁定的设计分叉（R22 共享面，本叶无权定）

S0 内部对「识别归谁」有两处相反表述，取哪边决定接缝落在哪一侧：

- **§十九·一（推导节）**：`order` 内联推导被否，理由写明「**引擎不得解释 idx 语义**」。
- **§十九·五（精确谓词）**：识别靠**所载** `type==TypeCheckpoint ∧ session==""`，原型在引擎侧逐条目读携带值——即引擎解释 idx 语义。

两案与代价：

| 案 | 改动面 | 形态 | 代价 |
|---|---|---|---|
| **甲（采纳，R80 收紧）** | **`AtomicStore` 接口新增**一条导出接缝：`EachCommittedNewestFirst(ctx context.Context, visit func(key string, value []byte) (bool, error)) error`；9 个实现方逐一落地（生产 3＋测试替身 6，清单见 §十八·7），无提交序的引擎按 `AtomicBatch`／`ErrBatchNotAtomic` 先例具名拒绝＋能力声明；识别与尾集合留在 `assembly.tailFactKeys` | 引擎只答「序＋原文」，语义解释不越界，合 §十九·一；落统一接口而非单引擎，兑现「switching engines needs no adaptation」 | 尾走查每键一次原文读（§十九·五 已声明该成本）；9 实现方改动面 |
| 乙 | **repurpose `journalKV.TailKeys`**（06/design.md:583 原计划） | 引擎内解释 idx 语义 | 与 §十九·一 冲突；且 `TailKeys` 被 4 处 ledger 锚钉在切面语义上（`journal_index_cut_test.go:184`、`retirement_snapshot_test.go:262`、`segment_store_test.go:713/:776`），repurpose 即令它们转红，须同批重述 |

甲案下 `tailFactKeys` 的签名仍须收 `ctx`（`seatDurableAccount` 已有）。**nil/empty 三态按 R80 冻结（勘误：本节原写「永不返回空集」，与 §十八·3 第 2 条「检查点即最新提交时尾真为空」自相矛盾，现改写一致）**：走查未找到框架检查点 ⇒ `nil`（未陈述，整份读兜底）；引擎不声明序能力 ⇒ `nil` ＋具名退化；找到框架检查点且无更新任务事实 ⇒ **empty 非 nil**（陈述为无，权威，零年轻任务入座，读有界）。三态判据与理由见 §十八·7。

### 十八·5 本叶未建立项（不勾的理由，逐条可核）

三枚冻结锚**全部缺席**（`grep` 主树零命中：`TestSeatTailNamesEveryTaskYoungerThanTheNewestFrameworkCheckpoint`／`TestASpellingBasedStopDropsTasksBehindASessionCheckpoint`／`TestAnEmptyNonNilTailMustNotSeatZeroYoungTasks`）。建立它们必须先有甲案接缝：

- 正锚与拼写负锚都要在**真实引擎**上构造「多任务多会话 + 框架检查点被会话检查点越过」的夹具，替身引擎会让否决点失效（R14）。
- 第三枚在 `runtime/state` 侧无法独立成立：`Seat` 收的是尾**参数**，空非 nil 尾是否漏座取决于尾的**来源**，而来源在 assembly/引擎；在状态层写它只能断言「空尾即零年轻任务入座」——那是把陷阱钉成期望，属削弱锚（明禁）。

### 十八·6 本单未验证项（如实）

`./memory/ledger/`、`./tests/...`、`./features/...` 未跑（并发单写面）；甲案接缝未实施故无红绿对；G2、G5、G8 未判；树未绿；未提交；未触发 CI。

### 十八·7 编排者裁定（R80，2026-11-16）：甲采纳，乙排除，三处收紧冻结

**乙不是真分叉**，两条独立冻结理由各自充分排除，故编排者独立裁不上呈：①§十九·一「引擎不得解释 idx 语义」；②`TailKeys` 是退位切面的尾，08.R3 整体删除退位，把新语义钉在待删符号上是建在沙上。

**① 接缝落 `AtomicStore` 接口，不是只落 `journalKV`。** 只导出单引擎方法会迫使 assembly 做引擎特定类型断言，违 `AtomicStore` docstring 明承诺「switching engines needs no adaptation」与「可换引擎」判据。**实现方 9 个（grep 实证）**：

| 类 | 符号 | 位置 |
|---|---|---|
| 生产 | `memKV` | config.go:429 |
| 生产 | `rustvikingKV` | rustviking_kv.go:150 |
| 生产 | `journalKV` | journal_kv.go:180 |
| 替身 | `seatReads` | segment_store_test.go:1017 |
| 替身 | `faultStore` | atomic_store_test.go:90 |
| 替身 | `countingKV` | kv_original_store_test.go:61 |
| 替身 | `filteredKV` | kv_original_store_test.go:152 |
| 替身 | `orderedStore` | kv_original_store_test.go:222 |
| 替身 | `factProbeEngine` | fact_commit_batch_test.go:31 |

无提交序的引擎按既有先例 `StoreCapabilities.AtomicBatch`／`ErrBatchNotAtomic` 办理：**具名拒绝＋能力声明**，不得假实现（R14）。此非「为单使用方造抽象」——导出是既有统一契约的兑现，替代形态违冻结承诺。

**② visit 必带 value。** 签名冻结 `EachCommittedNewestFirst(ctx context.Context, visit func(key string, value []byte) (bool, error)) error`。识别靠 typed `type==TypeCheckpoint ∧ session==""`；**键前缀判据被无前缀门与冻结裁决禁**。故「只给键、让 assembly 按需 Read」不可行。成本＝走查到最新框架检查点的距离（节奏），非历史，合 S2 有界。

**③ nil/empty 三态**（见 §十八·4 勘误）：未找到框架检查点 ⇒ `nil`；引擎不声明序 ⇒ `nil`＋具名退化（写入 spec 具名操作表，不得静默）；找到且无更新任务事实 ⇒ **empty 非 nil**。混淆第二、三态即同时破 S2 有界锚与漏座负锚。

**S0 矛盾裁定**：§十九·一 governs；§十九·五 的引擎侧逐条目读携带值是**原型**形态，生产把识别移 `assembly.tailFactKeys`，引擎只答「序＋原文」。06/design.md §十九·五 勘误待该文件释放后由编排者写。

**硬序**：06.R5 → V2（journal 面词汇归正，纯文本）→ 05.R3-甲（接缝＋谓词＋三锚）→ 独立验证单（`bash scripts/lint.sh` 全门）。05.R3-甲 与 V2 同包、构建闭包相交，**不得并发**。三枚冻结锚名不变：`TestSeatTailNamesEveryTaskYoungerThanTheNewestFrameworkCheckpoint`／`TestASpellingBasedStopDropsTasksBehindASessionCheckpoint`／`TestAnEmptyNonNilTailMustNotSeatZeroYoungTasks`。
