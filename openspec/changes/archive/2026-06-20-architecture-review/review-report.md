# tagent 架构审查报告

**审查日期**: 2026-06-20
**审查范围**: 全部 53 个 Go 源文件 + 6 个 wiki 文档 + README
**审查依据**: 6 项设计目标 + 3 项横向维度

---

## 一、严重级别分布

| 级别 | 数量 | 含义 |
|------|------|------|
| **P0** | 1 | 功能性缺陷 — 代码无法实现设计目标 |
| **P1** | 5 | 并发安全缺陷 — 可能导致数据竞争或数据损坏 |
| **P2** | 20 | 代码质量问题 — 不影响核心功能但影响可维护性 |
| **P3** | 5 | 轻微问题 — 文档过时、配置缺失等 |

**最高优先级修复项**: P0 collectCompressedKeys + 5 个 P1 并发安全问题

---

## 二、按设计目标维度的审查发现

### 目标 1: 事件驱动一致性

#### [P0] collectCompressedKeys 总是返回空 slice

- **模块**: agent/smart_compress.go
- **位置**: line 168-185
- **问题描述**: `collectCompressedKeys()` 调用 `parseEventKeyFromPrefix()` 解析消息内容中的 `[evt_<KEY>|type]` 前缀来提取 event_key。但 Phase 1 事件视图转换已移除，消息内容中不再包含此前缀，导致该函数总是返回空 slice。
- **影响**: `buildCompressEvent()` 在 keys 为空时走 else 分支（line 127-128），压缩事件不输出 key 列表。LLM 无法通过 recall agent 回溯被压缩的事件，破坏了事件驱动的一致性。
- **根因分析**: Phase 1 移除时，`collectCompressedKeys` 和 `parseEventKeyFromPrefix` 未同步更新。`Compress()` 接受 `inv *agent.Invocation` 参数但 `collectCompressedKeys` 不使用它。
- **修复建议**: 重写 `collectCompressedKeys`，从 `inv` 或消息的 `StateDelta` 中提取 event_key，而非依赖已移除的前缀格式。

---

### 目标 2: 内存中心一致性

#### [P1] SaveSnapshotToFile/truncateJournal 竞争条件

- **模块**: memory/relation_store.go
- **位置**: line 373-414
- **问题描述**: `SaveSnapshotToFile()` 调用 `truncateJournal()` 时未持有写锁。并发的 `SetParent()` 可能在 `truncateJournal()` 关闭 journal 文件后、重新打开前，调用 `appendJournal()` 写入已关闭的文件句柄。
- **影响**: 并发压实和关系设置可能导致 journal 文件损坏，崩溃恢复时数据丢失。
- **根因分析**: `truncateJournal()` 设计为内部函数但未加锁保护，假设调用方已持锁，但实际 `SaveSnapshotToFile()` 在获取快照后释放了锁。
- **修复建议**: 在 `truncateJournal()` 内部获取写锁，或让 `SaveSnapshotToFile()` 在调用 `truncateJournal()` 前持有写锁。

#### [P2] filterTombstoned 是 no-op stub

- **模块**: memory/compaction.go
- **位置**: line 273-276
- **问题描述**: `filterTombstoned()` 直接返回输入，注释标注 "TODO: Integrate with TombstoneSet in Phase 7"。`LifecycleManager.GetTombstoneFilterFunc()` 存在但从未被 Compactor 调用。
- **影响**: 压实后的数据可能包含已被标记为 tombstone 的事件，浪费存储空间。
- **修复建议**: 接入 `TombstoneSet.IsTombstone()` 检查，或调用 `LifecycleManager.GetTombstoneFilterFunc()`。

#### [P2] KVPut 错误被忽略（段元数据）

- **模块**: memory/segment_store.go
- **位置**: line 231
- **问题描述**: `s.kv.KVPut(metaKVKey, string(metaJSON))` 返回值被忽略。
- **影响**: 段元数据写入失败时无感知，可能导致后续查询窗口计算错误。
- **修复建议**: 检查错误并日志记录。

#### [P2] 自定义 toLower/contains 仅处理 ASCII

- **模块**: memory/in_memory_store.go
- **位置**: line 349-372
- **问题描述**: 自定义 `toLower()` 和 `contains()` 仅处理 ASCII 字符，非 ASCII 字符（如中文）直接透传。应使用 `strings.ToLower` / `strings.Contains`。
- **影响**: 包含非 ASCII 字符的关键词搜索可能大小写敏感（对拉丁扩展字符），或匹配结果不正确。
- **修复建议**: 替换为标准库 `strings.ToLower` / `strings.Contains`。

#### [P2] resolvePartitions 行为不一致

- **模块**: memory/in_memory_store.go:174-188 vs memory/segment_store.go:498-508
- **位置**: InMemoryStore vs FileSegmentStore
- **问题描述**: 当未指定 PartitionIDs 时，InMemoryStore 返回全部分区，FileSegmentStore 返回 nil（无结果）。
- **影响**: 同一查询在 memory backend 和 file backend 下行为不同，可能导致测试通过但生产失败。
- **修复建议**: 统一行为——建议两者都返回全部分区。

#### [P2] 死代码: tombstonePersistPrefix

- **模块**: memory/tombstone.go
- **位置**: line 17
- **问题描述**: `tombstonePersistPrefix = "tomb"` 常量声明后从未使用。
- **修复建议**: 删除死代码。

#### [P2] 多处被忽略的错误

- **模块**: memory/tombstone.go:56,64,67,73 — 多处 `_ = err`
- **模块**: memory/compaction.go:311 — SetParent 错误被忽略
- **模块**: memory/lifecycle.go:168 — MarkTombstone 错误被忽略
- **修复建议**: 检查错误并日志记录，对于关键操作应返回错误。

#### [P2] 脆弱的 JSON 提取

- **模块**: memory/lifecycle.go
- **位置**: line 256-270
- **问题描述**: `extractEventTypeFromJSON` 用字符串搜索替代 JSON 解析。
- **影响**: 如果 JSON 格式变化或字段名包含子串匹配，可能提取错误。
- **修复建议**: 使用 `encoding/json` 正式解析。

#### [P2] MockRustVikingClient.KVScan 返回未排序结果

- **模块**: memory/rustviking_client.go
- **位置**: line 332-345
- **问题描述**: Mock 遍历 map 后直接返回，map 迭代序随机。而真实 RustVikingClient 返回有序结果。
- **影响**: 依赖顺序的测试可能不稳定。
- **修复建议**: Mock 返回前按 key 排序。

#### [P3] 仅实现 L0→L1 压实

