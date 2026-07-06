## ADDED Requirements

### Requirement: 审查 SHALL 验证事件驱动一致性

审查者 SHALL 检查每个模块的事件处理逻辑是否符合事件驱动设计目标。检查项包括：事件类型分类（ExtractEventType）与消息 Role 映射是否正确；特殊事件（external_input/agent_output/thinking_plan）是否使用原文全文不被截断；MemoryPlugin.onEvent 的事件持久化路径是否完整；SummaryPlugin 的摘要生成是否与事件类型一致。

#### Scenario: 事件类型分类验证

- **WHEN** 审查者检查 event/types.go 的 ExtractEventType 函数
- **THEN** SHALL 验证 RoleUser/RoleSystem → external_input、RoleAssistant+ToolCalls → thinking_plan、RoleAssistant → agent_output、RoleTool → action_command 的映射逻辑是否正确且无遗漏

#### Scenario: 特殊事件截断保护验证

- **WHEN** 审查者检查 IsSpecialEventType 及其消费者
- **THEN** SHALL 验证 external_input/agent_output/thinking_plan 事件在压缩、摘要等处理中是否使用原文全文，不被截断或修改

### Requirement: 审查 SHALL 验证内存中心一致性

审查者 SHALL 检查 MemoryStore 是否作为唯一事实来源，Session 是否只保存轻量引用。检查项包括：FullEvent 的完整性（所有必要字段被正确填充）；MemoryPlugin 的 PartitionID 派生（AgentName → FNV-1a hash）；StateDelta 写回（event_key/partition_id/event_type）；InMemoryStore 与 FileSegmentStore 的行为一致性。

#### Scenario: PartitionID 隔离验证

- **WHEN** 审查者检查 MemoryPlugin.onEvent 的 PartitionID 派生逻辑
- **THEN** SHALL 验证 PartitionIDFromName 使用 FNV-1a hash 且 Memory 层不感知 agent 概念，只看到 PartitionID 整数

#### Scenario: StateDelta 写回验证

- **WHEN** 审查者检查 MemoryPlugin.onEvent 的 StateDelta 写回步骤
- **THEN** SHALL 验证 event_key、partition_id、event_type 三个字段被正确写入 evt.StateDelta，且下游消费者（SmartCompressor、AgentToolWrapper）能够读取

### Requirement: 审查 SHALL 验证视图转换完整性

审查者 SHALL 检查上下文压缩是否只修改发给 LLM 的 Request.Messages，不破坏 Session/MemoryStore 原始数据。检查项包括：SmartCompressor 的压缩流程是否纯函数（输入 messages → 输出 messages，无副作用）；context_compress 事件是否包含足够的回溯信息（被压缩事件的 key 列表）；ContextIntervention 的 BeforeModel 回调是否只修改 args.Request.Messages。

#### Scenario: 压缩回溯信息验证

- **WHEN** 审查者检查 SmartCompressor.collectCompressedKeys 和 buildCompressEvent
- **THEN** SHALL 验证压缩后被压缩事件的 EventKey 是否被正确收集并传递到 context_compress 消息中，使 LLM 可通过 recall agent 回溯

#### Scenario: 视图转换无副作用验证

- **WHEN** 审查者检查 ContextIntervention.BeforeModel 和 SmartCompressor.Compress
- **THEN** SHALL 验证这些函数只修改 args.Request.Messages，不修改 inv.Session 或 MemoryStore 中的任何数据

### Requirement: 审查 SHALL 验证因果关系完整性

审查者 SHALL 检查因果关系是否由独立的 RelationStore 管理，FullEvent 不含 ParentKey。检查项包括：MemoryPlugin 是否通过 RelationStore.SetParent 设置父子关系（而非 FullEvent 字段）；因果链是否按 PartitionID 独立维护；TombstoneSet.MarkTombstone 是否触发级联父引用修复（findAliveAncestor）；recall 子工具是否通过 RelationStoreProvider 接口获取 parentKey。

#### Scenario: 关系存储独立性验证

- **WHEN** 审查者检查 memory/types.go 的 FullEvent 结构和 plugin/memory_plugin.go 的 onEvent
- **THEN** SHALL 验证 FullEvent 不含 ParentKey 字段，且 SetParent 通过 RelationStoreProvider 接口调用

#### Scenario: 因果链分区隔离验证

- **WHEN** 审查者检查 MemoryPlugin 的 lastEventKeys map
- **THEN** SHALL 验证因果链按 PartitionID 独立维护，子 agent 事件不影响父 agent 的因果链

### Requirement: 审查 SHALL 验证上下文隔离完整性

审查者 SHALL 检查 LLM 是否只输出 int64 数字 key，AgentToolWrapper 是否服务端解析获取完整事件内容。检查项包括：AgentToolWrapper.Call 的 event_keys 解析和 IngestExternalEvents 注入；ReadNamespaces → ReadPartitionIDs 的跨命名空间读权限转换；recall 子工具是否自动注入 readPartitionIDs。

#### Scenario: event_key 隔离验证

