# segment-query-recency — 技术设计

## Context

`FileSegmentStore` 按时间窗分段存储事件（`{pid}:evt:{window_ts}:{seq}` + `{pid}:meta:{window_ts}`），`QueryEvents` 经 meta 扫描发现段、逐段扫事件、过滤、排序、截断。生产事故（2026-07-31 冥想 recall 失灵）暴露该路径三个确定性缺陷（B1 排序前截断、B2 剪枝单位错配、B3 剪枝跨度写死），探索结论详见 proposal。

关键地形事实（探索已核实）：

| 事实 | 影响 |
|------|------|
| meta 键 KVScan 字符串序 = 窗口旧→新（窗口为 10 位 epoch 秒，字符串序=数字序） | 逆序遍历可在 Go 侧对窗口列表 reverse，不依赖 KV 后端逆序扫描 |
| 窗内 `seq` 无零填充（字符串序 `0,1,10,2…`） | 窗内事件非时间序——早停必须以"整窗"为粒度 |
| 压实（L1→L2、L2→L3）复用普通键格式，layer 存于 meta JSON；先写目标层、后删源层 | 崩溃窗口内同一事件可能双层并存 → 查询需按 EventKey 去重；meta 扫描的 value 可直接读 layer |
| `SegmentMeta.min_time/max_time` 从未维护（生产全 0） | 不可用作剪枝依据；本 change 不启用该字段 |
| `InMemoryStore.QueryEvents` 语义正确（全收集→排序→offset/limit） | 作为一致性测试的参照实现 |
| `GetEvent`/`memory_recall items` 走 `{pid}:idx:` 精确索引 | 不受本 change 影响 |
| 调用方：recall `memory_query`/`memory_recent`、`memory_recall` query 模式、knowledge 子工具 | 全部只经 `QueryEvents` 接口，工具层 API 不动 |

## Goals / Non-Goals

**Goals:**
- `timestamp_desc` 查询在任何 limit 下最新匹配优先且不丢失（B1）；`memory_recent` 恢复"取最近"语义。
- 毫秒级 `StartTime/EndTime` 的时间过滤在窗口剪枝与事件过滤两级行为一致（B2）。
- L2/L3 段按真实跨度参与剪枝（B3）。
- InMemoryStore 与 FileSegmentStore 查询语义一致，共享测试守护。

**Non-Goals:**
- 不改键 schema、压实流程、生命周期/TTL、RelationStore。
- 不维护 `SegmentMeta.min_time/max_time`（更精细剪枝留后续）。
- 不为 compaction 崩溃窗口的双层并存做存储侧修复（查询侧去重即足够；存储侧收敛由压实下一轮自然完成）。
- 不引入向量检索或改变 keyword 匹配语义。

## Decisions

### D0：行为契约（五条，后续决策均从此推导）

召回与压缩是一对互补机制，**时间箭头必须同向**：压缩丢旧留新，召回必须新先于旧——否则压缩保下的最新记忆恰好是召回拿不到的（B1 事故的深层结构）。由此导出：

| # | 契约 | 对应决策 |
|---|------|---------|
| 1 | **声明式语义**：结果 ≡ 全集过滤 → 全序排序 → offset/limit；分段/剪枝/早停/后端全是优化，不得改变可观察结果 | D1/D5 |
| 2 | **全序确定性**：排序键为 `(timestamp, EventKey)`，同毫秒事件以 EventKey 决胜——任意实现、任意两次查询逐位一致 | D5 |
| 3 | **最新优先**：desc 下截断只牺牲最旧、永不牺牲最新 | D1 |
| 4 | **诚实截断**：limit 截断时调用方（LLM）能知道"还有更多"，不把"返回 10 条"误读成"只有 10 条" | D7 |
| 5 | **身份唯一**：EventKey 是事件身份，存储生命周期造成的物理重复对消费者不可见；重复时确定性地返回更高层版本 | D3 |

明确不做的边界：keyword 全史查询在匹配稀疏时仍需扫全库——这是无倒排索引的关键词检索固有成本，不违反素材律（它约束压缩路径）；设计预期是"取最近类查询 O(新窗口)，全史搜索诚实地花全史的钱"，本 change 不引入索引。