- **模块**: memory/compaction.go
- **位置**: line 148-156
- **问题描述**: `checkAndCompact()` 只实现 L0→L1（hourly seal），L1→L2 和 L2→L3 未调度。
- **影响**: 温数据和冷数据不会自动迁移，但功能上不影响正确性。
- **修复建议**: 补充 L1→L2 和 L2→L3 调度逻辑，或明确标注为未来实现。

---

### 目标 3: 视图转换完整性

#### [P2] KeepRecentTasks 单调递减，从未恢复

- **模块**: agent/context_intervention.go
- **位置**: line 81-85
- **问题描述**: 压缩循环中 `ci.compressor.KeepRecentTasks--` 递减后从不恢复原值。后续请求会使用已递减的值。
- **影响**: 多轮对话中 KeepRecentTasks 可能降到 1，导致过度压缩。
- **修复建议**: 在 BeforeModel 开始时保存原始值，结束后恢复；或使用局部变量。

#### [P2] 关于已移除 Phase 1 的陈旧注释

- **模块**: agent/smart_compress.go
- **位置**: line 164-167
- **问题描述**: 注释描述 "Parses the '[evt_<KEY>|<type>]' prefix added by Phase 1 event view transformation"，但 Phase 1 已移除。
- **修复建议**: 更新或删除陈旧注释。

---

### 目标 4: 因果关系完整性

因果关系通过独立的 RelationStore 管理，设计正确。发现的问题已在目标 2 中记录（truncateJournal 竞争、SetParent 错误忽略等）。

**正向确认**:
- FullEvent 不含 ParentKey ✅
- RelationStore 双图维护 ✅
- TombstoneSet 级联修复有环检测 ✅
- recall_subtools.go 通过 RelationStoreProvider 获取 parentKey ✅

---

### 目标 5: 上下文隔离完整性

**正向确认**:
- ContextIntervention.BeforeModel 只修改 `args.Request.Messages` ✅
- event_key 上下文隔离：LLM 只输出 int64 key，AgentToolWrapper 服务端解析 ✅
- AgentToolWrapper 的 event_keys 解析支持 array + single backward compat ✅
- IngestExternalEvents 只注入 EventSummary，不注入完整 Content ✅

无发现问题。

---

### 目标 6: 分层存储与生命周期一致性

发现的问题已在目标 2 中记录（filterTombstoned stub、L0→L1 only、tombstone 错误忽略等）。

**正向确认**:
- KV key schema（evt/idx/meta/tomb 前缀）完整 ✅
- LRU cache + PartitionState 各自有 mutex ✅
- LifecycleManager TTL + 容量驱逐 + 后台 scannerLoop ✅
- TombstoneSet MarkTombstone 级联修复逻辑正确 ✅

---

## 三、按横向维度的系统性发现

### 横向维度 1: 并发安全

#### [P1] TagentAgent.lastUserID/lastSessionID 数据竞争

- **模块**: agent/tagent_agent.go
- **位置**: line 181-182, 206-207 (写), line 269, 280 (读)
- **问题描述**: `lastUserID`/`lastSessionID` 在 `Run()`/`RunSimple()` 中写入，在 `InjectMessage()` 中从 tmux monitor goroutine 读取，无 mutex/atomic 保护。
- **影响**: 并发读写可能导致 InjectMessage 读取到不一致的值，可能注入到错误的 session。
- **修复建议**: 使用 `sync.Mutex` 或 `atomic.Value` 保护。

#### [P1] TmuxMonitor.running 裸 bool 无同步

- **模块**: tool/command/tmux_monitor.go
- **位置**: line 24 (字段声明), line 119-128 (Start), line 131-139 (Stop)
- **问题描述**: `running` 字段是裸 `bool`，Start/Stop 读写无 mutex 保护。command_tool.go:243 也读取此字段。
- **影响**: Start/Stop 并发调用可能导致 monitor goroutine 泄漏或重复启动。
- **修复建议**: 使用 `sync/atomic.Bool` 或加 mutex。

#### [P1] TmuxMonitor.checkSession 锁外修改 session 字段

- **模块**: tool/command/tmux_monitor.go
- **位置**: line 211-238
- **问题描述**: `checkAllSessions()` 复制 session slice 后释放锁，但 `checkSession()` 仍修改原始 session 对象的 Status、LastOutput、LastOutputMD5、StableSince 字段。
- **影响**: 状态变更可能丢失或覆盖，导致状态检测不准确。
- **修复建议**: 在锁内修改 session 字段，或使用 session 对象自身的 mutex。

#### [P1] CommandTool 读取 tmuxMonitor.running 无锁

- **模块**: tool/command/command_tool.go
- **位置**: line 243
- **问题描述**: `ct.tmuxMonitor.running` 在无锁的情况下被读取。
- **影响**: 与 TmuxMonitor.running 问题叠加，加剧数据竞争风险。
- **修复建议**: 提供 `IsRunning()` 方法带锁访问。

---

### 横向维度 2: 代码一致性

#### [P2] 注释引用已移除的 ParentKey / Phase 1 机制

| 文件 | 位置 | 陈旧内容 |
|------|------|---------|
| plugin/memory_plugin.go | line 22 | "Build FullEvent with ParentKey"（line 90 已更正） |
| agent/smart_compress.go | line 164 | "Phase 1 event view transformation" |
| docs/wiki/memory/memory-architecture.md | 多处 | FullEvent.ParentKey 字段、FileBackend |
| docs/wiki/agent/agent-architecture.md | line 575-583 | Phase 1 事件视图转换代码 |
| docs/wiki/agent/agent-architecture.md | line 757-765 | ParentKey: parentKey in FullEvent |

**修复建议**: 全局搜索 "ParentKey" 和 "Phase 1"，更新所有陈旧引用。

#### [P2] log 包不一致

- **command_tool.go:7**: 使用标准库 `"log"`
- **所有其他文件**: 使用 `trpc-agent-go/log`
- **修复建议**: 统一为 `trpc-agent-go/log`。

#### [P2] 被忽略的 error 返回值（跨模块汇总）

| 文件 | 位置 | 被忽略的调用 |
|------|------|------------|
| memory/tombstone.go | line 56,64,67,73 | KVPut/KVGet/KVDelete |
| memory/segment_store.go | line 231 | KVPut (meta) |
| memory/compaction.go | line 311 | SetParent |
| memory/lifecycle.go | line 168 | MarkTombstone |
| tool/recall/recall_subtools.go | line 106,332 | GetParent |

**修复建议**: 检查所有 error 返回值，至少日志记录；关键操作应传播错误。

---

### 横向维度 3: 接口契约

#### [P2] KVStore 接口行为不一致

