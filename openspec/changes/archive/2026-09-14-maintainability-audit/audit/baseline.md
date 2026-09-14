# 审计取证基线（baseline）

- base commit: `cf006e1adf7da3f9c7eb49421d42f450fe34a53c`（fix(task): pruneTerminal nil detector 守卫）
- 取证日期: 2026-09-13
- 工作区状态: 洁净（仅本变更目录未跟踪）

## 取证命令与结果

| 命令 | 结果 |
|------|------|
| `go vet ./...` | exit 0，零告警 |
| `go test ./... -short -count=1` | 全绿，零 FAIL |
| `go test ./event/ ./prompt/ ./agent/task/ ./memory/ -cover` | event 49.7% / prompt 86.1% / agent/task 83.0% / memory 75.5% |
| `go test ./agent/... ./tool/action/... ./memory/... -race` | 见 /tmp/race_baseline.log（后台运行中，结果回填） |

## 包/文件清单（非测试代码，按行数降序）

| 包 | 文件 | 行数 | 测试行 |
|----|------|------|--------|
| agent | 19 | 5310 | 6762 |
| memory | 16 | 4577 | 3716 |
| .（根包） | 11 | 3514 | 2355 |
| tool/action | 7 | 3349 | 4743 |
| agent/compress | 7 | 1925 | 2538 |
| agent/governance | 8 | 1705 | 933 |
| tool/recall | 4 | 1221 | 665 |
| evolution | 5 | 1143 | 652 |
| agent/task | 3 | 1098 | 1217 |
| memory/engine | 4 | 1090 | 889 |
| tool/knowledge | 3 | 967 | 318 |
| memory/kv | 2 | 808 | 643 |
| rl | 4 | 772 | 809 |
| event | 4 | 748 | 362 |
| tool/mcp | 3 | 637 | 462 |
| prompt | 3 | 591 | 719 |
| agent/reliability | 4 | 537 | 452 |
| plugin | 4 | 417 | 393 |
| memory/embedder | 3 | 372 | 285 |
| tool/plan | 1 | 302 | 270 |
| tool/spec | 3 | 294 | 71 |
| tool/task | 2 | 293 | 116 |
| tool/govx | 1 | 216 | 67 |
| tool/memoryx | 2 | 171 | 73 |
| workspace | 1 | 139 | 0 |
| tool/file | 1 | 122 | 128 |
| testutil | 1 | 93 | 0 |
| modelutil | 1 | 93 | 0 |
| tool | 1 | 49 | 218 |

合计 29 包，非测试 ~27.6k 行，测试 ~25.6k 行。tasks.md 预估 20 包，实际审计按本清单扩展：modelutil / tool（根）/ tool/file / tool/knowledge / tool/recall / tool/spec / workspace / testutil 并入就近批次（合并简报规则见 design Open Questions）。

## 覆盖率口径

`-cover` 为语句级包内覆盖率；agent 本体、tool/action、根包未单测 cover（其测试位于同包测试文件，cover 已含于上表未列项——后续逐包报告按需补测）。
