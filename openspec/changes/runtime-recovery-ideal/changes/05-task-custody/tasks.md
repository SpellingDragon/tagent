# 05 任务托管收编 · 孙任务

> **当前承接（R265）**：任务存档筛选、先任务账后信箱的恢复顺序、旧任务按需读回已有实现；下方1.x/旧域号及“只规划”段属于历史阶段，不据此判尚未实施。当前05.R1/R2/R3及原1.4验收仍待核销；重复生成与收据未加载疑点由05.R4联同12.R2承接，不等待12.R3。旧三锚/提交顺序能力已存在，历史“无能力/无测试”声称不作当前结论。

> verify 三要素随任务行冻结（R29）：正锚点名测试＋可复跑命令（测试名与 `-run` 正则逐字符一致）、负锚具名错误路径、消费者锚正式入口实测。**与 02-bounded-open-index / 04-acceptance-real-measure 合单原子（W1 boot 路径簇，一级 D4）**，不并行派发；1.3 的年轻链发现消费 02 的尾键集，1.4 的真量口径与 04 的 `durableLoadCount` 同源（`LoadStats`，runtime/state/durable.go:194-215）。S2 为共享症状锚（一级 design §六），本域只消费不重定义。

- [x] 1.1 扩携 tasks 切片三族收据（`StatedTask` 追加 consumed/attempts/rejected，**不用 omitempty**、空集落 `[]`；wire 类型声明于 `runtime/task`（state→task 单向依赖），`tasksStateSlice` 同时持 registry 与提交账户并按任务陈述三族；`RestoreCheckpoint`/`record` 校验收据身份归属，沿用 mutation 身份校验先例 checkpoint.go:249-252）—— 验证：正锚 `go test ./runtime/task/... -run TestTaskSliceStatesTheSettleAccount`（切片 round-trip 后三族逐项等于账户所陈，空集显式为 `[]` 而非缺键）；负锚 `go test ./runtime/task/... -run TestTaskSliceRefusesAReceiptOfAnotherTask`（收据任务身份与条目不符 → 具名拒绝，不静默丢收据）；消费者锚 `go test ./assembly/... -run TestCloseFilesTheCheckpointTheNextBootReads`（真实 boot 入口：close 落链的扩携切片被下一次 boot 读回，assembly/checkpoint_test.go:932）。—— **待复验（本轮）**：托管面在树内；但「按任务陈述」的计费不成立（`ReceiptsOf` 逐任务遍历全库 map，design E05-2），本条的锚须在 05.R2 后复跑仍绿。
- [x] 1.2 改 `DurableCommit.load` 读切片（抽出 `Commit` 入表段 durable.go:600-609 为同一记录应用函数，覆盖段/尾段/运行期三处共用；删 load 独立派生循环 :317-341；覆盖段零五前缀读，尾段按任务身份前缀 `factPrefix+taskID+"\x00"` 读；欠账恒＝outbox−consumed−rejected，由 `unowedLocked` :768 与删欠账 :466/:508 统一维护；既有 `TestLoadScanCounts` 保留为兜底读面的锚，未改断言）—— 验证：正锚 `go test ./runtime/state/... -run TestDurableLoadReadsTheTaskSlice`（覆盖段 `LoadStats` 无 factPrefix 全扫条目，revisions/facts/mutations/specs/owed/consumed/attempts/rejected 与全扫基线逐项相等）；负锚 `go test ./runtime/state/... -run TestDurableLoadRefusesAMismatchedReceiptIdentity`（切片收据身份与任务不符 → 具名拒绝入表，不按空账继续）；消费者锚 `go test ./assembly/... -run TestTaskRestoreBoundedByCheckpoint`（真实 boot：`durable.Records` 对覆盖任务仍答其链，assembly/task_checkpoint_test.go:176；折叠日志 chains/links/covered 口径不变）。—— **待复验（本轮）**：读切片路径仍在，但清减一旦改变切片陈述面（05.R2），本条的「切片 vs 全扫基线逐项相等」必须重跑；判重身份保留边界未定前不得改断言。
- [x] 1.3 后移 load 时序（`NewDurableCommit` 只构造空表 durable.go:226-249；账户入表移到 `restoreCheckpoint` 解出切片之后、`r.states.RestoreState` 之前，assembly/checkpoint.go:780-798；mailbox 注册顺序不动，`owedOfWork` 读到的欠账含覆盖段＋尾段；年轻链身份消费 02 尾键集，无 checkpoint（Offset=0）与旧切片无三族键走全链读兜底并记一行可检索日志；尾段读失败具名拒绝 boot）—— 验证：正锚 `go test ./assembly/... -run TestBootSeatsTheMailboxAfterTheDurableAccount`（切面后提交的未消费通知重启后仍欠账可投递，mailbox 座与实例按冻结路由开）；负锚 `go test ./assembly/... -run 'TestTaskRestoreWithoutCheckpointFoldsEveryChain|TestBootRefusesAnUnreadableCheckpoint'`（无 checkpoint 兜底全读仍逐链恢复 :261；坏 checkpoint 具名拒绝 boot :432，二者既有须保持绿）；消费者锚 `go test ./assembly/... -run TestTaskRestoreFoldsTheTailOfACoveredChain`（真实 boot 只折覆盖链尾段、账站在尾段状态 :206）＋ `go test ./assembly/... -run TestTaskTailFoldLeavesNoSweepStamp`（清扫语义不因链集改变而漂移 :308）。—— **待复验（本轮）**：时序与兜底仍在；12.R2 改变切面水位语义后，本条「尾段/无 checkpoint 兜底」的锚须重跑，确认兜底不被「应用水位」改写。
- [ ] 1.4 立任务域症状锚与真量消费者锚（多代＋多任务夹具：历史已终态任务数递增、存活账不变；量 `LoadStats` 的键计数与字节计数（真量，非正文计数代理量），分母取索引规模/历史任务数；W0 先写就且红，修复转绿，完整回滚必红，两次证据入 run-notes）—— 验证：正锚 `go test ./assembly/... -run TestSymptomAnchorTaskLoadCostFlatAcrossHistory`（load 键/字节计数跨代平坦且分母已增长；修复前复跑必红）；负锚 `go test ./runtime/state/... -run TestLoadScanCountsRefusedScanRecordsNothing`（拒读错误路径不记度量，既有 :1208 保持绿）＋回滚 1.2/1.3 后复跑正锚必红；消费者锚 `go test ./assembly/... -run TestSymptomAnchorBootCostFlatAcrossGenerations`（S2 共享症状锚，assembly/symptom_anchor_recovery_test.go，W0 已立红、04 精化四真量，本域消费其 `durableLoadCount` 转绿；既有 checkpoint_test.go:1231 同名 Session 锚保留不动）。

