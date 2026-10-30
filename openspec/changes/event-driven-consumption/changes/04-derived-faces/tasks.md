# 04 任务（孙任务 + verify 三要素）

## 1 导出配对

- [ ] 1.1 `ChainExporter` 配对改为「输出消费键 ↔ 输入键」 —— 验证：`go test ./features/rl -run '^TestExportPairsByConsumedInputs$' -count=1`
- [ ] 1.2 未消费输入不产样本 —— 验证：`go test ./features/rl -run '^TestUnconsumedInputProducesNoSample$' -count=1`
- [ ] 1.3 既有学习用例按新判据迁移（`tests/integration/learning/chain_export_test.go`）—— 验证：`go test ./tests/integration/learning/... -count=1`

## 2 请求内单快照

- [ ] 2.1 装配取一次 `ToolFace` + 可见技能集合，传给 Tools/ToolOrder/环境块/modelView —— 验证：`go test ./assembly -run '^TestOneSnapshotPerAssembly$' -count=1`
- [ ] 2.2 移除装配内重复调用 —— 验证：`grep -c 'state.ToolFace' assembly/request.go` 为 1（或用等价结构断言）

## 3 域内回归

- [ ] 3.1 `go test ./features/rl/... ./assembly/... ./tests/integration/learning/... -count=1`
- [ ] 3.2 `event-short` 十轮 —— 验证：`python3 scripts/scenarios/run_runtime_scenarios.py --contract golden/runtime/event_integrity_scenarios.json --scope event-short --repeat 10`

## 验收硬门

- 消费者锚：导出走 `rl.NewChainExporter` 正式入口；快照锚走 `assembly.Assemble`。
- 白名单外零改动：`features/rl/export_*.go`、`assembly/request.go`、必要测试。
- 若需改 `runtime/agent/loop.go` 的 `modelViewOf`，必须先与编排者合单（01 白名单）。
