# wiki-code-sync Specification

## Purpose
wiki 架构文档与 Go 源码的交叉校验规则：行数引用、字段名称、代码示例一致性。

## Requirements
### Requirement: agent-architecture.md 行数与结构体字段同步

agent-architecture.md 中引用的文件行数和结构体字段 SHALL 与实际 Go 源码一致。具体修正：
- tagent_agent.go 行数 339→434
- context_intervention.go 行数 290→216
- smart_compress.go 行数 298→446
- tagent.go 行数 ~230→423
- TagentAgent 结构体 SHALL 补充 `sessionMu sync.Mutex` 和 `closers []Closer` 字段
- NewTagentAgent 初始化步骤 SHALL 补充 Step 7（SessionService with AppendEventHook）和 Step 8（Runner with session service）
- Closer 接口定义 SHALL 记录在文档中

#### Scenario: 行数引用准确

- **WHEN** 阅读 agent-architecture.md 中关于 tagent_agent.go 的行数引用
- **THEN** 文档显示的数与实际文件行数一致（434 行）

#### Scenario: TagentAgent 结构体完整

- **WHEN** 阅读 agent-architecture.md 中 TagentAgent 结构体定义
- **THEN** 文档包含 `sessionMu sync.Mutex` 字段
- **AND** 文档包含 `closers []Closer` 字段

#### Scenario: NewTagentAgent 步骤完整

- **WHEN** 阅读 agent-architecture.md 中 NewTagentAgent 初始化流程
- **THEN** 文档包含 Step 7 SessionService 创建（含 AppendEventHook）
- **AND** 文档包含 Step 8 Runner 创建（含 WithSessionService 选项）

### Requirement: agent-architecture.md BeforeModel 代码段重写

agent-architecture.md 中 BeforeModel 拦截器的代码示例 SHALL 与实际 context_intervention.go 实现一致。具体修正：
- 移除不存在的 `getSessionEvents` 和 `applyEventView` 函数
- 替换为 `injectEventKeyPrefixes` 前缀注入方案
- `logPhaseComplete` SHALL 替换为 `logAccess`
- SmartCompressor 结构体 SHALL 补充 `maxTokens` 字段
- `WithMaxTokens` option SHALL 记录

#### Scenario: BeforeModel 代码段无虚构函数

- **WHEN** 阅读 agent-architecture.md 中 BeforeModel 拦截器代码
- **THEN** 代码段不包含 `getSessionEvents` 函数调用
- **AND** 代码段不包含 `applyEventView` 函数调用
- **AND** 代码段包含 `injectEventKeyPrefixes` 函数调用

#### Scenario: 日志函数名一致

- **WHEN** 阅读 agent-architecture.md 中 context_intervention 的日志调用
- **THEN** 使用 `logAccess` 而非 `logPhaseComplete`

#### Scenario: SmartCompressor 字段完整

- **WHEN** 阅读 agent-architecture.md 中 SmartCompressor 结构体
- **THEN** 文档包含 3 个字段（包括 `maxTokens`）

### Requirement: agent-architecture.md 补充 injectEventKeyPrefixes 文档

agent-architecture.md SHALL 记录 `injectEventKeyPrefixes` 函数的完整行为：跳过 system/tool 消息，按索引匹配 user/assistant 消息到事件，添加 `[evt_<KEY>|<type>]` 前缀。

#### Scenario: injectEventKeyPrefixes 行为描述

- **WHEN** 阅读 agent-architecture.md 中 injectEventKeyPrefixes 文档
- **THEN** 文档说明跳过 system 和 tool role 消息
- **AND** 文档说明按索引匹配 user/assistant 消息到 Session 事件
- **AND** 文档说明添加 `[evt_<KEY>|<type>]` 格式前缀

### Requirement: memory-architecture.md 移除 ParentKey 引用

memory-architecture.md 中所有对 FullEvent.ParentKey 字段的引用 SHALL 移除或替换为 RelationStore 说明。影响区域：§4.2 表格、§5.1、§5.3、§8.3、§14.3、§14.6。

#### Scenario: §4.2 表格无 ParentKey

- **WHEN** 阅读 memory-architecture.md §4.2 FullEvent 结构体表格
- **THEN** 表格不包含 ParentKey 行
- **AND** 表格包含注释说明因果关系由 RelationStore 维护

