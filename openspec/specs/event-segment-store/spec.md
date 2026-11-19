# event-segment-store Specification

> **勘误（2026-11-19 契约同步核验）**：本规范原「关闭停掉生命周期与压实子系统」「compaction 源窗口读失败必须中止本轮」「TTL 扫描读失败必须可观测」三条条款要求的压实器、生命周期管理器、墓碑集与关系仓在 Go 树零定义，该压实与淘汰机制已整体退役。关闭现按提交屏障加写权交还重写；「读不完整不得被当成空」这一保证由「存储失败不可冒充正常缺失」承载，不再以压实陈述；TTL 淘汰路径已不存在，故不再有可观测对象。

## Purpose

本规范定义 event-segment-store 能力。The system SHALL persist events in the RustViking KV store organized by time-windowed segments.
## Requirements
### Requirement: Store event as JSON Lines segment by time window

The system SHALL persist events in the RustViking KV store organized by time-windowed segments. Event content SHALL be stored under key `{pid}:evt:{window_ts}:{seq}` → JSON FullEvent. Events SHALL be appended with a monotonically increasing sequence number within each time window. The sequence counter SHALL be recovered from existing KV data on startup to prevent overwriting existing events.

#### Scenario: Append event to active segment

- **WHEN** `MemoryPlugin.onEvent()` calls `StoreEvent(fullEvent)`
- **THEN** the event is serialized as JSON and stored via KV `put` under key `{pid}:evt:{window_ts}:{seq}`, and an index entry is stored under key `{pid}:idx:{event_key}` → `{window_ts}:{seq}`

#### Scenario: Active segment auto-seals on hour boundary

- **WHEN** the current time crosses an hour boundary (e.g., 14:00:00)
- **THEN** the current window's segment metadata SHALL be updated to `Sealed: true` and `Layer: 1`, and a new window SHALL start with seq=0

#### Scenario: Seal produces segment metadata

- **WHEN** an active segment is sealed
- **THEN** the segment meta KV entry (`{pid}:meta:{window_ts}`) SHALL be updated with `event_count`, `sealed: true`, and `layer: 1`

### Requirement: seqCounter recovered from existing segment data

存储层 SHALL 在进入新窗口时从已有段数据恢复 `seqCounter`：扫 `{pid}:evt:{window_ts}:` 前缀取最大 seq，从 `max+1` 续写。扫描域 SHALL 仅限当前窗口，SHALL NOT 全分区或全层扫描。`Init()` 可保持惰性（不预扫分区），因为恢复已在写入路径上保证。

#### Scenario: 惰性恢复不预扫

- **WHEN** tagent 重启后首次向某分区写入
- **THEN** SHALL 仅对目标窗口发起一次前缀扫描，SHALL NOT 扫描其他分区或其他窗口

#### Scenario: Init on empty store

- **WHEN** `Init()` is called on a KV store with no existing data
- **THEN** the call SHALL succeed without error, leaving partition states at their zero values

> 取代原“seqCounter recovered from active-partition bitmap + cursor”——该旧条款依赖的 `global:active_partitions` bitmap 与 `{pid}:cursor` 不存在于实现，故 seq 由扫描事实链重建。

### Requirement: seq 槽位与事件身份一一对应

写入时分配的 `{pid}:evt:{window_ts}:{seq}` 槽位 SHALL NOT 覆写已存在的事件。当内存态判定进入一个“新窗口”时（含进程重启后首次写入），存储层 SHALL 先从 KV 恢复该窗口已使用的最大 seq，并从 `max+1` 续写（空窗口则从 0）。

#### Scenario: 重启后同窗写入不覆写

- **GIVEN** 某窗口已有 seq 0..4 的事件，进程重启（PartitionState 回到零值）
- **WHEN** 同一窗口内写入新事件
- **THEN** 新事件 SHALL 占用 seq 5，seq 0..4 的事件 SHALL 仍可经其 EventKey 取回

#### Scenario: 空窗口从 0 开始

- **WHEN** 向一个尚无事件的窗口写入首个事件
- **THEN** 其 seq SHALL 为 0

