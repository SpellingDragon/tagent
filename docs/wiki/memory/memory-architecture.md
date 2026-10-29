# tagent/memory 记忆存储：链、原文仓与账本

> **活契约页**：逐条对齐当前码面（`memory/`），代码内 `// 契约:` 注释指向本页锚点。分层口径统一于[存储概念地图](../../demos/storage-concept-map.html)：**链**管身份与因果（轻、恒重放）·**原文仓**管字节（重、按需取、可过期）·**账本**管责任（受理/消费/交付）·**其余一切现算**。回放与时光机演示见 [storage-time-travel.html](../../demos/storage-time-travel.html)，术语见 [GLOSSARY](../../GLOSSARY.md)，历史快照见 [PRE-REWRITE-MANIFEST](../../archive/wiki/PRE-REWRITE-MANIFEST.md)。

<a id="overview"></a>
## 一、模块定位与分层

`tagent/memory` 是 tagent 的**事件存储层**：把一条已提交的事实按分区与时间窗落成 KV 键（链），把事实的**消息正文**放进按内容寻址的**原文仓**，把"谁消费了谁"落成追加式**因果日志**。边界只有一条：**本层不认识 agent 语义**——身份到分区的映射由调用方经 `PartitionIDFromName` 完成，本层只按分区号归档。

