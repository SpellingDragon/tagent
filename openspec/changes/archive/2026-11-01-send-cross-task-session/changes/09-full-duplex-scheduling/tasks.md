# 09 full-duplex-scheduling 孙任务

- [x] 9.1 断言实证（首叶，门）：复跑既有慢工具/合批测量（`dispatch` 等待、`next` 调度目标测试），输出真伪报告入台账 —— 验证：`go test ./tests/integration/... -run 'SlowTool|CrossSourceBatch' -count=1 -json`（测试名以仓内实际为准，逐字符对齐后冻结）；报告含测量原始输出；实证为假→后续叶全部转登记 —— **已实证（R7 为假）**：字面命令在 HEAD 净树 d4aa4793 EXIT=0（`TestSlowToolDoesNotBlockNewInput`/`TestCrossSourceBatching` PASS），声称锚 `TestSchedule*` 类全仓 0 命中，故 9.2–9.4 按登记收敛；凭据=W3-C 实证报告（2026-11-01）`/tmp/send-cts-W3C/report.md`
- [x] 9.2 慢工具不阻塞新输入（N6 锚）——**登记**：9.1 实证 R7 为假，行为已由既有 `TestSlowToolDoesNotBlockNewInput`（3/3 绿+真实 Boot 锚）覆盖；声称锚 0 命中，展开=重复覆盖（W3-C 报告 /tmp/send-cts-W3C/report.md）
- [x] 9.3 多来源合批 —— **主体登记 + 补强叶 9.3b**：既有 `TestCrossSourceBatching` 只证到"同段下一次请求累积"，缺"单请求同批"证据与责任范围负锚 —— 9.3b：补合批强度测试（单请求同批断言 + `batch_never_mixes_responsibility` 负锚） —— 验证：`go test ./tests/integration/execution -run '^TestCrossSourceBatchStrength$' -count=1`；负锚 `batch_never_mixes_responsibility` —— **已交付**：`tests/integration/execution/batch_strength_test.go`（W3-D 报告 /tmp/send-cts-W3D/report.md）
- [x] 9.4 重入与消费面交互 —— **登记**：依赖未达（send 重入面彼时未入 main），解锁=02 已入 main（现已入），但 9.1 判 R7 为假后本叶降为回归确认：现有 `TestSendReentersAgentTask`/`TestScheduleReentryInputMerge` 语义由 02 域验收覆盖，重复展开无增量