- **MockRustVikingClient.KVScan**: 返回未排序结果（map 迭代序随机）
- **RustVikingClient.KVScan**: 返回有序结果
- **影响**: 依赖顺序的测试可能通过但生产失败，或反之。
- **修复建议**: Mock 返回前按 key 排序。

#### [正向确认] MemoryStore 接口完整实现

InMemoryStore 和 FileSegmentStore 均完整实现 MemoryStore 接口的所有方法 ✅

#### [正向确认] RelationStoreProvider 降级路径

memory_plugin.go、recall_subtools.go 中的类型断言均有降级路径 ✅

#### [正向确认] sessionInspector 接口完整实现

TmuxExecutor 完整实现所有 sessionInspector 方法 ✅

---

## 四、文档审查发现

### [P1] memory-architecture.md 严重过时

- 描述 FullEvent 含 ParentKey 字段（已移除，改用 RelationStore）
- 描述 FileBackend（不存在，已替换为 FileSegmentStore + KV store）
- 文件清单错误（3 个文件 → 实际 9 个文件）
- 代码示例引用 `evt.ParentKey`（实际通过 RelationStore.GetParent()）
- 无 KV store、LRU cache、compaction、lifecycle、tombstone 等机制的文档

### [P1] agent-architecture.md 包含已移除代码

- line 575-583: 展示 Phase 1 事件视图转换代码（已从 context_intervention.go 移除）
- line 757-765: 展示 `ParentKey: parentKey` in FullEvent 构造（已移除）
- line 952-977: 将已实现的 `lastEventKeys map` 描述为"改进"和"未来工作"

### [P2] README.md 项目结构严重不完整

- 仅列出 go.mod、.gitignore、README.md
- 缺失 agent/、memory/、plugin/、tool/、event/、prompt/、docs/ 等核心目录

### [正向确认] event-architecture.md 准确

内容与实现一致，无发现问题 ✅

---

## 五、修复优先级排序

### 第一优先级（P0 — 功能性缺陷）

1. **修复 collectCompressedKeys** — 重写函数从 Invocation/StateDelta 提取 event_key，使压缩事件可被 recall agent 回溯

### 第二优先级（P1 — 并发安全）

2. **TagentAgent.lastUserID/lastSessionID** — 加 mutex 或 atomic 保护
3. **TmuxMonitor.running** — 改用 atomic.Bool
4. **TmuxMonitor.checkSession** — 在锁内修改 session 字段
5. **InMemRelationStore.truncateJournal** — 持有写锁
6. **CommandTool.tmuxMonitor.running 读取** — 提供 IsRunning() 方法

### 第三优先级（P2 — 代码质量，批量修复）

7. **filterTombstoned stub** — 接入 TombstoneSet
8. **log 包统一** — command_tool.go 改用 trpc-agent-go/log
9. **错误处理** — 检查所有被忽略的 error 返回值
10. **注释更新** — 清理所有 ParentKey/Phase 1 陈旧引用
11. **文档更新** — 重写 memory-architecture.md，修正 agent-architecture.md
12. **其他 P2 项** — 见各模块详细发现

---

## 六、审查结论

tagent 的核心架构设计（事件驱动、内存中心、视图转换、因果关系、上下文隔离、分层存储）在代码层面基本得到落实。主要问题集中在：

1. **Phase 1 移除不彻底** — collectCompressedKeys 依赖已移除的前缀格式（P0），多处注释和文档仍引用旧机制
2. **并发安全防护不足** — 5 个 P1 数据竞争点，集中在 TmuxMonitor 和 TagentAgent 的异步事件注入路径
3. **错误处理松散** — 多处静默吞错，可能在生产环境导致难以排查的问题

建议按优先级分批修复：第一批修复 P0+P1（6 项），第二批修复 P2（20 项），第三批更新文档。

---

## 七、第二轮审查：设计预期 vs 实际实现对齐分析

**审查方法**：对 6 项设计目标逐个进行「预期做什么 / 需要做什么 / 目前怎么做」三维分析，反复与设计预期印证对齐，识别第一轮未覆盖的系统性偏差。

**新增发现级别**：P0×2, P1×2, P2×4（与第一轮不重复计数）

---

### 目标 1: 事件驱动一致性 — event_key 注入链路端到端断裂

#### 预期做什么

设计预期：事件是唯一驱动单元；压缩后的 context_compress 事件应包含被压缩事件的 key 列表，使 LLM 可通过 recall agent 回溯。

#### 需要做什么

完整的事件回溯链路：
1. event_key 在 LLM 可见的上下文中出现（消息内容或压缩事件中）
2. SmartCompressor 从可用来源提取被压缩事件的 event_key
3. 压缩事件 buildCompressEvent 输出 key 列表
4. LLM 可选择 key 传给 recall agent 回溯

#### 目前怎么做

追踪完整的 event_key 流向：
1. MemoryPlugin.OnEvent 将 event_key 写入 `evt.StateDelta["event_key"]`（memory_plugin.go:128）
2. 框架将 StateDelta merge 到 Session.State
3. **但消息内容中不含 event_key** —— Phase 1 前缀注入已移除，消息内容是原始 LLM 输出
4. SmartCompressor.collectCompressedKeys 调用 parseEventKeyFromPrefix 解析 `[evt_<KEY>|<type>]` 前缀 → **消息中无此前缀 → 总是返回空**
5. buildCompressEvent 在 keys 为空时只输出 `"[context_compress] 压缩了 N 个对话片段"`，无 key 列表
6. LLM 上下文中**没有任何 event_key**

#### [P0] event_key 注入链路端到端断裂

- **与第一轮的区别**：第一轮发现了 collectCompressedKeys 返回空（单个函数 bug），第二轮揭示了这是整条 event_key 注入链路的系统性断裂。
- **完整链路分析**：
  - **第一环断裂**：event_key 存在于 StateDelta/Session.State，但 SmartCompressor 不访问 Session.State，消息内容也不含 event_key
  - **第二环失效**：collectCompressedKeys 依赖已移除的前缀 → 返回空
  - **第三环失效**：压缩事件不含 key 列表 → LLM 看不到 key
  - **第四环失效**：LLM 不会传 event_keys 参数给 AgentToolWrapper（因为上下文中没有 key）
  - **第五环失效**：AgentToolWrapper 的 event_keys 解析逻辑正确但永远不被触发
  - **第六环失效**：IngestExternalEvents 实现正确但永远不会被调用（externalEvents 总是空）
