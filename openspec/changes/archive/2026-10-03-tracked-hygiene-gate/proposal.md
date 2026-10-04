# Proposal: 追踪卫生门的第四条判据（tracked ⇒ 不被 ignore 排除）

## Why

本档起草于 10/3 15:41，其主体（清理 hottest-sub1/2 四个零字节残骸、孤儿 `go.work.sum`、工作树遗物；立追踪卫生门并接入 `scripts/lint.sh`；顶层目录白名单 + README 登记）已由并行完成的变更 `root-structure-and-hygiene`（归档为 `2026-10-03-root-structure-and-hygiene`）交付，实现落点也不同：判据做进 `codetools tracked-hygiene` 子命令（纯函数 + 单测，与仓库既有门禁同构），而非新写一个 shell 脚本。

**唯有一条判据当时没实现，而它是对的**：追踪文件不得同时被 ignore 规则排除。落地时它立刻抓到 13 个真例——`examples/wechat-bot/` 用"默认全忽略 + 白名单"式 `.gitignore`，其中含 bot 的生产文件 `reincarnation_notice.go` 与其测试、`skills/url-fetcher/` 整套源码，全都处于"已追踪但被忽略"状态：这类文件的姊妹文件永不入库、修改需 `git add -f`，是静默漂移的温床。

## What Changes

- 卫生门新增第四条具名拒绝 `tracked-and-ignored`，判据取 `git ls-files -ci --exclude-standard`。**纠正原方案**：不可把路径喂给 `git check-ignore`——它连否定规则（`!README.md`）也算命中，在白名单式 `.gitignore` 上会整片误报（实测 13 条全误）。
- `examples/wechat-bot/.gitignore` 补齐白名单，使"追踪集 = 允许集"（13 文件 + 2 目录可穿越项），只点名既有追踪文件，不放大忽略面。
- 规格：不再新开 capability `repo-hygiene`（会与已归档进 `architecture-guardrails` 的同一条要求重复），改为对该需求做 MODIFIED，把第四条判据并入原文。

## Impact

- Affected specs: `architecture-guardrails`（MODIFIED：组合根物理边界与追踪卫生，+`tracked-and-ignored` 判据与场景）
- Affected code: `scripts/codetools/hygiene.go`（+判定函数与取值路径）、`scripts/codetools/hygiene_test.go`、`examples/wechat-bot/.gitignore`、`docs/comment-gate-tooling.md`（判据表 + check-ignore 陷阱）
- 无 Go 行为变更；负样本（tracked 一个 `*.log`）必红、撤除即绿
