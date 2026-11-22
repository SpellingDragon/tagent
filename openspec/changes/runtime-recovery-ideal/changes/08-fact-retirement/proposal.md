> **历史输入（2026-11-20）**：本目录不再作为当前派发或自动规格同步输入；下文旧勾选、方案与限制按原时点保留，不授权后续实施。当前只按[编排入口](../../ORCHESTRATION.md)派发三个语义单元，全部旧要求由[承接表](../../CARRYOVER.md)逐项接收，不因转历史而取消。

# 06 退位分层（用户裁定乙）

> ## ⚠️ 勘误（2026-11-16，用户裁定反转）：本域已改为「删退位」
> **用户裁定原话**：「采纳可寻址索引，退位退役」，随后纠正「准确的说不是"退役"，我们的所有变更都要0遗留0残留，不考虑框架兼容，最多考虑外部脚本辅助」。
> ⇒ 下文 Why/What/Capabilities 中把**退位作为交付**的条目一律作废（不实施），`fact-retirement` 能力不采用（其 delta spec 已删除，主 specs 零命中）；`storage-lifecycle-completion` 的 MODIFIED 只保留与退位无关的存活条款（保留租约、fail-closed、义务租约、零调用脚手架判据、原时间戳）。
> 机制依据：退位三条核心性判据全否（失败不阻断／关掉只变成本／不在拓展轨道上），它是性能层补偿；可寻址索引使开档/点读/提交跨历史全平，退位的成本买点被取代。现行事实：热层只追加、不搬迁；热层字节增长被显式声明，MUST NOT 被声称有界（见 `openspec/specs/session-checkpoint-material/spec.md`）。
> **下方原文保留为历史证据（5.8：历史不可变，推翻结论记在本节与 tasks.md 头部）**；生效叶见 tasks.md「当前有效叶」（08.R3／08.R4）。

## Why

用户原症状：「目前 WAL 的 journal.log 会无限膨胀」。一级 design §六 S1 直译：持续写入 N 事实＋折叠＋投递结清后，`journal.log` 字节 SHALL 有界于存活键集，不随历史线性增长——今天红（无界）。

物理无界的成因是引擎形态，不是缺陷：`journalLog` 是单写者只追加文件（journal.go:97-102），`order` 每帧每键永不回收（journal.go:115-121），开档整份回放（journal.go:471-501 → journalReplay:433）。01/02/03 治的是**恢复成本**（读切片、派生索引、尾增量），治不了**磁盘厚度**：已结事实的字节仍留在热文件里。

用户裁定**乙**（一级 D6.1）：物理有界取向＝退位，否甲（接受无界＋删脚手架）。乙是教义兼容延伸，因为红线只禁删除与改写，不禁搬迁：

- `session-checkpoint-material` 的 Requirement「派生态有界且不删除已提交事实」：「任何有界化手段 SHALL NOT 删除、覆盖或改写已提交事实；恢复期读全史的消费者 SHALL 仍能按身份读到其正文」。
- docs/wiki/memory/memory-architecture.md:50：「只能**搬迁、遮蔽或删除可见性**，不得改写正文与身份」。

同时，`memory/ledger/{lifecycle,retention,guard}.go` 脚手架**生产零调用**（✅ grep：`NewLifecycle`/`NewRetention`/`NewMemoryGuard` 只出现在 tests 与 memory/ledger/skeleton_assert.go:150），违一级 P6「未接线脚手架按乙接线」。其回收判据现为租约＋龄期＋容量（lifecycle.go:224-254），与教义的存活键集权威不同源——一级 D6.3 否「另立回收 oracle」。

## What

**退位＝搬迁，不是删除。** 已结事实（最新 checkpoint 不再点名 ∧ 无未结义务 ∧ 非尾）搬到冷层，热层只留存活键集＋尾；`GetEvent(key)` 对已搬迁事实**透明回冷层取原字节**，逻辑链恒完整、恒可按身份重放。