- **根因**：Phase 1 移除了前缀注入逻辑（context_intervention.go），但未提供替代的 event_key 注入机制。所有依赖 event_key 可见性的下游功能因此失效。
- **影响范围**：设计目标 1（事件回溯）和设计目标 5（上下文隔离）的端到端功能链路完全失效。
- **修复方向**：需要设计新的 event_key 注入机制——要么在 BeforeModel 中从 Session.State 提取 event_key 并注入到消息元数据中，要么让 SmartCompressor 从 Invocation 中获取 event_key 列表。

---

### 目标 2: 内存中心一致性 — Session 存储模型偏差

#### 预期做什么

设计预期：MemoryStore 是唯一事实来源（FullEvent），Session 只保存轻量引用（EventReference）。

#### 需要做什么

1. FullEvent 在 MemoryStore 中存储完整事件数据
2. Session 只存 EventReference（轻量引用）
3. 两者数据一致，MemoryStore 为权威来源

#### 目前怎么做

1. ✅ FullEvent 包含完整数据（Content, ToolCalls, Response）
2. ❌ **Session 仍存储完整 model.Message 列表** —— tagent 使用 trpc-agent-go 框架的 Session，Session 内部维护完整的消息历史。MemoryPlugin 在 OnEvent 中将事件持久化到 MemoryStore，但 Session 本身不引用 MemoryStore。
3. EventReference 仅作为 MemoryStore.QueryEvents 的返回类型，Session 不使用它

#### [P2] Session 存储模型与设计预期不符

- **偏差性质**：架构级偏差，非 bug
- **分析**：trpc-agent-go 框架的 Session 管理是不可修改的（tagent 在框架之上构建）。Session 存储完整消息是框架默认行为。tagent 添加 MemoryStore 作为补充持久层，但未实现"Session 只存引用"的设计目标。
- **影响**：数据冗余——Session 和 MemoryStore 各存一份完整事件数据。在长对话中，Session 的消息列表持续增长，与 MemoryStore 的压缩/压实机制脱节。
- **缓解因素**：SmartCompressor 的视图转换在 BeforeModel 中截断 Request.Messages，间接缓解了 Session 增长对 LLM 上下文的影响。但 Session 本身仍存储完整历史。
- **修复方向**：若要完全实现设计目标，需要自定义 Session 实现（替换框架默认 Session），或在 OnEvent 后主动截断 Session 的消息历史。但这涉及框架层面的修改，成本较高。

#### [P2] lastEventKeys 全局因果链不分 session/user

- **模块**: plugin/memory_plugin.go:41
- **问题描述**: `lastEventKeys map[int]int64` 按 PartitionID 维护因果链，但不区分 session 或 user。如果同一 PartitionID（即同一 agent name）被并发调用（如两个用户同时使用 tagent），lastEventKeys 会串线——用户 A 的事件可能被设为用户 B 事件的 parent。
- **与第一轮的区别**：第一轮发现了 lastEventKeys 的并发安全问题（mutex → 已有保护 ✅），但未分析跨 session/user 串线的逻辑问题。
- **影响**：因果链可能跨用户串联，导致 recall agent 追溯到其他用户的事件。
- **修复方向**：lastEventKeys 应按 (PartitionID, SessionID) 或 (PartitionID, UserID) 维护独立因果链。

---

### 目标 3: 视图转换完整性 — 视图转换状态泄漏

#### 预期做什么

设计预期：压缩只修改发给 LLM 的 Request.Messages，不破坏 Session/MemoryStore 原始数据。

#### 需要做什么

1. BeforeModel 只修改 args.Request.Messages
2. 不修改 Session/MemoryStore
3. **视图转换应是无状态的** —— 每次 BeforeModel 应基于原始配置压缩，不携带上次压缩的副作用

#### 目前怎么做

1. ✅ BeforeModel 只修改 args.Request.Messages
2. ✅ 不修改 Session/MemoryStore
3. ❌ **KeepRecentTasks 跨请求泄漏** —— context_intervention.go:81-85 在压缩循环中递减 `ci.compressor.KeepRecentTasks`，且从不恢复。后续请求使用已递减的值。

#### [P2] KeepRecentTasks 状态泄漏违反视图转换无状态原则

- **与第一轮的区别**：第一轮发现了 KeepRecentTasks 递减不恢复（作为独立 bug），第二轮从"视图转换应为无状态"的设计原则角度分析，揭示这是一个设计原则违反。
- **分析**："视图转换"的设计语义是——每次对 LLM 的请求视图是独立计算的，不应受上次视图转换的影响。KeepRecentTasks 递减不恢复意味着第 N 次请求的视图受前 N-1 次请求的压缩历史影响，违反了无状态原则。
- **修复方向**：使用局部变量保存原始 KeepRecentTasks，在 BeforeModel 结束时恢复；或将 KeepRecentTasks 作为 Compress 方法的参数而非结构体字段。

---

### 目标 4: 因果关系完整性 — 整体对齐良好

#### 预期做什么

FullEvent 不含 ParentKey，因果关系由独立的 RelationStore 管理；因果链按 PartitionID 独立维护；墓碑标记触发级联修复。

#### 需要做什么

1. FullEvent 无 ParentKey ✅
2. RelationStore 独立管理关系 ✅
3. PartitionID 隔离因果链 ✅（但见目标 2 的 lastEventKeys 串线问题）
4. 级联修复 ✅

#### 目前怎么做

- FullEvent 无 ParentKey ✅（types.go:26-29 明确注释）
- InMemRelationStore 双图 + WAL 持久化 ✅
- MemoryPlugin.lastEventKeys 按 PartitionID 维护 ✅（但有串线问题，见目标 2）
- TombstoneSet.MarkTombstone 级联修复有环检测 ✅
- truncateJournal 竞争条件（P1，第一轮已发现）

#### 对齐评估

因果关系完整性是 6 项设计目标中对齐最好的。内容-关系分离的设计正确且实现一致。主要问题集中在实现层面的并发安全（truncateJournal）和逻辑层面的因果链隔离（lastEventKeys 串线），而非设计偏差。

---

### 目标 5: 上下文隔离完整性 — 端到端链路失效

#### 预期做什么

LLM 只输出 int64 数字 key，AgentToolWrapper 服务端解析获取完整事件内容；子 agent 通过 IngestExternalEvents 注入父 agent 事件。

#### 需要做什么

完整链路：event_key 在 LLM 上下文中可见 → LLM 选择 key → AgentToolWrapper 解析 → IngestExternalEvents 注入

#### 目前怎么做

1. ❌ event_key 不在 LLM 上下文中（见目标 1 分析）
2. ✅ AgentToolWrapper.Declaration 声明 event_keys 参数（tool_agent.go:98-104）
3. ✅ AgentToolWrapper.Call 正确解析 event_keys（tool_agent.go:135-161）
4. ✅ IngestExternalEvents 注入 EventSummary（不含完整 Content）✅
5. ✅ ReadNamespaces → ReadPartitionIDs 转换正确（tagent.go:158-161）

