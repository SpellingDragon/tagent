## 1. 删除 FileBackend 旧代码

- [x] 1.1 删除 `memory/file_backend.go`：移除整个 FileBackend 类型及其所有方法
- [x] 1.2 删除 `memory/file_backend_test.go`：移除对应的测试文件
- [x] 1.3 扫描全项目移除 FileBackend 引用：搜索所有 `file_backend`、`FileBackend` 引用并清理

## 2. 精简 MemoryStore 接口

- [x] 2.1 从 `memory/types.go` 的 MemoryStore 接口移除 `SetParent` 方法
- [x] 2.2 从 `memory/in_memory_store.go` 移除 `SetParent` 方法实现
- [x] 2.3 从 `memory/segment_store.go` 移除 `SetParent` 方法实现
- [x] 2.4 在 FileSegmentStore 上添加公共 getter `RelationStore() RelationStore`
- [x] 2.5 编译验证接口变更无断裂

## 3. 清理 emptyRelationStore

- [x] 3.1 从 `memory/relation_store.go` 删除 `emptyRelationStore` 类型及其所有方法
- [x] 3.2 检查所有 `emptyRelationStore{}` 引用，替换为 `newSimpleInMemRelationStore()` 或 nil
- [x] 3.3 编译验证

## 4. 适配 MemoryPlugin

- [x] 4.1 修改 `plugin/memory_plugin.go`：移除 FileBackend 创建路径，始终创建 FileSegmentStore
- [x] 4.2 将 `SetParent` 调用改为通过 RelationStore 访问：`store.RelationStore().SetParent(eventKey, parentKey)`
- [x] 4.3 移除不再使用的 FileBackend 相关 import 和配置字段
- [x] 4.4 编译验证

## 5. 适配 tool/accessor

- [x] 5.1 精简 `tool/accessor.go` 的 MemoryStoreAccessor 接口：移除 `GetParent` 等方法（仅保留 QueryEvents、GetEvent）
- [x] 5.2 更新 `tool/recall/recall_subtools.go`：GetParent 调用改为通过具体 store 类型访问 RelationStore
- [x] 5.3 更新所有 accessor 实现类以满足精简后的接口

## 6. 测试更新与全量验证

- [x] 6.1 更新 `plugin/memory_plugin_test.go`：使用 FileSegmentStore 替代 FileBackend
- [x] 6.2 更新 `memory/` 包中引用 FileBackend 的测试用例
- [x] 6.3 `go build ./...` 全量编译通过
- [x] 6.4 `go vet ./...` 静态分析通过
- [x] 6.5 `go test ./...` 全量测试通过（除预存在的集成测试）
