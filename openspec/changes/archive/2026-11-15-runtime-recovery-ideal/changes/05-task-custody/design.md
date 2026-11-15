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