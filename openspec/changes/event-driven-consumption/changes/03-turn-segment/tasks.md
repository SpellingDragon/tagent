# 03 任务（孙任务 + verify 三要素）

## 1 红锚

- [x] 1.1 `TestUnconsumedInputStaysInProgress`：链序 `A → B → 对 A 的回答`，段只闭合 A，B 留在进行中段 —— 验证：`go test ./memory/replay -run '^TestUnconsumedInputStaysInProgress$' -count=1`，当前**必须红**
- [x] 1.2 `TestConsumedInputClosesItsSegment`：A 被其回答闭合（消费键命中） —— 验证：`go test ./memory/replay -run '^TestConsumedInputClosesItsSegment$' -count=1`

## 2 判据改造

- [x] 2.1 `splitSegments` 按 `pending ∩ consumed` 闭合；`pending \ consumed` 留在进行中段 —— 验证：1.1/1.2 双绿
- [x] 2.2 工具结果不开启新段，只作过程材料 —— 验证：`go test ./memory/replay -run '^TestToolResultJoinsSurroundingSegment$' -count=1`

## 3 进行中段与折叠

- [x] 3.1 进行中段判据改为「有未消费输入」，恒 L0、永不折叠 —— 验证：`go test ./memory/replay -run '^TestInProgressSegmentNeverFolds$' -count=1`
- [x] 3.2 `PlanByAge`/`FoldByAge` 在含未消费输入的段上不产产物 —— 验证：`go test ./memory/replay -run '^TestPlanSkipsInProgressSegment$' -count=1`

## 4 兼容与重建

- [x] 4.1 旧链缺 Meta 时按链序判据解释并标记 legacy —— 验证：`go test ./memory/replay -run '^TestLegacySegmentWithoutMeta$' -count=1`
- [ ] 4.2 重启重建：`seedReplay` 后同链同段 —— 验证：`go test ./tests/integration/host -run '^TestSegmentsRebuildAfterRestart$' -count=1`

## 5 域内回归

- [x] 5.1 `memory/replay` 全绿 + golden 场景迁移 —— 验证：`go test ./memory/replay/... -count=1 && go test ./tests/integration/acceptance/... -count=1`
- [ ] 5.2 `event-short` 十轮 —— 验证：`python3 scripts/scenarios/run_runtime_scenarios.py --contract golden/runtime/event_integrity_scenarios.json --scope event-short --repeat 10`

## 验收硬门

- 1.1 红证据 + 转绿证据双入 run-notes。
- 规格同步：`task-skeleton-compression` 的「以 agent_output 为段边界」改写为「按实际消费关系闭合」。
- 白名单外零改动：仅 `memory/replay/*` 与 specs。
