## 1. Phase 0 — TaskManager 核心与探测器抽象（确定性,可离线测）

- [x] 1.1 定义 `Task` 结构与 status 状态机（running/stable/alive-detached/completed/failed/suspect/dead/cancelled）
- [x] 1.2 实现 `TaskManager` 内存 registry + 幂等 spawn 去重（先用命令/spec 精确匹配）
- [x] 1.3 定义 `SettleDetector` 接口 + 通用（goroutine 返回）探测器
- [x] 1.4 实现 sync-wait 窗口原语：`spawn → select{ settle → inline ; after(sync_wait) → ack + track }`
- [x] 1.5 确定性单测：窗口内 settle 内联、超窗 ack + 转后台、幂等去重

## 2. Phase 1 — tmux/ActionTool 接入 + 看板 + task_settled

- [x] 2.0 回归基线：给默认集里 5 个真实 tmux `Call()` 慢测（action_test/tmux_complex）加 `testing.Short()` 跳过；`go test ./tool/action/ -short` 从超时(>120s)降到 ~0.5s
- [x] 2.1 tmux `SettleDetector`：包装现有 `TmuxMonitor`，将其状态映射为 settle `kind`（completed/stable/suspect）
- [x] 2.2 ActionTool **无状态**接入 task 层：`Call` 经 `InvocationFromContext(ctx)` 的 `RuntimeState["task_spawner"]` 取 `TaskSpawner`，用 `TmuxSettleDetector` spawn（inline/ack）；无 spawner 时同步回退保留当前语义；`TmuxMonitor` 改**按会话回调**以移除 ActionTool 的 `waiters` 状态；tagent 在 RunFlow 注入 spawner + 装配 `OnSettle`
- [x] 2.3 `task_settled` 事件类型 + 进入持久循环：空闲时唤醒 Pull、turn 进行中排队（不打断）
- [x] 2.3.1 调优：`DefaultMonitorConfig.Interval` 30s→3s + `sync_wait`=10s（短命令内联、长命令异步；真实 tmux 验证 3.1s 内联返回）
- [x] 2.4 看板渲染：`BeforeModel` 从 registry 重渲染，置于最后 agent_output 之后/当前输入之前；不参与压缩；已处理任务 age-out
- [x] 2.5 LLM 任务工具：`list_tasks`/`get_task_result(id)`/`cancel_task(id)`/`relaunch_task(id)`（即时同步返回，`tool/task` 包，经 `TaskControllerFromContext` 取控制器）；大结果在 task_settled 截断 + `get_task_result` 拉全量（结果留存于 registry，无需 event_key 间接）
- [x] 2.6 **启用** parallel tools（当前框架默认 `false`，需代码改动）：构造 agent 的 LLMAgent 时加 `llmagent.WithEnableParallelTools(true)`；影响评估=全仓 `-short` 回归全绿（16 包，无破坏）；tagent 工具皆无状态/加锁，安全；并行 sync-wait 窗口阻塞 ≈ `max_i min(sync_wait, real_i)`（由框架并发派发 + 各自 Spawn 保证）
- [x] 2.7 确定性单测：settle 三档、task_settled 触发回收 turn、看板不被压缩 + age-out、大结果 ref 拉取

## 3. Phase 2 — 服务型 alive-detached

- [x] 3.1 首次 `stable`（存活）→ 发一次"就绪"通知 → 转 `alive-detached`
- [x] 3.2 `alive-detached` 任务后续输出不再触发回收 turn；仅 `cancel`/进程死亡结束
- [x] 3.3 确定性单测：服务就绪后不刷屏、cancel 结束、看板紧凑常驻显示

## 4. Phase 3（gated）— 子 agent 异步

- [x] 4.1 `AgentToolWrapper` 接入 task 层（settle = `RunFlow` 返回的 finalOutput）+ 启用开关（**默认开/异步**，`SetAsyncDisabled` 回退；用户决策改默认关→默认异步）
- [x] 4.2 保持子 agent 并发隔离契约（`Run` 用局部独立 invBus/invCM/invProjection；`activeBus` 为死代码回退）；无 spawner / 关开关时同步回退
- [x] 4.3 确定性单测（mock）：快子 agent 窗口内内联、慢子 agent 超窗 ack、无 spawner 同步、关开关同步（`-race`）

## 5. Phase 4 — 端到端验证与收尾

- [x] 5.1 真实 LLM 端到端：长命令异步派发 + 回收 turn LLM 友好（看板可见、不重复 tool use、不空回复）
- [x] 5.2 全量回归：`go test ./agent/ -count=1` + `go test ./tests/ -short`（+ 关键真实 LLM 用例）
- [x] 5.3 数据清理：测试用 `t.TempDir()`/mock 不落盘；清理 tmux 残留（2128→0）；**根因修复**：完成/出错(进程真死)时 `TmuxSettleDetector` 回收 tmux 会话，避免长程 agent 累积死会话（验证 4 次真实命令后 0 残留）
- [x] 5.4 更新 README / wiki：任务层、看板、settle 模型、sync-wait 语义
- [x] 5.5 `openspec validate async-task-management --strict` 通过；按 Conventional Commits 分阶段提交
