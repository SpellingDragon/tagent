## Why

Wiki 架构文档（6 篇）和 README.md 与实际代码存在 **30+ 处差异**，涵盖行数错误、数据结构字段过时、新增函数/接口未记录、代码逻辑与实际不符等问题。这些不一致会导致新贡献者理解偏差、架构决策依据失真，尤其在数据竞争修复（Session AppendEventHook + Response.Clone）和批量摘要等关键特性落地后，文档亟需与代码同步更新。

## What Changes

- **修订 agent-architecture.md**：更新行数（tagent_agent.go 339→434、context_intervention.go 290→216、smart_compress.go 298→446、tagent.go 230→423）；补充 `sessionMu`/`closers` 字段、Step 7-8 SessionService hook 初始化、Closer 接口、`injectEventKeyPrefixes` 函数、SmartCompressor 新增 `maxTokens` 字段和 `WithMaxTokens` 选项；重写 BeforeModel 代码段（移除不存在的 `getSessionEvents`/`applyEventView`，替换为 `injectEventKeyPrefixes` + `logAccess`）
- **修订 memory-architecture.md**：全面移除已删除的 `ParentKey` 字段引用（§4.2 表格、§5.1、§5.3、§8.3、§14.3、§14.6）；补充 `RelationStoreProvider` 接口说明；更新行数（types.go 235→250、in_memory_store.go 327→499）；`FileBackend` 更名为 `FileSegmentStore`；补充 `QueryOptions.Keyword` 字段
- **修订 tool-architecture.md**：更新行数（tool_agent.go ~160→373）；补充 AgentToolWrapper 完整实现文档（Call 方法中的 Response.Clone 防御层、event_key→外部上下文解析、finalOutput 提取逻辑）
- **修订 plugin-architecture.md**：移除 FullEvent 中已不存在的 ParentKey 字段（§10.2）；修正 MemoryPlugin Step 10 的 SetParent 实现为 type assertion `p.memStore.(memory.RelationStoreProvider)`；更新行数（memory_plugin.go 206→223）
- **修订 event-architecture.md / prompt-architecture.md**：校对行数和小细节
- **增强 README.md**：补充项目架构概览、模块关系图、核心数据流说明，反映项目实际复杂度

## Capabilities

### New Capabilities
- `wiki-code-sync`: 6 篇 wiki 架构文档与代码的全面交叉修订，修正所有行数、结构体字段、函数签名、代码逻辑与实际不符的问题
- `readme-enhancement`: README.md 增强，补充项目架构概览和核心模块关系

### Modified Capabilities
- `batched-summarization`: wiki 中 SmartCompressor 的批量摘要文档需补充 `batchSegmentsByTokenBudget`/`summarizeBatches`/`collectCompressedKeys`/`parseEventKeyFromPrefix`/`findPendingUserMessage` 等新函数
- `production-wiring-fix`: wiki 中 SessionService AppendEventHook 和 Response.Clone 防御层文档需补充

## Impact

- **文档文件**：`docs/wiki/agent/agent-architecture.md`、`docs/wiki/memory/memory-architecture.md`、`docs/wiki/tool/tool-architecture.md`、`docs/wiki/plugin/plugin-architecture.md`、`docs/wiki/event/event-architecture.md`、`docs/wiki/prompt/prompt-architecture.md`、`README.md`
- **不涉及代码修改**：本次 change 仅修订文档，不改动任何 Go 源码
- **风险**：低 — 纯文档更新，不影响编译和运行时行为
