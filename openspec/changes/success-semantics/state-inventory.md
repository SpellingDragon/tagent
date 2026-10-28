# 独立写权状态全枚举表（叶 7 / D9·D10）

判据（D9）：**不改允许操作的状态必须通过「删掉后从已有记录无歧义算回来」检验**；通过者删写权改派生读。
不能算回者按 D10 第三格二选一：**补权威记录**（新增持久面）或**保留写权并写明依据**。

行取证口径：位置一律指本表所在提交的 `文件:行`；「能算回」指**从已提交事实链（或其同级权威账）**能唯一重建，不依赖进程内记忆。

## 一、runtime/task 簇（本叶改动面）

| # | 状态 | 位置 | 能否算回 | 改允许操作 | 裁决 | 依据 |
|---|------|------|---------|-----------|------|------|
| T1 | `StatusProgress`（原状态值） | `runtime/task/spec.go:80-94`（墓碑常量 `statusProgressRetired`） | **能**：链上 `NoticeProgress` 落在某事实 outbox | 否：全部分派面与 `StatusRunning` 等价 | **删写权，改派生读** `MemoryRegistry.ProgressReported`（`runtime/task/progress.go`） | 分派面四处原本 Progress≡Running：`lifecycle.go:302`（Resume 拒绝）、`lifecycle.go:423`（进审批等待）、`lifecycle.go:430`（离审批等待）、`logical.go:169`（`isExecuting`）。progress 通知本就是事实，恢复路径已读链 |
| T2 | `memoryTask.status` | `runtime/task/lifecycle.go:92` | 能：末条 committed fact | 是（状态机全部分支） | 保留（权威本身） | 权威记录即事实链 |
| T3 | `memoryTask.revision` | `runtime/task/lifecycle.go:93` | 能：链长 | 是（`ExpectedRevision` 校验） | 保留 | 与 `carrier.Revision` 同源 |
| T4 | `memoryTask.round` | `runtime/task/lifecycle.go:94` | 能：链上通知 round 最大值 + 1（`restore.go:249-250`），resume 计数 | 是（Resume 门、通知身份） | 保留（登记派生索引） | 恢复路径已按 outbox round 重算 |
| T5 | `memoryTask.mutations` | `runtime/task/lifecycle.go:95` | 即权威折 | 是（`repeatOf` 幂等） | 保留 | 链的内存折 |
| T6 | `memoryTask.obligations` | `runtime/task/lifecycle.go:96` | 能：outbox 事实 − 已销账 + answered/blocked 覆写 | 是（Flush/Close 面） | 保留为派生索引 | `host.go:1572` 同族先例：账在持久层，表在内存 |
| T7 | `memoryTask.blocked` | `runtime/task/lifecycle.go:97-104` | **不能**：拒收原因无事实载体（durable 账只存义务状态，不存目标的话） | **是**：blocked 义务按名上报，不读作「仍在路上」 | **保留写权**，依据写入代码注释 | D9 裁决；补权威写＝新增持久面，超出本案（`manager.go:211` `recordBlocked` 注释） |
| T8 | `memoryTask.exitReason` | `runtime/task/lifecycle.go:105-110` | **不能**：cancelled 事实不带 executor 的话 | 否：纯报告（`manager.go:70`（Obligations face 投影）+ `lifecycle.go:105-110` 字段注释| 保留（报告字段，登记）；补持久化归下轮 | 与 D7 final 同族：跨重启不承诺，重启读空而非推断 |
| T9 | `memoryTask.cancelRequested` | `runtime/task/lifecycle.go:111-116` | **不能**：请求未落任何事实 | **是**：无请求的确认停止被具名拒绝（`cancel.go:79` `ErrCancelNotRequested`） | 保留写权，依据写入代码注释 | 同上；重启读作未请求，不从状态反推 |
| T10 | `memoryTask.stops` | `runtime/task/lifecycle.go:117` | 不能：执行面报告 | 否：报告面 | 保留 | 同上 |
| T11 | `memoryTask.handoff` / `handoffRecord` | `runtime/task/lifecycle.go:118-119` | 能：`handoff:` 前缀 mutation | 是（未来通知路由） | 保留（派生） | `restore.go:240-242` fold 已 `applyHandoff` |
| T12 | `admittedAt` / `changedAt` / `runSince` | `runtime/task/lifecycle.go:120-122` | 不能：进程内观察时刻，链上无时刻 | 是（TTL/执行超时/静默分级） | 保留，零值即不分类 | `logical.go:125-134` 已声明「链重建的任务不被未观察的静默分类」 |
| T13 | `memoryTask.reported` | `runtime/task/lifecycle.go:123` | 不能：「已报告过」的记忆 | 否：只做报告去重 | 保留 | `logical.go:225-235` 去重 |
| T14 | `obligationEntry.answered` | `runtime/task/lifecycle.go:73` | **不能**：目标的话无事实载体 | **是**：门控重试（answered 不再当「没说过」重投） | **保留写权**，依据写入代码注释 | D9；同 T7 |
| T15 | `Obligations.Count` | `runtime/task/manager.go:67` | 能：**当场现算** `len(entry.obligations)` | 否 | **登记已成立，无代码改动** | D9 判据；此前已现算 |
| T16 | `Obligations.Open` | `runtime/task/manager.go:72` | 能：**当场现算**，逐条取自 obligations | 否 | **登记已成立，无代码改动** | 同上 |
| T17 | `MemoryCommit.owed/revisions/facts/mutations` | `runtime/task/commit.go:161-164` | 能：`facts` 是权威，其余为索引 | 是（提交/幂等） | 保留（持久层自身索引） | 与 T6 同族 |
| T18 | `NoticeSequencer.next` | `runtime/task/handoff.go:226` | 能：`Observe` 重放链上每条通知 | 是（通知身份唯一） | 保留派生索引 | `restore.go:241` 已逐条 `Observe` |
| T19 | `MemoryAdmission.tasks`（dedup 表） | `runtime/task/admission.go:40` | 能：fold 时按 admission 材质重登记 | 是（dedup 裁决） | 保留派生索引 | `restore.go:225-227` 已重登记 |

