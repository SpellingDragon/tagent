# tagent/event 事件契约：冻结类型、元数据键与事实身份

> **活契约页**：逐条对齐当前码面（`api/event/`），代码内 `// 契约:` 注释指向本页锚点（[`meta.go`](../../../api/event/meta.go) → [`#const-list`](#const-list)，[`types.go`](../../../api/event/types.go) → [`#overview`](#overview)）。历史快照见 [PRE-REWRITE-MANIFEST](../../archive/wiki/PRE-REWRITE-MANIFEST.md)。

<a id="overview"></a>
## 一、模块定位

`tagent/event` 是**词汇与协议的纯叶子包**：它只声明"一条事实长什么样"——冻结的事件类型、冻结的元数据键、身份与地址的类型——不持有状态、不做 IO、不生成摘要。所有函数都是纯函数，可并发安全调用；依赖方向只向外零条，因此任何层都能引用它而不会成环。

| 文件 | 职责 | 立得住的反事实 |
|---|---|---|
| `types.go` | `Event`、`Address`、类型化标识（`AgentID`/`SessionID`/`EventID`/`InvocationID`/`TaskID`/`InstanceID`/`EpisodeID`/`DecisionID`/`AttemptID`/`CallID`/`ApprovalID`） | 没有它：身份退化成 `string` 满仓飞舞，谁都能把 `EventID` 塞进 `TaskID` |
| `meta.go` | 冻结类型常量、冻结元数据键、透传前缀、`ParseEventMeta` | 没有它：键名靠字面量复制，跨包静默漂移 |
| `fact.go` | 事实类别契约 `Fact`（`FactKey`/`FactType`） | 没有它：一个事实家族无法在不 import 本包的地方自述身份 |
| `model_replacement.go` | 模型替换请求的事实构造（带 `trigger_source`、`event_summary`） | — |
| `compress_request.go` | 压缩请求的事实构造 | — |

`Event` 的四个块（字段全在 `types.go`）：

| 块 | 字段 | 契约要点 |
|---|---|---|
| **身份** | `Key`、`Type`、`Timestamp` | `Key` 是规范身份；`Timestamp` 是唯一语义时间轴（Unix 毫秒） |
| **内容** | `Meta`（`map[string]string`）、`Message`（原生模型负载） | 元数据**原样整表保留**；正文只在 `Message.Content` 一份 |
| **所有权** | `Owner`、`Source`、`Target`、`InvocationID`、`TaskID`、`InstanceID`、`EpisodeID`、`DecisionID`、`AttemptID` | `Owner` 是原文引用的 scope 轴；其余是血缘 |
| **账本** | `CallID`、`ApprovalID` | `CallID` **不是**全局唯一，去重范围＝`(InstanceID, InvocationID, DecisionID, CallID)`；`ApprovalID` 全局唯一 |

**地址** `Address{AgentID, SessionID}` 是投递与 scope 的共同坐标：`IsZero`/`IsComplete` 让"未声明"与"声明了但缺一半"可被区分——因果链的身份是 `(PartitionID, SessionID)`，空 session 一律具名拒绝，绝不退化成"整个分区一条链"。

<a id="const-list"></a>
## 二、冻结取值清单

**事件类型（冻结，不得新增同义拼写）**：

| 常量 | 值 | 语义 |
|---|---|---|
| `TypeExternalInput` | `external_input` | 一切非 Agent 回复、非工具结果的输入（用户消息、宿主注入、结算通知） |
| `TypeAgentOutput` | `agent_output` | Agent 的最终回复 |
| `TypeThinkingPlan` | `thinking_plan` | Agent 的思考/计划（可携带工具调用） |
| `TypeActionCommand` | `action_command` | 工具执行结果 |
| `TypeContextCompress` | `context_compress` | 压缩折叠产生的合成引用（负 key 语义） |
| `TypeTaskSettled` | `task_settled` | 任务结算通知，已升为一等事件类型（带自己的名字，不再靠文本前缀识别） |
| `TypeFeedback` | `feedback` | 反馈/评分/任务成败，经因果边绑定到具体产出事件 |
| `TypeGovernance` | `governance` | 治理记录（`subtype` 区分 `denial`/`goal`/`approval`/`degraded`/`audit`） |

**触发源印花（`trigger_source` 的取值）**：本包只声明一个值——`LineageDelegation = "delegation"`，即一次被委派的调用是 Agent 自己管理的流量，不开宿主面、不计新鲜度。其余取值（`user`、`task`、`reincarnation`、`system_alert`、`meditation`）**不在本包声明，也不在本包判定**：判定住在消费域自己的策略里（见[谱系可见性](../reliability/durable-delivery.md#lineage-visibility)），本包只保证印花这一格元数据原样过链。

**冻结元数据键**：

| 键 | 常量 | 谁写 | 谁读 |
|---|---|---|---|
| `event_key` | `MetaKeyEventKey` | 写入方在落链时盖章 | 审计、离线导出 |
| `partition_id` | `MetaKeyPartitionID` | 同上 | 审计、诊断 |
| `event_type` | `MetaKeyEventType` | 同上 | 审计、诊断 |
| `event_summary` | `MetaKeyEventSummary` | **各写入方自己盖章**（宿/任务/模型替换/失败通知各写各的） | 查询记录、投影条目、检索文本兜底 |
| `trigger_source` | `MetaKeyTriggerSource` | 每回合由运行流程设在转发事件上，入库时抄进事实 | 新鲜度判据、投递策略、交付账本的血统字段 |
| `settle_trigger_source` | `MetaKeySettleTriggerSource` | 结算面 | 结算归因 |
| `bundle_id` | `MetaKeyBundleID` | 装配面 | 版本可回溯 |
| `kind` | `MetaKeyNoticeKind` | 通知落链面 | 按通知类别分派，绝不解析正文 |
| `task_id` | `MetaKeyTaskID` | 通知落链面 | 按任务归集 |

**透传前缀** `MetaPrefix = "meta_"`：任何带该前缀的键原样转发，且**永不被读作业务字段**——业务键与受控控制面因此不共用命名空间。

`ParseEventMeta(meta)` 把一张表投影成三分并且**一条都不丢**：冻结键进命名字段、`meta_` 进 `PassThrough`、其余进 `Unknown`（原样保留，供本解析器不建模的条目存活）。这条"不丢未建模键"的性质是元数据契约能被扩展的前提。

<a id="event-shape"></a>
## 三、摘要与正文：写入方盖章，读面只读不算

上一形态的 `GenerateEventSummary` 已不在码面：**本包不生成摘要**。当下的分工是

| 面 | 行为 | 判据 |
|---|---|---|
| 写入 | 写入方把摘要盖进 `Meta[MetaKeyEventSummary]` | 摘要是一份**元数据视图**，不等于正文，也不替换正文 |
| 落链 | `memory.EventBody` 把 `Meta` 与 `Message` **整份**保留 | 不另存 `summary`/`content` 字段（见[记忆篇 §三](../memory/memory-architecture.md#event-shape)） |
| 读 | `EventBody.Record` 从元数据取摘要、从原生负载取正文 | 读时派生，因此不可能与事件说的不一致 |
| 呈现 | 投影条目的一行视图 `oneLineOf(summary, content)`：有摘要用摘要，否则取正文首行并界到 120 rune | 这是**呈现面的界**，事件本体不动 |
| 工具调用 | 无正文的调用消息在投影视图里写成 `[call <name>]`；工具结果无正文写成 `[tool <name> returned no text]` | 空不是可展示状态，必须有名字 |

两个由此固定的判据：①**摘要为空不是错误**，它是"写入方没有盖章"，读面会自然回落到正文首行；②**摘要绝不回写正文**——任何"用摘要替代内容"的路径都会让召回把摘要当成原文再序列化一次。

<a id="fact-identity"></a>
## 四、事实身份：两处拼写，不要混用

| 拼写 | 形态 | 谁产生 | 用途 |
|---|---|---|---|
| **事实身份**（`Event.Key` / `EventID`） | `<seq>-<16 字节随机 hex>` | `assembly/facts.go` `mintEventID`（`seq` 来自该次提交的槽位序号） | 一条事实的规范身份；`ref:<EventID>`、`idx:<EventID>`、因果边、回执都以它为键 |
| **段槽位键** | `<PartitionID>:evt:<WindowTS 毫秒>:<Seq>` | `memory/ledger/segment_store.go` `StoreEvent` | 链的物理位置；**只用于定位，不承载语义** |

两处拼写刻意不同：槽位键决定"写在哪一格"，事实身份决定"这是哪条事实"。因此重启后同一毫秒内的新事实不会与旧事实撞身份，而时间轴回退也不会改变已有事实的身份——**时光回退是天然成立的**，因为身份不编码时间。`<pid>:idx:<EventID>` 是两者之间唯一的桥：它把身份映射到槽位，缺 idx 的事件对所有读路径不可见。

`Fact` 契约（`fact.go`，两条红线）：`FactKey()` 每次问都答同一个身份、`FactType()` 答冻结类型；**不得从非身份字段重新推导身份，也不得改写被引用的身份**。一次重试保持原身份——这正是"重复提交是幂等成功"而不是"新事实"的依据。

<a id="rejections"></a>
## 五、拒绝语义：按名字，不靠零值

| 情形 | 结论 | 判据 |
|---|---|---|
| body 顶层无 `message` 键 | `ErrEventBody` | **键存在性判据**：上一版 `key/content/summary` 与 `event_key` 两种旧形态都因此被拒 |
| body 无 `key` | `ErrEventBody` | 没有身份就没有事实 |
| body 的 `Key` 与存储键不一致 | `ErrBodyKeyMismatch` | 槽位与身份必须一一对应 |
| 同一身份换一份内容 | `ErrFactKeyConflict` / `ErrContentConflict` | 一个身份只站一个事实 |
| 未声明的枚举值（如未知类型） | 原样保留，不猜语义 | 元数据解析把未建模键放进 `Unknown`，绝不丢弃 |

判据**不得**用解码后的结构体零值给出：空 `message` 是本 schema 内的合法事实（例如纯工具结果），零值判据会把合法事实一并误拒——这也是旧格式只能用"顶层键存在性"判的原因。

## 六、与其它篇的关系与已知缺口

- 事件落地后的形态与查询面：[记忆篇 §三](../memory/memory-architecture.md#event-shape)、[§五](../memory/memory-architecture.md#file-segment-store)。
- 触发源印花如何决定"这条产出能不能出宿主"：[持久投递篇 §三](../reliability/durable-delivery.md#lineage-visibility)。
- 事实类别（不只事件一种）的登记表：[FACT-VOCABULARY](../../FACT-VOCABULARY.md#families)。

| 缺口 | 现状与防线 | 候选方向 |
|---|---|---|
| **类型静态属性没有单一注册表** | 上一形态的 `registry.go`/`EventTypeSpec` 已退场；今天"类型级属性"分散在常量与本包外的策略里（如冥想域的触发源白名单） | 若新增类型的属性再次跨三个包，应重新引入一处声明式的单源，而不是各自维护枚举 |
| **摘要没有单一生成点** | 各写入方自己盖章，写漏就是空摘要（读面回落正文首行） | 在事实构造的一处统一盖章，本包只声明键不生成值 |
| **事件类型无版本化** | 语义演进（如摘要从"总结"变"元数据视图"）没有 schema 版本标记，历史事件按当前语义解读 | 元数据加 `schema_version`，读取侧按版本适配 |
| **`CallID` 的重复判定依赖四元组** | 单看 `CallID` 不唯一，判定范围是 `(InstanceID, InvocationID, DecisionID, CallID)` | 已经写进本页与 `types.go`；任何新消费方必须复用这个范围，不能只比 `CallID` |
