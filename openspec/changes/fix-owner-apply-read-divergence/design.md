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
