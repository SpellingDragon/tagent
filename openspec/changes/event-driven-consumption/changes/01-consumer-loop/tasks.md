# 01 任务（孙任务 + verify 三要素）

## 1 红锚先行

- [x] 1.1 写出症状直译红锚 `TestSlowToolDoesNotBlockNextConsumption`：慢工具未返回时，同一会话第二条消息经 `Host.Invoke` 得到回答 —— 验证：`go test ./tests/integration/host -run '^TestSlowToolDoesNotBlockNextConsumption$' -count=1`，红证据（EXIT=1）与转绿证据（EXIT=0）均入 run-notes
- [ ] 1.2 写出两条负锚：`TestUnarrivedEventIsNotConsumed`（未落链事件不进批）、`TestOneEventIsConsumedOnce`（同一事件不进两批）—— 验证：`go test ./tests/integration/host -run '^(TestUnarrivedEventIsNotConsumed|TestOneEventIsConsumedOnce)$' -count=1`，后者应随实现转绿、前者保持绿

## 2 批次取用

- [x] 2.1 `boundaryInput` 判据改为「本会话 + 已到达 + 未被取走」，去掉 `InvocationID` 过滤，保留来源字段 —— 验证：`go test ./runtime/agent/... -run '^TestTheBoundary' -count=1`（既有锚按新语义迁移后绿）
- [ ] 2.2 批次冻结：请求构造后不再追加；下一轮重新取批 —— 验证：新增 `TestBatchIsFrozenAtRequestBuild`，`go test ./runtime/agent -run '^TestBatchIsFrozenAtRequestBuild$' -count=1`
- [ ] 2.3 显式隔离：`MetaKeyIsolation` 置位的事件单独成批，不与同会话事件合并 —— 验证：`go test ./runtime/agent -run '^TestIsolatedEventDrivesItsOwnBatch$' -count=1`

## 3 工具解耦

- [ ] 3.1a `dispatch` 去掉 `await` 循环：receipts 进当轮 —— 验证：`go test ./runtime/agent -run '^TestDispatchReturnsWithoutWaiting$' -count=1`
- [ ] 3.1b 结果进入后续批次：工具结果作为 `action_command` 被下一批取到 —— 验证：`go test ./runtime/agent -run '^TestToolResultEntersNextBatch$' -count=1`（负锚：本轮 transcript 不含结果观察）
- [ ] 3.2 退出判据调整：未完成工具不阻塞退出 —— 验证：1.1 红锚转绿，`go test ./tests/integration/host -run '^TestSlowToolDoesNotBlockNextConsumption$' -count=1`
- [ ] 3.3 结果落链唤醒会话消费：`Run.record` 落链后唤醒该会话一次消费（`setResultWake` + `Host.consumeResults`），空批不产生模型请求 —— 验证：`go test ./tests/integration/host -run '^TestToolResultIsConsumedWithoutNewInput$' -count=1`（当前红：20s `timed out waiting for the answer of the settled result`）；负锚 `TestEmptyWakeStartsNoRequest`（待立，未落地即本叶未完成）

## 4 消费关系

- [ ] 4.1 `commitOutput` 在 `agent_output` Meta 写入 `MetaKeyConsumedInputs`（本请求携带的输入键，逗号分隔）—— 验证：`go test ./runtime/agent -run '^TestOutputRecordsConsumedInputs$' -count=1`
- [ ] 4.2 `rl.DecisionRecord.InputEvents` 与 Meta 同源 —— 验证：`go test ./features/rl/... -run '^TestDecisionRecordInputsMatchOutputMeta$' -count=1`

## 5 域内回归

- [ ] 5.1 既有锚迁移（`TestTheBoundaryStartsAtTheRangeHead`、`TestTheBoundaryReadsTheChainAgain`、`TestMessageReachesTheNextDecisionOfAInFlightTurn`）按新语义改写，不删断言 —— 验证：`go test ./runtime/agent/... ./tests/integration/execution/... ./tests/integration/host/... -count=1`
- [ ] 5.2 `event-short` 十轮 —— 验证：`python3 scripts/scenarios/run_runtime_scenarios.py --contract golden/runtime/event_integrity_scenarios.json --scope event-short --repeat 10`

## 验收硬门

- 1.1 红证据（实现前）+ 转绿证据（实现后）双入 run-notes。
- 负锚 1.2 两条均绿。
- 消费者锚：1.1/1.2 均经 `Host.Invoke` 正式入口。
- 白名单外零改动：`git diff --stat` 仅含 `runtime/agent/*` 与新增测试文件。
