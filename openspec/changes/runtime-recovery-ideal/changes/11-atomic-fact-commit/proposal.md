> **历史输入（2026-11-20）**：本目录不再作为当前派发或自动规格同步输入；下文旧勾选、方案与限制按原时点保留，不授权后续实施。当前只按[编排入口](../../ORCHESTRATION.md)派发三个语义单元，全部旧要求由[承接表](../../CARRYOVER.md)逐项接收，不因转历史而取消。

# 11 整事实原子提交（atomic-fact-commit）

> 本轮新增子域（一级 design D2 子域表 11）。阶段：**只规划**，不实施、不跑生产测试、不提交。设计与验收面**未冻结**：11.R1（G1）通过前不得派发 11.R2。

## Why

### 症状从哪来

一级 §六 的三条症状锚量的是 journal 字节、boot 成本、模型自主召回；事实层还有一条更早发生的可观测后果——**一条事实可以只落一半**。当前生产唯一事实提交入口 `Run.persistFact`（assembly/facts.go:355，调用点 facts.go:106）分两笔落链：

1. 判重读：`tier.Segments.GetEvent(ctx, evt.Key)`（facts.go:370）；
2. 原文笔：`tier.Originals.Put(ctx, body.Ref(), []byte(body.Message.Content))`（facts.go:377）；
3. 段笔：`tier.Segments.StoreEvent(ctx, record)`（facts.go:395），其内部把 `{pid}:evt:{window}:{seq}`、`{pid}:idx:{key}`、`{pid}:meta:{window}` 三键打成**一次** Batch（segment_store.go:616-621）。

即：**三键事件族是同批的；原文族（`ref:`/`blob:`）与事件族是两笔**（`kvOriginalStore.Put` 自带一次 Commit，kv_original_store.go:268）。两笔之间没有回滚，也没有跨笔可见性约定。

### 已承认的后果（文档既有记载，非本轮推断）

- memory-architecture.md:260「段提交失败可能留下孤儿原文」——原文仓先于段提交落地，失败后 `ref:`/`blob:` 无人引用；候选方向原话是「一个以链为真源的孤儿清扫器（当前无），或把提交改成『先段后原文』的可回滚组合」。
- memory-architecture.md:255「`file` 档批次无原子性」——一次 CLI 调用一个键，中途失败留半批；现由「顺序不变量＋重试幂等」代偿。
- `scripts/recover-kv` 正是为这一族缺陷而存在的**外部修复入口**（scripts/recover-kv/main.go:96 `recoverOrphanOriginals`，`--restore` 时 `:170` 写 `blob:`）。一条需要外部修工具的提交路径，说明提交本身没有闭合。

### 身份等价不等于位置＋字节相等（必须写进提交语义）

- `sameFactOf`（facts.go:420）经 `EventBody.SameFact`（memory/event_body.go:244）比较：规范化正文逐字节相等 **且** 源/目标范围戳按通配规则相容 **且** `TaskID` 相同。范围戳未声明方是通配（event_body.go:267-272），所以同一事实在不同调用点可以携带不同字面字节而仍是同一事实。
- 段槽位 `{partition, window, seq}` 是提交位置，不是身份：重复提交命中既有身份时 `StoreEvent` 直接返回 nil 而不占新号（segment_store.go:572-600）。
- 因此「未找到旧键 → 当作新事实重交」是错的推导。正常委派续轮本就会重交稳定身份的 instruction（runtime/agent/loop.go:1540「The instruction commits once per site under a stable identity; a later round of the same site re-commits that identity」），而退位把这批旧键搬到冷层（08）：热层未命中是常态，不是「新事实」的证据。**勘误（2026-11-16，退位机制整体删除）**：末句的搬迁前提作废——热层不再有「已搬走的旧键」；判据不变：键未命中不是新事实的证据，判重须由身份判据闭合。

### 为什么现在做

08 已把「热层未命中」制度化（identity-read 透明回冷）。提交侧若仍是「读一笔、写两笔」，则：

1. 崩溃后可能留下无主原文或半事实，需外部工具修；
2. 判重读与写入不在同一原子单元内，`file` 档更是逐键串行，无法同时收敛；
3. 退位后的重交路径依赖「冷层可读」这一个补偿机制来维持正确性，而不是提交本身的不变量。（**勘误 2026-11-16**：退位删除后该补偿机制不存在，第 3 条随之改述——重交路径的正确性只能落在提交自身的身份判重不变量上，而这正是本域要闭合的面。）

## What

三叶，两条必须串行（R1 → R2），R3 与 R2 同批集成。

