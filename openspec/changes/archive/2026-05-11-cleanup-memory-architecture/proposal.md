## Why

`event-storage-layered-architecture` 引入了分层存储架构（FileSegmentStore + RelationStore + Compaction + Tombstone/Lifecycle），但保留了旧的 FileBackend 和大量兼容性 stub（emptyRelationStore、FileBackend 的 GetParent/SetParent stub、accessor 双重接口等）。tagent 处于孵化期，尚未公开发布，无需承担向后兼容包袱。现在是在正式发布前清理架构、确保代码纯净的最佳时机。

## What Changes

- **BREAKING** 删除 `memory/file_backend.go` 及其测试（旧文件系统存储）
- **BREAKING** 删除 `memory/relation_store.go` 中的 `emptyRelationStore`（不再需要 no-op 兼容）
- 简化 `MemoryStore` 接口，移除仅为兼容新增的 `SetParent` 方法（父子关系由 RelationStore 独立管理）
- 清理 `tool/accessor.go` 中的 `MemoryStoreAccessor` 接口（瘦身到真正需要的 QueryEvents / GetEvent）
- 删除所有 `file_backend` 相关的配置引用和测试
- **BREAKING** `plugin/memory_plugin.go` 直接使用 `FileSegmentStore` 作为唯一存储实现

## Capabilities

### New Capabilities
<!-- This is a pure cleanup change - no new capabilities introduced. 
     All new capabilities were already introduced in event-storage-layered-architecture. -->

### Modified Capabilities
- `memory-store-interface`: 精简 MemoryStore 接口，移除兼容方法，FileSegmentStore 成为唯一实现
- `memory-plugin`: MemoryPlugin 不再支持双模式，直接绑定 FileSegmentStore

## Impact

- `memory/` 包：删除 `file_backend.go`、`file_backend_test.go`；精简 `types.go` 接口；清理 `relation_store.go`
- `tool/` 包：精简 `accessor.go` 的 MemoryStoreAccessor 接口
- `plugin/` 包：`memory_plugin.go` 移除 FileBackend 路径，直接使用 FileSegmentStore
- 测试文件：更新所有引用 FileBackend 的测试用例