- **冷层载体**：同引擎第二 journal 根（`<root>/cold`，经 OpenAtomicStore，config.go:147），同帧格式（journal.go:42/56），冷层只追加；不新增文件类型、不新增布局词汇。
- **搬迁三步**：复制（256 键/批，借 migrate.go:52）→ 新开视图逐键读回逐字节校验（借 verifyMigratedRoot，migrate.go:121-175，含 ref↔blob 配对不变量）→ 热层重写存活键集并替换（swap）。校验不过＝具名拒绝（ErrMigrateMismatch，migrate.go:48），热层原样不动。
- **引擎能力 journal-only**（D6.7）：AtomicStore 缝（atomic_store.go:50-61）表达不了 swap，新增 journal-only capability（参 commitOrder 先例 segment_store.go:224 / journal_kv.go:150），实现 `EnumerateKeys`/`Tombstone`/`ReclaimTombstoned`（LifecycleStore，lifecycle.go:69-79）＋搬迁；rustviking file 档无跨键原子，不做，显式声明能力缺失。
- **排他安静点**：安静点只是 drain 不是 fence（assembly/checkpoint.go:23/:102-107），搬迁须排他——Run 停写闸（提交入口 facts.go:84）＋ drain 归零 ＋ swap 持 `journalKV.mu`（journal_kv.go:105）。在途歧义（originals-before-segments，孤儿 vs 在途不可区分）只在闸内消解。
- **存活键集权威单一**（D6.3，§五冻结）：`live = checkpoint.Survivors ∪ {ref : ref.seq > Offset} ∪ obligationLeases ∪ chainReachableBlobs(live)`。恢复读它，退位搬它的补集——一个权威两处消费。
- **脚手架接线**：`Lifecycle.Scan` 判据改链可达（非龄期）；`Retention`/`MemoryGuard` 由**共享资源 owner** 持义务租约（未结投递义务 Hold，结清 Release；接线点 host_delivery.go:671 noteDeliveryState → run.go:2210 unpinFact，恢复侧 host_delivery.go:2069 已结早退）；headless-retired 的审计正文（host_delivery.go:866）按独立审计租约保留（用户无偏好，取默认 keep）。
- **保序**：热层重写按**逆帧序去重**反写（每键只留最新记录），否则认错最新 checkpoint（D7；锚 journal_kv_test.go:72 / segment_store_test.go:508）。
- **回退**：关闭退位＝热层回到只追加（基线零 diff），已搬迁事实冷层恒可读。

## Capabilities

> **勘误（见文首）**：本节两条能力声明按反转作废——`fact-retirement`（ADDED）不采用；`storage-lifecycle-completion`（MODIFIED）只保留存活条款。

- `fact-retirement`（ADDED，**作废·不采用**）：已结事实搬迁非删除、按身份恒可读、热 journal 有界于存活键集、排他安静点、保序、冷层损坏具名拒绝、journal-only 能力诚实。
- `storage-lifecycle-completion`（MODIFIED，**范围收缩**）：脚手架生产接线、义务租约由共享 owner 持有、已提交事实按身份恒可读、热层字节增长显式声明。

## 边界与依赖

> **勘误（见文首）**：本节「依赖」「被依赖方」「接口面」「禁止事项」中关于退位的条目按反转作废；存活条目（不得删改已提交事实、身份读不返回 missing、不读禁区）保持不变。