- **11.R1 事实提交设计门 G1（只调查/原型，不实施）**：回答四问并产出证据——①同一存储根的全部写入入口与并发范围（生产 boot 开档 assembly/run.go:622、正式修复入口 scripts/recover-kv/main.go:103/:179、冷层开档 segment_store.go:787 / kv_original_store.go:198、测试内并发开档），以及「同根第二实例被拒」这条注释（memory/ledger/config.go:185）**是否真有任何实现**（本轮只读检索未发现 flock/独占标记）；②一次 `Batch` 是否足以承载一整条事实（原文族＋事件三键）——journal 档 `Commit` 是一个整帧追加＋一次 sync（journal_kv.go:139-165），`file` 档是逐键 CLI（rustviking_kv.go:149-163），能力不同；③提交结果的精确语义（新增 / 已存在 / 冲突 / 不确定）与各自的可观测判据；④「原有规范等价」——原子化不得改变 `SameFact` 判定、槽位分配、冲突拒绝与 EventCount 语义。
- **11.R2 整事实原子提交（依 G1 结论）**：把一条事实的原文族与事件族合成**一次**提交，或在单拥有者不能保障时按 G1 冻结的条件写分叉；提交结果按四分类如实返回；不确定结果由调用方重读裁决，不得报成功也不得报失败。
- **11.R3 引擎一致性负测与真实消费者（与 11.R2 集成）**：`AtomicStore` 的 all-or-nothing 契约（atomic_store.go:50-61）必须对每个档位成立或**显式拒绝**：`file` 档逐键实现 MUST NOT 继续以满足该契约的身份出现（缺能力显式拒绝，不得把串行 CLI 当原子）；负测要能抓住「第 N 键失败前 N-1 键已发布」这次级故障——现有契约测试 `TestAtomicStoreContract`（memory/ledger/atomic_store_test.go:131）的 `contractEngines()`（:29-34）只跑 journal 与 retired_name，`refused_commit_publishes_nothing`（:198）的故障注入在包装层整批拒绝，**原理上抓不到引擎内部半批**。消费者锚必须是正式入口：`Run.persistFact`、`scripts/recover-kv --restore`、`TestAtomicStoreContract` 现有腿。

## Capabilities

优先修改既有能力，不新建同义规格，也不复制另一份权威：

- `event-segment-store`（MODIFIED，既有标题「后端不可变与隔离一致性」）：把「一次事实的三键同批」扩为「一条事实的原文族与事件族同笔」，并写入重复/冲突/不确定的答复语义。
- `partition-local-storage-access`（MODIFIED，既有标题「同步屏障与磁盘格式不变」）：写入「一个提交一个屏障」与「逐键串行不构成原子提交」；有界化/原子化不得改变既有磁盘格式语义（本条与 G3 的格式议题正交，本轮不做迁移）。
- `fact-vocabulary`（MODIFIED，既有标题「事实类别契约」）：一身份一不可变事实的落链单元是「整条事实」；等价由正文与范围戳的规范比较决定，MUST NOT 由位置或字节相等替代。

## 边界与依赖

- **依赖**：`AtomicStore` 缝与 `Batch`（atomic_store.go:39-61）；journal 档整帧提交（journal_kv.go:139）；`StoreEvent` 三键批（segment_store.go:560-621）；`kvOriginalStore.Put`（kv_original_store.go:268）；`EventBody.SameFact`（memory/event_body.go:244）；`persistFact` 现有判重与冲突分支（facts.go:370/395/409）。以上是**接口常数依赖**；11 不改 `AtomicStore` 四方法签名（一级禁止「无端新增通用事务框架」）。**勘误（2026-11-16，退位机制整体删除）**：原「退位的冷层与身份读（08 域，`GetEvent` 三态）」一项作废——08 域不再交付退位，身份读只需一次热层读取。
- **被依赖方**：08.R1（退位与回退、身份读；退位复制/校验的「一身份一不可变事实」需要提交侧先闭合）、06.R2（索引切片维护点与提交点在引擎内同一锁，提交批次形状影响切面写入时机）、`scripts/recover-kv`（修复入口的语义随提交闭合而收缩，见 R3）、以及远端部署的 `file` 档声明面（能力声明改变其可用性）。**勘误（2026-11-16）**：首项 08.R1 作废（该叶已反转，退位面删除）；其余依赖方不变。
- **接口面**：一次整事实提交的键集与顺序（原文族与事件族的相对次序）、提交结果四分类的类型与错误语义、档位能力声明（是否具备跨键原子）。三者属共享面，由编排者冻结后单写；本域在 R1 只产出**候选签名**。
- **禁止事项**：
  - 不新增通用事务框架，不改 `AtomicStore` 四方法签名；只有「单拥有者不能保障」时才按 G1 冻结的条件写设计分叉，且分叉当前一律标 `proposed`，**不是**用户裁决。
  - 不让任何引擎以「串行逐键」冒充原子提交；缺能力 MUST 显式拒绝，MUST NOT 静默降级为半批。
  - 不删除、覆盖或改写已提交事实（`session-checkpoint-material` 的 Requirement「派生态有界且不删除已提交事实」）；不为原子化做就地数据迁移（格式迁移另立，见 06.R1 的 G3 约束）。
  - 不在消费者里特判引擎文件布局（memory/ledger 之外不得出现 journal 结构知识）；不把 boot 成本与提交成本混为一个指标。
  - 不改写冻结归档（openspec/changes/archive/** 只附勘误）；不触 `examples/wechat-bot/.wechat-config/data`、`config-surface-normalization` 未提交变更与外来未跟踪文件。
  - 本文档阶段只规划：不实施生产代码、不跑生产测试/CI、不 commit/push。
