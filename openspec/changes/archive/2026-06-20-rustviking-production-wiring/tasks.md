## 1. MemoryConfig 新增 rustviking_binary 字段

- [x] 1.1 在 `config/config.go` 的 `MemoryConfig` 中添加 `RustVikingBinary string` 字段（`json:"rustviking_binary,omitempty" yaml:"rustviking_binary,omitempty"`）
- [x] 1.2 更新 wechat-bot 示例配置 `examples/wechat-bot/tagent.yaml`：添加 `rustviking_binary: rustviking`

## 2. 定义 RelationStoreProvider 接口

- [x] 2.1 在 `memory/types.go` 中定义 `RelationStoreProvider` 接口（`RelationStore() RelationStore`）

## 3. 替换匿名接口为 RelationStoreProvider

- [x] 3.1 在 `plugin/memory_plugin.go` 中将 `p.memStore.(interface{ RelationStore() memory.RelationStore })` 替换为 `memory.RelationStoreProvider`
- [x] 3.2 在 `tool/recall/recall_subtools.go` 中的两处 `interface{ RelationStore() memory.RelationStore }` 替换为 `memory.RelationStoreProvider`

## 4. 生产环境接线（真实 RustVikingClient + InMemRelationStore）

- [x] 4.1 修改 `tagent.go` 的 `resolveMemoryStore`：使用 `NewRustVikingClient(cfg.RustVikingBinary, mc.Path)` 替代 `NewMockRustVikingClient()`
- [x] 4.2 修改 `tagent.go` 的 `resolveMemoryStore`：使用 `NewInMemRelationStore(mc.Path)` 替代 `nil`（生产环境带 WAL 持久化）

## 5. 修复过时注释

- [x] 5.1 修复 `memory/types.go` 中 `FullEvent` 的注释：移除 "accessible via MemoryStore.GetParent() / GetChildren() methods"（这些方法已从 MemoryStore 移除）

## 6. 验证

- [x] 6.1 `go build ./...` 全量编译通过
- [x] 6.2 `go vet ./...` 静态分析通过
- [x] 6.3 `go test ./... -count=1` 全量测试通过
