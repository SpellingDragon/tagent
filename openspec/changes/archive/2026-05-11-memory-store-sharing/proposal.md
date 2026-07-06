## Why

MemoryStore 实例隔离策略在两个层面存在问题：(1) 同配置不同实例：两个 Agent 配置相同的 file 路径会创建互不协调的独立 FileBackend 实例，而 InMemoryStore 下同"类型"永远是隔离的——语义不一致；(2) recall agent 的子工具查询的是自身空 store，与"检索对话历史"的工具声明严重失配。这些导致多轮对话中 recall agent 的检索能力完全依赖 LLM 传入 event_keys，无法自主扩展检索范围，且 FileBackend 同路径场景存在并发安全隐患。

## What Changes

- 引入 MemoryStore 实例注册表，同路径的 FileBackend 返回同一实例（持同一锁），InMemoryStore 通过可选 `name` 字段支持命名共享
- recall agent 子工具（`memory_query`、`memory_recent`）支持可选的 PartitionID 过滤参数，允许精确限定检索范围
- **BREAKING** MemoryConfig 新增可选字段 `name`（仅对 InMemoryStore 生效），默认行为不变

## Capabilities

### New Capabilities
- `memory-store-registry`: MemoryStore 实例注册表，确保同路径/同名的 store 返回同一实例，消除 FileBackend 的并发安全隐患和两种后端语义不一致
- `recall-partition-filter`: recall 子工具支持 PartitionID 过滤，在共享 store 场景下精确限定检索范围，避免跨 agent 数据泄漏

### Modified Capabilities
<!-- 无现有 spec 需要修改 -->

## Impact

| 文件 | 影响 |
|------|------|
| `memory/types.go` | MemoryConfig 新增 `Name` 字段（可选） |
| `memory/registry.go` | **新文件** — 实例注册表逻辑 |
| `tagent.go` | `resolveMemoryStore()` 改为通过注册表获取/创建实例 |
| `tool/recall/recall_subtools.go` | 三个子工具的 QueryOptions 增加 PartitionIDs 过滤 |
| `tool/recall/recall_agent.go` | `buildRecallSubTools` 签名不变 |
