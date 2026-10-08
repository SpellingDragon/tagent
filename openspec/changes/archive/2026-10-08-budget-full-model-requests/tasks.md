# O3 叶任务（均未执行）

- [x] 3.1 建立完整SDK输入/工具声明的预算基准与真实Runner声明兼容钉测 —— 验证：`go test ./modelutil ./tests -short -run '^TestRequestSnapshot_DeclarationParity$' -count=1`，并保存baseline。
- [x] 3.2 实现S1快照及分项RequestBudget，未知媒体有标记、不漏长工具参数 —— 验证：`go test ./modelutil -run '^TestRequestBudget_FullInput$' -count=1`。
- [x] 3.3 把固定开销与可压缩历史预算接入唯一Compress调用，零预算不回退旧值 —— 验证：`go test ./agent/compress -run '^TestContextCompressor_FixedOverhead$' -count=1`。
- [x] 3.4 实现摘要总时限、取消感知收流及纯工程回退，票据/旧叙事保持 —— 验证：`go test ./modelutil ./agent/compress -run '^TestSummaryDeadline_' -count=1`。
- [x] 3.5 编排者完成最终门禁/恢复提示两阶段消费/iterator惰性与热参同组接线 —— 验证：`go test ./agent -short -run '^TestExecutionGate_FinalBudget$' -count=1`。
- [x] 3.6 编排者完成config/root构造及独立bot接线，验证唯一快照可供O5消费 —— 验证：`go test -short -race ./modelutil ./agent/... . -count=1`；bot模块另跑short全包。
- [x] 3.7 运行真实模型长schema/参数与压缩摘要用例及before/after构造基准 —— 验证：`TAGENT_REQUIRE_REAL_MODEL=1 go test ./tests -run '^TestRealModel_RequestBudget$' -count=1 -json`；`go test ./tests/offline_bench -run '^$' -bench '^BenchmarkRequestAssembly$' -benchmem -count=5`。
- [x] 3.8 交编排者同步预算精度/unknown、摘要超时及构造边界文档与API —— 验证：`bash scripts/lint.sh && bash scripts/check-openspec.sh`。

> 编排者核销（W1）：3.1 的真实管线钉测以 `tests/decision_capture_framework_parity_test.go::TestDecisionCapture_FrameworkAttribution` 落地（attribution 达 model ctx、存储对象=模型原 Response/ID 保留，EXIT=0）；3.2 证据见 /tmp/tagent-w1v/m1.log（modelutil ok 0.428s，8 用例）。

> 编排者核销（W2）：/tmp/tagent-w2/o3b/{red2,final_verify}.log（145 PASS=两包全量顶层用例、FAIL 0）；复验同绿。裁决已处置：拒发阈值 fixed≥maxTokens 维持、完整时间线维持、BudgetLine 不动、cardMaxChars 除数不并源、历史媒体计价列后续观察；3.5–3.8 归 W3。

> 编排者核销：3.5=C2（execution_gate 双路拒发+peek 不消费+同组热参，FinalBudget 5 子用例变异红→绿）；3.6=C2/C1b 全链（bot 模块绿、config 透传、face 搬运断言 TestFaceCarryForward）；3.8=D③行4/9。3.7 真模型+构造基准并入本 runbook。

> 编排者核销（补测 2026-10-08）：真模型场景=acceptance5 全 pass；构造基准=BenchmarkRequestAssembly 同树对照（tests/offline_bench/request_assembly_bench_test.go），判决与数据 /tmp/tagent-w4/bench_full.log 末节：+12.8µs/请求、38KB 瞬态，接受（完整性为既定目标，legacy 漏计面即被修对象）。