### Requirement: resolvePartitions 默认隔离（无分区参数返回空）

`resolvePartitions(query)` 在 `PartitionIDs` 与 `PartitionID` 均未指定时 SHALL 返回空集（即不扫描任何分区）——子 Agent SHALL NOT 通过无参查询盲扫其他分区的历史；分区读取 SHALL 由工具层显式注入 `PartitionIDs`：自身命名空间分区恒在首位（事件写入即写入该分区，agent 无额外配置也应能查询自身时间线），跨分区读取再根据 `read_namespaces` 追加（去重）。

#### Scenario: 无分区参数的查询不盲扫

- **WHEN** `QueryEvents` 未指定任何分区
- **THEN** 返回结果 SHALL 为空，SHALL NOT 返回其他分区的事件

#### Scenario: 显式分区授权生效

- **WHEN** 工具层注入 `PartitionIDs: [144, 432]`
- **THEN** 查询 SHALL 仅覆盖这两个分区

> 取代原“resolvePartitions defaults to all active partitions”——原条款未实现，且与“发现（查询）走隔离”的跳命名空间读权设计相矛盾。

### Requirement: eventCount 在进程生命期内反映实际事件数

计数 SHALL 是成功提交发布的事件数：打开时由链自述的计数一次读出，链不自述时由索引枚举重建，每条索引记录一条事件身份，窗口元数据未到达的条目不计；此后每次成功提交只加一。重复提交与写失败 SHALL NOT 增计数。

#### Scenario: 重启后计数可恢复
- **WHEN** 新进程打开一个已有成功提交的存储
- **THEN** 计数 SHALL 等于该链已提交事件的数量
- **AND** 未提交的条目与窗口元数据未到达的条目 SHALL NOT 被计入

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

`FullEvent.Timestamp` SHALL 是唯一语义时间轴：排序与时间过滤均 SHALL 只依据它。`EventKey` 内嵌的时间（写入时刻）SHALL 仅用于段放置与同毫秒全序决胜，SHALL NOT 参与语义时间判定。两者在异步事件下允许分叉；剪枝与早停仅读封口段的事件时间边界（见“段边界可信性”），故分叉 SHALL NOT 影响查询结果的正确性。

段按写入时间放置是 **LSM 顺序追加的设计特性，不是缺陷**：段名与层级表示写入新近度与压实代数，与事件的逻辑时间正交。

#### Scenario: 双时间分叉时查询结果不变

- **GIVEN** 一组事件其 EventKey 内嵌时间与 `Timestamp` 显著分叉（异步回写场景）
- **WHEN** 执行排序与时间过滤查询
- **THEN** 结果 SHALL 与“仅按 `Timestamp` 过滤排序”的参照完全一致

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

### Requirement: FileSegmentStore 以提交屏障关闭并交还写权

关闭 SHALL 以一次空批提交作为屏障收尾，然后关掉该视图背后的事件引擎并交出它在该根上取得的写权，使下一个写者被接纳；写权的交还 MUST NOT 以屏障成功为条件，屏障失败 MUST NOT 留下已关闭视图仍占着根。关闭 MUST NOT 依赖任何后台扫描、压实或墓碑子系统停机。屏障失败与关引擎失败的任一者 SHALL 如实上报，MUST NOT 以另一者的成功掩盖。

#### Scenario: 关闭先过屏障再关引擎

- **WHEN** 对一个事件段仓发起关闭
- **THEN** 其实现 SHALL 先以一次提交屏障收尾，再关掉该视图背后的事件引擎，并交出该根的写权
- **AND** 屏障失败时写权 SHALL 仍被交出

#### Scenario: 关闭重复调用不重复关引擎

- **WHEN** 对同一个事件段仓再次关闭
- **THEN** 第二次关闭 MUST NOT 再次关掉该引擎，且 MUST NOT 报出首次关闭未报的错误

### Requirement: LocalFileKV Sync 为原子快照屏障

