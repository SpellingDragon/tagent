# 01.101 · 状态与入口取证（W1-C）

- 候选：`52d7b664`（开工重核一致）。
- 方法：运行类探针一律在 `/tmp/w1c-copy`（`git archive HEAD` 副本），仓库零改动（`git diff --stat` 空、`git status --short` 无非 untracked 行）。探针源文件：`/tmp/w1c-copy/tests/integration/history/w1c_state_probe_test.go`（疑点 A）、`/tmp/w1c-copy/assembly/w1c_new_consumer_probe_test.go`（疑点 B）；疑点 C 复用 W1-A 的 `assembly/symptom_anchor_coherence_test.go`（拷入副本运行，仓库原文件未动）。
- 每条命令独立语句记录原始退出码；无管道吞码。

## A. 任务在线/恢复双解释 — 已驳回（主口径），一条边界差异如实记录

**结论**：同一提交序列分别走在线路径（`runtime/task/lifecycle.go:400` `applyCommitLocked` 及其调用方）与恢复路径（`runtime/task/restore.go:239` `foldLocked`），可观察状态 status/revision/round/receipts 在全部三个变体下一致，包括跨两轮的序列——`replayedRound`（`restore.go:303-317`）从 mutation 身份推轮次（`reactivate:`/`resume:` 前缀 +1、`settle:`/`reactivate-rollback:` 携带轮次取 max、outbox `Notice.Round` 取 max）与在线侧的显式 `entry.round++`（`lifecycle.go:387`、`reactivate.go:87`）收敛同一答案。唯一分叉字段是 obligations，且仅在「内存消费、日志无收据」形态出现（下述变体 A2），机理为恢复侧以日志确认为消费口径（`restore.go:234-236` 注释原文：Notices a restart still owes are stripped from their outbox once the log confirms consumption），属 at-least-once 重投的既有语义，非两套解释权任意分叉；重投由 `MemoryStore.AcceptNotice` 的身份占位幂等吸收（`runtime/state/mailbox.go:102-104`、`:158-169`）。按 D1 判定口径：不构成「状态分叉」反例，驳回；A2 差异不升格为缺陷，记录为边界行为。

**命令与退出码**（工作目录 `/tmp/w1c-copy`，`-count=1`）：

```sh
go test ./tests/integration/history/ -run 'TestW1COnlineVsRestoreReceipted' -v -count=1
# A1 exit 0（收据落账、单轮：受理→progress 通知的 running 提交→settle 终态→两条收据）
go test ./tests/integration/history/ -run 'TestW1COnlineVsRestoreFlushedOnly' -v -count=1
# A2 exit 1（同序列，义务仅经 manager.Flush 内存清偿、日志无收据）
go test ./tests/integration/history/ -run 'TestW1COnlineVsRestore' -v -count=1
# A3 总 exit 1 = A1 PASS + A2 FAIL(预期红) + MultiRound PASS（两轮：settle→Reactivate→progress→settle，四条收据）
```

**关键输出**：

A1/A3 差异表（全 same）：A3 多轮 `revision=6|6`、`round-by-reactivate=3|3`（恢复侧 Reactivate 行为探针，证明 fold 后轮次为 2、再入开 3）、`receipts-consumed=4|4`、`obligation-count=0|0`、`status=4|4`（StatusSettled）。

A2 差异表（`/tmp/w1c-a2.log`）：

```text
w1c field            | online | restore
obligation-0         | <absent> | awaiting-delivery:progress | DIFF
obligation-1         | <absent> | awaiting-delivery:settled  | DIFF
obligation-count     | 0      | 2      | DIFF
receipts-consumed    | 0      | 0      | same
revision             | 3      | 3      | same
round-by-reactivate  | 2      | 2      | same
status               | 4      | 4      | same
```

**file:line**：在线写侧 `lifecycle.go:400-417`（status/revision/outbox 义务）、`lifecycle.go:387` 与 `reactivate.go:86-93`（round++ 与 AwaitingTool 义务）、`lifecycle.go:328-330`（终态关执行义务）；恢复写侧 `restore.go:239-301`（fold：admission 重建 `round=1`、每链 `ExpectedRevision+1`、outbox 重开义务）、`restore.go:303-347`（replayedRound/opensRound/roundOfMutation 的字符串推导）；消费口径对齐机制 `runtime/state/durable.go:1184-1205`（`Records` 经 `unowedLocked` 剥离已消费通知）、`:1278-1294`、收据写侧 `runtime/state/mailbox.go:267-300`（`Consume`→`recordConsumption`）、`runtime/state/durable.go:825-858`；内存清偿不落收据 `runtime/task/manager.go:182-209`（`Flush`）。

**未查面（边界声明）**：序列未含 `Resume` 轮（`resume:` 前缀推导）与 `RollbackReactivation` 回滚链、`handoff` 链；`statusProgressRetired` 保留序读回（`progress.go:31-36`）未单独构造旧链验证。均为同构推导路径，本轮不扩为实施单。

## B. 旧 checkpoint 无新消费者切片 — 已复现