### D1：窗口按 OrderBy 方向遍历，整窗粒度早停

`scanPartition` 重构为两阶段：

1. **窗口发现**：扫 `{pid}:meta:` 前缀得到 `(windowTS, layer)` 列表（layer 直接解析 pair.Value，零额外 KVGet），按 windowTS 排序；`timestamp_desc` 逆序遍历，`timestamp_asc`（含默认）正序。
2. **逐窗收集**：每个窗口**完整**扫描（窗内 seq 字符串序非时间序，不可窗内早停），过滤后并入结果；当 `len(matched) >= offset + limit` **且**当前窗口已收完时停止遍历后续窗口。

正确性论证：窗口间时间范围互不重叠（同层内），desc 遍历下"已收满 offset+limit 且下一窗口整体更旧"时，后续窗口不可能贡献进入前 limit 名次的事件——早停安全。

> **前提修正（见 D10）**：上述论证最初以“窗口标称边界”作为比较基准，而标称上界对压实产生的 L2/L3 段**不成立**（D9）。D10 把已收集侧的基准改为实际事件时间戳，候选窗口侧改为真实边界，该论证方告成立。跨层窗口可能时间重叠（L2 日窗 vs 未删净的 L1 时窗），由 D3 去重兜底；早停判定需以**窗口起点时间**为界：仅当下一待扫窗口的 `[start,end)` 整体早于当前已收集第 `offset+limit` 名的时间戳时才可停——实现上取保守简化：**收满后再多扫完所有与已收窗口时间区间重叠的窗口**（重叠仅发生在跨层，数量有限）。

*为什么不全收集再排序*：功能等价，但"取最近"类高频查询会退化为全库扫描；分层窗口天然是时间索引，应当利用。

### D2：时间单位契约 = 毫秒，剪枝在窗口边界换算

- `QueryOptions.StartTime/EndTime` 注释明确为 Unix 毫秒（与 `FullEvent.Timestamp`、recall 工具文档一致）。
- 窗口剪枝：`windowStartMs = windowTS * 1000`，`windowEndMs = (windowTS + spanSec) * 1000`；`EndTime > 0 && windowStartMs > EndTime` 或 `StartTime > 0 && windowEndMs < StartTime` 时跳过。
- 事件级过滤（`matchesQueryFilters`）已是毫秒语义，不动。

*为什么不改成秒*：事件级过滤与所有工具层契约都是毫秒，改秒的破坏面更大；错的是剪枝这一处。

### D3：跨窗按 EventKey 去重，重复时保留更高 layer 版本（方向无关）

`scanPartition` 收集时维护 `seen map[int64]已选版本层级`（按 EventKey）。重复时的保留规则 SHALL 与遍历方向无关：**保留来自更高 layer（更新窗口）的版本**——压实写入目标层即宣告 canonical 状态（crash-safe 顺序保证 L3 存在 ⇒ 压实已决策），返回摘要化版本正是压实的语义意图。

*为什么不是"保留先遇到的"*：先遇到谁取决于遍历方向（desc 先遇新层、asc 先遇旧层）——同一数据不同排序方向会返回不同内容版本，违反契约 1（声明式语义）。去重范围限单次查询内，无跨查询状态。

### D4：剪枝跨度按 layer 取值

| layer | 跨度 |
|-------|------|
| 0 / 1（及 meta 缺 layer 字段的历史段） | 3600s |
| 2 | 86400s |
| 3 | 604800s |

layer 来自窗口发现阶段解析的 meta value；解析失败回退 3600s（保守：宁可少剪、多扫一窗）。

> **修正（见 D9）**：按 layer 取跨度只修正了“标称跨度写死 1h”，仍假定“标称跨度 = 真实覆盖”。实证该假定对压实产生的段不成立（生产 L2 段溢出 57.3h），故 D9 将真实边界上升为主要依据，本决策降为“无 MaxTime 且 layer≤1”时的回退途径。

### D5：双实现一致性用共享测试矩阵守护，全序键 = (timestamp, EventKey)

