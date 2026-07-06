## ADDED Requirements

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
