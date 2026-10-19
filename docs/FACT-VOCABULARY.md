# 事实词汇表（FACT-VOCABULARY）

**这份文档面向扩展者与评审者**：读完之后应当知道两件事——框架里"事实"的判据是什么，以及每一族事实住在哪个包、身份是什么、类型是什么、靠什么机制锁定。表里每一行都能在对应包内逐字核到（方法名、类型值、锁定拒绝都能复跑）。

<a id="fact-category"></a>
## 一、事实类别契约

`event.Fact`（`api/event/fact.go`）是自证契约，只问两件事：`FactKey() string`（身份，同一事实同一身份）与 `FactType() string`（类型，冻结后不再改写）。实现者在**所属包内**加方法，不搬包——域边界=语义单元，事件包只声明契约与词汇。唯一例外是压缩请求族：判定它的机制跨三域（memory/replica 的溢出结果、evolution.meditation 的卡片序列、memory 的请求日志），判定词汇归 event 轴，producer/reason 仍留判定机制。

- 身份是字符串而非 `event.EventID`：身份是一处**拼写**（事件 hex 键、四元组合成串、指纹 hex），声明族所在包不得 import event，契约若要求事件类型，恰好把要登记的族挡在门外。
- 两问就是"产出即锁定"的最小自证：它叫什么、它是什么。第三问（谁消费、什么时态）属于化归判据，不属于事实本体。
- **被否**：(a) 全部事实类型搬进 event 包——任务/训练/声明词汇互不依赖的域边界会被破坏；(b) 全局注册表——运行期可变结构且当前无消费者。

<a id="families"></a>
## 二、已登记族（一行一族实现）

| # | 族 | 归属包 | 身份（`FactKey`） | 类型（`FactType`） | 锁定机制 |
|---|---|---|---|---|---|
| 1 | 核心事件 | `event` | `Event.Key`（规范 hex 存储键） | `Event.Type`（`TypeAgentOutput` 等冻结类型） | 存储键即身份：同一 Key 只存一条事实，Type 随事实冻结 |
| 2 | 通知 | `task` | `TaskID:Round:Kind:Seq` 合成（`runtime/task/fact.go`） | `string(Kind)` | 幂等派发：同一身份重复投递不记第二条事实，重试保留原身份 |
| 3a | 训练记录·attempt | `rl` | `AttemptRecord.ID` | `rl.attempt` | 决策边界下的一次隐藏重试不算第二次 attempt，记录只追加 |
| 3b | 训练记录·feedback | `rl` | `FeedbackRecord.FeedbackID` | `rl.feedback` | 同 ID 幂等、异内容报 `rl.ErrFeedbackConflict`（拒而不覆盖） |
| 3c | 训练记录·reward | `rl` | `RewardRecord.InstanceID` | `rl.reward` | 每次派生取新 instance、revision 递增，旧记录不被原地改写 |
| 4 | 声明 | `config` | `Fingerprint(d)` 的规范指纹 hex（`api/config/fact.go`） | `config.declaration` | 指纹只取声明面：同内容重载同一身份；源文档 `Raw` 与运行期绑定不入指纹；config 不 import event，结构性满足 `Fact` |
| 5 | 引用票据 | `memory/ledger` | `OriginalRef.Key`（原文 blob 的规范 hex 键，`reference.go`） | `memory.original` | 去重域（owner/format/retention）只分开票据、不重标识 blob：同内容多票据共键，`Put` 不失效既有票据 |
| 6 | 回放结算 | `memory/replay` | `ReplaySettleFor(decision).ID`（`set-`＋决策内容的 sha256 前缀，`replay_settle.go`） | `replay.settle` | 同决策同身份：条目/呈现模式/预览相同即同一 `FactKey`，重复组装不追加第二条；预算跳过的条目不结算 |
| 7 | 折叠·压缩链 | `evolution/meditation` | `CompactionFactKey(payload)`（`fold-`＋折叠线内容的 sha256 前缀，`prefold.go`） | `context_compress`（`CompactionEventType` → `event.TypeContextCompress`） | 内容寻址：summary ref、`full_boundary`、已消费键（`ConsumedKeys`）与 refs 顺序/内容定身份，同一折叠线重复提交同键；提交时刻不入身份 |
| 8 | 压缩请求 | `event` | `CompressRequest.ID`（producer‖subject‖reason 内容寻址，`api/event/compress_request.go`；日志在 `memory/compress_filing.go`） | `compress.request` | 同判定同身份：重复/重试判定不分叉第二条请求，`At` 只作首落盘戳；消费后单次 retire |
| 9 | 凝缩产物 | `evolution/meditation` | `meditationArtifactKey`（内容寻址：summary refs），`MeditationArtifact` | `memory.condensed_card` | 同折叠同 identity：重跑不重打戳、产物内容不变即同一身份 |