#### [P0] 上下文隔离端到端链路失效

- **与目标 1 的关联**：这是目标 1 中 event_key 注入链路断裂的直接后果。上下文隔离的整个设计依赖于 LLM 能看到并选择 event_key，但 event_key 从未出现在 LLM 上下文中。
- **影响**：
  - AgentToolWrapper 的 event_keys 参数声明正确，但 LLM 永远不会传递此参数（因为上下文中没有 key 可选）
  - AgentToolWrapper.Call 中的 event_keys 解析逻辑（line 135-161）永远不会执行到有数据的分支
  - IngestExternalEvents 实现正确但永远不会被调用（externalEvents 总是空）
  - 子 agent 无法获取父 agent 的完整事件上下文
- **与第一轮的区别**：第一轮标记为"无发现问题"（目标 5 正向确认），第二轮揭示了端到端链路失效。
- **修复方向**：与目标 1 的修复方向一致——需要先解决 event_key 注入问题，才能激活上下文隔离链路。

---

### 目标 6: 分层存储与生命周期一致性 — 压实调度不完整

#### 预期做什么

L0→L1→L2→L3 压实流程完整；墓碑过滤在合并时生效；TTL + 容量驱逐；Repair 修复悬空引用。

#### 需要做什么

1. 完整的 L0-L3 自动压实调度
2. 墓碑过滤在 Compactor 中生效
3. TTL + 容量驱逐
4. 悬空引用修复

#### 目前怎么做

1. ❌ checkAndCompact 只调用 checkHourlySeal（L0→L1），不调用 CompactL1ToL2 和 CompactL2ToL3
2. ❌ filterTombstoned 是 no-op stub
3. ✅ LifecycleManager TTL + 容量驱逐 + scannerLoop
4. ✅ repairDanglingRefs 通过 RelationStore 修复

#### [P2] L1→L2/L2→L3 方法已实现但未调度

- **与第一轮的区别**：第一轮说"仅实现 L0→L1"，第二轮发现 CompactL1ToL2（compaction.go:182-242）和 CompactL2ToL3（compaction.go:366-427）方法已完整实现（含 Merge→Filter→Repair→Write→Cleanup 全流程），只是 checkAndCompact（line 148-156）不调用它们。
- **分析**：代码注释说"Full partition discovery and compaction scheduling will be added in subsequent iterations"。压实逻辑已编码，但缺少分区发现和调度触发——即缺少"哪些 L1 段已经积累到阈值、需要合并为 L2"的判断逻辑。
- **修复方向**：在 checkAndCompact 中添加 L1 段计数逻辑，当 L1 段数达到 L1Threshold 时触发 CompactL1ToL2；类似地处理 L2→L3。

#### [P2] lifecycle.go TTL 检查使用窗口时间近似事件时间

- **模块**: memory/lifecycle.go:165
- **问题描述**: `eventAge := now - eventPK.WindowTS*1000` 使用段窗口时间戳（小时级精度）近似事件实际时间，而非从事件 JSON 中解析 Timestamp 字段。
- **影响**：TTL 过期检查的精度是小时级的。一个在窗口开始时创建的事件和在窗口结束时创建的事件，TTL 检查结果相同。可能导致事件被过早或过晚标记为 tombstone。
- **修复方向**：从事件 JSON 中解析 Timestamp 字段用于 TTL 计算，而非使用窗口时间戳。

---

### 横向分析: Phase 1 移除的系统性影响

#### 预期做什么

Phase 1 是事件视图转换机制——在消息内容前添加 `[evt_<KEY>|<type>]` 前缀，使 LLM 能感知 event_key。

#### 需要做什么

Phase 1 移除后，需要提供替代的 event_key 注入机制，确保依赖 event_key 可见性的所有下游功能继续工作。

#### 目前怎么做

Phase 1 移除了前缀注入逻辑，但**未提供替代机制**。受影响的下游功能：

| 下游功能 | 依赖 | 当前状态 |
|---------|------|--------|
| collectCompressedKeys | 消息前缀中的 event_key | ❌ 总是返回空 |
| buildCompressEvent key 列表 | collectCompressedKeys 返回值 | ❌ 不输出 key 列表 |
| LLM 回溯被压缩事件 | 压缩事件中的 key 列表 | ❌ 无 key 可用 |
| AgentToolWrapper event_keys 参数 | LLM 上下文中的 event_key | ❌ LLM 不传此参数 |
| IngestExternalEvents | AgentToolWrapper 解析的 event_keys | ❌ 永不触发 |

#### 结论

Phase 1 移除是一个**半完成的重构**——移除了前缀注入，但未提供替代的 event_key 注入机制。这不是单个函数的 bug，而是**整条 event_key 可见性链路的断裂**，影响设计目标 1 和 5 的核心功能。

---

### 横向分析: 文档与代码的系统性脱节

#### 预期做什么

wiki 文档应准确反映当前代码实现。

#### 需要做什么

代码经历 Phase 1-7 迭代后，文档需要同步更新。

#### 目前怎么做

| 偏差类型 | 涉及文档 | 具体内容 |
|---------|---------|--------|
| FullEvent.ParentKey | memory/agent/plugin/tool wiki | 仍描述 ParentKey 字段（已移除）|
| FileBackend | memory/plugin/tool wiki | 仍描述 FileBackend（不存在）|
| Phase 1 代码 | agent wiki | 展示已移除的事件视图转换代码 |
| 已实现功能标为"未来" | agent/plugin/tool wiki | lastEventKeys map 已实现但仍标为"改进/未来工作"|
| README 项目结构 | README.md | 仅列 3 个文件，缺失全部核心目录 |

#### 结论

文档是在某个早期版本生成的，之后代码经历了 Phase 1-7 迭代，但文档未同步更新。6 份 wiki 中仅 event-architecture.md 和 prompt-architecture.md 准确。

---

## 八、第二轮审查结论与修复路线图

### 对齐评估总览

| 设计目标 | 对齐程度 | 核心偏差 |
|---------|---------|--------|
| 1. 事件驱动一致性 | ❌ 严重偏差 | event_key 注入链路端到端断裂 |
| 2. 内存中心一致性 | ⚠️ 部分偏差 | Session 仍存完整消息；lastEventKeys 跨 session 串线 |
| 3. 视图转换完整性 | ⚠️ 部分偏差 | KeepRecentTasks 状态泄漏违反无状态原则 |
| 4. 因果关系完整性 | ✅ 对齐良好 | 仅实现层面并发安全问题 |
| 5. 上下文隔离完整性 | ❌ 严重偏差 | 端到端链路失效（目标 1 断裂的后果）|
| 6. 分层存储与生命周期 | ⚠️ 部分偏差 | L1→L2/L2→L3 未调度；TTL 精度不足 |

