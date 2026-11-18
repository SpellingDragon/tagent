# 03 governance 托管（W2 并行域）

> **阶段状态（本轮重整）**：旧编号 3.1–3.4 与其凭据**保留为历史时点证据**（曾全勾），不推导当前完成；受影响条目在 `tasks.md` 标「待复验」。本轮新增叶 **07.R1**（设计门 G2：治理/审批有效期/历史查询边界）与 **07.R2**（有效状态与尾恢复），只规划、不实施。
> **行号漂移（R29 先例）**：本文旧引用的 `approval_store.go:481/:485` 与 `host.go:608-610` 在当前树已漂移；实测为 `rebuild` approval_store.go:570、`os.ReadDir` :574、`governance.Open` host.go:621（`WithLedger` :624 / `WithGoals` :625）、`seatGovernance` :773。行号不是契约，接线事实不变。
> **当前生效条款**：主规格 `openspec/specs/session-checkpoint-material/spec.md` 的「governance 恢复读切片而非全流扫」（已由 10 域同步转正）。旧 `specs/checkpoint-state/spec.md` 的 MODIFIED 块（条款名「框架统一 checkpoint 事实承载可恢复状态」）**降级为历史时点证据**，取代关系已在该文件头注明；本轮新增 delta 见 `specs/session-checkpoint-material/spec.md`。

## Why

一级 [design.md](../../design.md) D1 判据 P3「全部可恢复态是 Stateful 切片」未兑现：governance 不在框架托管之列。实证（✅ grep）：生产注册的 checkpoint 切片恰 6 个——projection（assembly/checkpoint.go:1048）、delivery（assembly/host_delivery_checkpoint.go:120）、mailbox（runtime/state/mailbox_checkpoint.go:37）、tasks（assembly/task_checkpoint.go:26）、inputs（assembly/checkpoint_inputs.go:52）、compress（assembly/compress_checkpoint.go:50），governance 缺席。违 checkpoint-state 的「框架统一 checkpoint 事实承载可恢复状态」条款「框架 SHALL 承载全部可恢复状态切片」（该 capability 无主 spec 目录，权威文本见 12-checkpoint-state 冻结归档，见 05 域 proposal 实测）。

两条恢复旁路须分清（不可混为一谈）：

