## 1. 恢复 event_key 注入链路（P0）

- [x] 1.1 在 agent/context_intervention.go 中新增 `injectEventKeyPrefixes` 函数：遍历 args.Request.Messages，按位置匹配 inv.Session.Events，为 user/assistant 消息添加 `[evt_<KEY>|<type>]` 前缀；跳过 system 和 tool 消息；处理 inv/Session 为 nil 的降级情况
- [x] 1.2 在 ContextIntervention.BeforeModel 中调用 `injectEventKeyPrefixes`（在压缩逻辑之前）
- [x] 1.3 更新 agent/smart_compress.go 的 collectCompressedKeys 注释，移除"Phase 1 event view transformation"引用，描述新的 BeforeModel 注入机制
- [x] 1.4 验证 collectCompressedKeys + parseEventKeyFromPrefix 在前缀注入后正常工作（已有逻辑无需修改）
- [x] 1.5 验证 buildCompressEvent 在 keys 非空时输出 key 列表（已有逻辑无需修改）
- [x] 1.6 验证 AgentToolWrapper.Call 的 event_keys 解析逻辑可被 LLM 触发（已有逻辑无需修改）
- [x] 1.7 验证 IngestExternalEvents 在 externalEvents 非空时被调用（已有逻辑无需修改）

## 2. 并发安全加固 — TagentAgent（P1）

- [x] 2.1 在 agent/tagent_agent.go 的 TagentAgent 结构体中新增 `sessionMu sync.Mutex` 字段
- [x] 2.2 修改 Run/RunSimple 方法，在写入 lastUserID/lastSessionID 时加锁
- [x] 2.3 修改 InjectMessage 方法，在读取 lastUserID/lastSessionID 时加锁
- [x] 2.4 提供 `getSessionContext() (string, string)` 和 `setSessionContext(string, string)` 内部方法封装锁访问

## 3. 并发安全加固 — TmuxMonitor（P1）

- [x] 3.1 在 tool/command/tmux_monitor.go 中将 `running bool` 改为 `running atomic.Bool`（import `sync/atomic`）
- [x] 3.2 修改 Start 方法：使用 `tm.running.Swap(true)` 原子操作
- [x] 3.3 修改 Stop 方法：同上原子操作
- [x] 3.4 新增 `IsRunning() bool` 方法：`return tm.running.Load()`
- [x] 3.5 修改 checkAllSessions：快照 session 列表后逐个调用 checkSession，锁内修改+锁外回调避免死锁
- [x] 3.6 检查 checkSession 死锁风险：回调在锁外执行，避免 GetSession RLock 死锁
- [x] 3.7 修改 tool/command/command_tool.go：`ct.tmuxMonitor.running` → `ct.tmuxMonitor.IsRunning()`
- [x] 3.8 新增 `GetSessionStatus()` 线程安全方法，修复测试中直接读取 session.Status 的数据竞争

## 4. 并发安全加固 — InMemRelationStore（P1）

- [x] 4.1 在 memory/relation_store.go 的 truncateJournal 方法内部添加 `rs.mu.Lock()` / `defer rs.mu.Unlock()`
- [x] 4.2 验证 SaveSnapshotToFile → Snapshot（读锁）→ 释放 → truncateJournal（写锁）的锁序列无死锁
- [x] 4.3 验证并发 SetParent 在 truncateJournal 等待写锁期间完成 journal 写入

## 5. 因果链隔离（P2）

- [x] 5.1 在 plugin/memory_plugin.go 中将 `lastEventKeys map[int]int64` 改为 `lastEventKeys map[string]int64`
- [x] 5.2 修改 NewMemoryPlugin 初始化：`make(map[string]int64)`
- [x] 5.3 修改 onEvent 方法：从 inv.Session.ID 提取 sessionID，构造复合 key；注意 parentKey 通过 RelationStore.SetParent 维护（非 FullEvent.ParentKey）
- [x] 5.4 使用复合 key 读写 lastEventKeys
- [x] 5.5 处理 inv.Session 为 nil 的降级情况（sessionID 为空，key 为 `"partitionID:"`）

## 6. 视图转换无状态化（P2）

- [x] 6.1 在 agent/context_intervention.go 的 BeforeModel 方法中，在压缩循环前保存 `originalKeepRecent := ci.compressor.KeepRecentTasks`
- [x] 6.2 添加 `defer func() { ci.compressor.KeepRecentTasks = originalKeepRecent }()`
- [x] 6.3 验证压缩循环中递减 KeepRecentTasks 的逻辑不受影响

