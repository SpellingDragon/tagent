# 05 任务（孙任务 + verify 三要素）

## 1 宿主判读

- [ ] 1.1 wechat-bot：`KindUnknown` 判为已受理，不判失败、不重发 —— 验证：`cd examples/wechat-bot && go test -tags smoke ./... -run '^TestWechatNoDuplicateDeliveryOnKindUnknown$' -count=1`
- [ ] 1.2 cli-host：`KindUnknown` 时提示已受理、不重复提交 —— 验证：`cd examples/cli-host && go test ./... -count=1`

## 2 端到端症状锚

- [ ] 2.1 冒烟：慢工具在途时第二条消息在闸门放行前得到回答 —— 验证：`cd examples/wechat-bot && go test -tags smoke ./... -run '^TestWechatSlowToolDoesNotBlockNextMessage$' -count=1`
- [ ] 2.2 冒烟：第一条回答闭合后第二条不在已完成历史中 —— 验证：同上用例内的投影断言（或独立 `TestWechatUnconsumedInputNotInClosedTurn`）

## 3 域内回归

- [ ] 3.1 `examples/wechat-bot` 与 `examples/cli-host` 全量 —— 验证：两目录各自 `go test ./... -count=1`（冒烟另跑 `-tags smoke`）
- [ ] 3.2 `event-short` 十轮 —— 验证：`python3 scripts/scenarios/run_runtime_scenarios.py --contract golden/runtime/event_integrity_scenarios.json --scope event-short --repeat 10`

## 验收硬门

- 消费者锚：症状锚走真实 `wechat.Bot` + `assembly.Boot` 装配，禁止手工补接线。
- 白名单外零改动：仅 `examples/wechat-bot/*`、`examples/cli-host/*`。
- 若发现宿主需要新的公开 API，停并回编排者（可能触发签名变更）。
