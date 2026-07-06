## Context

架构审查（architecture-review）揭示了 tagent 实现与设计目标之间的系统性偏差。本设计文档详细说明如何修复这些偏差，重点关注技术决策和实现方案。

### 当前状态

**event_key 链路断裂的根因代码**：

1. `plugin/memory_plugin.go:128` — MemoryPlugin.OnEvent 将 event_key 写入 `evt.StateDelta["event_key"]`
2. `trpc-agent-go/session/session.go:533` — 框架的 `ApplyEventStateDelta` 将 StateDelta **覆盖合并**到 `Session.State`（`sess.State[key] = val`），导致 `Session.State["event_key"]` 只保留最后一个事件的 key
3. 但 `Session.Events` 是 `[]event.Event`，每个事件**保留自己的 StateDelta** — 这是可用的数据源
4. `agent/context_intervention.go:51` — BeforeModel 已获取 `inv`（含 Session），但未使用 Session.Events
5. `agent/smart_compress.go:168-185` — collectCompressedKeys 解析消息内容中的 `[evt_<KEY>|<type>]` 前缀，但 Phase 1 移除后消息内容无此前缀

**框架 API 可用性**（已验证）：
- `agent.Invocation.Session` → `*session.Session`（直接可访问）
- `session.Session.Events` → `[]event.Event`（每个事件保留 StateDelta）
- `session.Session.EventMu` → `sync.RWMutex`（保护 Events 访问）
- `event.Event.StateDelta` → `map[string][]byte`（含 "event_key"、"event_type"）
- `model.Message` → 无 metadata 字段（只有 Role/Content/ToolCalls 等）

**并发安全缺陷的根因代码**：
- `agent/tagent_agent.go:54-55` — `lastUserID/lastSessionID` 裸 string，Run/RunSimple 写 vs InjectMessage 读
- `tool/command/tmux_monitor.go:24` — `running` 裸 bool，Start/Stop 写 vs checkAllSessions/command_tool 读
- `tool/command/tmux_monitor.go:211-238` — checkSession 修改 session 对象字段，但 checkAllSessions 已释放锁
- `memory/relation_store.go:402-414` — truncateJournal 关闭/重开 journal 文件，未持写锁
- `tool/command/command_tool.go:243` — 直接读取 `ct.tmuxMonitor.running`

**因果链存储机制的实际实现**（与 wiki 描述的差异）：
- wiki 描述 `FullEvent.ParentKey` 字段维护因果链 → **实际代码已移除 ParentKey**（`memory/types.go:26-29` 注释明确说明）
- 实际机制：MemoryPlugin.onEvent 通过 `RelationStore.SetParent(eventKey, parentKey)` 维护因果关系（`plugin/memory_plugin.go:111-118`）
- MemoryStore 通过 `RelationStoreProvider` 接口暴露 RelationStore
- lastEventKeys 仍为 `map[int]int64`（按 PartitionID），需改为复合 key

**框架自动注入缺失**（与 wiki 描述的差异）：
- wiki（agent-architecture.md §12.5.7, tool-architecture.md §4.4/§13.3）描述「Flow 层从 StateDelta 提取 event_key 合并到 tool jsonArgs」
- **实际框架不存在此机制**——AgentToolWrapper.Call 从 LLM 传递的 JSON 参数中解析 event_keys（`agent/tool_agent.go:135-154`）
- 实际机制是 LLM 驱动选择：LLM 需在上下文中看到 event_key → LLM 在 tool_call 参数中显式传递 event_keys → AgentToolWrapper 解析
- 因此前缀注入是激活整条链路的必要条件

## Goals / Non-Goals

**Goals:**

1. 恢复 event_key 注入链路：LLM 可感知 event_key → 可回溯被压缩事件 → 可传 key 给子 agent
2. 修复 5 个 P1 并发安全缺陷
3. 因果链按 (PartitionID, SessionID) 隔离，防止跨 session 串线
4. 视图转换无状态化，消除 KeepRecentTasks 跨请求泄漏
5. 接入 L1→L2/L2→L3 压实调度和墓碑过滤
6. 修复 TTL 时间精度
7. 统一错误处理和代码一致性
8. 同步文档与代码

