## 1. 阶段一 — 组合根审查

- [x] 1.1 审查 tagent.go：依赖方向（root → agent → plugin → memory 单向无环）、buildAgent 递归创建、resolveMemoryStore 存储 backend 选择、buildAgentToolRef 的 AgentToolWrapper 包装
- [x] 1.2 审查 config.go：声明式配置（Config/AgentConfig/MemoryConfig/ToolRef）、DefaultConfig 的三个核心 agent、EventParams 声明、ReadNamespaces 跨命名空间读权限
- [x] 1.3 审查 builtin.go：工厂注册机制（RegisterToolAgent/RegisterPlainTool）、默认工具注册

## 2. 阶段一 — agent 层审查

- [x] 2.1 审查 tagent_agent.go：7 步初始化流程、Run/RunSimple 接口实现、pendingExternalEvents 注入、InjectMessage 超时保护、IngestExternalEvents 外部上下文注入
- [x] 2.2 审查 tool_agent.go：AgentToolWrapper 的 event_keys 解析、Call 方法的 parentStore.GetEvent → IngestExternalEvents 链路、工厂注册机制
- [x] 2.3 审查 smart_compress.go：两阶段压缩（任务边界切分 + LLM 摘要）、collectCompressedKeys 的 event_key 收集路径、buildCompressEvent 的 key 列表输出、视图转换无副作用
- [x] 2.4 审查 context_intervention.go：BeforeModel 回调的 token 预算检查、循环压缩的 stalled 检测、ensureUserPrompt 保护、是否只修改 args.Request.Messages
- [x] 2.5 审查 token_counter.go：字符启发式估算的合理性、中英文混合场景的 CharsPerToken

## 3. 阶段一 — memory 层审查

- [x] 3.1 审查 types.go：FullEvent 结构（无 ParentKey）、MemoryStore 接口完整性、RelationStoreProvider 可选接口、Snowflake EventKey 生成与解析、PartitionIDFromName FNV-1a hash
- [x] 3.2 审查 segment_store.go：FileSegmentStore 的 KV key schema、LRU 缓存、PartitionState、向量搜索 stub、StoreEvent/GetEvent/QueryEvents 实现
- [x] 3.3 审查 in_memory_store.go：map[PartitionID]map[EventKey]FullEvent 结构、RelationStoreProvider 内嵌实现、keyword 过滤、与 FileSegmentStore 行为一致性
- [x] 3.4 审查 relation_store.go：InMemRelationStore 双图（childToParent/parentToChildren）、WAL journal 持久化、Snapshot + ReplayJournal 崩溃恢复
- [x] 3.5 审查 compaction.go：Compactor 的 L1→L2→L3 流程（Merge→Filter→Repair→Write→Cleanup）、filterTombstoned 实际有效性、repairDanglingRefs 通过 RelationStore 修复
- [x] 3.6 审查 lifecycle.go：LifecycleManager 的 TTL 过期策略、容量驱逐、后台 scannerLoop、默认 TTL 配置合理性
- [x] 3.7 审查 tombstone.go：TombstoneSet 的 MarkTombstone 级联修复（findAliveAncestor）、IsTombstone 查询、RecoverFromKV 崩溃恢复、死代码检查
- [x] 3.8 审查 key_schema.go：KV key 格式（evt/idx/meta/tomb 前缀）、ParseKey 解析完整性、Prefix Scan 函数
- [x] 3.9 审查 rustviking_client.go：RustVikingClient 的 CLI 调用封装、KVRange 客户端过滤、KVBatch stdin pipe、MockRustVikingClient 行为一致性

## 4. 阶段一 — plugin 层审查

- [x] 4.1 审查 memory_plugin.go：onEvent 10 步流程、PartitionID 派生、Snowflake EventKey 生成、事件类型推断、FullEvent 构建、StateDelta 写回、RelationStore.SetParent 关系设置、lastEventKeys 因果链
- [x] 4.2 审查 summary_plugin.go：OnEvent 钩子的摘要生成、Tag 注入格式

## 5. 阶段一 — event 层审查

- [x] 5.1 审查 event/types.go：7 种事件类型常量、ExtractEventType 的 Role 分类映射、IsSpecialEventType 截断保护、GenerateEventSummary 摘要生成

