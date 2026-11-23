# Design · 04 放行

## D1 证据顺序

1. 冻结候选：源码、测试、注释、规格增量、golden、CI 文件全部停止改动。
2. 04.401 生命周期与故障矩阵。
3. 04.402 完整矩阵与 CI 接线；任一步失败按原文归因（产品/夹具/环境/接线）。
4. 04.403 独立只读审分轮，逐条裁定，修复后再审。
5. F1 承接与契约断言 → F2 全量门禁 → F3 DoD → F4 归档。

## D2 已知 CI 红处置

| 失败 | 归因入口 | 处置 |
|---|---|---|
| `TestRetryLeavesPresentedRangeAtTheLatestRound` 等三项 | 02.204 任务 apply 后重跑 | 产品问题修产品；锚过期则按签发重表达 |
| `TestBootWallClockRejectsUnattributedGrowth` | 04.402 成本面 | 先判墙钟/计量分母，再决定修产品或判据 |
| `TestSymptomAnchorPerGenerationCostFlatOverALongRun` | P4 / 04.402 | 与 1.3× 判决并列，不静默放宽 |
| `TestScenario03ToolReceiptAndBackground`、`TestNativeConversationRoundTrip` | 正式入口回归 | 重跑＋邻片定性，区分负载 flake 与产品 |
| event-integrity-short (6) | 对应 shard 原文 | 重取该 shard，禁止整包跳过 |
| archive-preflight evidence-binding exit 1 | 证据绑定 | 修 CI 产物链，不改判据 |
| archive-preflight spec-hygiene exit 2 | 缺 OpenSpec CLI | CI 安装固定版本后重跑 |

## D3 停止条件

- 任一红项无法归因到产品/夹具/环境三类之一：停，不进入 F。
- 放宽判据、删除作业或跳过用例才能绿：停，回设计门并呈报取舍。
- 独立审非空 frontier：修后重审，不宣告放行。

## D4 签发区

填写冻结候选指纹、全矩阵命令与选择集、三 module 身份、CI 作业映射、独立审范围、回滚触发与未查面。
