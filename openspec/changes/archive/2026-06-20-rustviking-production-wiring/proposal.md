## Why

当前代码已完成了事件存储分层架构的核心组件（FileSegmentStore、RelationStore、RustVikingClient、KV key schema 等），但 **生产环境接线未完成**——`tagent.go` 和所有测试中均使用 `MockRustVikingClient`（内存 map），事件数据不会被持久化到 RocksDB。同时 `RelationStore` 也使用了 `simpleInMemRelationStore`（无 WAL/快照），崩溃后关系图会丢失。

## What Changes

- 将 `tagent.go` 的 `resolveMemoryStore` 从 `NewMockRustVikingClient()` 切换到真实 `RustVikingClient`
- 为 `MemoryConfig` 新增 `rustviking_binary` 字段，指定 rustviking CLI 路径
- 生产环境使用 `InMemRelationStore`（WAL + 快照），替代 `simpleInMemRelationStore`
- 定义 `RelationStoreProvider` 接口，替代脆弱的类型断言
- 修复 `FullEvent` 注释中引用已移除方法的过时描述

## Capabilities

### New Capabilities
- `rustviking-config`: MemoryConfig 新增 `rustviking_binary` 字段，支持指定 CLI 路径
- `relation-store-provider`: 定义 `RelationStoreProvider` 接口，统一 RelationStore 访问

### Modified Capabilities
<!-- 无已有 spec 需修改 -->

## Impact

- **Affected code**: `tagent.go`, `memory/types.go`, `memory/segment_store.go`, `plugin/memory_plugin.go`, `tool/recall/recall_subtools.go`, `agent/tool_agent.go`
- **配置变更**: MemoryConfig 新增字段（需更新 yaml 示例）
- **Breaking**: 无（Mock 仍可用于测试）
