# event-segment-store Delta

## ADDED Requirements

### Requirement: QueryEvents 召回正确性（最新优先不丢失）

`FileSegmentStore.QueryEvents` 在 `OrderBy=timestamp_desc` 下 SHALL 按时间窗**新→旧**遍历段（`timestamp_asc` 与默认为旧→新），SHALL 以整窗为粒度收集（窗内 seq 为字符串序、非时间序，不得窗内早停），且仅当已收集数达到 `offset + limit` 且后续窗口不可能贡献更新事件时方可早停——limit SHALL NOT 导致更新时间窗中的匹配事件被排除。

#### Scenario: 匹配数超过 limit 时最新事件优先

- **GIVEN** 三个时间窗（旧、中、新）各含 10 条匹配 keyword 的事件，limit=10，OrderBy=timestamp_desc
- **WHEN** 执行 QueryEvents
- **THEN** 返回的 10 条 SHALL 全部来自最新时间窗
- **AND** SHALL NOT 返回旧窗口事件（当前缺陷：只返回最旧窗口的 10 条）

#### Scenario: 取最近语义（无 keyword）

- **GIVEN** 库中事件跨越多天，limit=20，OrderBy=timestamp_desc，无 keyword
- **WHEN** 执行 QueryEvents（memory_recent 场景）
- **THEN** 返回的 20 条 SHALL 是全库时间戳最新的 20 条事件

#### Scenario: offset 与早停组合

- **GIVEN** 多窗口匹配事件共 30 条，offset=10、limit=10，OrderBy=timestamp_desc
- **WHEN** 执行 QueryEvents
- **THEN** 返回按时间降序的第 11–20 条，与全收集后排序截断的结果一致

### Requirement: 时间过滤毫秒契约与窗口剪枝换算

`QueryOptions.StartTime/EndTime` 的单位 SHALL 为 Unix 毫秒（与 `FullEvent.Timestamp` 及 recall 工具层文档一致）。窗口级时间剪枝 SHALL 将窗口秒级边界换算为毫秒后比较，SHALL NOT 以秒级窗口边界与毫秒级查询参数直接比较。

#### Scenario: 毫秒 since 查询不再恒空

- **GIVEN** 库中存在最近 1 小时内的事件，StartTime = 1 小时前的 Unix 毫秒
- **WHEN** 执行 QueryEvents
- **THEN** SHALL 返回该时段的事件（当前缺陷：秒/毫秒错配剪光所有段，恒返回 0）

#### Scenario: 剪枝与事件级过滤一致

- **GIVEN** 同一组 StartTime/EndTime（毫秒）
- **WHEN** 分别经窗口剪枝与事件级过滤
- **THEN** 窗口剪枝 SHALL NOT 剪掉任何包含满足事件级过滤事件的段

### Requirement: 窗口剪枝跨度按段层级取值

窗口级时间剪枝 SHALL 按段的 `SegmentMeta.Layer` 取窗口跨度：L0/L1 为 3600 秒、L2 为 86400 秒、L3 为 604800 秒；layer 缺失或解析失败时 SHALL 回退 3600 秒（保守少剪）。layer SHALL 从 meta 扫描已返回的 value 解析，SHALL NOT 为剪枝引入额外的每段 KVGet。

#### Scenario: L2 日段不被误剪

- **GIVEN** 一个 L2 段（窗口按天对齐），查询时间范围落在该天的下午
- **WHEN** 执行带 StartTime/EndTime 的 QueryEvents
- **THEN** 该 L2 段 SHALL 参与扫描（当前缺陷：按 1 小时跨度剪枝将其误剪）

### Requirement: 跨窗查询按 EventKey 去重且保留更高层版本

QueryEvents SHALL 在单次查询内按 EventKey 去重：压实"先写目标层、后删源层"的崩溃窗口内同一事件双层并存时，SHALL 仅返回一次，且保留规则 SHALL 与遍历方向无关——保留来自更高 layer（更新窗口）的版本（压实写入目标层即 canonical 状态）。

#### Scenario: 压实中间态不返回重复事件

- **GIVEN** 同一事件同时存在于 L1 源段与 L2 目标段（压实未完成清理）
- **WHEN** 执行 QueryEvents
- **THEN** 该 EventKey SHALL 只出现一次

#### Scenario: 去重版本选择与方向无关

- **GIVEN** 同一事件双层并存（L2 版本已摘要化）
- **WHEN** 分别以 timestamp_desc 与 timestamp_asc 执行同一查询
- **THEN** 两次返回的该事件内容 SHALL 均为 L2（更高层）版本

### Requirement: 双实现查询语义一致与全序确定性

查询排序 SHALL 使用全序键 `(timestamp, EventKey)`（timestamp_desc 即两键同时逆序），同毫秒事件以 EventKey 决胜——对相同输入，任意实现、任意两次查询的返回序列 SHALL 逐位一致。`InMemoryStore` 与 `FileSegmentStore` 对相同事件集与相同 `QueryOptions`（排序方向、limit/offset、毫秒时间范围、keyword、事件类型过滤的任意组合）SHALL 返回一致的 EventKey 序列，并以共享一致性测试守护。