#### Scenario: §5.1 代码示例无 ParentKey

- **WHEN** 阅读 memory-architecture.md §5.1 事件创建代码示例
- **THEN** 代码不设置 `evt.ParentKey` 字段
- **AND** 代码通过 `rsp.RelationStore().SetParent(eventKey, parentKey)` 设置因果关系

#### Scenario: §8.3 查询代码示例无 ParentKey

- **WHEN** 阅读 memory-architecture.md §8.3 查询相关代码示例
- **THEN** 代码不使用 `evt.ParentKey` 访问字段

### Requirement: memory-architecture.md 补充 RelationStoreProvider 接口

memory-architecture.md SHALL 记录 `RelationStoreProvider` 接口定义及其使用方式：`MemoryStore` 可通过 type assertion 转为 `RelationStoreProvider`，调用 `RelationStore()` 获取 `RelationStore`。

#### Scenario: RelationStoreProvider 接口文档

- **WHEN** 阅读 memory-architecture.md 中 RelationStoreProvider 文档
- **THEN** 文档包含接口定义：`RelationStoreProvider interface { RelationStore() RelationStore }`
- **AND** 文档说明通过 type assertion 使用

### Requirement: memory-architecture.md 行数和命名同步

memory-architecture.md 中的文件行数和类型名称 SHALL 与实际代码一致：
- types.go 行数 235→250
- in_memory_store.go 行数 327→499
- `FileBackend` SHALL 替换为 `FileSegmentStore`
- `QueryOptions.Keyword` 字段 SHALL 记录

#### Scenario: 行数引用准确

- **WHEN** 阅读 memory-architecture.md 中 types.go 的行数引用
- **THEN** 文档显示 250 行

#### Scenario: FileBackend 已更名

- **WHEN** 搜索 memory-architecture.md 中的 "FileBackend"
- **THEN** 返回零结果
- **AND** 所有引用已替换为 "FileSegmentStore"

#### Scenario: QueryOptions 包含 Keyword

- **WHEN** 阅读 memory-architecture.md 中 QueryOptions 结构体
- **THEN** 文档包含 `Keyword string` 字段

### Requirement: tool-architecture.md AgentToolWrapper 完整文档

tool-architecture.md SHALL 记录 AgentToolWrapper 的完整实现，包括：
- 行数 ~160→373
- Call 方法中的 Response.Clone() 防御层
- event_key → 外部上下文解析逻辑
- finalOutput 提取逻辑（取最后一个有效 choice）

#### Scenario: AgentToolWrapper 行数准确

- **WHEN** 阅读 tool-architecture.md 中 tool_agent.go 的行数引用
- **THEN** 文档显示 373 行

#### Scenario: Response.Clone 防御层文档

- **WHEN** 阅读 tool-architecture.md 中 AgentToolWrapper.Call 方法文档
- **THEN** 文档说明对 evt.Response 调用 Clone() 方法
- **AND** 文档说明 Clone 的目的是防御性隔离

### Requirement: plugin-architecture.md ParentKey 和 SetParent 修正

plugin-architecture.md SHALL 移除 FullEvent 中的 ParentKey 字段（§10.2），并修正 MemoryPlugin Step 10 的 SetParent 实现为 type assertion 方式。

#### Scenario: §10.2 FullEvent 无 ParentKey

- **WHEN** 阅读 plugin-architecture.md §10.2 FullEvent 结构体
- **THEN** 不包含 ParentKey 字段

#### Scenario: Step 10 SetParent 使用 type assertion

- **WHEN** 阅读 plugin-architecture.md 中 MemoryPlugin Step 10 SetParent 实现
- **THEN** 代码使用 `p.memStore.(memory.RelationStoreProvider)` type assertion
- **AND** 包含 ok 检查保护

#### Scenario: memory_plugin.go 行数准确

- **WHEN** 阅读 plugin-architecture.md 中 memory_plugin.go 的行数引用
- **THEN** 文档显示 223 行

### Requirement: recall_subtools.go 代码示例无 ParentKey

所有 wiki 文档中引用 recall_subtools.go 的代码示例 SHALL 不访问 `evt.ParentKey` 字段。

#### Scenario: recall 相关代码示例无 ParentKey

- **WHEN** 搜索所有 wiki 文档中对 `ParentKey` 的引用
- **THEN** 返回零结果（除非在历史说明上下文中）