两实现的排序 SHALL 使用全序键 `(timestamp, EventKey)`（desc 即两键同时逆序）——同毫秒事件（并行工具调用常见）无 tie-break 时 `sort.Slice` 非稳定排序会让一致性断言间歇性失败，也违反契约 2。InMemoryStore 同步补齐 tie-break（行为级对齐，非仅 FileSegmentStore 修复）。

新增 store-agnostic 的查询一致性测试：同一事件集写入 InMemoryStore 与 FileSegmentStore（LocalFileKV 后端），对一组 `QueryOptions` 矩阵（desc/asc × limit 边界 × 毫秒 since/until × keyword × 多窗口分布 × **同毫秒 tie**）断言两者返回的 EventKey 序列逐位一致。FileSegmentStore 额外覆盖：L2 段剪枝、跨层重复去重（含 asc 方向保留高层版本）、offset+early-stop 组合。

*RustVikingClient 后端*：其 KVScan 由 RocksDB 保证字典序（与 LocalFileKV 的 Go 排序一致），实现时以现有 rustviking client 测试确认有序性断言即可，不在一致性矩阵内跑真实 rustviking（外部依赖）。

### D6：key_schema.go 文档漂移修正

头注释删除从未实现的 `{pid}:L{layer}:evt:` / `{pid}:L{layer}:meta:` 键格式描述，改为如实说明"压实复用普通键格式，层信息在 SegmentMeta.Layer"。纯注释改动。

### D7：诚实截断——工具层提示先行，接口改造留后续

早停机制免费产出"还有更多"信号，但 `QueryEvents` 返回 `[]EventReference` 无处承载。本 change 采方案 (b)：**recall 工具层（memory_query / memory_recent / memory_recall query 模式）在 `len(results) == limit` 时于返回 message 中注明**"已达 limit，更旧的匹配未返回；可缩小时间范围或增大 limit"——零接口改动，防止 LLM 把截断误读为全量（本次冥想事故的行为模式）。启发式的局限（恰好等于 limit 时误报一次"可能还有"）无害。

方案 (a)（QueryEvents 返回结构化 `truncated` 标记，波及 6 个调用方）记录为后续接口演进方向，本 change 不做。

### D8：时间真相源契约——Timestamp 是唯一时间轴

存在两个时间：`EventKey` 内嵌**写入时刻**（`NewSnowflakeEventKey(pid, 0)` → `time.Now()`），`FullEvent.Timestamp` 是**事件产生时刻**（`extractTimestamp(evt)` / `evt.Timestamp`）。异步事件（task_settled 回写、批量投递）下二者可分叉。

契约：**`FullEvent.Timestamp` 是唯一语义时间轴**——排序、时间过滤、TTL 年龄、卡片时间线均只认它；`EventKey` 内嵌时间仅用于**段放置**（`WindowTimestamp(TimestampFromEventKey(key))`）与**同毫秒全序决胜**。两者均在写入处一次派生，且**无任何判定同时依赖两个时间**，因此分叉无害（仅影响“事件落在哪个段”这个放置问题，而放置不参与语义）。

> **前提修正（实现期间发现，见 D14）**：“无任何判定同时依赖两个时间”不成立——窗口剪枝比较的是“段边界”（由 key 内嵌写入时间推导）与“查询范围”（事件时间轴），分叉时剪枝错误。解决不是改放置依据（LSM 的顺序追加要求放置按写入时间，正确），而是让剪枝只读**封口时写入的事件时间边界**（D14）。

落地：`memory/types.go` 注释 + wiki §16.5 契约 6；补不变量测试（构造 key 时间与 Timestamp 分叉的事件，断言查询结果与“只按 Timestamp 排序过滤”的参照完全一致）。

### D9：段边界可信性——真实 min/max 与三档回退

实证：`CompactL1ToL2` 把分区内全部 L1 窗合并进以 `computeDailyWindow(windowTSs[0])` 命名的单段，故**标称上界不可信**（生产溢出 57.3h）。一个关键的不对称：`computeDailyWindow(w0) ≤ w0 ≤ 所有事件 ts`，所以**标称下界仍是有效下界**，只有上界失真。

