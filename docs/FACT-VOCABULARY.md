# 事实词汇表（FACT-VOCABULARY）

**这份文档面向扩展者与评审者**：读完之后应当知道两件事——框架里"事实"的判据是什么，以及每一族事实住在哪个包、身份是什么、类型是什么、靠什么机制锁定。表里每一行都能在对应包内逐字核到（方法名、类型值、锁定拒绝都能复跑）。

<a id="fact-category"></a>
## 一、事实类别契约

`event.Fact`（`event/fact.go`）是自证契约，只问两件事：`FactKey() string`（身份，同一事实同一身份）与 `FactType() string`（类型，冻结后不再改写）。实现者在**所属包内**加方法，不搬包——域边界=语义单元，事件包只声明契约与词汇。

- 身份是字符串而非 `event.EventID`：身份是一处**拼写**（事件 hex 键、四元组合成串、指纹 hex），声明族所在包不得 import event，契约若要求事件类型，恰好把要登记的族挡在门外。
- 两问就是"产出即锁定"的最小自证：它叫什么、它是什么。第三问（谁消费、什么时态）属于化归判据，不属于事实本体。
- **被否**：(a) 全部事实类型搬进 event 包——任务/训练/声明词汇互不依赖的域边界会被破坏；(b) 全局注册表——运行期可变结构且当前无消费者。

<a id="families"></a>
## 二、已登记族（一行一族实现）

| # | 族 | 归属包 | 身份（`FactKey`） | 类型（`FactType`） | 锁定机制 |
|---|---|---|---|---|---|
| 1 | 核心事件 | `event` | `Event.Key`（规范 hex 存储键） | `Event.Type`（`TypeAgentOutput` 等冻结类型） | 存储键即身份：同一 Key 只存一条事实，Type 随事实冻结 |
| 2 | 通知 | `task` | `TaskID:Round:Kind:Seq` 合成（`task/fact.go`） | `string(Kind)` | 幂等派发：同一身份重复投递不记第二条事实，重试保留原身份 |
| 3a | 训练记录·attempt | `rl` | `AttemptRecord.ID` | `rl.attempt` | 决策边界下的一次隐藏重试不算第二次 attempt，记录只追加 |
| 3b | 训练记录·feedback | `rl` | `FeedbackRecord.FeedbackID` | `rl.feedback` | 同 ID 幂等、异内容报 `rl.ErrFeedbackConflict`（拒而不覆盖） |
| 3c | 训练记录·reward | `rl` | `RewardRecord.InstanceID` | `rl.reward` | 每次派生取新 instance、revision 递增，旧记录不被原地改写 |
| 4 | 声明 | `config` | `Fingerprint(d)` 的规范指纹 hex（`config/fact.go`） | `config.declaration` | 指纹只取声明面：同内容重载同一身份；源文档 `Raw` 与运行期绑定不入指纹；config 不 import event，结构性满足 `Fact` |
| 5 | 引用票据 | `memory` | `OriginalRef.Key`（原文 blob 的规范 hex 键） | `memory.original` | 去重域（owner/format/retention）只分开票据、不重标识 blob：同内容多票据共键，`Put` 不失效既有票据 |

合成身份的语义（具名裁定）：通知的身份是它自己携带的四元组，`Notice.Event` 指向的已提交事实**永不替换**这个身份，所以四元组才是 `FactKey` 的输入、被指向的事件只是消费线索；声明的身份是规范指纹（`config/fact.go` 具名注释）；票据的身份是原文 hex 键，去重域只决定票据能否并存，不改写身份。

<a id="reduction-criteria"></a>
## 三、化归判据：新增能力先过三问

| 问 | 判据 | 落在哪 |
|---|---|---|
| ① 是不是事实 | 产出即锁定、有身份、可被引用（不是"能算出来的中间量"） | 是→新族需在本表登记一行并实现 `Fact`；否→既有族的字段或投影 |
| ② 谁消费它 | 点名消费者（回合驱动 `Loop.Drive`、时间驱动 `Reflection.Tick`、宿主循环）以及它读后产出什么新事实 | 无消费者的结构不是事实，是没人读的占位 |
| ③ 什么时态 | 存续（memory/state）、在途（task/rl 记录）、回放（query）、产物（view 呈现） | 映射到时态层，不新建平行词汇 |

三问都答不出来，就不是新能力而是既有能力的字段——这是本变更要收敛的那类偏差。