> **1.4 现状与本轮接管**：该行仍为未完成，`TestSymptomAnchorTaskLoadCostFlatAcrossHistory` **尚未建立**（本轮 grep 无此函数）；其 `durableLoadCount` 在既有夹具恒 0 的原因已记于下方实施记录（夹具把 checkpoint 写在最后，账户被整份覆盖）。本轮**不撤回**该验收面，改由 **05.R1**（判据 + 可判定的夹具要求）与 **05.R2**（清减后重立平坦锚）承接；原断言面「load 键/字节计数跨代平坦且分母已增长」原样保留，只换夹具驱动路径（同类先例：W0 的 S2 锚改驱动路径不改判据）。

## 实施记录（W3 05+06+07 合单，2026-11-15）

- **1.2/1.3 落地形状**：`DurableCommit` 增 `Seat(ctx, stated []byte, tail map[string]struct{})`（切片覆盖段由条目入表、尾段按任务身份读、无切片/无切面整份读兜底）与 `applyRecordLocked`（运行期 `Commit`、覆盖段、尾段共用同一记录应用路径）。boot 增 `NewEmptyDurableCommit` + `seatDurableAccount`：账户入表在 `restoreCheckpoint` 解出切片之后、`r.states.RestoreState` **之前**（mailbox 读欠账）；无 checkpoint 时在 `RestoreEmptyState` 之前整份读。
- **偏离（记入战报 ④）**：`NewDurableCommit` **保留**构造即全量 load 的语义（未按 1.3 原文改成「只构造空表」）：调用方 30 余处（runtime/state 与 assembly 既有测试、非 boot 消费者）持有 no checkpoint，全量读是它们的合法读面。boot 走 `NewEmptyDurableCommit` + `Seat`，语义（账户入表先于全部切片恢复、覆盖段零前缀读）完全达成。
- **1.4 未勾**：点名的 `TestSymptomAnchorTaskLoadCostFlatAcrossHistory` 未新建。`durableLoadCount` 在本夹具恒 0（该夹具把 checkpoint 写在最后，账户被整份覆盖），故任务域「平坦」的正锚须换夹具，未完成；共享锚 S2 已转绿（`durableLoadCount=[0 0 0 0 0]` 在界内），尾段读的正锚由 1.3 的 `TestBootSeatsTheMailboxAfterTheDurableAccount`（按身份读到切面后的任务）与本域 `TestDurableLoadReadsTheTaskSlice` 承载。

---

## 本轮补差（R265）

- [ ] **05.R4 未加载历史收据与重复生成的联合闭环**。产出：辨明已结任务未预装时，旧信箱载荷如何判断已消费；先复现R10197两次生成的真实切点，证明或驳回`DurableCommit.settled()`只查内存是该现场根因，不从源码疑点直接裁修法。前置：11事实结果与12.R2共享失败语义；不依赖12.R3。写面：`runtime/state/durable.go`、`runtime/state/durable_test.go`、`assembly/checkpoint_test.go`、`examples/wechat-bot/smoke_test.go`、`examples/wechat-bot/smoke_harness_test.go`；与12/04相交时同单或串行，不新增第二份已消费真相。正/负锚（拟新增、未运行）：`go test ./runtime/state -run '^(TestUnloadedSettledReceiptStaysSettled|TestUnloadedOwedReceiptRemainsOwed)$' -count=10 -race -v`，按身份读取历史仍已结、真正未结仍欠账。消费者锚归12.R2的`TestRedeliveredNoticeAfterRestartSettlesOnce`，同一测试单一拥有者；实际重启后再次到达不产生第二收据或第二结算轮。原1.4、05.R2体积/加载量正负锚同时保持，不靠恢复预装全历史换取正确性。最终由独立审者核切点、数据来源与最终候选证据。

# 本轮重整孙任务（05.R1 / 05.R2）

> **阶段状态**：只规划、不实施。下列行登记验收面与实施期文件白名单；标「拟新增、尚未建立」的测试今天不存在，不得写作 passed。
> verify 三要素随任务行冻结（R29）；**前置链**：05.R1 → G2（与 07.R1、12.R1 共用判据表）→ 05.R2（另依 11.R2 的事实提交结果）。共同门：每叶结账前 `go build ./...` exit 0（build/test 与 commit 永不串联），`go test` 命令 exit 0 且命中用例数 > 0。

