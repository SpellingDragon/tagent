# 02 任务（孙任务 + verify 三要素）

## 1 红锚

- [ ] 1.1 `TestInvokeAdmitsWhileTurnInFlight`：一次消费进行中到达的第二条消息被受理，且在消费者下一次取批时得到回答 —— 验证：`go test ./tests/integration/host -run '^TestInvokeAdmitsWhileTurnInFlight$' -count=1`
- [ ] 1.2 `TestInvokeDoesNotRewriteSourceIdentity`：输入事实的来源等于调用者身份，不因在途被改写 —— 验证：`go test ./tests/integration/host -run '^TestInvokeDoesNotRewriteSourceIdentity$' -count=1`

## 2 提交即受理

- [ ] 2.1 `Host.Invoke` 改为「提交 → tryConsume」；删除 `acceptInflight` 与两次 `running()` 复核 —— 验证：`go test ./tests/integration/host -run '^(TestInvokeAdmits|TestInflightUserMessageIsAcceptedNotQueued)' -count=1`（后者按新语义迁移）
- [ ] 2.2 `addressGate` 退化为占用原语：删 `inflight` 字段与 `running()`，保留 token 与取消 —— 验证：`go build ./... && go test ./assembly/... -count=1`

## 3 来源身份

- [ ] 3.1 `inputEventOf` 去掉在途身份参数；`Turn.InvocationID` 报调用者身份 —— 验证：1.2 转绿

## 4 内部驱动委托

- [ ] 4.1 `fireNoticeTurn` 改为「提交 + tryConsume」，不再自持 drive —— 验证：`go test ./tests/integration/host -run '^TestEventIntegrity(ReflectionDelivery|Delivery)' -count=1`
- [ ] 4.2 `InjectEvent` 改为「提交 + tryConsume」 —— 验证：`go test ./tests/integration/evolution/... -count=1`
- [ ] 4.3 `SettleDriver` 只保留唤醒与账户确认 —— 验证：`go test ./tests/integration/host -run '^TestSettle' -count=1`

## 5 域内回归

- [ ] 5.1 `assembly` 与 `tests/integration/host` 全绿 —— 验证：`go test ./assembly/... ./tests/integration/host/... -count=1`
- [ ] 5.2 `event-short` 十轮 —— 验证：`python3 scripts/scenarios/run_runtime_scenarios.py --contract golden/runtime/event_integrity_scenarios.json --scope event-short --repeat 10`

## 验收硬门

- 1.1/1.2 双锚入 run-notes（1.1 实现前红、实现后绿）。
- 白名单外零改动：仅 `assembly/host.go`、`assembly/invoke.go`、`assembly/run.go` 与 tests。
- 不新增对外 API：`go doc` 面不变（用 `git diff --stat` + `docs/api` 重新生成对比）。
