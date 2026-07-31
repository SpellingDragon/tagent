# segment-query-recency

> 范围说明：change 名沿用最初的查询召回视角，但经全面核查后实际范围为**记忆子系统的时间语义层 + 遗忘层通电**（为避免归档记录抖动，保留原名）。

## Why

生产实证（wechat-bot 2026-07-31）：冥想触发的 recall 反复返回 `found 0 events` 或只返回数天前的旧事件，5 轮迭代耗尽后 flow_error。核查从查询路径出发，最终对 memory 子系统做了全量数据流建模（含不经函数调用、靠 KV 键名/内存态/后台工人隐式成立的连接），确认持久化本身健康（单一共享 store、WAL 活跃、压实零重复：2868 条目 = 2868 唯一 EventKey），但发现**一组同根缺陷：凡是拿"段的标称窗口"或"KV 键结构"做语义推理的地方，都建立在错误前提上**。

已实证的缺陷（均有生产数据或全历史 git 证据）：

1. **查询召回三连缺陷**（已在本 change 前期修复）：排序前截断 × 旧→新扫描（`memory_recent` 语义反转为"取最旧"）、秒/毫秒剪枝错配（带 `since` 恒返回 0）、剪枝跨度写死 1h。
2. **段名不是时间单位**：`CompactL1ToL2` 把分区内全部 L1 窗合并进以最早窗命名的单段。生产 L2 段标称 `[07-25 08:00, 07-26 08:00)`、实际含 `07-26 06:48 → 07-28 17:18`（溢出 57.3h，1650 条 = 全库 58%）。依赖标称边界的剪枝使**时间范围查询静默丢失 62% 的在范围事件**（实测 682 vs 真值 1791）。`SegmentMeta.MinTime/MaxTime` 字段存在却从未被写入或读取。
3. **TTL 回收全程空转**：`ParseKey` 对 `evt` 键不填 `EventKey`（该字段只对 idx/tomb 键有意义），而 `checkTTL`/`evictOldest` 以 `EventKey == 0 → continue` 为跳过条件 → 吞掉每个事件。生产 **0 个墓碑键**、**1034 个超期 `thinking_plan`**（TTL=3 天）仍在库。注意：`event-lifecycle` 规格早已明文要求"从事件 JSON 读 event_key 而非 KV 键"，故这是**实现对既有正确规格的回归**，非规格缺口。
4. **seq 槽位被覆写，已吞噬事件**：`Init()` 是空实现、`PartitionState` 零值起步，重启后同窗 seq 从 0 重计覆写旧槽位。生产 **idx=2936 vs evt=2935，恰 1 个 dangling idx** 指向不存在的槽位——完整吞噬链路（覆写 → 压实删窗 → idx 成孤儿）已真实发生一次，直接违反"事件不可变"零号承诺。
5. **死代码与接口地雷**：`StoreEvents` 批量路径不写 meta（meta 前缀扫描是窗口发现的唯一入口 → 批量写入的事件对查询不可见），且 memory 包外无任何生产调用方；`ensureSegmentMeta` 零调用。
6. **规格债**：`harden-event-storage-for-scale` 归档时 **0/80 任务全未勾**，但其 delta 已同步进主 specs，形成"规格说有、代码没有"的空头契约——`{pid}:ttl_cursor`、`global:active_partitions` bitmap、`{pid}:cursor` 在全历史 `git log -S` 中零命中。受影响：`ttl-cursor-scan` 全部 3 条、`event-lifecycle` 2 条游标条款、`event-segment-store` 中 seqCounter/StoreEvents/resolvePartitions 相关条款。
7. **双时间源无明文契约**：`EventKey` 内嵌写入时刻（`time.Now()`）、`FullEvent.Timestamp` 是事件产生时刻，异步场景可分叉；两者各自驱动不同机制（排序 vs 段放置）却无"谁是权威"的声明。

## What Changes

**时间语义层（查询正确性）**

- QueryEvents 召回正确性：`timestamp_desc` 按时间窗新→旧遍历、整窗粒度收集、跨窗按 EventKey 去重且方向无关地保留更高 layer 版本；全序键固定 `(Timestamp, EventKey)`；`StartTime/EndTime` 毫秒契约（已完成）。
- **早停判据改用实际事件时间戳**：以已收集事件的真实时间戳极值（而非窗口标称边界）作为比较基准，并用严格不等式——修正原实现在跨层窗口重叠下不成立的安全性论证。
- **段边界可信性**：压实写入 `SegmentMeta.MinTime/MaxTime`（合并时已按时间排序，取首尾即得，零额外扫描）；剪枝与早停按三档取真实上界——`MaxTime>0` 用它；`layer≤1` 无 MaxTime 用标称 `start+1h`（结构性真实）；`layer≥2` 无 MaxTime（历史遗留段）视为 +∞ **不剪不跳**（保守，随下一轮压实自愈）。
- **时间真相源契约**：明文声明 `FullEvent.Timestamp` 为唯一时间轴（排序/过滤/TTL/卡片时间线只认它），EventKey 内嵌时间仅用于段放置与同毫秒决胜；补不变量测试（构造双时间分叉，断言结果与"只按 Timestamp"参照一致）。