> **被 D14 取代**：本决策的“三档回退”仍让第 2 档回退到标称边界，而标称边界是写入时间的界而非事件时间的界（D8 修正）。D14 用 LSM 的键范围元数据模型替代三档逻辑：封口段写入真实边界，无边界一律不剪不跳。下文保留为历史记录。

- 写入：`CompactL1ToL2` / `CompactL2ToL3` 写 `SegmentMeta.MinTime/MaxTime`——`mergeEvents` 已按时间升序，filter 后取首尾即得，**零额外扫描**。`SealCurrent` **不写**：L0/L1 窗口由事件自身 ts 推导，标称边界结构性真实，写它反而多一次 KVScan。
- 消费（真实上界三档）：`MaxTime>0` → 用它；`layer≤1` 且无 MaxTime → 标称 `start+1h`；`layer≥2` 且无 MaxTime（历史段）→ **视为 +∞：不剪不跳**。
- 存量段：不做惰性修复（读路径写 meta 会与压实竞争——压实刚删源段、修复回写其 meta 就造出“有 meta 无事件”的幽灵段）；靠下一轮压实自然痊愈。

### D10：早停判据改用实际事件时间戳 + 严格不等式

已收集事件的真实时间戳就在手里，无需用标称值代理：

- 将 `minScannedStartMs`/`maxScannedEndMs`（窗口标称边界）换为 `minCollectedTs`/`maxCollectedTs`（已收集事件的 Timestamp 极值）。这同时修掉一个隐蔽问题：**asc 方向也不可靠**——它比较的 `maxScannedEndMs` 同样来自失真的标称上界。
- 判据：desc 仅当 `候选窗真实上界 < minCollectedTs` 时跳过；asc 仅当 `候选窗真实下界 > maxCollectedTs` 时跳过。
- **必须严格不等式**：原实现的 `<=` 靠“半开窗口 end 排他”侥幸成立；改成闭区间的 `MaxTime` 后，`<=` 会让同毫秒事件的 EventKey 决胜被错误跳过。

### D11：TTL/容量回收直接接线 + YAML 配置化

根因：`ParseKey` 对 `evt` 键只填 WindowTS/Seq（EventKey 只对 idx/tomb 键有意义），而 `checkTTL`/`evictOldest` 以 `EventKey == 0 → continue` 为跳过条件 → 全程空转。

- 修正：从事件 JSON 读 `event_key`（与已有的 `timestamp` 解析合并为一次 unmarshal）。**这正是 `event-lifecycle` 规格早已明文要求的行为**（含 Scenario）——属实现回归，无需改规格。
- **直接接线，不做影子模式**（用户决策）；但保留可配置能力：`LifecycleConfig` 经 YAML 注入（`global_ttl_days` / `type_ttl` / `check_interval` / `max_events_per_partition`），替掉 `tagent.go` 两处硬编码 `DefaultLifecycleConfig()`——当前连关都关不掉。
- 上线预期：首个检查周期会标记约 1034 个超期低价值事件（thinking_plan TTL=3d）；墓碑→查询过滤→召回 miss→压实 finalize 这条链路**首次真实运行**，固化物豁免（`getEffectiveTTL` 负值语义）是主要防线。
- `eventCount` 在 `DeleteEvent` 与压实 `deleteSegments` 时递减（当前只有 `++`）；跨重启不恢复的局限如实写进规格。

### D12：seqCounter 轻量恢复（而非规格的 bitmap+cursor）

实证：`Init()` 空实现 + `PartitionState` 零值起步 → 重启后同窗 seq 从 0 覆写旧槽位；生产 `idx=2936 vs evt=2935` 与 1 个 dangling idx 证实已吞噬 1 个事件。

选择：当内存态认为“新窗口”（`state.currentWindow != windowTS`）时，先扫 `{pid}:evt:{窗}:` 前缀取已存在的最大 seq，seqCounter 从 `max+1` 续写（空窗口则 0）。

