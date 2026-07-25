## Context

tagent 的持久循环是 `for { Pull(bus); RunFlow }`。一个 turn 内：

- `RunFlow` 消费框架 `eventCh`，对每个事件：`onEvent`（写投影）→ 转发到 `outputCh`（消费端 main.go 在此收事件并投递）→ 若是 final response，再把 assistant 内容 echo 成 `agent_output` **发回 bus**（被 `BuildInvocation` 过滤，用于防自触发）。
- 消费端（wechat-bot main.go）从 **`outputCh`** 收事件，`evt.IsFinalResponse()` 为真则按 `trigger_source` + `meta_chat_id` 路由投递。
- 后台任务经 `spawn` 启动，detach 后由 `OnSettle → newTaskSettledEvent → bus.Publish` 回收成新 turn（`trigger_source=task`）。

已核实的两个缺陷（详见 proposal），本设计解决其"怎么改"。一个易错点必须先厘清：**消费端拿到 final 事件是通过 `outputCh`，不是 bus echo**。因此"空响应抑制"是两层，而非只改 bus echo 守卫。

**架构定位（经确认）**：tagent 以**单一 agent + 单一 loop + 单一共享 `wechat-session`** 服务全体微信用户——这是**有意的**单一共享助理设计（跨用户共享记忆/历史）。在此模型下 `chat_id` 不是会话边界，而是**每事件必须携带的路由标签**，供单一 `outputCh` 消费端扇出。既有 user 路径已遵守"每事件携带路由 metadata"这一不变量；task 路径合成 `task_settled` 时把它丢了——**本变更是把该不变量补齐到 task 路径（统一化），不是新增功能**。

## Goals / Non-Goals

**Goals:**
- 退化空 final 响应（`content=""` 且无 `tool_calls`）不产出、不投递、不污染 bus。
- 后台任务结算的最终答复能路由回发起会话（`chat_id` 从 spawn 存活到输出）。
- 工具层对路由 metadata 完全无感；改动集中在 RunFlow 边界与消费端。

**Non-Goals:**
- **不**为"空+空闲（真卡住）"引入框架兜底 nudge/重试（决策 A1：视为模型/prompt 质量问题）。
- 不改 `isFinalResponse` 的结构语义（仍是"assistant 且无 tool_calls = turn 结束"）；只收紧 echo/投递守卫。
- 不改任务看板注入机制（已每轮 BeforeModel 自动注入，无需显式拉取）。
- 不引入新的 metadata 传播管线——复用既有 `extractRootMetadata → SetInvocationMetadata → onEvent 写 meta_*`。
- **不**改动"全体用户共享单一 `wechat-session`/记忆"的架构——经确认这是**有意**的单一共享助理设计。per-user agent 隔离（每用户独立 loop/session，届时可删掉整个 per-event 路由层）是另一量级的架构变更，明确**不在本次范围**；本次在既有共享架构内把 `chat_id` 路由不变量补齐。

## Decisions

### D1：空响应抑制分两层，各司其职
- **框架层（RunFlow echo 守卫）**：把 `if outMsg.Content != "" || outMsg.Role != ""` 收紧为 `if outMsg.Content != ""`。原守卫因 `Role` 恒为 `"assistant"` 而形同虚设。空内容不再 echo 回 bus。
- **消费端层（投递策略）**：final response 且 `content==""` 且无 `Response.Error` 时，**丢弃**（不投递、不再兜 `"(empty response)"`）。

**为什么两层**：消费端经 `outputCh` 收 final 事件，仅改 bus echo 守卫**挡不住投递**；仅改消费端则 bus 仍被空 echo 污染（多一次 no-op 循环迭代 + 投影里留空 assistant）。两层分别根治"投递"与"bus 污染"。
**备选**：只在 RunFlow 层从 `outputCh` 转发中过滤退化 final。否决——`outputCh` 同时承载 interim 事件（typing 指示等），在转发处按 final 内容判定会把"turn 结束"信号一并吞掉，边界更脆。分层更清晰。

