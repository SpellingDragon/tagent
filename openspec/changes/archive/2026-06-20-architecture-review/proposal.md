## Why

tagent 基于 trpc-agent-go v1.7.0 框架，采用"事件驱动 + 内存中心 + 视图转换"的核心架构。项目经历 Phase 1-7 多轮迭代，部分机制在迭代中被移除或重构（如 Phase 1 事件视图转换、FullEvent.ParentKey 字段等）。迭代过程中代码实现可能与设计目标产生偏移，需要一次系统性的全量架构审查来识别偏移并形成审查报告，为后续修复变更提供依据。

本变更不直接修复代码，而是**定义审查标准并执行审查**，产出审查报告。审查报告将作为后续修复变更的输入。

## What Changes

### 审查范围

对 tagent 全部 53 个 Go 源文件进行逐模块、逐代码审查，覆盖以下模块：
- **组合根**：tagent.go、config.go、builtin.go
- **agent 层**：tagent_agent.go、tool_agent.go、smart_compress.go、context_intervention.go、token_counter.go
- **memory 层**：types.go、segment_store.go、in_memory_store.go、relation_store.go、compaction.go、lifecycle.go、tombstone.go、key_schema.go、rustviking_client.go
- **plugin 层**：memory_plugin.go、summary_plugin.go
- **event 层**：types.go
- **tool 层**：command/（command_tool、tmux_executor、tmux_monitor、command_executor）、knowledge/（knowledge_agent、knowledge_subtools、websearch）、recall/（recall_agent、recall_subtools）、accessor.go
- **prompt 层**：loader.go

### 审核标准（6 项设计目标）

审查以 tagent 的 6 项核心设计目标为审核标准，检查每个模块的实现一致性：

1. **事件驱动一致性**：每次 Agent 执行基于事件，事件是唯一驱动单元；事件类型分类与消息 Role 映射正确；特殊事件（external_input/agent_output/thinking_plan）不被截断
2. **内存中心一致性**：MemoryStore 是唯一事实来源（FullEvent），Session 只保存轻量引用（EventReference）；MemoryPlugin 通过 OnEvent 钩子持久化事件；PartitionID 隔离正确（AgentName → FNV-1a hash → PartitionID）
3. **视图转换完整性**：压缩只修改发给 LLM 的 Request.Messages，不破坏 Session/MemoryStore 原始数据；压缩后的 context_compress 事件应包含足够的回溯信息（如被压缩事件的 key 列表）
4. **因果关系完整性**：FullEvent 不含 ParentKey，因果关系由独立的 RelationStore 管理；SetParent/GetParent/GetChildren 操作正确；因果链按 PartitionID 独立维护；墓碑标记触发级联父引用修复
5. **上下文隔离完整性**：LLM 只输出 int64 数字 key，AgentToolWrapper 服务端解析获取完整事件内容；子 agent 通过 IngestExternalEvents 注入父 agent 事件；ReadNamespaces → ReadPartitionIDs 跨命名空间读权限正确
6. **分层存储与生命周期一致性**：L0→L1→L2→L3 压实流程完整；墓碑过滤在合并时生效；TTL 过期 + 容量驱逐 + 墓碑标记生命周期管理正确；Compactor 的 Repair 阶段修复悬空引用

### 审查维度

除设计目标一致性外，审查还覆盖以下横向维度：
- **并发安全**：所有共享状态访问（goroutine 间共享的 map、struct 字段）是否有正确的同步机制
- **代码一致性**：注释与实现是否一致；log 包使用是否统一；错误处理是否健壮（无静默吞错）；stub/TODO 是否有明确的后续计划
- **接口契约**：MemoryStore/RelationStore/KVStore 等接口的实现是否满足契约；可选接口（RelationStoreProvider）的断言是否安全

### Non-Goals

- 不在 proposal 中预设具体问题或发现——审查发现将在执行审查后产出
- 不直接修复代码——修复归后续独立变更
- 不改变任何接口签名或数据模型
- 不评价新功能设计（向量搜索、RustViking 进程池化等未实现部分）

## Capabilities

### New Capabilities

- `architecture-review-standards`: 架构审查标准，定义 6 项设计目标的可测试检查项和 3 项横向审查维度，作为逐模块审查的依据

### Modified Capabilities

（无——本变更是审查标准定义，不修改已有 spec）

## Impact

### 产出
- 审查报告：记录逐模块、逐代码审查的发现，按设计目标和审查维度分类，标注严重级别（P0 功能退化 / P1 并发隐患 / P2 代码质量 / P3 文档过时）
- 审查报告作为后续修复变更的输入，驱动独立的 fix 变更

### 审查覆盖文件
- 全部 53 个 Go 源文件（见审查范围）
- 3 份 wiki 架构文档（agent/event/memory）
- README.md

### 依赖
- `trpc-agent-go v1.7.0` 框架知识（BeforeModel 回调、OnEvent 钩子、StateDelta 机制、Session/Invocation 结构）
- tagent 设计文档（docs/wiki/）