1. **事件账本双全扫——今天是测试态，非生产态**。`DenialLedger.BindStore`（denial.go:121）在 :127 经 `seedSeqLocked`（:196）调 `seedEventSeq`（:327→`log.Scan`:331）全扫一遍找尾 seq，又在 :129 经 `rebuildFromStore`（:230）调 `replayGovernanceEvents`（:304→`log.Scan`:305）第二遍全扫重建索引；`GoalRegistry.BindStore`（goal.go:126）同构双扫（:301/:336）。**但 `BindStore`/`WithLedger`/`WithGoals`/`NewDenialLedger`/`NewGoalRegistry` 生产零调用**——调用方只有 tests/integration/capabilities/*_test.go 与 skeleton_assert.go（编译期断言，非接线）。`seedEventSeq` 遇 `Scan` 错即 `return 0`（denial.go:333）静默重置尾 seq，重启后复用已写键 → re-key 覆盖已提交事实风险（违 `session-checkpoint-material` 的 Requirement「派生态有界且不删除已提交事实」）。
2. **审批簿目录扫——这才是生产恢复路径**。生产 `governance.Open`（assembly/host.go:608）只挂 `WithRiskRules`+`WithApprovals(hostApprovals)`（:609-610），不挂 ledger/goals。`hostApprovals = approval.NewApprovalManager(dir, 0)`（host.go:562，dir=root/approvals，`hostApprovalsDir="approvals"`:330），其 `rebuild()`（approval_store.go:481）经 `os.ReadDir`（:485）逐文件读回审批态——这是今天真实发生的 governance 恢复全扫，随审批历史线性增长。

## What

承一级 D6.5（governance 托管＝切片携尾 seq）：

- **governance 收编为一个 Stateful 切片**（StateName/SnapshotState/RestoreState 走冻结接缝 runtime/state/checkpoint.go:36），携尾 seq（denial/goal 两流）+ 审批态；切片是派生快照，governance 事件链仍是真相与回放源。
- **读切片消全扫**：`seedEventSeq`/`replayGovernanceEvents` 改读恢复后的切片（尾 seq + 派生索引），不再 `log.Scan` 全流；审批簿 `rebuild` 的目录扫亦收编为切片派生（restore + 尾回放），不再 `os.ReadDir` 全扫。
- **生产接线**：host.go:608 的 `governance.Open` 挂上 ledger/goals（`WithLedger`/`WithGoals`），并经 `Run.RegisterStateSlice`（assembly/checkpoint.go:284，与 delivery 切片 host_delivery_checkpoint.go:152 同一接缝）把 governance 切片注册到生产状态接缝——使托管真实生效，而非停在测试态。
- **seq 连续性 + 响亮失败**：19 位零填充键（denial.go:190 / goal.go:295 `%019d`，字典序＝操作序）保序不变；尾 seq 取自切片，切片缺失/损坏 SHALL 具名拒绝而非静默 `return 0` 重置。

## Capabilities

- `checkpoint-state`（MODIFIED）：把 governance（尾 seq + 审批态）纳入「全部可恢复状态切片」托管枚举，恢复读切片非全流扫，seq 连续、生产接线、审批态恢复一致。delta 见 [specs/checkpoint-state/spec.md](specs/checkpoint-state/spec.md)。

## 边界与依赖

- **依赖**：冻结 Stateful 接缝（runtime/state/checkpoint.go:36，§五，签名不动，切片携字段由本域声明、编排者核冲突）；`Run.RegisterStateSlice`（assembly/checkpoint.go:284）；既有 Sync 写屏障（denial.go:297 `putGovernanceEvent`→`log.Sync`）；审批簿 attention 链对账面（approval_store.go:512-525 `fileQuestion`/`fileAnswer`）。
- **被依赖方**：06 退位（W3）以本域建立的 governance 存活（切片 + 尾 seq）为权威之一；04 验收真量锚（boot 成本有界）消费本域消全扫后的结果。**勘误（2026-11-16，退位机制整体删除）**：首项作废——退位不属于任何波次，governance 切片只服务恢复与清减；04 真量锚的消费不变。
- **波次**：W2 并行——触碰集 features/evolution/governance + assembly/host.go:608 + runtime/task/approval，与 boot 路径簇（01+02+04：assembly/run.go / memory/ledger/journal.go / segment_store.go）**不相交**（一级 D4）；前置 W1 绿。
- **禁止事项**：
  - **SHALL NOT 删除/覆盖/改写已提交事实**（`session-checkpoint-material` 的 Requirement「派生态有界且不删除已提交事实」）；governance 事件链恒可按身份重放，切片只是派生加速。
  - **保 seq 连续性**：19 位零填充键形与字典序＝操作序不改不重排；尾 seq 不得静默重置致 re-key。
  - 不触外来未跟踪文件、冻结归档（archive/** 只附勘误）、config-surface-normalization 未提交变更（同 dev 分支独立 change，互不触碰）。
  - 不擅改 §五冻结签名（Stateful 接缝 / 存活 权威 / 验收真量四量）。
  - 本文档阶段**只规划不实施**；实施待用户审过 design 后另行编排。

## 本轮重整：托管已达成，有效态与历史查询边界未达成

**已核事实（读码，本轮）**：托管面在树内且已生产接线（`governance.Open` host.go:621 + `WithLedger` :624 + `WithGoals` :625；切片经 `StateSeat` 注册）。但**切片陈述的是历史而不是有效态**：

- `StateSeat.SnapshotState`（features/evolution/governance/checkpoint.go:~104-118）经 `ledger.stated()`（denial.go:215，返回**全部** `records`）、`goals.stated()`（goal.go:172，返回**全部** goals）、`approvals.SnapshotEvidence()`（approval_store.go:140-160，返回**全部** evidence 条目）陈述——三处都随历史线性增长，违反主规格「派生态有界且不删除已提交事实」（checkpoint 大小有界于开放项）。
- **有效期已存在但边界未定义**：`effectiveStatus`（approval_store.go:488-497）在读取时按 `ExpiresMs` 判过期；`RestoreEvidence`（:162-188）**跳过**已过期条目且不复活（符合「过期不复活」），但对 `ApprovalApproved`/`ApprovalDenied` 的已结条目**照常恢复并重放 attention 事实**（`fileQuestion`/`fileAnswer`）。切片因此既陈述有效态（pending 未过期）又陈述已结历史，语义混杂。
- **历史查询边界未定义**：`DenialLedger.Query(limit)`（denial.go:164-180）与 `Count()`（:183-189）以内存 `records` 为唯一来源，`Query(0)` 表示「全部」。当前消费方是测试（features/evolution/governance/checkpoint_test.go:242/:252/:382/:385/:411、tests/integration/capabilities/governance_recovery_test.go:49/:52/:67/:70/:335/:434 等）；生产侧没有经该面的查询入口（✅ grep），故**边界可以定义**，但一旦把索引裁成窗口，`Query(0)`/`Count()` 的既有断言面必须显式改写而不是悄悄变空。
- 两流尾序号（`DenialSeq`/`GoalSeq`/`GoalOrder`）与审批的**有效证据**是真正的有效态；denial 记录、已结目标、已结审批属于「历史，按身份可查」——这是 07.R1 要下的定义，不是今天已成事实。

**新增叶（叶号即依赖序）**：

- **07.R1 设计门 G2（治理域）**：定义三类边界——①**有效态**（两流尾 + 未结目标 + 未过期未结审批）；②**有效期**（跨重启语义：有效期以链上事实的 `ExpiresMs` 为准，切片 MAY 只陈述未过期项，但 SHALL 保证过期项不被复活且可按身份查到它已过期）；③**历史查询边界**（`Query(limit)`/`Count()`/目标列表在索引成窗口后的语义，边界外是「按身份查链」还是「具名拒绝」）。产出：判据表（与 05.R1、12.R1 **共用一张**）+ 红着的锚 + 对 `Query(0)`/`Count()` 既有消费方的处置清单（改写/保留/退役，涉及验收面须上报编排者）。
- **07.R2 治理有效状态与尾恢复（依 G2）**：切片只陈述有效态（尾 + 未结审批 + 当前目标）与**判重/去复活所需的最小历史身份**；恢复 = 切片 + 尾回放，与运行期同一 fold（`foldTail` denial.go:229、goal 侧同构）；`RestoreEvidence` 不再重放已结审批的 attention 事实（除非判据要求）；19 位零填充键与「尾序号取自切片、不得静默重置」保持不变。

**本轮不做的**（显式登记）：不改治理事件链（真相）；不把 denial 历史删除（只停止在切片里复制）；不改审批目录镜像的逐请求 durable 写（它是 `RestoreBoot` 的兜底）；不改 `Query(0)` 消费方的断言除非 G2 裁定。

### 边界与依赖（本轮新增叶）

- **依赖**：G2 判据表（与 05.R1/12.R1 共建）；`Stateful` 接缝签名（runtime/state/checkpoint.go:36-45，冻结）；治理事件流与尾回放（denial.go:229 `foldTail`、`:375` `governanceTail`、`:401` `eventSeqOf`）；审批读取面（`effectiveStatus` approval_store.go:488、`RestoreEvidence` :162、`rebuild` :570）；**12.R2 的 J1 边界**（切面水位不得覆盖未应用事实——治理切片若引入窗口，必须与该边界一致）。
- **被依赖方**：06/08 的存活权威与退位（治理事件的按身份可读性是「历史可查」的实现基础）；12.R3（治理切片是唯一按宿主注册的带义务切片，卸载/替换交接的样本）；09 的 boot 真量（本域清减后 `journalBytes`/`wallClock` 才能进一步下降，但不替代 S2 判据）。**勘误（2026-11-16，退位机制整体删除）**：末句中的「退位」作废；治理事件的按身份可读性仍是「历史可查」的实现基础，该判据不依赖搬迁。
- **接口面（候选、未冻结）**：`stateSliceWire`（checkpoint.go:52-72）可能追加「有效态/历史态分界」与审批窗口字段；`Query(limit)`/`Count()` 的签名**不改**（语义边界由 G2 定义）；`DenialLedger.stated()`/`GoalRegistry.stated()` 返回面可能收窄（候选）；不新增状态接缝方法、不新增挂点。
- **禁止事项**：SHALL NOT 删除/覆盖/改写已提交治理事件（19 位键形与字典序＝操作序不改不重排）；尾序号 MUST NOT 静默重置致 re-key；不把审批目录镜像当第二真相；不触 `openspec/specs/**`、`scripts/gates/**`、`golden/**`、外来未跟踪文件与 `config-surface-normalization` 未提交变更；不改写冻结归档；本轮不提交/推送/归档。