- [ ] **05.R1 设计门 G2（任务域）：有效集 / 判重身份保留边界 / 乱序完成 / 与 11 的衔接** —— 交付（全部落本域 design.md，不新增文件）：§八 四问逐条给出「判据 + 反例 + 可复跑命令」，产出与 07.R1/12.R1 合并的**一张**判据表；实施期文件白名单（候选）：只写 `assembly/task_account_size_anchor_test.go`（拟新增）与 `runtime/state/task_account_size_test.go`（拟新增）＋本域 `design.md`/`tasks.md`；**只读**：`runtime/state/durable.go`、`runtime/task/checkpoint.go`、`assembly/{checkpoint,task_checkpoint,run}.go`、`runtime/state/mailbox.go`；**禁写**生产代码、`openspec/specs/**`、`scripts/gates/**`、`golden/**`、`memory/ledger/**`、外来未跟踪文件。
  验证：正锚（拟新增、尚未建立，今天必红）`go test ./assembly/ -run '^TestTaskAccountStatedSizeTracksLiveWork$' -count=1`（同任务数、已结收据递增 N 代，切面陈述条目/字节不随已结收据线性增长；失败信息须点名「每任务遍历全库 map」的实测计数）；负锚（拟新增、尚未建立；**原名 `TestTaskSliceRefusesToDropAReceiptOfAnOwedNotice` 已按 R68 退役——重定后的两枚锚见下方「05.R2 执行记录（冻结锚重定）」**）`go test ./runtime/task/ -run '^(TestTaskSliceReadsBackTheReceiptItDoesNotState|TestTaskSliceTrustsAStatedEmptyReceiptSet)$' -count=1`（省略收据族 → 座后不得重新欠账且收据从链读回；陈述空集而链上有 → 账户信任陈述；红转绿后须 exit 0）；消费者锚（既有，须保持绿）`go test ./runtime/state/ -run '^(TestDurableLoadReadsTheTaskSlice|TestDurableLoadRefusesAMismatchedReceiptIdentity)$' -count=1`（runtime/state/durable_seat_test.go:57/:108）＋ `go test ./runtime/task/ -run '^(TestTaskSliceStatesTheSettleAccount|TestRegistryCheckpointStatesTheAccountNotTheHistory)$' -count=1`（runtime/task/checkpoint_test.go:278/:178）。

- [ ] **05.R2 清减与恢复一致实现（依 G2 + 11.R2）** —— 交付：按 G2 择定实现经批准的候选（推荐甲：按任务键索引；乙仅在甲或等价索引存在时可用），使 `ReceiptsOf` 与切面收据陈述成本＝O(该任务收据)；清减判据写成「按身份判重仍可答」，MUST NOT 用 TTL 删除未完义务、MUST NOT 删三族；恢复侧沿用 `Seat`（切片入表＋尾段）/无切片整份读，并保持与运行期 `Commit` 同一记录应用路径。实施期文件白名单（候选）：`runtime/state/durable.go`、`runtime/task/checkpoint.go`、新增 `runtime/state/durable_receipts_index_test.go`、新增 `runtime/task/checkpoint_size_test.go`、本域 `design.md`/`tasks.md`；**禁写**：`assembly/run.go`（02/05 簇）、`assembly/checkpoint.go`（12.R2 在改，同触须串行或合单）、`memory/ledger/**`（06/08）、`features/evolution/governance/**`（07）、`openspec/specs/**`、`golden/**`。
  验证：正锚（拟新增、尚未建立）`go test ./assembly/ -run '^TestTaskAccountStatedSizeTracksLiveWork$' -count=1` 须由红转绿；另 `TestSettledTaskStateStaysConsistentAcrossRestore`（拟新增、尚未建立）`go test ./runtime/state/ -run '^TestSettledTaskStateStaysConsistentAcrossRestore$' -count=1`：清减后 restore 与全扫基线在欠账/已消费/尝试/拒绝四面上逐项相等；负锚（拟新增、尚未建立）`go test ./runtime/state/ -run '^(TestTaskAccountRefusesToClearAnOwedReceipt|TestSliceStatesUnstatedApartFromEmpty)$' -count=1`（判据写错 → 具名拒绝；「未陈述」与「陈述空」必须可区分，否则升级后首次 boot 把已消费通知重新欠账）；消费者锚（既有，须保持绿）`go test ./assembly/ -run '^(TestSymptomAnchorBootCostFlatAcrossGenerations|TestBootSeatsTheMailboxAfterTheDurableAccount)$' -count=1`（assembly/symptom_anchor_recovery_test.go:417、assembly/task_custody_anchor_test.go:27，`durableLoadCount` 不退化、欠账仍可投递）＋ 跨会话/跨进程原验收 `go test ./runtime/task/ -run '^TestRestoredTaskKeepsTheRevisionTheTailExpects$' -count=1`（checkpoint_test.go:235）与拟新增 `TestRedeliveredNoticeAfterRestartSettlesOnce`（尚未建立，跨进程重投至多一条收据）。

---

## 05.R1 执行记录（G2 任务域取证，2026-11-16）

**框未勾，理由见下。判决＝PARTIALLY-PROVEN，全部判据/反例/命令落本域 `design.md` §八·答、§八·表（G2-T1…G2-T9）、§八·N11、§十一、§十二。**

### 源树绑定（ORCHESTRATION 二节强制三件）

① 本轮读与量的是 **HEAD `9547faca`**，经 `git archive 9547faca | tar -x -C /tmp/recovery-g2task/tree` 解出的隔离树；② 未引用任何 CI 制品，N6/N11 的引用取自 ORCHESTRATION 台账并标明；③ 工作树候选未消费、未删改——主树**零 `.go` 写入、零 `go test`/`go build`**（另一单在改 `memory/ledger/{segment_store,journal}.go` 与四个测试文件）。`memory/ledger/segment_store.go`、`assembly/symptom_anchor_recovery_test.go` 一律读 HEAD（后者经 `git show 9547faca:` 取出）。

### 四问结论

1. **有效集**＝切片点名（携链）∪ 尾键集，成员判据是 `d.facts`（`TaskIDs()` durable.go:1003-1014），**不是** registry 现存任务、**也不是** registry ∪ 未结收据所属任务。已清扫任务的五族键永不搬冷（segment_store.go:954-1009），故收据在账外仍按身份可查。反例 PASS ×10。
2. **判重身份保留边界**＝选 **(c)**，且实测 (c) 在任务族上**永不到达** ⇒ 三族不可清减，成本只能由候选甲降。**旧键重提交是合法路径**：三层各自幂等（facts.go:94-105 / durable.go:850-856 / mailbox.go:161-173），`loop.go:1540-1549` 的稳定 instruction 键走第一层，同身份同内容不落第二条、不推进投递态。反例 PASS ×10。
3. **乱序完成**：欠账恒等式与前缀水位**不分叉但不同层**；`session.Consumed` **不进任何切面**（mailbox_checkpoint.go:32-34），`foldOwed` 重分配 `AcceptSeq`（memory.go:465-468），故它是每进程重建的局部量，**不得作清减边界**。反例 PASS ×10（position=0、owed 恰剩洞、max-seq 水位丢 1/1）。
4. **与 11 的衔接**：**BLOCKED**。收据身份是 notice 四段，`Event` 是内容不是身份（durable.go:68-71）；11.R2 未落地，不自造第二判据。

