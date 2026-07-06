## 1. 撤销注册表方案

- [x] 1.1 `config.go`: 删除 `MemoryConfig.Name` 字段及其注释；新增 `ReadNamespaces []string` 字段（`json:"read_namespaces,omitempty" yaml:"read_namespaces,omitempty"`）
- [x] 1.2 `tagent.go`: 删除 `import "sync"`；删除全局 `fileBackendMu`/`fileBackendStores`/`namedMemMu`/`namedMemStores` 变量及注释块
- [x] 1.3 `tagent.go`: `resolveMemoryStore` 恢复为简单工厂（去除 filepath.Abs、注册表查找/存储逻辑、InMemoryStore Name 分支）
- [x] 1.4 `tagent_test.go`: 删除 `TestFileBackend_RegistryDedup`、`TestInMemoryStore_RegistryNamedSharing`、`TestCrossAgentFileBackendRead`

## 2. 实现 ReadNamespaces → ReadPartitionIDs 注入

- [x] 2.1 `agent/tool_agent.go`: `ToolAgentFactoryConfig` 新增 `ReadPartitionIDs []int` 字段
- [x] 2.2 `tagent.go`: `buildAgent` 中在构造 `ToolAgentFactoryConfig` 前，将 `acfg.Memory.ReadNamespaces` 逐个通过 `memory.PartitionIDFromName` 转换为 `readPartitionIDs []int`，赋值给 `factoryCfg.ReadPartitionIDs`
- [x] 2.3 `tagent.go`: `resolveMemoryStore` 不再接收 `path/filepath` import（若无其他用途则移除）

## 3. recall 子工具内部注入

- [x] 3.1 `tool/recall/recall_subtools.go`: 回退 `recallQueryArgs.PartitionIDs` 和 `recallRecentArgs.PartitionIDs` 字段
- [x] 3.2 `tool/recall/recall_subtools.go`: `NewRecallQueryTool` 签名改为 `(accessor, readPartitionIDs []int)`，handler 内将 `readPartitionIDs` 合并到 `opts.PartitionIDs`
- [x] 3.3 `tool/recall/recall_subtools.go`: `NewRecallRecentTool` 同上
- [x] 3.4 `tool/recall/recall_subtools.go`: 更新 `memory_query` 和 `memory_recent` 的 Description（移除 partition_ids 引用）
- [x] 3.5 `tool/recall/recall_subtools.go`: `buildRecallSubTools` 签名改为 `(accessor, readPartitionIDs []int)`，传给子工具构造函数
- [x] 3.6 `tool/recall/recall_agent.go`: 更新 `NewRecallAgent` 中 `buildRecallSubTools` 的调用，传入 `cfg.ReadPartitionIDs`

## 4. 样例配置 & 验证

- [x] 4.1 `examples/wechat-bot/tagent.yaml`: recall 的 memory 改为 `type: file; path: .wechat-config/agent-events; read_namespaces: [tagent]`（即用路径共享替代实例共享）
- [x] 4.2 运行 `go build ./...` 确认全量编译通过
- [x] 4.3 运行 `go test ./...` 确认所有测试无回归
- [x] 4.4 编译 wechat-bot 示例确认无错误