- **依赖（W3＝W1+W2 绿）**：01/02/03 建立的存活键集权威（§五 `checkpoint.Survivors`；现状 api/event/checkpoint.go:62 只有 `Offset` 水位，折叠幸存者今在 `metaKeyCompressSurvivors`，compress_consumer.go:41/565，由 02 收编为索引）；02 的派生索引加载（冷层懒开不得把 O(全史) 搬回 boot）；既有 Stateful 接缝（runtime/state/checkpoint.go:36）、AtomicStore 缝、安静点 drain（assembly/checkpoint.go:99）、MigrateShardedRoot 的复制＋校验半段（migrate.go:76）。**作废项**：01/02 的「冷层懒开」前提出自退位，退位不存在故该项不依赖。
- **被依赖方**：一级 S1 症状锚与 F3 DoD；远端 wechat-bot 部署磁盘（一级 §七）；全部按身份读与读全史的消费者——assembly/read.go:118/174/202/236/328、run.go:2281（CommittedEvent）/2304（eachCommittedEvent）、host_tools.go:926（eachDeliveryFact 全史扫）、facts.go:370/401（重复提交检查）、scripts/recover-kv/main.go:140（正式修复入口）。
- **接口面（作废·见文首勘误）**：新增 journal-only capability（域内声明，编排者核冲突）；`StorageConfig`（config.go:46）与 `hostStorage`（host_document.go:76-79）增退位开关与冷层根，经 host.go:635 接线；`LifecycleStore` 三方法语义在退位路径上＝搬迁（tombstone＝待搬迁标记，Reclaim＝复制＋校验＋热层移除），须与 05 域同步主 spec 与 golden、过 schema_allowlist 门。**退位开关与冷层根不实施**（零残留：不留配置面残留）。
- **禁止事项**：
  - **SHALL NOT 删除、覆盖或改写已提交事实**（`session-checkpoint-material` 的 Requirement「派生态有界且不删除已提交事实」）；热层移除只发生在冷层逐字节校验通过之后，且 `GetEvent` 恒返回原字节。〔本项随退位删除失效，见文首勘误〕
  - **SHALL NOT 因搬迁返回 missing**（§五 identity-read 不变量）；冷层读不出＝具名拒绝（ErrSegmentBodyMissing，segment_store.go:205/546），不静默丢事实。
  - 不另立回收 oracle（D6.3）；不以龄期/容量单独决定搬迁。
  - 不做 file 档（rustviking）退位（D6.7）；不触 memory/replay 投影（纯回放不写链）。
  - 不触外来未跟踪文件、冻结归档（archive/** 只附勘误）、config-surface-normalization 未提交变更与 agent 配置面（本域只动 storage 面）。
  - 不触 `examples/wechat-bot/.wechat-config/data`；远端部署启用声明由一级收尾协调，本域只在测试夹具声明开关。
  - 本文档阶段**只规划不实施**。

## 本轮重整（08.R1 / 08.R2）

一级 proposal「本轮重整」与 design D8–D11 撤销了旧整体完成结论。本域 6.1–6.5 与 7.x 的实施记录作为**历史凭据保留**（时点：2026-11-15 W3/W4，memory/ledger 包级 21.5s 绿、S1 锚绿，见 run-notes.md §08 与 §F3），但当前完成状态一律回退为待复验。已核出的偏差：

| 偏差 | 实证 | 影响 |
|---|---|---|
| 契约与代码反向 | spec 仍要求 `EnumerateKeys`/`Tombstone`/`ReclaimTombstoned` 三方法，实现是单一 `RetireFacts`（segment_store.go:888） | P5「主 spec 与代码同向」不成立；验收面须按单能力重写 |
| 两条判据并存 | design/spec 写存活权威 `live = checkpoint.Survivors ∪ 尾 ∪ obligationLeases ∪ chainReachableBlobs`；实现的 keep 是「尾（past cut）＋每链最新 checkpoint＋framework checkpoint」（assembly/checkpoint.go `keptHotKeys`） | 判据不唯一；D6.13 已把退位判据降为水位制，spec 未同步 |
| 闸的形态与文档不同 | 6.2 写「Run 停写闸」，实现是「引擎独占（`FileSegmentStore.mu` 贯穿复制/校验/重写）＋只搬快照读到的键」，并在 `commitFactChecked` 处有闸（R6 回写） | 负锚按旧形态写（`ErrColdArchiveNotQuiet`、停写超时）不可实现 |
| 脚手架仍未接线 | `NewLifecycle`/`NewRetention`/`NewMemoryGuard` 生产零调用（仅 tests 与 skeleton_assert.go:150） | 6.4 未完成；与水位制判据并存会造第二 oracle |
| 搬迁单位与提交单位不同 | 搬迁以「一条事实的段三键＋原文族」为同批单位（6.1），而提交侧仍是**两笔**（assembly/facts.go:377/:395） | 11 未闭合前，「同批搬」的输入面本身没有原子边界 |

### 本域重整后的两叶

- **08.R1 整合退位与回退／身份读（依 06.R2＋11.R2）**：把退位、回退开关、身份读三面整合为一条可验收的路径——搬迁单位对齐整事实提交后的键集；回退（关闭退位）后热层只追加且已搬事实仍按身份可读；身份读三态在冷层损坏时具名拒绝。含旧 6.3/6.5 的复验面。
- **08.R2 核租约/脚手架与 D6.13 是否已被取代（只调查＋判定，产出留/删结论）**：核实「存活键集权威（D6.3，含义务租约与链可达 blob）」在 D6.13（水位制）之后是否仍作为退位判据；据实证给出 `Lifecycle`/`Retention`/`MemoryGuard` 的**留（接线到水位制 keep 判据）或删（零调用且语义重复）**结论，不允许留成「存在但零调用、语义与判据冲突」的第三态。

### 边界与依赖（本轮增补）

- **依赖**：06.R2（引擎索引与切面的维护点；退位后身份读的定位依赖它）、11.R2（整事实提交；搬迁的复制/校验单位要与提交单位一致才有「一身份一不可变事实」的边界）、D6.13 裁定原文（design D6.13「正确性＝identity-read＋全局提交序；存活权威是性能层非正确性门；判据简化水位制」）。
- **被依赖方**：09 的 S1 锚与 F3 回滚证据、10 的契约同步（本域 spec 与代码同向是本域自己的责任，不推给 10）、11.R3（`recover-kv` 的语义随提交闭合收缩）。
- **禁止事项（本轮增补）**：不以「删事实」换有界；不另立第二回收 oracle（D6.3）；不留零调用脚手架作为「已存在」的第三态；不在热层移除前跳过冷层逐字节校验；不改写冻结归档；本阶段只规划。