## 7. 分层存储完善 — 压实调度（P2）

- [x] 7.1 在 memory/compaction.go 的 Compactor 结构体中新增 `tombstone *TombstoneSet` 字段
- [x] 7.2 修改 NewCompactor 签名，接受 `tombstone *TombstoneSet` 参数
- [x] 7.3 更新测试文件中 NewCompactor 的调用处，传入 nil（生产代码在 tagent.go 中传入实际实例）
- [x] 7.4 重写 filterTombstoned：检查 `c.tombstone != nil`，遍历事件调用 `c.tombstone.IsTombstone(evt.EventKey)`，过滤掉 tombstoned 事件
- [x] 7.5 新增 `checkL1ToL2Compaction()` 方法：遍历分区，统计 L1 段数量，达到 L1Threshold 时调用 CompactL1ToL2
- [x] 7.6 新增 `checkL2ToL3Compaction()` 方法：遍历分区，统计 L2 段数量，达到 L2Threshold 时调用 CompactL2ToL3
- [x] 7.7 在 checkAndCompact 中调用 checkL1ToL2Compaction 和 checkL2ToL3Compaction
- [x] 7.8 新增 `getSegmentMeta(pid int, windowTS int64) (*SegmentMeta, error)` 辅助方法，从 KV store 读取段元数据以区分层级

## 8. 分层存储完善 — TTL 精度修复（P2）

- [x] 8.1 在 memory/lifecycle.go 的 checkTTL 方法中，将 `eventAge := now - eventPK.WindowTS*1000` 替换为从事件 JSON 解析 Timestamp 字段
- [x] 8.2 使用 `json.Unmarshal` 解析 `{"timestamp": <int64>}` 结构体
- [x] 8.3 处理 JSON 解析失败的降级情况（跳过该事件，日志记录）

## 9. 错误处理与代码一致性（P2）

- [x] 9.1 修改 tool/command/command_tool.go：将标准库 `"log"` import 改为 `trpc-agent-go/log`，所有 `log.Printf` 改为 `log.Infof`/`log.Errorf`
- [x] 9.2 修改 memory/tombstone.go：检查 GetChildren/SetParent/RemoveRelations 错误并日志记录
- [x] 9.3 修改 memory/segment_store.go：检查 KVPut (meta) 错误并日志记录
- [x] 9.4 修改 memory/compaction.go：检查 SetParent 错误并日志记录
- [x] 9.5 修改 memory/lifecycle.go：检查 MarkTombstone 错误并日志记录
- [x] 9.6 修改 tool/recall/recall_subtools.go：检查 GetParent 错误，日志记录
- [x] 9.7 清理 plugin/memory_plugin.go 注释：移除 ParentKey 引用
- [x] 9.8 清理 plugin/memory_plugin.go 步骤编号：修复跳号问题
- [x] 9.9 清理 agent/smart_compress.go 注释：移除"Phase 1 event view transformation"引用

## 10. 文档同步

- [x] 10.1 修正 docs/wiki/memory/memory-architecture.md：更新 FullEvent（移除 ParentKey），补充 RelationStore 因果链机制，移除 FileBackend，更新文件清单
- [x] 10.2 修正 docs/wiki/agent/agent-architecture.md：更新 lastEventKeys 描述，修正 §12.5.7 event_key 注入机制（BeforeModel 前缀注入 + LLM 驱动选择），移除 FileBackend/NewFileBackend
- [x] 10.3 修正 docs/wiki/plugin/plugin-architecture.md：移除 ParentKey/FileBackend，更新 lastEventKeys 为复合 key map，补充 RelationStore.SetParent 机制
- [x] 10.4 修正 docs/wiki/tool/tool-architecture.md：移除 ParentKey/FileBackend，更新 lastEventKeys，修正 §13.3 event_key 注入机制（BeforeModel 前缀注入 + LLM 驱动 + AgentToolWrapper 解析）
- [x] 10.5 更新 README.md：补充完整项目结构

## 11. 验证

- [x] 11.1 编译验证：`go build ./...` 无错误
- [x] 11.2 测试验证：`go test ./... -short` 全部通过
- [x] 11.3 并发验证：`go test -race ./... -short` 无数据竞争
- [x] 11.4 端到端验证：验证 event_key 注入 → collectCompressedKeys → buildCompressEvent → LLM 可见 key 列表的完整链路
- [x] 11.5 降级验证：验证 inv/Session 为 nil 时 event_key 注入安全降级
