## Context

当前 tagent 的 MemoryStore 实例管理在 `tagent.go:resolveMemoryStore()` 中。每次调用都创建新实例。两个 Agent 配置相同的 file 路径时，会生成两个独立的 `FileBackend`，各自持有 `sync.RWMutex`，互不协调。InMemoryStore 永远隔离——同"类型"无法共享。

Recall agent 的子工具（`memory_query`、`memory_recent`、`memory_get`）操作的是 recall 自身的 store，与父 agent 的 store 完全隔离。配置相同 file 路径可以"偶然"共享（无锁保护），但 InMemoryStore 无法共享。

## Goals / Non-Goals

**Goals:**
- 同一 FileBackend 路径返回唯一实例（共享 mutex），消除并发安全隐患
- InMemoryStore 通过 `name` 字段支持可选共享，保持默认隔离行为
- recall 子工具支持 PartitionID 过滤，共享 store 时精确限定检索范围
- 所有变更向后兼容：不传 `name` 的 InMemoryStore 保持隔离；不传 `partition_ids` 的子工具保持全分区查询

**Non-Goals:**
- 不修改 MemoryStore 接口
- 不修改 Snowflake EventKey 的生成逻辑
- 不修改 AgentToolWrapper 的 event_key 解析机制
- 不在子工具中自动推导 PartitionID（由 LLM 根据上下文选择）

## Decisions

### 决策 1：注册表放在 `tagent` 包 vs `memory` 包

**选择**：注册表放在 `tagent` 包（修改 `resolveMemoryStore()`），不新增 `memory` 包文件。

**备选方案**：在 `memory` 包新增 `registry.go`。

**理由**：`resolveMemoryStore` 已在 `tagent.go` 中，是唯一创建 MemoryStore 的入口。注册表逻辑内聚在此处，不增加 `memory` 包的职责。`memory` 包保持纯数据模型和存储实现。

### 决策 2：FileBackend 注册表的 key

**选择**：`path` 的绝对路径（`filepath.Abs` 解析后）作为 map key。

**备选方案**：用户输入的原始字符串作为 key。

**理由**：`"./data"` 和 `"/app/data"` 指向不同目录，需解析为绝对路径后去重。使用 `filepath.Abs` 标准语义。

### 决策 3：InMemoryStore 注册表的 key

**选择**：`MemoryConfig.Name` 字段，空字符串不注册。

**备选方案**：使用类型标签（如 `"default"`）。

**理由**：空字符串=不共享，保持默认行为。命名共享需要用户显式设置 `name`，避免意外共享。

### 决策 4：recall 子工具的 PartitionID 参数

**选择**：在 `memory_query` 和 `memory_recent` 的参数结构体中新增 `PartitionIDs []int` 可选字段。`memory_get` 不加——EventKey 自包含 PartitionID。

**备选方案**：三个子工具全加。自动从 `memory_get` 的返回值推导 partition 并隐式传给下次调用。

**理由**：`memory_get` 通过 EventKey 内部的 PartitionID 精确定位，加 partition 字段无意义且增加 LLM 负担。保持参数语义最小化。

### 决策 5：`memory_query` 不在 function tool 声明中暴露 `partition_ids`

**选择**：在 Go struct 中新增 `PartitionIDs` 字段，在 function tool 的 `Description` 中描述此参数。不做额外的 InputSchema 强制。

**备选方案**：通过 `function.WithInputSchema` 显式声明 `partition_ids`。

**理由**：`function.NewFunctionTool` 自动从 Go struct 生成 InputSchema，新增字段自动暴露。描述中引导 LLM 使用即可。

## Risks / Trade-offs

| 风险 | 缓解 |
|------|------|
| 注册表全局 `sync.Mutex` 在大量 agent 并发创建时成为瓶颈 | agent 创建只在 `tagent.New()` 时发生（一次性），不存在热路径 |
| 用户错误配置 name 导致意外共享/隔离 | 文档说明 `name` 语义；空值保持隔离的默认行为不变 |
| recall sub-tool 新增参数可能被 LLM 忽略 | description 中明确说明 partition_ids 用途 |
| 注册表无清理机制，长期运行可能持有不再使用的 store | agent 生命周期与进程一致，store 引用是合理的长期持有 |

## Migration Plan

1. 修改 `resolveMemoryStore` 逻辑，引入注册表
2. `MemoryConfig` 新增 `Name` 字段（`omitempty`）
3. 修改 recall sub-tools 的 Go struct，新增 `PartitionIDs` 字段
4. 运行现有测试确认无回归
5. 无需配置文件迁移——所有新增字段可省略