- **WHEN** 审查者检查 agent/tool_agent.go 的 AgentToolWrapper.Call
- **THEN** SHALL 验证 LLM 只看到 int64 数字 key，完整事件内容由 AgentToolWrapper 从 parentStore.GetEvent() 获取并注入子 agent

#### Scenario: 跨命名空间读权限验证

- **WHEN** 审查者检查 config.go 的 ReadNamespaces 和 tool/recall/recall_subtools.go 的 readPartitionIDs 注入
- **THEN** SHALL 验证 ReadNamespaces 正确转换为 ReadPartitionIDs，且 recall 子工具的查询自动注入分区过滤

### Requirement: 审查 SHALL 验证分层存储与生命周期一致性

审查者 SHALL 检查 L0→L1→L2→L3 压实流程是否完整，墓碑过滤是否在合并时生效。检查项包括：Compactor 的 CompactL1ToL2/CompactL2ToL3 流程（Merge→Filter→Repair→Write→Cleanup）；filterTombstoned 是否实际过滤已删除事件；LifecycleManager 的 TTL 过期和容量驱逐；Compactor 的 repairDanglingRefs 是否通过 RelationStore 修复悬空引用。

#### Scenario: 墓碑过滤有效性验证

- **WHEN** 审查者检查 memory/compaction.go 的 filterTombstoned 方法
- **THEN** SHALL 验证该方法是否实际调用 TombstoneSet.IsTombstone() 过滤已删除事件，并记录其实际行为

#### Scenario: 悬空引用修复验证

- **WHEN** 审查者检查 Compactor 的 repairDanglingRefs 方法
- **THEN** SHALL 验证该方法通过 RelationStore 查找最近存活祖先修复悬空引用，而非简单删除

### Requirement: 审查 SHALL 验证并发安全

审查者 SHALL 检查所有 goroutine 间共享状态访问是否有正确的同步机制。检查项包括：TmuxMonitor 的 running 字段和 session map 访问；MemoryPlugin 的 lastEventKeys map 访问；TombstoneSet 的 keys map 访问；InMemRelationStore 的双图访问；CommandTool 的 tmuxMonitor 生命周期管理。

#### Scenario: 共享状态同步验证

- **WHEN** 审查者检查任何被多个 goroutine 访问的 map 或 struct 字段
- **THEN** SHALL 验证访问是否有 mutex 保护或使用 atomic 操作，无数据竞争风险

### Requirement: 审查 SHALL 验证代码一致性

审查者 SHALL 检查注释、log 包、错误处理是否与实现保持一致。检查项包括：注释是否引用已移除的字段或机制；log 包使用是否统一（trpc-agent-go/log vs 标准库 log）；错误处理是否健壮（无 `_ = err` 静默吞错）；stub/TODO 是否有明确的后续计划标注。

#### Scenario: 注释一致性验证

- **WHEN** 审查者检查任何代码注释
- **THEN** SHALL 验证注释不引用当前代码中不存在的字段或机制，注释内容与实际实现保持一致

#### Scenario: 错误处理健壮性验证

- **WHEN** 审查者检查任何 `_ = err` 或被忽略的 error 返回值
- **THEN** SHALL 验证是否有合理的理由（如 best-effort 操作）或应记录日志

### Requirement: 审查 SHALL 验证接口契约

审查者 SHALL 检查 MemoryStore/RelationStore/KVStore 等接口的实现是否满足契约。检查项包括：InMemoryStore 和 FileSegmentStore 是否都完整实现 MemoryStore 接口；可选接口（RelationStoreProvider）的类型断言是否安全；KVStore 接口的 MockRustVikingClient 和 RustVikingClient 行为是否一致。

#### Scenario: 接口实现完整性验证

- **WHEN** 审查者检查 MemoryStore 接口的所有实现
- **THEN** SHALL 验证每个实现都完整实现了接口的所有方法，无 stub 或 panic

#### Scenario: 可选接口断言安全性验证

- **WHEN** 审查者检查 RelationStoreProvider 的类型断言（如 `if rsp, ok := p.memStore.(memory.RelationStoreProvider); ok`）
- **THEN** SHALL 验证断言失败时有合理的降级路径，而非静默跳过关键逻辑

### Requirement: 审查 SHALL 验证生产接线完整性

审查者 SHALL 从生产入口（tagent.go）出发，纵向追踪每个组件是否被正确创建、配置、启动和关闭。检查项包括：MemoryStore 的创建路径是否完整接入所有依赖组件（LifecycleManager、Compactor、TombstoneSet）；每个有 Start()/Stop() 方法的组件是否在生产入口被调用；工厂函数（如 commandFactory）是否将安全配置（runAsUser、workspace）传递给所有子组件（CommandExecutor 和 TmuxExecutor）；组件的关闭顺序是否正确（先停后台 goroutine，再 flush 持久化）。

#### Scenario: 后台组件启动验证

- **WHEN** 审查者检查任何有 Start() 方法的组件（LifecycleManager、Compactor、TmuxMonitor 等）
- **THEN** SHALL 追踪从生产入口（tagent.go resolveMemoryStore / builtin.go 工厂）到该组件 Start() 调用的完整路径，验证组件在生产环境中确实被启动