**遗忘层（通电）**

- **TTL/容量回收修复并直接接线**（不做影子模式）：从事件 JSON 读 `event_key`（兑现 `event-lifecycle` 既有规格）；`LifecycleConfig` 提到 YAML（`global_ttl_days`/`type_ttl`/`check_interval`/`max_events_per_partition`），替换 `tagent.go` 两处硬编码 `DefaultLifecycleConfig()`——当前连关都关不掉。
- `eventCount` 在 `DeleteEvent` 与压实 `deleteSegments` 时递减（使其在进程生命期内准确；跨重启不恢复的限制如实写进规格）。

**写入侧不变量**

- **seqCounter 轻量恢复**：写入某窗口首事件前（或 `Init` 时）按 `{pid}:evt:{窗}:` 前缀扫已有槽位，取 `max(seq)+1` 续写，杜绝覆写。不实现规格承诺的 bitmap+cursor 全量方案（80 项体量、且引入新的可腐化持久状态）——扫描仅针对当前活跃窗口，成本可忽略。
- **死代码清理**：删除 `ensureSegmentMeta`；从 `MemoryStore` 接口移除 `StoreEvents`（含两个实现与测试引用）——遵循本仓库"删除死代码留存"纪律。

**规格债处置（未实现即修订文档）**

- `ttl-cursor-scan`：3 条要求全部移除（能力从未实现，撤回空头承诺）。
- `event-lifecycle`：2 条游标要求改写为如实描述（当前为周期性全扫；有界成本扫描列为已知缺口）。
- `event-segment-store`：seqCounter 要求改写为本 change 实现的轻量语义；`StoreEvents` 两条随方法移除；`resolvePartitions` 改写为"无分区参数返回空 = 默认隔离"（原条款与 wiki §13 的隔离设计相矛盾）。

不改：键 schema、压实流程与调度、`GetEvent`/idx 精确路径、RelationStore、recall 工具层 API 签名、压实按日/周分组（后续）、`QueryEvents` 接口级 `truncated` 标记（后续）。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `event-segment-store`：查询召回正确性与全序确定性；段边界真实性（min/max 写入与三档回退消费）；时间真相源单一；seq 槽位与身份一一对应（轻量恢复）；移除 StoreEvents 相关条款；resolvePartitions 隔离语义如实化；eventCount 递减与其限制。
- `recall-protocol`：查询类召回结果的诚实截断提示。
- `event-lifecycle`：TTL 配置经 YAML 注入；游标条款如实降级。
- `ttl-cursor-scan`：能力撤回（从未实现）。

## Impact

- **代码**：`memory/segment_store.go`（查询重写、seq 恢复、eventCount 递减、删死代码）、`memory/compaction.go`（写 min/max、deleteSegments 递减）、`memory/in_memory_store.go`（全序 tie-break、移除 StoreEvents）、`memory/types.go`（毫秒契约、接口收敛）、`memory/lifecycle.go`（event_key 解析修正 + 配置注入）、`memory/key_schema.go`（注释）、`tagent.go` + `config.go`（lifecycle YAML 配置）、`tool/recall/`（截断提示）。
- **测试**：查询矩阵（已有 9 项）+ 超宽段剪枝/早停、legacy 无 MaxTime 段、seq 恢复防覆写、TTL 真的标墓碑与固化物豁免、双时间分叉不变量、KVScan 有序性。
- **行为**：`memory_recent` 恢复"取最近"；带 since 查询恢复正常；时间范围查询不再丢 62%；TTL 首次真实生效（生产将在首个周期标记约 1034 个超期低价值事件——这条链路（墓碑→查询过滤→召回 miss→压实 finalize）首次真实运行，需上线观察）。
- **风险面**：TTL 通电后"卡片票据指向已过期原文"的既定折损（`getEffectiveTTL` 固化物豁免为主要防线）首次可被触发；两个 KV 后端的 KVScan 有序性依赖已加测试守护。