**Non-Goals:**

- 不实现 Session 只存引用的设计目标（需修改框架层，成本过高，留作未来工作）
- 不实现向量搜索（RustViking embedding 集成）
- 不实现框架层 event_key 自动注入到 tool args（wiki 描述的「Flow 层从 StateDelta 提取 event_key」机制；实际使用 LLM 驱动选择替代）
- 不改变任何接口签名或公开 API
- 不评价新功能设计

## Decisions

### 决策 1: event_key 注入 — 位置匹配前缀注入法

**选择**：在 BeforeModel 中，通过位置匹配将 event_key 从 Session.Events 注入到消息内容前缀。

**方案**：

```
BeforeModel:
  1. 从 inv.Session.Events 提取 event_key 列表（每个事件的 StateDelta["event_key"]）
  2. 遍历 args.Request.Messages，按位置匹配 Session.Events：
     - 跳过 system 消息（非事件来源）
     - 跳过 tool 消息（属于前一个 assistant 事件）
     - user/assistant 消息按序对应 events[eventIdx++]
  3. 为匹配到的消息添加前缀："[evt_<KEY>|<type>] " + 原内容
  4. collectCompressedKeys 现有逻辑（parseEventKeyFromPrefix）可直接工作
  5. buildCompressEvent 输出 key 列表 → LLM 可见
```

**为什么选择前缀注入而非其他方案**：

| 方案 | 优点 | 缺点 | 结论 |
|------|------|------|------|
| A. 前缀注入（本方案） | collectCompressedKeys 无需修改；LLM 可在每条消息上看到 key；上下文隔离可直接工作 | 修改消息内容（但是视图转换的合法行为）；每条消息增加 ~20 字符 token 开销 | ✅ 选择 |
| B. 从 Session.Events 直接收集 key | 不修改消息内容 | LLM 只在压缩事件中看到 key，非压缩事件的 key 不可见 → 上下文隔离不完整 | ❌ 不满足目标 5 |
| C. 消息 metadata 注入 | 不修改内容 | model.Message 无 metadata 字段；需修改框架 | ❌ 不可行 |
| D. 压缩事件中列出所有 key | 简单 | LLM 只在压缩后看到 key，无法在压缩前选择 key 传给子 agent | ❌ 不满足目标 5 |

**与 Phase 1 的区别**：Phase 1 在单独的视图转换步骤中注入前缀，然后用指纹匹配（内容比较）将前缀消息映射回 Session.Events，导致不匹配。本方案在 BeforeModel 中直接通过**位置匹配**（非内容匹配）建立映射，避免指纹问题。

**位置匹配的安全性**：
- system 消息：框架始终将其放在 messages[0]，不是事件来源 → 跳过
- tool 消息：由框架从 assistant 事件的 ToolCalls 生成，不是独立事件 → 跳过
- user/assistant 消息：按出现顺序对应 Session.Events → 位置匹配

**边界情况处理**：
- 如果消息数 > 事件数（如 InjectMessage 注入的额外消息）：eventIdx 越界时停止注入
- 如果消息内容与事件内容不匹配：跳过该消息不注入（安全降级）
- 如果 inv 或 inv.Session 为 nil：跳过注入，collectCompressedKeys 返回空（降级为当前行为）

**event_key 端到端流程**（前缀注入后激活的完整链路）：

