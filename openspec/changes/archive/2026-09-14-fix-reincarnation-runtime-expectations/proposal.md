# fix-reincarnation-runtime-expectations

## Why

2026-09-13 的转世（hot-swap）演练暴露了三个缺陷，其中两个是框架级、一个阻断消息回复：

1. **P0 崩溃**：`pruneTerminal` 无判空解引用 nil detector，**每轮模型调用 / 每次工具调用**都会踩到。
   当日两次实测 panic（18:40:04 经 `List()`、18:50:27 经 `Spawn()`），用户消息回复直接失败。
2. **P1 配置死锁**：plan 子 agent 只覆盖 `model` 未覆盖 `provider`，回退全局 `deepseek` →
   `glm-5.3` 被发给 DeepSeek 端点 → 400。plan 工具（openspec 规划/报账）全程不可用。
3. **P1 运行预期缺口**：转世后的"应该恢复什么"没有成文预期。实测两条记忆恢复路径中
   projection 重建为 no-op（无 compaction 快照锚点即静默空投影），且 nil-probe 任务
   （plan/subagent）永不回收、永久挂看板并挡住同名 re-spawn。

## What Changes

- `pruneTerminal` 对 nil detector 判空（持 `t.mu` 读，兼顾与 `Resume` swap 的竞态）。
- plan 子 agent 显式 `provider: zhipu`。
- 定义并落地**转世运行预期**：启动后应恢复的三类状态（任务看板 / 会话锚点 / 记忆投影）
  各自的预期行为、失效降级与可观测证据；补 projection 无快照时的 fallback 重建。

## Impact

- Affected specs: `task-registry-rebuild`（新增）, `event-sourced-projection`, `async-task-execution`
- Affected code: `agent/task/task_manager.go`, `examples/wechat-bot/tagent.yaml`,
  `agent/projection_rebuild.go`
