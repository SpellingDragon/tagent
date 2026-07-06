## Context

当前 `memory-store-sharing` 方案通过全局 `map[string]*FileBackend` 注册表实现实例 dedup。该方案存在抽象偏差：MemoryStore 的"共享"应该是存储层多命名空间支持的结果，而非实例级单例的结果。FileBackend 天然按 PartitionID 子目录组织数据——两个独立 `*FileBackend` 实例指向同一路径可以分别读写不同分区的数据。正确的"共享"是：recall 的 store 能读到 tagent 分区的数据，这通过 QueryOptions.PartitionIDs 即可实现，无需共享实例。

## Goals / Non-Goals

**Goals:**
- 移除全局注册表，恢复每个 agent 独立拥有 MemoryStore 的原始设计
- 通过 `ReadNamespaces` 配置项声明跨命名空间读权限
- recall 子工具自动注入 ReadPartitionIDs，LLM 无需感知分区机制
- FileBackend 多实例指向同路径的行为明确化（各自读写，无共享 mutex）

**Non-Goals:**
- 不修改 MemoryStore 接口
- 不实现跨进程/跨节点的命名空间共享
- 不修改 Snowflake EventKey 生成逻辑
- 不实现写权限控制（各 agent 只写自己的分区，由 PartitionID 隐式保障）

## Decisions

### 决策 1：注册表回退 — 回归独立实例

**选择**：删除 `fileBackendStores`、`namedMemStores` 全局变量和所有 dedup 逻辑。`resolveMemoryStore` 回归为简单的工厂函数。

**备选方案**：保留注册表但改名/重构为"命名空间后端管理器"。

**理由**：注册表的本质问题是耦合了"实例共享"和"数据共享"两个独立概念。FileBackend 数据共享通过文件系统天然实现，无需实例共享。去掉注册表使代码更简单，也消除了全局状态。

### 决策 2：ReadNamespaces 在 buildAgent 层解析

**选择**：`buildAgent()` 中读取 `acfg.Memory.ReadNamespaces`，逐个调用 `memory.PartitionIDFromName(name)` 得到 `[]int`，通过 `ToolAgentFactoryConfig.ReadPartitionIDs` 注入。recall factory 再将此列表传给 `buildRecallSubTools`。

**备选方案**：在 recall factory 内部自行解析 ReadNamespaces。

**理由**：`buildAgent` 是唯一统一构建入口，在此解析避免各 factory 重复实现。同时 `PartitionIDFromName` 依赖 agent name 作为输入，`buildAgent` 已有 name 上下文。

### 决策 3：recall 子工具内部注入 ReadPartitionIDs

**选择**：`NewRecallQueryTool(accessor, readPartitionIDs)` 和 `NewRecallRecentTool(accessor, readPartitionIDs)` 接受 `readPartitionIDs []int`。在 handler 内，将 `readPartitionIDs` 合并到 `opts.PartitionIDs`。子工具的 args struct 不暴露 `PartitionIDs` 给 LLM。

**备选方案**：让 LLM 传入 `partition_ids`（当前实现）。

**理由**：分区是存储实现细节，不应暴露给 LLM。LLM 的 tool call 应关注语义（"查历史记录"）而非存储拓扑（"查分区 201"）。自动注入消除了 LLM 猜错分区号的可能，也减少了 token 消耗。

### 决策 4：FileBackend 多实例并发安全

**选择**：文档化说明：多个 FileBackend 实例指向同一路径时，各实例的 mutex 互不共享。由于各 agent 写入各自的分区子目录，而读取操作（通过 `QueryEvents`）主要涉及列目录和读已存在的文件，文件系统层面的原子性通常是足够的。

**备选方案**：引入文件锁或共享 mutex。

**理由**：tagent 是单进程模型，所有 agent 在同一个 Go runtime 中运行。列目录（`os.ReadDir`）和文件写（`os.WriteFile`）在 POSIX 系统上原子性良好。引入额外同步机制会增加复杂度，而收益有限。如果未来出现并发问题，可以在 FileBackend 中引入 `flock` 级别的文件锁。

## Risks / Trade-offs

| 风险 | 缓解 |
|------|------|
| FileBackend 多实例读写同一分区文件可能 race | tagent 只写自己的分区，recall 只读别人的分区——读/写操作针对不同文件，核心场景下无冲突 |
| InMemoryStore 无法跨实例共享 | 当前 recall 默认用 file 型存储（与 tagent 同路径）；in-memory 型的跨命名空间场景暂不支持，未来按需实现 |
| ReadNamespaces 名字拼写错误时静默失败 | 后续可增加配置校验，目前错误的名字会导致 PartitionIDFromName 返回一个不会匹配到数据的 hash 值 |