```
1. MemoryPlugin.OnEvent
   → 生成 Snowflake EventKey
   → 写入 evt.StateDelta["event_key"]
   → StoreEvent 到 MemoryStore
   → RelationStore.SetParent(eventKey, parentKey)

2. 框架持久化事件到 Session.Events
   → 每个事件保留自己的 StateDelta（含 event_key）

3. ContextIntervention.BeforeModel（新增前缀注入）
   → 从 inv.Session.Events 逐个提取 StateDelta["event_key"]
   → 位置匹配 args.Request.Messages
   → 为 user/assistant 消息添加 [evt_<KEY>|<type>] 前缀

4. SmartCompressor.Compress（触发压缩时）
   → collectCompressedKeys 从旧段消息解析 [evt_<KEY>|<type>] 前缀
   → buildCompressEvent 输出 key 列表到压缩事件内容
   → LLM 在压缩事件中看到被压缩的 key 列表

5. LLM 发起 tool_call
   → LLM 从前缀或压缩事件中看到 event_key
   → LLM 在 tool_call 参数中传递 event_keys: [k1, k2, ...]

6. AgentToolWrapper.Call（已有代码）
   → 从 JSON args 解析 event_keys
   → parentStore.GetEvent(key) 获取完整 FullEvent
   → IngestExternalEvents(externalEvents) 注入子 Agent

7. 子 Agent 运行
   → 看到完整事件上下文（含 Content、ToolCalls）
   → 返回结果给父 Agent
```

**关键**：步骤 3（前缀注入）是整条链路的激活点。没有前缀注入，步骤 4-6 全部失效：
- collectCompressedKeys 找不到 key → 压缩事件无 key 列表
- LLM 看不到 key → 不会传 event_keys 给工具
- AgentToolWrapper 得不到 event_keys → IngestExternalEvents 不触发
- 子 Agent 缺少上下文 → 上下文隔离设计目标落空

### 决策 2: 并发安全 — 最小侵入式同步

**选择**：对每个并发问题采用最小侵入式的同步方案。

| 问题 | 方案 | 理由 |
|------|------|------|
| TagentAgent.lastUserID/lastSessionID | 加 `sync.Mutex`，提供 get/set 方法 | 字段少，mutex 足够；atomic.Value 需要类型转换，更复杂 |
| TmuxMonitor.running | 改用 `sync/atomic.Bool` | 单 bool 字段，atomic.Bool 是标准方案 |
| TmuxMonitor.checkSession | 在 checkAllSessions 中持锁调用 checkSession | checkSession 修改 session 字段必须在锁内；需检查是否有死锁风险（checkSession 不调用需要锁的方法） |
| InMemRelationStore.truncateJournal | 在 truncateJournal 内部获取写锁 | SaveSnapshotToFile 调用 Snapshot（读锁）后释放锁，再调用 truncateJournal（无锁）→ 在 truncateJournal 内加写锁 |
| CommandTool.tmuxMonitor.running | 提供 `IsRunning() bool` 方法 | 封装 atomic.Bool 读取，避免外部直接访问字段 |

**truncateJournal 加锁的细节**：

当前代码：
```go
func (rs *InMemRelationStore) SaveSnapshotToFile() error {
    snapshot, err := rs.Snapshot()  // 获取读锁，释放
    // ... 写文件 ...
    rs.truncateJournal()  // 无锁！并发 SetParent 可能写入已关闭的 journal
}
```

修复后：
```go
func (rs *InMemRelationStore) truncateJournal() error {
    rs.mu.Lock()
    defer rs.mu.Unlock()
    // ... 关闭旧 journal、打开新 journal ...
}
```

注意：SaveSnapshotToFile 调用 Snapshot（读锁）后释放，然后 truncateJournal 获取写锁。这之间可能有并发的 SetParent 写入 journal（旧文件），但这些写入会在 truncate 前完成（因为 SetParent 持有写锁写 journal，truncate 等待写锁）。truncate 获取写锁后，关闭旧 journal 并打开新 journal，后续 SetParent 写入新 journal。这是安全的。

### 决策 3: 因果链隔离 — 复合 key 方案

**选择**：将 `lastEventKeys` 的 key 从 `int`（PartitionID）改为 `string`（复合 key：`"partitionID:sessionID"`）。