## 6. 阶段一 — tool 层审查

- [x] 6.1 审查 tool/command/command_tool.go：exec/tmux_exec 双模式、MessageInjector 注入机制、handleStateChange 状态变更通知、CommandProperties 配置
- [x] 6.2 审查 tool/command/tmux_executor.go：CreateSession/KillSession/GetSessionOutput、ProcessExists/IsPaneDead 检测、SendHeartbeat 心跳检测、RestartSession 同名重建
- [x] 6.3 审查 tool/command/tmux_monitor.go：monitorLoop ticker、detectSessionState 状态检测（StableSince 时间窗口）、handleFakeAlive 重启、handleFakeDead 清理、session map 并发访问
- [x] 6.4 审查 tool/command/command_executor.go：Execute 的超时和进程组清理、buildCommand 的 sudo 隔离、buildEnv 环境变量继承
- [x] 6.5 审查 tool/knowledge/knowledge_agent.go：NewAgent 配置组装、PromptConfig 加载、sub-tools 装配
- [x] 6.6 审查 tool/knowledge/knowledge_subtools.go：BuildSubTools 装配、skill_search/skill_load 渐进式披露、mcp_discover 发现、memory_query 历史知识查询、searchSkills 模糊匹配算法
- [x] 6.7 审查 tool/knowledge/websearch.go：多引擎搜索（DuckDuckGo/Bing/Baidu/Brave）、区域自动检测、HTML 解析器、结果截断
- [x] 6.8 审查 tool/recall/recall_agent.go：NewAgent 配置组装、ReadPartitionIDs 注入、getDefaultRecallPrompt 兜底文案
- [x] 6.9 审查 tool/recall/recall_subtools.go：memory_query/memory_get/memory_recent/memory_trace 四子工具、RelationStoreProvider 获取 parentKey、readPartitionIDs 自动注入
- [x] 6.10 审查 tool/accessor.go：MemoryStoreAccessor 最小接口、SkillRepository 抽象

## 7. 阶段一 — prompt 层审查

- [x] 7.1 审查 prompt/loader.go：CompositeConfig 组合加载（inline→files→dir）、LoadBootstrap 顺序加载、ErrNotExist 检查健壮性、SplitCSV 工具函数

## 8. 阶段一 — 文档审查

- [x] 8.1 审查 docs/wiki/memory/memory-architecture.md：文件列表、FileBackend 描述、ParentKey 字段、EventKey 类型（string vs int64）是否与实现一致
- [x] 8.2 审查 docs/wiki/agent/agent-architecture.md：Phase 1 事件视图转换描述、MemoryPlugin ParentKey 构建描述、设计决策章节准确性
- [x] 8.3 审查 docs/wiki/event/event-architecture.md：事件类型分类、摘要生成描述是否与实现一致
- [x] 8.4 审查 README.md：Project Structure 是否反映实际目录结构

## 9. 阶段二 — 横向维度：并发安全交叉验证

- [x] 9.1 跨模块检查所有 goroutine 间共享的 map/struct 字段：TmuxMonitor.sessions/running、MemoryPlugin.lastEventKeys、TombstoneSet.keys、InMemRelationStore 双图、FileSegmentStore.partitionStates
- [x] 9.2 检查 TmuxMonitor 的 Start/Stop/checkSession/checkAllSessions 调用链中的锁使用
- [x] 9.3 检查 CommandTool 的 tmuxMonitor 生命周期管理（启动、停止、并发访问）

## 10. 阶段二 — 横向维度：代码一致性交叉验证

- [x] 10.1 跨模块检查注释一致性：是否有注释引用已移除的 FullEvent.ParentKey 或 Phase 1 机制
- [x] 10.2 跨模块检查 log 包统一性：是否全部使用 trpc-agent-go/log，有无标准库 log 混用
- [x] 10.3 跨模块检查错误处理：是否有 `_ = err` 静默吞错、被忽略的 error 返回值
- [x] 10.4 跨模块检查 stub/TODO：是否有未接入的 stub（如 filterTombstoned）、是否有明确后续计划标注

