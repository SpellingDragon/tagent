# Proposal: 修「热更 applied 但消费读旧值」的 owner/记录分叉（fix-owner-apply-read-divergence）

## Why

CI race job 红（`org_hotreload_test.go:1759` 期望 7 实得 2），worktree 二分证明**长期潜伏**（我的 park 修复之前、根包测试合并之前皆同断言红），且稳定复现于单进程重复执行：`-count=12` 从第 3–4 轮起必红，跨进程 `-count=1` 全绿。

失败那一轮的日志是关键证据：`agent "g24_b" hot params applied: ... keepRecent=7` 与 `numeric-only full apply recorded` **都打了**，而 owner 的 `OrgKeepRecent()` 读回 2。`KeepRecentValue()` 走 `liveNums()` 活解析，故 2 只能来自「源里该 agent 的记录值不是 7」或「该 owner 解析到的源不是提交点那个源」。

即：**apply/记录的一侧与 owner 消费的一侧在重复执行下分叉**。这不是测试脆弱性问题——生产长跑中同样可达（多轮换代/多次 numeric-only 后），且正是我方刚立的红线要防的形状：**「携带而消费点未读 = 假热更」**。当前形态是"日志声称 applied，消费端读旧值"，比假热更更坏（带虚假成功证据）。

## What Changes

- **定位**（探针先行）：查清分叉发生在哪一侧——源闭包捕获、记录视图轮转、还是 owner 与 resident 表身份错位；产出唯一结论并据此选修法。
- **修复**：使「提交点记录的值」与「owner 消费时解析的值」成为**同一权威的同一次读取**；不留第二份可独立漂移的权威。若定位为纯测试侧 stale 句柄（产品无错），仍以契约测钉死「applied 即 consumable」这一不变量，防真分叉。
- **回归基线**：`-count=12` 单进程稳定绿入 CI 口径（race 门已 `-p 1` 串行），并作本变更 fail-before 的判据。
- **明确不做**：调低 count、改测试期望、重试掩盖、给断言加 sleep。

无 BREAKING：修的是热应用与消费的一致性，语义向"更诚实"收敛。

## Capabilities

### Modified Capabilities

- `config-hot-reload`: +1 需求「提交点记录即消费值（apply 与读值同源）」——把「applied 日志为真」升格为可测不变量。