#### Scenario: 同毫秒事件排序确定

- **GIVEN** 多条事件共享同一毫秒时间戳（并行工具调用场景）
- **WHEN** 重复执行同一查询
- **THEN** 返回序列 SHALL 逐位稳定（以 EventKey 决胜，无非稳定排序抖动）

#### Scenario: 一致性矩阵

- **GIVEN** 同一事件集分别写入 InMemoryStore 与 FileSegmentStore（LocalFileKV 后端），含同毫秒 tie 事件
- **WHEN** 以 desc/asc × limit 边界 × 毫秒 since/until × keyword 的组合矩阵分别查询
- **THEN** 两实现返回的 EventKey 序列 SHALL 逐位相等

### Requirement: 段边界可信性（键范围元数据与 memtable 语义）

段是 **LSM 式放置单位**：按写入时间放置（顺序追加），其窗口名与层级表示写入新近度与压实代数，SHALL NOT 被当作内容的事件时间边界。时间推理（剪枝、早停）的唯一依据 SHALL 是段的键范围元数据：

- 封口（`SealCurrent`）与压实（L1→L2、L2→L3）SHALL 将段内事件的真实时间边界写入 `SegmentMeta.MinTime/MaxTime`（封口后段不可变，边界从此稳定；压实合并时已按时间排序，取首尾即得，不得为此引入额外全量扫描）。
- 剪枝与早停 SHALL 仅使用存在的边界做事件时间判定；**Sealed=false 的活跃段与缺边界的段（历史遗留）SHALL 被视为 memtable：永远参与扫描，不剪枝、不跳过**。
- 窗口名的层级跨度（日/周对齐）仅作为压实调度与命名概念，SHALL NOT 参与查询的时间判定。

#### Scenario: 封口写入真实边界

- **WHEN** 一个活跃段被封口
- **THEN** 其 meta 的 `MinTime`/`MaxTime` SHALL 等于段内事件的最小/最大 `Timestamp`，且 `Sealed=true`

#### Scenario: 超宽段不被时间范围查询误剪

- **GIVEN** 一个 L2 段标称跨度 1 天、实际含跨至第 3 天的事件，且 meta 已写 `MaxTime`
- **WHEN** 查询时间范围落在第 3 天
- **THEN** 该段 SHALL 参与扫描，其在范围内的事件 SHALL 被返回

#### Scenario: 活跃段与历史段一律不剪不跳

- **GIVEN** 一个 `Sealed=false` 的活跃段，或一个 `Layer=2` 且 `MaxTime=0` 的历史段
- **WHEN** 执行带时间范围或已达 budget 的查询
- **THEN** 该段 SHALL NOT 被剪枝或跳过

#### Scenario: 写入/事件时间分叉时活跃段不丢事件

- **GIVEN** 一批事件的写入时刻与 `Timestamp` 显著分叉（异步回写至活跃段）
- **WHEN** 以覆盖该批事件 `Timestamp` 的范围查询
- **THEN** 这些事件 SHALL 全部可见（活跃段作为 memtable 参与扫描）

### Requirement: 早停判据以实际事件时间戳为基准

早停（跳过候选窗口）的比较基准 SHALL 是**已收集事件的真实 `Timestamp` 极值**，SHALL NOT 使用窗口标称边界作为已收集侧的代理。当已收集数达到 `offset+limit` 后：`timestamp_desc` 下仅当候选窗真实上界 **严格小于** 已收集最小时间戳时方可跳过；`timestamp_asc` 下仅当候选窗真实下界 **严格大于** 已收集最大时间戳时方可跳过。

#### Scenario: 超宽段不被早停误跳

- **GIVEN** 一个标称很旧、实际含较新事件的 L2 段，且 desc 遍历下已收满 budget
- **WHEN** 遇到该段
- **THEN** 若其真实上界 ≥ 已收集最小时间戳，该段 SHALL 被扫描

#### Scenario: 同毫秒边界不被错误跳过

- **GIVEN** 候选窗真实上界恰等于已收集最小时间戳
- **WHEN** 判定是否跳过
- **THEN** SHALL NOT 跳过（严格不等式；同毫秒事件的 EventKey 决胜可能使其入选）

### Requirement: 时间真相源单一

`FullEvent.Timestamp` SHALL 是唯一语义时间轴：排序、时间过滤、TTL 年龄均 SHALL 只依据它。`EventKey` 内嵌的时间（写入时刻）SHALL 仅用于段放置与同毫秒全序决胜，SHALL NOT 参与语义时间判定。两者在异步事件下允许分叉；剪枝与早停仅读封口段的事件时间边界（见“段边界可信性”），故分叉 SHALL NOT 影响查询结果的正确性。

段按写入时间放置是 **LSM 顺序追加的设计特性，不是缺陷**：段名与层级表示写入新近度与压实代数，与事件的逻辑时间正交。

#### Scenario: 双时间分叉时查询结果不变

