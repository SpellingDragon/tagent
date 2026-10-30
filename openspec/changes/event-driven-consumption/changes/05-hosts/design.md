# 05 技术设计

## 文件清单

- `examples/wechat-bot/wechat.go`（intake 与交付判读）
- `examples/wechat-bot/smoke_test.go`（端到端症状锚）
- `examples/cli-host/*`（调用循环）

## 技术要点

### 1. wechat-bot

- `intake` 保持「只圈受理段」的锁粒度，不引入会话门。
- `KindUnknown` 判读为已受理：不判失败、不重发；回答由 `deliverFresh` 按 `baseline` 之后的助手产出投递。
- 若一次调用就地消费（`KindCompleted`），仍按既有路径即时投递，避免重复。

### 2. cli-host

- 交互循环每次输入一次 `Host.Invoke`；`KindUnknown` 时提示「已受理，回答稍后送达」，不重复提交。
- 保持 stdin 阻塞读取；不做客户端排队。

### 3. 端到端症状锚

`examples/wechat-bot` 冒烟新增：

- 慢工具工具（可控闸门）在途时，用户第二条消息经真实渠道路径进入，并在闸门放行前收到针对第二条消息的回答；
- 第一条回答闭合后，第二条不出现在「已完成历史」查询中（调用投影读面断言）。

## 验证

- 正锚：`TestWechatSlowToolDoesNotBlockNextMessage`（冒烟，`-tags smoke`）。
- 负锚：`TestWechatNoDuplicateDeliveryOnKindUnknown`（不重发、不重投）。
- 消费者锚：真实 `wechat.Bot` + `assembly.Boot` 装配路径。
- 域内：`cd examples/wechat-bot && go test -tags smoke ./... -run 'TestWechat' -count=1`。

## 风险与回退

- 冒烟用例依赖 tmux 与本地装配；在 CI 上以 `event-short` 场景路径为准。
- 回退：还原 `wechat.go`/`cli-host`；症状锚（宿主级）回滚后必红。