### 修复路线图（按依赖关系排序）

**第一阶段：恢复 event_key 注入链路（解决目标 1+5 的根本问题）**

1. 设计新的 event_key 注入机制——在 BeforeModel 中从 Session.State/StateDelta 提取每条消息对应的 event_key，以某种形式注入到 LLM 可见的上下文中（如消息元数据或压缩事件中）
2. 重写 collectCompressedKeys 从 Invocation/Session.State 提取 event_key
3. 确保 buildCompressEvent 输出 key 列表
4. 验证 AgentToolWrapper → IngestExternalEvents 链路可被触发

**第二阶段：修复实现层面缺陷（第一轮 P0+P1）**

5. 修复 5 个 P1 并发安全问题（TagentAgent/TmuxMonitor/truncateJournal）
6. 修复 KeepRecentTasks 状态泄漏
7. 接入 filterTombstoned 到 TombstoneSet

**第三阶段：完善分层存储调度**

8. 在 checkAndCompact 中添加 L1→L2/L2→L3 调度逻辑
9. 修复 lifecycle.go TTL 时间精度
10. 修复 lastEventKeys 跨 session 串线问题

**第四阶段：文档同步**

11. 重写 memory-architecture.md（最严重过时）
12. 修正 agent-architecture.md（移除 Phase 1 代码，更新 lastEventKeys 描述）
13. 修正 plugin/tool wiki（移除 ParentKey/FileBackend）
14. 更新 README.md 项目结构

### 与第一轮的关系

第二轮审查不是第一轮的重复，而是从"设计预期对齐"的更高视角重新审视。第一轮发现了 31 个具体问题（P0×1, P1×5, P2×20, P3×5），第二轮揭示了这些问题的系统性根因：

- **第一轮的 P0（collectCompressedKeys 返回空）** 只是 event_key 注入链路断裂的一个表现。第二轮揭示了整条链路（6 个环节）的系统性失效。
- **第一轮标记为"无发现问题"的目标 5（上下文隔离）** 实际上端到端链路已失效。第二轮纠正了这个遗漏。
- **第一轮发现的 KeepRecentTasks 递减** 不只是代码 bug，而是视图转换无状态原则的违反。
- **第一轮的“仅实现 L0→L1”** 实际上 L1→L2/L2→L3 方法已完整实现，只是缺少调度触发。

---

## 九、第三轮审查准备：方法论盲区反思

**触发原因**：fix-design-alignment 完成后，用户对工具执行安全性和 event store 可持续性提出质疑。通过代码审查发现了 3 个 P0 级和 2 个 P1 级问题，这些问题在第一轮和第二轮审查中均未被发现。本节反思盲区根因，为第三轮审查（阶段三：端到端数据链追踪）做准备。

### 盲区根因分析

#### 根因 1：审查方法论缺少“端到端数据链追踪”维度

两阶段方法（逐模块 + 横向维度）适合发现模块内部问题和跨模块系统性问题，但不适合发现**跨模块接线断链**。design.md D1 已修订为三阶段方法，增加阶段三端到端数据链追踪。

#### 根因 2：spec.md 的“生产接线完整性”和“工具执行安全”在审查后追加

这两个 Requirement 是 fix-design-alignment 修复完成后才添加的“ADDED Requirements”，审查报告从未按这些标准执行。现已增加 5 个新 Scenario 补全。

#### 根因 3：即使回溯应用现有标准，仍缺少关键 Scenario

| 缺失的 Scenario | 实际后果 | 已修复 |
|---|---|---|
| 字段使用完整性验证 | TmuxExecutor.runAsUser 有字段有 setter，但 CreateSession 从未使用 | ✅ spec.md 已增加 |
| 生产入口创建验证 | LifecycleManager/Compactor 从未在 tagent.go 中创建 | ✅ spec.md 已增加 |
| 状态回收闭环验证 | 事件只进不出，TTL/压实/墓碑均未启动 | ✅ spec.md 已增加 |
| 数据传递链完整性验证 | TmuxCreateOptions.Env 被接收但被丢弃 | ✅ spec.md 已增加 |
| 跨路径行为对称性验证 | exec 有 sudo 隔离但 tmux_exec 没有 | ✅ spec.md 已增加 |

### 审查发现（第三轮正式结果）

以下发现基于 tasks.md 阶段 13 的 7 项端到端数据链追踪任务，每项发现均经过代码引用验证。

---

### 十、第三轮审查：端到端数据链追踪结果

#### 10.1 配置链追踪（任务 13.1）

**追踪路径**：YAML 配置 → config.go CommandProperties → builtin.go commandFactory → NewCommandTool → CommandExecutor / TmuxExecutor → 实际执行点

**run_as_user 配置链**：
```
YAML run_as_user → CommandProperties.RunAsUser → commandFactory WithCommandRunAsUser → ct.runAsUser
  ├─ exec 模式: ct.runAsUser → CommandSpec.RunAsUser → buildCommandWithContext → sudo -n -u ✅
  └─ tmux_exec 模式: ct.runAsUser → NewTmuxExecutor() 未传递 ❌ 断链
```

**workspace 配置链**：
```
YAML workspace → CommandProperties.Workspace → commandFactory WithCommandWorkspace → ct.workspace
  ├─ exec 模式: ct.workspace → CommandSpec.Workspace → buildCommandWithContext → cmd.Dir ✅
  └─ tmux_exec 模式: ct.workspace → NewTmuxExecutor() 未传递 ❌ 断链
```

**env 配置链**：
```
LLM args.Env
  ├─ exec 模式: args.Env → CommandSpec.Env → buildEnv → cmd.Env ✅
  └─ tmux_exec 模式: args.Env → TmuxCreateOptions.Env → CreateSession 未使用 ❌ 断链
```

#### [P0] tmux_exec 模式安全配置全面断链

- **模块**: tool/command/command_tool.go NewCommandTool 第 103 行
- **问题描述**: `NewTmuxExecutor()` 创建 TmuxExecutor 时未传递 `WithTmuxRunAsUser(ct.runAsUser)`、`WithTmuxWorkspace(ct.workspace)`，导致安全配置在 tmux_exec 模式下完全失效
- **断链清单**: runAsUser ❌、runAsGroup ❌、workspace ❌、env ❌
- **影响**: 即使 YAML 配置了 `run_as_user: tagent-runner`，tmux_exec 模式下的命令仍以 tagent 进程用户执行，无用户隔离
- **严重性提升理由**: 从 P1 提升为 P0——这是安全边界的系统性缺失，不是单个字段遗漏

