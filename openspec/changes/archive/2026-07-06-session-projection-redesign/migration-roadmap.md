# Session-Projection Redesign: 代码迁移路线图

> 本文档给出从当前实现到设计目标的分阶段迁移路径。每一阶段都包含具体任务、验收标准和回滚策略。

## 阶段一：止损纠偏（Stop-the-Bleed）

**目标**：解决 Session 无限增长和子 agent 循环失控问题，恢复系统可运行性。

### 任务 1.1: Session 投影数据结构落地

- **内容**：在 `agent` 包引入 `[]memory.EventReference` 作为 Session 投影。
- **实现方案**：
  - 在 `agent/session_projection.go` 定义 `SessionProjection` 结构体，封装 `[]memory.EventReference` 和读写锁。
  - 在 `AgentLoop` 中新增 `projection *SessionProjection` 字段（暂时与 `session *session.Session` 并存）。
  - `makeOnEventCallback` 在 MemoryPlugin 持久化后，将 `event.Event` 转换为 `memory.EventReference` 追加到 `projection`。
- **关键依赖**：`memory.EventReference`、`memory.FullEvent`、`plugin.MemoryPlugin`。
- **验收标准**：
  - 单测验证 onEvent 后 `projection` 长度 +1。
  - 单测验证 `projection` 中元素为 `EventReference` 而非完整 `event.Event`。
  - 集成测试验证长会话后 projection 内存占用显著下降。

### 任务 1.2: Preprocessor 从 EventReference 构建 messages

- **内容**：修改 `Preprocessor.Process` 从 `SessionProjection` 读取并构建 messages。
- **实现方案**：
  - `Preprocessor` 构造函数注入 `memory.MemoryStore`。
  - 遍历 `EventReference` 列表：最近 `KeepRecentTasks * 2` 条通过 `MemoryStore.GetEvent` 拉取完整 Content；更旧的引用直接使用 `EventSummary`。
  - `injectEventKeyPrefixesFromSession` 改为从 `EventReference` 注入前缀。
- **关键依赖**：任务 1.1、MemoryStore 接口。
- **验收标准**：
  - `preprocessor_test.go` 通过 mock MemoryStore 验证按需拉取逻辑。
  - 注入前缀格式仍为 `[evt_KEY|type] content`。

### 任务 1.3: Compact 机制实现

- **内容**：实现 Compact 清理 `SessionProjection`。
- **实现方案**：
  - 在 `agent/compact.go` 实现 `Compact(projection []EventReference) []EventReference`。
  - 策略：按任务边界切分 EventReference（通过 event_type 判断 agent_output 为边界），保留最近 N 个完整任务；旧引用替换为一个 `context_compress` 类型的 summary reference（内含被压缩的 EventKey 列表）。
  - 在 `Preprocessor.Process` 中 SmartCompress 后仍超限时调用 Compact。
- **关键依赖**：任务 1.1、任务 1.2。
- **验收标准**：
  - 单测验证 Compact 后 projection 长度减少且保留最近任务。
  - 单测验证 Compact 不调用 MemoryStore 写操作。
  - 集成测试验证长会话不无限增长。

### 任务 1.4: MaxToolIterations 默认值修正

- **内容**：主 agent 默认 50，子 agent 默认 10。
- **实现方案**：
  - 修改 `agent/tagent_agent.go` 和 `config.go` 的默认值常量。
  - 在 `TagentConfig` 中区分 entry/sub-agent 的默认值；或在 `tagent.go` 的 `buildAgent` 中根据 agent 名称设置。
- **关键依赖**：无。
- **验收标准**：
  - 单测验证 entry agent 默认 50，子 agent 默认 10。
  - 子 agent 测试验证 10 次 tool_calls 后强制 final response。

### 阶段一回滚策略

- 保留旧的 `session.Events []event.Event` 路径作为 fallback，通过 feature flag 切换。
- 如果 Compact 导致信息丢失，可关闭 Compact 并调大 MaxToolIterations。

---

## 阶段二：语义对齐（Semantic Alignment）

**目标**：消除 AgentLoop session copy，修正 dispatch 时机，让所有输出回写 bus。

### 任务 2.1: 消除 AgentLoop session copy

- **内容**：让 AgentLoop 不再手动 append event 到自己的 session copy，统一由 onEvent 追加 EventReference。
- **实现方案**：
  - 删除 `agent/agent_loop.go:184-188` 和 `agent/agent_loop.go:391-399` 的手动 append。
  - `makeOnEventCallback` 直接操作 `AgentLoop.projection`（或 TagentAgent 持有的 projection）。
  - 评估 `SessionService` 的必要性：Runner 仍需要 session 进行 plugin 生命周期管理；但 projection 不再依赖 `sessionSvc.AppendEvent`。