### 本轮新发现的两处 HEAD 既有缺陷（首次取得源码级实证，非本轮引入）

- **缺口 A（G2-T6）**：`Commit` 入表段 durable.go:881-887 对 outbox 无条件写 owed，`applyRecordLocked` :375-388 过滤 consumed/rejected ⇒ **runtime owed=3 / recovery owed=2**（同一组记录同一条收据）。RED ×10。「单一 g(x)」抽出了入表段、没抽出过滤段。
- **缺口 B（G2-T7）**：`RestoreCheckpoint` 只对未结义务 `seq.Observe`（checkpoint.go:267-269），链折叠对每条信封 Observe（restore.go:293），`CheckpointRestored` 不 Observe ⇒ 覆盖路径**重发已结四段身份**（covered seq=0 / folded seq=1）。RED ×10。与缺口 A 组合即重启后二次投递的可达机制；**组合路径未端到端实证**（design.md §十二·2 第 1 项）。

### 锚凭据（命令与冻结任务行逐字符一致，全部在隔离树）

| 锚 | 命令 | exit | 命中 |
|---|---|---|---|
| 正锚 | `go test ./assembly/ -run '^TestTaskAccountStatedSizeTracksLiveWork$' -count=10 -v` | 1 | 10 RUN / 10 FAIL |
| 负锚 | `go test ./runtime/task/ -run '^TestTaskSliceRefusesToDropAReceiptOfAnOwedNotice$' -count=10 -v` | 1 | 10 RUN / 10 FAIL |
| 消费者锚 | `go test ./runtime/state/ -run '^(TestDurableLoadReadsTheTaskSlice\|TestDurableLoadRefusesAMismatchedReceiptIdentity)$' -count=10 -v` | 0 | 20 RUN / 20 PASS |
| 消费者锚 | `go test ./runtime/task/ -run '^(TestTaskSliceStatesTheSettleAccount\|TestRegistryCheckpointStatesTheAccountNotTheHistory)$' -count=10 -v` | 0 | 20 RUN / 20 PASS |
| 共同门 | `go build ./...` | 0 | — |
| 共同门 | `go vet ./runtime/state/ ./runtime/task/ ./assembly/` | 0 | — |

正锚量逐字（10 次相同）：陈述收据条目 **8 → 40**（5×＝代数），存活任务恒 **2**，全库收据 **8 → 40**，陈述收据字节 **2379 → 6943**，整份 payload **3713 → 8277**；失败信息点名 `2 tasks x 3 families x 40 store receipts = 240 comparisons per cut`（240 中 2 与 40 是实测，×3 来自 durable.go:1053/1062/1071 三个全 map range 的读码，未插桩）。`bound=first*2` 是**候选容差非冻结裁决**（R19 N1）；线性判定与倍数无关。墙钟未作断言面（470µs→145µs 反向，噪声大于信号）。

### 未勾理由（三条，任一即不足）

1. **G2-T9 BLOCKED**：11.R2 未落地，判据表不完整。
2. **G2-T6/G2-T7 未修**：正确性缺口，须先于或同批于任何清减；G2-T7 的修法有设计级分叉，须编排者冻结。
3. **两枚拟新增锚只在隔离树建立，主树未落盘**（派发令禁写主树 `.go`）。冻结命令与测试名逐字符一致，但验收面要求主树可复跑。另：负锚落在 `runtime/task/g2_owed_receipt_test.go`（外部测试包 `task_test` 引 `runtime/state`，无环），**不在 05.R1 原候选白名单**（原列 assembly + runtime/state 两文件）内，改道须编排者确认。

### 交给编排者的三项

- **G2-T4 是三域共用水位行**，07.R1/12.R1 不得另写一版；本域 portion 只含 `G2-T*` 行，未写 07/12。
- **N11 前置条件**（design.md §八·N11）：`Seated()` 真 ∧ `TaskIDs()` 非空且逐代增长 ∧ `Scans` **为空**（区别于「五条 keys=0 的兜底扫描」，现有 `durableLoadCountOf` 把两者都求和成 0）。三条同时成立时零才是有界证据。供 09.R2 实施，本域不实现。
- **N6 attestation 判据**：托管 boot 的真实可观测后果是「座下了哪些任务、按身份读回什么四面」，不是族扫描行数；**不得**补发库存计数（R20 N6 已删该形态）。接线归 09.R2/13.R2。
- **候选乙勘误**：design.md §九 已标 G2 勘误——乙在 (c) 下**判据相反**，不是「前提不足」，不得实施。05.R2 任务行原文「乙仅在甲或等价索引存在时可用」按派发令未改（只追加执行记录），以 design.md §九 勘误为准。

## 05.R1 锚落盘（主树，2026-11-16 第二轮）

**源树绑定**：HEAD `9547faca` 的**主工作树**；`runtime/state/` 与 `runtime/task/` 本轮开工前 `git status` 干净（并发单在改 `memory/ledger/`、`assembly/`、`scripts/`）。三个包的生产文件与隔离树逐字节相同（`diff -rq ... | grep -v _test.go` 输出为空），故 §八·答 的行号与判定在落盘位置同样成立。

**落盘位置与改名（旧名→新名，隔离树临时名一律去 `G2` 前缀）**：