### D2："等 monitor 事件恢复"是既有语义的自然结果，不新增等待机制
抑制空 echo 后，空 turn 结束 → bus 无新事件 → `Pull` 自然阻塞 → 直到 `task_settled`（或用户输入）到达才恢复。这正是用户要的"等待 monitor 事件再继续"。**无需**新增显式等待状态或让模型主动调 `get_task_result`——**前提是看板确实被注入**（见 D6：此前因构造顺序 bug，看板从未注入，该前提一直为假）。

### D6：修复"任务看板从未注入"的构造顺序 bug（根因级）
核实发现：看板 BeforeModel 回调的注册被**构造期守卫** `if cm.taskController != nil`（context_manager.go:339）挡住——而 `cm.taskController` 在 `NewContextManager` 返回**之后**才于 agent.go:344 赋值。故守卫在构造期恒为 nil → **看板回调从未注册 → LLM 从未看到后台任务看板**。这使 D2 依赖的"看板已自动注入"前提此前为假，也是空/困惑响应与重复 spawn 的一个根因级诱因。

- **修复**：**无条件注册**该回调，把 `taskController != nil` 判断移入闭包**运行时**（届时已 wiring）；运行时判定对未来 wiring 顺序变化天然健壮。
- **备选（改 wiring 顺序 / 经 config 传入 taskController）**：需改 `ContextManagerConfig` + 构造签名，diff 更大且仍是"靠顺序正确"的脆弱保证。否决，选运行时判定。
- **注意**：`RunFlow` 内 `WithTaskSpawner` 的 taskController 判断在运行期（line 548），不受影响——所以 async 任务能 spawn（ack 正常回），只是看板缺失。与日志现象一致。

### D3：spawn 时快照"发起轮来源上下文"为不透明 baggage（保持任务层纯粹）
结算发生在脱离发起 turn 的时刻，届时已无法回溯发起会话身份。故在**还处于发起 turn、`cm` 持有 invocation metadata 的 spawn 时刻**快照该上下文，随任务一路带到 settle。

**关键约束**：任务执行层（`Kind`/`Desc`/`Key`/`Relaunch`）只关心"怎么跑、怎么判结算"，SHALL NOT 知道"投递路由/`chat_id`"。因此该快照以**不透明 baggage** 承载——任务层只忠实"spawn 收下、settle 原样交还"，从不读取/解释其内容（OpenTelemetry baggage 模式）。**任务层是信使，不是路由器。**

- `TaskSpec`/`Task` 新增可选字段 `Origin map[string]string`，文档明确其为"发起轮 invocation metadata 的**不透明快照**，框架填充、任务层不解释"（零值即旧行为）。
- RunFlow 注入 spawner 处**包一层** metadata-capturing wrapper：其 `Spawn` 在 `spec.Origin == nil` 时填入 `cm.GetInvocationMetadata()` 的**副本**再委托真实 spawner。工具继续裸调 `spawner.Spawn(spec, detector)`，无感。
- `Task` 承载 `Origin`（spawn 时一次性写入，settle 时只读 → 无数据竞争）。

**为什么用不透明 baggage 而非"路由字段"**：若把它命名/使用为"`chat_id`/路由 metadata"，任务层就被耦合进投递关切——这正是"加法 muddies 架构"。以不透明 baggage 承载则耦合消失（不知情=不耦合），且与既有 `Key`（不透明幂等串）风格一致。
**为什么在 RunFlow 包 wrapper 而非让 TaskManager 读 context**：捕获属于"turn 边界职责"，与现有"在 RunFlow 注入 spawner"模式一致；TaskManager 与来源上下文彻底无关，职责更纯。
**否决的备选（agent 侧表）**：在 agent 层维护 `map[taskID]origin` + settle 时查表。否决——引入有生命周期/泄漏风险的状态，而 baggage 透传是无状态的、更简单。

### D4：结算事件携带 metadata，复用既有传播管线
`newTaskSettledEvent(task, sig)` 把 `task.Origin` 逐键拷进 `AgentEvent.Metadata`。此后**完全复用既有管线**：event loop `extractRootMetadata([settled])` 读出 `chat_id` → `SetInvocationMetadata` → `onEvent` 写 `StateDelta[meta_chat_id]` → 输出事件带齐 `meta_chat_id` + `trigger_source=task`。零新增管线。

