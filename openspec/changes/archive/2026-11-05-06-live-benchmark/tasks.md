# 真实任务对照：孙任务

- [x] 1.1 夹具与验收器共享包（先行，W3 之前置）：`bench/` 下任务夹具 12 个（问答/文件/exec/多轮各 3）+ 机读验收器 + 任务接口；**包布局定稿即把 `bench/`、`bench/tasks/`、`bench/runners/`、`bench/metrics/`、`bench/report/` 登记 `golden/runtime/architecture.json`（packages+edges；无 bench/bench）**；**本包先行入 main，06 其余与 07 并行消费它**；**`bench` 根包必须导出可 import 的任务/验收器 API（非仅 `_test.go`）**；**bench 新导出类型（`Task`/`Verifier` 等）在 `golden/runtime/architecture.json` 的 `concept_bindings.bindings` 登记：绑定 canonical concept + kind（按 D1 判据，接口=port）＋非 superseded 主 spec 锚 `live-benchmark`** ——`TestBenchTasksAllVerifiable`（每任务验收器对正/反例判定正确）—— 验证：`go test ./bench -v -run 'TestBenchTasksAllVerifiable' -count=1` PASS + `python3 scripts/gates/check_runtime_architecture.py --implementation` exit 0（默认模式）+ `python3 scripts/gates/check_concept_budget.py --json` 的 `errors` 为空。
- [x] 1.2 harness 自测（scripted 模式，零真实配额）：`TestBenchHarnessScriptedEndToEnd` —— 验证：`go test ./bench -v -run 'TestBenchHarnessScriptedEndToEnd' -count=1` PASS。
- [x] 1.2a 子进程协议（真实路径）：`TestRunnerProtocolSubprocess`——真实起本域新建的微型 main `examples/wechat-bot/cmd/bench-bridge` 子进程，经其行式 stdin 协议交付 1 个任务并回传结果与指标（复用 wechat-bot 模块既有装配与工具面；任务信封与指标回传由 runner 侧编解码）—— 验证：`go test ./bench -v -run 'TestRunnerProtocolSubprocess' -count=1` PASS。
- [x] 1.3 基线 runner：trpc-agent-go 最小循环+指标；负锚 `TestBaselineRunnerRejectsInvalidTask` —— 验证：`go test ./bench -v -run 'TestBaselineRunnerRejectsInvalidTask' -count=1` PASS。
- [x] 1.4 tagent runner + **DSH 适配（不分期，硬要求）**：`TestRunnersShareTaskInterface`（三方接口一致性，**并断言任务能力映射被三方工具面覆盖（映射表登记为证）；tagent 侧断言对象=`examples/wechat-bot/cmd/bench-bridge` 声明的工具面**）+ **`TestDSHRunnerExecutesRealTask`（DSH 经本机 checkout 真实执行至少 1 个任务、产出可验收产物——不是只编译）** —— 验证：`go test ./bench -v -run 'TestRunnersShareTaskInterface|TestDSHRunnerExecutesRealTask' -count=1` **期望 PASS 行数=实际执行的测试数（DSH 缺席 skip 时期望数随减，并在输出中计数 skip 行=1）**；三方齐备由 1.5 硬断言。
- [x] 1.5 真实对照 run（硬断言）：三方×12 任务×3 重复；`TestRunDirContainsAllThreeRunners`（run 目录断言**三方结果文件齐备**——缺任一方即红，硬要求可失败化）+run-id+metrics JSON+五指标（**送达率口径统一为发送方渠道确认**：tagent 分子=桥接内渠道 Send 回执，DSH=发送 API 成功，基线=任务终态并注明口径差；**人工介入次数含 `breakdown.approval_calls`/`breakdown.blocked_reports`/`total` 分项，jq 断言分项和=总**；**run 记录含 `status` 字段（completed/skipped/blocked）**）；无 ZAI_API_KEY：**run 级（运行级）=Skip 记账（不阻断 harness 与 W3 其余）；change 级（变更级）收尾仍无凭据=blocked 法定停点上报（不结项）** —— 验证：`go test ./bench -v -run 'TestRunDirContainsAllThreeRunners' -count=1` PASS+jq 断言五指标字段存在且 `breakdown.approval_calls + breakdown.blocked_reports == total`+调用计数>0；或 run 级 Skip 记账 + change 级 blocked 记录入台账（两层分开记）。
- [x] 1.6 域门禁：`go test ./bench -v -count=1` 全绿 + product-surface 基线同步（golden）+ **concept budget 门自清**：`python3 scripts/gates/check_concept_budget.py --json` 的 `errors` 为空（1.1 的 bench 导出类型已绑定 `live-benchmark` spec 的 concept）—— 验证：exit 0 + 基线 diff 入台账 + concept budget `errors: []`。

## scenario ↔ 测试映射（F32）

| spec scenario | 测试/凭据 |
|---|---|
| 同任务集三方执行 | 1.5 run-id+五指标 JSON + run status 字段 + TestRunnersShareTaskInterface |
| 工具面等价 | TestRunnersShareTaskInterface（能力映射覆盖） |
| 子进程协议 | TestRunnerProtocolSubprocess（1.2a） |
| DSH 真实执行 | TestDSHRunnerExecutesRealTask（1.4） |
| 无凭据 | run 级 Skip 记账；change 级收尾 blocked 停点记录（不结项） |

> **验证约定（r4 F14）**：本文件所有 `go test -run` 验证命令默认附加 `-v`；“用例数=N”=输出中 `--- PASS:` 行数；零匹配（ok [no tests to run]）视为空转不算通过。
