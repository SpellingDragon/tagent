# Design

## D1 与已归档变更的边界

本档不重复实现已完成部分：残骸清理、空文件/运行产物/顶层目录三条判据、lint 接入、README 登记纪律均由 `2026-10-03-root-structure-and-hygiene` 交付并归档。本档只交付它遗漏的第四条判据与其暴露的真债。

## D2 判据必须问 git，而不是问 check-ignore（原方案的错）

"追踪 ∧ 被忽略"的唯一正确取法是 `git ls-files -ci --exclude-standard`：它按 git 的最终判定（含否定规则与目录不可入规则）返回结果。原方案写的 `git ls-files | git check-ignore --no-index --stdin` 问的是"有没有命中任何模式"，而白名单式 `.gitignore` 的放行行本身就是模式——于是 13 个合法追踪文件全部"命中"，门一上线即红，或者被迫把门调松。这是一次**判据表述正确性**的纠正，不是实现细节。

## D3 白名单补齐的范围纪律

只把**当前已追踪**的文件点名进 `examples/wechat-bot/.gitignore`，不引入 `!skills/**` 这类目录级放行：`*` 仍挡住未来落进 skills 的运行产物，目录穿越用 `!skills/` 单独放行。补齐后 `git ls-files -ci --exclude-standard` 为空，门在现库上绿——门不靠豁免表工作。

## D4 判据落点与防回归

判定函数 `checkTrackedAndIgnored(ignored []string)` 是纯函数（列表由 live 命令喂入），单测覆盖逐路径具名拒绝与空集干净；负样本用 `git add -f probe_tracked.log` 现场验证 exit 1、撤除后 exit 0。规格侧并入既有条文（MODIFIED），不新开 capability，避免同一规则两处真相。