LocalFileKV 的 `Sync()` SHALL 将全部键值整序列化并以临时文件写入加原子 rename 落盘；快照 SHALL 按分区分片（每分区独立文件），`Sync()` 只重写自上次屏障以来变更过的分区——单次提交的屏障成本 SHALL 与其触碰的分区成正比，MUST NOT 与全库键数成正比。`Sync()` 返回成功后，独立新进程 SHALL 能读回该快照代表的全部键值。快照文件本体损坏 SHALL 启动失败（fail-fast）。FileSegmentStore.StoreEvent 的提交屏障契约不变：evt/idx/必需 meta 写完且 `Sync()` 成功后才发布缓存、计数与成功结果；屏障未完成 MUST NOT 报告 durable 成功，调用方 MUST 在成功后才投影。

后端档位 SHALL 各自**单独证明**实际屏障，MUST NOT 假设批量 API 天然原子。内存档（`InMemoryStore`）SHALL 复用同一业务合同（提交/隔离/去重语义一致），但**不承诺进程恢复**，MUST NOT 在重启后凭空补出未持久记录。系统 MUST NOT 为规避屏障合同而新增数据库或 WAL 选型。

#### Scenario: 屏障后跨进程读回

- **WHEN** StoreEvent 提交且 `Sync()` 返回成功，进程随即终止而不经 Close
- **THEN** 独立新进程可经 EventKey 取回原文及索引

#### Scenario: 屏障失败不报 durable 成功

- **WHEN** 事件提交遇快照写入或 rename 失败
- **THEN** 返回非 nil error、保留故障证据，投影不追加，消费者不收到 durable 成功

#### Scenario: 内存档复用合同但不耐久

- **WHEN** 使用内存档提交事件后重启
- **THEN** 提交/隔离/去重语义 SHALL 与持久档一致，但该记录 MUST NOT 被承诺跨进程恢复（重启不出「凭空补出」的未持久记录）

#### Scenario: 不新增数据库规避屏障

- **WHEN** 审查后端档位实现
- **THEN** SHALL 保持 memory/journal/file 三档及各自屏障证明（`localfile` 是耐久本地的退役名，构建按 journal 打开），MUST NOT 引入新的数据库/WAL 以逃避「不假设批量原子」的合同

### Requirement: 后端不可变与隔离一致性

存储 SHALL 保持已提交事实不可变、分区查询隔离及返回对象隔离。事实提交入口 SHALL 以领域等价规则判断同一身份是否已存在，底层位置与字节完整性校验 SHALL 保全原有记录；不得把调用方重新编码的字节相等作为领域重复提交的额外条件。

一条事实的原文、引用、事件与必要位置索引 SHALL 在同一原子提交单元内保存。确定拒绝的提交 SHALL 不发布任何部分；新增成功 SHALL 只占一次事件编号与计数。提交结果不确定时 SHALL 保持不确定并通过可验证读取解决，SHALL NOT 假定零写入或回报确定成功。

事实提交结果 SHALL 按五种结局如实答复：新增、已存在、冲突、不确定，以及**写前确定的拒绝**（`FactRefused`）。领域等价的重复提交返回原事实与原位置，不写入或刷新既有内容；领域不等价的同身份提交具名拒绝（冲突）；引擎在写盘前即判定不承载该提交时 SHALL 答确定的拒绝，MUST NOT 答不确定——该结局在其余四类中无槽位（什么都没落，故不是新增；无领域等价对象，故不是已存在；无身份或内容冲突，故不是冲突；确知零写入，故不是不确定），并进任何一类都会让调用方对一个必然失败的提交反复重读。只有引擎无法命名其耐久结局时 SHALL 答不确定。该判定与实际写入间的隔离 SHALL 由设计门证明覆盖真实写者。

#### Scenario: 一条事实一次提交

- **WHEN** 正式入口提交一条带独立原文的事实
- **THEN** 原文、引用及事件索引 SHALL 同笔可见，重开后身份与原字节不变，计数只增加一次

#### Scenario: 确定拒绝不发布部分

- **WHEN** 引擎在部分物理写入后确认整笔提交未成立
- **THEN** 该批任何部分 SHALL 不成为已提交可读状态，且不能由下一次提交带入

#### Scenario: 领域等价但编码字节不同