**结论**：第一进程不挂某消费者、落框架 checkpoint 后关进程；第二进程 boot 前注册该消费者再启动，其 fold 只覆盖 cut 之后的尾段：**fold 8 / 全链 24，缺 16 条覆盖区事实，且按空态启动**（RestoreState 从未被调，stated account 仅含 tail 派生项）。对照：既有 `TestNewConsumerRegistersSliceOnly`（两进程都挂）exit 0，证明差异仅由「第一进程不挂」引入。

**命令与退出码**（工作目录 `/tmp/w1c-copy`）：

```sh
go test ./assembly/ -run 'TestW1CLateConsumerCoversTheWholeChain' -v -count=1
# exit 1（复现红）
go test ./assembly/ -run 'TestNewConsumerRegistersSliceOnly' -v -count=1
# exit 0（对照绿）
```

**关键输出**（`/tmp/w1c-b.log`）：

```text
w1c late consumer folded 8 fact(s); the chain holds 24 fact(s) beside the checkpoint
w1c late consumer stated account: ["w1c-late-2-0=external_input:...", ... 共 8 项，全部为 cut 后尾段派生]
the late consumer missed 16 of the 24 fact(s) the chain holds beside the checkpoint: the boot replayed only the tail the projection cut states, and the account started empty
```

**机理链（file:line）**：`assembly/run.go:2611-2620` `bootIndexWalk` 以 `checkpointCut(checkpointSliceProjection)` 定界；`assembly/checkpoint.go:1132-1143` `checkpointCut` 只要 projection 切片在册（框架切片，任何 checkpoint 必含）即返回整 cut，`walkIndexPastCut`（`run.go:2636-2669`）有界即止、不回读 cut 之前；恢复侧 `runtime/state/checkpoint.go:442-468` `RestoreState` 只把 checkpoint **声明的** slice 还给已挂 holder，晚挂 holder 无 slice 可还；`RestoreEmptyState`（`:481-491`）仅在 tier 无 checkpoint 时走（`assembly/run.go:1081-1088`），pending 机制（`:452-461`、Register `:180-199`）只覆盖反方向（checkpoint 有 slice、holder 后挂）。两条路径叠加：晚挂消费者空态启动 + 有界 walk 只喂尾段 = 缺覆盖区。

## C. Boot/reload 贡献矩阵

| 入口 | 调用者 `WithPlugins` 贡献是否生效 | 证据（file:line / 运行） |
|---|---|---|
| `Assemble` | **生效** | `assembly/run.go:504→509`（opts 直达 assemble）、`:598-603`（`cfg.plugins(r)` → `r.plugins = instances`）。运行：`go test ./tests/integration/history/ -run 'TestEventIntegrityConsumptionRecovery' -v -count=1` exit 0——该测试经 `assembly.Assemble(..., assembly.WithPlugins(...))` 挂 probe 并真实驱动（`tests/integration/history/event_integrity_test.go:921-941`）。 |
| `Boot` | **不生效（已复现）** | `assembly/host.go:479-482` 消费 opts 进 `cfg`（`HostOption` 即 `Option` 别名，`run.go:1404`，故 `WithPlugins` 类型上可传 Boot）；`host.go:632-720` 重建 options **从未引用 `cfg.plugins`**（`grep -n "cfg\.plugins" assembly/host.go` 零命中，exit 1）；`host.go:690-705` 只装自家 `WithPlugins(hostPlugins(seam))`；`host.go:722` `assemble(...)` 只收自建 options——调用者贡献被静默丢弃。运行：`go test ./assembly/ -run 'TestSymptomAnchorHostBootMountsContributedPlugin' -v -count=1` exit 1（`/tmp/w1c-c1.log`）：builder 运行 **0 次**、贡献实例观察 0 事实，而 turn 正常完成落链（turn start/end fact 各一）——空观察即贡献被丢，非 turn 未跑。 |
| `reload` | **不重建插件集（无 WithPlugins 路径）；Boot 时装好的实例跨代存活** | `assembly/host_reload.go:200-392` `Reload` 全程只走 `loadHostDocument → publishDeclared`（`:292` 唯一发布入口），无任何 `r.plugins`/`WithPlugins` 触碰；`assembly/run.go:1533-1538` `WithPlugins` 契约原文「the builder runs once … and the instances it answers are never replaced」；`examples/wechat-bot/config_watch.go:55-64` 监视器只调 `host.Reload`。推论：reload 世代切换由 generation-scoped 消费者重建（`host.go:460-464`），run 级插件集冻结于 Boot 装配；经 Boot 传入的调用者贡献因 Boot 行丢弃而无从存在。本行纯静态＋契约注释，未另设运行探针（按派发单允许）。 |

## 探针可复跑性

- 探针源码位于 `/tmp/w1c-copy`（副本生命周期内可复跑）；若需长期留存，探针文件内容已由本节命令与关键输出完整代言，仓库未引入新测试文件（W1-C 只读取证单）。
- 复跑前置：`mkdir -p /tmp/w1c-copy && git -C /Users/pengweiye/Documents/codes/tagent archive HEAD | tar -x -C /tmp/w1c-copy`，再落两份探针文件并 `cp` 仓库工作区的 `assembly/symptom_anchor_coherence_test.go` 入副本。
- 计时敏感测试（`TestSymptomAnchorPerGenerationCostFlatOverALongRun`、`TestBootWallClockRejectsUnattributedGrowth`）未运行，不归本单。