**方案**：
```go
type MemoryPlugin struct {
    memStore      memory.MemoryStore
    mu            sync.Mutex
    lastEventKeys map[string]int64  // "partitionID:sessionID" → lastEventKey
}

func (p *MemoryPlugin) onEvent(ctx, inv, evt) {
    partitionID := memory.PartitionIDFromName(agentName)
    sessionID := ""
    if inv != nil && inv.Session != nil {
        sessionID = inv.Session.ID
    }
    causalKey := fmt.Sprintf("%d:%s", partitionID, sessionID)
    parentKey := p.lastEventKeys[causalKey]

    // ... 构建 FullEvent（不含 ParentKey）...
    // ... StoreEvent ...

    // 通过 RelationStore 维护因果关系（非 FullEvent.ParentKey）
    if parentKey != 0 {
        if rsp, ok := p.memStore.(memory.RelationStoreProvider); ok {
            rsp.RelationStore().SetParent(eventKey, parentKey)
        }
    }

    p.lastEventKeys[causalKey] = eventKey
}
```

**注意**：FullEvent 已移除 ParentKey 字段（`memory/types.go:26-29`）。因果关系通过 RelationStore.SetParent 维护，实现了不可变事件内容与可变关系的分离。

**为什么选择复合 key 而非嵌套 map**：
- 复合 key `map[string]int64` 简单，无需两层 map 管理
- sessionID 为空时（理论不应发生），所有事件归入同一因果链（降级为当前行为）
- 内存开销可接受：每个活跃 session 一个条目，session 结束后可清理

**Session 清理**：lastEventKeys 不会自动清理已结束 session 的条目。对于长期运行的进程，可以添加 TTL 清理或在 session 结束时清除。但作为初始修复，内存泄漏风险很低（每个条目仅 ~50 字节），留作后续优化。

### 决策 4: KeepRecentTasks 无状态化 — 局部变量方案

**选择**：在 BeforeModel 中保存原始值，defer 恢复。

```go
func (ci *ContextIntervention) BeforeModel(ctx, args) {
    originalKeepRecent := ci.compressor.KeepRecentTasks
    defer func() {
        ci.compressor.KeepRecentTasks = originalKeepRecent
    }()
    // ... 压缩循环中可安全递减 KeepRecentTasks ...
}
```

**为什么选择 defer 恢复而非参数传递**：
- defer 恢复最小侵入，不改变 Compress 方法签名
- 参数传递需要修改 Compress → collectCompressedKeys → buildCompressEvent 调用链，侵入性更大
- defer 恢复保证即使压缩循环 panic 也能恢复原值

### 决策 5: 压实调度 — 段计数触发方案

**选择**：在 checkAndCompact 中添加 L1/L2 段计数逻辑。

```go
func (c *Compactor) checkAndCompact() {
    c.checkHourlySeal()    // L0→L1（已有）
    c.checkL1ToL2Compaction()  // 新增
    c.checkL2ToL3Compaction()  // 新增
}

func (c *Compactor) checkL1ToL2Compaction() {
    // 遍历所有分区
    c.store.partitions.Range(func(key, value interface{}) bool {
        pid := key.(int)
        // 获取该分区的所有 L1 段
        windows, _ := c.store.ListSegments(pid)
        var l1Windows []int64
        for _, w := range windows {
            meta, _ := c.getSegmentMeta(pid, w)
            if meta != nil && meta.Layer == 1 {
                l1Windows = append(l1Windows, w)
            }
        }
        // 达到阈值则触发压实
        if len(l1Windows) >= c.config.L1Threshold {
            c.CompactL1ToL2(pid, l1Windows)
        }
        return true
    })
}
```

**Compactor 需要新增 TombstoneSet 引用**：

```go
type Compactor struct {
    store     *FileSegmentStore
    kv        KVStore
    rel       RelationStore
    tombstone *TombstoneSet  // 新增
    config    CompactionConfig
    // ...
}
```