| 隔离树原型 | 主树落点 | 新名 | 本轮实测 |
|---|---|---|---|
| `g2_task_domain_test.go` / `TestG2OwedKeepsTheHoleWhenLaterNoticesComplete` | `runtime/state/durable_seat_test.go` | `TestOwedSetKeepsTheHoleWhenLaterNoticesComplete` | PASS |
| 同上 / `TestG2SeatedAccountAgreesWithIdentityRead` | 同上 | `TestSeatedAccountAgreesWithIdentityRead` | PASS |
| 同上 / `TestG2EffectiveSetIsTheStatedAccountPlusTheTail` | 同上 | `TestEffectiveTaskSetIsTheStatedAccountPlusTheTail` | PASS |
| 同上 / `TestG2RepeatCommitOfAStoredMutationReowsNothing` | 同上 | `TestRepeatCommitOfAStoredMutationReowsNothing` | PASS |
| 同上 / `TestG2RuntimeCommitMustNotReoweAConsumedNotice` | 同上 | `TestRuntimeCommitMustNotReoweAConsumedNotice` | **FAIL（缺口 A，故意红）** |
| `g2_sequencer_test.go` / `TestG2CoveredTaskMustNotReissueASettledNoticeIdentity` | `runtime/task/checkpoint_test.go` | `TestCoveredTaskMustNotReissueASettledNoticeIdentity` | **FAIL（缺口 B，故意红）** |
| `g2_owed_receipt_test.go` / `TestTaskSliceRefusesToDropAReceiptOfAnOwedNotice` | `runtime/task/task_slice_receipt_anchor_test.go`（新文件，外部测试包 `task_test`） | 同名（冻结名） | **FAIL（故意红）** |
| 原型内辅助：`g2Account`/`g2ReadAccount`/`g2SameAccount`/`g2TailOf`/`g2SeedThreeNotices`/`g2OwedNoticeLog` | 同文件 | `accountFaces`/`facesOf`/`sameFaces`/`tailOf`/`seedThreeNoticeTask`/`taskSliceFactLog` | — |

**落位理由（非白名单缩水）**：五枚任务账锚**并入既有文件** `durable_seat_test.go`（该文件的 `契约` 就是 `durable-delivery.md#canonical-fact-resolution`，同目录再开一个同锚非镜像文件会命中 `responsibility-fragmentation`）；缺口 B 并入镜像 `checkpoint_test.go`（生产面在 `runtime/task/checkpoint.go:267-269`）。**负锚的改道已由本轮派发令批准**（`runtime/task/` 在写面内），外部测试包 `task_test` 引 `runtime/state` 无环这一形态得以保留。

**原始退出码（主树，`go1.27.1 darwin/arm64`）**：

| 命令 | exit | 命中 |
|---|---|---|
| `go build ./...` | 0 | — |
| `go vet ./runtime/state/ ./runtime/task/ ./features/evolution/governance/` | 0 | — |
| `go test ./runtime/state/ -run '^(TestOwedSetKeepsTheHoleWhenLaterNoticesComplete\|TestSeatedAccountAgreesWithIdentityRead\|TestEffectiveTaskSetIsTheStatedAccountPlusTheTail\|TestRepeatCommitOfAStoredMutationReowsNothing\|TestRuntimeCommitMustNotReoweAConsumedNotice)$' -count=1 -v` | 1 | 5 RUN / 4 PASS / 1 FAIL（`runtime commit owes 3 / recovery owes 2`） |
| `go test ./runtime/task/ -run '^(TestTaskSliceRefusesToDropAReceiptOfAnOwedNotice\|TestCoveredTaskMustNotReissueASettledNoticeIdentity)$' -count=1 -v` | 1 | 2 RUN / 2 FAIL（`re-issued the four-part identity …:1:progress:0`；`re-owed the notice …:1:progress:1`） |
| 三枚红锚 `-count=3`（`red_state_x3.log` / `red_task_x3.log`） | 1 | 9 RUN / 9 FAIL，**每枚 3/3 同因**（同一失败原文各 3 次） |
| `go test ./runtime/state/ ./runtime/task/ ./features/evolution/governance/ -count=1` | 1 | 全部失败项**仅**为本轮故意红锚，无既有用例转红 |
| `go run ./scripts/comment_policy -no-baseline runtime/state runtime/task features/evolution/governance` | 0 | **0 finding** |

**仍未勾 05.R1，理由（本轮新增，两条独立）**：①冻结正锚 `TestTaskAccountStatedSizeTracksLiveWork` 的落点是 `assembly/`，**不在本轮写面**（派发令把它列为勿动目录，并发单正在写同包），主树 grep 该函数仍零命中——叶的**正锚缺席**，按「锚不存在就不是证据」不得勾；②R30 记录的另一条未勾理由 **G2-T9 BLOCKED**（11.R2 的事实身份等价未回写本域）不因本轮落盘而消失。本轮只闭合了原理由③（锚只在 /tmp）。

**退役相关性核对（R51）**：本域落盘的七枚锚**无一**断言「任务五族留热」或依赖退位机制——`grep -n 'Retire\|retire\|cold\|keptHot'` 在三个原型上零命中，缺口 A/B 的机制在 `Commit`/`RestoreCheckpoint`，不在存储层。design.md 答 2 里「(c) 边界永不到达」的**论证**引用了「五族永不搬冷」，R51 删掉退位后该前提变为平凡真，**结论（按身份重放可达 ⇒ 已结收据必须恒可陈述）不变**，故判据 (c) 与负锚不依赖被删机制。


---

## 05.R2 执行记录（缺口 A / A′ 半单，2026-11-16）

**框未勾，且本单不构成 05.R2 的完成。** 05.R2 含两个面：①`runtime/state/` 的运行期与恢复期 owed 集一致（缺口 A/A′，本单已闭合）；②`runtime/task/` 的覆盖路径与链折叠路径身份序列一致（缺口 B，**另一派发单**）。②未做，故叶不得勾。

**源树绑定**：主工作树（HEAD `9547faca` ＋ 未提交候选），非隔离树。开工前 `git status` 已带 `M runtime/state/durable_seat_test.go`（上一轮锚落盘，+325 行），本单未写该文件。本单写面：`runtime/state/{durable,mailbox,mailbox_checkpoint}.go`、`runtime/state/mailbox_checkpoint_test.go`、本域两文档。

**交付（详见本域 `design.md` §十五）**：

