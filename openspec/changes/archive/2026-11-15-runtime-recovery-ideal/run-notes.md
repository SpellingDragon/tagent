# run-notes · runtime-recovery-ideal

> 逐轮执行凭据。F3（DoD 核验＋症状锚红转绿）证据在 §F3；域 08 锚对账证据在 §08。

## F3 症状锚红转绿（两次证据：当前树绿 → 回滚红 → 复原绿）

**隔离法**：实施全部未提交（主树 `M` 面），`git worktree` 于 HEAD 不含修复。故用**整树副本** `/tmp/recovery-f3/tree`（rsync，排除 `.git`/`examples`/`docs`/`openspec`/`logs`），回滚只改副本；副本复原以 **md5 与主树逐字节相等** 为凭据。主树全程未写入（下三文件 md5 会话前后一致，见 §隔离核验）。

| 锚 | 命令 | 当前树 | 副本基线 | 回滚后 | 复原后 | 回滚手法（副本内） |
|---|---|---|---|---|---|---|
| S1 `TestJournalBytesBoundedByLiveSet` | `go test ./tests/integration/history/ -run '^TestJournalBytesBoundedByLiveSet$' -count=1` | exit 0（0.989s） | exit 0（0.976s） | **exit 1** | exit 0 | `FileSegmentStore.RetireFacts` 早退空转＝退位能力缺席 |
| S2 `TestSymptomAnchorBootCostFlatAcrossGenerations` | `go test ./assembly/ -run '^TestSymptomAnchorBootCostFlatAcrossGenerations$' -count=1` | exit 0（3.604s） | exit 0（3.603s） | **exit 1** | exit 0 | `relocatableStorage` 原样返回＝装配层不开退位，boot 全史重放 |
| S3 `TestSymptomAnchorMidbandProcessRecalledByTicket` | `go test ./tests/integration/history/ -run '^TestSymptomAnchorMidbandProcessRecalledByTicket$' -count=1` | exit 0（0.370s） | exit 0（0.363s） | **exit 1** | exit 0 | `foldTicketLineOf` 只回摘要行＝中段折回「摘要而非票据」 |

**绿量（当前树 -v 日志）**：S2 `the boots paid journalBytes=[0 110 110 110 110] indexEntries=[0 2 2 2 2] durableLoadCount=[0 0 0 0 0] wallClock=[14.11ms 11.60ms 16.53ms 13.13ms 15.36ms]`；S3 `the presented sequence holds 14 entr(ies), 2 segment-fold product(s), history summary=false, and the fold took [00-1-plan 00-2-call 01-1-plan 01-2-call]` 且 `recall_get("evt_00-1-plan") answered 228 byte(s)`。

**回滚红原文（行为红，非编译错）**：

- S1：`journal.log holds 356558 real bytes (os.Stat) after 5 generations beside the 71326 bytes the live key set plus tail account for: the bytes grow linearly with the settled history instead of staying bounded by the live key set (the segment and original families of every settled generation are still hot: committed=[71254 142580 213906 285232 356558] retained=[71254 142580 213906 285232 356558] appended=[71254 71326 71326 71326 71326])`
- S2：`the boot of generation 2 replays 327775 journal bytes beside the 134991 of generation 1, over the bound 269982: the open read grows with the history instead of staying bounded (journalBytes=[0 134991 327775 559914 831274] indexEntries=[0 43 94 145 196] durableLoadCount=[0 12 24 36 48] …)`
- S3：`the request carries no ticket "[evt_00-1-plan|thinking_plan]" for the folded entry 00-1-plan (thinking_plan: "plan 00"): the model is handed the fold product's summary and no handle on the process text, so it cannot recall what the fold consumed (folded=[00-1-plan 00-2-call 01-1-plan 01-2-call], …)`

**分母自证**：S1 红日志同时陈述 retained 线性增与 appended 恒定；S2 红日志的 `indexEntries`/`durableLoadCount` 两量同向线性；S3 红日志陈述 folded 四键具名且 premise 腿已先证 `recall_get` 可取全文。三者均非空转。

## 隔离核验

主树三处回滚目标 md5，会话开始与结束时一致：

```
d75f6bdfed29c46f38c395149b29b399  memory/ledger/segment_store.go
58d9020ee81f102bdb52111cfb26d73e  assembly/run.go
21f22e96dea84eaaa26b370bd820b03b  memory/replay/age_segments.go
```

