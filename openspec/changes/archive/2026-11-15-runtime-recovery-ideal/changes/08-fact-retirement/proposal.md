# 06 退位分层（用户裁定乙）

## Why

用户原症状：「目前 WAL 的 journal.log 会无限膨胀」。一级 design §六 S1 直译：持续写入 N 事实＋折叠＋投递结清后，`journal.log` 字节 SHALL 有界于存活键集，不随历史线性增长——今天红（无界）。

物理无界的成因是引擎形态，不是缺陷：`journalLog` 是单写者只追加文件（journal.go:97-102），`order` 每帧每键永不回收（journal.go:115-121），开档整份回放（journal.go:471-501 → journalReplay:433）。01/02/03 治的是**恢复成本**（读切片、派生索引、尾增量），治不了**磁盘厚度**：已结事实的字节仍留在热文件里。

用户裁定**乙**（一级 D6.1）：物理有界取向＝退位，否甲（接受无界＋删脚手架）。乙是教义兼容延伸，因为红线只禁删除与改写，不禁搬迁：

- session-checkpoint-material/spec.md:44：「任何有界化手段 SHALL NOT 删除、覆盖或改写已提交事实；恢复期读全史的消费者 SHALL 仍能按身份读到其正文」。
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

- `fact-retirement`（ADDED）：已结事实搬迁非删除、按身份恒可读、热 journal 有界于存活键集、排他安静点、保序、冷层损坏具名拒绝、journal-only 能力诚实。
- `storage-lifecycle-completion`（MODIFIED）：脚手架生产接线、回收判据改链可达（非龄期）、义务租约由共享 owner 持有、原文可读至搬迁**且搬迁后按身份恒可读**。

## 边界与依赖

- **依赖（W3＝W1+W2 绿）**：01/02/03 建立的存活键集权威（§五 `checkpoint.Survivors`；现状 api/event/checkpoint.go:62 只有 `Offset` 水位，折叠幸存者今在 `metaKeyCompressSurvivors`，compress_consumer.go:41/565，由 02 收编为索引）；02 的派生索引加载（冷层懒开不得把 O(全史) 搬回 boot）；既有 Stateful 接缝（runtime/state/checkpoint.go:36）、AtomicStore 缝、安静点 drain（assembly/checkpoint.go:99）、MigrateShardedRoot 的复制＋校验半段（migrate.go:76）。
- **被依赖方**：一级 S1 症状锚与 F3 DoD；远端 wechat-bot 部署磁盘（一级 §七）；全部按身份读与读全史的消费者——assembly/read.go:118/174/202/236/328、run.go:2281（CommittedEvent）/2304（eachCommittedEvent）、host_tools.go:926（eachDeliveryFact 全史扫）、facts.go:370/401（重复提交检查）、scripts/recover-kv/main.go:140（正式修复入口）。
- **接口面**：新增 journal-only capability（域内声明，编排者核冲突）；`StorageConfig`（config.go:46）与 `hostStorage`（host_document.go:76-79）增退位开关与冷层根，经 host.go:635 接线；`LifecycleStore` 三方法语义在退位路径上＝搬迁（tombstone＝待搬迁标记，Reclaim＝复制＋校验＋热层移除），须与 05 域同步主 spec 与 golden、过 schema_allowlist 门。
- **禁止事项**：
  - **SHALL NOT 删除、覆盖或改写已提交事实**（spec.md:44）；热层移除只发生在冷层逐字节校验通过之后，且 `GetEvent` 恒返回原字节。
  - **SHALL NOT 因搬迁返回 missing**（§五 identity-read 不变量）；冷层读不出＝具名拒绝（ErrSegmentBodyMissing，segment_store.go:205/546），不静默丢事实。
  - 不另立回收 oracle（D6.3）；不以龄期/容量单独决定搬迁。
  - 不做 file 档（rustviking）退位（D6.7）；不触 memory/replay 投影（纯回放不写链）。
  - 不触外来未跟踪文件、冻结归档（archive/** 只附勘误）、config-surface-normalization 未提交变更与 agent 配置面（本域只动 storage 面）。
  - 不触 `examples/wechat-bot/.wechat-config/data`；远端部署启用声明由一级收尾协调，本域只在测试夹具声明开关。
  - 本文档阶段**只规划不实施**。