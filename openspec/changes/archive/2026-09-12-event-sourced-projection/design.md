## Context

R1 需求：常驻 bot 热换装/重启后压缩上下文逐字节复原以复用 LLM prefix-cache。经代码验证（2026-09-12）确认现状与约束：

**文档铁律（memory-architecture.md）**：
- :1069「因果链与投影是写入的**旁路产物**，不参与事实链本身；**事实链只在 KV 里**」——KV LSM 事实链是唯一真相源。
- :1060「SessionProjection.Add **同点投影**（store 与视图同步）」——投影是 StoreEvent 的增量旁路产物。
- :1061「StoreEvent → LocalFileKV：kv.wal.jsonl 追加 → flushLoop → compactLocked dump kv.json」——kv.wal 是 KV 的**物理 durability 日志**（压实截断），非独立完整原始事件史。
- :983「压缩产物（context_compress 滚动摘要）是投影内**负 key** summary reference，不经 StoreEvent 落库」。

**代码事实**：
- 综述 ref 负 key、仅投影：`summaryRef{EventKey:-minTs, EventType:context_compress, EventSummary:composed}`（context_compressor.go:1110-1117），前置 retained 首位。
- **折叠 path-dependent + 非连续**：`deterministicLevel` 的 `age=totalSegs-1-segIdx`（smart_compress.go:145）依赖累积投影段数；折叠保留 skeleton 边界事件、丢中间事件（交错有洞）→ retained 集不可从原始事件重折得出。
- **两处 LLM 不可重算**：`synthesizeRollingNarrative`（context_compressor.go:886/968）+ `condenseCardLines`（curateCards 内）。
- **恰两种负 key 合成 ref**：`context_compress`（综述，registry.go:208）+ `tool_chain`（registry.go:210，`buildToolChainRef` 产 `EventKey:-minTs`/`EventSummary:"- 工具链: …"`/store 无对应事件，context_compressor.go:494-530）；`buildRetainedRefs` 一律保留存活 tool_chain（:1057-1062），旧综述被吸收（:1029-1048）。`TypeConsolidation`（:213）非 Synthetic、正 key。
- **渲染字节来源**：`resolveRef` full 走 `GetEvent().Content`（:649-659，含 ToolCalls/ToolID），非 full 走 `ref.EventSummary`（:677），由 fullBoundary 门控；fullBoundary=`anchorFullBoundary(retainedRefs,recentFullCount)`（:332）纯函数。
- **StoreEvent 结构性吃不下负 key**：从 key 反解 partition+时间窗（segment_store.go:195-201），负 key 解出垃圾 → 合成 ref 不能直接 StoreEvent。
- **可用原语**：`MemoryStore.DeleteEvent`（types.go:101，真删，InMemoryStore:219-231/FileSegmentStore:706-739）；`QueryEvents` 支持 EventTypes/OrderBy/Limit（types.go:135-143）；`SessionProjection.Replace/Append/GetAll`（projection.go:29/49/57）；wiring 点 build_agent.go ~427-447（NewTagentAgent 后、事件循环前）。
- **L3 "summarization" 是丢弃、非折叠**：`CompactL2ToL3`（compaction.go:519-546）对低价值类型仅 `Content=""`+`ToolCalls=nil`——与投影折叠是不同层的不同机制，**帮不上投影重建**。

**dev 补丁（将被移除）**：`persistSnapshotEvent`（context_manager.go:478-540）每轮超阈把三态快照存成正 key `context_compress` 事件（Metadata[SnapshotMetaKey]）、`ReplayProjectionHandler` 经 `Replace` 回灌活投影、接 spill 恢复 → recall 污染 + Replace-over-live + race + 写入常开 + **投影态游离于事实链之外（双真相源）**。

## Goals / Non-Goals

**Goals:**
- 让压缩折叠成为事实链的一等 **compaction 事件**，使投影重新是**完整事件日志的纯回放**（兑现 :1060/:1069 的旁路产物语义）。
- 冷启动回放逐字节重建投影 → prefix-cache 复用。
- 单一真相源（事实链），无游离 checkpoint；移除 dev 补丁子系统。
- **为 R2/R3/R4 立范与奠基**（resident-continuity-roadmap D2）：投影是「状态⊥执行器」轴上第一个完成事件溯源外置的状态层——「事实链旁路产物 + 事件回放重建进空态 + 状态由 cm 外置（执行器之外）」一般模式由 R2（RebuildTaskBoardFromWAL）/R3（ReattachResidentSessions 重构）沿用（**snapshot+tail 是 R1 特例**——因折叠压缩而有；R2 任务板无压缩语义、纯全量回放即可）；投影外置亦是 R4（SwappableExecutor）无损热换的前提之一。本变更不为此扩大范围（R2/R3 届时按各自子变更展开）。