## 11. 阶段二 — 横向维度：接口契约交叉验证

- [x] 11.1 检查 MemoryStore 接口的所有实现（InMemoryStore、FileSegmentStore）是否完整实现
- [x] 11.2 检查 RelationStoreProvider 可选接口的类型断言是否有降级路径
- [x] 11.3 检查 KVStore 接口的 MockRustVikingClient 和 RustVikingClient 行为一致性
- [x] 11.4 检查 sessionInspector 接口的 TmuxExecutor 实现完整性

## 12. 形成审查报告（第一轮 + 第二轮）

- [x] 12.1 按设计目标维度（6 项）组织审查发现，每项发现包含：模块/文件、代码位置、问题描述、严重级别（P0/P1/P2/P3）、根因分析、修复建议
- [x] 12.2 按横向维度（3 项）补充跨模块系统性发现
- [x] 12.3 汇总严重级别分布，标注最高优先级修复项
- [x] 12.4 审查报告存档，作为后续修复变更的输入

## 13. 阶段三 — 端到端数据链追踪

- [x] 13.1 **配置链追踪**：从 YAML 配置（run_as_user、workspace、env、timeout 等）→ config.go 解析 → 工厂函数（commandFactory）→ 子组件构造（NewCommandTool → NewCommandExecutor / NewTmuxExecutor）→ 实际执行点（CommandExecutor.buildCommand / TmuxExecutor.CreateSession），验证每个配置项在每条路径上到达最终执行点；重点验证 TmuxExecutor.CreateSession 是否使用了 runAsUser 和 opts.Env
- [x] 13.2 **生命周期链追踪**：从 tagent.go resolveMemoryStore / builtin.go 工厂出发，追踪所有有 Start() 方法的组件（LifecycleManager、Compactor、TmuxMonitor）的完整路径：生产入口 → 构造函数（NewXXX）→ Start() 调用 → Stop() 调用；验证每个组件在生产代码中被创建和启动，而非仅在测试代码中
- [x] 13.3 **字段使用完整性追踪**：对所有通过 Option/Builder 模式设置的结构体字段（TmuxExecutor.runAsUser、TmuxExecutor.workspace、CommandExecutor.runAsUser 等），验证字段在所有应该使用它的方法中被实际引用；对每个字段执行 grep 搜索其引用点，标记“定义了 setter 但从未在业务逻辑中使用”的死字段
- [x] 13.4 **状态回收闭环追踪**：追踪事件存储（StoreEvent → TTL 扫描 → MarkTombstone → filterTombstoned → 物理删除）和 tmux session（CreateSession → 状态检测 → KillSession → map 删除）的完整闭环；验证 TTL 过期扫描、压实调度、墓碑过滤在生产代码中均被启动，不存在“只进不出”的资源泄漏
- [x] 13.5 **数据传递链完整性追踪**：对所有函数参数和结构体字段（如 TmuxCreateOptions.Env、CommandSpec.RunAsUser、TmuxCreateOptions.IsInteractive 等），从定义点逐级追踪到最终使用点，验证每个中间环节都正确传递数据，不存在“接收了参数但从未使用”的断链
- [x] 13.6 **跨路径行为对称性追踪**：对比 exec vs tmux_exec、正常执行 vs RestartSession 重启恢复、首次创建 vs 恢复重建在权限隔离、环境变量、工作目录、超时控制方面的行为；逐一验证不存在一种路径有保护而另一种路径缺失的情况
- [x] 13.7 **可选依赖 nil 行为追踪**：对所有“optional”依赖（TombstoneSet、LifecycleManager、Compactor），验证生产环境中是否为 nil；若为 nil，追踪该依赖保护的功能是否完全失效，且这种失效是否可接受

## 14. 形成审查报告（第三轮）

- [x] 14.1 将阶段三端到端数据链追踪的发现按“配置链、生命周期链、数据传递链、状态回收链”四类组织，补充到审查报告中
- [x] 14.2 更新严重级别分布，标注新发现的 P0/P1 问题
- [x] 14.3 修订修复路线图，将新发现的问题按依赖关系排序
- [x] 14.4 审查报告存档，作为后续修复变更的输入
