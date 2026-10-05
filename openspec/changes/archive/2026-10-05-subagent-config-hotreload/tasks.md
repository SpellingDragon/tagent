# Tasks（一级：只编排）

> 本文件只做编排与收尾；实现任务全部在二级 `changes/<NN-domain>/tasks.md`。
> **启动门**：域 02 实现类孙任务须等域 01 归属结论入 main（见 specs/orchestration）。
> **波次语义**：W 标签仅为汇报分组；解锁=依赖入 main，非整波等待——域 03 无依赖可立即开工。

```mermaid
flowchart LR
    D01["01 hotupdate-matrix-audit<br/>(W0)"] -->|"摘要 knob 归属结论<br/>(接口常数:字段名+解析点)"| D02["02 summary-knobs-hotparams<br/>(W1)"]
    D03["03 percall-subagent-overrides<br/>(W0 可并行)"]
    D02 --> F["F1-F4 收尾<br/>(一级直管)"]
    D03 --> F
    style D01 fill:#fff9c4
    style D03 fill:#c8e6c9
    style F fill:#ffe0b2
```

## 二级子变更清单

- [x] **01 hotupdate-matrix-audit**（W0，关键路径）：6/6 完成 —— 矩阵 62 行（`changes/01-.../matrix.md`，每行带行号/测试名）；FP 面 9 顶层+30 子测、SRC 面 keepRecent 行为针补齐；**摘要改判 FP 面已热**（情报 C3 采纳，见一级 design D1b）；跨域情报 C1（FILE 第三通道）已回写 spec
- [x] **02 summary-knobs-hotparams**（**用户裁决收口 2026-10-05**：摘要已在 FP 代际面＝配置变更下回合生效，满足热更目标；粒度下移不值其契约扩展成本：域 01 实证摘要 knob 在 FP 面＝**已热（代际粒度）**，本域从"补真缺口"降级为"粒度下移可选增强"（下一次摘要动作即生效）；接口常数已备（矩阵：`SummaryMaxTokens`/`tagent.go:397-429`/`HotNumbers`+`liveNums`/消费点 `effectiveSummaryMaxTokens:408`，另须处置 C2 `recent_full_count`）→ 见 `changes/02-summary-knobs-hotparams/tasks.md`
- [x] **03 percall-subagent-overrides**（W0）：3.1–3.7 完成 —— fail-before 双红（行为红非编译红）转绿 8 测；四参数经真实委派路径；覆盖挂 invocation 结构性防泄漏；`Declarative.Overrides` 冻结+重放；3.7 系 R8/R9 情报回写（跨重启重投递透传，编排者越域修 `RedispatchAsync`/`SubagentRedispatcher`/`SubagentSpecFromDeclarative`）。注：域内 tasks 尾部曾被 IDE 写入缺陷截断，已按编排者复验证据重建

## 波次执行注记（W0）

- 并发纪律生效：两 agent 白名单零交集、git 权限归编排者；四查抓出域 03 tasks 尾部丢失与域 01 快照期 policy 快照失真（03 后续自修），编排者复跑定谳。
- 环境级发现（如实入档）：本工作区 IDE 写入工具对多个文件出现"保存失败实为已写入"/"延迟回写覆盖后改内容"，致体内注释复活与文件尾残段；全部以锚点计数复核 + `gofmt -e` 语法核查 + 全量复跑收敛。**教训：本仓并发期以磁盘真相为准，凡改必复核。**

## 导览（域 → 证据/踩坑）

| 域 | 证据锚 | 踩坑/情报 |
|---|---|---|
| 01 矩阵审计 | `changes/01-.../matrix.md`（62 行，每行带行号/测试名）；`agent/org/hotupdate_matrix_fp_test.go`、`agent/compress/hotupdate_matrix_source_test.go` | C1 FILE 第三通道（spec 已回写三分类）；C3 摘要改判 FP 面已热（驱动域 02 收口）；C2 recent_full_count 构造期派生边界（未实施域的遗留记录） |
| 02 摘要热参 | proposal 裁决注记 | 未实施——用户裁决收口；接口常数备查矩阵摘要行 |
| 03 per-call 覆盖 | `agent/percall_override_test.go`（8 测，fail-before 双红为行为红）、`agent/task/percall_declarative_test.go`；生产面 `tool_agent.go`/`session.go`/`task_manager.go`/`task_record_sink.go`/`declarative.go` | R8/R9 情报回写为 3.7（跨重启重投递透传，编排者越域修）；域 tasks 尾部曾被 IDE 写入缺陷截断，按复验证据重建；声明面测试改显式白名单 |
| 环境级 | 波次执行注记 | IDE 延迟回写三干扰（体内注释复活/文件尾残段/签名旧缓冲覆盖）——处置纪律：锚点计数复核+gofmt -e+提交前双跑 build |

## F 收尾（一级直管）

- [x] F1 三禁区零改动断言：`git diff 68eb8bc..HEAD -- agent/org/fingerprint.go` 为空（实测 0 行）；恒装源（agent/agent.go:569）与提交点零写入经 diff 复核未被触碰 —— 验证：编排者实测入 W0 战报
：`git diff <基线>..HEAD -- agent/org/fingerprint.go` 为空；恒装源与提交点零写入语义以代码检视+域内测试双证 —— 验证：`git diff $(git merge-base <基线> HEAD)..HEAD -- agent/org/fingerprint.go | wc -l` 输出 0
- [x] F2 全量门禁 —— `bash scripts/lint.sh` exit 0（lint: ok）；`go test ./... -short -count=1` 35 包 ok；`go test -race . ./agent ./agent/task ./rl` 4 包净；`openspec validate --specs --strict` 106/106；CI @ b1e0e72 四 job success：`bash scripts/lint.sh`；`go test ./... -short -count=1`；`go test -race . ./agent -count=1`；`openspec validate --specs --strict`；CI 四 job 绿 —— 验证：各命令 exit 0
- [x] F3 DoD 逐项核验 —— 域 01（6/6，矩阵 62 行）/域 03（3.1-3.7，8 测 + 3.7 编排者越域修）/域 02（用户裁决收口 2026-10-05，proposal 留裁决注记）；F1 禁区 diff 0 行；F2 四门 exit 0；勾选四查完成（含域 03 tasks 截断重建与 IDE 写入缺陷三次干扰的完整处置记录，见波次执行注记）（本文件勾选 + 各二级 tasks 勾选真实性四查：勾选/产物/数字/遗漏） —— 验证：grep 无 `- [ ]` 残留于三二级 tasks
- [x] F4 openspec archive（--skip-specs：能力 delta 已手工并入主 specs——config-hot-reload +矩阵需求、+新 capability per-call-subagent-overrides；编排门随 change 消亡）+ 导览（上节）（域 → 证据链接矩阵入执行日志） —— 验证：`openspec list --json` 为空