**Non-Goals:**
- 不改压缩折叠算法（deterministicLevel/curateCards/buildRetainedRefs 不动）。
- **不另建独立 WAL**：KV LSM 事实链本就是 event log（不可变、snowflake 序、append-only、kv.wal+snapshot 持久化）；本变更补齐它缺的 compaction 事件，不新造物理存储层。
- 不做 R2/R3（任务接续）、R4（配置热更新）。
- 不承诺停机超 TTL 后的逐字节（retained 原文墓碑后降级摘要渲染）。

## Decisions

### D0：理想模型 = 事件溯源，KV LSM 事实链即 event log（不另建 WAL）

**选择**：认定 KV LSM 事实链（段存储，:1069「事实链只在 KV 里」）就是事件溯源的 event log；投影是它的**纯回放 fold**（:1060 同点投影）。R1 的缺口只是「折叠产物不在日志里」→ 补一条 **compaction 事件**即达终态。

**理由**：① 段存储已是不可变、snowflake 序、append-only、kv.wal+snapshot 持久化的事件日志——它**就是** WAL/真相源，另建独立 WAL 会与之重复（双日志）。② 用户「投影是日志的映射」原则由此兑现：投影 = fold(事实链)。③ **不留未来重构抉择**：日志已是唯一真相源、投影已是其回放，无「日后再迁到 WAL-主」的补丁债。**备选否决**：另建完整 WAL（重复现有事实链、大重构、超 R1）；KV-meta checkpoint（下述 D7）。

### D1：compaction 事件 = 事实链一等条目，载荷 = 折叠操作完整产物

