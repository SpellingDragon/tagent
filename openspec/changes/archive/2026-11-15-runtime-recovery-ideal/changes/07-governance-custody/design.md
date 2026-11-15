# 03 governance 托管 · 设计

> 静态决策见一级 [design.md](../../design.md) D1（P3 全部托管 / P4 有界恢复）、D6.5（governance 托管＝切片携尾 seq）、D4（03 独立 W2 并行）、§五（Stateful 接缝冻结签名）。本域只列实施要点与实证触碰集，不擅改冻结签名。

## 技术要点（承一级 D6.5）

1. **governance Stateful 切片**：新增一个切片（StateName 如 `governance`），实现冻结接缝 `StateName/SnapshotState/RestoreState`（runtime/state/checkpoint.go:36）。`SnapshotState` 是纯读（接缝契约 :30-35：不 flush、不 commit、不等自身提交锁），携 **尾 seq**（denial 流 `evtSeq` denial.go:108 / goal 流 `evtSeq` goal.go:112）+ **审批态**（`ApprovalManager.evidence` approval_store.go:88）+ 重建索引所需的派生记录（`DenialLedger.records` / `GoalRegistry.goals`）。切片是派生快照，governance 事件链（`tagent/governance/v1/{denial,goal}:` denial.go:62-63）仍是真相与回放源；Offset 之后的事件经同一 reducer 回放（P2 单一 g(x)）。
2. **seedEventSeq 读切片**：`seedEventSeq`（denial.go:327）不再 `log.Scan`（:331）全流找 max，改读恢复后切片的尾 seq。`seedSeqLocked`（denial.go:196 / goal.go:301）从切片取尾 seq 续号，19 位零填充键（`%019d` denial.go:190 / goal.go:295）保字典序＝操作序不变。**切片缺失/损坏 SHALL 具名拒绝**，取代今天 `Scan` 错即 `return 0`（denial.go:333）的静默重置（re-key 覆盖已提交事实风险）。
3. **replayGovernanceEvents / rebuildFromStore 读切片**：`rebuildFromStore`（denial.go:230 / goal.go:336）不再经 `replayGovernanceEvents`（:304）`log.Scan`（:305）第二遍全扫，改读切片携带的派生索引 + 对 Offset 后尾部事件回放。`BindStore`（denial.go:121 / goal.go:126）的双全扫降为「读切片 + 尾增量」，与 tasks 切片（D6.4）同构。
4. **审批簿 rebuild 收编为切片派生**：`ApprovalManager.rebuild`（approval_store.go:481）的 `os.ReadDir`（:485）逐文件全扫改为从 governance 切片的审批态 restore + 尾回放。须保留既有对账语义：未问过的补 `fileQuestion`（:517）、已结的补 `fileAnswer`（:519/:524）、过期的不复活（:506-508 语义由切片 + 尾回放等价表达）。每请求文件镜像（`write` approval_store.go:455-469）作为审批的逐请求 durable 写保留，切片是其派生加速，boot 恢复读切片不读目录。
5. **生产接线**：host.go:608 `governance.Open` 在 `WithRiskRules`+`WithApprovals`（:609-610）之外挂 `WithLedger(ledger)`+`WithGoals(goals)`（policy.go:187/:166），ledger/goals 经 `BindStore` 绑定 durable 日志面；并经 `Run.RegisterStateSlice`（assembly/checkpoint.go:284，与 delivery 切片 host_delivery_checkpoint.go:152 同一接缝）把 governance 切片注册到生产状态接缝。**这一步使托管从测试态转为生产态**——今天 ledger/goals 生产零调用，切片若不接线则仍是死脚手架（违 P6）。

## 实证触碰集（✅ grep，不照搬一级清单）

