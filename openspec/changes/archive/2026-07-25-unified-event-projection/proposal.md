# Proposal: unified-event-projection

## Why

异步化反复"失败"的根因不在异步层，而在它脚下的地基：**LLM 输入的装配机制**存在三个结构性裂缝——

1. **投影写入放错了位置**：框架事件的投影 Append 在 RunFlow 消费 goroutine（管线外），与框架 flow 竞速，"BeforeModel 时投影完整"只是碰巧而非构造保证。
2. **装配读回框架消息尾部**:`extractCurrentTurnMessages` 用 `[evt_` 字符串前缀启发式区分"投影的"与"新的"，在投影已投影一切的时代只可能产出重复（本次实证：重复 tool result + 孤儿 tool_call → 模型被迫空响应）。
3. **tool 协议配对**:action_command 渲染为 `role=tool` 引入配对约束，可被压缩（边界切分 call/result)、重复、丢 id 破坏——8 次同类修复（事件流/压缩/配对/空响应）全部落在这一个关节。

同时，三个既有的设计意图一直没有被贯彻到底：plugin 本应就是"事件→存储→投影"的转换点；**action_command 本应是一种特殊 input 事件**（异步任务结果是"通知"，不是协议意义上的 tool result);**事件元数据的注入与解析本应是框架的责任**（目前散落为三处手写的 stringly 约定）。

## What Changes

1. **投影写入统一进插件管线**（同步、有序、与 store 原子同点）。框架对工具结果事件的 completion-wait（RequiresCompletion + AddNoticeChannelAndWait）使"BeforeModel 时投影完整"成为**构造保证**(FIFO + 传递性）。消费侧 onEvent 收窄为纯投递职责（outputCh / 元数据传播 / 冥想计时）。
2. **装配单行化**:`BeforeModel = [system] + render(投影) + 任务看板`，句号。**删除** `extractCurrentTurnMessages`、`filterUser`、session 回显过滤等全部读回启发式。边界变单向：事件流→投影→请求，永不读回。
3. **配对自由的时间线渲染**（贯彻"result 是特殊 input 事件"的设计）:
   - `external_input`(user / task 通知 / monitor / meditation)→ input 事件文本（role=user，带类别前缀）
   - `action_command`（同步工具应答）→ input 事件文本，**文本级携带关联标识**（工具名+短 id)
   - `thinking_plan` → assistant 文本，tool_calls 转文本描述（"调用了 X(args 摘要）")，不保留原生 tool_calls
   - `agent_output` → assistant 文本
   - 历史渲染中**不再出现 role=tool** → 配对概念消失，孤儿/重复/4xx 该类 bug 从构造上消除；压缩任意窗口都不会产生孤儿
4. **事件元数据契约升为框架一等职责**：统一 key 常量 + 注入/解析 API(`event_key`/`partition_id`/`event_type`/`event_summary`/`trigger_source`/`meta_*`)；消费端（example）经框架 API 解析，不再裸读字符串键；"每个投递事件元数据完整"成为可测不变量。
5. **删除 vestigial `agent_output` bus echo**:bus 上 agent_output 被所有消费方过滤，echo 只是空转唤醒；循环本就靠 Pull 等待。
6. **删除 L2 tool 配对修复器**(`message_validate.go`)：配对概念消失后无需修复；保留 L1 投影幂等（Replace/压缩场景仍然需要）。保留 H1（退化空 agent_output 不存储）作为存储卫生。
7. **压缩不再要求配对原子性**（无配对），但摘要必须保留关联标识文本（task id / 工具名），使"通知↔调用"在内容上始终可关联。
8. **流式 partial 守卫**作为新写入路径的设计不变量（未来流式场景）：管线内跳过 partial 事件，只投影聚合事件。

## Capabilities

### New Capabilities
- `event-timeline-rendering`:LLM 输入的装配与渲染契约——投影为唯一装配源；写入路径统一于插件管线；配对自由的时间线渲染规则（含通知类事件类别）;I1-I4 可证伪不变量。
- `event-metadata-contract`:事件元数据（存储标识、路由来源、透传元数据）的框架级注入/解析契约——统一 key、注入点、解析 API、完整性不变量。

### Modified Capabilities
- `conversation-self-heal`：删除"发送前校验 tool 配对"与"畸形消息保守修复"两项需求（配对概念消失）；保留投影幂等需求。
- `persistent-event-loop`：新增"循环不依赖 bus echo 自触发"约束（agent_output echo 删除，循环仅依赖 Pull 等待外部/任务事件）。
- `async-task-execution`:新增"task_settled 为通知类 input 事件"需求（跨回合、非应答、文本级关联标识）。
- `value-driven-compression`：新增"摘要必须保留关联标识文本"要求；压缩边界因配对概念消失而不再受配对原子性约束。

## Impact

- **agent/**:`context_manager.go`(RunFlow 职责收窄、echo 删除、BeforeModel 装配单行化）、`context_compressor.go`(resolveRef 渲染规则重写、extractCurrentTurnMessages 删除）、`session.go`(onEvent 收窄为投递）、`message_validate.go`（删除）、`event_loop.go`(echo 过滤清理）、`smart_compress.go`（摘要保留关联标识）
- **plugin/memory_plugin.go**：写入路径统一（store→project 原子同点）、partial 守卫、保留 H1 空 agent_output 跳过
- **agent/（新）元数据契约**：key 常量 + 注入/解析 API;`event_bus.go` / `task_manager.go` 共用
- **examples/wechat-bot/main.go**：改用框架元数据解析 API
- **测试**：新增 I1-I4 不变量测试（写入统一/时序/渲染合法/边界单向）+ 元数据完整性测试；更新依赖 role=tool 渲染的既有测试；真实 LLM e2e 复跑
- **不影响**:bus 单消费者语义、任务层（spawn/settle/看板/origin baggage)、冥想、RL 轨迹、压缩调度（值密度/预算逻辑不变，仅边界约束简化）
- **BREAKING**（内部）:`extractCurrentTurnMessages`、`repairToolPairing`、agent_output bus echo 被移除；历史渲染不再产生 `role=tool` 消息（对模型行为有正面预期影响，但属于可观察行为变化）