- **WHEN** 正常重提交只改变领域等价规则允许忽略的时间或运行戳，而领域内容仍等价
- **THEN** 事实提交 SHALL 返回已存在的原事实与原位置，不重新编码并覆盖、不占号，也不把该差异判成内容冲突

#### Scenario: 同身份内容冲突

- **WHEN** 同一身份提交领域不等价的内容
- **THEN** SHALL 具名拒绝，原记录和计数保持不变

#### Scenario: 屏障结果不确定

- **WHEN** 引擎不能确定整批是否已持久化
- **THEN** SHALL 返回不确定，由受控重读确认是否已存在；不得以零写入断言掩盖已实际落盘的事实

#### Scenario: 写前确定的拒绝不答不确定

- **WHEN** 引擎在追加写盘之前就判定该批不承载（例如超出单帧界或写入权已关）
- **THEN** 提交 SHALL 答复确定的拒绝，MUST NOT 答复不确定
- **AND** 该批的任何键 SHALL 零落地（含原文族），且答复 SHALL 与其他四类结局可区分

#### Scenario: 可变返回对象与分区隔离

- **WHEN** 调用方修改返回对象或发起缺少规定分区过滤的查询
- **THEN** 原事实 SHALL 不被修改，查询 SHALL 遵守既有分区隔离约束

### Requirement: 存储失败不可冒充正常缺失

KV/事件接口 SHALL 区分 typed not-found、duplicate 与存储 I/O 错误；GetEvents/QueryEvents SHALL NOT 吞掉 I/O 后返回完整成功。部分结果与非 nil error 可同时返回；上层 SHALL 处理 partial。元数据提交失败 SHALL NOT 被忽略为成功事件。

#### Scenario: 多分区部分扫描失败
- **WHEN** 一个已授权分区可读而另一个扫描失败
- **THEN** 查询返回可用部分和明确错误，不报告空结果或完整成功

#### Scenario: 内部重放修复未完成写入
- **WHEN** 原文已写而索引/meta 未完成，使用相同身份和内容重试
- **THEN** 确定性补齐并经过屏障后才成功，不生成不同内容或重复投影

> 退役（10.R5d）：原「ReplayEvent 窗口重入与 StoreEvent 同做 sealed 降级」要求已删除——其三项依赖（ReplayEvent、seqCounter、sealed 窗口元数据）在 Go 树零定义，窗口不封口、不搬迁、不按元数据包络剪枝，故「两处共用同一实现」无对象可言；重放与普通提交现走同一条提交路径（`CommitFact`／`StoreEvent`），事件可见性按事件自身 `Timestamp` 判定。

### Requirement: 未确认恢复材料的单属主 pin

未结投递义务的结算事实 SHALL 由恢复 owner 按义务身份 pin 在热窗上：登记表以义务身份为键，一条未结义务对应一条 pin，pin 集合挂在运行的热窗上。持有者是投递账户本身，不是任一 agent。登记与解除随义务状态：义务未结即登记、结清即解除，同一身份换用新结算事实时旧 pin 同批解除，已结义务 MUST NOT 重新持有 pin。pin 集合 MUST 在启动时由既有 checkpoint 重建，MUST NOT 引入第二张持久保留表或全历史去重集合。pin 是单属主登记：一个身份在表里恒有一条登记，解除不等待归零、不跨 agent 合并，MUST 幂等。**持久恢复档启用时**，若 store 缺少保护/回收协调能力，系统 SHALL **具名报告能力不完整并拒绝启动或禁止破坏性清理**（fail-closed），MUST NOT 以「无保护即成功」继续；**非耐久档**（不承诺进程恢复）缺少恢复能力 SHALL 以**显式 capability 缺失**声明，不得伪造保护已生效。

pin 存续期间，热窗裁剪 MUST NOT 物理销毁被 pin 的事实：裁剪把 pin 命中项移出待删集合，其余事实按普通窗口老化。保护只覆盖热窗裁剪这一条销毁路径：该裁剪的待删前缀由热窗边界给出——热窗投影仍在呈现的最旧原文之前的事实，pin 不改变这条判据，只把命中项从待删集合里摘出。义务结清后该事实按其原有时间戳恢复参与老化，MUST NOT 重新盖时间。恢复材料清除后的任意手工历史重放不属于自动恢复保证，MUST NOT 为此新增永久去重库。

