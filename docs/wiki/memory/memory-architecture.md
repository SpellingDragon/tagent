> **历史文档（historical-verbatim）**：本篇描述已移除的生产实现；当前架构见 [docs/wiki/README.md](../README.md#当前架构速览)。原版逐字保存在 git 历史（取回：`git show e4e884a48c6b9a5a70fcd13265e28d3aa55a67c8:docs/wiki/memory/memory-architecture.md`），指针清单见 [docs/archive/wiki/PRE-REWRITE-MANIFEST.md](../../archive/wiki/PRE-REWRITE-MANIFEST.md)；本篇是同一语义面的压缩重写，只保留契约、机制对照与判据。

# tagent/memory 记忆存储：语义契约与机制对照

`tagent/memory` 是 tagent 的**结构化事件存储层**，为 Agent 提供因果链追踪、按需精确检索、多维度查询能力。边界：本层只存事实与派生投影，不认识 agent 身份；身份→分区的映射发生在记忆层之外。

**设计原则（逐字保留）**：
- **信息隔离**：Session 只保存轻量引用（`EventReference`），完整数据在 MemoryStore
- **因果优先**：每个事件通过 `RelationStore.SetParent` 指向其前驱事件，支持因果回溯
- **视图独立**：压缩只修改 LLM 消息视图，不修改 MemoryStore 中的数据
- **引擎旁路**：向量索引是派生投影（事件不可变；遗忘物理删除时联动移除向量），不进存储同步点

<a id="overview"></a>
## 一、语义契约（红线与不变量）

<a id="hard-contracts"></a>
### 1.1 九条硬契约

这些是记忆子系统对外的行为承诺；修改本模块时它们是不得跌破的底线。

| 契约 | 含义 | 保障机制 |
|---|---|---|
| **1. 事件不可变** | 写入后的 FullEvent 永不被原地修改；压缩与遗忘只作用于“视图”和“可见性” | 写入只有 `StoreEvent` 一条路径；压实是搬迁而非改写；TTL 用墓碑而非原地删 |
| **2. 槽位与身份一一对应** | `{pid}:evt:{窗}:{seq}` 槽位里装的必是 `{pid}:idx:{eventKey}` 指向它的那个事件 | seq 由 `PartitionState.seqCounter` 单调递增分配；evt 与 idx 同次写入 |
| **3. 声明式查询语义** | `QueryEvents` 结果 ≡ 全集过滤 → 全序排序 → offset/limit；分段/剪枝/早停仅为优化 | 全序键 `(Timestamp, EventKey)`；剪枝与早停只依据真实时间边界；双实现一致性测试矩阵 |
| **4. 召回时间箭头与压缩同向** | 压缩丢旧留新，因而召回必须新先于旧；`timestamp_desc` 下截断只牺牲最旧 | 窗口按查询方向遍历；整窗粒度收集（窗内 seq 是字符串序而非时间序） |
| **5. 召回底线（可寻址性）** | 卡片里的 `[key]` 票据要么取回原文、要么诚实报 miss，绝不静默返回错误内容 | `GetEvent` 先查墓碑再查 idx；固化物（summary）/ consolidation / governance 豁免 TTL 与容量淘汰 |
| **6. 时间真相源单一** | `FullEvent.Timestamp` 是**唯一时间轴**（排序/过滤/TTL/卡片时间线均只认它）；EventKey 内嵌时间仅用于段放置与同毫秒决胜 | 两者均在写入处一次派生；无任何判定同时依赖两个时间，因此异步事件下的分叉无害 |
| **7. 分区隔离与显式授权** | 子 Agent 不得盲扫其他分区；跳区读取需 `read_namespaces` 显式授权 | `resolvePartitions` 无分区参数时返回空；工具层注入 `ReadPartitionIDs` |
| **8. 压实 crash-safe** | 任何时刻崩溃不丢事件，最多短暂双层并存 | 先写目标层、后删源层；查询侧按 EventKey 去重并保留高层版本 |
| **9. 同点投影** | “对 LLM 可见”与“已入投影”不得分家 | `persistBusEvent` 同步双写 store 与投影 |

**事实链红线**：**关键**：压缩只修改发给 LLM 的消息视图，不修改 MemoryStore 和 RelationStore。因果关系在整个生命周期中保持不变。事件入库后，任何后台工人（封口、压实、TTL、容量）只能搬迁、遮蔽或删除**可见性**，不得改写正文与身份；fold/compaction 只追加新事实，从不修改旧事实。

### 1.2 压缩固化语义（素材律与折叠）

记忆只有三个原语：**store**（事件入库，不可变）、**compress**（总结+自然遗忘，同一动作两面）、**recall**（回忆）。没有独立"总结引擎"——内容级总结只在压缩固化时刻发生。

折叠（fold）的产物是**事实链上的一等事件**，不是投影私货：**event-sourced-projection 起，压缩折叠本身是事实链的一等事件**：真折叠时向事实链追加一条 `context_compress_summary` 正 key **compaction 事件**（`Metadata[compaction]=v1` 代际标记；Content/EventSummary=综述正文——可召回正文即叙事；`Metadata[compaction_payload]` 载重建载荷：综述 ref + 有序 retained 列表[负 key tool_chain 合成 ref 全身份逐字节/正 key 只存 key] + fullBoundary），并滚动 supersede（写前查 prior、写后 DeleteEvent，限定代际标记——legacy 固化物不删不选）。投影由此成为**事实链的纯回放**：运行期 `StoreEvent→projection.Add` 增量；重启 `RebuildProjectionFromWAL` = 最新 compaction 作 snapshot + 按 `QueryOptions.MinEventKey` 写序过滤的尾部重放，逐字节重建、prefix-cache 复用。折叠产物在投影内仍是负 key summary ref（渲染语义不变）。

`context_compress_summary` 固化物仅存量存在：保留 TTL 豁免（`getEffectiveTTL` 负值语义 + evict 跳过）与读路径容错、自然清退，不再产生新固化物；记忆召回经卡片行 `[evt_key]` 票据 → recall 精确回补。

### 1.3 正负 key 与两条时间轴

**正键表示真实事件，负键保留给合成摘要引用**（压实产物）。分区字段只能取 10 位：若给 11 位，分区号 ≥ 1024 会把键**翻成负数**（例如按名字哈希得到的 `plan` 分区就是 1810）。后果不是"数值难看"，而是全代码库的 `EventKey > 0` 守卫集体失效——存储解析、投影幂等判定、保留引用计数都会把这些 agent 的事件当作**不可解析**，模型最终只剩摘要与占位符，而没有任何一处会显式报错。历史上早期 mask 为 11 位（0x7FF），回归测试 `TestSnowflakeEventKey_AlwaysPositive` 锁定现 10 位 mask。

两个时间字段职责严格分离，**任何决策都不得同时依赖两者**：`Timestamp` ＝ 语义时间轴（排序、过滤、TTL 年龄、卡片时间线只读它）；EventKey 内编码的时间 ＝ 写入时间（只决定段窗口与同毫秒平局）。异步事件会让两者分叉；分叉无害恰恰因为段落位置不承载语义。

<a id="partitionidfromname"></a>
**分区 ID 派生**：`PartitionIDFromName(name)` 用 FNV-1a 把名称（如 AgentName）映射为 **0-1023** 之间的稳定 PartitionID（与雪花键 10 位分区域一致，bit63 恒 0）；相同名称总是产生相同 PartitionID，`sync.Map` 缓存。无稳定名称可用时 `NewPartitionID()` 以原子计数器生成全局唯一分区。

<a id="counts-known"></a>
### 1.4 计数未知不等于零

存储统计里的活跃计数由事实链重建。后端不支持分区枚举、或扫描失败时，必须置"计数未知"位并如实上报：**未知永远不得伪装成精确的 0**——否则容量驱逐会把"不知道"当成"还有很多余量"或"已经空了"来做决定。**计数未知时整体暂停容量淘汰**，且未知绝不参与判定。

<a id="ttl-authority"></a>
### 1.5 TTL 的唯一权威源

- 类型化 TTL **派生自事件类型注册表**（唯一权威源）；记忆层不得另存一份 TTL 表——两处表迟早漂移，而漂移的表现是某些事件被提前或永不遗忘。
- **淘汰每轮只把存活事件标墓碑**：已墓碑化的键必须被跳过，否则会击穿计数并对已死事件重复递减。
- **事件键在 value 里，不在 KV 键里**：`{pid}:evt:{窗}:{seq}` 槽位键不含 EventKey（`ParseKey` 对 evt 键留零），因此按类型曲线判龄的扫描必须解码事件的**共享头部**（`event_key`/`timestamp`/`event_type`）取键；从槽位键猜 EventKey 会让所有事件的键读成 0，淘汰判定整体静默失效。同一理由，读侧一律复用共享事件头而不是另写一份解码——两份解码迟早漂移。

<a id="consolidation"></a>
### 1.6 巩固产物的来源收据

巩固（蒸馏／经验总结）写回的产物**必须携带源事件票据收据与服务器侧计算的内容指纹**。指纹只能由服务端算：若信任模型提交的指纹，模型改正文而保留原指纹即可让伪造内容通过校验——防篡改与防伪造是两件事，只有服务端指纹同时满足。冥想/工具触发的巩固是**建议式**：执行权与质量门在 LLM + 工具硬校验。`memory_consolidate` 在服务端计算源事件收据 SHA1 指纹（LLM 不可伪造——指纹不进 prompt，回放时 `VerifyConsolidation` 重算比对）。

| 判据 | 行为 | 理由 |
|---|---|---|
| 收据解析失败／指向已被 TTL 或墓碑删除的事件 | 计入"已墓碑化"计数，**不报错** | 源事件被合法遗忘是**诚实的衰减信号**，不是错误；把它当错误会让蒸馏通路在正常老化后持续报错 |
| 收据未全部取回 | **跳过指纹比对** | 指纹只对"完整集合"有意义：少一条时重算出的集合本就不同，比对必然假阳性 |
| 收据校验关闭（缺省配置） | 宽松放行，仅记录完整性诊断维度 | 兼容既有行为，同时让伪造面可被度量而非静默存在 |

<a id="compaction-integrity"></a>
### 1.7 压实的完整性判据

- **源窗口读失败必须中止本轮压实**：读不到源事件就不能产出摘要，否则等于凭不完整输入改写历史；中止后由后续轮次重试。
- **墓碑化时必须同步移除向量**（内存索引与持久索引都要动）：只标墓碑不删向量，会让已遗忘的事实在向量检索里复活成死键。
- 压实后必须成批移除被吸收的墓碑（内存与 KV 两侧），否则墓碑只增不减。

<a id="feedback-bind"></a>
### 1.8 反馈绑定不是提交凭据

反馈**不是** durable 提交或 ack 的凭据。它只在事实链已 durable、投影已 append 之后运行，位于 ack 下游；`BindFeedback` 失败既不撤销提交、也不改变 `stored` 的返回，更不影响可靠 inbox 的 durable 判定（那里的凭据是 `PublishReceipt.Durable`）。guardrail 的 `negative_feedback_rate` 只作行为信号，绝不作输入确认或重放凭据。

确定性任务裁决由 `writeSettleFeedback` 写为 feedback 事件，因果边指向对应的 `task_settled` 事件：`completed` 记 positive、`failed` 记 negative；`suspect`、`alive-detached` 与未知状态一律不写——只记确定性裁决，防止噪声污染 guardrail。写失败仅记日志，反馈是旁路产物，不阻塞主链路。两类失败必须可区分：父事件不存在（应回 404，客户端可纠正后重试）与"已落库但因果边写入失败"（应回已创建的告警语义，客户端**不得**盲目重试，否则重复反馈）；归因窗口依赖事件的产出物版本，缺版本时回退时间窗会在跨版本场景把反馈归给错误的对象。

<a id="causal-chain"></a>
## 二、事实形态、因果链与键空间

<a id="event-shape"></a>
### 2.1 记录与引用

一条事实有两种形态：`FullEvent` 是**完整记录**（唯一真源），`EventReference` 是**轻量引用**（键、类型、摘要、时间、角色）。会话侧只持有引用列表，全文按需经 `GetEvent`/`GetEvents` 水合——这是"检索只给排序票据"纪律的落点。因果父引用**不在记录里**（原 `ParentKey` 字段已让位于关系存储），目的是把不可变的事件内容与可变的关系分开。

| 字段 | 含义 | 契约要点 |
|---|---|---|
| `EventKey` | 事件唯一标识 | Snowflake int64；写入时刻编码在键里 |
| `PartitionID` | 存储分区 | 纯存储概念，记忆层不认识 agent；身份→分区映射发生在记忆层之外 |
| `EventType` | 事件类型 | 常量单点定义在事件包 |
| `EventSummary` | 简要摘要 | 供上下文使用，非全文 |
| `Timestamp` | 事件发生时刻 | **Unix 毫秒**；全部语义时间判断只读它 |
| `Role` / `Content` | 原始消息角色与正文 | 角色取 user/assistant/tool/system |
| `ContentParts` | 多模态部件 | 必须同时存活于事实链与真实请求两条路径；附加式 `omitempty` |
| `ToolCalls` / `ToolID` / `ToolResults` | 工具面 | `ToolID` 记录工具结果对应哪次调用，保证跨存储→解析不丢配对 |
| `Metadata` | 附加元数据 | 键的声明与归属单源在事件包（见[元数据键的归属](../event/event-architecture.md#metadata-keys)）。`call_id` 即在此：写入方 = `MemoryPlugin`（按精确响应 ID 命中才盖），读取方 = 离线训练导出；不进 `StateDelta`，也不带 `meta_` 透传前缀 |
| `Response` | LLM 响应快照 | 可选；按契约视为只读，故不做深拷贝 |

`EventReference` 只含 key、PartitionID、type、EventSummary、Timestamp、Role；**关键区别**：Session 中的 `EventReference` 不包含 `Content`、`ToolCalls` 和 `Response`，LLM 看到的只是 `EventSummary`。

### 2.2 因果链（RelationStore）

`InMemoryStore` 与 `FileSegmentStore` 都直接嵌入 `RelationStore` 并实现 `RelationStoreProvider`；第三方实现经该接口暴露因果能力。内容与关系分离：`FullEvent` 不可变，`RelationStore` 维护可变父子边。`parent=0` 表示根事件。

回溯侧（`memory_turn` 与统一 `recall` 的 `turn_key` 形态）沿 `GetParent` 回走时，走不到回合起点必须**具名说出它观察到了什么**，而不是把它当成"没有历史"。六个读数都是观察陈述，不推断原因——TTL、驱逐、权限都不是存储层报出来的，替它猜一个就是把不确定转成错误结论：

| `reason` | 停在哪 | 读者该怎么办 |
|---|---|---|
| `missing_ancestor` | 记着的父边指向一个存储取不回的事件 | 链断在一条**不存在的事实**上；更早的内容不可达，不等于"没有更早" |
| `relation_error` | 读父边本身失败 | 存储侧故障，可按错误重试 |
| `relation_unavailable` | 后端完全没有关系能力 | 这份存储不可能有因果回溯——别把空结果当证据 |
| `cycle` | 走到了已经走过的事件 | 关系数据被写坏（某键成为自身祖先） |
| `limit` | 步数预算先用完 | 提高 `max_steps` 再走一次 |
| `no_parent_edge` | 停在一条没有父边的记录上 | 段起点：重启后或游标被回收后的新段首，**没有猜父** |

三字段与判读规则：`complete=true` 时 `reason` 必为空；`capped` 显式区分"预算用完"这一种不完整。三个字段都是**加法**——读方在缺失时按"不完整"处理，绝不当作空历史。写侧两条规则决定这些读数必然出现而非偶发：`parentKey==0` 时新段就地起链（不猜父），以及因果游标超上界（`maxLastEventKeys`）时回收最久未更新的键（正在提交的键被引用计数保护，不会被抽掉锚）；被回收的域下一次提交就会以 `no_parent_edge` 开头。

**因果链自身的持久化顺序**：`SetParent`／解除父子关系都**先追加 `relations.journal`、后改内存邻接表**，且两步落在同一把锁里。这样一条失败的日志追加之后，读侧看到的仍是改动前的关系——调用方拿到错误、可原样重试，而内存里绝不会挂上一条校日志无法重放的边。同一判据也约束删除侧：journal 未落盘就不撤内存边。

<a id="event-key"></a>
### 2.3 EventKey 位布局与跨重启单调性

| 位 | 字段 | 宽度 | 含义 |
|---|---|---|---|
| 63 | 符号位 | 1 | **恒为 0** |
| 62–53 | `PartitionID` | 10 | 存储分区（0–1023），由调用方推导，记忆层不解释其语义 |
| 52–22 | 时间戳 | 31 | 距 `snowflakeEpoch`（2024-01-01 00:00:00 UTC）的**秒**数，约 68 年量程 |
| 21–12 | 序号 | 10 | 同秒内计数（0–1023），提供亚秒唯一性 |
| 11–0 | 保留 | 12 | 预留给未来（如分布式 worker id） |

序号计数器按分区各自维护（互斥保护）。两条真实约束：

1. **墙上时钟回退（NTP 向后阶跃）时不得回退**：把时间戳钉在上一次已发出的值上，而不是重置。因为压实的"渲染冻结"全窗口锚点（`引用键 >= 边界`）**硬依赖键的单调性**；一旦回退，冻结窗口会重新纳入已渲染过的事件。
2. **新进程不继承内存计数器，只继承事实链**：因此启动后必须以磁盘上"已发出的最大键"播种单调性守卫（`RaiseSnowflakeFloor`）。缺这一步时，**同一秒内的重启会重新发出与已提交事实相撞的键**；而每次相撞都判为内容冲突，配合"冻结键不得覆盖"的规则，**冲突会永久持续**（这一面曾以反复重启的实测序列确认）。守卫是**单向**的：只有严格更高的观测值才抬升它，更低或相等一律空操作。同秒序号用尽时**把时间戳提前一秒**继续发号。

分区号两条推导路径：由名字（FNV-1a，0-1023，同名恒得同分区；**允许碰撞**——分区是为因果链隔离，不是为唯一性）；无稳定名字时用原子计数器（取模乘法在 10 位空间上是**双射**，周期恰为 1024，即同一进程内超过 1024 个分区才会首次撞号）。EventKey 对 LLM/工具的展示统一为 **16 进制**（`FormatEventKey/ParseEventKey`，负号保留给摘要引用），存储层仍为 int64。

<a id="file-segment-store"></a>
### 2.4 KV 键空间与段的生命周期

五类键之间**没有数据库级的引用约束**，全靠键名格式约定互相指向。键形：`{pid}:evt:{窗}:{seq}`→FullEvent、`{pid}:idx:{eventKey}`→`窗:seq`、`{pid}:meta:{窗}`→SegmentMeta、`{pid}:tomb:{eventKey}`→墓碑、`tagent:vec:{eventKey}`→embedding；因果另存 `relations.journal`/`relations.snap`。

| 指向契约 | 由谁维护 | 由谁消费 | 在本模块的作用 |
|---|---|---|---|
| idx 值指向的 evt 槽位装的就是该 eventKey 的事件 | `StoreEvent`（evt+idx 同时写）、压实（重建 idx） | `GetEvent` 票据召回 | 票据召回的 O(1) 寻址能力 |
| 每个有事件的窗口都有 meta 键 | `StoreEvent`（首事件时建）、`SealCurrent`、压实 | `QueryEvents` 窗口发现、`ListSegments`、压实/生命周期枚举 | meta 前缀扫描是**发现段的唯一入口**：无 meta 的事件对查询不可见 |
| tomb 键存在 ⇒ 对应事件对所有读路径不可见 | `TombstoneSet.MarkTombstone`（同时级联修因果链） | `GetEvent` 前置检查、`QueryEvents` 过滤、压实 `filterTombstoned` | 惰性遗忘：标记即不可见，物理清除推迟到下一次压实 |
| `SegmentMeta.MinTime/MaxTime` 是段内容的真实时间包络 | 压实写入（合并时已按时间排序，取首尾即得） | `QueryEvents` 的窗口剪枝与早停判定 | 时间推理的权威来源（而非段名，见 2.4） |

**关键概念澄清：段是 LSM 式放置单位，不是时间单位**。**层级与段名 = 写入新近度与压实代数**，与事件的逻辑时间正交。段按写入时间放置（LSM 顺序追加）；一个“日段”完全可能装着跨数天的事件——段名是对齐命名，不是覆盖承诺。封口与压实在段变不可变时写入 `meta.MinTime/MaxTime`；查询只读它。`Sealed=false` 的活跃段 = memtable，查询永远扫描它。压实的 crash-safe 顺序是先写目标层、后删源层；崩溃窗口内同一事件可短暂双层并存，查询按 EventKey 去重并保留**更高层**版本。压实段复用同一套键形，**层位记在 `SegmentMeta.Layer` 里，绝不编码进键**——否则同一条事实换层就要换键。

| 层 | 语义 | 写入方式 | 阈值 |
|---|---|---|---|
| L0（热） | 当前时间窗事件 / memtable | 直写 KV | 永远被扫描 |
| L1（温） | 已过窗封存段 | 封口时写 `MinTime/MaxTime` | `checkHourlySeal` 跨小时 |
| L2（冷） | 压实段 | `CompactL1ToL2` | L1 段数 ≥ 24 |
| L3（归档） | 更冷段；低价值类型清空 Content | `CompactL2ToL3` | L2 段数 ≥ 7 |

<a id="inmemory-store"></a>
### 2.5 两种存储实现对照

| 维度 | InMemoryStore | FileSegmentStore |
|---|---|---|
| **数据结构** | Go map（`map[int]map[int64]FullEvent`） | KVStore + 时间窗分段（L0-L3）+ SegmentMeta + LRU + 墓碑 |
| **持久化** | 无（`DataDir=":memory:"`） | 有（RustViking KV 或本地 JSON KV） |
| **进程重启** | 数据丢失 | 数据保留 |
| **适用场景** | 测试、原型 | 生产环境 |
| **读性能** | O(1) | LRU 缓存 + EventKey 定位（分区/窗口） |
| **生命周期** | 无 | Tombstone + TTL（固化物豁免）+ 分层压实 |
| **扩展性** | 受内存限制 | 受磁盘限制；冷段压实控制放大 |
| **向量搜索** | 空实现（`ErrVectorSearchNotSupported`） | 空实现；引擎经装饰器旁路 |

| 后台工人 | 周期 | 读 | 写 | 职责边界 |
|---|---|---|---|---|
| `Compactor.checkHourlySeal` | 5min | `PartitionState.currentWindow` | `SealCurrent` → meta（sealed=true） | 只管封口，不动事件 |
| `CompactL1ToL2` / `CompactL2ToL3` | 5min | meta（按 layer 选源）+ 源段 evt 全量 | 目标段 evt/idx/meta → 删源段 → finalizeTombstones（物理删除时联动 `removeVector`） | **分辨率管理**；L3 对低价值类型清空 Content |
| `LifecycleManager.checkTTL` / `checkCapacity` | 1h | 分区内段与事件 | `MarkTombstone` → tomb 键 | **价值衰减管理**（按类型遗忘曲线，固化物豁免） |
| `LocalFileKV.flushLoop` | 连续 | WAL 队列 | kv.wal.jsonl 追加 / 周期性 kv.json 全量 dump | 持久化而已，无语义 |

三层遗忘/降级的**职责不重叠**是本模块的设计意图：压实管“存多细”、TTL 管“还存不存”、容量兼作最后保险。三者均不得破坏召回底线（契约 5）。

### 2.6 状态与恢复

| 状态 | 位置 | 恢复机制 |
|---|---|---|
| 事件事实 | `kv.json` + `kv.wal.jsonl` | 启动 `loadSnapshot` + `replayWAL` |
| 因果链 | `relations.snap` + `relations.journal` | `NewInMemRelationStore` 构造时 `recover()`（快照 + 日志重放） |
| 墓碑集 | `{pid}:tomb:*` | `TombstoneSet.RecoverFromKV` |
| LRU 缓存 | 仅内存 | 不需恢复（冷启动自然回填） |
| 窗口 / seq / 事件计数 | 仅内存 `PartitionState` | 靠首次写入重建——契约 2 的软肋，见缺口表 |
| 向量索引 | `tagent:vec:*`（引擎 KV） | 启动异步重建（Ready 门控；窗口期退化关键词，不阻塞启动） |
| StoreEvent 失败兜底 | `<MemSpillDir>/<agent>.jsonl` | memory 恢复（onChange）触发 ReplaySpilled——重放前 `GetEvent` 预检幂等（防 already-exists），重放走 inner 绕过 ErrorTrackingStore 防递归 |

<a id="read-paths"></a>
## 三、读写路径：隔离、召回与装饰链

### 3.1 两条读路径与分区作用域

| 读路径 | 入口 | 分区作用域 | 是否受 `read_namespaces` 约束 |
|--------|------|-----------|------------------------------|
| **按条件查询** | `QueryEvents`（`recall_query`/`recall_recent`） | `opts.PartitionIDs` 指定的分区集合 | ✅ 是——通过注入 `ReadPartitionIDs` 限定 |
| **按 Key 直读** | `GetEvent(key)`（AgentToolWrapper/`recall_get`） | 单个 Key 精确定位（Key 内含 PartitionID） | ❌ 否——按 Key 跨任意分区还原 |

**自身命名空间恒排在首**：每个 agent 的事件写在 `PartitionIDFromName(agentName)` 里，所以读作用域必须自带自己那一份——没配 `read_namespaces` 时 `resolvePartitions` 把「无分区」当作「什么都不扫」，`FileSegmentStore` 上的按条件查询会**静默返回 0 条事件**（不报错、召回像是没历史）。`read_namespaces` 只能叠加跨命名空间可读，不替代自身那一份。

**⚠️ 查询层作用是 default-nothing，不是"回退全库"**：`FileSegmentStore.resolvePartitions` 与 `InMemoryStore.resolvePartitions` 同形——`PartitionIDs` 与 `PartitionID` 两个字段都未指定时返回**空列表**；`QueryEvents` 逐个扫描该列表里的分区，列表为空 ⇒ **一个分区都不扫、返回 0 条且不报错**。

运维风险的方向与直觉相反：**新增挂载 recall 类工具的 Agent 时漏配 `read_namespaces`，表现是跨命名空间召回静默为空（"像是没有历史"），而不是越权扫全库。** 诊断此类失忆，第一步核对该 Agent 的工具工厂是否把 `ReadPartitionIDs` 传进了构造函数。设计意图：发现（查询）走隔离，还原（按 Key）走全库——顶层 Agent 已持有 `event_key`，需要精确还原完整 `FullEvent` 喂给子 Agent，这是「子写、顶读、顶编排」模式的技术基础。

**第三条消费**：冥想的 novelty 判据把两条读路径串起来——按条件查询给降序引用页（default-nothing 挡住未授权分区），按 Key 直读逐条水合成 `FullEvent` 读持久归因 `Metadata[trigger_source]`，**命中即停**；分页上界即水合量上界，成本受 `Limit` 约束。盖章发生在入库：`trigger_source` 由写入旁路从归因面抄进 `FullEvent` 元数据，判据侧只读不写；缺该键的存量事件按未知谱系处理、不计入新鲜度。

### 3.2 查询语义与省 IO 判据

| 契约 | 含义 |
|------|------|
| 声明式语义 | 结果 ≡ 全集过滤 → 全序排序 → offset/limit；分段、窗口剪枝、早停均为**优化**，不改变可观察结果 |
| 全序确定性 | 排序键为 `(Timestamp, EventKey)`——同毫秒事件（并行工具调用常见）以 EventKey 决胜，任意实现/任意两次查询逐位一致 |
| 最新优先 | `timestamp_desc` 下 limit 截断只牺牲最旧、永不牺牲最新——召回的时间箭头与压缩同向（压缩丢旧留新，召回必须新先于旧） |
| 身份唯一 | 压实"先写目标层、后删源层"的崩溃窗口内同一事件可能双层并存；查询按 EventKey 去重，且**方向无关地**保留更高 layer 版本 |

`FileSegmentStore` 的窗口发现从 meta 解析 `(windowTS, layer)`（L0/L1 剪枝跨度 1h、L2 1d、L3 1w），按查询方向遍历；窗内 `seq` 是字符串序而非时间序，故收集以**整窗**为粒度，早停只跳过时间上不可能贡献结果的窗口。**扫描侧的省 IO 判据**：窗内遍历时**先只解事件的轻量头部**（键、类型、时间等路由/去重/过滤所需字段），只有关键词过滤确实需要正文时才物化可能很大的 `Content`；解不出头部的那条按与全文解码失败同样的方式跳过。关键词匹配是面向召回的分词语义：单条词整串子串匹配；多条词**任一词命中即算命中**（整串字面匹配会静默返回零结果）；`-` `_` `.` `@` `#` `$` **刻意不当分隔符**（它们出现在标识符、路径、邮箱与 hex 票据内部）；空关键词视为全部匹配。

### 3.3 recall 协议（索引卡=召回票据）

| 输入形态 | 路径 | 特性 |
|---|---|---|
| `items=[{key,hint?}]` | 批量 `GetEvent` 精确回补 | 纯函数零幻觉；未命中显式 `miss`；hint 回显对账 |
| `query`(+filters) | `QueryOptions` 关键词检索 | 检索层可独立演进（→向量），入口协议不变 |
| `recall(turn_key=key)` | 沿 `GetParent` 回走到 `external_input` | 重建整轮执行过程（含被压缩丢弃的 tool 步骤）；边界=external_input，无需正向遍历 |

统一入口 `recall` 参数即路由：items 为票据直达纯函数（确定性路径无 LLM 中间层）；turn_key 为因果链召回（锚 agent_output 卡片回走重建“怎么做的”）；RecallAgent 收编为 orchestrate 分支内部编排引擎（trace 等多跳）。**收敛三张脸**：模型侧不再区分 `memory_recall`、`memory_turn`、recall 子 agent——单一 `recall` 工具：`items`→票据直达（零 LLM）/ `turn_key`→因果链重建整轮 / `query`→工程检索 / `orchestrate: true`→LLM 多跳编排保留形态（未接线时返回明确指引，不静默降级）。确定性形态永不进 LLM 路径；输出协议不变（`{key,type,summary,content,time}` 条目）；`memory_recall`/`memory_turn` 注册名退役（内部实现保留为路由目标）。

### 3.4 写入全景与装饰链

两个生产者归一到**同一条 `StoreEvent` 写入路径**（EventBus 注入经 `persistBusEvent`；框架 LLM 事件经 `MemoryPlugin.OnEvent`），因而只有一组写入不变量需要守护；压缩折叠的 compaction 事件是第三类合法写入——它记录折叠本身，不产生事实事件。窗口与 seq 的分配住在内存态 `PartitionState`（`sync.Map`，按 pid 惰性创建），这是“槽位分配”的唯一权威，也是恢复链路的关键。因果链（RelationStore）与投影（SessionProjection）是写入的**旁路产物**，不参与事实链本身；事实链只在 KV 里。投影重建是同一 fold 的全量入口：`RebuildProjectionFromWAL` 在启动期（先于 spill 重放）从事实链回放复原（compaction snapshot + 尾部），与运行期增量 `Add` 等价。

<a id="engine-overview"></a>
**装饰链（冻结契约 C2）**：`resolveMemoryStore → wireMemoryEngine`（配置 `memory.engine` 时包 engineBridge；未配置原样返回=零行为变化）`→ ErrorTrackingStore`（DegradationManager 启用时最外层包裹，内含 MemSpill）。顺序固定：**错误追踪在最外层、引擎桥在中间、事件存储在最内层**；错误追踪必须在最外层，否则它看不见引擎与存储层抛出的失败。边界能力接口（引擎提供者、KV 提供者、向量移除器）定义在被缝合的两侧共同依赖的核心包中，而不是引擎包内。

<a id="engine-bridge"></a>
### 3.5 装饰器必须递归透传的能力

KV 后端、WAL 隔离计数、保留租约的保护/释放与 arm、登记屏障（begin/end hold）、关系存储、事件回放——任何一项不透传，都会在包裹链外形成"能力突然为 nil"的静默降级；内层若无该能力则这些调用是 no-op。装饰器同时保证：未接线引擎时内层 store 行为逐字节不变。

<a id="engine-wiring-gates"></a>
### 3.6 接线判定：三扇门在装配期闭合

| 配置与回调 | 得到的 store | 可观察判据 |
|---|---|---|
| 无 `engine` 或无 `embedding`，且没有 store 事件回调 | **内层 store 本体**，一字未改 | 它不是引擎提供者 |
| 有事件回调而无向量能力 | 仅容量包裹 | 事件照常被回灌，检索只有关键词路 |
| 有向量能力但引擎构建失败（密钥未配、backend 名不认识） | **降为仅容量包裹，不报错** | 不再是引擎提供者；故障不阻断 agent 构建 |

增强能力的故障只关掉增强本身：既不"构建失败就把 agent 打死"，也不"静默留下一个每次调用都失败的引擎"——前者让故障传染主链路，后者让故障不可观测。

<a id="bridge-write-replay"></a>
### 3.7 写入、回放与索引的关系

- `StoreEvent`：先写内层（同步点语义不变），成功后才旁路投递引擎索引；索引投递非阻塞、失败只计数，**写入主链路永不因索引失败而失败**。
- 容量触发计数（巩固容量的入口）在写入成功后旁路调用，与引擎是否接线无关；该回调**只在装配期设置一次、运行期只读**，因此无锁也是安全的。
- 回放路径只在**真正写入或修复**了事实时才计数并投递索引；结果为"已提交"时两者都跳过——否则会重复增加实时计数，且引擎侧并不保证重复索引是幂等的。
- `StoreEventWithEmbedding`（调用方自带向量）**不经引擎索引**：MVP 引擎只索引经 `StoreEvent` 的文本嵌入，以保持嵌入模型一致性。
- `DeleteEvent` 删内层后尽力从引擎移除，防悬挂召回；物理遗忘的删除回调同样同步移除内存索引与 KV 持久向量。

<a id="store-instance-sharing"></a>
### 3.8 MemoryStore 实例共享与 path 语义

`resolveMemoryStore()` 按 `MemoryConfig.Type` 选择实现：`memory`/空 path → 新建隔离实例，非空 path 按注册表去重（同 path → 同实例）；`file` → FileSegmentStore + RustViking CLI + 生命周期组件；`localfile` → FileSegmentStore + LocalFileKV + 生命周期组件；后两者同 path 复用已注册实例。**为什么三类后端都必须共享**：同一份物理存储上各建实例，生命周期组件也会各起一套——两个 Compactor 基于各自视图并发写同一批 KV 键，关系图同样各自独立，跨 agent 的 `read_namespaces` 因此读到分歧的因果链。共享实例把"一份存储、一套生命周期"钉成事实。**热重建壳按 agent 身份借用**：换代构造的执行壳不新建存储，而是从常驻身份绑定表取该 agent **自己**那一份；存储租约始终归常驻层，壳不获取也不释放。

`path` 在三种类型下均表示"存储定位符"：`file`/`localfile` 是文件系统目录；`memory` 是逻辑标识符，同 type + 同 path → 单例。

<a id="tombstone"></a>
### 3.9 墓碑与级联父引用修复

物理遗忘一个事件时，它的子事件不能悬空指向已不存在的父。墓碑集记录"已被合法删除"的事件键（内存驻留，并持久化到 KV 以便崩溃后重建），删除时按固定顺序做四件事：

1. **先把该键标为墓碑，再做任何级联修复**。顺序是承重的：若先修复，`findAliveAncestor` 会把这个"正在被删"的键当作存活祖先返回，级联结果仍然是错的。
2. 遍历其子事件，为每个子找**最近的存活祖先**并重设父引用；**找不到存活祖先时子事件成为根**（父置 0），而不是保留坏引用。
3. 摘除该键自身的关系；父链遍历带访问集合，**环不会导致死循环**。
4. 持久化该墓碑键。

| 面 | 契约 | 理由 |
|---|---|---|
| 无 KV 后端 | 跳过持久化，仅内存生效 | 内存模式合法存在，不得为此报错 |
| 变更是否需落盘 | 由脏标记表达；快照加载与 KV 重建后清零 | 避免每次启动都无谓写盘 |
| 崩溃后恢复 | 启动扫描 `tomb` 前缀重建集合，忽略非本类型与零键 | 墓碑必须跨进程存活，否则遗忘可被复活 |
| 压实之后 | 已被压实吸收的墓碑成批移除（含 KV 键） | 否则墓碑只增不减 |
| 回放遇墓碑 | 判为"已合法遗忘"并拒绝复活 | 与重复/冲突、I/O 失败三者互不相同 |

<a id="curation"></a>
## 四、压缩固化与记忆策展

### 4.1 卡片序列（压缩历史的唯一表示）

被压缩历史住在滚动 summaryRef（负 key `context_compress` 引用）里：`[Compacted N] + 卡片行序列 + recent keys`。卡片行由 `extractCardLine` 从边界事件（external_input/agent_output）工程化提取，冥想产出带 ★ 高亮；跨轮由 `buildRetainedRefs` 吸收合并（计数累计/卡片继承/时间下界继承）；超限由 `curateCards` LLM 整理（输出单行化防解析丢行），无模型则最旧行沉底为 `(earlier n items)` 计数。解析正则行锚定（卡片行含用户可控文本，防注入）。

成本律：定级与票据层纯工程零 LLM，开销 O(新增段) 与历史总量无关；LLM 仅两处低频叠加——L3 滚动综述 `synthesizeRollingNarrative`（每轮折叠 1 次，单行 `〔历史综述〕`，编译期常量限长）与卡片超限浓缩（`condenseCardLines`），均无模型/失败时降级纯工程。

### 4.2 三层摄取模型与可追溯提示

| 层 | 内容 | 成本/定位 |
|---|---|---|
| 内联（基座） | 边界卡片：external_input 意图 + agent_output 结果（`[evt_key]` 票据） | 低冗余、高价值 what/result，直接入窗 |
| 按需（精确） | `recall(turn_key=…)`：锚 agent_output 卡片沿因果链回走到 external_input，取回该轮被 L1 丢弃的 thinking_plan/action_command | 低频 how，不占窗；被丢弃事件从未离开 MemoryStore，因果链独立于压缩 |
| 可选顶层（可读） | LLM 文摘：`condenseCardLines` 只读卡片生成浓缩梗概 | 长历史可读性叠加层，不替换卡片基座 |

`buildRetainedRefs` 压缩时统计每轮被丢弃的 tool 步数，L3（整段离场）回合的 agent_output 卡片追加“含 N 步工具调用，可用 recall(turn_key=…) 追溯”——计数只在 agent_output（回合结束）重置，不被回合中途 bus 注入的 external_input（task_settled 等）误清零。

### 4.3 滚动摘要常驻与指数定级

**滚动摘要豁免 L3（常驻可见）**。滚动摘要 ref 被 prepend 到投影最前→渲染成第一条消息→落进**段0**（与第一回合同段）。段0 段龄最高、最先升 L3，`applySegmentLevel` L3 返回 nil 会把**摘要消息随段0 一起丢出模型上下文**——ref 还在投影里（`buildRetainedRefs` 每轮重建），但消息死了。修复：`compressSkeleton` 用 `splitRollingSummaryMessage`（类比 `SplitSystemMessage`）把领先的 `context_compress` 消息摘出、不参与分段/定级，压缩后无条件回填到系统消息后——**滚动摘要永远可见**。`buildRetainedRefs` 不受影响（负 key ref 仍被吸收重建，投影照常携带）。

**指数定级（缓存复用）**。`deterministicLevel` 段龄阈值从线性 `{k, 2k, 3k}` 改**指数 `{k, 2k, 4k}`**（边界 = keepRecent×2^level，底数固定 2）。段在每个级别驻留更久（L2 跨度从 k 翻倍到 2k）、被折叠进滚动摘要的频率降低→滚动摘要（前缀）与段重渲染变化更慢→**LLM 前缀缓存复用率提升**。代价：retained 段略增（~4k vs ~3k），预算内。

**配置公式化**。以 `max_tokens`（M）与 `keep_recent_tasks`（k）为主变量，其余派生：`threshold = compress_threshold × M`、`recent_full_count = 4k`、定级边界 = `{k,2k,4k}`、`card_max_chars = M/20`、`compact_keys_listed = card_max_chars/200`。未显式设置时用公式默认，显式设置优先。

### 4.4 容量单维触发与渲染冻结

**容量单维触发**。整理（compaction）只由 `usedTokens > compress_threshold × max_tokens` 触发；未超阈轮 pass-through（不折叠、不定级、不重建 refs）。`keep_recent_tasks`/`recent_full_count`/定级边界全部纯化为**整理后状态参数**。小内容长会话 refs 缓慢增长是接受的行为变化（无容量压力时保持原文即最高保真；token 终将随 L2 骨架驻留累积触达阈值）。轮数维度（`completeTurns > keepRecent`）已退役：稳态下它每轮触发整理、与前缀缓存复用冲突。

**渲染冻结（整理边界锚定）**。全文窗口在整理轮锚定为边界 key（最近 `recent_full_count` 条 retained 正 key refs 的最前一条），整理间冻结：新追加事件凭 Snowflake 单调 key 天然全文（活跃前沿），旧 refs 渲染方式不变——未触发轮的消息序列公共前缀字节级相同。折叠（`foldToolRuns`）同步移入整理路径：整理间老化工具对按 EventSummary 渲染（有界且稳定），折叠是“整理动作”而非持续维护。

### 4.5 工具链折叠（进行中段有界化）

**问题**：进行中段（当前 ReAct 循环）恒 L0 不压缩，长研究任务的工具调用历史无界累积，其中纯工具调用的 thinking_plan（content 空）老化后退化为零信息占位符。

**D1 空摘要根治**：`GenerateEventSummary` 对纯工具调用 thinking_plan（`Content==""` 且 `ToolCalls` 非空）在**存储时**生成 `调用 <工具名>` 摘要（工程提取、零 LLM）；带散文的 thinking_plan 仍取原文。

**D2 工具链折叠**：`foldToolRuns` 把**老化区（full=false）连续 ≥2 条工具事件**（thinking_plan/action_command，不被 external_input/agent_output 打断）折叠为一个负 key 的 `tool_chain` 合成引用：

```
- 工具链: read_file→grep→edit_file（3步）[evt_first→evt_last]
```

工具名取自 ref.EventSummary；`[evt_first→evt_last]` 是召回票据，`recall(turn_key=evt_last)` 沿因果链可取回被折叠的完整工具链（工具事件本体永在 MemoryStore，I4）。**相邻链合并**：新一轮老化对与尾部已有链连续时合并为一（扩展名/步数/票据），一轮内收敛为**每段连续工具序列一条链**而非每轮一条。**归档退役**：`buildRetainedRefs` 仅保留消息存活的链（`retainedChainKeys`）；段被 L3 归档后链行退役，不滞留为僵尸 ref。**活跃前沿保护**：折叠只作用于老化完整对；最近 `recentFullCount` 条、未完成 tool_call、边界事件不折叠，原生配对合法性不破。

**五项不变量**：I1 有界（进行中段工具历史 O(链行数)，与循环长度解耦）、I2 稠密（无零信息占位符）、I3 锚定（滚动摘要常驻）、I4 无损（工具事件本体永在，票据可召回）、I5 原生前沿（活跃前沿保持原生）。

### 4.6 settle 输出转储与文案票据化

**超大内容的本体是文件，不是事件**——产生时转储（防进上下文）、记忆只持有界内容+票据（防召回复发）、read_file 分页（防读回爆炸）。结果超阈值（`MaxTokens/2×4` 字符）→ 全文写 `tool-output/task-<id>-<ts>.txt`，事件 Content = 尾部 2000 + 文件路径票据，**事件本体有界**；全文经 `read_file(start_line, num_lines)` 行级分页消费。`get_task_result` 工具退役；list/cancel/relaunch/resume 保留。

**文案票据化**：框架注入文案只发“什么在哪儿”（task id / evt key 票据），不广告工具名（装配因 agent 而异）：“怎么取”归工具声明——后者天然与装配一致，提示-能力脱钩被结构性消灭。

### 4.7 证据门控巩固的写入面

`memory_consolidate` 工具在**服务端**计算源事件收据 SHA1 指纹（LLM 不可伪造——指纹不进 prompt，回放时 `VerifyConsolidation` 重算比对）；consolidation 事件经注册表注册（TTL 豁免）；源事件后续墓碑 = 诚实衰减（不阻止，追溯留痕）。诊断（`memory_health`）读实时引擎状态而非死计数器。类型曲线（Role/低价值/骨架/TTL/可嵌入/可召回）唯一权威源为事件包 `registry.go` 的 EventTypeSpec 注册表——`lifecycle` TypeTTL、compaction 低价值清空、嵌入选择性生成全委托/派生自它（"加一个类型只改注册表一处即全链路生效"）；策展与治理两类记录按注册表声明豁免遗忘，逐类理由见[事件篇 12.7](../event/event-architecture.md#type-rationale)。

<a id="engine-contract"></a>
## 五、引擎、扩展与诊断

### 5.1 三条可替换面

| 路径 | 目标 | 实现 | 接线点 |
|------|------|------|--------|
| A. KV 存储后端 | 换持久化底座，保留事件/检索全套语义 | 记忆包 `kv/` 子包新文件实现 `memory.KVStore`（Put/Get/Delete/Scan/Range/Batch） | 根包 `resolveMemoryStore` 加 case + `MemoryConfig.Type` 校验登记 |
| B. 语义检索引擎 | 换向量/混合召回 | 记忆包 `engine/` 子包新文件实现 `memory.MemoryEngine`（票据两段式、Ready 门控退化关键词、遗忘联动 RemoveVector） | 根包 `buildMemoryEngine` 加 case + `memory.engine` 配置 |
| C. 嵌入器 | 换/加 embedding 供应商 | 实现 `Embedder` 接口（或复用 zhipu/mock/traced） | `memory.engine.embedding.provider` 路由 |

**约束红线**（A/B 共享）：白盒测试文件不得 import 适配器子包（测试 import 环）——跨包协作测试一律黑盒（`package memory_test`）；适配器失败永不传染主链路（降级为关键词/透传）。分包原则：`KVStore`／`MemoryEngine`／`Embedder` 三个契约都定义在核心记忆包，实现居子包，避免子包反向依赖导致成环；新增实现不需要改契约。tagent 记忆有两条相互独立的拓展路径，共同前提：**核心包的存储／压缩／事件代码都不需要改**——`resolveMemoryStore` 与 `buildMemoryEngine` 是唯一的两个接线点（都在根包）。

<a id="extension-paths"></a>
路径 A 的 `KVStore` 语义约束：键为字符串（键格式由 `key_schema` 单点定义，含 evt/idx/meta/tomb 四类）；`Scan`/`Range` **按字典序**返回；`limit <= 0` 表示不限制。路径 B 的契约红线（四条，缺一不可）：只返回**排序票据**（两段式，全文由调用方水合）；`Ready()` 为假时**退化到关键词而不是报错**；遗忘必须联动 `RemoveVector`；引擎报错不得传染写入主链路。

<a id="embedder"></a>
<a id="embedder-contract"></a>
### 5.2 Embedder 契约

`Embedder` 是文本→向量抽象，三条约束：`Embed` 返回与输入**等长且顺序对应**的切片；`Dimension` 为 0 表示尚未探测；`ModelID` 用于索引指纹比对，以防换模型后新旧向量混用。实现必须尊重 ctx 取消/超时——排空与回收路径依赖它。未配置 key 时返回 error，调用方按"功能关闭"优雅降级为关键词检索。

| 供应商面 | 契约 |
|---|---|
| 真实供应商（zhipu 兼容端点） | **未配置即关闭**：密钥为空直接返回 error，不静默构造必败实例；**分批**：超上限按批切片顺序拼回，请求可能乱序，**必须按 `index` 还原**，任一 index 缺失即整批不可信并报错；**重试分类**（只重试一次）：网络错误、`429`、`5xx` 可重试，其余 `4xx` 与解析错误**不可重试**，ctx 已取消不重试；**失败不外溢**：某批失败只影响该事件缺向量；真实端点实测 opt-in，无密钥即 Skip |
| mock | 文本哈希映射到固定维度的**确定性伪向量**：相同文本必得相同向量、共享词元越多余弦越高。只验证机制（融合、过滤、降级），**不承诺任何真实语义质量**；零值实例必须仍可用 |
| traced | ①**可观测只落在嵌入层内部**：span/metric 都在装饰器里产生，工具/引擎的 `Declaration` 零触碰，否则扰动 prefix-cache；②**未配置导出时真正零开销、行为逐字不变**：属性只携带元数据，**嵌入文本内容绝不进 span** |

**已裁决**：嵌入走 tagent 侧的 HTTP 供应商，而**不是** rustviking CLI——后者的向量索引是进程内易失的，不能当持久后端。

<a id="vector-persist"></a>
### 5.3 向量持久化与启动重建

向量存的是**持久 KV**（不是进程内易失索引 CLI），序列化时随向量一并存 `分区/类型/时间戳` 与**嵌入模型指纹**：

- 重建无需回读事件即可恢复过滤维度；换模型或换维度后，指纹不匹配的旧向量在重建时**跳过**并计数——否则会产生维度不匹配的零分候选与跨语义混用。
- 启动重建为异步扫描，不阻塞构造；重建期间向量**渐进可用**，生产检索不等待。扫描条数有上限（覆盖万级事件规模），底层 scan 默认条数很小必须显式传大值。
- 重建出的向量数计入"已索引"；损坏或不可解析的键跳过并计数。
- KV 持久失败只记日志加计数，**绝不传染**索引与检索主链路：向量是增强索引，不是事实来源。
- **向量索引的持久性取决于是否配置 KV**：未配置 KV 时向量仅存内存，重启后旧事件向量丢失，关键词路仍正常工作（事件本身由存储持久）；配置 KV 后向量随 flush 序列化入 KV 并在启动时重建。
- **索引按 EventKey 幂等**：同一 key 重复投递只覆盖那条向量，不产生第二个逻辑条目。
- **`Index` 的三条静默过滤**：已关闭或无嵌入器（纯关键词降级）、非正 EventKey（合成投影引用不入索引）、事件类型未注册为可嵌入（选择性生成，由事件类型注册表决定）。三者都返回成功——索引投递是尽力而为，绝不把失败传染给写入主链路。

<a id="diagnostics-realtime"></a>
### 5.4 健康度诊断必须读实时状态

记忆健康度按维度度量（向量索引健康、检索能力、存储规模、WAL 隔离行数），原则是**读引擎与 store 的实时状态，而不是维护一套平行计数器**——平行计数器会与真实状态分叉，从而给出好看但假的健康度。指标经一个具名的可选契约一次读取（而非多处匿名接口断言），签名漂移会变成编译期错误。索引健康率 = 成功索引 / (成功 + 丢弃 + 嵌入错误)；无数据时记为 1.0。引擎与 store 都可以缺失（对应维度省略），诊断器本身可为 nil。

### 5.5 原始向量 API 的分区风险与 `Close` 归属

`SearchByEmbedding` 以**空分区白名单**调用向量检索，语义是**全库检索、不按命名空间过滤**——因为该原始向量 API 不携带分区上下文。目前无生产调用方；若要接入且需命名空间隔离，必须改走携带分区集的检索接口或由调用方注入分区集合。`Close` 是 store 关闭链的**委托腿**，不持有共享释放权：独享场景由 agent 的关闭尾部触发；共享场景真正的回收是经资源注册表关闭同代的引擎与底层 store；引擎自身的 `Close` 幂等。

<a id="inmemory-retrieval"></a>
### 5.6 进程内引擎的检索路径与已知取舍

`InMemoryEngine` 是无外部依赖的轻量实现，用于开发、测试与降级；每条检索路径都有明确的退化规则，不存在"静默返回空"：

| 输入形态 | 走法 |
|---|---|
| 无查询词（纯过滤/浏览）或显式要求关键词 | 直接走 store 的关键词路 |
| 显式向量模式有结果 | 返回向量结果；**无结果则按契约退化为关键词**，不报错 |
| hybrid 且向量零命中 | 直接纯关键词 |
| hybrid 且向量有命中 | 关键词 ∪ 向量做 RRF 融合 |

四条不可忽略的细节：

1. **候选数取超取补偿与配置 topK 下限的较大者**：向量/关键词各自的 topK 配置必须真正生效，否则融合前就已被截断。
2. **关键词腿按时间倒序排名，不是相关度**：因此 RRF 融合结果在关键词侧**偏向新近事件**；向量腿才是余弦相关度秩。这是 MVP 的已知取舍——词面相关度列为后续增强。
3. **重建与关闭的边界**：启动重建跑在独立 goroutine，**不进入 worker 的等待组**（否则 KV 后端挂住会让 `Close` 无限阻塞）；重建期间引擎 `Ready` 为假，检索退化到关键词路，不返回半成品结果。
4. **退出时排空队列必须用独立、不继承取消、带超时的 context**：沿用被取消的 ctx，尊重取消语义的嵌入器在排空期必然失败，结果就是丢掉在途向量且这些向量也不会持久化。

**RRF 融合**：`score(d) = Σ_lists 1/(k + rank)`，`rank` 从 1 起，`k` 默认 60（业界惯例）；**同分决胜取更大的 EventKey**——Snowflake key 随时间单调，即新事件优先。嵌入与索引的后台化是同样的原则：写入主链路只投递不等待，失败只计数与记日志。

<a id="local-file-kv"></a>
## 六、KV 后端契约（LocalFileKV 与 rustviking CLI）

### 6.1 `LocalFileKV`：仅够跨进程验证的临时后端

落盘模型（仅验证级）：写先进内存 map，进程内读恒一致；`Sync()` 是屏障，只序列化自上次屏障以来的脏桶。每个桶文件以临时写 + POSIX 原子 rename 替换，进程被 KILL 不会留下撕裂快照——重开必见该桶最后一次成功 Sync 的状态。它是**故意简陋**的模型：内存 map ＋ 按命名空间分桶的快照文件；不提供生产级持久性、安全性与长期可维护性保证。

| 语义 | 规则 |
|---|---|
| 写入可见性 | 写立即进入内存 map，故**进程内读永远一致**；对**新进程**可见必须跨过 `Sync()` 屏障 |
| 快照原子性 | 落盘走 tmp＋rename；POSIX rename 原子 ⇒ 进程被 KILL 也不会留下半张快照，重开必然看到最后一次成功 Sync 的状态 |
| 无 fsync | 明确**不做** fsync：能挺过进程重启/重开（这正是被验证的保证），**挺不过操作系统掉电**。未 Sync 的写在重启时丢失——诚实的"仅 flush"语义 |
| 返回值含义 | `KVPut`/`KVBatch` 返回 nil **不是**持久性保证；只有 `Sync()` 成功返回后才谈得上持久 |
| 空脏检查 | 无变更时 `Sync`/`Close` 不重写快照；`Close` 幂等并把未落盘变更 flush 完 |
| 遗留 tmp | 打开时清掉上次被杀在 rename 中途留下的 tmp |

**快照编码的派生缓存（`bucketCodec`）只加速，不是第二真源**：每桶驻留已转义的片段与可复用输出缓冲；值变更即失效重编码；单个键值对合计达到 64 KiB 就不驻留；全部桶驻留总量以 8 MiB 为界。这份缓存永不落盘，也不重排屏障顺序——键序每次从活 map 重算，因此输出字节与完全不使用缓存时逐字相同（`TestLocalFileKV_SnapshotEncodingEquivalence` 钉的就是这条等价性）。扫描限定单桶的判据：前缀含冒号时才敢限定（首个冒号使命中键必属该桶）；不含冒号的模糊前缀可能跨桶，退回全量扫描。

<a id="kv-partition-discovery"></a>
### 6.2 分区发现依赖键命名空间

`ListPartitionIDs` 从键前缀解析分区：`{pid}:evt|idx|meta|tomb:…`——**某分区命名空间下存在任意持久化键，即证明该分区存在**；非分区命名空间（如 `global:*`）与不可解析前缀一律忽略。冷分区发现靠这条事实，而不是另建一份分区清单（那会与键空间分叉）。

<a id="rv-cli"></a>
### 6.3 rustviking CLI 接口约定

- 统一 JSON 信封 `{success, data, error}`；退出码 0=成功、1=用户错误、2=系统错误；请求体经 stdin 传入（批量操作）。
- **取不存在返回 null value 而非报错**，因此包装层必须把 null 翻译成**类型化的 key-not-found**，使调用方能把它与 CLI/传输层 I/O 故障区分开——否则一次进程启动失败会被误读成"键不存在"。
- 前缀扫描返回嵌套结构 `{count, entries, prefix}`；扫描**字典序**排序，`limit` 在排序之后截断（mock 后端必须同样排序，否则契约在测试里被悄悄放宽）。

`KVRange` 是模拟出来的：CLI **没有** range 原语，实现取 `start`/`end` 的最长公共前缀扫描后在客户端过滤；**当二者无公共前缀时直接报错**——绝不退化成空前缀全量扫描（那是隐形的全库扫描）。需要大范围扫描时应显式走 `KVScan`。

<a id="rv-vector-cmds"></a>
### 6.4 向量命令的真实形态与一处未接线能力

真实的向量命令是 `index insert|search|delete|info`（**不是** `vector *`，也不存在 `embed` 命令）；向量以逗号分隔的 f32 传参；`index search` 返回 `{query_dimension, count, results:[{id, score, level}]}`。历史上包装层曾按虚构的 `vector insert/search/embed` 契约实现，那是向量检索永远退化为 stub 的根因。

`VectorInsert`/`VectorSearch`/`VectorDelete` 目前**无生产调用方**，属预留能力：MVP 的向量持久化走 KV 序列化＋启动重建（原生 `index` 是进程内易失索引，不能当持久后端）。两条使用前的硬前提：① `level` 参数语义**未经真实二进制验证**，当前显式传 0 会偏离 rustviking 默认 level=1，接入前必须实测 0/1 的索引结构差异；② 原生 index 与 KV 重建是**两条互斥的持久化路线**，同时启用会让向量出现两个真源。

<a id="typed-errors"></a>
## 七、判据表：错误分类与归因处置

### 7.1 类型化存储错误：四类必区分，绝不塌缩

调用方必须能区分"键确实不存在"与"存储 I/O 失败"。把两者塌缩成一个，等于**把一次故障伪装成一次空召回**，把一次恢复失败伪装成"这条链本来就没有"——两者都会静默产生错答案，而不是响亮地产声错误。四类语义各不同，处置也不同：

| 错误 | 何时产生 | 调用方处置 |
|---|---|---|
| 键未命中 | KV 后端确认键不存在 | 当作缺失继续（其它任何错误都**不得**当成缺失） |
| 事件键重复 | 写入时该键已提交 | 按**幂等成功**处理——事件键就是事件的身份，覆盖一律被拒 |
| 事件已被合法遗忘 | 回放时该键上有墓碑 | **绝不复活**；与"重复/冲突"和"I/O 失败"三者互不相同：保留恢复材料、不要 ack，与冲突同样处置 |
| 事件受保留租约保护 | 删除时仍有未确认恢复的归属方需要原文 | 显式拒删且**不破坏记录**（无损搬迁仍允许，只有销毁被拒）；租约释放后重试 |

spill 重放只走 canonical 路径：`ReplayWithNotify` 依赖 store 的 `EventReplayer` 契约，对着耐久事实链原子区分「新提交 / 孤儿修复 / 已提交」。未实现 `EventReplayer` 的 store 被拒绝且 spill 原件保留；读侧弱回退不成立（GetEvent 命中只证明可读，不证明提交屏障与索引发布完成）。key 释放严格排在 durable 重写之后。

<a id="error-tracking"></a>
### 7.2 退化检测装饰器：归因矩阵

存储链的**最外层**是错误追踪装饰器。用装饰器而不是在插件里就地处理，是为了**单一挂点**——否则"上报方"与"降级状态机"两处各写一套判定，迟早分裂。它同时把内层全部方法（含可选接口）原样透传，只在出错时按特征归因到依赖并旁路上报。

**门控关闭必须真正零行为**：上报目标为 nil 时**纯透传、不上报**：配置关闭退化检测的部署，行为与未引入本装饰器时逐字节一致。这是"默认关闭即无副作用"的判据，不能靠"应该没影响"来相信。

| 判定顺序 | 归因 | 依据与陷阱 |
|---|---|---|
| 先判 | 磁盘 | 磁盘满/配额类错误必须**先于**向量依赖判：向量后端的 CLI 错误文本常内嵌其后端名，若先按名字匹配会把 ENOSPC 误归向量依赖，**掩盖磁盘满**——而两者的降级动作不同 |
| 次判 | 向量后端 | 匹配面**收窄到进程派生失败与二进制缺失**（确证是 CLI 起不来），不用宽泛的后端名匹配——否则任何提到该词的业务错误都会被算成向量依赖故障 |
| 其余 | 记忆存储 | 默认归因 |

| 三类"不是故障" | 处置 | 为什么 |
|---|---|---|
| 公共写入路径的重复键 | 原样上抛：**不**上报失败、**不**兜底落盘 | 重复是调用方的契约冲突，不是依赖故障；上报会污染降级矩阵，落盘会把存储已有的事实再投一份 |
| 不支持向量检索 | 视为**能力声明**，不上报 | 否则未配置语义检索的部署一调用就把向量依赖打成 degraded |
| 内部回放失败 | 上报归因，但**不**兜底落盘 | 可靠收件箱已自持 at-least-once 重试；再落一份会在兜底文件里造出重复条目 |

### 7.3 恢复必须被证明，且只在写路径证明

- **写成功** ⇒ 同时上报"记忆／磁盘／向量"三者健康：一次成功写入证明这三条写路径都通。缺了这条，磁盘或向量依赖一旦进入降级就**再无恢复信号**，只能等到重启。对已处于正常态的依赖上报成功只重置失败计数，因此三报安全。
- **读成功** ⇒ **不**上报恢复：读通不代表写依赖已恢复，用它当恢复证据会误退出降级。
- 兜底落盘本身失败（例如磁盘满）只告警：此时事件确实会丢，但退化状态已记录，属可观测的最后一搏，不再叠加重试。
- 启用兜底落盘时必须把保留租约接进兜底文件，并在放行扫描器前按现存条目重建保留集；这一步失败**必须上抛**而非吞掉——吞错会造出"热更成功＋悬空遗忘屏障"的组合，待重放的持久原文可能被扫描器销毁。调用方据此 fail-closed，旧实例继续服务。
- 重放成功路径支持注册投影补写回调，使"存储与投影同点提交"的等价语义在退化恢复路径上仍成立；回调失败或 panic 不影响重放——事件不丢优先，投影可后补。

**状态迁移上报不得写回被包裹的 store**：退化状态迁移本身要落一条 governance 事件。这条落笔必须写到**装饰链之前**的那份 store：写到包裹后的 store 会让写失败再次触发归因与上报，「写失败 → 上报 → 状态迁移 → 再写一次」成了自循环。因此构造装饰器时要向下游交出一个能落到真身的引用，上报路径始终用它。

## 已知缺口与演进方向

| 缺口 | 现状与防线 | 候选方向 |
|------|-----------|---------|
| **压缩老化（摘要丢细节）** | 卡片行沉底为 `(earlier n items)` 计数后，约束/日期类细节只剩 recall 票据可达。防线：票据永不丢（key 保留）、固化物豁免 TTL、L0 边界事件保原文；**执行过程（how）经 `recall(turn_key=…)` 因果链召回** | 沉底前抽取“约束型事实”入固化物；对账测试常态化 |
| **压缩触发 token 估值偏乐观（大上下文溢出风险）** | `DefaultTokenCounter`（`CharsPerToken=2.0`，仅计 `msg.Content`）对**中英混排+代码**长上下文实测**低估 ~15%**（远端语料 ≥8k tok 段 est/real p50=0.846、83% 低估）→ 压缩**晚于** provider 真实上限触发 → **provider 400 溢出风险**；且不含 tools-schema 每请求开销。防线：溢出即显式报错 | 复现：`rl/trajectory_analyze.py [estimator-bias]`；预算线预留 ~15%、tools-schema 开销入模、内容分型/自适应系数 |
| **rustviking 原生向量 CLI 未接线** | 引擎 MVP 走内存索引+KV 序列化持久化；`index insert/search/delete` 为预留后端 | 实测后迁移同库向量后端（引擎侧适配，协议不变） |
| **固化物因果回溯不完整** | legacy L3 归档经 `SetParent` 挂链 + `source_keys` 溯源；骨架路径多段压缩仅产卡片行（无段摘要固化物，溯源靠卡片 [key] 票据）；从"任务结果"反查固化物缺 `task.resultRef` 桥 | resultRef 字段 + RelationStore 反向索引 |
| **LocalFileKV 压实成本** | WAL 已把增量写摊平为 O(ops)；压实时刻仍全量 marshal 且在锁内（4MiB WAL 触发一次） | 分片 snapshot 或锁外压实 |
| **历史脏数据** | 旧 11 位 mask 时代的负 key / 超界分区（如 1167）残留于实机存量 | TTL 自然清退；不做主动迁移（读路径已容错） |
| **压实未按日/周分组** | L2/L3 段名仅是对齐命名，一个“日段”可装跨数天事件——真实边界使其不再影响正确性，但超宽段剪枝粒度粗 | 压实按日/周分组产段 |
| **TTL 扫描成本不受控** | 每周期全量扫描分区内所有事件（O(事件数)/周期）；小库可接受，规模增加后成为瓶颈 | 游标式增量扫描或按段年龄剪枝 |
| **雪花 key 同秒碰撞窗口** | key 的同秒计数器存于内存；重启前后同一秒写入会生成相同 key——已由 `StoreEvent` 碰撞检测拒绝写入防住静默覆写，但写入会失败需重试 | 雪花 seq 随窗口恢复持久化，或 key 格式加随机后缀 |
| **规格与实现系统性漂移（I6）** | `harden-event-storage-for-scale` 0/80 任务未完成即归档，delta 已入主 specs，形成"规格说有、代码没有"的空头契约（已处置：ttl-cursor-scan 撤回、event-lifecycle 改写、seqCounter 改写为轻量语义、StoreEvents 删除） | **归档应加实现核对：tasks 未完成不得同步 delta 入主 specs**；其余存量规格逐条兑现或如实降级 |