- **关键依赖**：任务 1.1。
- **验收标准**：
  - 单测验证 AgentLoop 不再维护独立 copy。
  - 集成测试验证 onEvent 后 Preprocessor 能读取到同一 projection。

### 任务 2.2: 修正 dispatch 时机

- **内容**：将 tool_use dispatch 从 Pull 后移到 `handleResponse` 中。
- **实现方案**：
  - 删除 `agent/agent_loop.go:159-167` 的 Step 1 dispatch 循环。
  - 在 `handleResponse` 的 tool_calls 循环内，`bus.Publish(tool_use)` 后立即 `dispatchToolUse(ctx, tc)`。
  - 调整 `shouldCallModel` 逻辑：batch 中只要存在未处理的 external_input 就调用模型；tool_use 事件在 handleResponse 中已被消费，不单独触发模型。
- **关键依赖**：无。
- **验收标准**：
  - `agent_loop_test.go` 验证 tool_use 在 handleResponse 中立即 dispatch。
  - 集成测试验证子 agent 调用延迟降低。

### 任务 2.3: 所有输出回写 bus

- **内容**：final response 也回写 bus（作为 external_input/agent_output）。
- **实现方案**：
  - 在 `emitAgentOutput` 中除了发送到 outputCh，也 `bus.Publish(NewExternalInputEvent("agent_output", ...))`。
  - 在 Preprocessor 中过滤掉 source="agent_output" 的事件，避免自我触发。
- **关键依赖**：任务 2.2。
- **验收标准**：
  - 单测验证 final response 被 publish 到 bus。
  - 验证不会导致无限循环。

### 阶段二回滚策略

- dispatch 时机改动较大，可先保留 Pull 后 dispatch 的代码分支，通过配置切换。

---

## 阶段三：深层结合（Deep Framework Integration）

**目标**：复用 trpc-agent-go 框架的 Flow / ContentRequestProcessor / FunctionCallResponseProcessor / BeforeModel，减少 ~1000 行自建代码。

### 任务 3.1: 调研框架 Flow 接口

- **内容**：确认 `trpc-agent-go` 的 `LLMAgent`、`Flow.Run`、`ContentRequestProcessor`、`FunctionCallResponseProcessor`、`BeforeModel` 的接口和使用方式。
- **关键依赖**：外部框架源码。
- **验收标准**：输出框架接口映射文档。

### 任务 3.2: 用 Flow.Run 替换 AgentLoop.Run 内部循环

- **内容**：`StartLoop` 中 Pull 事件后，构造 `agent.Invocation` 并调用 `LLMAgent.Run(Flow.Run)`。
- **实现方案**：
  - 保留 `EventBus` + `StartLoop` 作为 tagent 独有层。
  - 将 `onEvent` 注册为框架 `Plugin.OnEvent`。
  - 将 `SmartCompressor` 注册为框架 `BeforeModel` 回调。
  - 将 `Compact` 实现在 `BeforeModel` 中（因为 ContentRequestProcessor 已构建 messages）。
- **关键依赖**：任务 1.x、任务 2.x、任务 3.1。
- **验收标准**：
  - 集成测试验证持久事件循环仍可用。
  - 验证获得框架 tracing/telemetry 能力。

### 任务 3.3: Tmux 异步模型适配

- **内容**：Flow.Run 结束后回到 StartLoop，tmux 结果作为下一条 external_input 进入。
- **实现方案**：
  - 移除 AgentLoop 在 Pull 中阻塞等待 tmux 结果的逻辑。
  - `ActionTool` 的 TmuxMonitor 回调通过 `InjectMessage` 发布 external_input。
- **关键依赖**：任务 3.2。
- **验收标准**：
  - 集成测试验证 tmux 异步命令完成后正确触发下一轮处理。

### 阶段三回滚策略

- 阶段三为可选大重构，建议在独立分支进行，保留原 AgentLoop 实现作为 fallback。

---

## 四、里程碑与验证

| 里程碑 | 完成标志 | 验证命令 |
|--------|----------|----------|
| 阶段一完成 | `go test ./agent/...` 通过；长会话测试不溢出 | `go test ./agent/... -run TestCompact` |
| 阶段二完成 | dispatch 语义测试通过；session copy 消除 | `go test ./agent/... -run TestDispatch` |
| 阶段三完成 | 框架 Flow 集成测试通过 | `go test ./...` |

## 五、风险提示

| 风险 | 缓解措施 |
|------|----------|
| Compact 误删关键上下文 | 保留最近 N 个完整任务；summary reference 保留 EventKey 列表供 recall |
| EventReference 拉取失败 | fallback 使用 EventSummary；记录 warning |
| dispatch 时机改变破坏现有测试 | 大量更新测试断言；保留旧分支 |
| 框架 Flow 集成改变 tmux 行为 | 充分集成测试；保留旧分支 |

