## Context

tagent 项目有 6 篇 wiki 架构文档（agent、memory、tool、plugin、event、prompt）和 1 篇 README.md，覆盖 8 个核心代码包。近期经历了两次重大代码变更（数据竞争修复、批量摘要），但文档未同步更新，导致 30+ 处 wiki-代码不一致。

**当前状态**：
- 文档中的行数引用、结构体字段、函数签名、代码示例与实际 Go 源码存在偏差
- FullEvent.ParentKey 已从代码中移除，但 memory-architecture.md 和 plugin-architecture.md 仍多处引用
- 新增的关键接口（RelationStoreProvider、Closer）和函数（injectEventKeyPrefixes、batchSegmentsByTokenBudget 等）未在文档中记录
- BeforeModel 拦截器的代码示例与实际实现有结构性差异（事件视图转换→前缀注入）
- README.md 仅 52 行，未反映项目实际复杂度

**约束**：
- 纯文档修订，不修改任何 Go 源码
- 修订以代码实际实现为准（source of truth 是 Go 代码）
- 保持 wiki 文档的中文书写风格

## Goals / Non-Goals

**Goals:**
- 修正所有 wiki 文档中的行数引用，使其与实际代码行数一致
- 移除所有对已删除字段（ParentKey）的引用，替换为 RelationStoreProvider 说明
- 补充所有新增但未记录的接口、函数、结构体字段
- 重写 BeforeModel 代码示例，匹配实际 `injectEventKeyPrefixes` 实现
- 修正 MemoryPlugin Step 10 SetParent 的 type assertion 实现
- 增强 README.md，补充架构概览和模块关系
- 建立"数据链"和"逻辑链"视角的文档组织结构

**Non-Goals:**
- 不重构文档整体结构（仅做局部修正和补充）
- 不添加新的代码测试或 CI 检查来防止文档漂移
- 不翻译或重写 event-architecture.md 和 prompt-architecture.md（仅校对小细节）
- 不修改任何 Go 源码

## Decisions

### Decision 1: 以 Go 代码为 source of truth

**选择**：所有差异以代码为准进行修订

**理由**：代码是可执行的真相，文档是辅助理解的工具。当两者不一致时，代码行为才是系统实际行为。

**替代方案**：双向校对（部分以文档为准修正代码）——不适用，因为代码经过测试验证，文档是描述性的。

### Decision 2: 按文档逐篇修订，非按主题横切

**选择**：逐篇修订 6 篇 wiki + README，每篇独立验证

**理由**：
- 每篇文档有独立的主题域，修改互不干扰
- 便于 code review 逐篇验证
- 避免大杂烩式修改难以追踪变更

### Decision 3: 数据链视角组织 agent-architecture 修订

**选择**：在 agent-architecture.md 中补充数据链视图

**数据链**：`LLM Response → Flow → Plugin(MemoryPlugin/SummaryPlugin) → Session(AppendEventHook) → MemoryStore → AgentToolWrapper → 子 Agent`

**理由**：当前文档按模块独立描述，缺少端到端数据流视角。数据链视角帮助理解事件在各组件间的流转路径。

### Decision 4: 逻辑链视角组织 context_intervention 修订

**选择**：重写 BeforeModel 代码段，反映实际的三阶段拦截链

**逻辑链**：`injectEventKeyPrefixes → TokenBudget 检查 → SmartCompress 两阶段压缩`

**理由**：wiki 当前展示的是已废弃的 `getSessionEvents + applyEventView` 事件视图转换，实际代码已替换为 `injectEventKeyPrefixes` 前缀注入方案。

## Risks / Trade-offs

- **[行数快速过时]** → 行数引用本质上是一次性快照，后续代码变更会再次失准。**缓解**：接受这是文档的固有局限，标注为"约"或给出精确数字并注明校对日期。
- **[代码示例维护负担]** → 内联代码段需要在代码变更时同步更新。**缓解**：只保留关键代码片段，使用行号引用指向源文件而非复制代码。
- **[README 增强范围模糊]** → README 应写多详细难以界定。**缓解**：定位为"项目入门索引"，包含架构概览图、模块职责表、快速启动指引，不深入实现细节。
