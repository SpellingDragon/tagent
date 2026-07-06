## Why

架构审查（architecture-review）揭示了 tagent 当前实现与 6 项设计目标之间的系统性偏差。最严重的偏差是 **event_key 注入链路端到端断裂**——Phase 1 移除了前缀注入逻辑但未提供替代机制，导致 event_key 从未出现在 LLM 可见上下文中，进而使压缩回溯（目标 1）和上下文隔离（目标 5）的整条功能链路失效。此外，5 个 P1 并发安全缺陷、压实调度不完整（L1→L2/L2→L3 方法已实现但未调度）、因果链跨 session 串线等问题也需要系统性修复。

本变更基于审查报告的修复路线图，按依赖关系分阶段修复所有 P0+P1 问题及关键 P2 问题，使实现与设计预期重新对齐。

## What Changes

### 第一阶段：恢复 event_key 注入链路（解决 P0）

- **新增 event_key 前缀注入机制**：在 ContextIntervention.BeforeModel 中，通过位置匹配 args.Request.Messages 与 inv.Session.Events，为 user/assistant 消息添加 `[evt_<KEY>|<type>]` 前缀，使 LLM 可感知并选择 event_key
- **保留现有 collectCompressedKeys / parseEventKeyFromPrefix**：这些函数已实现 `[evt_<KEY>|<type>]` 前缀解析逻辑，前缀注入恢复后可直接工作，无需重写或删除
- **确保 buildCompressEvent 输出 key 列表**：压缩事件包含被压缩事件的 key 列表，激活 LLM → recall agent 回溯链路
- **激活 AgentToolWrapper → IngestExternalEvents 链路**：LLM 可从前缀中看到 event_key，选择相关 key 传给子 agent；AgentToolWrapper.Call 已实现 event_keys 解析 → parentStore.GetEvent → IngestExternalEvents 完整链路（已有代码，无需修改）
- **澄清框架自动注入缺失**：wiki 描述的「Flow 层从 StateDelta 提取 event_key 合并到 tool jsonArgs」机制在框架中不存在；实际机制是 LLM 驱动选择——LLM 从前缀看到 key → LLM 在 tool_call 参数中传 event_keys → AgentToolWrapper 解析

### 第二阶段：并发安全加固（解决 P1）

- **TagentAgent.lastUserID/lastSessionID**：加 mutex 保护，修复 Run/RunSimple 写入与 InjectMessage 读取的数据竞争
- **TmuxMonitor.running**：改用 `sync/atomic.Bool`，修复 Start/Stop/command_tool.go 读取的竞争
- **TmuxMonitor.checkSession**：在锁内修改 session 字段，修复 checkAllSessions 释放锁后仍修改原始 session 对象的问题
- **InMemRelationStore.truncateJournal**：在 truncateJournal 内部持有写锁，修复 SaveSnapshotToFile 调用时的竞争条件
- **CommandTool.tmuxMonitor.running 读取**：提供 IsRunning() 方法带锁访问

### 第三阶段：因果链隔离与视图转换无状态化（解决 P2）

- **lastEventKeys 按 (PartitionID, SessionID) 隔离**：修复同一 PartitionID 跨 session/user 串线问题
- **KeepRecentTasks 无状态化**：使用局部变量保存原始值，BeforeModel 结束后恢复，消除跨请求状态泄漏

### 第四阶段：分层存储与生命周期完善（解决 P2）

- **接入 L1→L2/L2→L3 调度**：在 checkAndCompact 中添加段计数逻辑，达到阈值时触发 CompactL1ToL2/CompactL2ToL3
- **接入 filterTombstoned 到 TombstoneSet**：替换 no-op stub，调用 LifecycleManager.GetTombstoneFilterFunc
- **修复 lifecycle.go TTL 时间精度**：从事件 JSON 中解析 Timestamp 字段，而非使用窗口时间戳近似

### 第五阶段：错误处理与代码一致性（解决 P2）

- **统一 log 包**：command_tool.go 从标准库 `log` 改为 `trpc-agent-go/log`
- **检查被忽略的 error 返回值**：tombstone.go、segment_store.go、compaction.go、lifecycle.go、recall_subtools.go 中的 `_ = err`
- **清理陈旧注释**：移除所有引用已移除 ParentKey/Phase 1 机制的注释

