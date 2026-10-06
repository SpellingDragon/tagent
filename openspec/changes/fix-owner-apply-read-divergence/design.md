# Design

## D1 证据与机理候选

**已确证事实**（本会话亲测，非推读）：

| 事实 | 出处 |
|---|---|
| 失败轮日志同时有 `agent "g24_b" hot params applied: ... keepRecent=7` 与 `numeric-only full apply recorded (revision 2)` | `/tmp/many.log` 第 162-164 行 |
| 同轮 `ownerB.OrgKeepRecent()` 读回 2 | 断言 `org_hotreload_test.go:1759` |
| 读侧是活解析：`KeepRecentValue() → cc.liveNums()` | `agent/compress/context_compressor.go:213-216` |
| 写侧提交点：`applyHotAll` 对每个 routable resident `a.SetHotSource(coord.currentHotFor)` + 记 receipt；`receipts` 只作诊断 | `tagent.go:456-487` |
| 跨进程 `-count=1` 12/12 绿、单进程 `-count=12` 第 3-4 轮起红 | 本会话实测 |
| worktree 二分：`d6b32c4^`（我的修复前）、`c579ae3`、`68eb8bc`（测试合并后）三处同断言红 | `/tmp/wt_*.log` |

**强线索（前科在册）**：`2026-09-17-resident-readiness-plan/notes.md` 的 **4.7 保持未勾**，原文记录该形态——

> 「`OrgKeepRecent` 读 `ContextCompressor.keepRecent`，而启动显式值只喂内层 `SmartCompressor`（**双真源**）——SeedKeepRecent 初版修补已回滚（需与 cc 构造默认路径合一）」

⇒ 本缺陷很可能就是这条**未收口的双真源**在重复执行下的显形：外层字段与内层权威值两处可各自持有 keepRecent，源轮转只刷新其一，读侧命中的是另一份。

机理候选（定位须三者逐一判证）：

| # | 候选 | 判据 |
|---|---|---|
| M1 | **双真源**：`ContextCompressor.keepRecent`（外层字段）与 `SmartCompressor.KeepRecentTasks`（内层权威）不同步，`liveNums()` 命中未更新的一侧 | 同轮打印两侧值 + 源解析值，三者不等即证 |
| M2 | 源闭包/记录视图轮转次序：`SetHotSource` 捕获的 `coord.currentHotFor` 与提交点 `record*` 写入的记录不是同一份，重复执行下错位 | 打印源返回的 (value, ok) 与 coord 记录内容比对 |
| M3 | owner 身份分叉：测试持有的 `ownerB` 与提交点遍历的 resident 对象非同一实例 | `reflect.ValueOf(...).Pointer()` 同一轮比对 resident 表与捕获句柄 |

## D2 定位探针法（先证后改，禁推读定案）

在同一测试内、失败轮上，一次性打印三方：提交点 receipt 值、`coord.currentHotFor("g24_b")` 解析值、`cc` 外层字段与内层 `KeepRecentValue()`。三值一比即把 M1/M2/M3 收敛到唯一。探针为临时件，结论写入本 design 的 D2b，代码不保留。

## D3 修复原则

- **唯一权威**：`keepRecent` 只允许一处真值来源（源解析），构造值仅作"源缺席时的兜底"，禁止第二条可独立写入的字段路径；若确需外层字段，必须与内层同源于同一次解析（读侧合一），不得双侧可写。
- **同源即同测**：新增契约测钉「提交点记为 applied ⇒ 消费侧读到该值」，并在**同一进程内重复执行**（count≥10）下仍成立——这是本缺陷的永久回归门。
- 不改热更通道模型（fp/源拉取面划分不动）、不改 4.7 之外的公共契约面。

## D4 验证口径

- fail-before：`go test . -run '^TestRollbackOfHotAddNumericWithInFlightTurn$' -count=12`（现红）→ 修后 exit 0。
- 新契约测单跑与 `-count=12` 均绿；根包 `-short`、`-race . ./agent`、`./scripts/race_check.sh`（CI 同形）全绿。
- CI：dev push 四 job 绿（race job 不再偶发红）为收口判据。

## D5 否决区

| 否决项 | 理由 |
|---|---|
| 降 `-count` / 改断言期望 / 加 sleep 重试 | 掩盖而非归因，违反"不以复跑绿结案"纪律 |
| 只把测试改成重新取 owner（绕过 M3） | 若 M3 为真相，生产同样会持 stale 句柄，属产品缺陷，不能靠测试绕 |
| 给 `ContextCompressor` 再加一条写入通道 | 双真源正是病灶，加通道只会加深 |

## D2b 定位结论（探针实测）

探针为临时件（`zz_probe_test.go`，跑完即删；原始输出留存会话日志），另以既有测试专用提交屏障
（`orgCommitBarrier`，生产恒 nil）把同一形状固化成确定性观测。**判据全部来自实测读数，不来自读码。**