提交屏障与 pin SHALL 各自独立：屏障成功是提交对外可见的前提，pin 是「未结义务的结算事实仍在窗内」的登记，两者不得互相短路（屏障不因 pin 存在而放宽，pin 不因屏障成功而解除）。

#### Scenario: 未结义务的结算事实不被热窗裁剪

- **WHEN** 一条链增长到热窗之外，而它对应的投递义务仍未结清
- **THEN** 该结算事实仍在窗内可达，MUST NOT 被裁剪销毁；同一窗口内与未结义务无关的事实照常老化

#### Scenario: 重启时 pin 先于首轮裁剪重建

- **WHEN** 进程从 checkpoint 恢复仍欠投递的义务
- **THEN** pin 集合 SHALL 由该 checkpoint 的 pin 表重建，其后的裁剪 MUST NOT 把这些结算事实当作待删项

#### Scenario: 义务结清解除 pin

- **WHEN** 一条义务结清，或该身份的结算事实被新的替换
- **THEN** 该身份的登记解除、旧 pin 同批解除，MUST NOT 留下无人认领的 pin；其后该事实按其原有时间戳恢复参与老化

#### Scenario: 持久恢复档缺能力 fail-closed

- **WHEN** 启用持久恢复档而 store 未提供保护/回收协调能力
- **THEN** 系统 SHALL 具名报告能力不完整并拒绝启动或禁止破坏性清理（fail-closed），MUST NOT 当作无保护成功

#### Scenario: 非耐久档显式 capability

- **WHEN** 使用不承诺进程恢复的档位
- **THEN** 其恢复能力缺失 SHALL 以显式 capability 声明，MUST NOT 伪造保护已生效

### Requirement: 恢复能力经过包装层真实透传

内置后端及 engine／error-tracking 包装链 SHALL **递归**核验并透传**恢复/保留/登记屏障**所需能力；能力 SHALL 按**真实消费者**所需端口表达（具体端口名称/签名**不在此冻结**，DESIGN-GATE(G1) 依真实 consumer 收敛——不复述旧 5 方法清单）。包装层 MUST NOT 以外层满足接口代替内层能力，也不得以普通读写的存在作为重放能力的弱回退。

**持久恢复档启用时**，若内层缺少保护/回收协调能力，MUST fail-closed：**具名报告能力不完整**、拒绝启动或禁止破坏性清理，MUST NOT 静默降级为「无保护即成功」，也 MUST NOT 以「屏障类空操作」伪装协调已生效。**非耐久档**缺少恢复能力 SHALL 显式声明为 capability 缺失。

重放成功时 SHALL 把**内层返回的 canonical fact** 交给下游消费，MUST NOT 以传入副本代替。重放错误 MUST 到达恢复 owner：可靠收件箱已自持这些事件的 at-least-once 重试，因此重放失败**不得**再落一份 mem_spill 兜底（那会在兜底文件里造出重复条目）；普通写入原有的 spill 行为不变。spill 重建出的 pin 若失败，MUST 以错误上抛由调用方 fail-closed、旧实例继续服务，MUST NOT 以「无 pin 即成功」继续。

绝对事件计数只在真实提交路径递增；判为「已存在」的重复提交 MUST NOT 递增，否则一次幂等重提会被当作新增而抬高计数。

#### Scenario: 持久恢复档缺协调能力 fail-closed

- **WHEN** 启用持久恢复档而内层包装链缺少保护/回收协调能力
- **THEN** 系统 SHALL 具名报告能力不完整并拒绝启动或禁止破坏性清理，MUST NOT 以空操作或「无保护即成功」降级

#### Scenario: 重放失败不被 mem_spill 兜底掩盖

- **WHEN** 重放调用失败
- **THEN** 错误 SHALL 到达恢复 owner（at-least-once 重试），MUST NOT 再落一份 mem_spill 兜底条目

