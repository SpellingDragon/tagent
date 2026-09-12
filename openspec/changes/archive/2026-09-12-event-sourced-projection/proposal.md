## Why

R1（对话上下文跨重启连续 + prefix-cache 复用）要求压缩后的投影在重启后**逐字节复原**。但按文档铁律，投影本应是事实链的**同点旁路产物**（memory-architecture.md:1060「同点投影」、:1069「事实链只在 KV 里」）——即投影 = 事件日志（KV LSM 事实链）的回放映射。当前它**不是**：压缩折叠的产物（滚动综述 ref、`tool_chain` 合成 ref）是**负 key、仅投影、不经 StoreEvent 落库**（:983），既不在事实链、也不可重算（折叠 path-dependent：`deterministicLevel` 的 age 依赖累积投影段数；narrative/浓缩卡片是 LLM 产物）。→ 投影无法由事实链回放重建 → 重启即失 → R1 缺口。

dev 的应对是打**补丁**（`persistSnapshotEvent` 把三态快照存成可召回 `context_compress` 正 key 事件、`Replace` 回灌活投影、接 spill 恢复路径），引出 recall 污染 / Replace-over-live / race / 写入常开四缺陷——且它是「投影态存到事实链之外的游离快照」，**违背「投影=事实链旁路产物」，制造双真相源**。

**本变更按理想模型演进，不打补丁**：让压缩折叠成为事实链里的一等 **compaction 事件**，使投影重新成为**完整事件日志的纯回放**。KV LSM 事实链本就是事件溯源的 event log（不可变、snowflake 序、append-only、kv.wal+snapshot 持久化），**无需另建 WAL**；补齐它缺失的 compaction 事件即达终态，**不留未来重构抉择**。

**定位（resident-continuity-roadmap · R1）**：本变更是 R1-R4 统一架构「持久事件溯源状态 ⊥ 可热换无状态执行器」的第一阶段与**模式立范者**——确立的状态层事件溯源一般模式（事实链旁路产物 + 事件回放重建进空态 + 状态由 cm 外置持有；**snapshot+tail 是 R1 特例**——因折叠压缩而有，无压缩语义的状态层如 R2 任务板为纯全量回放）将由 R2（任务板）/R3（常驻会话态）沿用，并为 R4（执行器热换）奠基。

## What Changes

- **折叠 = 向事实链追加 compaction 事件**：真折叠时 `StoreEvent` 一条 `context_compress_summary` 正 key 事件（事实链一等条目），载荷 = 折叠这个操作的完整产物：① composed 综述正文（逐字节）② 综述 ref 身份（`EventKey=-minTs`/`EventType=context_compress`/Timestamp/Role）③ **有序 retained-ref 列表**（负 key `tool_chain` 合成 ref 存全身份+EventSummary；正 key ref 存 key）④ fullBoundary。
- **投影 = 事实链的纯回放（一等不变量）**：运行期每次 `StoreEvent → projection.Add`（增量回放，:1060 同点投影）；折叠追加 compaction 事件；重启 = 回放事实链（最新 compaction 事件作事件溯源 snapshot + 其后尾部事件 `projection.Add`）。**运行期与重启是同一「投影=日志 fold」不变量的两个入口**，非 bolt-on restore。
- **滚动 supersede**：写新 compaction 事件**前**查 prior、写后 `DeleteEvent(prior)`，只留最新活 compaction（TTL 豁免否则无限累积）；重启回放把 prior key reseed 回 cm。
- **折叠原文不删**：被折叠原始事件冷存（`[evt_key]` 票据仍 recall 回补）；回放按 retained-ref 列表跳过它们（被综述代表）。
- **综述可召回**（`Recallable:true`，用户裁决）：compaction 事件内容 = 〔历史综述〕正文（固化物长存），非 dev 的内部元数据噪音。
- **移除 dev 补丁子系统**：`persistSnapshotEvent` / `agent/compress/snapshot.go` / `ReplayProjectionHandler` 的 Replace-over-live 快照分支（保留 append/meditation 分支；spill 恢复保持 append-only）。
- **BREAKING**（对 dev ②）：移除快照事件与 Replace 回灌路径。

## Capabilities

### New Capabilities
- `event-sourced-projection`: 投影被定义为事实链（KV LSM 事件日志）的纯回放映射；压缩折叠作为一等 compaction 事件进事实链（载综述 + tool_chain 合成 ref + retained-ref 列表 + fullBoundary，滚动 supersede）；冷启动回放「最新 compaction snapshot + 尾部」逐字节重建投影（prefix-cache 复用）；单一真相源（事实链），无游离 checkpoint。

### Modified Capabilities
- `task-skeleton-compression`: 折叠点新增「向事实链追加 compaction 事件」——定向逆转 `context-efficiency-and-trajectory` 的「`context_compress_summary` 固化物产生源移除」，经减法判定标准校验为「缺失补最小量」（累积综述 path-dependent+LLM 不可重算，prefix-cache 重启要求逐字节 → 必须进事实链）；载体是 compaction 事件（composed 综述 + retained-ref 列表含 tool_chain），非 legacy 逐段原文副本。

## Impact

- **代码**：`agent/compress/context_compressor.go`（折叠点 `buildRetainedRefs` 后构造 compaction 事件载荷）、`agent/context_manager.go`（删 `persistSnapshotEvent`、折叠点 StoreEvent compaction + supersede、新增 `RebuildProjectionFromWAL` 回放）、`agent/replay_restore.go`（删 Replace-over-live 快照分支，保留 append/meditation）、`agent/compress/snapshot.go`（删）、`build_agent.go`（冷启动回放接线 ~427-447，先于 spill 重放）、`event/registry.go`（确认 `context_compress_summary` Recallable:true）。
- **存储**：`context_compress_summary` 正 key compaction 事件（TTL 豁免、可召回、滚动 supersede → 恒定 1 条活）；`MemoryStore.DeleteEvent`（types.go:101）做 supersede；折叠原文不删（冷存供 recall）。**无新增物理存储层**——复用 KV LSM 事实链 + kv.wal/snapshot 持久化。**fresh-eyes 二轮新增契约**：①存储契约新增 `QueryOptions.MinEventKey`（tail 的 key 序过滤原语，两 store 补 matchesQueryFilters——StartTime 近似会踩双时间分叉）；②compaction 事件带 `Metadata[compaction]=v1` 代际标记（supersede/snapshot 限定带标记者，保护 legacy 存量固化物不误删）；③事件契约：`trigger_source` 经 Attribution 注入 agent_output 事件 Metadata（冥想回种前置——现仅活内存不落库）；④`persistBusEvent` 对齐 stored-gate（StoreEvent 失败不 Append，交 spill 双写补回）。
- **逐字节边界**：retained 原文 full 渲染走 `GetEvent().Content`，保证成立于其 TTL 窗内（最紧 thinking_plan 3d）；热换装重启=分钟级 ≪ 3d 成立；超 TTL 降级摘要渲染、前缀在该点后失效（文档化）。
- **消除 dev 缺陷**：🔴 Replace-over-live（回放改冷启动 Replace 进空投影 + 尾部 append）、🔴 race（单 goroutine 启动期）、🔴 写入常开（仅真折叠）、🟠 recall 污染（内容改综述正文 + 进 recall 是特性）。
- **分支**：`dev-resident-bugfixes`（off dev c05fd7b）；与 dev ② 快照实现 supersede 关系，合并以本变更为准。