- `settledLocked`＝`consumed ∪ rejected` 成为唯一判据；`oweLocked` 成为唯一折叠函数，`Commit` 与 `applyRecordLocked` 同调它；`unowedLocked` 改读同一判据。
- `durableConsumption` 增 `settled(notice)`（`DurableCommit` 实现），`owedOfWork` 折叠前对切面载荷与账户两源**同样**过滤。**未加 `Consumed` 切面字段**（§十三·6）。
- 新增锚 `TestMailboxSliceRestoreSkipsANoticeTheAccountSettledPastTheCut`（`mailbox_checkpoint_test.go`），走 `StateRegistry.RestoreState`（与 `assembly/checkpoint.go:883` 同入口）。

**原始退出码（主树，`go1.27.1 darwin/arm64`）**：

| 命令 | exit | 命中 |
|---|---|---|
| `go test ./runtime/state/ -run '^TestRuntimeCommitMustNotReoweAConsumedNotice$' -count=1 -v` | 修前 **1** → 修后 **0** | 修前 `runtime commit owes 3 / recovery owes 2`；修后 1 RUN / 1 PASS |
| `go test ./runtime/state/ -run '^TestMailboxSliceRestoreSkipsANoticeTheAccountSettledPastTheCut$' -count=1 -v` | 修前 **1** → 修后 **0** | 修前 3 座位 / 应 1；修后 1 RUN / 1 PASS |
| 两锚 `-count=10 -v` | 0 | 20 RUN / 20 PASS / 0 FAIL |
| 两锚 `-race -count=10` | 0 | 无 DATA RACE |
| `go test ./runtime/state/ -count=1 -v` | **0** | 69 RUN（29 顶层＋40 子）/ 全 PASS / 0 FAIL |
| 修前同包 `go test ./runtime/state/ -count=1` | 1 | 仅 `TestRuntimeCommitMustNotReoweAConsumedNotice` 一枚红 |
| `go build ./...` / `go vet ./runtime/state/` | 0 / 0 | — |
| `go run ./scripts/comment_policy -no-baseline runtime/state` | 0 | **0 finding** |
| `go test ./runtime/task/ -run '^TestCoveredTaskMustNotReissueASettledNoticeIdentity$' -count=1` | **1**（缺口 B，未认领） | 失败原文指向 `runtime/task/checkpoint.go:267-269` |

**未验证（如实）**：本包之外**一次都没跑**（`assembly/`、`tests/...`、`memory/ledger/`、`features/...` 属并发单写面，其结果不可信）。故「运行期 `Commit` 不再为已结通知开账」对下游消费者的影响未经实测，须在写面释放后由消费 `runtime/state` 的包复跑。G2 整体仍未判，树未绿，CI 未触发。

---

## 05.R2 执行记录（缺口 B 半单，2026-11-16）

**框仍未勾。** 05.R2 含三个面：①`runtime/state/` 运行期与恢复期 owed 集一致（缺口 A/A′，上一单已闭合）；②`runtime/task/` 覆盖路径与链折叠路径身份序列一致（缺口 B，本单已闭合）；③候选甲（按任务键索引）＋清减面（未做，冻结负锚 `TestTaskSliceRefusesToDropAReceiptOfAnOwedNotice` 仍红）。③未做，故叶不得勾。

**修法**：`RestoreCheckpoint` 的 `seq.Observe` 来源改为「切片陈述的每条 mutation 的 outbox 信封」，与 `foldLocked` 同源同集（详见本域 `design.md` §十六）。**不变量**：恢复交给 sequencer 的身份集＝账户已提交的全部通知身份，与义务是否结清无关；两条恢复路径对同一账户发出同一身份序列。

**原始退出码（主树，`go1.27.1 darwin/arm64`）**：

| 命令 | exit | 命中 |
|---|---|---|
| `go build ./...` / `go vet ./runtime/task/` | 0 / 0 | — |
| `go test ./runtime/task/ -run '^TestCoveredTaskMustNotReissueASettledNoticeIdentity$' -count=1 -v`（修前） | **1** | 1 RUN／1 FAIL（`re-issued the four-part identity …:1:progress:0`） |
| 同命令（修后） | **0** | 1 RUN／1 PASS（`covered path issues seq=1, folded path issues seq=1`） |
| 两锚 `-count=10` / `-race -count=10` | 0 / 0 | 稳定绿，无 DATA RACE |
| `go test ./runtime/task/ -count=1 -v` | 1 | 30 顶层／93 RUN：28 PASS／2 FAIL，两枚均为既有故意红（`TestConsumptionFailureIsReportedByName` 属 12 域 G2-C2；`TestTaskSliceRefusesToDropAReceiptOfAnOwedNotice` 属本叶清减面） |
| `go test ./runtime/state/ -count=1`（下游） | **0** | ok |
| `go run ./scripts/comment_policy -no-baseline runtime/task` | 0 | **0 finding** |

**新增锚**：`TestCoveredTaskKeepsTheIdentityOfANoticeItStillOwes`（覆盖任务仍欠一条通知时不得复用其身份）。鉴别力由变异实测证明：观察集改为「outbox 减去未结义务」→ 该锚红、冻结锚仍绿。

**未验证（如实）**：`./assembly/`、`./tests/...`、`./features/...`、`./memory/ledger/` 未跑（并发单写面）；真实 boot 入口后果未实测；G2 整体未判，树未绿，未提交，未触发 CI。

## 05.R2 执行记录（候选甲半单，2026-11-16）

**框仍未勾。** 05.R2 含三个面：①`runtime/state/` 运行期与恢复期 owed 集一致（缺口 A/A′，第三轮已闭合）；②`runtime/task/` 覆盖路径与链折叠路径身份序列一致（缺口 B，第三轮已闭合）；③候选甲＋清减面——**本单只做了候选甲的成本面**，清减面的冻结负锚判据须重定（`design.md §十七·4`，属编排者权限且文件在白名单外）。③未全，故叶不得勾。