合成身份的语义（具名裁定）：通知的身份是它自己携带的四元组，`Notice.Event` 指向的已提交事实**永不替换**这个身份，所以四元组才是 `FactKey` 的输入、被指向的事件只是消费线索；声明的身份是规范指纹（`api/config/fact.go` 具名注释）；票据的身份是原文 hex 键，去重域只决定票据能否并存，不改写身份。

**触发源戳的归属**（认知整理三通道对称）：`event.MetaKeyTriggerSource` 写入的戳名由产出它的机制包持有，三通道的词汇各归其包；戳只标记来源、不构成身份，因此不在上表新增族。

| 戳 | 常量 | 归属包 |
|---|---|---|
| `meditation` | `LineageReflection` | `evolution/meditation`（观察面：自己分区＝自维护，他人分区＝旁路冥想） |
| `consolidation_hint` | `LineageConsolidationHint` | `evolution/consolidation`（记忆侧巩固唤醒，载荷是候选清单） |

**投影账本 vs 事实链**（2026-10-19 裁决）：`replay.settle` 是呈现决策的留痕、不是领域事实——它落在投影自己的结算账本（`ReplayProjection.Settles()`），装配只读事实链、从不写回；`context_compress` 折叠事实则落事实链（`CompactionChainFact` 提交，回放按 `full_boundary` 续读）。两者身份都内容寻址，只有后者参与回放。**`Summary` 不是模型总结**（2026-10-19 裁决）：投影条目的 `Summary` 取自已提交事件的 `event_summary` 元数据，是原文的一行读数（工具调用的工具行、消息的短渲染），不是模型二次改写的摘要；`Content` 才是原文，`PresentationSummary` 只换读这一行，原文仍按 `Key` 逐字节读回；折叠载荷里 summary ref 同理。

**六面产物的归属**（消费者协议第二辑，面见 `docs/EXTENSIONS.md` 第四节）：

| 消费面 | 产物（身份） | 事实族归属 |
|---|---|---|
| Subscribe | `query.Update`（Op/Revision/Ref） | 不入族：本代投影的读视图 |
| Deliverer | `task.DeliveryAcceptance` 与 blocked 原因 | `task` 通知族的投递状态，不新增 `FactKey` 行 |
| Apply | `state.PublicationRecord`（代际＋指纹） | 不入族：发布台账，不是任务事实链 |
| Shutdown | `agent.DrainReport`（Drained/Unsettled） | 不入族：结算读数 |
| Observer | `plugin.AuditRecord`（Fact＋Attempts） | 不入族：观测证据，只引事实身份 |
| Exporter | `rl.SampleLine`（`Provenance.ExportID` 为快照身份，`Fact.EventKey` 引用源事实） | `rl` 制品事实（导出快照）；源事实仍属 `event` 族 |

一句话裁定：只有导出的封印快照是制品事实，其余五面的产物是读视图、投递状态、发布台账、结算读数或观测证据，都不新登记族——新族只在本表第二节登记。

<a id="reduction-criteria"></a>
## 三、化归判据：新增能力先过三问

| 问 | 判据 | 落在哪 |
|---|---|---|
| ① 是不是事实 | 产出即锁定、有身份、可被引用（不是"能算出来的中间量"） | 是→新族需在本表登记一行并实现 `Fact`；否→既有族的字段或投影 |
| ② 谁消费它 | 点名消费者（回合驱动 `Loop.Drive`、时间驱动 `Reflection.Tick`、宿主循环）以及它读后产出什么新事实 | 无消费者的结构不是事实，是没人读的占位 |
| ③ 什么时态 | 存续（memory/state）、在途（task/rl 记录）、回放（query）、产物（view 呈现） | 映射到时态层，不新建平行词汇 |

三问都答不出来，就不是新能力而是既有能力的字段——这是本变更要收敛的那类偏差。
