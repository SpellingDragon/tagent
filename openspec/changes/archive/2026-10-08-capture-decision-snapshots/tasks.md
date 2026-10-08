# O5 叶任务（均未执行）

- [x] 5.1 用锁定依赖真实Runner钉住响应ID/ctx/工具结果关联与两agent隔离；失败即停精确关联实施 —— 验证：`go test ./tests -short -run '^TestDecisionCapture_FrameworkAttribution$' -count=1`；编排者持有跨包测试写权。
- [x] 5.2 实现v2结构与SDK请求/声明深拷贝，复用O3快照并保持旧模式 —— 前置：O3.2；验证：`go test ./rl -run '^TestTrajectoryCapture_RequestSnapshot$' -count=1`。
- [x] 5.3 实现响应序列/终态保真、取消/Iter惰性与单条字节上界 —— 验证：`go test ./rl -run '^TestTrajectoryCapture_StreamFidelity$' -count=1`。
- [x] 5.4 实现在途/队列总字节、单run磁盘/句柄上界，独立丢失计数及可确认FlushAndWait/manifest —— 验证：`go test -race ./rl -run '^TestTrajectoryCapture_LossAccounting$' -count=1`，含disk_limit与16句柄上界子用例。
- [x] 5.5 编排者集成调用scope、精确响应匹配和提交后fact-link；无ID/冲突显式unbound —— 前置：O5.1、O1.6、O2.6、O3.6；验证：`go test ./tests -short -run '^TestDecisionCapture_EndToEnd$' -count=1`。
- [x] 5.6 接线新配置、共享writer关闭与私有权限，完成跨agent/热更/重试回归 —— 验证：`go test -short -race ./rl ./plugin ./agent . -count=1`；新增TestTrajectoryCapture_Configuration必须PASS。
- [x] 5.7 本地真实模型执行nonce工具往返，检查实际采集声明/参数/结果/归属及开关成本 —— 验证：`TAGENT_REQUIRE_REAL_MODEL=1 go test ./tests -run '^TestRealModel_DecisionCapture$' -count=1 -json`；`go test ./rl -run '^$' -bench '^BenchmarkTrajectoryCapture$' -benchmem -count=5`。
- [x] 5.8 交编排者同步SDK边界、可选trace、丢失/封账与配置文档，登记不得推断wire/策略版本 —— 验证：`bash scripts/lint.sh && bash scripts/check-openspec.sh`。

> 编排者核销（W2）：5.1 钉测 EXIT=0；5.2–5.4 红→绿+变异自证 /tmp/tagent-w2/o5/*；复验 rl ok（含代理自报 -race 2.216s）。字段差异裁决：partition_id 保持 string、顶层 response_id 与 capture_scope 接受、manifest 平铺接受、v1 endpoint 脱敏归 5.6 config 处置。5.5–5.8 归 W3。

> 编排者核销：5.5=tests/decision_capture_e2e_test.go（EndToEnd 2 子用例：call_id 盖入/不串/unbound 语义，C3）；5.6=C1b 装配+config 校验（Configuration 用例+权限断言）；5.8=D③行6（v2 形态/条件 trace/训练导出节）。5.7 真模型+开销账并入 runbook。

> 编排者核销（W4）：DecisionCapture 真机 pass（响应身份直证 bound、request_digest/owner 非空、nonce 往返）；开销账=C4 200x 双档（3057→13247ns/op、1317→4571B/op）+ acceptance5 封账 PendingBytes/MaxPending 入账（verify 14 项绿）。
