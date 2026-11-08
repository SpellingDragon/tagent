# Design: runtime-simplicity-features（deferred）

## D1 来源与转移表（叶 → 原 09 编号 → 能力 → 目标文件 → 阻塞归因）

全部叶逐字符沿用 09 的编号与验证行，便于与原账目对齐；阻塞归因来自 09 的 §8/§10 现场登记，本节不改其结论，只把它升为独立变更的重入前提。

| 叶 | 能力 delta | 目标文件（实证状态） | 阻塞归因 |
|---|---|---|---|
| 0.1 缺陷基线见证 | 无 | `tests/integration/upgrade/baseline_witness_test.go`（目录 MISSING） | 验收目录不存在；需先建目录，与流式变更 P1 同批 |
| 0.2 受影响主规格清单 | 无 | 本变更 `design.md` | 原命令用未登记 change 名 `runtime-simplicity-upgrade`，见 D2 |
| 2.2 loop 内生效参数合并 | `request-evidence` | `runtime/agent/loop.go`（EXISTS，`withEffectiveConfig` 已落） | 代码已落，缺 `tests/integration/upgrade` 冻结验收面；`runtime/agent/**` 归他单 |
| 2.4 按引用读回 + 摘要校验 | `request-evidence` | `assembly/request.go`（EXISTS） | `assembly/**` 归他单 |
| 2.5 读取面口径声明 | `request-evidence` | `features/rl/{capture,line}.go`、`docs/api` | 实质已完成（09 已勾并附 I6 凭据），见 D3 |
| 4.2 关键指标补齐 | `runtime-observability` | `features/telemetry/metrics.go`（MISSING） | 组件侧文件待建，且三处生产者（请求终态/订阅丢弃在 `assembly/run.go`、`runtime/agent`；token 用量在 `api/model`）全在白名单外 |
| 4.3 采集损失与义务查询 | `runtime-inspection` | `assembly/host_diagnostics.go`（EXISTS） | 属 `assembly/**` |
| 5.4 真实消费者读取 | `offline-training-export` | `training/tests/test_consumer.py`（`training/` MISSING） | 目录与 `uv` 依赖链在本仓库不存在 |
| 6.2 删除旧形状门 | 无 | `scripts/lint.sh`、`.github/`、`features/rl/skeleton_assert.go` | `scripts/**`、`.github/**` 归门禁单 |
| 6.3 文档分区落地 | 无 | `docs/**` | 归文档单 |
| F1–F4 | 无 | 本变更 `tasks.md` | 编排者亲验；F4 归档需明示授权 |

## D2 0.2 的验证命令须换 change 名

09 原文为 `openspec validate runtime-simplicity-upgrade --strict --no-interactive`，但该名从未在 CLI 登记（`openspec list --json` 只有 `hot-update-and-purity-closure`、`event-driven-consumption`、`online-rl-training`、`post-training-data-loop`，现加本变更与 `runtime-streaming-increments`），原文命令不可复跑。本变更的 0.2 改用本变更自身名：`openspec validate runtime-simplicity-features --strict --no-interactive`。这不是缩小验收面，而是把指向空名的命令改指真实对象。

## D3 2.5 是 09 的重复条目

09 的 tasks.md 有两条同文 2.5：一条 `- [x]`（附 I6 凭据：`go test ./features/rl ./api/model` EXIT=0、`gen_godoc --check` 输出 77 packages、四处注释改为"运行时请求快照"），一条 `- [ ]`（漏勾的重复条目）。处置：完成事实留在 09 的已勾条目，本变更的 2.5 只承接"复核"语义——开工时先复跑两条命令，若仍绿则无需改代码，若因后续改动转红则在此收口。不把已完成工作降格为未做的 deferred。

## D4 状态：deferred 的机械含义

- `tasks.md` 全为 `- [ ]`；本变更在 `openspec list` 中可见但**不计入** `hot-update-and-purity-closure` 的完成度，不占派发单写面。
- 冻结验收面：每条叶的验证行按 09 原文保留（正锚点名 + 负锚具名 + 消费者锚）。开工时若发现命令指向不存在的目录（如 `tests/integration/upgrade`），先建目录再跑，不得以"零匹配 exit 0"冒充通过。

## D5 重入前提（逐叶申请写面，不做一次性放开）

1. 目录/文件面：`tests/integration/upgrade/`、`training/`、`features/telemetry/metrics.go` 三处不存在，必须先建再验。
2. 写面面：`assembly/**`、`runtime/agent/**`、`scripts/**`、`docs/**`、`training/**` 按叶逐条申请，理由与被触碰文件写进派发单。
3. 用户明示"特性叶开工"。

## D6 被否方案

- 把特性叶留在 09（否：09 已闭合并证，留则一级变更归档被未开工叶无限期拖住；开放数虚高）。
- 用 `--skip-specs`/缩水验收面把特性叶"勾掉"（否：谎报；验收面冻结，缩水即重开）。
- 为 deferred 变更造占位实现或空测试（否：结构自证，第二使用方不存在）。

## 风险与回退

风险在 `openspec validate --strict` 对 deferred 变更的解读：本变更 tasks 全未勾，`openspec list` 会显示 in-progress，可能被误读为"在飞"。回退＝若用户裁定 deferred 变更不应出现在活跃列表，删除本目录并在 09 恢复原登记（09 的转移注记同步回滚）。
