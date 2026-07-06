## 1. MemoryStore 实例注册表

- [x] 1.1 `config.go`: `MemoryConfig` 新增 `Name string` 字段（`json:"name,omitempty" yaml:"name,omitempty"`）
- [x] 1.2 `tagent.go`: 新增全局实例注册表变量（`fileBackendMu` + `fileBackends map[string]*FileBackend`、`namedMemMu` + `namedMemStores map[string]*InMemoryStore`）
- [x] 1.3 `tagent.go`: 重写 `resolveMemoryStore()`：FileBackend 按 `filepath.Abs(path)` 去重，InMemoryStore 按 `Name` 非空时去重
- [x] 1.4 `tagent_test.go`: 补充测试 `TestFileBackend_RegistryDedup` — 同路径返回同一实例
- [x] 1.5 `tagent_test.go`: 补充测试 `TestInMemoryStore_RegistryNamedSharing` — 同名共享、不同名隔离、无名保持独立

## 2. recall 子工具 PartitionID 过滤

- [x] 2.1 `tool/recall/recall_subtools.go`: `recallQueryArgs` 新增 `PartitionIDs []int` 字段（`json:"partition_ids,omitempty"`），`NewRecallQueryTool` 中将该字段传入 `QueryOptions.PartitionIDs`
- [x] 2.2 `tool/recall/recall_subtools.go`: `recallRecentArgs` 新增 `PartitionIDs []int` 字段，`NewRecallRecentTool` 中传入 `QueryOptions.PartitionIDs`
- [x] 2.3 `tool/recall/recall_subtools.go`: 更新 `memory_query` 和 `memory_recent` 的 `Description`，说明 `partition_ids` 参数用途
- [x] 2.4 运行 `go build ./...` 确认编译通过

## 3. 集成验证

- [x] 3.1 运行 `go build ./...` 确认全量编译通过
- [x] 3.2 运行 `go test ./...` 确认所有现有测试无回归
- [x] 3.3 `tagent_test.go`: 补充 `TestCrossAgentFileBackendRead` — 验证 tagent store 写入的事件可通过 recall store 读取
