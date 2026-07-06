# production-wiring-fix Specification

## Purpose
TBD - created by archiving change production-readiness-fix. Update Purpose after archive.
## Requirements
### Requirement: 生产入口创建并启动生命周期组件

当 MemoryConfig.Type 为 "file" 时，resolveMemoryStore SHALL 创建 TombstoneSet 并注入 FileSegmentStore，创建 LifecycleManager 并调用 Start()，创建 Compactor 并调用 Start()。这些组件 MUST 在返回 MemoryStore 之前全部启动。

#### Scenario: file 类型存储完整接线

- **WHEN** resolveMemoryStore 接收 MemoryConfig{Type: "file", Path: "/data/tagent"}
- **THEN** 创建 FileSegmentStore 后，创建 TombstoneSet 并调用 store.SetTombstoneSet()
- **AND** 创建 LifecycleManager(store, tombstone, config) 并调用 lm.Start()
- **AND** 创建 Compactor(store, kv, rel, tombstone, config) 并调用 compactor.Start()
- **AND** 返回的 MemoryStore 已具备 TTL 扫描、压实调度、墓碑过滤能力

#### Scenario: memory 类型存储不创建生命周期组件

- **WHEN** resolveMemoryStore 接收 MemoryConfig{Type: "memory"}
- **THEN** 仅创建 InMemoryStore，不创建 LifecycleManager/Compactor/TombstoneSet
- **AND** InMemoryStore 不需要墓碑过滤（全内存，无持久化）

### Requirement: FileSegmentStore 支持构造后注入 TombstoneSet

FileSegmentStore SHALL 提供 SetTombstoneSet(ts *TombstoneSet) 方法，允许在构造后注入 TombstoneSet。注入后，GetEvent 和 QueryEvents MUST 使用 TombstoneSet.IsTombstone() 过滤已标记墓碑的事件。

#### Scenario: 未注入 TombstoneSet 时跳过墓碑检查

- **WHEN** FileSegmentStore 的 tombstones 字段为 nil
- **THEN** GetEvent 返回事件（即使该事件理论上应被墓碑标记）
- **AND** QueryEvents 返回所有匹配事件（不过滤墓碑）

#### Scenario: 注入 TombstoneSet 后过滤墓碑事件

- **WHEN** FileSegmentStore 注入了 TombstoneSet，且事件 key=42 已被 MarkTombstone(42)
- **THEN** GetEvent(42) 返回 nil, nil（存在但已墓碑）
- **AND** QueryEvents 的结果不包含 key=42 的事件

### Requirement: 优雅关闭流程

FileSegmentStore SHALL 提供 Close() 方法，按顺序停止 Compactor → 停止 LifecycleManager → flush TombstoneSet 持久化 → 关闭 RelationStore。Close() MUST 幂等（多次调用不 panic）。

#### Scenario: Close 停止所有后台组件

- **WHEN** 调用 store.Close()
- **THEN** Compactor.Stop() 被调用（停止压实调度）
- **AND** LifecycleManager.Stop() 被调用（停止 TTL 扫描）
- **AND** TombstoneSet 持久化到 KV 存储
- **AND** RelationStore.Close() 被调用

#### Scenario: Close 幂等

- **WHEN** 连续调用 store.Close() 两次
- **THEN** 第二次调用不 panic、不 error
- **AND** 后台组件不会被重复停止

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