**选择**：真折叠时 `StoreEvent` 一条 `context_compress_summary` 正 key 事件，载荷：① composed 综述正文（逐字节）② 综述 ref 身份（`EventKey=-minTs`/**`EventType=context_compress`**（非载体类型）/Timestamp/Role）③ **有序 retained-ref 列表**（保持投影交错顺序）：负 key `tool_chain` 合成 ref 存**完整身份 + EventSummary 逐字节**（store 无对应事件、不可 GetEvent、不可重算），正 key ref 只存 key（EventSummary/Content/ToolCalls/ToolID 经 GetEvent 逐字节复原，immutable）④ fullBoundary。

**理由（fresh-eyes 2026-09-12 修正承重假设）**：折叠产物含**负 key `tool_chain` 合成 ref**（:1057-1062 一律保留、主 spec task-skeleton-compression:198-200 强制），其 EventSummary 是合成文本、GetEvent 取不到 → 只存正 key 会丢它 → 工具会话重启 render 缺 `- 工具链:` 行、逐字节失败。故合成 ref 必须存全身份+EventSummary；正 key ref 仍 key-only（fresh-eyes ✅ GetEvent 逐字节复原）。**备选否决**：只记折叠边界（折叠非连续）；纯正 key 列表（丢 tool_chain）；把 tool_chain 拆成正 key 事件（污染 recall + 一次折叠多条 + 生命周期复杂——它是折叠派生物，随 compaction 事件携带最内聚）。

### D2：投影 = 事实链纯回放（正常路径精确成立；退化路径最终一致，边界文档化）

**选择**：确立一等不变量——**投影恒等于 fold(事实链)**（正常运行路径精确成立）。运行期：每次 `StoreEvent → projection.Add`（增量回放，:1060）；折叠：追加 compaction 事件（不改不变量，折叠也是日志的一条事实）。重启：回放事实链重建（D3）。二者是同一 fold 的增量式与全量式两个入口。

**不变量边界（fresh-eyes 二轮 E）**：两处退化路径——① `persistBusEvent` 现状在 StoreEvent 失败后仍 Append（context_manager.go:605-623，与 MemoryPlugin 的 stored-gate 不一致）→ 本变更对齐 stored-gate（失败不 Append，由 ReplaySpilled 双写恢复同点语义）；② spill 补写沿用旧 key（error_tracking.go:143-152，key 不可变）：事件在 compaction 前 spill、恢复在 compaction 后补写时，重启 tail（key>compaction）会切掉它——**文档化边界**（跨退化恢复的重启可能缺 spill 补写事件，最终一致非逐字节）。

**理由**：兑现「投影是旁路产物」（:1069），杜绝 dev 补丁的双真相源；不变量表述按 fresh-eyes 修正为分级（正常精确/退化最终一致），不过度承诺。

### D3：冷启动重建 = 回放「最新 compaction snapshot + 尾部」（含 tail-replay）

**选择**：`RebuildProjectionFromWAL` 仅启动期对**空投影**执行一次（非空则 WARN 不静默跳过，且**先于 spill 重放**）：
1. `QueryEvents([context_compress_summary], timestamp_desc, Limit 1)` 取最新 compaction 事件（= 事件溯源 snapshot）；
2. 按其 retained-ref 列表**交错顺序**复原：负 key 合成 ref（综述+tool_chain）用存的身份+EventSummary 逐字节（不 GetEvent）；正 key ref 用 `GetEvent(key)`（缺失/墓碑降级跳过+WARN）→ `Replace` 进空投影（综述 ref 首位）；
3. **尾部重放**（fresh-eyes 二轮 A/B 修正）：经**新增查询原语 `QueryOptions.MinEventKey`**（按 EventKey/写入序过滤，两 store 实现补 matchesQueryFilters）取 `EventKey > compaction key` 的事件，**分页取全**后**按 EventKey（写入序）升序**逐条 `projection.Add`。**三禁**（各有可达反例）：禁 StartTime 近似（双时间分叉 types.go D8——bus 滞留事件 Timestamp=入 bus 时刻而写入更晚，会被错切）；禁按 Timestamp 排序回放（运行期投影 append 序≡写入序 EventKey 序；task_settled 滞留 drain 反例：运行期尾序 [F,E] 而 Timestamp 序 [E,F]，颠倒即非逐字节）；禁截断（QueryEvents 默认 Limit=100 且 asc 排序截的是最新端）；
4. seed fullBoundary（携带值或 `anchorFullBoundary` 重算，含尾部）+ **回种 meditationKeys**（snapshot 复原的**与尾部回放的**每个 `agent_output` 且 `Metadata[trigger_source]==meditation` 的正 key ref 调 `MarkMeditationKey`）+ **reseed cm 的 priorSummaryKey**（= 最新 compaction key，使重启后首次折叠能正确 supersede）。

**冥想回种前置（fresh-eyes 二轮 C：一轮修法不成立）**：生产路径 `trigger_source` 现仅活在内存 StateDelta（session.go:244-252 由 RunFlow 注入），MemoryPlugin 落库只盖 agent_name+Attribution（memory_plugin.go:139-150）→ `Metadata[trigger_source]` 从不写入、回种条件恒 false。**前置任务**：RunFlow 的 Attribution 注入 `trigger_source`（或 MemoryPlugin 从 StateDelta 盖章），使冥想身份可从事实链判读——否则本条回种是空头支票。

**理由**：这是标准事件溯源「加载 snapshot + 回放其后事件」——snapshot=最新 compaction 事件、tail=其后原始事件。**tail-replay 是必需**（否则丢标记后的近期事件，非逐字节）。冷启动进空投影 `Replace` 安全（fresh-eyes：Replace-over-**live** 才破坏）；综述前置+交错顺序由 Replace 切片序保证（解 Append 尾追）；单 goroutine 启动期无 race。

### D4：滚动 supersede = 写前查 prior、写后 DeleteEvent，限定代际标记

**选择**：折叠点**先**查 prior（限定条件见下）→ `StoreEvent` 新 compaction 事件 → `DeleteEvent(prior)`。**顺序关键**：写后再查会取到自己 → 自删 → 事实链断。只留最新活 compaction（snapshot 语义）。

**代际标记（fresh-eyes 二轮 D）**：compaction 事件 SHALL 带 `Metadata[compaction]=v1`；supersede 查 prior 与 snapshot 选取**均限定带此标记者**——存量 legacy 固化物（`context_compress_summary`，TTL:-1 永活，主 spec 承诺「TTL 自然清退」）不带标记 → **不删不选**，防首次折叠误删长期记忆。

**compaction 事件自身 Timestamp = 写入时刻**（fresh-eyes 二轮 🟡1：若没综述 minTs，多条 compaction 的 Timestamp 全同，「取最新」全靠 (Timestamp,EventKey) tie-break 巧合——规定写入时刻则直接正确）。

**理由**：TTL 豁免不 supersede 则无限累积；DeleteEvent 真删且 QueryEvents/GetEvent 一致（fresh-eyes ✅）。

### D5：折叠原文不删，回放按 retained-ref 跳过

**选择**：被折叠原始事件**不 DeleteEvent**（冷存）；回放只复原 retained-ref 列表里的 ref，折叠原文自然不进投影（被综述代表），但 `GetEvent`/recall 票据仍可取。

**理由**：卡片行 `[evt_key]` 票据靠 GetEvent 回补原文（两段式 recall）；删原文则票据悬空。

### D6：综述 Recallable:true（用户裁决 2026-09-12）

**选择**：compaction 事件保持 `context_compress_summary` 的 `Recallable:true`（registry.go:206）。可召回内容 = 〔历史综述〕正文（放 EventSummary/Content），内部元数据（retained-ref 列表/fullBoundary）放 Metadata。

**理由**：综述正文是老历史的有用高层摘要，进 recall = 兑现「原文可忘、固化物长存」；区别于 dev 补丁把内部元数据当可召回内容的污染。

### D7：否决 KV-meta checkpoint（补丁/双真相源）

**选择**：**不**把投影态存成事实链之外的 KV-meta checkpoint（KVPut 到保留键）。

**理由**：KV-meta checkpoint 虽机械上更简单（覆写、无 supersede、不进 recall），但它把投影态**游离于事实链之外** → 制造「事实链 + checkpoint」双真相源，**违背 :1069「事实链只在 KV 里」+ :1060「投影是旁路产物」**，正是用户警惕的「打补丁给未来演进留重构抉择」。compaction 事件在事实链**内**（一等条目），投影是其回放 → 单一真相源、终态无债。

## Risks / Trade-offs

- **[逐字节受 TTL 限]** → retained 原文墓碑后 full 渲染降级摘要、前缀在该点后失效。最紧 thinking_plan 3d（registry.go:202）；热换装=分钟级 ≪ 3d 成立；spec 明确边界。
- **[tail-replay 与 compaction 边界衔接]** → tail = `EventKey > compaction key`（经新增 `QueryOptions.MinEventKey` 原语）；fresh-eyes 二轮 ✅ 主路径坐实（drain→Compress 固定顺序 + 同 partition snowflake 单调 + NTP 回拨 pin，types.go:216-231）；例外=spill 补写旧 key（已入 D2 边界）。**实现项**：MinEventKey 原语两 store 实现+测试；bus 滞留（task_settled 晚 drain）装置入回归 4.2/4.10。
- **[retained-ref 列表体积]** → 受投影预算有界；正 key 仅 int64，负 key 合成 ref 存 EventSummary（综述受 rollingNarrativeCapChars、tool_chain 单行），payload 可控。
- **[回放 GetEvent 缺失某 retained key]** → 墓碑/缺失时降级跳过该 ref + WARN，不阻断重建。
- **[与 dev ② 补丁并存期]** → 本变更移除补丁子系统；合并 dev 以本变更为准。

## Migration Plan

1. 折叠点发 compaction 事件（context_compressor.go 构造载荷 + context_manager.go StoreEvent + supersede）。
2. 新增 `RebuildProjectionFromWAL`（snapshot 复原 + tail-replay + fullBoundary/meditationKeys/priorKey reseed）+ build_agent 冷启动接线（先于 spill 重放）。
3. 移除 dev 补丁（persistSnapshotEvent/snapshot.go/Replace-over-live 分支）+ 同步删改连带测试。
4. 回归：compaction 事件写入 + 冷启动逐字节重建（含 tool_chain + 浓缩卡片 + tail）+ fail-before + supersede 不自删 + 折叠原文 recall 回补。
- **回滚**：compaction 事件写入可配置门控；重建仅启动期触发，未重启零影响；revert 即回 dev 行为。

## Open Questions

1. retained-ref 列表 + fullBoundary 存 Metadata 还是 Content(JSON)？倾向 Content JSON——fresh-eyes 二轮 ✅ 确认 `context_compress_summary` 无 LowValue 标志（registry.go:206），L3 压实不清空其 Content/Metadata（compaction.go:51-53/539-545），两处皆安全；实施时定。
2. ~~tail-replay 的 key 序~~ 已由 fresh-eyes 二轮收口：主路径成立（drain→Compress 固定顺序 + 同 partition snowflake 单调 + NTP 回拨 pin，types.go:216-231）；例外=spill 补写旧 key 场景，已入 D2 文档化边界。新增待坐实项=QueryOptions.MinEventKey 原语的实现与测试。