### 第六阶段：文档同步

- **重写 memory-architecture.md**：移除 ParentKey/FileBackend，补充 KV store/compaction/lifecycle/tombstone 机制
- **修正 agent-architecture.md**：移除 Phase 1 代码，更新 lastEventKeys 描述为已实现
- **修正 plugin/tool wiki**：移除 ParentKey/FileBackend 描述
- **更新 README.md**：补充完整项目结构

## Capabilities

### New Capabilities

- `event-key-visibility`: event_key 前缀注入机制——在 BeforeModel 中通过位置匹配从 Session.Events 提取 event_key，为消息添加 `[evt_<KEY>|<type>]` 前缀；激活压缩回溯链路（collectCompressedKeys → buildCompressEvent → LLM 可见 key 列表）和上下文隔离链路（LLM 选择 event_keys → AgentToolWrapper 解析 → parentStore.GetEvent → IngestExternalEvents）
- `concurrency-hardening`: 并发安全加固——TagentAgent 字段保护、TmuxMonitor 状态同步、InMemRelationStore journal 截断加锁、CommandTool 安全访问
- `causal-chain-isolation`: 因果链隔离——lastEventKeys 按 (PartitionID, SessionID) 维护独立因果链，防止跨 session/user 串线
- `view-transform-stateless`: 视图转换无状态化——KeepRecentTasks 使用局部变量，消除跨请求状态泄漏
- `storage-lifecycle-completion`: 分层存储与生命周期完善——L1→L2/L2→L3 调度接入、filterTombstoned 接入 TombstoneSet、TTL 时间精度修复
- `error-handling-consistency`: 错误处理与代码一致性——log 包统一、被忽略 error 检查、陈旧注释清理

### Modified Capabilities

（无——本变更新增能力定义，不修改已有 spec）

## Impact

### 受影响代码

- `agent/smart_compress.go` — 更新 collectCompressedKeys 注释（移除 Phase 1 引用），保留 parseEventKeyFromPrefix 现有逻辑
- `agent/context_intervention.go` — 新增 event_key 注入逻辑，KeepRecentTasks 无状态化
- `agent/tagent_agent.go` — lastUserID/lastSessionID 加 mutex 保护
- `plugin/memory_plugin.go` — lastEventKeys 按 (PartitionID, SessionID) 隔离（复合 key），清理陈旧注释（ParentKey 引用、步骤跳号）
- `memory/relation_store.go` — truncateJournal 加写锁
- `memory/compaction.go` — checkAndCompact 接入 L1→L2/L2→L3 调度，filterTombstoned 接入 TombstoneSet
- `memory/lifecycle.go` — TTL 时间精度修复，错误检查
- `memory/tombstone.go` — 错误检查
- `memory/segment_store.go` — 错误检查
- `tool/command/command_tool.go` — log 包统一，tmuxMonitor.running 安全访问
- `tool/command/tmux_monitor.go` — running 改 atomic.Bool，checkSession 锁内修改
- `tool/recall/recall_subtools.go` — GetParent 错误检查
- `docs/wiki/memory/memory-architecture.md` — 重写
- `docs/wiki/agent/agent-architecture.md` — 修正
- `docs/wiki/plugin/plugin-architecture.md` — 修正
- `docs/wiki/tool/tool-architecture.md` — 修正
- `README.md` — 更新项目结构

### 依赖

- `trpc-agent-go v1.7.0` — BeforeModel 回调、OnEvent 钩子、StateDelta 机制、Session/Invocation 结构
- 审查报告 `openspec/changes/architecture-review/review-report.md` — 作为修复依据

### 风险

- event_key 注入机制需要与 trpc-agent-go 框架的 Session.State/StateDelta 交互，需验证框架 API 的可用性
- lastEventKeys 按 (PartitionID, SessionID) 隔离需要修改 MemoryPlugin 的数据结构，可能影响现有的因果链测试
- truncateJournal 加锁可能影响压实性能，需评估锁持有时间