*为何不做规格的 bitmap+cursor*：那是 80 项任务体量，且引入新的可腐化持久状态（cursor 本身要恢复、要与压实改窗同步）。轻量方案的成本是“每个新窗口一次前缀扫描”（每小时级 1 次、扫描域是单窗），可忽略；且**不新增任何需要恢复的状态**。

### D13：死代码清理——删 `StoreEvents` 而非补写 meta

`StoreEvents` 批量路径不写 meta（使其写入的事件对窗口发现不可见），且 memory 包外零生产调用方；`ensureSegmentMeta` 零调用。按本仓库 wiki 自身的“历史机制被替代时删除死代码留存”纪律，选择**删除**而非修好：从 `MemoryStore` 接口移除 `StoreEvents`（含两个实现与测试引用）、删 `ensureSegmentMeta`。影响面全在 memory 包内。

相应地，`event-segment-store` 中两条 StoreEvents 要求随之移除（它们本来也从未实现：既未按 (pid,windowTS) 分组，也未原子写 meta/cursor/bitmap）。

### D14：LSM 语义对齐——层级=写入新近度，键范围=元数据，活跃段=memtable

实现期间用户明确了设计意图：记忆按 **LSM 树**管理。这解决了 D8 修正暴露的矛盾，也给出了比 D9 三档回退更简洁的模型。LSM 里有两个正交的东西，对应到本系统：

| LSM 概念 | 含义 | 本系统对应 |
|---|---|---|
| 层级 / 文件名 | 写入新近度、压实代数（与逻辑键无关） | `layer` + 段窗口名（按写入时间） |
| SSTable 元数据的 min/max key | 该文件能答什么查询（剪枝唯一依据） | `SegmentMeta.MinTime/MaxTime` |
| memtable | 键范围未定 → 永远参与查询、不剪枝 | 未封口的活跃段（`Sealed=false`） |

由此的结论：

- **段放置保持按写入时间（不变，且是正确的）**：LSM 的立命之本是顺序追加、写放大最小；误导期间考虑过的“改按事件时间放置”会破坏该特性并让迟到事件复活已压实窗口——作废。
- **封口即 flush**：`SealCurrent` 在封口时写入该段的 `MinTime/MaxTime`（封口后段不可变，边界从此稳定）；压实重算并写入（已有）。成本：每分区每小时一次单窗扫描。
- **剪枝/早停只读键范围**：段有 bounds → 按真实事件时间判定；未封口或无 bounds（历史段）→ **永远扫描**（memtable 语义，不剪不跳）。`windowSpanSec` 退出时间推理（仅作为命名/压实概念，若无其他用处则删除）。
- **热度不是层级**：用户提出“频繁激活的记忆应视为 L0”。这不是 LSM（LSM 从不因读而升层），且与本 change 的去重规则冲突（保留更高层版本——压实写目标层即更新的写入；热度提升会让规则反转），并破坏“层级单调变旧”这个压实不变量。热度的正确归属是视图层（投影/卡片/★ 策选，已存在）与策略输入。作为后续方向记录：压实降细节时豁免高召回事件（recall_count 阈值）、TTL 类型维度扩热度维度、LRU/block cache 已涵盖读速度。

## 零基 Code Review 修复（D16：第二轮审查后的加固）

变更扩大后的零基审查发现 1 Blocker + 4 Major + 2 预存地雷，全部修复并附回归测试（tasks 第 10 组）：

- **B1（配置语义）**：负数 `GlobalTTLDays` 是 TTL 总开关，必须优于类型表且不得被默认值钳制。
- **M1（边界语义）**：`MinTime` 是封口时记录的**事件时间**真实下界，与按**写入时间**命名的窗口起点在异步回写下发散——信任 MinTime，不做 max()。
- **M2（写入顺序）**：身份检查（碰撞守卫）必须先于任何内容写入——段扫描不经 idx，被拒写入不得留下幽灵。
- **M3（降级方向）**：seq 恢复失败时"降级为 0"会复现它要防的覆写——正确方向是 fail loud，让上游幂等重试。
- **M4（计数语义）**：`eventCount` 是**逻辑存活**计数——墓碑标记即递减（而非等物理删除），否则容量淘汰每周期多杀一批活事件。
- **P1/P2（预存地雷，顺带排除）**：压实目标窗口与源窗口撞名时先写后删会自删（日/周对齐概率 1/24、1/7）；活跃段不得作为压实源。
- **m5（半态闭环）**：写入已封口窗口 → 降级回 memtable 语义，边界重新变为"不可证"。

