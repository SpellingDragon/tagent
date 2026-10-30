# 01 技术设计

## 文件清单

- `runtime/agent/loop.go`：批次取用、`dispatch` 解耦、`boundaryInput` 判据、`ConsumptionFact` 记录。
- `runtime/agent/loop_test.go`：既有锚迁移。
- 新增 `runtime/agent/consumption.go`：批次结构与取用辅助（若行数可控，可并入 loop.go）。

## 技术要点

### 1. 可消费事件的判据

一次批次由「本会话、已到达、未被此前消费取走」的事实组成：

- `external_input`：用户输入（含通知材料）；
- `action_command`：工具结果事实（结算后落链）；
- `feedback` 且 `trigger_source=task`：通知材料。

沿用 `intake.read` 作为「已被取走」的记账（键去重），**不再用 `InvocationID` 过滤**。`startsRange` 的语义从「开启 range」收窄为「可驱动模型消费的事件」；历史边界 `endedAt` 推进逻辑保持由 `admitsRange` 决定，二者不再混用（03 负责段判据）。

### 2. `dispatch` 解耦

- 派发后收集 receipts，**不再进入 `await` 循环**；`observations` 留空。
- 工具结果作为未来批次的事件，由下一轮 `boundaryInput` 取到。
- 未完成工具不影响本轮退出判定：退出判据只看「本批次是否已被消费」与收尾轮。

### 3. 冻结批次

`messages` 在请求构造时固定；批次内事件按到达顺序进入，不在请求中途追加。下一轮重新取批。

### 4. 消费关系

`commitOutput` 在 `agent_output` 事实的 `Meta` 写入本次请求携带的输入键（逗号分隔，`event.MetaKeyConsumedInputs`）。该字段是 03 段判据的唯一来源；`rl.DecisionRecord.InputEvents` 同步写入同一集合，供训练导出（04）对齐。

### 4.1 显式隔离

`event.MetaKeyIsolation`（共享面，编排者写入 `api/event`）：事件声明需要独立上下文时置位；批次取用时遇到隔离事件，单独成批（不与同会话其他事件合并），隔离结束后恢复正常合批。隔离 MUST 由事件声明，MUST NOT 由入口调用编号推定。

### 5. 退出与收尾

- 一次消费结束条件：本批次已全部进入请求，且模型产出终态；工具结果不回灌本轮。
- 收尾轮语义不变（预算/轮数）；`final_round.request` 事件化由并行变更推进，本域不接线。

## 验证

- 正锚：`TestSlowToolDoesNotBlockNextConsumption`（慢工具未返回时第二条消息得到回答）。
- 负锚：`TestUnarrivedEventIsNotConsumed`（尚未落链的事件不得进入本批）与 `TestOneEventIsConsumedOnce`（同一事件不重复进入两批）。
- 消费者锚：上述两锚均经 `assembly.Host.Invoke` 正式入口驱动，不使用手工补接线。
- 域内：`go test ./runtime/agent/...` 全绿；`event-short` 十轮。

## 风险与回退

- 既有锚（`TestTheBoundaryStartsAtTheRangeHead`、`TestTheBoundaryReadsTheChainAgain`、`TestMessageReachesTheNextDecisionOfAInFlightTurn`）按新语义迁移，禁止删除断言。
- 回退：单文件还原 `loop.go`/`consumption.go`；症状锚回滚后必红即证据。
