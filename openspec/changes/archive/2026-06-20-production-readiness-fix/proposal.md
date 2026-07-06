## Why

经过三轮架构审查（逐模块 + 横向维度 + 端到端数据链追踪），发现 tagent 存在 **4 个 P0 + 5 个 P1 + 1 个 P2** 级系统性缺陷：生产接线完全断裂（LifecycleManager/Compactor/TombstoneSet 从未创建）、工具执行安全不对称（tmux_exec 模式无用户隔离）、资源泄漏（TmuxMonitor.Stop 从未调用）。同时，7 份设计锚点文档与实际实现之间存在显著偏差：事件类型重复定义、压缩策略不完整、分批摘要未实现。

这些问题不修复则项目不可用于生产。而 `harden-event-storage-for-scale` change 是 14 阶段大规模重构且阻塞于 RustViking v0.2.0，不适合作为紧急修复路径。本 change 聚焦**接线补全 + 安全对齐 + 设计偏差修正**，复用已有组件代码，不引入新依赖。

## What Changes

### P0：生产接线补全

- **resolveMemoryStore 完整接线**：在 `tagent.go` 的 file 分支中创建 TombstoneSet → 注入 FileSegmentStore → 创建 LifecycleManager → 创建 Compactor → 调用 Start()
- **FileSegmentStore 增加 SetTombstoneSet 方法**：允许构造后注入 TombstoneSet，使 GetEvent/QueryEvents 的墓碑过滤生效
- **TagentAgent.Close() 增加 store 关闭流程**：停止 LifecycleManager → 停止 Compactor → flush tombstones

### P0：工具执行安全对齐

- **NewCommandTool 安全配置传递**：将 runAsUser/runAsGroup/workspace 传递给 TmuxExecutor（WithTmuxRunAsUser/WithTmuxWorkspace）
- **TmuxExecutor.CreateSession 权限接入**：当 runAsUser 非空时，用 `sudo -n -u <user>` 包装 tmux 命令；使用 opts.Env 设置环境变量（`tmux set-environment`）
- **TmuxExecutor.RestartSession 安全上下文保持**：传递 runAsUser 和 Env

### P1：资源生命周期管理

- **CommandTool.Close() 方法**：调用 TmuxMonitor.Stop()，清理所有 tracked session
- **TagentAgent 关闭链**：Close() 时调用所有注册的 CommandTool.Close()
- **KillSession 失败不删除**：handleFakeDead 中 KillSession 失败时保留 session，下周期重试
- **handleFakeAlive 传递安全上下文**：构造 TmuxCreateOptions 时包含 runAsUser 和 Env

### P2：TUI 会话回收

- **TUI 会话超时回收**：fakeDead 阈值后 TUI 会话标记为超时，触发回收而非无限保留

### 设计偏差修正

- **事件类型统一**：删除 `memory/types.go` 中的 `EventType*` 常量，全局使用 `event.Type*`；更新所有引用
- **压缩 User Message 策略**：实现 `compress-user-message-final.md` 设计——查找 pending user message 并保留，或添加引导消息（替代当前的"继续"硬编码）
- **分批摘要**：实现 `batched-summarization-design.md` 设计——按 token 预算分批，每批独立摘要，容错跳过失败批次

## Capabilities

### New Capabilities

- `production-wiring-fix`: 生产入口创建并启动 LifecycleManager、Compactor、TombstoneSet，关闭时优雅停止
- `tool-security-alignment`: tmux_exec 模式与 exec 模式安全对称（用户隔离、环境变量、工作目录）
- `tool-lifecycle-management`: CommandTool 生命周期管理（Close/Shutdown），TmuxMonitor 优雅停止
- `event-type-unification`: 事件类型常量单一来源，消除 event 包与 memory 包的重复定义
- `compression-user-message`: 压缩后保证 User message 存在的策略——保留 pending user 或添加引导消息
- `batched-summarization`: 按 token 预算分批摘要，多条摘要，容错跳过

### Modified Capabilities

（无——本 change 不修改已有 spec 的 Requirement，而是新增 capability）

## Impact

### 修改文件

- `tagent.go`：resolveMemoryStore 完整接线 + TagentAgent.Close 关闭链
- `memory/segment_store.go`：新增 SetTombstoneSet 方法 + Close 方法
- `memory/types.go`：删除 EventType* 常量
- `event/types.go`：无变更（已是唯一来源）
- `plugin/memory_plugin.go`：EventType* → event.Type* 引用更新
- `plugin/summary_plugin.go`：无变更（已使用 event.Type*）
- `tool/command/command_tool.go`：NewCommandTool 传递安全配置 + Close 方法
- `tool/command/tmux_executor.go`：CreateSession/RestartSession 权限接入 + Env 设置
- `tool/command/tmux_monitor.go`：KillSession 失败处理 + TUI 回收 + handleFakeAlive 上下文
- `agent/smart_compress.go`：分批摘要 + User message 策略
- `agent/context_intervention.go`：ensureUserPrompt 改为 pending user 策略
- `agent/tagent_agent.go`：Close 链增加 CommandTool 关闭
- 所有引用 `memory.EventType*` 的文件：改为 `event.Type*`

### 不引入新依赖

- 复用已有 LifecycleManager、Compactor、TombstoneSet 代码
- 不依赖 RustViking v0.2.0（使用现有 CLI 接口）
- 不修改 MemoryStore 接口签名

### 风险

- **TombstoneSet 接入后 GetEvent 行为变化**：已被标记墓碑的事件不再返回，调用方需处理 nil。缓解：墓碑仅由 LifecycleManager 标记，正常查询不会命中
- **tmux sudo 权限**：tmux_exec 模式下 sudo 需要免密配置。缓解：runAsUser 为空时不使用 sudo（向后兼容）
- **分批摘要增加 LLM 调用次数**：每批一次调用。缓解：压缩是低频操作，可接受
