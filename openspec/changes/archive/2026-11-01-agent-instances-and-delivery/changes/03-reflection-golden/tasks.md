# 03 reflection-golden 孙任务

验证约定：I4 锚**红锚先立**（先写失败 golden、红因命中"水位回退/重复消费"，再补夹具使其绿）；断言用事件计数与状态读数，不基于墙钟（R5）。

- [x] 3.1 golden 夹具：脚本模型驱动的反思循环夹具（节奏下限压缩、水位读数接口、资源计数器） —— 验证：`go test ./tests/integration/evolution -run '^TestFixtureReflectionLoop$' -count=1`；负锚 `fixture_without_model_calls_fails`（夹具失真即红）
- [x] 3.2 golden 主体（I4 锚）：30–50 轮反思循环断言——水位单调不回退、零重复消费、零资源泄漏（goroutine/文件句柄计数）、重启后水位续接 —— 验证：`go test ./tests/integration/evolution -run '^TestInstancesSymptomReflectionGolden$' -count=1 -timeout 600s`（I4 红→绿双证据）；负锚 `watermark_regresses_is_red`、`duplicate_consumption_is_red`（咬证变异）
- [x] 3.3 跨会话投递逐目标恰一次：反思产出经 send 投多目标，账目/事实计数逐目标恰一次（02.4 后升级账目直读） —— 验证：`go test ./tests/integration/evolution -run '^TestReflectionGoldenMultiTarget$' -count=1`；负锚 `duplicate_target_delivery_is_red`
- [x] 3.4 golden 入门：接入 CI 常规测试面（`go test` 常规位或 tests/gates 门），墙钟分钟级确认（R32 标注） —— 验证：`go test ./tests/integration/evolution -run 'Golden' -count=1` 全绿且墙钟记录入报告；`go build ./...` 绿
