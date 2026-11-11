# 02-timing-evidence 提案

## Why

「耗时不符合预期」当前不可判定：load() 的 5 次 Scan 无分段计数，长报告写在 $TMPDIR 而 CI 只上传 --out 树，是证据黑洞。

## What Changes

为 5 次 Scan 与 restoreChains 的 fold 增加分段键数/字节/耗时计数，写入 CI 可上传产物。

## 边界与依赖

依赖：无（只加观测）；被依赖方：03（用计时证明优化有效）、05（CI 上传路径）；接口面：runtime/state/durable.go、assembly/run.go、scripts/scenarios/run_runtime_scenarios.py 产物树；禁止事项：MUST NOT 改恢复语义与数据准确性断言。
