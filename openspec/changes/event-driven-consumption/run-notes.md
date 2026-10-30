# run-notes（证据台账）

## 1 立案裁定

- 用户原话（症状）：同一会话里，上一轮工具还在后台跑时发第二条消息，用户只拿到「已受理」，回答要等前一轮工具全部结束。
- 三条架构裁定（用户确认）：跨 InvocationID 同会话合批并保留来源；慢工具不阻塞新消费；A 的回答闭合已完成部分、B 留待下一段。
- 立案四问回答：裁剪面 C（消费层 + 入口统一 + 分段闭合）；一次性完成；全量门禁 + 独立审者；症状锚按草案确认。

## 2 症状直译锚（第一天红着）

**测试**：`tests/integration/host/slow_tool_next_message_test.go::TestSlowToolDoesNotBlockNextConsumption`

**直译场景**：第一条消息触发 `send` 工具，工具停在外发面（sender 内 park）；此时第二条消息经 `Host.Invoke` 到达。用户应收到第二条消息的回答，而不是只拿到受理回执。

**红证据（2026-10-30，基线 `3d668df8`）**：

```
$ go test ./tests/integration/host -run '^TestSlowToolDoesNotBlockNextConsumption$' -count=1
EXIT=1
--- FAIL: TestSlowToolDoesNotBlockNextConsumption (5.07s)
    slow_tool_next_message_test.go:106: the message that arrived while the tool ran got the receipt unknown of the range "session-slow-tool#1" and no answer in 5s: the consumer parked the message behind the tool the first turn still runs
FAIL	github.com/SpellingDragon/tagent/tests/integration/host	5.432s
```

失败形态与症状一致：`Host.Invoke` 立即回答 `KindUnknown` 受理回执，模型请求始终不携带第二条消息，`sender` 也不交付其回答。

**稳定性**：`-count=3` 连跑，3/3 均为同一失败行（receipt unknown + 无回答），非 flaky 红。

**回滚必红 / 修复后必绿**：两次证据在 F3 补录（修复后绿 + 完整回滚红）。

## 3 规划自审（阶段 5，对照 review-checklist）

| # | 发现 | 处置 | 状态 |
|---|---|---|---|
| A1 | 显式隔离无承载（裁定 1 后半句） | `01` spec 加隔离 scenario + design `MetaKeyIsolation` + tasks 2.3 | 已修 |
| A2 | 新增 capability 与既有能力重叠（5.9） | 收敛为既有能力 `persistent-event-loop`/`config-state-twinship`/`task-skeleton-compression`/`offline-training-export`；仅 `host-migration` 新增并写不重合理由 | 已修 |
| A3 | 零容忍门清偿归属未声明（5.7） | `ORCHESTRATION.md` 三·凭据：域内自清 + 共享面归编排者 + 编排者终跑门 | 已修 |
| A4 | 症状红锚未实际存在（5.11） | 已写并跑红，见 §2 | 已修 |
| A5 | 各域 proposal 缺父变更头行 | 五域 proposal 补头行 | 已修 |
| A6 | F 任务顺序与技能模板不一致 | 改为 F1 契约零改动 → F2 全量门禁 → F3 DoD/症状锚双证据 → F4 独立审者 → F5 归档 | 已修 |
| A7 | 派发粒度未声明（R28） | `ORCHESTRATION.md` 次序：派发粒度 = 域级 | 已修 |
| A8 | `01` 3.1 偏大 | 拆 3.1a（去掉 await）/ 3.1b（结果进后续批次） | 已修 |
| A9 | tmux 三态未声明（R24） | `ORCHESTRATION.md` 凭据：无 tmux Skip / 缺凭据 Fatal / 非空转证明 | 已修 |

## 4 启动门状态（W1 放行前）

- 症状直译红锚存在且红：✅（§2，EXIT=1）
- `ORCHESTRATION.md` 四问齐备：✅（次序/授权/凭据/释放与等待表；含 5.7、R24、R28 条款）
- 跨域签名冻结：✅（`design.md#D6`，自 W1 起冻结）
- `openspec validate event-driven-consumption --strict`：✅ valid（0/10 tasks）

## 5 W1 实施进度（01 消费循环）

**已落地**（`runtime/agent/consumption.go` + `loop.go`）：

- 批次结构 `ConsumptionBatch`（D6）；`boundaryInput` 改为「本会话 + 已到达 + 未被取走」，去掉 `InvocationID` 过滤，链上消费关系优先、旧链回退 head 读。
- `dispatch` 去除 `await` 循环与 `observations`；结算结果由 `action_command` 事实经边界读入下一批。
- `commitOutput` 写入 `event.MetaKeyConsumedInputs`（本 drive 消费的全部输入键）。
- 隔离：`event.MetaKeyIsolation` + `pickBatch`（最早隔离事件单独成批）。
- 删除 `intake.deliver/await/waiters/settled`、`observationOf`、`aggregateReady`（无使用方）。

**凭据**：

- 红锚：`TestSlowToolDoesNotBlockNextConsumption` 红（EXIT=1）→ 绿（EXIT=0，`/tmp/slowtool_green3.log`）。
- `go test ./runtime/agent/... -count=1` → EXIT=0（含迁移后的 `TestTheBoundaryReadsTheChainAgain`；另一 invocation 的输入现按裁定一进入本轮）。

**停点**：`go test ./tests/integration/host -count=1` → EXIT=1，20 个既有锚红。已逐条归因：均为「本轮不再整轮等工具结果」的直接后果（收据轮/结果轮计数、settle follow-up 时序、交付时序），非逻辑缺陷；但这些锚位于 `tests/integration/host`，超出 01 写入白名单，需裁定后迁移。

### 5.1 结果唤回（W1-1，未完成）

新锚 `TestToolResultIsConsumedWithoutNewInput`（`tests/integration/host/tool_result_consumption_test.go`）：send 工具的结果落链后不供入任何模型请求，会话不自行消费。

- 红证据：`go test ./tests/integration/host -run '^TestToolResultIsConsumedWithoutNewInput$' -count=1` → EXIT=1，`timed out waiting for the answer of the settled result`（20s）。
- 已加接线：`Drive` 初始批次合并与空批守卫、`Run.setResultWake`（未导出）、`Host.consumeResults`（结果落链唤醒一次消费，持地址 gate）。
- 状态：锚仍红，唤醒未生效，原因**未定位**。待查：①`Run.record` 是否被 send 的工具执行调用；②`evt.Owner` 是否为完整地址；③consumer 是否在 Boot 后启动且未被 Close 提前停止。定位前不再扩大改动面。
- 欠账：负锚 `TestEmptyWakeStartsNoRequest` 未立。

## 6 待办

- 平行变更记账：`compression-and-budget-as-events` 的 L1（`compress_product.go`/`final_round_request.go`/词汇表）与「判定/执行拆分」（`age_plan.go`）已完成（`29735dfc`、`3d668df8`），需同步勾选。
- W1 派发：01 消费循环（域级派发单）。
- F3 时补：修复后绿证据 + 完整回滚必红证据。