#### Scenario: 依赖注入完整性验证

- **WHEN** 审查者检查工厂函数（如 commandFactory、resolveMemoryStore）
- **THEN** SHALL 验证工厂将所有配置项（runAsUser、workspace、env、timeout 等）传递给所有子组件，不遗漏任何子组件的安全配置

#### Scenario: 可选依赖接入验证

- **WHEN** 审查者检查任何标记为"可选"或"optional"的依赖（如 TombstoneSet、LifecycleManager）
- **THEN** SHALL 验证在生产环境中该依赖是否为 nil——如果为 nil，该依赖保护的功能是否完全失效，且这种失效是否可接受

#### Scenario: 资源关闭顺序验证

- **WHEN** 审查者检查有 Stop()/Close() 方法的组件
- **THEN** SHALL 验证生产代码中是否存在关闭调用，且关闭顺序正确（先停后台 goroutine → flush 持久化 → 释放文件句柄）

#### Scenario: 字段使用完整性验证

- **WHEN** 审查者检查任何通过 Option/Builder 模式设置的结构体字段（如 runAsUser、workspace、env 等）
- **THEN** SHALL 验证该字段在所有应该使用它的方法中被实际引用，不存在"定义了 setter 但从未在业务逻辑中使用"的死字段；对于安全配置（如 runAsUser），SHALL 验证它在所有执行路径（sync exec、async tmux_exec、session 重启）中被使用

#### Scenario: 生产入口创建验证

- **WHEN** 审查者检查任何有 Start() 方法的后台组件（LifecycleManager、Compactor、TmuxMonitor 等）
- **THEN** SHALL 追踪从生产入口（tagent.go resolveMemoryStore / builtin.go 工厂）到该组件构造函数（NewXXX）的完整路径，验证组件在生产环境中确实被创建，而非仅在测试代码中创建；若组件未在生产入口创建，SHALL 记录为 P0 级发现

#### Scenario: 状态回收闭环验证

- **WHEN** 审查者检查任何持续产生数据的组件（事件存储、tmux session、进程等）
- **THEN** SHALL 追踪从数据创建到数据回收/过期/清理的完整闭环，验证回收机制在生产代码中被启动且有效执行，不存在"只进不出"的资源泄漏；对于事件存储，SHALL 验证 TTL 过期扫描、压实调度、墓碑过滤在生产代码中均被启动

### Requirement: 审查 SHALL 验证工具执行安全

审查者 SHALL 检查命令执行工具的权限隔离、环境传递和资源清理。检查项包括：sync 执行（exec 模式）和 async 执行（tmux_exec 模式）的权限隔离是否一致；执行用户（runAsUser）是否在所有执行路径中生效；环境变量是否在所有执行路径中正确传递；进程组和 tmux session 是否在异常情况下被正确清理；重启/恢复路径是否保持原始安全上下文。

#### Scenario: 执行权限一致性验证

- **WHEN** 审查者检查 CommandTool 的 exec 模式和 tmux_exec 模式
- **THEN** SHALL 验证两种模式使用相同的用户隔离策略（runAsUser/runAsGroup），不存在一种模式有隔离而另一种没有的情况

#### Scenario: 环境变量传递验证

- **WHEN** 审查者检查命令执行工具的环境变量处理
- **THEN** SHALL 验证用户传入的 env 参数在所有执行路径（sync exec、async tmux_exec、session 重启）中被正确传递到实际命令进程

#### Scenario: 安全上下文保持验证

- **WHEN** 审查者检查会话重启/恢复路径（如 handleFakeAlive → RestartSession）
- **THEN** SHALL 验证重启后的会话保持原始的 runAsUser、workspace 和 env 配置，不丢失安全上下文

#### Scenario: 资源清理完整性验证

- **WHEN** 审查者检查异常路径（KillSession 失败、进程崩溃、超时）
- **THEN** SHALL 验证资源清理是否有重试或兜底机制，不会因清理失败而残留僵尸进程或 tmux session

#### Scenario: 数据传递链完整性验证

- **WHEN** 审查者检查任何函数参数或结构体字段的传递路径（如 TmuxCreateOptions.Env、CommandSpec.RunAsUser 等）
- **THEN** SHALL 从参数定义点逐级追踪到最终使用点，验证每个中间环节都正确传递数据，不存在"接收了参数但从未使用"的断链；对于通过多层函数调用传递的参数，SHALL 在每一层验证参数被读取和使用

#### Scenario: 跨路径行为对称性验证

- **WHEN** 审查者检查同一组件的不同执行路径（如 sync exec vs async tmux_exec、正常执行 vs RestartSession 重启恢复、首次创建 vs 恢复重建）
- **THEN** SHALL 逐一对比不同路径在权限隔离、环境变量、工作目录、超时控制方面的行为，验证不存在一种路径有保护而另一种路径缺失的情况；对于不对称的路径，SHALL 记录为 P1 级发现
