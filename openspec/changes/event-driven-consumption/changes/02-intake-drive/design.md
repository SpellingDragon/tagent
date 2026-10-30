# 02 技术设计

## 文件清单

- `assembly/host.go`：`Invoke`、`gateOf`（占用原语化）、`fireNoticeTurn`、`driveLocked` 拆分。
- `assembly/invoke.go`：`inputEventOf` 不再接受在途身份参数。
- `assembly/run.go`：`InjectEvent` 委托。
- 对应 tests（`tests/integration/host/*`）。

## 技术要点

### 1. 提交即受理

`Host.Invoke`：

1. 校验地址与 sender；
2. 铸 `InvocationID`（调用者身份）；
3. 提交输入事实（无条件）；
4. `tryConsume`：会话消费者空闲则本调用驱动一次消费并返回其 `Turn`；忙则返回 `Turn{InvocationID: 本次身份, Terminal: KindUnknown}`（已受理，结果由消费者后续产生）。

`Turn.InvocationID` 仍报调用者身份，不报「谁在跑」的 range——关联字段不再承担归属转写。

### 2. 占用原语

`addressGate` 退化为「本会话正在消费」的占用原语（保留 token 语义与 `lockAddressContext` 的取消行为），删除 `inflight` 字段与 `running()` 读取。占用只决定「本调用能否就地驱动」，不决定「是否受理」。

### 3. 来源身份

`inputEventOf(req, msg, invocationID, now)`：`invocationID` 只来自调用者（或无则新铸），不由 gate 提供。工具结果、通知等下游事实的来源由各自提交点写入。

### 4. 内部驱动委托

- `fireNoticeTurn`：提交通知事实（保持 `noticeInvocation` 幂等身份）→ `tryConsume`；不再自己 `driveLocked`。
- `InjectEvent`：提交 → `tryConsume`；返回值语义不变（返回其消费的 `Turn` 或 `KindUnknown`）。
- `SettleDriver`：只保留「唤醒 + 账户确认」职责；不成为会话总调度。

### 5. 交付与回执

- 用户 turn 的交付仍按 `baseline` 之后的助手产出投递；`KindUnknown` 时本次不投递，由实际消费的 turn 投递。
- 通知回执（`Consume`）由消费方在通知被实际携带进请求后写入，替代「同步驱动成功」的旧证据。

## 验证

- 正锚：`TestInvokeAdmitsWhileTurnInFlight`（在途时第二条消息被受理且最终得到回答）。
- 负锚：`TestInvokeDoesNotRewriteSourceIdentity`（输入事实的来源保持调用者身份）。
- 消费者锚：`Host.Invoke` 正式入口 + `tests/integration/host` 既有链路；`examples/wechat-bot` 冒烟在 05 验收。
- 域内：`go test ./assembly/... ./tests/integration/host/... -count=1`；`event-short` 十轮。

## 风险与回退

- `driveLocked` 拆分影响 settle 账户与交付读取；先迁 `Invoke`，再迁 `fireNoticeTurn`/`InjectEvent`，每步可独立回滚。
- 回退：还原 `host.go`/`invoke.go`/`run.go` 三文件；症状锚回滚后必红。