## 已知规格债（本 change 如实记录）

归档考古发现：`openspec/changes/archive/2026-06-20-harden-event-storage-for-scale` 归档时 **tasks 0/80 全未勾**，但其 delta 已同步进主 specs，形成“规格说有、代码没有”的空头契约。全历史 `git log -S` 证实 `ttl_cursor` / `global:active_partitions` / `{pid}:cursor` **从未出现在代码中**。

| 规格条目 | 实现现状 | 本 change 处置 |
|---|---|---|
| `ttl-cursor-scan` 全部 3 条（游标扫描、O(过期窗口)、重启恢复） | 从未实现 | **移除**（能力撤回）；有界成本扫描归入 wiki 已知缺口 |
| `event-lifecycle`：TTL 经游标推进 / 容量淘汰避免全分区扫描 | 从未实现（当前每周期全扫） | **如实改写**为当前行为契约 |
| `event-segment-store`：seqCounter 经 bitmap+cursor 恢复 | 从未实现 | **改写**为 D12 的轻量恢复语义 |
| `event-segment-store`：StoreEvents 分组 seq / 原子写 meta+cursor+bitmap | 从未实现 | **移除**（方法删除，D13） |
| `event-segment-store`：resolvePartitions 默认返回所有活跃分区 | 未实现（返回 nil），且**与 wiki §13 的默认隔离设计相矛盾** | **改写**为隔离语义（无分区参数 → 返回空，调用方必须显式指定） |
| `event-segment-store`：eventCount 删除后准确 | 未实现（只有 `++`） | **部分兑现**：补递减（D11），并如实声明“进程生命期内准确、跨重启不恢复” |

流程教训（写入 wiki 已知缺口）：归档应加实现核对——tasks 未完成的 change 不得将 delta 同步进主 specs。

## Risks / Trade-offs

- **[早停保守扫描的成本]** 跨层重叠时多扫有限几个窗口 → 与正确性相比可接受；稳态（压实源已删净）无重叠、零额外成本。
- **[desc 下去重保留目标层版本]** L3 摘要化后 Content 被清空的低价值事件，若与源层短暂并存，查询返回的是摘要化版本（且与遍历方向无关，D3）→ 这正是压实的语义意图，非折损。
- **[工具层截断提示是启发式]** `len==limit` 恰好等于全量时误报一次"可能还有"→ 引导 LLM 多一次缩范围查询，无害；精确方案（接口 truncated 标记）留后续。
- **[历史 meta 无 layer 字段]** 老段 meta 若缺 `layer`（JSON 零值 0）→ 跨度回退 1h，与现状一致，无回归。
- **[行为变化对既有依赖]** 有调用方若无意依赖了"返回最旧事件"的错误行为 → 排查过全部 6 个调用方，语义均期望"最新优先"，无此依赖。

## Migration Plan

1. 纯查询路径修复，存储格式零改动——无迁移、可直接回滚（revert 单文件）。
2. 上线观察点：冥想回顾日志中 recall 返回事件的时间跨度应覆盖最近会话；`memory_recent` 返回时间应接近当前。

## Open Questions

- 压实按日/周分组（让“日段”名副其实）：真实边界到位后仅影响剪枝粒度（性能），不影响正确性，留后续。
- 热度作为策略输入（非层级）：压实降细节时豁免高召回事件（recall_count 阈值）、TTL 类型维度扩热度维度（D14 记录的接法）。
- 窗内 seq 零填充（如 `%06d`）可让窗内扫描有序、支持窗内早停——涉及键格式迁移，收益有限，暂不做。
- 诚实截断的精确方案（QueryEvents 接口返回 `truncated` 标记，波及 6 调用方）：方向已定（D7 记录），留后续接口演进。
