# production-wiring Specification

## Purpose

本规范定义 production-wiring 能力。`resolveMemoryStore("file")` in `tagent.go` SHALL create and start the complete lifecycle management stack in addition to the base FileSegmentStore.

## Requirements

### Requirement: Production wiring creates and starts all lifecycle subsystems

`resolveMemoryStore("file")` in `tagent.go` SHALL create and start the complete lifecycle management stack in addition to the base FileSegmentStore. This includes: calling `Init()` to recover seqCounter, initializing per-partition TombstoneSets, and starting LifecycleManager and Compactor background goroutines.

#### Scenario: FileSegmentStore is initialized before any writes

- **WHEN** `resolveMemoryStore("file")` creates a FileSegmentStore
- **THEN** `store.Init()` SHALL be called before the store is returned to any caller
- **AND** Init SHALL recover seqCounter and currentWindow via `global:active_partitions` bitmap + `{pid}:cursor` (see capability `startup-active-partition-bitmap`)

#### Scenario: TombstoneSet is available for lifecycle operations

- **WHEN** a FileSegmentStore is created for production use
- **THEN** each partition's TombstoneSet SHALL be lazily initialized with correct `pid`, `kv`, and `rel` references
- **AND** `RecoverFromKV()` SHALL be called automatically on first TombstoneSet access

#### Scenario: LifecycleManager starts scanning after wiring

- **WHEN** `resolveMemoryStore("file")` completes
- **THEN** a LifecycleManager SHALL be created with the store's TombstoneSet and default LifecycleConfig
- **AND** `LifecycleManager.Start()` SHALL be called, beginning the TTL/capacity scanner loop

#### Scenario: Compactor starts scheduling after wiring

- **WHEN** `resolveMemoryStore("file")` completes
- **THEN** a Compactor SHALL be created with the store's KV client, RelationStore, and default CompactionConfig
- **AND** `Compactor.Start()` SHALL be called, beginning the compaction scheduler loop

#### Scenario: LifecycleManager and Compactor are stoppable via Close

- **WHEN** `FileSegmentStore.Close()` is called
- **THEN** LifecycleManager SHALL be stopped gracefully
- **AND** Compactor SHALL be stopped gracefully
- **AND** all TombstoneSet dirty state SHALL be flushed to KV

### Requirement: Subsystem references injected via setter methods

FileSegmentStore SHALL provide `SetLifecycleManager(*LifecycleManager)` and `SetCompactor(*Compactor)` setter methods to receive subsystem references injected by the production wiring code, avoiding circular import dependencies.

#### Scenario: SetLifecycleManager stores reference for Close

- **WHEN** `store.SetLifecycleManager(lm)` is called
- **THEN** the store SHALL retain the reference for later use in `Close()`

#### Scenario: SetCompactor stores reference for Close

- **WHEN** `store.SetCompactor(c)` is called
- **THEN** the store SHALL retain the reference for later use in `Close()`

### Requirement: Close follows strict shutdown order

`FileSegmentStore.Close()` SHALL shut down subsystems in the following order, waiting for each step before proceeding:

1. `Compactor.Stop()` — wait for any in-flight compaction task to drain; no new compaction SHALL start
2. `LifecycleManager.Stop()` — stop TTL/capacity scanner goroutines
3. Flush all dirty `TombstoneSet` state to KV via an atomic batch per partition
4. `RelationStore.Close()` — flush LRU dirty entries (if any) and sync RocksDB WAL (see capability `relation-store-lru-rocks`)

A failure at any step SHALL be logged but SHALL NOT abort the remaining steps; the method returns a joined error describing all failures.

#### Scenario: Close blocks on in-flight compaction

- **WHEN** `Close()` is invoked while a compaction task is running
- **THEN** `Close()` SHALL wait until the compaction task returns before proceeding to stop the LifecycleManager

#### Scenario: Close is idempotent

- **WHEN** `Close()` is invoked a second time
- **THEN** it SHALL return the same outcome as the first call (typically `nil`) without re-executing the shutdown sequence

### Requirement: 配置严格解析

tagent.yaml 解析 SHALL 采用 strict 模式（KnownFields）：出现未知字段 SHALL 使加载失败，错误信息列出全部未知字段名；字段拼写错误 MUST NOT 被静默忽略。仓库内全部随载 yaml（示例/测试/资源）SHALL 通过 strict 解析。

#### Scenario: 配置键拼写错误

- **WHEN** yaml 含拼错的键（如 `wroking_dir`）
- **THEN** 启动报配置错误并列出该未知字段，进程不进入「配置被忽略、行为悄然偏离预期」状态

### Requirement: agent 引用环构建期检测

buildAgent 递归装配 SHALL 携带 visited 集：agent 引用构成环（A↔B 或自引用）时 SHALL 返回明确配置错误（指明环上的 agent 名），MUST NOT 以栈溢出崩溃。

#### Scenario: 配置声明了引用环

- **WHEN** yaml 中 agent a 的 tools 引用 agent b、b 又引用 a
- **THEN** 构建返回形如 `agent "a" <-> "b" reference cycle detected` 的配置错误

### Requirement: 公开运行时入口在根包装配入口上完善，不重复建立运行时

公共运行时入口 SHALL 位于 module 根 `package tagent`（import 路径 `github.com/SpellingDragon/tagent`），是**唯一**公共门面；系统 MUST NOT 同时提供 `tagent/` 同名第二层门面或另建第二套组合根。公开 SDK/CLI 的构造/启动/关闭/诊断查询 SHALL 建立在**根包最小公共装配入口**（首次由执行域 `02.5` 使用）之上，宿主 MUST NOT 绕过该入口直连内部机制另起运行时。

集成测试 SHALL 经根包公开面驱动运行时，MUST NOT import `prototype` 包。

#### Scenario: 唯一公共门面

- **WHEN** 审查运行时公共入口
- **THEN** 公共 `package tagent` 位于 module 根；不存在并行的嵌套同名门面；宿主经该入口构造运行时

#### Scenario: 集成测试不依赖历史骨架

- **WHEN** 编译/运行 `tests/integration/host` 下的宿主集成测试
- **THEN** 其 import 图中不含 `prototype`；测试经根包公开面驱动真实运行时

#### Scenario: 不重复建立运行时

- **WHEN** 宿主在根包装配入口之外试图自建 org/state/task 组合
- **THEN** 评审/结构断言拒绝：宿主只消费公共入口，不复制运行时