#### 10.2 生命周期链追踪（任务 13.2）

**LifecycleManager**：
```
tagent.go resolveMemoryStore → ❌ 没有 NewLifecycleManager
  └─ 仅在 lifecycle_test.go:161 中创建和启动
```

**Compactor**：
```
tagent.go resolveMemoryStore → ❌ 没有 NewCompactor
  └─ 仅在 compaction_test.go:17 中创建和启动
```

**TmuxMonitor**：
```
NewCommandTool → NewTmuxMonitor ✅ → executeAsync Start() ✅
  └─ Stop(): 仅在 command_test.go 中调用 ❌ 生产代码无 Stop()
```

#### [P0] LifecycleManager 从未在生产代码中创建和启动

- **模块**: tagent.go resolveMemoryStore（第 320-354 行）
- **问题描述**: resolveMemoryStore 创建了 FileSegmentStore，但完全没有创建 LifecycleManager，更没有调用 Start()
- **影响**: TTL 过期扫描不运行、容量驱逐不运行
- **后果**: 事件永不过期，RustViking KV 无限增长

#### [P0] Compactor 从未在生产代码中创建和启动

- **模块**: tagent.go resolveMemoryStore（第 320-354 行）
- **问题描述**: resolveMemoryStore 没有创建 Compactor，没有调用 Start()
- **影响**: L0→L1 seal 不运行、L1→L2/L2→L3 压实不运行
- **后果**: 分层存储形同虚设，所有事件停留在 L0

#### [P1] TmuxMonitor.Stop() 从未在生产代码中调用

- **模块**: tool/command/command_tool.go
- **问题描述**: TmuxMonitor.Start() 在 executeAsync 中调用（第 244 行），但 CommandTool 没有 Close/Shutdown 方法，TmuxMonitor.Stop() 从未在生产代码中调用
- **影响**: 进程退出时 monitor goroutine 泄漏，tmux session 可能残留
- **仅在测试中**: command_test.go 14 处 `defer tool.tmuxMonitor.Stop()`

#### 10.3 字段使用完整性追踪（任务 13.3）

| 结构体字段 | 定义位置 | Setter | 业务引用 | 状态 |
|---|---|---|---|---|
| TmuxExecutor.runAsUser | tmux_executor.go:16 | WithTmuxRunAsUser | 无 | ❌ 死字段 |
| TmuxExecutor.workspace | tmux_executor.go:15 | WithTmuxWorkspace | CreateSession:109, RestartSession:304 | ✅ |
| TmuxExecutor.prefix | tmux_executor.go:14 | WithTmuxPrefix | CreateSession:97, ListSessions:261 | ✅ |
| CommandExecutor.runAsUser | command_executor.go:18 | WithExecutorRunAsUser | buildCommandWithContext:174 | ✅ |
| CommandExecutor.runAsGroup | command_executor.go:19 | WithExecutorRunAsGroup | buildCommandWithContext:178 | ✅ |
| CommandTool.runAsUser | command_tool.go:36 | WithCommandRunAsUser | executeSync:198 | ⚠️ 未传递给 TmuxExecutor |
| CommandTool.runAsGroup | command_tool.go:37 | WithCommandRunAsGroup | executeSync:199 | ⚠️ 未传递给 TmuxExecutor |
| CommandTool.workspace | command_tool.go:34 | WithCommandWorkspace | executeSync:196 | ⚠️ 未传递给 TmuxExecutor |

#### [P1] TmuxExecutor.runAsUser 是死字段

- **模块**: tool/command/tmux_executor.go
- **问题描述**: runAsUser 字段定义在第 16 行，WithTmuxRunAsUser setter 在第 37-40 行赋值，但 CreateSession、KillSession、RestartSession 等所有方法从未引用 `te.runAsUser`
- **影响**: 即使通过 WithTmuxRunAsUser 设置了用户，tmux session 中的命令仍以当前进程用户执行

#### 10.4 状态回收闭环追踪（任务 13.4）

**事件存储闭环**：
```
StoreEvent ✅ → TTL 扫描 ❌ → MarkTombstone ❌ → filterTombstoned ❌ → 物理删除 ❌
```
闭环完全断裂——事件只进不出，无任何自动清理机制在生产代码中运行。

**tmux session 闭环**：
```
CreateSession ✅ → AddSession ✅ → monitorLoop ✅ → checkSession ✅ → KillSession+delete ✅
  ├─ KillSession 失败仍删除 ⚠️
  ├─ TUI 会话永不回收 ⚠️
  └─ Stop() 从未调用 ⚠️
```
基本闭环但有不完善处。

#### [P0] 事件存储状态回收闭环完全断裂

- **模块**: tagent.go / memory/lifecycle.go / memory/compaction.go / memory/tombstone.go
- **问题描述**: 事件存储的完整回收链（TTL 扫描 → 墓碑标记 → 压实过滤 → 物理删除）在生产代码中无任何环节被启动
- **影响**: 事件只进不出，RustViking KV 无限增长
- **根因**: LifecycleManager 和 Compactor 从未在 resolveMemoryStore 中创建

#### [P1] KillSession 失败后 session 仍从 map 删除

- **模块**: tool/command/tmux_monitor.go handleFakeDead（第 405-415 行）
- **问题描述**: `KillSession(session.ID)` 失败时只 log.Errorf，session 仍从 sessions map 删除（shouldRemove=true）
- **影响**: 可能残留僵尸 tmux session，无法被 monitor 追踪或清理

#### [P2] TUI 会话在 fakeDead 阈值后永不回收

- **模块**: tool/command/tmux_monitor.go detectSessionState（第 337-341 行）
- **问题描述**: TUI 会话在 stableDuration > fakeDeadDuration 时返回 SessionRunning（非 FakeDead），且 StableSince 保留，导致每个周期都重新评估但永不触发回收
- **影响**: TUI 会话（如 vim、htop）永不回收，除非进程自然退出

#### 10.5 数据传递链完整性追踪（任务 13.5）

**TmuxCreateOptions.Env 传递链**：
```
CommandArgs.Env → executeAsync: TmuxCreateOptions.Env (line 226) → CreateSession: 未使用 ❌
```

#### [P1] TmuxCreateOptions.Env 被接收但从未使用

