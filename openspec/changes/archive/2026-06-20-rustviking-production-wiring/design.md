## Context

当前代码实现现状：

- `FileSegmentStore` + `RustVikingClient` + `MockRustVikingClient` + KV key schema 已完整实现
- `InMemRelationStore`（WAL + snapshot）和 `simpleInMemRelationStore`（纯内存）均已实现
- `MemoryStore` 接口已精简（移除 SetParent/GetParent/GetChildren）
- `MemoryPlugin` / `recall_subtools` 已改为通过类型断言访问 RelationStore

但生产接线缺失：

| 组件 | 当前（临时） | 目标 |
|------|------------|------|
| KV 存储 | `NewMockRustVikingClient()` 内存 map | `NewRustVikingClient(binaryPath, dataDir)` RocksDB |
| 关系图 | `newSimpleInMemRelationStore()` 无持久化 | `NewInMemRelationStore(dataDir)` WAL+快照 |
| RelationStore 访问 | 类型断言 `interface{ RelationStore() memory.RelationStore }` | `RelationStoreProvider` 接口 |

## Goals / Non-Goals

**Goals:**
- 生产环境 `resolveMemoryStore` 创建 `RustVikingClient`（真实 CLI 调用）
- 生产环境 `FileSegmentStore` 使用 `InMemRelationStore`（带持久化）
- `MemoryConfig` 新增 `rustviking_binary` 配置项
- 定义 `RelationStoreProvider` 接口，消除脆弱的类型断言
- 修复 `types.go` 中过时的注释

**Non-Goals:**
- 不引入 RustViking gRPC/HTTP server 模式（仅 CLI 模式）
- 不修改 KV key schema（已对齐设计文档）
- 不实现新的查询能力

## Decisions

### D1: RustVikingClient 通过 MemoryConfig 配置

```
tagent.yaml:
  memory:
    type: file
    path: .tagent-data/events    # RocksDB 数据目录
    rustviking_binary: rustviking  # CLI 路径（可选，默认 "rustviking"）
```

`rustviking_binary` 为空时默认 `"rustviking"`（查找 PATH），保持向后兼容。

### D2: RelationStoreProvider 接口

当前访问 RelationStore 的模式：

```go
// 脆弱：类型断言
if rs, ok := p.memStore.(interface{ RelationStore() memory.RelationStore }); ok {
    rs.RelationStore().SetParent(...)
}
```

改为定义明确定义的接口：

```go
// memory/types.go
type RelationStoreProvider interface {
    RelationStore() RelationStore
}

// 使用
if rsp, ok := p.memStore.(RelationStoreProvider); ok {
    rsp.RelationStore().SetParent(...)
}
```

`FileSegmentStore` 和 `InMemoryStore` 均已实现 `RelationStore()` 方法，接口天然满足。

### D3: 测试保持使用 Mock

单元测试继续使用 `MockRustVikingClient` + `simpleInMemRelationStore`。生产路径仅在 `tagent.go` 的 `resolveMemoryStore` 中切换。

### D4: FileSegmentStore 构造函数保持统一

`NewFileSegmentStore(kv, rel, dataDir, cacheSize)` 签名不变：
- 生产：`NewFileSegmentStore(NewRustVikingClient(binary, dataDir), NewInMemRelationStore(dataDir), dataDir, 1000)`
- 测试：`NewFileSegmentStore(NewMockRustVikingClient(), nil, ":memory:", 100)`

RelationStore 传 nil 时自动 fallback 到 `simpleInMemRelationStore`。

## Risks / Trade-offs

- **[Risk] RustViking CLI 进程启动开销（~1ms/次）** → 缓解：批量操作（batch/KVScan）摊薄；未来可按需评估 Library 嵌入
- **[Risk] JSON 序列化/反序列化开销** → 可忽略（<10μs，远小于 LLM 延迟）
- **[Risk] rustviking 二进制未安装导致启动失败** → 启动时健康检查 `rustviking kv put --help`；失败时明确报错

## Migration Plan

1. 在 `MemoryConfig` 添加 `RustVikingBinary` 字段（可选，默认 `"rustviking"`）
2. 修改 `resolveMemoryStore` 创建 `RustVikingClient` + `InMemRelationStore`
3. 定义 `RelationStoreProvider` 接口，替换所有类型断言
4. 更新 wechat-bot 示例配置
5. `go build ./...` + `go test ./...` 验证
