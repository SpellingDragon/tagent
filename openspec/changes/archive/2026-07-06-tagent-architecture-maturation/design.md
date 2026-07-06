## Context

tagent 的原型 (`prototype/agent.go`) 用 126 行定义了事件驱动 Agent 的最小骨架：eventBus + inputs（投影）+ tools + model + Run/Compact。当前生产实现基于 trpc-agent-go，通过 EventBus + runEventLoop + ContextManager + MemoryPlugin 完成了核心架构。

回归原型三个不变量审视，当前实现存在以下差距：
- **不变量 ③（工具结果回写 bus）** 未完全实现：框架 Runner 工具结果回流到 SessionProjection，但未 Publish 到 EventBus
- **错误处理**：runEventLoop 中 RunFlow 失败只打日志，无重试/退避
- **可观测性**：压缩管道缺乏结构化指标
- **生产韧性**：MemoryStore 原子写入、崩溃恢复、A2A 超时未覆盖
- **回归保护**：三个不变量无自动化测试

约束：
- 不修改 prototype/agent.go（原型是架构契约）
- 不修改 trpc-agent-go 框架代码
- 保持 agent 包不依赖 tool 包的单向依赖
- 保持现有工具（ActionTool/RecallAgent/KnowledgeAgent）核心逻辑不变

## Goals / Non-Goals

**Goals:**
- 补全事件驱动闭环：工具结果 Publish 到 EventBus
- 增强错误韧性：RunFlow 失败重试 + 错误事件发布
- 增加压缩可观测性：结构化指标输出
- 验证 MemoryStore 生产化：原子写入 + 崩溃恢复
- 增加 A2A 调用韧性：超时 + 重试
- 建立 RL 训练闭环验证：TrajectoryRecorder flush + AReaL 集成
- 保护架构不变量：自动化回归测试

**Non-Goals:**
- 不重写 EventBus 或 ContextManager 的核心架构
- 不引入新的外部依赖（如 Prometheus client）
- 不修改 trpc-agent-go 框架的 Runner/Session/Plugin 接口
- 不实现向量搜索（RAG）——这是独立未来工作
- 不重构 ToolRegistry 或 AgentConfig 的配置体系

## Decisions

### D1: RunFlow 失败重试策略 — 指数退避 + 错误事件

**决策**：`runEventLoop` 中 `RunFlow` 失败后，使用指数退避重试（100ms → 200ms → 400ms，最多 3 次）。超过重试上限后，将失败信息封装为 `AgentEvent{Type: "external_input", Source: "error"}` 发布到 EventBus，然后 continue 到下一轮 Pull。

**理由**：
- 直接 continue 会丢失失败上下文，调用方无感知
- 发布错误事件让 `BuildInvocation` 自然跳过（`Source == "error"` 不合并到 user message），但外部监听器可感知
- 指数退避避免模型 API 限速时雪崩

**备选**：
- 直接 panic/退出 Loop → 过于激进，单次失败不应终止 Agent
- 无限重试 → 可能死循环
- 写入 dead letter queue → 过度设计，EventBus 本身就是队列

### D2: 工具结果 Bus 桥接 — onEvent 回调中选择性 Publish

**决策**：在 `ContextManager.RunFlow` 的事件转发循环中，当事件类型为 `action_command` 时，额外 `bus.Publish` 一份 `AgentEvent{Type: "external_input", Source: "tool_result"}`。

**理由**：
- 原型不变量 ③ 要求工具结果回写 bus
- 只桥接 `action_command`（工具执行结果），不桥接 `thinking_plan`（中间推理），避免噪音
- 外部监听器（如 TmuxMonitor 回调链、冥想响应判断）可消费工具结果事件

**备选**：
- 桥接所有事件类型 → 噪音过大，EventBus 会快速填满
- 不桥接，让外部直接读 outputCh → outputCh 是 `<-chan`，外部无法主动拉取

### D3: 压缩指标 — log.Infof + 可选 OTLP span attribute

**决策**：SmartCompressor/Compactor 触发时，通过 `log.Infof` 输出结构化指标（JSON 格式），并设置 OTLP span attribute（如果 trace 启用）。不引入 Prometheus client 等外部依赖。

**理由**：
- tagent 已依赖 `trpc-agent-go/log`，零新增依赖
- OTLP span attribute 是框架已有的可观测性通道
- JSON 格式日志可被日志收集系统（ELK/Loki）结构化解析

**备选**：
- 引入 Prometheus client → 违反"不新增外部依赖"约束
- 自定义 metrics channel → 过度设计
- 只打文本日志 → 难以结构化解析

### D4: FileSegmentStore 原子写入 — tmpfile + rename

**决策**：`FileSegmentStore.StoreEvent` 写入时先写临时文件 `{eventKey}.json.tmp`，完成后 `os.Rename` 为 `{eventKey}.json`。

**理由**：
- `os.Rename` 在同一文件系统上是原子的
- 崩溃时最多留下 `.tmp` 文件，不影响已有数据
- 启动时扫描清理 `.tmp` 残留文件

**备选**：
- WAL（Write-Ahead Log）→ 过重，单文件写入不需要
- fsync + fdatasync → 性能影响大，rename 已足够保证原子性

### D5: AgentToolWrapper 超时 — context.WithTimeout + 1 次重试

**决策**：`AgentToolWrapper.Call` 使用 `context.WithTimeout(ctx, 120s)` 包装调用。失败后重试 1 次（仅远程 A2A 调用，本地不重试）。

**理由**：
- 本地调用失败通常是逻辑错误，重试无意义
- 远程调用失败可能是网络抖动，重试 1 次合理
- 120s 超时覆盖大多数 LLM + 工具执行场景

**备选**：
- 不超时 → 子 Agent 可能挂死
- 指数退避重试多次 → 工具调用场景下延迟过高
- 熔断器 → 过度设计，当前只有少量子 Agent

### D6: 不变量测试 — tests/invariants_test.go

**决策**：新建 `tests/invariants_test.go`，使用 `InMemoryStore` + mock model 验证三个不变量。

**理由**：
- 不变量是架构契约，回归测试是最有效的保护
- 使用 InMemoryStore 避免文件系统依赖
- 测试可直接验证 SessionProjection 内容、MemoryStore 不可变性、onEvent 回流

## Risks / Trade-offs

- **[工具结果 Bus 桥接增加 EventBus 负载]** → 只桥接 `action_command`，单轮 ReAct 循环中工具调用次数有限（通常 <10），不会撑满 256 缓冲
- **[RunFlow 重试可能导致事件重复处理]** → 重试是在 `RunFlow` 内部，`runner.Run` 每次调用是独立的；如果 `runner.Run` 部分成功（已产出部分事件）后失败，重试会重复处理。缓解：重试前检查 `outputCh` 是否已收到 final response，如果是则不重试
- **[错误事件 Source="error" 被 BuildInvocation 跳过]** → 这是设计意图：错误事件不触发模型调用，但允许外部监听器感知。如果未来需要错误重试触发模型，可在 BuildInvocation 中增加 source 过滤逻辑
- **[FileSegmentStore rename 在跨文件系统时不原子]** → 文档中注明 `path` 必须是本地文件系统，不能跨 NFS
- **[A2A 重试可能导致子 Agent 重复执行]** → 仅重试远程 HTTP 调用失败（网络层），不重试子 Agent 逻辑失败