## 二、兄弟簇（只读登记，本叶不改）

| # | 状态 | 位置 | 能否算回 | 改允许操作 | 裁决 | 依据 |
|---|------|------|---------|-----------|------|------|
| X1 | `host.pendingDeliveries` | `host.go:190` | **能**：`tagent/task/v1/delivery:` 前缀记录 `Scan` 重建 | 是（审批/拒绝面） | 派生索引（已成立） | boot 重建：`host.go:1572`（Scan）→ `host.go:1609-1616`（装表）；字段注释 `host.go:185-189` 已声明 |
| X2 | `host.heldSeq` | `host.go:191` | 能：记录 id 内序号 | 是（同 id 排序） | 派生索引 | 同上 |
| X3 | `state.Occupancy.slots/withdrawn` | `runtime/state/occupancy.go:43-47` | **不能**：在跑决策是活体事实，无持久源 | **是**：并发闸门（`ErrSlotHeld`，`occupancy.go:19`） | 保留写权：重启后零在跑即空态，**空态就是正确的重建值**，不新增持久面 | 与 T9 不同族：这里的「不能算回」正是其语义（进程内并发） |
| X4 | `delegationEntry.record.Status`（progress 投影） | `runtime/agent/delegate.go:176`、写入 `:326`、命名 `:616` | 部分：通知在链上，但本账是**投递观察**记录，不读链 | 是（`isTerminalStatus(entry.record.Status)` 门控 `:398`） | 保留为投递观察投影；**投影值改为 `StatusRunning`（progress 事实＝`record.Progress` 里的通知）**，改动属 `runtime/agent` 单元 | 见 §三 投影适配行 |
| X5 | `delegationEntry.seen/pending/booked`、`Delegator.open` | `runtime/agent/delegate.go:179-183`、`:200` | 不能：投递观察记忆 | 是（重复投递去重、land 门控） | 保留 | 同 X4 族：观察面而非权威面 |

## 三、投影适配行（兄弟单元 `runtime/agent`）

删掉 `StatusProgress` 后，`runtime/agent/delegate.go` 的两处引用改为运行相位（该文件属兄弟单元，随 `runtime/agent` 自身的改动落地，本叶未改）：

| 位置 | 原 | 现 | 语义 |
|------|----|----|------|
| `delegate.go:326`（进度通知分支） | `entry.record.Status = task.StatusProgress` | `entry.record.Status = task.StatusRunning` | progress 报告的事实载体是 `record.Progress` 里那条 `NoticeProgress`（`Observe` 已按稳定身份去重），状态位只报「仍在运行」 |
| `delegationStatusName`（`delegate.go:616` 起） | `case task.StatusProgress: return "progress"` | 该分支删除 | 状态词表不再有 progress 这个名字 |

全仓 `grep -rn "StatusProgress" --include=*.go .` 零命中即为本行落地的判据（`go build ./...` 同时通过）。
`tests/integration/execution/delegation_completion_test.go:220` 已按此改写期望值。

复跑判据：
`go build ./... && go test ./runtime/task ./runtime/agent ./memory/... ./tests/integration/... -count=1`

## 四、Status 重放语义（以 restore 为 oracle）

- 状态**按序号存储**，故删除 `StatusProgress` 不让序号前移：3 号保留为墓碑 `statusProgressRetired`，`StatusSettled` 仍是 4、`StatusAwaitingApproval` 仍是 7（`runtime/task/progress_test.go` 钉死）。
- 重放映射在**折叠处**（`restore.go:205` 调 `replayedStatus`），不在解码处：`DecodeTaskRecord` 保留原始字节，链只经 fold 读成任务。
- 旧记录（末条 fact 状态＝3）重放为 `StatusRunning`，其 progress 通知仍在链上，故 `ProgressReported` 仍答 progress——**旧链与新链答同一个答案**，这是「无歧义算回」的可判定口径。
