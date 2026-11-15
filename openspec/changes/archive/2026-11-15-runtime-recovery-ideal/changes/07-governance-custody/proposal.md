# 03 governance 托管（W2 并行域）

## Why

一级 [design.md](../../design.md) D1 判据 P3「全部可恢复态是 Stateful 切片」未兑现：governance 不在框架托管之列。实证（✅ grep）：生产注册的 checkpoint 切片恰 6 个——projection（assembly/checkpoint.go:1048）、delivery（assembly/host_delivery_checkpoint.go:120）、mailbox（runtime/state/mailbox_checkpoint.go:37）、tasks（assembly/task_checkpoint.go:26）、inputs（assembly/checkpoint_inputs.go:52）、compress（assembly/compress_checkpoint.go:50），governance 缺席。违 checkpoint-state/spec.md:7「框架 SHALL 承载全部可恢复状态切片」。

两条恢复旁路须分清（不可混为一谈）：

1. **事件账本双全扫——今天是测试态，非生产态**。`DenialLedger.BindStore`（denial.go:121）在 :127 经 `seedSeqLocked`（:196）调 `seedEventSeq`（:327→`log.Scan`:331）全扫一遍找尾 seq，又在 :129 经 `rebuildFromStore`（:230）调 `replayGovernanceEvents`（:304→`log.Scan`:305）第二遍全扫重建索引；`GoalRegistry.BindStore`（goal.go:126）同构双扫（:301/:336）。**但 `BindStore`/`WithLedger`/`WithGoals`/`NewDenialLedger`/`NewGoalRegistry` 生产零调用**——调用方只有 tests/integration/capabilities/*_test.go 与 skeleton_assert.go（编译期断言，非接线）。`seedEventSeq` 遇 `Scan` 错即 `return 0`（denial.go:333）静默重置尾 seq，重启后复用已写键 → re-key 覆盖已提交事实风险（违 spec.md:44 红线）。
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
- **被依赖方**：06 退位（W3）以本域建立的 governance 存活（切片 + 尾 seq）为权威之一；04 验收真量锚（boot 成本有界）消费本域消全扫后的结果。
- **波次**：W2 并行——触碰集 features/evolution/governance + assembly/host.go:608 + runtime/task/approval，与 boot 路径簇（01+02+04：assembly/run.go / memory/ledger/journal.go / segment_store.go）**不相交**（一级 D4）；前置 W1 绿。
- **禁止事项**：
  - **SHALL NOT 删除/覆盖/改写已提交事实**（spec.md:44 红线）；governance 事件链恒可按身份重放，切片只是派生加速。
  - **保 seq 连续性**：19 位零填充键形与字典序＝操作序不改不重排；尾 seq 不得静默重置致 re-key。
  - 不触外来未跟踪文件、冻结归档（archive/** 只附勘误）、config-surface-normalization 未提交变更（同 dev 分支独立 change，互不触碰）。
  - 不擅改 §五冻结签名（Stateful 接缝 / 存活 权威 / 验收真量四量）。
  - 本文档阶段**只规划不实施**；实施待用户审过 design 后另行编排。