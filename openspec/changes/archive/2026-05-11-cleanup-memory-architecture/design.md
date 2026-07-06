## Context

`event-storage-layered-architecture` 完成后，memory 包存在新旧并存的状态：
- **新架构**：`FileSegmentStore`（RustViking KV）、`RelationStore`（InMemRelationStore）、`Compactor`、`TombstoneSet`、`LifecycleManager`
- **旧架构**：`FileBackend`（文件系统 JSONL 存储，仅保留 stub 方法以满足 MemoryStore 接口）
- **兼容层**：`emptyRelationStore`、MemoryStore 接口上的 `SetParent`（本应由 RelationStore 独立管理）、MemoryStoreAccessor 的冗余方法

tagent 处于孵化期，无向后兼容需求。这是清理架构的最佳时机。

## Goals / Non-Goals

**Goals:**
- 删除 `FileBackend` 和所有相关引用
- 精简 `MemoryStore` 接口到纯存储语义（CRUD + Query，父子关系归 RelationStore）
- 让 `FileSegmentStore` 成为 MemoryStore 的唯一实现
- 清理 `tool/accessor.go` 中的冗余接口方法
- 所有测试通过，无回归

**Non-Goals:**
- 不修改 FileSegmentStore 的核心逻辑（store/query/relation 已稳定）
- 不修改 Compaction / Tombstone / Lifecycle 模块
- 不引入新功能
- 不做数据迁移（无历史数据需要迁移）

## Decisions

### D1: 删除 FileBackend 而非标记 deprecated

**选择**：直接删除
**替代方案**：保留为 deprecated 并在未来删除
**理由**：tagent 未发布，没有外部用户依赖 FileBackend。保留只会增加维护负担和混淆。

### D2: 从 MemoryStore 接口移除 SetParent

**选择**：保持 MemoryStore 接口纯粹为 CRUD+Query，SetParent 仅存在于 RelationStore
**理由**：`event-storage-layered-architecture` 为兼容新增了 `SetParent` 到 MemoryStore，但这是错位的抽象——父子关系管理是 RelationStore 的职责。plugin 层直接调用 `store.GetRelationStore().SetParent()` 即可。

### D3: MemoryStoreAccessor 瘦身

**选择**：只保留 `QueryEvents` 和 `GetEvent`（tool 层实际使用的方法）
**替代方案**：保留所有现有方法
**理由**：accessor 是 tool 包对 memory 包的抽象层，只应暴露 tool 需要的接口。`GetParent` 等已在 recall_subtools 中通过 accessor 调用，需一并适配。

### D4: 删除 emptyRelationStore

**选择**：直接删除
**理由**：该类型是为"不需要关系跟踪"场景提供的 no-op 实现，现由 `simpleInMemRelationStore` 替代。删除可减少代码路径。

## Risks / Trade-offs

- **[Risk] 删除代码导致引用断裂** → **Mitigation**: 分步实施：先分析引用 → 逐文件修改 → 编译验证 → 测试验证
- **[Risk] plugin 测试重写工作量** → **Mitigation**: plugin 已通过 SetParent 适配，改动仅限 store 创建方式

## Migration Plan

1. 删除 `file_backend.go` 和 `file_backend_test.go`
2. 从 `types.go` 的 MemoryStore 接口移除 `SetParent`
3. 从 `InMemoryStore`、`FileSegmentStore` 移除 `SetParent` 实现
4. 删除 `emptyRelationStore`
5. 更新 `plugin/memory_plugin.go`：移除 FileBackend 路径
6. 更新 `tool/accessor.go`：精简 MemoryStoreAccessor
7. 更新所有测试文件
8. 全量编译+测试验证