**候选甲已落地（`runtime/state/durable.go`）**：`receiptsByTask` ＋ 唯一归档入口 `holdReceiptLocked`（12 处写入全经它），`ReceiptsOf` 退化为 O(该任务)。**前提按 R51 反重重核成立且与反转无关**（实测：`ReceiptsOf` 零日志读，夹具无 `FactLog` 参与）；反转改变的是另一面——按身份读的前缀成本由 O(全部键)（`journalLog.keys`）降为 O(该前缀键数)，故「省略收据族、开档读回」这条清减形态在反转后更便宜。

**红绿对（原始退出码）**：

| 命令 | exit | 命中 |
|---|---|---|
| `go build ./...` / `go vet ./runtime/state/` | 0 / 0 | — |
| `go test ./runtime/state/ -count=1 -v` | **0** | **71 RUN（31 顶层＋40 子）／0 FAIL**（修前 69 RUN） |
| 新锚 `-count=10`（两枚） | 0 | 20 RUN／20 PASS |
| `go test ./runtime/state/ -count=1 -race` | 0 | 无 DATA RACE |
| `go test ./runtime/task/ -count=1 -v`（下游） | 1 | 93 RUN／28 PASS／2 FAIL，与本单修前**逐条相同**（两枚既有故意红） |
| `go run ./scripts/comment_policy -no-baseline runtime/state` | 0 | **0 finding**（该组原有 1 finding 一并清零） |

**新增锚（并入同锚既有文件 `durable_seat_test.go`，非新建——新建触发 `responsibility-fragmentation`）**：
- `TestReceiptsOfAnswersTheReceiptsOfOneTask`：四读路径（运行期写入／整份读／切片座／身份读）× 两任务 × 三族，与整表参考读逐项相等；**5 处归档点逐条变异，全部转红**。
- `TestReceiptsOfCostsOnlyItsOwnTask`：同一任务答案在 2 任务 vs 256 任务（128 vs 16,384 收据）下成本比 ≤4×；实测 ≈1×；**变异改回整账户走查 → 92× 红**。

**成本读数（实测，同机）**：一次切面（逐任务过一遍）修前 63.5µs → 36.3ms（2→256 任务，二次），修后 21.8µs → 2.79ms（线性）；单次 `ReceiptsOf` 修前 20.9µs → 168µs，修后 9.8→8.5µs（平坦）。

**偏离计划一处（已记 design §十七·3）**：任务行拟新增 `runtime/state/durable_receipts_index_test.go`，实际并入 `durable_seat_test.go`——新建文件在 `comment_policy` 的 `responsibility-fragmentation` 下多出 1 finding（同锚组已有无生产镜像的测试文件），并入同锚文件同样满足「锚在包内可复跑」，并把该组清到 0。

**冻结负锚处置**：主树 `-run '^TestTaskSliceRefusesToDropAReceiptOfAnOwedNotice$'` → exit 1（原文同前）。隔离树实测两向：把变异字面量 `[]`→`nil` 即绿（生产零改动，说明清减路径已存在、锚红在「以空集陈述权威空集」）；让 `Seat` 忽略载荷三族则锚绿但三枚既有绿锚转红。**判据互斥，改锚不在本单写面，提议的不变量见 `design.md §十七·4`，待编排者裁定。**

---

## 05.R2 执行记录（冻结锚重定，2026-11-16 第五轮）

**框仍未勾。** 清减面的冻结负锚按编排者裁定 R68 重写（`design.md §十七·4 勘误`）：**旧锚 `TestTaskSliceRefusesToDropAReceiptOfAnOwedNotice` 已退役**，其判据被证明可被夹具编辑平凡满足（`[]`→`nil`，生产零改动即绿），且与三枚既有绿锚互斥。替代它的是同包内两枚锚：

- 正锚 `TestTaskSliceReadsBackTheReceiptItDoesNotState`：省略收据族 → 不重欠、`ReceiptsOf` 答回同一条收据；负向控制＝链上无该收据时必须重欠。
- 负锚 `TestTaskSliceTrustsAStatedEmptyReceiptSet`：陈述空集 → 账户信任陈述（零前缀读、零收据、重新欠账），故 `nil` 与 `[]` 不可互换。

**复跑命令（替换旧名，避免零匹配 exit 0 的假绿）**：
`go test ./runtime/task/ -run '^(TestTaskSliceReadsBackTheReceiptItDoesNotState|TestTaskSliceTrustsAStatedEmptyReceiptSet)$' -count=1` → **exit 0／2 PASS**（`-count=10` → 20/20）。

**红绿与门禁（主树原始退出码）**：`go build ./...` 0／`go vet ./runtime/state/ ./runtime/task/` 0／`go test ./runtime/state/ -count=1` **0（71 RUN／0 FAIL）**／`go test ./runtime/task/ -count=1` **1（94 RUN／30 顶层 PASS＋63 子 PASS／1 FAIL，仅 `TestConsumptionFailureIsReportedByName` 属 12 域既有故意红）**／`go run ./scripts/comment_policy -no-baseline runtime/state runtime/task` 0（0 finding）。三枚既有绿锚（`TestDurableLoadReadsTheTaskSlice`、`TestDurableLoadRefusesAMismatchedReceiptIdentity`、`TestSeatedAccountAgreesWithIdentityRead`）同命令 3 PASS。

**鉴别力（变异，隔离树逐条实测后复原复核）**：生产把省略当陈述空 → 正锚红；生产把陈述空当省略（`nil` 判据改 `len()==0`）→ 负锚红；夹具分别把两锚的族在 `nil`／`[]` 之间翻转 → 各自那半红；生产 `readTaskLocked` 丢弃扫到的收据 → 正锚红。详见 `design.md §十七·4 勘误` 的变异矩阵。

**为何仍不勾（逐条可核）**：本行冻结的其余验收面尚未建立——`./assembly/` 正锚 `TestTaskAccountStatedSizeTracksLiveWork`、`runtime/state/` 的 `TestSettledTaskStateStaysConsistentAcrossRestore`、`TestTaskAccountRefusesToClearAnOwedReceipt`、`TestSliceStatesUnstatedApartFromEmpty` 与 `TestRedeliveredNoticeAfterRestartSettlesOnce` 在包内 `grep` 均零命中；`./assembly/` 与 `./tests/...` 本窗口禁跑。**未跑未建即不勾。**

