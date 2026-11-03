# 09 技术要点

1. **实证门**（D13）：9.1 先复跑既有测量（dispatch 等待/合批），真伪报告入台账后才展开 9.2+。R7 断言成色=⚠️ 待实证，不得在报告中升格为事实。
2. **收敛目标**：`consumer-loop-convergence` 既有条款——"工具派发后不等结果"；慢工具结果以通知/回流进后续决策，结果路由回正确责任范围（Task 归属）。
3. **合批**：同会话多来源输入按既有合批语义（`aggregateReady`，见 docs/wiki/agent/event-flow.md；原计划误写 `mergeBatch`，2026-11-01 W3-C 实证修正）进一次决策；合批不得混淆责任范围（负锚钉住）。
4. **与 02 交互**：send 重入输入是消费循环的普通输入来源之一，进下一次决策恰一次。

文件清单：`runtime/agent/loop.go`、`tests/integration/host|execution`。
风险：等待语义改动可能影响既有 settle 恰好一次约束（`settle_wake_single_drive_test` 回归必跑）；9.1 为假时全域降登记，零实现改动。