### D5：`task` 作为一等可路由触发源（消费端）
消费端 `switch triggerSource` 新增 `case "task"`：路由逻辑与 `"user"` 一致（投递到 `meta_chat_id`）。一个后台任务结算即是在兑现用户的原始委托，语义上等同用户可见回复。
**格式**：默认原样投递；是否加"[后台完成]"类前缀列为次要开放项（D 见 Open Questions）。

### D7：退出任务的状态驱动清理 + 会话资源回收
`TaskManager.tasks` 从不删除已退出任务 → 内存/会话资源无界泄漏 + 每轮 `List()`/去重 O(N)（看板修好后遍历成本更显形）。
- **保留模型（状态驱动，不止靠 TTL）**：稳态 registry **只保留存活任务**——running/stable/alive_detached/suspect（`alive_detached` 是仍存活的服务，须可 cancel/查询/看板可见，故保留）。已退出任务（completed/failed/cancelled/dead）被清理，触发以**退出状态**为主；**短 grace TTL 仅**用于保证回收 turn 内 `get_task_result` 可用 + "从未 surface" 的兜底。
- **资源回收（关键，用户强调）**：清理一个退出任务时 SHALL 先调用其 `detector.Cancel()` 释放底层资源（funcSettleDetector 的 goroutine/context；TmuxMonitor 的轮询与 **tmux 会话**），再从 map 删除。**仅 `delete(map)` 不释放资源 = 静默泄漏，比原来更糟**。`Cancel()` SHALL 幂等（对已 settle/cancel 的任务重复调用安全）。
- **并发**：`pruneTerminal` 在**锁内**收集 victims 并从 map 删除、取出各 detector，在**锁外**逐个 `Cancel()`（可能做 IO/杀会话）——绝不持 `tm.mu` 期间阻塞。`List()`/`Spawn()` 入口惰性触发。
- **清理安全**：settle 结果已随 `task_settled` 入 projection/历史；grace TTL ≫ "settle→回收 turn→读取" 延迟，回收 turn 内 `get_task_result` 必可用，只清久远退出任务。
- **否决**：①settle 即删——破坏回收 turn 的 `get_task_result`；②后台 sweeper goroutine——引额外并发/停机清理面，惰性已足够。

## Risks / Trade-offs

- [抑制空 echo 可能掩盖"模型卡住"] → 决策 A1 已明确：空+空闲不兜底。缓解：诊断日志在 turn 结束且无内容时记一条 `[runflow] suppressed empty final (active_tasks=N)`，N=0 即"卡住"信号，供运行时观察，但不自动干预。
- [metadata 快照包含非路由键（user_name 等）] → 影响可忽略：map 很小；消费端只取需要的键。快照用副本，避免与后续 turn 的 `currentMetadata` 覆盖相互影响。
- [多事件同轮 Pull 时 metadata 合并覆盖] → 既有 `extractRootMetadata` 行为（later override earlier），本变更不改；task turn 通常单事件，边界可接受。
- [消费端丢弃空 final 可能让 typing 指示器悬挂] → main.go 既有 60s typing 超时兜底；空 final 无内容可展示，丢弃是正确的。

## Migration Plan

纯增量，无数据迁移：
1. 框架改动（D1 框架层 / D3 / D4）+ 消费端改动（D1 消费端层 / D5）一并落地。
2. `TaskSpec.Origin` 零值即旧行为，既有调用点无需改。
3. 回滚：还原 RunFlow echo 守卫、`TaskSpec.Origin`、`newTaskSettledEvent` metadata 拷贝、消费端两处即可，互不耦合。

## Open Questions

- **D（次要）**：`task` 触发源投递是否加可见前缀（如"[后台任务完成] "）以便用户区分主动回复与异步回收？默认不加，落地时可一行开关。
- 是否把"退化空 final 不更新 meditation idle 锚点"一并纳入？倾向**不纳入**（保持 `isFinalResponse` 语义单一，scope 收敛），如需另开变更。
