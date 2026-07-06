## 1. RustVikingClient 封装层

- [x] 1.1 创建 `memory/rustviking_client.go`：定义 `RustVikingClient` 结构体、`CLIResponse` 类型、`KVOp` 类型
- [x] 1.2 实现 `KVPut` / `KVGet`：封装 `rustviking kv put -k <key> -v <value>` 和 `kv get -k <key>` 的 CLI 调用与 JSON 解析
- [x] 1.3 实现 `KVScan(prefix, limit)`：封装 `kv scan -p <prefix> -l <limit>` 的 CLI 调用
- [x] 1.4 实现 `KVRange(start, end, limit)`：封装 `kv range -s <start> -e <end> -l <limit>` 的 CLI 调用
- [x] 1.5 实现 `KVBatch(ops)`：封装 `kv batch` 调用（支持 stdin pipe 传入批量操作）
- [x] 1.6 实现 `KVDelete(key)`：封装 `kv delete -k <key>` 的 CLI 调用
- [x] 1.7 实现 `rustviking_client_test.go`：用 mock CLI 脚本验证各操作正确性和错误处理

## 2. KV Key Schema 定义

- [x] 2.1 创建 `memory/key_schema.go`：定义 key 格式常量和构造函数
- [x] 2.2 实现 `EventKey(eventKey)` — 生成 `{pid}:evt:{window_ts}:{seq}` 格式 key
- [x] 2.3 实现 `IndexKey(eventKey)` — 生成 `{pid}:idx:{event_key}` 格式 key
- [x] 2.4 实现 `MetaKey(windowTs)` — 生成 `{pid}:meta:{window_ts}` 格式 key
- [x] 2.5 实现 `TombstoneKey(eventKey)` — 生成 `{pid}:tomb:{event_key}` 格式 key
- [x] 2.6 实现 key 解析函数：从 key 反向提取 pid、window_ts、seq 等字段
- [x] 2.7 实现 `key_schema_test.go`：验证格式正确性、前缀匹配、边界条件

## 3. RelationStore 实现

- [x] 3.1 创建 `memory/relation_store.go`：定义 `RelationStore` 接口（SetParent / GetParent / GetChildren / GetParents / RemoveRelations / Snapshot / LoadSnapshot / ReplayJournal）
- [x] 3.2 实现 `InMemRelationStore` 结构体：childToParent map + parentToChildren map + sync.RWMutex
- [x] 3.3 实现 `SetParent`：双图更新 + WAL journal 追加（`+1:childKey:parentKey` 格式）
- [x] 3.4 实现 `GetParent` / `GetParents`：内存 O(1) 查询，GetParents 单次锁获取批量返回
- [x] 3.5 实现 `GetChildren`：反向索引 O(1) 查询
- [x] 3.6 实现 `RemoveRelations`：双图清理 + journal 追加（`-1:key` 格式）
- [x] 3.7 实现 journal 管理：append-only + fsync + 截断不完整行
- [x] 3.8 实现 `Snapshot` / `LoadSnapshot`：序列化 childToParent map + 截断 journal
- [x] 3.9 实现 `ReplayJournal`：逐行解析 journal 并 apply 到 map
- [x] 3.10 实现 `relation_store_test.go`：覆盖所有接口方法、并发安全、崩溃恢复

## 4. FileSegmentStore 核心实现

- [x] 4.1 创建 `memory/segment_store.go`：定义 `FileSegmentStore` 结构体（dataDir + partitions map + relationStore + cache + KVStore）
- [x] 4.2 实现 `partitionStore`：管理单分区的 active segment 写入和段读取
- [x] 4.3 实现 `StoreEvent(fullEvent)`：序列化 FullEvent → JSON → RustViking KVPut + 索引 KVPut + 元数据写入
- [x] 4.4 实现段文件 seal 流程：更新 segment meta 标记 L1（通过 RustViking KV）
- [x] 4.5 实现 `GetEvent(eventKey)`：EventCache LRU → index KVGet → EventKey KVGet 精确读
- [x] 4.6 实现 `QueryEvents(QueryOptions)`：meta prefix 扫描发现段 → 时间裁剪 → 段内扫描 → 过滤 → 排序
- [x] 4.7 实现 EventCache LRU：基于 `container/list` + `map` 的 FullEvent 缓存（默认 1000 条）
- [x] 4.8 实现启动恢复：RustViking RocksDB 通过 WAL 自动处理崩溃恢复，FileSegmentStore 延迟初始化
- [x] 4.9 实现 `segment_store_test.go`：覆盖 StoreEvent → GetEvent 往返、QueryEvents 时间范围、并发分区写入

## 5. MemoryStore 接口适配