| 形态 | 落点 | 生命周期 | 立得住的反事实 |
|---|---|---|---|
| **链**（事实本体） | `<pid>:evt:<窗>:<seq>`、`<pid>:idx:<EventID>`、`<pid>:meta:<窗>`（[§五](#file-segment-store)） | 不可变、恒重放 | 没有它：状态无单一来源，回退到任意时刻无从谈起 |
| **原文仓** = blob + ref | `blob:sha256:…` + `ref:<EventID>`（[§三](#event-shape)） | 字节可过期、账目随链 | 没有它：正文进链，装载与重放被重字节拖垮，且无法去重 |
| **因果日志** | `relations/relations.journal`（`memory/ledger/relations_journal.go` 的 `RelationsJournalName`，[§四](#causal-chain)） | 追加、启动重放 | 没有它：因果塞进链，轻链变重 |
| **账本**（任务/回执/交付） | `tagent/task/v1/…`（`runtime/state/durable.go`、`assembly/host_delivery.go`） | 逐条追加、不覆盖 | 没有它：只留终态，任务历史没有时光机 |
| **其余现算**（投影/装配/检索页） | 不持久 | 每次重算 | 没有它：presentation 落盘成第二真源 |

文件清单（不列行数，行数必然腐化）：

| 文件 | 职责 |
|---|---|
| `memory/event_body.go` | 事实本体形态 `EventBody`：编码/解码、`Record`、`Ref`、`Event`、`SameFact` |
| `memory/partition.go` | 分区派生 `PartitionIDFromName` 与授权命名空间展开 `PartitionsOf` |
| `memory/query_events.go` | 声明式查询面 `QueryOptions` / `QueryEvents` |
| `memory/store_query.go` | 观察表：把已 land 的事实装成可查询、可回捞引用的面 |
| `memory/recall.go` | 向量缝 `VectorSeam` 与 `Embedder` 契约 |
| `memory/replay/` | 投影、段龄轴、折叠、因果链、回放预算（全部现算） |
| `memory/search/` | 检索端口 `Searcher`、融合 `Fusion`、真实 store 上的检索实现 |
| `memory/embedder/` | 嵌入器装配期校验与向量生成 |
| `memory/ledger/` | 原文仓（blob+ref）、段存储、kv 分层、因果日志、账本事实面、租约与生命周期 |
| `memory/query/` | 只读诊断投影（装配面诊断读，非存储面） |

<a id="hard-contracts"></a>
## 二、硬契约

| # | 契约 | 含义 | 保障机制（可查证处） |
|---|---|---|---|
| 1 | **事件不可变** | 一个 key 只命名一个事实；写入后的字节永不原地改写 | 同 key 异字节具名拒绝：`ErrFactKeyConflict`（`event_body.go`）、`ErrContentConflict`（`localfile.go` `Put` / `segment_store.go` `StoreEvent`） |
| 2 | **槽位与身份一一对应** | `<pid>:evt:<窗>:<seq>` 里装的就是 `EventID` 指向它的那条事件 | `StoreEvent` 一次 `kvBatch` 同写 evt/idx/meta 三键；`LandEvent` 拒绝 `body.Key != stored.Key`（`ErrBodyKeyMismatch`） |
| 3 | **链上没有正文** | 链只存身份、时间、元数据与原生负载；消息正文只走原文仓 | 写入点 `assembly/facts.go:125`：`tier.Originals.Put(ctx, body.Ref(), []byte(body.Message.Content))` |
| 4 | **声明式查询语义** | 结果 ≡ 全集过滤 → 全序排序 → offset/limit；剪枝与早停只是优化 | 全序键 `(Timestamp, Key)`（`query_events.go` `queryEventRecords`） |
| 5 | **授权 default-nothing** | 空分区集合＝什么都不授权，绝不回落全库 | `QueryOptions.resolvePartitions` 两字段皆空返回空列表；`search.Searcher` 四条红线含同一条 |
| 6 | **越权读作不存在** | scope 外的引用一律读作 miss，连"该 key 存在"都不泄漏 | `ScopedOriginalStore` 三条红线 + `kvOriginalStore.GetScoped` 只在 scope 内匹配（`reference.go`） |
| 7 | **旧格式具名拒绝，不迁移** | 上一版 body 字节按名字拒绝，冷启动重建而非就地猜格式 | `DecodeEventBody` 判据是**顶层是否有 `message` 键**，无则 `ErrEventBody` |
| 8 | **跨批无事务，靠顺序不变量补偿** | `kvBatch` 是原子单位，批次之间没有事务；正确性由"先写什么后写什么"保证 | 段三键同批、`relations.journal` 先落盘后改内存、交付先登记后发送 |
| 9 | **能力诚实** | 非持久档必须自报不持久，不得假装恢复面存在 | `StoreCapabilities.Durable=false`（`memory` 档）+ 装配告警 `storage tier keeps no record across process exit` |
| 10 | **枚举要么全量要么具名失败** | 部分列表不得被当作完整视图来对账 | `LifecycleStore` 红线：`MUST enumerate every key it holds, or fail by name` |

**事实链红线**：任何后台动作（过期、容量、折叠、装配）只能**搬迁、遮蔽或删除可见性**，不得改写正文与身份。压缩只改发给模型的消息视图，`memory/replay` 的投影是链的纯回放：`Fold` 只替换它消费的条目并铸造产物，装配按预算决定一个条目怎么呈现，两者都不写链。

<a id="event-shape"></a>
## 三、事实形态：`EventBody`、blob 与 ref

一条事实只有**一份**记录形态 `EventBody`；它自描述——解码回查询记录、回捞引用与原事件本体，重启后不需要第二份注册表。

| 字段 | 含义 | 契约要点 |
|---|---|---|
| `Key` | 事件规范身份（hex 拼写） | 与存储键一致，不一致即 `ErrBodyKeyMismatch` |
| `Type` | 冻结事件类型 | 取值见[事件篇](../event/event-architecture.md#const-list) |
| `Timestamp` | 事件时间（Unix 毫秒） | 语义时间轴；排序/窗口/过滤只认它 |
| `Owner` | 该引用登记的地址 | 与 `Format`、`RetentionPolicy` 一起构成 ref 的 scope |
| `Format` / `RetentionPolicy` | 原文格式 / 保留策略 | 现网提交值 `text/plain` / `keep`（`assembly/facts.go`） |
| `Meta` | 元数据投影（原样保留整张表） | `event_summary` 就在这里，**不另存摘要字段** |
| `Message` | 原生模型负载（角色/正文/工具调用/推理） | 正文的唯一权威副本；摘要不从它派生 |
| `Source` / `Target` | 来源与投递地址 | 参与 `SameFact` 的通配比较 |
| `InvocationID`…`ApprovalID` | 血缘标识 | `CallID` 的重复判定范围＝`(InstanceID, InvocationID, DecisionID, CallID)` |

三条派生规则（都是"不存第二份"的直接后果）：

- `Record(partitionID, windowTS)`：摘要从 `Meta[MetaKeyEventSummary]`、正文从 `Message.Content` **读时派生**，所以记录永远等于事件自己说的话。
- `Ref()`：返回 `{Key, Owner, Format, RetentionPolicy}`，**不含 Integrity**——完整性由原文仓对落盘字节计算，派生引用不自带完整性声明。
- `Event()`：逐字段还原；字节没带的字段留空，绝不回填。
- `SameFact(other)`：清掉提交协议自己盖的戳（时间戳、`invocation_id`、`instance_id`、`format`、`retention_policy`）后比对事实本体，`Source`/`Target`/`TaskID` 按"一方未声明即通配"两两比较。**同一次事实的重复提交是幂等成功，同一个 key 换一份内容才是冲突。**

### 3.1 原文仓：blob + ref

两件部件、一个职责，对外只有一个名字：

| 部件 | 键 | 内容 | 判据 |
|---|---|---|---|
| **blob**（内容寻址字节仓） | `blob:sha256:<hex>` | 正文原始字节 | sha256 即地址，同字节只存一份（天然去重） |
| **ref**（引用账目） | `ref:<EventID>` | `{owner, format, retention_policy} → {digest, length, stored_at}` | 一条 ref 一份授权；读取只能经它进入 blob |

| 写入/读取规则 | 行为 | 理由 |
|---|---|---|
| 首次提交 | 一次 `kvBatch` 同写 ref 与 blob，屏障成功后才发布到内存态 | 账目与字节同批可见，不会只剩半条 |
| 同 key 同 scope 再来一次 | 幂等成功（`return nil`） | 重放与重试不制造第二个事实 |
| 同 key 不同字节 | `ErrContentConflict` | 一个身份只站一个事实 |
| 同 key **第二个 scope** | ref 记录里 join 一条账目，共享同一份 blob | fork / 转世后血缘册共用字节，不复制正文 |
| 读取（`GetScoped`/`ReadSegmentScoped`） | 只认 scope 内的账目；越权读 = `RecallMissing`，并 `verify()` 复算 digest | 零泄漏：连存在性都不给 |
| 缺失/过期/失败 | `RecallMissing` / `RecallExpired` / `RecallFailed` / `RecallFound` 四态分开 | 把 I/O 故障伪装成"空召回"会静默产生错答案 |

**Retention（在册未接线）**：`memory/ledger/retention.go` 的租约是原文过期的唯一判据——`Register` 经 guard 持有引用计数（首登记时间不被 re-hold 重打戳）、`Retire` 需出示当前 lease（否则 `ErrLeaseMismatch`）、`Held` 转问 guard 的 `Protected`；`lifecycle.go` 自述的语义是"过期键只标墓碑、物理销毁留给它自己的压实阶段"，且受保护的原文永不被标。当前装配面**没有**挂接 `Retention`/`Lifecycle`（`skeleton_assert.go` 只钉签名），因此线上没有过期执行面，原文随链存续；`retention_policy` 的提交值也只有 `keep`。接线时判据已就绪，缺的只是装配决定。

### 3.2 写入路径：谁把正文放进原文仓

唯一的生产写入点是 `assembly/facts.go` 的 `persistFact`，顺序是承重的：

| 步 | 动作 | 判据 |
|---|---|---|
| 1 | `Segments.GetEvent(key)` 预检 | 已提交且 `SameFact` → 幂等成功；已提交但内容不同 → `ErrFactKeyConflict` |
| 2 | `Originals.Put(ctx, body.Ref(), []byte(body.Message.Content))`（`assembly/facts.go:125`） | **正文先落进原文仓**：段里出现的 `EventID` 一定有可读原文可指 |
| 3 | `Segments.StoreEvent(...)`，`partition = PartitionIDFromName(Owner.AgentID)`、`window = Timestamp.UnixMilli()`（负值具名拒）、`seq = landedSeq[partition]+1` | **段提交才是持久写**：它不成功，事实就不算落地，`landedSeq` 不前进 |
| 4 | `source.LandEvent` + `seam.Submit` | 提交成功之后的**后果**：失败只告警，绝不回滚已落地的提交 |

两条由此可查证的结论：①无存储档（`tier == nil`）或事实没有 `Owner.AgentID` 时，事实只留在内存，不假称持久；②第 2 步成功而第 3 步失败会留下一条无人引用的 ref/blob——重放同一条事实会复用同一 digest，因此不会堆积第二份字节，但清理面不在本层（见[§十二](#gaps)）。

<a id="causal-chain"></a>
## 四、因果：`relations.journal`

因果边不写在事实里（事件不可变，父子边可变），也不写在段里，而是根目录下**一份追加日志**：

| 规则 | 行为 | 判据 |
|---|---|---|
| 回放形态 | `relations.journal` 每行一条 `{child, parent}` JSON；同一 child 后出现的记录覆盖先前的，空 parent 删去该边 | `OpenMemoryRelations` 逐行回放 |
| 持久判据 | `Append` 写一行并 `file.Sync()` 成功才返回；失败按写入前长度截断回滚 | **返回即持久**——journal 没落盘就不动内存边 |
| 写序 | `SetParent` 在同一把锁里**先追加日志、后移动内存邻接表** | 日志写失败时链保持改动前状态，调用方可原样重试 |
| 打开 | `OpenRelationsJournal` 先裁掉最后一个换行之后的尾行（那次追加从未返回成功） | 尾部撕裂不等于损坏 |
| 损坏 | 不可解析的行 → `ErrRelationsJournalCorrupt`，**拒绝打开** | 宁可不启动，也不带着半条链服务 |
| 自证 | `VerifyReplay` 把回放结果与持有的链逐项比对，不等即 `ErrRelationsReplayMismatch` | 回放不是"尽力而为" |
| 隔离 | 链的身份是 `(PartitionID, SessionID)`；空 session 具名拒绝 | 同分区不同用户不并成一条链 |

隔离（`ChainKey`）是承重的：同一 agent 名的两个用户共用分区但**永远不共用 session**，因果回溯不会被跨用户缝合。投影、段龄轴、折叠都按这条链身份重建，重启后与运行期增量逐字节一致。

<a id="partitionidfromname"></a>
### 4.1 分区派生

- `PartitionBits = 10`：名称经 FNV-1a 折进 **0–1023**，`sync.Map` 缓存，同名恒得同号（`memory/partition.go`）。
- **允许碰撞**——分区是为隔离与寻址，不是为唯一性；碰撞只影响共享分区名，不影响事实身份（身份是 `Key`）。
- `PartitionsOf(id, extra)` 把"自己那一份"排在首，再叠加显式授权，去重且保持声明顺序：**授权只能叠加，不能替代自身命名空间**。

<a id="file-segment-store"></a>
## 五、段存储与键空间

链的物理单位是"窗段"：一个分区一个时间窗一段，窗内按 `seq` 追加。三类键**没有数据库级引用约束**，靠键名格式互相指向：

| 键形 | 值 | 维护者 | 消费方 |
|---|---|---|---|
| `<pid>:evt:<WindowTS>:<Seq>` | `EventBody` 字节 | `StoreEvent` | `GetEvent`、`Events`、`Load` |
| `<pid>:idx:<EventID>` | `window:seq` | `StoreEvent` | 按键取槽位（`GetEvent`） |
| `<pid>:meta:<WindowTS>` | `{PartitionID, WindowTS, EventCount, MinTime, MaxTime}` | `StoreEvent`（`nextWindowMeta` 只加宽、只自增） | 段发现、时间包络、`EventCount` |

`StoreEvent`（`memory/ledger/segment_store.go:210`）的一次提交就是这三键的**同一个 `kvBatch`**：

1. 窗口 `WindowTS` 为负 → 具名拒绝；`seq` 由调用方按分区单调分配。
2. 已提交过同一记录 → `return nil`（重复提交是幂等成功）；同一槽位换一份字节 → `ErrContentConflict`。
3. 提交后**跨过屏障**才写进内存 `cache`/`count`：屏障失败时回滚暂存（`kvUnstage`），内存态不出现"读得到但没落盘"的事件。

`Events()` 的顺序是 `(PartitionID, WindowTS, Seq, Key)`；`Load()` 只在 body、idx、meta **三件齐备**时发布该记录，缺任何一件就当作没提交过。`SegmentRequest` 按字节切读（`DefaultSegmentMaxBytes = 64 KiB`），越界与 EOF 分开报。

**线上没有分层压实、没有墓碑执行、没有 TTL 扫描**：分层压实/TTL/回收这一整套仍在 `lifecycle.go` 里在册（其代码注释自述"过期只标墓碑、物理销毁在压实发生"），但装配面未接线，所以链在线上天然只有一层——这也是"链恒重放"能成立的前提。演进方向见 `openspec/changes/runtime-simplicity-upgrade`（把整片快照换成追加日志，未实施）。

<a id="local-file-kv"></a>
## 六、kv 分层：分片快照

`localfile` 档的 KV 是**按 key 首前缀分片的快照**，不是单库、也不是 WAL：

| 语义 | 规则 | 判据 |
|---|---|---|
| 分片判据 | `shardLabelOf`＝第一个冒号前的部分；没有冒号归 `global` | `ref:`/`blob:`/`<pid>:` 天然各成一片 |
| 分片载体 | `<root>/kv/<label>/snapshot.json`，`{version, entries}` | 打开时清掉上次被杀在 rename 中途的 `.tmp` |
| 写入 | `kvCommit` 先应用 deletes 再应用 puts 到内存 map，然后**只把脏片**写盘 | 触碰哪片重写哪片，写放大不扩散 |
| 原子性 | 临时文件 + `os.Rename` 换入 ⇒ 进程被 KILL 不会留下撕裂快照 | 重开必见该片最后一次成功提交的状态 |
| 可见性 | 提交返回即对**进程内**可见；对**新进程**可见即跨过 rename | 热路径全内存（`OpenTier` 后 `shards` map），读不碰盘 |
| 无 fsync | 故意**不**做 fsync：能挺过进程重启，**挺不过操作系统掉电** | 与"仅 flush"的诚实声明一致 |
| 事务边界 | `kvBatch` 是原子单位；**跨批没有事务** | 正确性靠顺序不变量（[§二](#hard-contracts) 第 8 条） |
| 损坏 | 分片不可解析或版本不认识 → `ErrSnapshotCorrupt`，拒绝打开 | 不猜格式、不静默丢片 |

读口只有两种形状：单 key 取（`kv get`）与前缀扫描（按 key 排序）。**没有 range 原语**，也没有"第二个真源"——内存态是分片的投影，不是独立账本。

<a id="store-instance-sharing"></a>
## 七、打开与实例共享

`OpenTier(ctx, StorageConfig)`（`memory/ledger/config.go:86`）是唯一的打开入口，返回**同一后端的原文仓与段存储**：

| 档 | 后端 | 得到的两个 store | 能力声明 |
|---|---|---|---|
| `memory` | 进程内 `memKV` | `newFileSegmentStore(newMemKV())` + `NewMemoryOriginalStore()` | `Durable=false`（装配期告警明确说"不跨进程"） |
| `localfile` | `openLocalFileKV(Root)` | `newKVOriginalStore(kv, nil)` + `newFileSegmentStore(kv)` | `Durable=true` |
| `file` | rustviking CLI（默认 `rustviking`，配置默认 `<Root>/config.toml`） | `newRustVikingKV` 之上的同一对 store | `Durable=true` |

**共享语义（与旧页相反，按当前码面）**：`OpenTier` **没有单例注册表**——每次调用各开一套 store 落在同一个后端上。这不是缺陷而是判据：共享的是**后端里的那份事实**，不是 Go 对象。因此

- 两次 `OpenTier(localfile, 同一 root)` 读到的是同一批分片（第二次打开加载第一次提交的字节）；
- 不需要"为避免两套生命周期而共享实例"，因为**生命周期组件没有被装配**（[§三](#event-shape) 末段）；
- 真正的单写者约束由上层给：`file` 档的每次提交是 CLI 调用，`localfile` 档的写入必须串行在一份 `localFileKV` 上——同一进程内不要开两份写同一 root。

热路径的两个 store 只是**同一份内存分片上的两个视图**，因此"原文仓"与"段存储"永不打架：它们共享 `kvPort`，谁提交谁就带上自己的屏障。

<a id="read-paths"></a>
## 八、读路径

三条读面，各自的授权来源不同，**没有任何一条会因为"没配置"而静默返回全库**：

| 读面 | 入口 | 授权来源 | 返回 |
|---|---|---|---|
| 声明式查询 | `QueryEvents(ctx, QueryOptions)`（`memory/query_events.go`） | `PartitionIDs`（default-nothing） | `[]EventReference`（键/类型/摘要/时间） |
| 检索（召回） | `search.Searcher.Search(ctx, Query)`（`memory/search/port.go`） | `Query.Partitions`（空集＝不授权任何分区） | `[]ledger.OriginalRef` + 续页游标 |
| 原文读取 | `GetScoped` / `ReadSegmentScoped`（`memory/ledger/reference.go`） | `ReadScope{owner, format, retention_policy}` | 字节或四态结论 |

**声明式语义**：结果 ≡ 过滤（分区 → 类型 → 时间窗 → 关键词）→ 全序 `(Timestamp, Key)` sort（desc 时两者同时反向）→ offset/limit。关键词匹配是"任一词命中即算命中"的大小写不敏感子串匹配，作用在摘要或正文上；空关键词视为全部匹配。这类"可观察结果只由这几步决定"的性质，是分片布局能被替换的唯一前提。

**观察表**（`memory/store_query.go`）：检索与按键回捞都读同一张表，表里装的只是**已 land 的事实**——`ObserveStore` 全量装入，`LandEvent` 增量装入并拒绝 `body.Key` 与存储键不一致。判据是"一条事件在被观察之前不能被查询消费"，所以查询面永远不含正在提交中的半条。

**装配面**（`assembly/read.go:67` `supplyRetrieval`）：观察表 → （可选）向量缝 → `NewStoreSearcher`。没有嵌入器时不建缝、不调嵌入，检索只走关键词腿——这是契约写明的**零变化行为**。原文回捞的 scope 由装配期 `NewScopedReader` 绑定一次（`assembly/tools.go` `RecallPort.GetScoped`），绑定后无法在调用点放宽。

<a id="inmemory-retrieval"></a>
## 九、检索与融合

检索端口只给**引用**，不给正文：命中是"哪条事实"，正文永远留在原文仓按 scope 取。这是"索引是派生结构、永不是真源"的落点。

| 规则 | 行为 |
|---|---|
| 查询必须有检索内容 | `Text` 为空 → `ErrQueryTextMissing`（listing 不是降级形态） |
| 页有界 | `DefaultLimit = 20`；`Limit <= 0` **不是**无界，而是回落到默认值 |
| 游标 | 不透明、绑定到产生它的查询（文本＋过滤集），换条件即 `ErrCursorUnbound`；游标**不携带授权** |
| 关键词腿 | 映射成 `QueryOptions`：`Text → Keyword`、`Since/Until → Unix 毫秒`、固定最新优先；分区集合由读缝注入，方法本身**不带**分区 |
| 向量腿 | 只读授权范围内的候选、整批打分再按 `vectorTopK` 切；嵌入失败或空向量 = 空腿，不报错 |
| 融合 | RRF：`score = Σ 1/(K + rank)`，`K = 60`，rank 自 0 起；同分取**更大 Key**，再按 `(Owner.AgentID, Owner.SessionID, Format, RetentionPolicy)` 升序 |
| 退化 | 空向量腿与 `keyword_only` 走**同一段代码**（`keywordOnly`），两者逐字节相同——向量管线后上线不会改变今天的答案 |
| 未接线策略 | `vector_only` 具名拒绝 `ErrStrategyUnwired`（没有消费者）；未知策略 `ErrStrategyUnknown` |
| 调参 | `SetRetrievalTuning` 负值 → `ErrRetrievalTuning`，不静默夹取 |

融合端口自身的红线：只把两条腿合成**一个有序引用集**、不越权读腿、不用正文副本回答融合命中。分页在融合之前由检索端口完成，融合不发放续页。

<a id="embedder-contract"></a>
## 十、Embedder 契约

`memory/embedder` 把"文本 → 向量"钉成装配期契约，而不是运行期惊喜：

| 阶段 | 规则 | 拒绝 |
|---|---|---|
| 声明校验 | provider 属于已知集合（`openai`/`zhipu`）、model 非空、`Dimensions > 0`、覆盖 endpoint 必须是绝对 http(s) | `ErrEmbeddingDeclaration` |
| key | 密钥在装配期读取；未设或为空即具名拒绝，不构造必败实例 | `ErrEmbeddingKey` |
| 生成 | 尊重 ctx；空文本拒、空向量拒、宽度不符拒 | `ErrEmptyText` / 具名错误 |
| 未接线 | 没有绑定嵌入器时 `Embed` 返回 `ErrEmbeddingUnwired`，调用方按"功能关闭"降级为关键词 | `ErrEmbeddingUnwired` |

`VectorSeam`（`memory/recall.go`）是**非阻塞**的向量缝：`Submit` 只入队，丢弃计数、失败计数，从不阻塞写入主链路；未绑定 seam 接受任何提交但不产出向量。`Close` 排空在途任务。因此"嵌入器慢/挂"不会推迟一次事实提交，只会让检索暂时只有关键词腿。

<a id="rv-cli"></a>
## 十一、rustviking CLI 后端

`file` 档把 KV 交给外部 CLI（`memory/ledger/rustviking_kv.go`）：

- **一条命令一份 JSON 信封** `{success, data, error}`；`success=false` 或非信封响应都包成 `ErrRustViking`（带 CLI 的 stderr 原文），**不猜**。
- 读：单 key `kv get -k <key>`；前缀 `kv scan -p <prefix> -l 1000000`（按 key 序）。
- 值一律 base64 跨进程；**取不存在的键返回 `null` value 而不是报错**——包装层把它翻译成"缺键"，与"CLI 起不来"严格区分（`cliEntry.Value *string`）。
- 写：`kv del` 全部先跑，再 `kv put` 逐个跑；一次调用一个键。因此"提交成功"意味着 CLI 已接受，**没有暂存态跨过调用**；代价是批量提交没有跨键原子性，正确性回到[§二](#hard-contracts) 第 8 条的顺序不变量。
- 结束前回 `ctx.Err()`：取消即失败，不把半批当成功。

<a id="gaps"></a>
## 十二、已知缺口与演进方向

| 缺口 | 现状与防线 | 候选方向 |
|---|---|---|
| **分片快照不是追加日志** | 每次提交重写整片 JSON，无 fsync，崩溃后的持久性只到"进程重启"级别 | `openspec/changes/runtime-simplicity-upgrade`：整片快照 → 追加日志（**未实施**，旧目录不自动迁移，新格式具名拒开） |
| **原文过期没有执行面** | 租约与生命周期在册未接线，`retention_policy` 取值为 `keep`，原文随链永存 | 接线 `NewLifecycle` + `Retention`；接线前不得声称"按策略过期" |
| **`file` 档批次无原子性** | 一次 CLI 调用一个键；中途失败留半批 | 依赖顺序不变量与重试幂等；或在 CLI 侧提供批量原子命令 |
| **检索只有一条腿在线** | 装配面传 `nil` 嵌入器即纯关键词；`vector_only` 无消费者 | 真正的向量持久索引（当前向量只在进程内 seam 里） |
| **`EventRecord.WindowTS` 注释仍提"压实层"** | 码面已无压实；该注释是上一个形态的残留 | 随 `runtime-simplicity-upgrade` 一并校正（本页已按实际语义写：窗段编号） |
| **旧格式字节只能拒不能读** | `DecodeEventBody` 具名拒绝上一版 body | 冷启动重建；不做就地迁移 |
| **段提交失败可能留下孤儿原文** | 原文仓先于段提交落地；失败后 ref/blob 无人引用（同 digest 重放会复用，不堆第二份字节） | 需要一个以链为真源的孤儿清扫器（当前无），或把提交改成“先段后原文”的可回滚组合 |
