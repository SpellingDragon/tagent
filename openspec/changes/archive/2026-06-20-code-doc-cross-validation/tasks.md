## 1. agent-architecture.md 修订

- [x] 1.1 更新文件行数引用：tagent_agent.go 339→434、context_intervention.go 290→216、smart_compress.go 298→446、tagent.go ~230→423
- [x] 1.2 补充 TagentAgent 结构体的 `sessionMu sync.Mutex` 和 `closers []Closer` 字段
- [x] 1.3 补充 NewTagentAgent Step 7（SessionService with AppendEventHook）和 Step 8（Runner with WithSessionService）
- [x] 1.4 补充 Closer 接口定义和 RegisterCloser/Close 方法说明
- [x] 1.5 重写 BeforeModel 代码段：移除 `getSessionEvents`/`applyEventView`，替换为 `injectEventKeyPrefixes` 前缀注入
- [x] 1.6 将 `logPhaseComplete` 替换为 `logAccess`
- [x] 1.7 补充 SmartCompressor 的 `maxTokens` 字段和 `WithMaxTokens` option
- [x] 1.8 补充 `injectEventKeyPrefixes` 函数文档：跳过 system/tool 消息、按索引匹配 user/assistant、添加 `[evt_<KEY>|<type>]` 前缀
- [x] 1.9 补充数据链视角描述：LLM Response → Flow → Plugin → Session(AppendEventHook) → MemoryStore → AgentToolWrapper → 子 Agent

## 2. memory-architecture.md 修订

- [x] 2.1 移除 §4.2 FullEvent 结构体表格中的 ParentKey 行，添加 RelationStore 注释
- [x] 2.2 修正 §5.1 事件创建代码示例：移除 `evt.ParentKey` 赋值，改为 `rsp.RelationStore().SetParent()`
- [x] 2.3 修正 §5.3 代码示例中的 ParentKey 引用
- [x] 2.4 修正 §8.3 查询代码示例中的 ParentKey 引用
- [x] 2.5 修正 §14.3 和 §14.6 中的 ParentKey 引用
- [x] 2.6 补充 `RelationStoreProvider` 接口定义和 type assertion 使用方式
- [x] 2.7 更新文件行数：types.go 235→250、in_memory_store.go 327→499
- [x] 2.8 将所有 `FileBackend` 替换为 `FileSegmentStore`
- [x] 2.9 补充 `QueryOptions.Keyword` 字段说明

## 3. tool-architecture.md 修订

- [x] 3.1 更新文件行数：tool_agent.go ~160→373
- [x] 3.2 补充 AgentToolWrapper 完整实现文档：Call 方法流程、Response.Clone() 防御层、event_key 解析、finalOutput 提取

## 4. plugin-architecture.md 修订

- [x] 4.1 移除 §10.2 FullEvent 结构体中的 ParentKey 字段
- [x] 4.2 修正 MemoryPlugin Step 10 SetParent 代码为 type assertion 方式：`p.memStore.(memory.RelationStoreProvider)`
- [x] 4.3 更新文件行数：memory_plugin.go 206→223

## 5. event-architecture.md / prompt-architecture.md 校对

- [x] 5.1 校对 event-architecture.md 行数和小细节
- [x] 5.2 校对 prompt-architecture.md 行数和小细节
- [x] 5.3 全局搜索所有 wiki 文档中的 `ParentKey` 引用，确保零残留

## 6. README.md 增强

- [x] 6.1 补充项目架构概览：核心模块列表（agent/memory/tool/plugin/event/prompt/config）及职责描述
- [x] 6.2 补充模块依赖关系说明：tagent.go 作为组装入口
- [x] 6.3 补充核心数据流描述：用户请求 → TagentAgent → Runner → LLM → BeforeModel → Plugin → Session → MemoryStore
- [x] 6.4 补充基于 config.go 声明式配置的快速启动指引（最小 YAML 示例 + `tagent.New(cfg)` 调用）

## 7. 验证

- [x] 7.1 逐一核对 tasks.md 中所有 checkbox，确认每项已完成
- [x] 7.2 在所有 wiki 文档中 grep `ParentKey`，确认零匹配
- [x] 7.3 在所有 wiki 文档中 grep `FileBackend`，确认零匹配
- [x] 7.4 在所有 wiki 文档中 grep `getSessionEvents`，确认零匹配
- [x] 7.5 在所有 wiki 文档中 grep `applyEventView`，确认零匹配
- [x] 7.6 在所有 wiki 文档中 grep `logPhaseComplete`，确认零匹配
- [x] 7.7 核对所有行数引用与实际文件行数一致
