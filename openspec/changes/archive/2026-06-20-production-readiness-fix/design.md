## Context

tagent 经历了 11 个 Phase 的代码实现 + 3 轮架构审查。当前架构已回归 trpc-agent-go 的 Runner + Plugin 模式（TagentAgent 使用 llmagent.LLMAgent + runner.Runner + MemoryPlugin + SummaryPlugin + ContextIntervention），这是正确的方向。

但第三轮端到端数据链追踪发现：**已有组件代码正确但从未在生产入口接线**。LifecycleManager、Compactor、TombstoneSet 三个组件的代码在 `memory/` 包中完整实现且有单元测试，但 `tagent.go` 的 `resolveMemoryStore` 从未创建它们。同理，CommandTool 的 TmuxExecutor 从未接收安全配置，TmuxMonitor.Stop() 从未在生产代码中调用。

此外，7 份设计锚点文档与实现存在偏差：`memory/types.go` 仍有 `EventType*` 重复定义、SmartCompressor 未实现分批摘要、ensureUserPrompt 策略不完整。

## Goals / Non-Goals

**Goals:**

1. 生产入口完整接线 LifecycleManager + Compactor + TombstoneSet，使事件存储回收闭环运行
2. tmux_exec 模式与 exec 模式安全对称（用户隔离、环境变量、工作目录）
3. CommandTool 有 Close 方法，TmuxMonitor 优雅停止
4. 事件类型常量单一来源
5. 压缩后保证 User message 存在（保留 pending user 或引导消息）
6. 分批摘要按 token 预算分批，多条摘要，容错跳过

**Non-Goals:**

- 不重写 RelationStore（由 `harden-event-storage-for-scale` 负责）
- 不修改 MemoryStore 接口签名（由 `harden-event-storage-for-scale` 负责）
- 不引入 RustViking v0.2.0 依赖
- 不实现 LLM 驱动的 L3 摘要化（由 `llm-event-summary` 负责）
- 不修改 trpc-agent-go 框架代码

## Decisions

### D1：生产接线策略——构造后注入而非构造时传入

**选择**：FileSegmentStore 新增 `SetTombstoneSet(*TombstoneSet)` 方法，resolveMemoryStore 先创建 store 再注入 tombstone。

**理由**：NewFileSegmentStore 当前签名 `(kv, rel, path, maxKeys)` 被 8 个测试文件调用。修改签名会引发大面积测试改动。构造后注入是 Go 惯用模式（如 http.Server.SetKeepAlives），且 TombstoneSet 是可选依赖（InMemoryStore 不需要）。

**替代方案**：修改 NewFileSegmentStore 签名增加 tombstone 参数 → 拒绝，侵入性太大。

### D2：tmux 用户隔离策略——sudo 包装 tmux 命令

**选择**：当 `te.runAsUser != ""` 时，CreateSession 构建 `sudo -n -u <user> [-g <group>] tmux new-session ...` 命令。

**理由**：exec 模式已用 `sudo -n -u` 实现用户隔离，tmux_exec 模式应对称。sudo 包装 tmux 进程本身，确保 tmux server 和子进程都以目标用户运行。

**替代方案**：在 tmux session 内用 `send-keys` 切换用户 → 拒绝，tmux server 本身仍以原用户运行，隔离不完整。

### D3：tmux 环境变量策略——set-environment 前置

**选择**：CreateSession 后、执行命令前，通过 `tmux set-environment -t <session> <key> <value>` 逐个设置环境变量。

**理由**：tmux 的 `new-session` 命令不支持直接传递环境变量。`set-environment` 是 tmux 原生机制，设置的环境变量会被子进程继承。

**替代方案**：在命令前加 `env KEY=VALUE` 前缀 → 拒绝，与用户命令拼接易出错（shell 转义问题）。

### D4：CommandTool 关闭策略——io.Closer 接口

**选择**：CommandTool 实现 `io.Closer` 接口（`Close() error`），TagentAgent 维护 `[]*command.CommandTool` 列表，Close() 时逐个关闭。