**未验证（如实）**：`./assembly/`、`./tests/...`、`./features/...`、`./memory/ledger/` 未跑；真实 boot 入口后果未实测；G2 整体未判；树未绿；未提交；未触发 CI。

## 新增叶（阶段5自审 A5 回写，2026-11-16）

- [ ] **05.R3 Seat尾谓词的三态与真实boot复核**。提交序入口已有实现；按所载`type==TypeCheckpoint ∧ session==""`识别最新框架检查点，不按键拼写。遵循design §十八·4/§十八·7：未找到框架检查点或引擎无提交序能力⇒nil并具名走完整恢复；找到框架检查点且确无更新任务⇒非nil空集，合法地恢复零年轻任务；确有更新任务⇒精确尾键集。删除“永不为空集”旧要求，不用人为填键掩盖来源错误。正/负锚均已存在、本轮未跑：`go test ./assembly -run '^(TestSeatTailNamesEveryTaskYoungerThanTheNewestFrameworkCheckpoint|TestASpellingBasedStopDropsTasksBehindASessionCheckpoint|TestAnEmptyNonNilTailMustNotSeatZeroYoungTasks)$' -count=10 -v`，第三枚属assembly而非runtime/state，钉有真实年轻任务时不可误给空尾，另在同场景核真正空尾合法。消费者锚：`go test ./assembly -run '^(TestBootSeatsTheMailboxAfterTheDurableAccount|TestTaskRestoreFoldsTheTailOfACoveredChain)$' -count=10 -v`，正式boot→任务账→信箱；原runtime/state、runtime/task、tests完整回归面仍在本叶终验内。独立审核三态来源、逐名命中及同候选结果，不借旧零匹配命令完成。
  - **文件白名单（精确）**：`assembly/checkpoint.go`（`tailFactKeys` 取法）、`runtime/state/durable.go`（仅 nil/empty 分支语义须调整时）、`runtime/state/durable_test.go`、`assembly/checkpoint_test.go`、本域四件套。
  - **前置**：**06.R4（S2）完成**（可寻址 order 可用后才谈「按 order 新→旧」）。**冲突**：与 09.R4／06.R4／assembly 锚合批单同触 `assembly/` → **串行**。**注意**：本叶与 08.R3 有顺序耦合——谓词须在退位删除**之前或同批**就位，否则删退位即触发静默漏座。


## 05.R3 执行记录（阻塞，2026-11-16）

**框未勾。本单在派发令写面内零 `.go` 写入**：主树 `assembly/`、`runtime/state/`、`memory/ledger/` 本轮未落一个字节（`git status` 与开工前逐条相同；`memory/ledger/*` 的 `M` 属并发单）。

**阻塞项（一条，可核）**：冻结谓词要「按 order 新→旧收集键直到最新框架检查点」，而**引擎全键序走查在包外不可达**——`journalAddress.eachNewestFirst`（journal.go:2095）、`journalLog.eachKeyNewestFirst`（journal.go:2419）、`journalKV.eachCommittedNewestFirst`（journal_kv.go:291）**三者皆未导出**；`AtomicStore` 接口（atomic_store.go:54-65）只有 `Read`/`Keys`/`Namespaces`/`Commit`，无任何序。`memory/ledger` 本窗口由并发单持有，派发令明禁本单写入 ⇒ **取数接缝缺席**。

**探针实测（主树，`go test -overlay` 注入，仓内零落盘）**：

| 命令 | exit | 读数 |
|---|---|---|
| `go test ./assembly/ -overlay /tmp/recovery-05r3/overlay.json -run '^TestProbeDurableKeysAreInvisibleToTheIndexWalk$' -count=1 -v` | **0** | `durable_keys=2 index_walk_entries=0 durable_keys_seen_by_the_walk=0` |

即唯一导出的序走查 `EachIndexNewestFirst`（segment_store.go:1565）只走 store 事件索引（`s.order` 仅在 segment_store.go:709 追加事件键，`commitOrderOf` :565-577 按事件族过滤），**持久任务族键一条不命名**；按它实现尾＝恒空非 nil 尾＝本叶要防的静默漏座（S0 水位尾实测 0/3）。

**基线读数（主树，与本单开工前逐条相同——本单未改生产，故为基线而非本单成果）**：

| 命令 | exit | 命中 |
|---|---|---|
| `go build ./...` | **0** | — |
| `go vet ./assembly/ ./runtime/state/` | **0** | — |
| `go test ./runtime/state/ -count=1 -v` | **0** | 71 RUN（31 顶层＋40 子）／0 FAIL |
| `go test ./runtime/task/ -count=1 -v` | **1** | 94 RUN／30 顶层 PASS／**1 FAIL**：`TestConsumptionFailureIsReportedByName`（12 域既有故意红，未动） |

**三枚冻结锚全部缺席**（主树 `grep` 零命中）：`TestSeatTailNamesEveryTaskYoungerThanTheNewestFrameworkCheckpoint`、`TestASpellingBasedStopDropsTasksBehindASessionCheckpoint`、`TestAnEmptyNonNilTailMustNotSeatZeroYoungTasks`。建立它们的前置是 `design.md §十八·4` 的甲案接缝；第三枚在 `runtime/state` 侧不能独立成立（空非 nil 尾是否漏座取决于尾的来源，状态层只能把陷阱钉成期望＝削弱锚）。

**交给编排者两项**：①`design.md §十八·4` 的甲/乙分叉须按 R22 冻结签名（推荐甲：引擎新增 `EachCommittedNewestFirst(ctx, visit func(key string, value []byte) (bool, error)) error`，识别与尾集合留在 `assembly.tailFactKeys`；乙 repurpose `TailKeys` 与 §十九·一「引擎不得解释 idx 语义」冲突，且令 4 处 ledger 切面锚转红）；②该接缝落地后本叶方可派实施（三锚＋正锚转绿的实测），且**必须仍在 08.R3 之前或同批**——否则删退位即触发静默漏座。
