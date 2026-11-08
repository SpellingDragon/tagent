# dev 反向同步到 main 的 squash 线

**Status**: accepted（2026-11-04 记录，惯例自 2026-10-11 起已在用）

`origin/dev` 会被强制更新到 `main` 的 squash 合并点：PR 以 squash 方式并入 `main` 后，未 squash 的 dev 线（含其逐提交历史）被丢弃，`dev` 指针移到该 squash 提交。这是刻意的取舍：保留一条线性、可逐提交审计的发布线，代价是 dev 分支历史被重写、不可快进，本地须用 `git fetch` + `git checkout -B dev origin/dev` 采纳，不能普通 `git pull`。不做反向同步时 dev 与 main 会逐渐积累两份内容相同但提交不同的历史（v0.4.0 tag `521ee2c2` 与旧 dev 头 `407ab532` 树完全相同，却在 v0.3.0 处分叉 112/44），合并评审无法判断哪条是权威。

- **触发**：PR squash 并入 `main` 之后；不是每次 dev 提交。
- **前置**：确认 squash 后的树与待丢弃的 dev 树等价（`git diff --stat <old-dev> <new-tip>` 为空），并把本地未提交工作先 stash/备份。
- **不做的事**：不把 `main` 合回 dev 生成 merge commit（那会重建双线），不改写 `main` 历史。
- **依据**：历史 merge 标题 `chore(sync): 反向同步 main 的 squash 合并点到 dev`（`50acdfe1`/`b4074939`/`bbdba9ed`）、`openspec/changes/archive/2026-10-18-runtime-rebuild/baseline.md:13`；本次复核见 `openspec/changes/archive/2026-11-03-runtime-architecture-convergence/evidence/08-ledger-reconciliation.md`。