**理由**：io.Closer 是 Go 标准接口，调用方无需导入 command 包即可判断是否可关闭（type assertion）。TagentAgent 已有 Close() 方法调用 runner.Close()，在此追加 CommandTool 关闭逻辑。

### D5：KillSession 失败处理——保留 + 重试

**选择**：handleFakeDead 中 KillSession 失败时，不删除 session，保留 StableSince，下个检测周期重新评估。连续 3 次失败后强制删除并记录 error。

**理由**：当前行为是 KillSession 失败仍删除 session，导致僵尸 tmux session 无法被追踪。保留 session 让 monitor 继续尝试清理，3 次上限防止无限重试。

### D6：TUI 会话回收——超时标记 + 状态回调

**选择**：TUI 会话在 fakeDead 阈值后不返回 SessionRunning，而是返回新状态 `SessionTimedOut`。StateChangeCallback 通知 agent，然后从 monitor 移除。

**理由**：当前 TUI 会话在 fakeDead 阈值后返回 Running 且保留 StableSince，导致每个周期重新评估但永不回收。引入 SessionTimedOut 让 agent 知道会话已超时，可以决定是否重新启动。

### D7：事件类型统一——memory 包引用 event 包

**选择**：删除 `memory/types.go` 中的 `EventType*` 常量，所有引用改为 `event.Type*`。memory 包新增对 event 包的 import。

**理由**：`event/types.go` 已有完整的 Type* 常量和辅助函数（ExtractEventType、GenerateEventSummary 等）。memory 包保留 EventType* 是历史遗留——最初事件类型定义在 memory 包，后来统一到 event 包但未清理 memory 侧。

**循环依赖检查**：event 包仅依赖 model 包，memory 包依赖 event 包和 model 包，无循环。

### D8：压缩 User Message 策略——保留 pending user 优先

**选择**：压缩后按以下顺序确保 User message 存在：
1. 查找最后一个 agent_output（assistant 无 tool calls）之后的 user message
2. 如果找到，保留该 user message（用户的未完成请求）
3. 如果未找到，添加引导消息 `"（以上是对话历史摘要。如果有新任务，请告诉我。）"`

**理由**：当前 ensureUserPrompt 只检查是否有任何 user message，如果没有就加"继续"。"继续"语义模糊，LLM 可能困惑。保留 pending user 让 LLM 知道需要完成什么任务；引导消息明确告知 LLM 等待新任务。

### D9：分批摘要——token 预算 + 独立摘要 + 容错

**选择**：SmartCompressor.Compress 在 Stage 2 生成摘要时：
1. 计算 `maxInputTokens = maxTokens / 2`（最小 1000）
2. 遍历 oldSegments，按 token 预算分批
3. 每批独立调用 LLM 生成摘要
4. 单批失败跳过（log warning），继续处理其他批次
5. 生成多条摘要事件（System role），替换原始 oldSegments

**理由**：当前实现将所有 oldSegments 一次性发给 LLM 生成单条摘要，当历史很长时输入 token 超限。分批确保每批在预算内，多条摘要保留更多信息，容错防止单批失败导致全部丢失。

## Risks / Trade-offs

- **[TombstoneSet 接入后 GetEvent 返回 nil]** → 调用方需处理 nil。缓解：仅在 TTL 过期后标记墓碑，正常时间窗口内不会命中。GetEvent 已有 error 返回值，改为返回 `nil, nil`（存在但已墓碑）。

- **[tmux sudo 需要 NOPASSWD 配置]** → 生产环境需配置 sudoers。缓解：runAsUser 为空时不使用 sudo，向后兼容。文档中说明 sudoers 配置要求。

- **[分批摘要增加 LLM 调用次数]** → N 批 = N 次 LLM 调用。缓解：压缩是低频操作（仅在 token 超阈值时触发），且可配置最大批次数。

- **[memory 包新增 event 包依赖]** → 增加耦合。缓解：event 包是叶子包（仅依赖 model），不会引入循环依赖。

- **[SessionTimedOut 新状态]** → StateChangeCallback 调用方需处理新状态。缓解：只有一个调用方（CommandTool.handleStateChange），已处理所有状态字符串。
