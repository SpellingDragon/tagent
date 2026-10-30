# 05-hosts — 宿主迁移

父变更：`event-driven-consumption`（依赖 02 的入口语义）。

## 边界与依赖

- **依赖**：02 的入口语义（提交即受理、`KindUnknown` 表示已受理）。
- **被依赖方**：无（终端用户可见面）。
- **接口面**：`examples/wechat-bot` 的 intake 与交付、`examples/cli-host` 的调用循环。
- **禁止事项**：不把宿主侧的排队/锁重新引入为「会话门」；不改 `assembly/*`；不改 `runtime/agent/*`。

## Why

宿主当前按旧语义工作：

- wechat-bot 的 intake 锁从「整个 turn」降级为「受理段」后，仍依赖 `KindUnknown` 判读为「已受理、稍后回答」；
- cli-host 假设 `Host.Invoke` 返回的 `Turn` 就是本次输入的答案。

入口改为「提交即受理」后，宿主需要按新语义判读，并把「等待回答」交给交付面，而不是假设同步返回。

## What Changes

1. wechat-bot：`KindUnknown` 一律视为已受理；回答由交付面投递；不因 `KindUnknown` 放弃或重发。
2. cli-host：一次输入一次调用；`KindUnknown` 时等待交付面或打印「已受理」提示，不重复提交。
3. 端到端症状锚落在 wechat-bot 的真实装配路径上（非手工补接线）。

## Risk

- 用户可见行为变化（异步化）需要与 Symptoms 锚一起验收；回滚时宿主回到旧判读。
