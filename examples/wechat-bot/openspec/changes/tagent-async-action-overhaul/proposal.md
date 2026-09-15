## Why

今晚白盒审计实证：tagent 异步 action 对长驻任务存在系统性误判——60s 静默被 Stable-settle 判定为"完成"后，任务层 Cancel→reap→kill-session 把仍在运行的进程误杀，KillSession 里 os.Remove(pipeFile) 同时拆掉了审计现场；结算卡片对 Stable/Completed 均渲染 "completed"，外部观察者无从分辨。根因：现有模型假设"任务=等退出"，长驻进程（server/watcher/ REPL）在此模型中是异物。

## What Changes

- **A1 settle 语义**：任务层对 SettleStable 只投递静默通报（∞ alive-detached），不 Cancel/reap、继续跟踪；仅终态（Completed/Error）才结束任务并 reap。落点 `agent/task/task_manager.go` watch/emitBackground/applyStatus 链路（677 附近 SettleSignal 消费）。
- **A2 结算卡片按 kind 标注**：`agent/event_bus.go` settleMarkerAndStatus（~L118）已有 ∞/⚠/✓/✗ 四态映射，扩展为渲染 静默通报/已完成/疑似假死 + elapsed + 进程存活信息，杜绝 Stable 渲染成 completed 的误读。
- **A3 pipe 文件延迟删除**：`tool/action/tmux_executor.go` KillSession（~L253）不再无条件 os.Remove(pipeFile)；非终态 detach 时归档到 `.tagent-workspace/tool-output/`，保留现场。
- **B1 模式原语**：TmuxCreateOptions 增加 Mode: oneshot(默认)/resident/interactive（现 IsInteractive 字段 tmux_executor.go:118/150/241，tmux_monitor.go:619/657/663/689/695）；resident 会话不触发 Stable 结算、不做 fake-dead、不自动 reap，仅意外死亡（Completed/Error）时报。
- **B2 命名注册表**：action 工具支持可选 name 参数，常驻会话按名注册（内存 + 落盘 JSON），供 P3 重挂。
- **B3 peek/send/stop 原语**：peek（增量读 pipe 文件 + offset + strip ANSI）、send（send-keys 注入）、stop（TERM→KILL 阶梯 / Ctrl-C），经 action 工具可选参数扩展暴露。
- **C1 pattern watch**：spawn 声明 watch 正则，goroutine 增量 tail pipe 文件，命中→唤醒（±5 行上下文），5s 合并窗口、每分钟唤醒上限、计数摘要。
- **C2 liveness probe**：spawn 声明 probe 命令（如 curl healthz），周期探活，连败 N 次才报警；resident 零输出不算异常。
- **D1 跨重启重挂**：注册表落盘 + agent 启动对账，tmux session 仍在的 resident 重挂 monitor + pipe-pane（pipe 文件续写）。
- **D2 泄漏防护**：无人认领 resident TTL 清扫 + 并发 resident 数上限。
- **D3 pipe 文件 10MB 轮转**。
- **E 验证与发布**：go build ./... + go vet 全绿；go test ./tool/... ./agent/task/... 全绿（先读 tests/async_*_test.go 三件契约：async_result_delivery_e2e_test.go / async_result_routing_test.go / async_task_e2e_test.go）；examples/wechat-bot 本地构建成功；读 restart-maintenance.sh 确认触发机制（cron 每分钟 flock 保险 + healthz 门控）后触发保险链重启，healthz 探活成功即闭环。

## Impact

- 框架：`agent/task/task_manager.go`、`agent/event_bus.go`、`tool/action/tmux_executor.go`、`tool/action/tmux_monitor.go`、`tool/action/action_tool.go`、`tool/action/settle.go` + 新增 resident 原语/注册表/事件订阅/重挂文件。
- 应用：`examples/wechat-bot`（构建验证 + 经 restart-maintenance.sh 保险链重启，SIGTERM 前告知用户短暂离线）。
- 行为变更对既有用户可见：Stable settle 不再终结任务、结算卡片状态词变化、pipe 文件保留策略变化。
- 约束：破坏性变更前对受影响文件留快照（.bak 或 git stash 前置）；不改 60s 静默唤醒本身；不动 pipe-pane 捕获链路；不混 quiet_timeout 假死检测。
