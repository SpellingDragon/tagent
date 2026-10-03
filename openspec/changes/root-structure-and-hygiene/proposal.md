# Proposal: 根包结构与追踪卫生（大扫除第二阶段，A+B+C 合并）

## Why

上一阶段清理的是语义层（注释/文档/门禁）。本阶段对象是物理层，两项都已量化：

1. **追踪卫生**：`hottest-sub1/`、`hottest-sub2/` 被 git 追踪，内容是 4 个**零字节**运行残骸（`.tagent-writer.lock`/`relations.journal`，9 月测试以相对路径落盘误入库）；工作树另有 9 月遗物目录（`own-*`、`hottest-sub3`、`hottest-drop-*`）与孤儿 `go.work.sum`。`testStore` 已挪临时目录（今日十余轮测试未再生成），故这是"删 + 钉门"而非活缺陷——但没有门，下一个 `hottest-sub1` 还会来。
2. **根包结构**：32 文件 / 生产 18,216 行 + 测试 12,393 行平铺一个包，实为三个职责：装配面 ≈3.5k、世代治理族 ≈1.6k（org_hotreload/org_candidate_txn+overlay/owner_retirement/partition_collision）、资源与模型引用 ≈0.6k。三个巨型测试文件（2007/1969/1715 行）是刚立的同位门最该管的形状。

## What Changes

- **A 卫生**：移除被追踪的空残骸与孤儿文件；validators job 新增追踪卫生门（追踪文件必须非空、不得命中运行期产物模式），带负路径探针。
- **B 拆包**：世代治理族移入新包 `agent/org`，经**注入的壳构造契约**与组合根协作（依既有分层立法，agent 域子包不得 import 根包；盘点证实库外对 `Org*` 导出面零引用、对根包内部耦合仅 `runtimeConfig`×7 与 `buildAgent`×1 三点）；根包保留类型别名与 `TagentAgent` 薄委托，公共 API 面不变。asset_drift 与资源族留根包（无世代耦合）。
- **B' 测试拆分**：三个巨型测试文件按域拆为 ≤600 行的文件（世代集成测试留在根包——它们本质是装配层集成测试，依赖根包未导出面；纯逻辑单测随包走），以 `check_test_merge.sh` 证无损，同位门逐文件核对。
- **C 命名**：`train/`（Python RL 轨迹工具）移至 `scripts/rl/`，引用与文档同步。

## Impact

- Affected specs: `architecture-guardrails`（ADDED：组合根物理边界与追踪卫生）
- Affected code: 根包 11 个生产文件移位 + 测试拆分、`.github/workflows/ci.yml`（validators）、`scripts/`（新增卫生检查）、`train/ → scripts/rl/`、`.gitignore` 微调
- 行为零变更：纯移动 + 接口化；每步 `go build ./...` + 全量 short 绿后提交，`git mv` 保历史
