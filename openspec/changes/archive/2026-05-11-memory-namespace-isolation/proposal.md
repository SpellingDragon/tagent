## Why

之前的 `memory-store-sharing` 方案通过全局实例注册表实现 store 共享——同路径 FileBackend 返回同一实例。但实例级 dedup 是错误抽象：真正的需求是**同类型存储按命名空间隔离数据**，而非实例共享。FileBackend 天然支持多命名空间（per-partition 子目录），多个独立实例指向同一路径可以各自读写不同分区。正确方案是让配置显式声明跨命名空间读权限，而非在实例层做文章。

## What Changes

- 撤销 `memory-store-sharing` 变更中引入的全局实例注册表（`fileBackendStores`、`namedMemStores` 变量及 dedup 逻辑）
- 撤销 `MemoryConfig.Name` 字段
- `MemoryConfig` 新增 `ReadNamespaces []string` 字段：声明本 agent 可读取的其他 agent 的命名空间
- **BREAKING** recall 子工具不再需要 LLM 传入 `partition_ids`；改为由 factory 层根据 `ReadNamespaces` 自动注入，工具声明保持简洁
- `buildAgent` 将 ReadNamespaces 解析为 PartitionID 列表，通过 `ToolAgentFactoryConfig` 注入 recall factory
- wechat-bot 样例配置：recall 的 `read_namespaces: [tagent]`

## Capabilities

### New Capabilities
- `read-namespaces-config`: `MemoryConfig.ReadNamespaces` 字段允许 agent 声明跨命名空间读权限，替代基于实例共享的旧方案
- `namespace-scoped-store`: MemoryStore 实例化时绑定命名空间上下文，自动将 ReadNamespaces 注入查询参数

### Modified Capabilities
<!-- 无现有 spec 需要修改 -->

## Impact

| 文件 | 影响 |
|------|------|
| `config.go` | MemoryConfig: 删除 `Name`，新增 `ReadNamespaces []string` |
| `tagent.go` | 删除全局注册表变量和 `resolveMemoryStore` dedup 逻辑；`buildAgent` 中计算 ReadNamespaces→PartitionIDs 并注入 factory config |
| `tagent_test.go` | 删除 `TestFileBackend_RegistryDedup`、`TestInMemoryStore_RegistryNamedSharing`、`TestCrossAgentFileBackendRead` |
| `tool/recall/recall_subtools.go` | 回退 `recallQueryArgs.PartitionIDs`、`recallRecentArgs.PartitionIDs`；新增 `ReadPartitionIDs` 内部字段，由 factory 注入 |
| `tool/recall/recall_agent.go` | `buildRecallSubTools` 接受 ReadPartitionIDs，自动注入子工具 |
| `agent/tool_agent.go` | `ToolAgentFactoryConfig` 新增 `ReadPartitionIDs []int` |
| `examples/wechat-bot/tagent.yaml` | recall 配置 `read_namespaces: [tagent]` |