**唯一结论：M2 家族成立——分叉不在"源与消费的两个权威"，而在"提交与返回的轮转次序"。**
精确机理：`reload()`（tagent.go:568 起；锁外判戳快路径 573–576，锁内落戳 589，提交在戳之后）在**提交之前**就把 `lastSeenMtime` 落下，而
`docs/wiki/platform/org-hot-reload.md#trigger-timing`（§十四）明文规定该戳的语义是
「**处理完成的标志是记下的 mtime**」。于是同进程里任何第二个进入者只要先按戳认领了这次
mtime 变更并进入长临界区，随后运维同步入口 `CheckOrgReload()` 就会在**锁外**比较
`mt == lastSeenMtime` 而判定"已处理"、**不取重载互斥量、不等提交**直接返回；测试断的读就落在
这个返回与那次提交之间。**实测已证的**是"另一 goroutine 先落戳、提交仍在飞"这一半（发散轮里
测试侧再无任何触发，值却在 +0.990ms/+1.018ms 自行翻到 7）；**据代码闭合**的是那个 goroutine
的身份：除运维同步入口外，reload 的唯一其他进入者是回合驱动的懒检测（每 LLM 调用一次
`orgReloader` → `requestCheck` → `go reload()`），而两个发散轮同有 main 回合在飞
（探针日志里 `runEventLoop:main`/`under budget` 与翻转前后相邻）。
§十四 给同步入口的绑定是「等整次构建与发布完成」，实测被违反。

### 三方读数（发散轮，同一次观测内）

| 侧 | 读数 | 含义 |
|---|---|---|
| 提交点 receipt | `agent "g24_b" hot params applied: keepRecent=7` | 只是"这一轮要 applied 7"的意图日志，落在视图轮转之前 |
| `coord` 源解析（含 ok） | `HotSnapshot() = (keep=2, ok=true)` | 源活解析正常，返回的是**上一代已提交记录** |
| 消费读值 | `OrgKeepRecent() = 2`，内层不经请求（Compress 每边界用 liveNums 覆盖） | 与源解析**完全一致** |
| 同步入口耗时 | `7.292µs / 7.916µs`（正常 reload 为 `468µs–1.9ms`） | 早退特征：快约两个数量级 |
| 之后再无任何触发 | `+0.990ms / +1.018ms` 自行翻到 7 | 提交当时仍在另一 goroutine 手中飞行 |

确定性门的同形状读数（12/12 轮一致）：`entryWaited=false entryElapsed=6.291µs–11µs
keepAtEntryReturn=2 sourceAtEntryReturn=(keep=2,ok=true) ttlAtEntryReturn=1m0s keepAfterCommit=7`。
TTL 轴同窗（记为 applied 的 `5m` 此刻仍读 `1m0s`）⇒ 整批数值轴都受同一次序影响，不是某字段的问题。

### M1/M3 判证为否

- **M1 双真源：否。** 40/40 轮（探针）+ 12/12 轮（门）都满足
  `OrgKeepRecent() == HotSnapshot().KeepRecentTasks` 且 `ok=true`；且提交一旦落地消费侧**立即**读到 7
  （`keepAfterCommit=7`），不存在"某一侧停在构造值不动"的第二权威。notes.md 4.7 描述的
  「`OrgKeepRecent` 读外层字段而启动显式值只喂内层」在今天的代码上已不成立：`KeepRecentValue() →
  liveNums()` 每边界一次源解析、外层 `keepRecent` atomic 仅是**源缺席时**的构造兜底
  （context_compressor.go:183–200），内层 `SmartCompressor.KeepRecentTasks` 不在读路径上
  （Compress 以 liveNums 结果作 `CompressOptions.KeepRecentTasks`，同文件 362/383 行）。
  ⇒ 4.7 的 SeedKeepRecent 回滚**不必**重启，也不得按"补一条写入通道"去修（正是 D5 否决区）。
- **M3 owner 身份分叉：否。** 40/40 轮 `reflect.ValueOf(ownerB).Pointer() ==
  reflect.ValueOf(residentCacheForTest(entry)[g24B]).Pointer()`（`sameInstance=true`），
  且新取句柄与捕获句柄读数逐位相同 ⇒ 测试持有的不是 stale 句柄，生产句柄也不分叉。

### 修复位点与白名单冲突（已停手，交裁决）

两处次序都在 `tagent.go`，而 `tagent.go` 不在本 change 写入白名单，其中提交点 `applyHotAll`
另列禁区，故按纪律停手（方案见 tasks 2.2 注记）。**没有**任何 `agent/compress/*` 或
`agent/context_manager.go` 内的改法能在不动提交点的前提下闭合此窗：读侧已经合一（M1 否证），
把等待塞进消费侧（轮询/sleep）或在压缩器里再存一份"applied 值"都正是 D5 的否决项。