- `features/evolution/governance/`：denial.go（`BindStore`:121 / `seedSeqLocked`:196 / `seedEventSeq`:327→Scan:331 / `rebuildFromStore`:230 / `replayGovernanceEvents`:304→Scan:305 / `eventKeyLocked`:188-190 / `putGovernanceEvent`:289-297 Sync 屏障）、goal.go（`BindStore`:126 / `seedSeqLocked`:301 / `rebuildFromStore`:336 / `eventKeyLocked`:293-295）、policy.go（`WithLedger`:187 / `WithGoals`:166 / `Open`）、**新增 governance 切片文件**（如 `checkpoint.go`，实现 Stateful 三面）。
- `assembly/`：host.go:562（`NewApprovalManager(dir,0)`）/ :608-610（`governance.Open` 挂 ledger/goals）/ :330（`hostApprovalsDir`）；切片注册经 `Run.RegisterStateSlice`（checkpoint.go:284）。
- `runtime/task/approval/`：approval_store.go（`rebuild`:481 / `os.ReadDir`:485 / `write`:455-469 / `evidence`:88 / 对账 :512-525）。
- `runtime/state/checkpoint.go:36`：Stateful 接缝**只读不改**（§五冻结）；切片携字段由本域声明，编排者核冲突。
- **测试**：tests/integration/capabilities/governance_recovery_test.go（`TestGovernanceRecovery`:33，双扫恢复回归锚）、governance_test.go:560-561、event_integrity_test.go:44、critical_calls_hold_through_formal_path_test.go:39/175。
- **不改**：skeleton_assert.go 的编译期断言形态（接线后 `WithLedger`/`WithGoals`/`BindStore` 签名不变，断言仍成立）；boot 路径簇（assembly/run.go / memory/ledger/journal.go / segment_store.go，属 01+02+04，与本域不相交）。

## 子步序（每步 go build 保持可编译）

3.1 切片携尾 seq + 审批态（Stateful 三面 + 派生索引）→ 3.2 seedEventSeq/replayGovernanceEvents/rebuild 读切片消全扫（含静默重置改响亮）→ 3.3 生产接线 host.go:608 挂 ledger/goals + RegisterStateSlice + 审批簿 rebuild 收编 → 3.4 负锚 + 消费者锚。（门绿是 W2 完成判据非每子步，build 绿才是每子步。）

## 风险与回退

- **测试态 vs 生产态须分清（核心精确点）**：今天事件账本双全扫（seedEventSeq/replayGovernanceEvents）**只在测试发生**——`BindStore`/`WithLedger`/`WithGoals`/`NewDenialLedger`/`NewGoalRegistry` 生产零调用（✅ grep：调用方仅 *_test.go + skeleton_assert.go 编译期断言）。生产真实恢复全扫是**审批簿 `rebuild` 目录扫**（host.go:562→approval_store.go:481/485）。**不得声称生产今天全扫 governance 事件**。本域既要把 ledger 接线生产（使切片托管真实生效），又要把接线后新活的事件账本 seed/replay 与审批簿 rebuild 一并改为切片派生。
- **seq 连续性**：19 位零填充键形（denial.go:190 / goal.go:295）与字典序＝操作序是契约，不改不重排；尾 seq 取自切片须等于已存储 max，重启不复用键。切片缺失/损坏静默 `return 0`（denial.go:333）会 re-key 覆盖已提交事实（违 spec.md:44）→ 改具名拒绝。
- **crash 一致性**：既有 Sync 写屏障（denial.go:297）保 durable 写成功语义；切片快照在安静点纯读（接缝契约 checkpoint.go:30-35），Offset 后事件经同一 reducer 回放，按事件键/seq 幂等不重不漏（与 tasks 切片 D6.4 同构）。审批簿切片派生须与 attention 链对账（approval_store.go:512-525），不复活已结/过期请求。
- **回退**（承一级 D7：core 回退＝恢复全史读，症状回红）：本域回退＝host.go:608 卸 ledger/goals（回到只 `WithRiskRules`+`WithApprovals`）+ 注销 governance 切片 + seedEventSeq/replayGovernanceEvents 恢复 `log.Scan` 全扫 + 审批簿 rebuild 恢复 `os.ReadDir` 目录扫。切片是增量派生，回退不删任何已提交 governance 事件（链恒可按身份重放），回到今天「ledger 测试态 + 审批簿目录扫」基线。