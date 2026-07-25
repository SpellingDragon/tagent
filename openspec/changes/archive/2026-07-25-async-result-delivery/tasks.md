## 1. 框架：退化空 final 响应抑制（persistent-event-loop）

- [x] 1.1 `agent/context_manager.go` RunFlow：将 echo 守卫 `if outMsg.Content != "" || outMsg.Role != ""` 收紧为 `if outMsg.Content != ""`，空内容 final 不再 echo 回 bus
- [x] 1.2 `agent/context_manager.go` RunFlow：turn 结束若为退化空 final，记一条诊断日志 `[runflow] suppressed empty final (active_tasks=N)`（N 由 taskController.List() 活跃数得出；N=0 即"卡住"信号，仅观察不干预）
- [x] 1.3 新增测试 `agent/empty_final_suppression_test.go`：空 content 的 final assistant 响应 SHALL NOT 产生 agent_output bus 事件；非空 content 的 final 响应 SHALL 正常 echo

## 2. 框架：修复任务看板从未注入的构造顺序 bug（根因，task-registry-and-board）

- [x] 2.1 `agent/context_manager.go`：任务看板 BeforeModel 回调改为**无条件注册**，把 `if cm.taskController != nil` 判断从注册处（构造期，此时恒为 nil）移入闭包**运行时**；`taskController==nil` 时闭包内安全跳过
- [x] 2.2 新增测试 `agent/task_board_injection_test.go`：模拟"ContextManager 构造后才 wiring taskController"，进入含活跃任务的 turn 时 BeforeModel 仍注入看板（回归保护，锁死顺序无关）；无 taskController 时安全跳过不 panic

## 3. 框架：退出任务清理 + 会话资源回收（补齐 async 机制，task-registry-and-board）

- [x] 3.1 `agent/task_manager.go`：新增短 grace TTL（`TaskManagerConfig` 可选字段 + 合理默认，仅覆盖回收 turn 的 `get_task_result` 窗口，非长期保留）
- [x] 3.2 `agent/task_manager.go`：确保所有退出转移都记录 `settledAt`（`applyStatus` 已记；补 `Cancel`/`dead` 路径）
- [x] 3.3 `agent/task_manager.go`：新增 `pruneTerminal()`——**锁内**收集"已退出且超 grace"的任务、从 map 删除并取出其 detector；**锁外**逐个 `detector.Cancel()` 回收资源（goroutine/context/tmux 会话）；存活态（含 alive_detached）永不清；在 `List()`/`Spawn()` 入口调用
- [x] 3.4 `agent/task_manager.go` + `tool/action/`：确保 `detector.Cancel()` 幂等——`funcSettleDetector.Cancel`（context cancel 天然幂等）与 `TmuxSettleDetector.Cancel`（reapOnce+closeOnce 两 sync.Once）在已终止任务上重复调用安全
- [x] 3.5 新增测试 `agent/task_prune_test.go`：退出任务超 grace 后经 List/Spawn 被移除**且 detector.Cancel 被调用**（spy detector 记录）；running/stable/alive_detached 不被清也不 Cancel；grace 内 `get_task_result` 仍可用；重复 prune 幂等不 panic（极短 TTL/可注入时钟，不 sleep 真实分钟）

## 4. 框架：spawn 时捕获发起轮来源上下文为不透明 baggage（async-result-delivery）

- [x] 4.1 `agent/task_manager.go`：`TaskSpec`/`Task` 新增可选字段 `Origin map[string]string` —— 文档标注为"发起轮 invocation metadata 的**不透明快照**（origin baggage），框架填充、任务层不解释"（零值即旧行为）
- [x] 4.2 `agent/task_manager.go`：`Task` 承载 `Origin`（经 `task.Spec.Origin`），在 `Spawn` 构造 Task 时一次性写入（settle 时只读，无数据竞争）；任务执行/结算/relaunch 逻辑 SHALL NOT 读取 `Origin`（信使不路由）
- [x] 4.3 `agent/context_manager.go` RunFlow：注入 spawner 处改为包一层 `originSpawner`（嵌入 TaskController，仅增强 Spawn）—— 其 `Spawn` 在 `spec.Origin == nil` 时填入 `cm.GetInvocationMetadata()` 的**副本**再委托真实 spawner；工具调用点无需改动
- [x] 4.4 新增测试 `agent/spawn_metadata_capture_test.go`：用户轮（带 chat_id）内 Spawn 的 Task 绑定含 chat_id 的 `Origin` baggage 快照；工具未显式提供时被自动填入；显式 Origin 不被覆盖；快照为副本（copy isolation）

## 5. 框架：结算事件携带路由 metadata（async-result-delivery）

- [x] 5.1 `agent/event_bus.go`：`newTaskSettledEvent(task, sig)` 将 `task.Spec.Origin` 逐键拷入 `AgentEvent.Metadata`（复用既有 `extractRootMetadata → SetInvocationMetadata → onEvent 写 meta_*` 管线，不新增管线）
- [x] 5.2 新增/扩展测试 `agent/task_settled_test.go`：绑定 chat_id 的 Task settle → `task_settled` 事件的 Metadata 含 chat_id；`extractRootMetadata([settled])` 能取出 chat_id
- [x] 5.3 测试：未绑定 `Origin` 的 Task settle → `task_settled` 事件正常产生且不含路由 metadata（回归保护）

## 6. 消费端：task 路由 + 丢弃空 final（examples/wechat-bot）

- [x] 6.1 `examples/wechat-bot/main.go`：final response 且 `content==""` 且 `Response.Error==nil` 时**丢弃**（不投递、移除 `content = "(empty response)"` 兜底）
- [x] 6.2 `examples/wechat-bot/main.go`：`switch triggerSource` 新增 `case "task"`（与 `"user"` 合并分支）—— 路由到 `meta_chat_id`；缺 `meta_chat_id` 时记告警并 `continue`（SHALL NOT 落入 default "未知触发源"）
- [x] 6.3 更新 `examples/wechat-bot/main.go` 中 triggerSource 取值注释（补充 `task` 为一等可投递源）

## 7. 验证与归档

- [x] 7.1 `go build ./...` 与 `go vet ./...` 通过
- [x] 7.2 `go test ./agent/ -count=1` 全绿（含新增五组测试；tool/action 的 tmux 失败为环境性，非本变更）
- [x] 7.3 `cd examples/wechat-bot && go build ./...` 通过
- [x] 7.4 `openspec validate async-result-delivery --strict` 通过
- [ ] 7.5 （人工）wechat-bot 实跑：spawn 后台任务后，后续 turn 的上下文**能看到任务看板**（活跃任务列表）；任务完成后结果**投递回原会话**；连续 tool 失败后不再出现 `"(empty response)"`；`task` 触发源不再报"未知触发源"；长跑后任务表不无界增长
- [ ] 7.6 `openspec archive async-result-delivery` 并同步 specs