- **模块**: tool/command/tmux_executor.go CreateSession（第 94-142 行）
- **问题描述**: TmuxCreateOptions 有 Env 字段，executeAsync 传递了 `Env: args.Env`，但 CreateSession 构建 tmux 命令时未设置环境变量
- **影响**: 用户传入的环境变量被静默丢弃
- **断链位置**: CreateSession 第 100-116 行构建 `tmux new-session` args 时无 env 设置逻辑

#### 10.6 跨路径行为对称性追踪（任务 13.6）

**exec vs tmux_exec 对称性**：

| 维度 | exec 模式 | tmux_exec 模式 | 对称？ |
|---|---|---|---|
| 用户隔离 | sudo -n -u \| 无 | ❌ |
| 环境变量 | buildEnv(spec.Env) | opts.Env 被丢弃 | ❌ |
| 工作目录 | cmd.Dir = workDir | tmux -c workDir | ✅ |
| 超时控制 | context.WithTimeout | monitor 周期检查 | ⚠️ 设计差异 |
| 进程清理 | SIGKILL 进程组 | KillSession | ⚠️ 不同机制 |

**正常执行 vs RestartSession 对称性**：

| 维度 | CreateSession | RestartSession | 对称？ |
|---|---|---|---|
| 用户隔离 | 无 | 不传递 | ❌ 都无 |
| 环境变量 | 丢弃 | 不传递 | ❌ 都无 |
| 工作目录 | te.workspace | te.workspace | ✅ |
| 命令 | opts.Command | opts.Command | ✅ |
| IsInteractive | opts.IsInteractive | opts.IsInteractive | ✅ |

#### [P1] exec 与 tmux_exec 权限/环境不对称

- **模块**: tool/command/command_executor.go vs tmux_executor.go
- **问题描述**: exec 模式通过 sudo 实现用户隔离，通过 buildEnv 传递环境变量；tmux_exec 模式两者都缺失
- **影响**: 配置了 run_as_user 的生产环境中，sync 命令有隔离但 async 命令无隔离

#### [P1] handleFakeAlive → RestartSession 丢失安全上下文

- **模块**: tool/command/tmux_monitor.go handleFakeAlive（第 380-402 行）→ tmux_executor.go RestartSession（第 292-314 行）
- **问题描述**: handleFakeAlive 构造 TmuxCreateOptions 时只传递 Command/WorkDir/IsInteractive，不传递 runAsUser 和 Env（虽然当前 CreateSession 也不使用它们，但这是双重缺陷）
- **影响**: 重启后的 session 丢失用户隔离和环境变量

#### 10.7 可选依赖 nil 行为追踪（任务 13.7）

| 可选依赖 | 生产环境状态 | nil 时行为 | 可接受？ |
|---|---|---|---|
| TombstoneSet | nil（NewFileSegmentStore 不设置） | GetEvent/QueryEvents 跳过墓碑检查 | ❌ |
| LifecycleManager | 不存在（从未创建） | TTL 过期扫描不运行 | ❌ |
| Compactor | 不存在（从未创建） | 压实调度不运行 | ❌ |

#### [P0] TombstoneSet 从未接入 FileSegmentStore

- **模块**: tagent.go resolveMemoryStore / memory/segment_store.go
- **问题描述**: NewFileSegmentStore 构造时 tombstones 字段为 nil，没有 SetTombstone 方法；resolveMemoryStore 也没有创建 TombstoneSet 并注入
- **影响**: GetEvent（第 321 行）和 QueryEvents（第 479 行）中的 `s.tombstones != nil` 检查永远为 false，墓碑过滤完全失效
- **后果**: 已删除事件仍可被查询到，filterTombstoned 接收 nil tombstone 返回原输入

---

### 十一、第三轮审查严重级别分布

| 级别 | 第一轮 | 第二轮 | 第三轮（新增） | 累计 |
|---|---|---|---|---|
| P0 | 1 | 2 | 4 | 7 |
| P1 | 5 | 2 | 5 | 12 |
| P2 | 20 | 4 | 1 | 25 |
| P3 | 5 | 0 | 0 | 5 |

**第三轮新增 P0 发现**（4 项）：
1. tmux_exec 模式安全配置全面断链（runAsUser/workspace/env 全部未传递）
2. LifecycleManager 从未在生产代码中创建和启动
3. Compactor 从未在生产代码中创建和启动
4. TombstoneSet 从未接入 FileSegmentStore

**第三轮新增 P1 发现**（5 项）：
5. TmuxMonitor.Stop() 从未在生产代码中调用
6. TmuxExecutor.runAsUser 是死字段
7. KillSession 失败后 session 仍从 map 删除
8. TmuxCreateOptions.Env 被接收但从未使用
9. handleFakeAlive → RestartSession 丢失安全上下文

**第三轮新增 P2 发现**（1 项）：
10. TUI 会话在 fakeDead 阈值后永不回收

---

### 十二、第三轮修复路线图

**第五阶段：生产接线完整性修复（解决第三轮 P0）**

1. **resolveMemoryStore 完整接线** — 创建 LifecycleManager + Compactor + TombstoneSet，调用 Start()，接入 FileSegmentStore
2. **NewCommandTool 安全配置传递** — 将 runAsUser/workspace 传递给 TmuxExecutor
3. **TmuxExecutor.CreateSession 权限接入** — 使用 runAsUser 构建 sudo 命令，使用 opts.Env 设置环境变量

**第六阶段：安全上下文一致性修复（解决第三轮 P1）**

4. **RestartSession 保持安全上下文** — 传递 runAsUser 和 Env
5. **CommandTool 关闭流程** — 增加 Close 方法，调用 TmuxMonitor.Stop()
6. **KillSession 失败重试** — 失败时不删除 session，等待下次检测周期重试
7. **TUI 会话回收** — fakeDead 阈值后触发超时回收

---

### 十三、审查方法论总结

审查方法论已从两阶段升级为三阶段：

1. **阶段一（逐模块）** — 逐文件阅读代码，对照 6 项设计目标记录发现
2. **阶段二（横向维度）** — 并发安全、代码一致性、接口契约交叉验证
3. **阶段三（端到端数据链追踪）** — 4 类链路追踪：
   - 配置链：YAML → 解析 → 工厂 → 子组件 → 执行点
   - 生命周期链：生产入口 → 构造 → Start → Stop
   - 数据传递链：参数定义 → 逐级传递 → 最终使用
   - 状态回收链：创建 → 检测 → 回收/清理

第三轮审查验证了三阶段方法论的有效性：阶段三发现了 4 个 P0 和 5 个 P1 问题，这些问题在前两轮中均未被发现，因为它们都是跨模块接线断链——实现存在但未接线、字段定义但未使用、组件可创建但未创建。