副本三次回滚后均以 `cp` 复原，实测 md5 与上表逐项相等；复原后三锚在副本复跑 `S1_RESTORED_EXIT=0 / S3_RESTORED_EXIT=0 / S2_RESTORED_EXIT=0`。主树 `config-surface` 未提交变更、外来未跟踪文件、`.wechat-config/data`、冻结归档全程未触。

## 域 08 锚对账（能力形状＝`RetireFacts` 单一原子能力）

对账对象：8 域原冻结面写 `EnumerateKeys`/`Tombstone`/`ReclaimTombstoned` 三方法，实现以 `RetireFacts(ctx, keepKeys)` 一步（复制→校验→重写，`memory/ledger/segment_store.go:889`）替代，避「已标记未搬」中间态（编排者 R3 认可）。对账后验收面＝**RetireFacts 单能力 ＋ identity-read ＋ 排他安静点**。

| 锚 | 命令 | exit |
|---|---|---|
| `TestRelocatedFactReadsByIdentity` | `go test ./memory/ledger/ -run '^TestRelocatedFactReadsByIdentity$' -count=1` | 0 |
| `TestColdArchiveRefusesCorruptColdTier` | `go test ./memory/ledger/ -run '^TestColdArchiveRefusesCorruptColdTier$' -count=1` | 0 |
| `TestRetirementSettlesOnlyTheKeysItsSnapshotRead` | 同上 -run | 0 |
| `TestTaskAdmissionMaterialStaysHotAcrossARetirementPass` | 同上 -run | 0 |
| **新增** `TestColdArchiveHoldsTheExclusiveGate` | `go test ./memory/ledger/ -run '^TestColdArchiveHoldsTheExclusiveGate$' -count=1` | 0（1.89s） |
| 同上 -race ×10 | `go test ./memory/ledger/ -run '^TestColdArchiveHoldsTheExclusiveGate$' -race -count=10` | 0（24.17s） |
| **新增** `TestRecoverKVReadsARelocatedRoot`（recover-kv 消费者锚） | `go test ./tests/integration/history/ -run '^TestRecoverKVReadsARelocatedRoot$' -count=1` | 0（0.83s） |

**新增两锚的非空转凭据（副本回滚）**：

- 排他闸锚：回滚＝退位后删除冷层字节（以删除代替搬迁）→ **exit 1**，`the write gate-writer-0 answers missing after the reopen, want the bytes the store acknowledged: the pass erased a write that landed inside its window`。复原后 exit 0。
- recover-kv 消费者锚：回滚＝原文族拆开搬（`blob:` 留热、`ref:` 搬冷）→ **exit 1**，`relocate the settled fact: memory: stored record is corrupt: original "recover-kv-settled-fact" has a reference but no independent body`——引擎在拷贝校验处以具名拒绝挡住拆分，命令因此永不面对「引用在热、正文不在」的孤儿根。复原后 exit 0。

**包级回归**：`go test ./memory/ledger/ -count=1` exit 0（21.515s）；`go test ./tests/integration/history/ -count=1` exit 0（18.030s）；`go build ./...` exit 0。

## 疑惑点与边界（如实申报）

1. **排他闸的红无法用「开闸」演示**：只在 `RetireFacts` 内放掉 `FileSegmentStore.mu` 不足以造红——`journalKV.retireSettled` 自持 `k.mu`，写者在重写期内仍被挡住。故闸的可观测后果由两腿承担：已确认写入不丢（重写只写快照键）＋搬迁中按身份读不答 missing；红以「删除代替搬迁」取得。
2. **recover-kv 消费者锚的红落在引擎具名拒绝，不在命令的孤儿报告**：族拆被 `ErrSnapshotCorrupt` 先拦，命令侧「误报孤儿」路径在现设计下不可达。故该锚的负腿是「引擎拒拆」，不是「命令少报」。
3. **S2 的 `durableLoadCount` 现树为 0 而非 12/代**：绿日志三量中该量全程 0，`indexEntries` 仅 2，S2 的红实际由 `journalBytes` 单量触发（红日志另两量同向线性）。这不减弱 S2（红绿两次均实测），但「三量各自独立敏感」本轮只对 journalBytes 取得实证。