- [x] 5.1 修改 `memory/types.go`：FullEvent 移除 ParentKey 字段；EventReference 的 ParentKey 由调用方通过 RelationStore 填充
- [x] 5.2 修改 `MemoryStore` 接口：新增 `GetParent(key int64) (int64, error)` 和 `GetChildren(key int64) ([]int64, error)` 方法
- [x] 5.3 修改 `memory/in_memory_store.go`：在 InMemoryStore 中嵌入 RelationStore，实现新接口方法，适配 FullEvent 不再含 ParentKey
- [x] 5.4 修改 `plugin/memory_plugin.go`：StoreEvent 流程适配（ParentKey 改为调用 RelationStore.SetParent）
- [x] 5.5 修改 `tool/recall/recall_subtools.go`：memory_trace 改为调用 GetParent 而非读事件的 ParentKey 字段
- [x] 5.6 修改 `tool/knowledge/knowledge_subtools.go`：适配 FullEvent 结构变更（无需改动 - 仅使用 EventReference.EventType/EventSummary）
- [x] 5.7 全量编译验证：`go build ./...` 通过

## 6. Compaction 引擎

- [x] 6.1 创建 `memory/compaction.go`：定义 Compactor 结构体和 compaction 5 步流程（Merge → Filter → Repair → Compress → Cleanup）
- [x] 6.2 实现 L0→L1 seal：定时检查 active 段是否跨小时边界，触发 seal
- [x] 6.3 实现 L1→L2 compaction：扫描 L1 段 → 合并排序 → 墓碑过滤 → 悬垂修复 → 写 L2 + idx → 删 L1
- [x] 6.4 实现 L2→L3 deep compaction：L1→L2 流程 + 低价值事件摘要化（丢弃 Content/ToolCalls）
- [x] 6.5 实现悬垂引用修复：遍历存活事件 → 检查 parentKey 是否为墓碑 → 沿链找到最近活祖先
- [x] 6.6 实现 compaction 调度器：后台 goroutine，每 5 分钟检查触发条件，单任务串行执行
- [x] 6.7 实现 compaction 原子性：先写新段再到删旧段（crash-safe），使用 KVBatch 保证原子性
- [x] 6.8 实现 `compaction_test.go`：端到端 compact 流程、墓碑过滤、悬垂修复、窗口计算
## 7. 墓碑与生命周期管理

- [x] 7.1 创建 `memory/tombstone.go`：实现 TombstoneSet（基于内存 map + RustViking KV 持久化）
- [x] 7.2 实现 `MarkTombstone(key)`：标记墓碑 + 触发级联悬垂修复（通过 RelationStore）
- [x] 7.3 实现 `IsTombstone(key)`：map 成员检查
- [x] 7.4 实现墓碑持久化：map serialize → RustViking KVPut，重启时恢复
- [x] 7.5 创建 `memory/lifecycle.go`：实现 TTL 扫描器（定时检查事件时间戳，标记过期事件）
- [x] 7.6 实现容量逐出：检测分区事件数超阈值 → 从最老段批量标记墓碑
- [x] 7.7 实现类型权重 TTL：按 EventType 配置不同过期时间
- [x] 7.8 实现 `lifecycle_test.go`：TTL 过期、容量逐出、类型权重、墓碑可见性

## 8. 构建验证与回归测试

- [x] 8.1 `go build ./...` 全量编译通过（无编译错误）
- [x] 8.2 `go vet ./...` 静态分析通过
- [x] 8.3 现有 `memory/` 包测试适配并通过（InMemoryStore + FileSegmentStore 均实现 MemoryStore 接口）
- [x] 8.4 现有 `tool/recall/` 包测试适配并通过（memory_trace 改为调用 GetParent）
- [x] 8.5 现有 `plugin/memory_plugin.go` 测试适配并通过
- [x] 8.6 性能基准测试：million-event QueryEvents < 10ms, GetEvent < 1ms（benchmark 用例）

## 9. 渐进迁移与清理

- [ ] 9.1 实现双写模式：MemoryPlugin 同时写入 FileBackend（旧）和 FileSegmentStore（新）
- [ ] 9.2 实现读取切换：RecallAgent / KnowledgeAgent 从 FileSegmentStore 读取，FileBackend 只读保留
- [ ] 9.3 实现历史迁移工具：`cmd/migrate-events/` 将旧 FileBackend 事件批量转为 RustViking KV
- [ ] 9.4 迁移完成后删除 `memory/file_backend.go` 和 `memory/file_backend_test.go`
- [ ] 9.5 移除双写逻辑，清理遗留代码
- [ ] 9.6 全量测试通过（确认无回归）