filterTombstoned 改为：
```go
func (c *Compactor) filterTombstoned(events []FullEvent) []FullEvent {
    if c.tombstone == nil {
        return events
    }
    var result []FullEvent
    for _, evt := range events {
        if !c.tombstone.IsTombstone(evt.EventKey) {
            result = append(result, evt)
        }
    }
    return result
}
```

### 决策 6: TTL 时间精度 — JSON 解析方案

**选择**：从事件 JSON 中解析 Timestamp 字段，替代窗口时间戳近似。

```go
func (lm *LifecycleManager) checkTTL() {
    // ...
    for _, pair := range pairs {
        // 解析事件 JSON 获取实际 Timestamp
        var evt struct {
            Timestamp int64 `json:"timestamp"`
        }
        if err := json.Unmarshal([]byte(pair.Value), &evt); err != nil {
            continue
        }
        eventAge := now - evt.Timestamp
        // ...
    }
}
```

**为什么不复用 extractEventTypeFromJSON 的字符串搜索方式**：
- Timestamp 是 int64，字符串搜索需要额外处理数字提取
- json.Unmarshal 更健壮，且性能影响可接受（TTL 检查不在热路径，每小时一次）

## Risks / Trade-offs

- **[风险] 位置匹配可能错位** → 添加内容验证安全检查：如果消息 Role 与事件 Response 的 Message Role 不匹配，跳过注入。降级为不注入（当前行为），不会产生错误数据。

- **[风险] 前缀注入增加 token 开销** → 每条消息增加 ~20 字符（`[evt_123456789|agent_output] `）。对于 8000 token 预算、20 条消息的场景，增加 ~400 字符 ≈ ~100 token，占比 ~1.25%。可接受。

- **[风险] truncateJournal 加写锁影响压实性能** → truncateJournal 只在 SaveSnapshotToFile 时调用（低频），且操作仅关闭/打开文件（毫秒级）。写锁持有时间极短，不会成为瓶颈。

- **[风险] lastEventKeys 内存泄漏** → 每个条目 ~50 字节，1000 个 session 约 50KB。可接受。后续可添加 session 结束时清理。

- **[权衡] 不实现 Session 只存引用** → Session 仍存完整消息，与设计目标 2 部分偏离。但实现需要修改框架层，成本过高。SmartCompressor 的视图转换间接缓解了影响。明确标注为未来工作。

- **[风险] L1→L2 压实调度可能误触发** → 段计数基于 ListSegments 返回的所有段，需要准确区分 L0/L1/L2 层级。通过读取 SegmentMeta.Layer 字段确保准确。

## Wiki 文档与实际代码的差异总结

通过对比 6 份 wiki 文档与实际源码，识别出以下系统性差异，需在第六阶段文档同步中修复：

| 差异项 | Wiki 描述 | 实际代码 | 影响范围 |
|--------|----------|----------|----------|
| FullEvent.ParentKey | 有 ParentKey 字段维护因果链 | 已移除，用 RelationStore.SetParent | memory/plugin/tool wiki |
| FileBackend | 一文件一事件 (dataDir/{partition}/{key}.json) | 已替换为 KVStore + SegmentStore 分层存储 | memory wiki |
| Phase 1 事件视图转换 | BeforeModel 中有 applyEventView 代码 | 已移除，BeforeModel 仅做 token 预算检查 | agent wiki §8 |
| 框架自动注入 event_key | Flow 层从 StateDelta 提取 event_key 合并到 tool args | 不存在；AgentToolWrapper 从 LLM 传的 JSON args 解析 | agent/tool wiki |
| lastEventKey 全局单例 | 描述为"当前问题"，建议改为 map | 已实现 `map[int]int64`，剩余问题是跨 session 串线 | agent/tool wiki |
| Session 存储模型 | Session 只存 EventReference[] | Session 存完整 event.Event（Phase 3 未实现） | memory/plugin wiki |
| memory 文件清单 | 3 个文件 (types/in_memory/file_backend) | 9+ 个文件 (含 compaction/lifecycle/tombstone/relation_store/segment_store/kv_store) | memory wiki |
