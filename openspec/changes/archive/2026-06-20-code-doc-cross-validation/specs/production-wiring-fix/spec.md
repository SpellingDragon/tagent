## ADDED Requirements

### Requirement: wiki 记录 SessionService AppendEventHook

wiki 文档 SHALL 记录 NewTagentAgent 中 SessionService 的创建和 AppendEventHook 配置：
- SessionService 通过 `sessioninmemory.NewSessionService()` 创建
- AppendEventHook 在 Session 追加事件前创建事件副本（含 Response.Clone()）
- Hook 执行后恢复原始事件指针，确保后续 Plugin 看到原始数据
- 此 Hook 解决 Session 和 Plugin 并发访问同一 Response 的数据竞争

#### Scenario: AppendEventHook 行为描述

- **WHEN** 阅读 wiki 中 SessionService AppendEventHook 文档
- **THEN** 文档说明 Hook 在 `next()` 调用前创建 `evtCopy`（浅拷贝）
- **AND** 文档说明 Hook 对 `original.Response` 调用 `Clone()` 创建深拷贝
- **AND** 文档说明 Hook 在 `next()` 返回后恢复 `ctx.Event = original`

#### Scenario: Response.Clone 防御层说明

- **WHEN** 阅读 wiki 中数据竞争修复说明
- **THEN** 文档说明 AgentToolWrapper.Call 中也对 evt.Response 调用 Clone()
- **AND** 文档说明这是 defense-in-depth 策略（两层独立 Clone）

### Requirement: wiki 记录 resolveMemoryStore 完整组装

wiki 文档 SHALL 记录 tagent.go 中 resolveMemoryStore 的完整组装逻辑（file 类型）：
- `NewInMemRelationStore(mc.Path)` 创建关系存储
- `NewRustVikingClient(mc.RustVikingBinary, configPath)` 创建 KV 客户端
- `NewFileSegmentStore(kv, rel, mc.Path, 1000)` 创建文件段存储
- `NewTombstoneSet(rel, kv, 0)` 创建墓碑集
- `NewLifecycleManager(store, tombstone, config)` 创建生命周期管理器
- `NewCompactor(store, kv, rel, tombstone, config)` 创建压实器

#### Scenario: resolveMemoryStore 组装文档

- **WHEN** 阅读 wiki 中 resolveMemoryStore 文档
- **THEN** 文档列出 file 类型的完整组件创建顺序
- **AND** 文档说明 RustVikingClient 是 KV 存储后端
- **AND** 文档说明 InMemRelationStore 维护事件关系索引