- **GIVEN** 一组事件其 EventKey 内嵌时间与 `Timestamp` 显著分叉（异步回写场景）
- **WHEN** 执行排序与时间过滤查询
- **THEN** 结果 SHALL 与“仅按 `Timestamp` 过滤排序”的参照完全一致

### Requirement: seq 槽位与事件身份一一对应

写入时分配的 `{pid}:evt:{window_ts}:{seq}` 槽位 SHALL NOT 覆写已存在的事件。当内存态判定进入一个“新窗口”时（含进程重启后首次写入），存储层 SHALL 先从 KV 恢复该窗口已使用的最大 seq，并从 `max+1` 续写（空窗口则从 0）。

#### Scenario: 重启后同窗写入不覆写

- **GIVEN** 某窗口已有 seq 0..4 的事件，进程重启（PartitionState 回到零值）
- **WHEN** 同一窗口内写入新事件
- **THEN** 新事件 SHALL 占用 seq 5，seq 0..4 的事件 SHALL 仍可经其 EventKey 取回

#### Scenario: 空窗口从 0 开始

- **WHEN** 向一个尚无事件的窗口写入首个事件
- **THEN** 其 seq SHALL 为 0

## MODIFIED Requirements

### Requirement: seqCounter recovered from existing segment data

存储层 SHALL 在进入新窗口时从已有段数据恢复 `seqCounter`：扫 `{pid}:evt:{window_ts}:` 前缀取最大 seq，从 `max+1` 续写。扫描域 SHALL 仅限当前窗口，SHALL NOT 全分区或全层扫描。`Init()` 可保持惰性（不预扫分区），因为恢复已在写入路径上保证。

#### Scenario: 惰性恢复不预扫

- **WHEN** tagent 重启后首次向某分区写入
- **THEN** SHALL 仅对目标窗口发起一次前缀扫描，SHALL NOT 扫描其他分区或其他窗口

#### Scenario: Init on empty store

- **WHEN** `Init()` is called on a KV store with no existing data
- **THEN** the call SHALL succeed without error, leaving partition states at their zero values

> 取代原“seqCounter recovered from active-partition bitmap + cursor”——该条款所依赖的 `global:active_partitions` bitmap 与 `{pid}:cursor` 从未实现（见 change design 的“已知规格债”）。

### Requirement: resolvePartitions 默认隔离（无分区参数返回空）

`resolvePartitions(query)` 在 `PartitionIDs` 与 `PartitionID` 均未指定时 SHALL 返回空集（即不扫描任何分区）——子 Agent SHALL NOT 通过无参查询盲扫其他分区的历史；跳分区读取 SHALL 由工具层根据 `read_namespaces` 显式注入 `PartitionIDs`。

#### Scenario: 无分区参数的查询不盲扫

- **WHEN** `QueryEvents` 未指定任何分区
- **THEN** 返回结果 SHALL 为空，SHALL NOT 返回其他分区的事件

#### Scenario: 显式分区授权生效

- **WHEN** 工具层注入 `PartitionIDs: [144, 432]`
- **THEN** 查询 SHALL 仅覆盖这两个分区

> 取代原“resolvePartitions defaults to all active partitions”——原条款未实现，且与“发现（查询）走隔离”的跳命名空间读权设计相矛盾。

### Requirement: eventCount 在进程生命期内反映实际事件数

`PartitionState.eventCount` SHALL 在事件被永久删除时递减（`DeleteEvent` 减 1；压实 `deleteSegments` 按被删段的 `EventCount` 递减）。该计数 SHALL 被理解为**进程生命期内的计数**：它不持久化、重启后从 0 重新累计，因此仅适用于容量淘汰这类近似判据，SHALL NOT 被当作持久化的全库事件总数。

#### Scenario: eventCount decremented on DeleteEvent

- **WHEN** `DeleteEvent(key)` successfully deletes an event
- **THEN** `state.eventCount` SHALL be decremented by 1

#### Scenario: eventCount decremented on compaction cleanup

- **WHEN** compaction's `deleteSegments()` removes a segment with `SegmentMeta.EventCount=247`
- **THEN** the partition's `eventCount` SHALL be decremented by 247 after successful deletion

#### Scenario: 重启后计数不保证全库准确

- **WHEN** 进程重启后读取 `GetStats().TotalEvents`
- **THEN** 其值只反映本进程已写入的事件数，而非存储中的历史总量

## REMOVED Requirements

> 收窄原“eventCount reflects actual event count after deletions”的承诺边界。

### Requirement: StoreEvents partitions seq counter by (pid, windowTS)

**移除理由**：`StoreEvents` 方法自 `MemoryStore` 接口删除（memory 包外零生产调用方，且其实现不写 meta 而使写入事件对窗口发现不可见）。本要求描述的分组 seq 行为从未实现。批量写入能力如未来需要，应作为新能力重新提案。

### Requirement: StoreEvents batch writes segment metadata and cursor atomically

**移除理由**：同上（方法删除）。且其依赖的 `{pid}:cursor` 与 `global:active_partitions` bitmap 从未实现。
