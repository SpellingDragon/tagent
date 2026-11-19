# 11 整事实原子提交 · 设计

> 静态决策：一级 [design.md](../../design.md) D2（子域 11）、D8（E3/E4 证据与 G1 门）、D9（依赖与并发）、D10（目标—验证覆盖）、D11（边界）。本域只列实施要点与实证触碰集。**未冻结**：接口签名、提交结果类型、档位能力面均为候选（`proposed`），R1 通过后由编排者冻结。
>
> ## ⚠️ 文件级勘误（2026-11-16，退位机制整体删除）
> 本文件多处把「冷层」当作活载体（`segment_store.go`／`kv_original_store.go` 的冷层开档、「退位后已搬身份重交仍判重复」经 `coldEventLocked`／`coldEntryLocked`、`RelocateReport`、`WithRetirement`）。**这些符号与路径已随退位删除**，原文保留为历史证据（5.8），不得据以实施。
> **仍成立且不受影响的部分**（本域的主体）：一身份一不可变事实、`ErrContentConflict`／`ErrFactKeyConflict` 分界、`EventCount` 为全史标量、一次成功提交＝一个号且重复与冲突不占号、32MiB 单帧上限、同根一根一写者（`ErrRootWriteLocked`，flock）。原「退位后的重交判重依赖冷层可读」这一补偿前提消失后，重交正确性只能落在**提交自身的身份判重不变量**上——这正是本域要闭合的面。

## 一、现状机理（✅ 读码实证，行号为当前工作树）

| 笔 | 站点 | 提交单位 | 原子范围 |
|---|---|---|---|
| 判重读 | `persistFact` facts.go:370 `GetEvent` | 单键读 | 无写 |
| 原文笔 | facts.go:377 `Originals.Put` → kv_original_store.go:268、Commit 在 `Put` 尾 | `ref:<key>`（含 `storedRefs` JSON）＋`blob:<key>`（经 `ensureLoaded` 与其内部 Batch） | 原文族内部一笔 |
| 段笔 | facts.go:395 `StoreEvent` → segment_store.go:616-621 | `{pid}:evt:{w}:{s}`、`{pid}:idx:{key}`、`{pid}:meta:{w}` | 事件三键一笔 |

结论：一条事实横跨两笔提交，判重读在两者之前；第一笔成功、第二笔拒绝会留下原文已在而事件未提交的中间状态。仅凭该调用顺序不能声称必然出现相反的「事件在而原文不在」：那需要另外的引擎失约、损坏或并发路径证据。任一提交返回不确定时，须按实际落盘结果处理，不能把所有错误归为零写入。

引擎能力分布：

- journal 档：`Commit` 把整批打成**一个 journalRecord 帧**追加并 `sync` 一次（journal_kv.go:139-165）；失败时帧未完整落盘即整批不发布。跨键原子成立。
- file 档（rustviking）：`Commit` 序遍历 `kv del` 再逐键 `kv put`（rustviking_kv.go:149-163），无暂存态、无跨键原子；文档已在 memory-architecture.md:245/:255 承认。

契约与测试面的现状（这是 R3 的缺口，不是推论）：

- `AtomicStore` 的 JSDoc 明文承诺「Commit is all or nothing: a commit that returns an error publishes no part of its batch」（atomic_store.go:47）。file 档实现违反该承诺却仍实现该接口，且 `Tier.Capabilities` 只有 `Tier`/`Durable` 两个字段（config.go:76-84），**没有原子性声明**——调用方无法在类型或能力面区分。这是「伪装」的确切含义：接口面与能力面，不是文档。
- `TestAtomicStoreContract`（atomic_store_test.go:131）的 `contractEngines()`（:29-34）只列 journal 与 retired_name，**不含 file 档**；`refused_commit_publishes_nothing`（:198-232）的故障注入由包装层（`faultStore`）在引擎看到批之前整批拒绝，因此**原理上不可能看见引擎内部半批**。既有绿不能证明「每个档位原子」。

身份与位置（必须进提交语义，见 proposal「Why」）：`SameFact`（memory/event_body.go:244）＝规范化正文相等 ∧ 范围戳相容 ∧ TaskID 相等；槽位 `{partition,window,seq}` 是位置；`StoreEvent` 命中既有身份时不占新号（segment_store.go:572-600）。

## 二、11.R1 设计门 G1（只调查/原型验证；不通过不得进入 11.R2）

四问，各带判别测试与"不过就停"的条件：

**G1.1 单存储根写者与锁覆盖面。** 枚举全部 `OpenTier`/`OpenAtomicStore` 调用点：生产 boot `assembly/run.go:622`（经 `relocatableStorage`）、正式修复入口 `scripts/recover-kv/main.go:103`（读）与 `:179`（`--restore` 时的独立写者，`:170` Commit）、冷层开档 `segment_store.go:787` 与 `kv_original_store.go:198`、迁移先例 `migrate.go:83/:135`、契约示例 `examples/contract/tool_recall/Table_example.go:35`。
判别测试：区分只读视图与活动写者；同根只读校验不应因另有写者而被一概禁止。两个活动写者分别经事实提交与修复写入口操作同一root，验证身份判断和整笔提交不被交错；若采用拒绝第二写者，只拒写权而非任意第二视图。现状证据：`config.go:185` 注释声称「A second runtime instance over one directory is refused by the production open path that owns that root」，但本轮只读检索在 `memory/ledger` 与 `assembly` 内**未找到** flock/独占标记（全仓 `Flock|LOCK_EX|lockfile` 生产代码零命中）。故该拒绝**当前未证实**，G1 必须用真实入口取证；不成立则「单拥有者」不成立，原子化只能在引擎内保证「整批一次落盘」，跨笔窗口需另写条件分叉。

**G1.2 一次 Batch 能否承载整事实。** 键集：原文族（`blob:`＋`ref:`）＋事件三键，实测键数 ≥4；值大小上限：单帧 ≤32MiB（journal.go:56）。判别测试：以一次性 Batch 提交一条带 20KiB 正文的事实，重开档后四类键全部可读且 EventCount/索引位置不变；再以超过单帧上限的正文验证具名拒绝（不静默截断）。file 档同测试预期**失败或具名拒绝**（见 G1.4）。

**G1.3 提交结果的精确语义。** 四分类及判据：

| 结果 | 判据 | 现状来源 |
|---|---|---|
| 新增 | 身份与位置都不存在，四键同批落地 | `StoreEvent` :601-637 ＋ `Put` :302-345 |
| 已存在（同事实重交） | 事实提交入口按 `SameFact` 判领域等价，返回原事实与原位置；引擎保全原字节，不额外要求调用方重新编码结果逐字相同 | `storedFactConflict` facts.go:436、memory/event_body.go 的 SameFact |
| 冲突 | 同键不同内容：位置不同或正文不同 | `ErrContentConflict`（segment_store.go:573/584/593/597）、`ErrFactKeyConflict`（event_body.go:92） |
| 不确定 | 提交或屏障未能声明结果 | `ErrCommitUncertain`（atomic_store.go:63） |

判别测试：同一身份重交（字节相同、范围戳不同）→ 已存在且不占新号；同键异正文 → 冲突且任何键都不动；注入「屏障后返回不确定」→ 调用方重读后答复新增或已存在，MUST NOT 报成功或失败。

**G1.4 原规范等价。** 原子化不得改变：`SameFact` 判定、槽位分配（`s.commitSeq`/`s.count` 语义，segment_store.go:604/:634-635）、`ErrContentConflict` 与 `ErrFactKeyConflict` 的分界、`EventCount` 为全史标量（:584）、退位后已搬身份重交仍判重复（08 identity-read）。R1判别验证只记录现有正式入口的等价/冲突行为与现有Batch能力，并在隔离测试原型中验证整批承载的可行性，不要求生产合一先完成；生产合一后的 `TestAtomicStoreContract`、`TestRelocatedFactReadsByIdentity`、`TestStorageCapabilities` 等价回归归11.R2/R3。

**G1设计验证通过条件**：四问给出当前行为证据、隔离原型可行性、待实施差距与候选签名；已知生产两笔提交尚未合一不构成R1循环阻断。单拥有者未成立时先确定可执行的隔离方案；原型也不能兑现目标、等价定义不明确或部署后果未明确时不派R2。生产合一后的正确性由R2/R3验证，不能提前要求它作为R1结果。

## 三、候选设计（R1 产出结论；此处为**候选**，不是裁决）

- **甲 一次整事实 Batch（倾向）**：`persistFact` 把原文族与事件三键合成一次 `Commit`。事件族的 `seq`/meta 计算需要 `StoreEvent` 内部状态（`nextWindowMeta`、`commitSeq`），故实现面是「统一事实存储层提供完整提交入口、内部复用引擎原子Batch」，而不是给genericKV加入任务或事实等价规则，也不是在 assembly 手工拼 Batch——否则 assembly 要知道 `{pid}:evt/idx/meta` 布局，违「消费者不特判引擎布局」。前提：G1.1 证明写者单一（或跨笔窗口可被锁覆盖）。
  - 否「assembly 拼 Batch」：把段布局泄漏给装配层；否「先段后原文」（memory-architecture.md:260 的另一候选）：反而让「事件在原文不在」成为常态，身份读要长期容忍孤儿。
- **乙 两笔＋可回滚补偿**：保留两笔，段笔失败时删除已写原文。否：删除已提交字节；且崩溃窗口内补偿不执行，缺陷依旧。
- **丙 条件分叉（仅当 G1.1 不成立）**：单拥有者无法保障时，整事实提交以「引擎持锁的读-判断-写」在一个锁内完成，并把该锁的覆盖范围写成显式前置（含 recover-kv 互斥调用约束）。此分叉标 `proposed`。
- **丁 依赖外部 CLI 批量原子命令**：否。改造面在仓库外，且本变更不能声称能力已具备。

## 四、提交结果的调用方契约

- 提交返回「不确定」时，调用方 MUST 通过能确认引擎持久结果的受控读取裁决；普通未刷新的内存视图不足以证明未落盘时继续保持不确定，不允许猜测（具体重读入口由G1冻结）（现状已有先例：`ErrCommitUncertain` 在 runtime/task 侧被作为未知结果处理）；MUST NOT 由调用方猜测后重交，因为重交在退位后是常态路径（loop.go:1540），猜测会把「已存在」记成「新增」。
- 提交返回「已存在」时，MUST 不写任何键（含不刷新 meta 与不占号）。现状 `StoreEvent` :586 返回 nil 且没有写入，原子化后必须保持。
- 提交返回「冲突」时，MUST 不改动任何既有键；调用方 MUST 用具名错误上报，不重试覆盖。

## 五、触碰集（精确文件；R1 只读，不改）

- 引擎：`memory/ledger/atomic_store.go`（仅 JSDoc／能力声明，不改四方法签名）、`memory/ledger/segment_store.go`、`memory/ledger/kv_original_store.go`、`memory/ledger/journal_kv.go`、`memory/ledger/rustviking_kv.go`、`memory/ledger/config.go`（`StoreCapabilities` 能力面）、`memory/ledger/migrate.go`（仅作复制＋校验先例引用，不改）
- 装配：`assembly/facts.go`（`persistFact` 与 `sameFactOf`/`storedFactConflict` 的判据保持）
- 修复入口：`scripts/recover-kv/main.go`（R3 消费者锚：提交闭合后其语义收缩，是否保留为运维工具由 R3 结论定）
- 测试（既有，作为锚，不假设其断言会随实现自动变绿）：`memory/ledger/atomic_store_test.go:131`、`memory/ledger/cold_archive_test.go`、`memory/ledger/retirement_snapshot_test.go`、`tests/integration/history/storage_capabilities_test.go`、`tests/integration/history/recover_kv_relocated_root_test.go`
- **不改**：`memory/replay/**`（投影纯消费）、`features/evolution/governance/**`、`runtime/task/**`（task 账户的提交语义由 05/12 域另行处理）、`openspec/specs/**` 主规格文本（本轮只改本域 delta）、`examples/wechat-bot/.wechat-config/data`

## 六、风险与回退

- **半批仍在**：若原子化只覆盖 journal 档，file 档仍逐键——必须显式拒绝多键整事实提交，MUST NOT 留「看似成功」。风险是部署若真在 file 档跑事实提交，会在启用后立即响亮失败；这是有意行为（G1.4 的等价面必须以「拒」而非「半批」表达）。
- **锁面误信**：把 `config.go:185` 注释当作既有保障将导致跨进程交错写。G1.1 必须以真实入口取证，不以注释为准。
- **序号语义漂移**：把整事实提交改为一次 Batch 后，`commitSeq`/`count` 的自增点必须仍是「一次成功的整事实提交＝一个号」，否则 08 的冷热全序（`segmentIndexCarried.Commit`）与 `EventCount` 断言漂移。
- **判重读退化**：原子化后若把判重读移进写路径，`SameFact` 的领域语义必须仍在（不能在引擎里退化为字节相等）。
- **回退**：本域是增量（提交形状与能力声明）；回退＝恢复两笔提交＋移除能力声明，file 档回到「逐键＋顺序不变量」并在文档缺口表恢复原状；已提交事实零改动，冷层/热层布局不变。回退后 11.R3 的多键拒绝测试必红，作为回滚证据。

## 七、G1 判决与证据（11.R1 执行轮，2026-11-16）

**判决：G1 ＝ FAIL（部分证实）。** G1.2 / G1.3 / G1.4 三问成立并有原型实证；**G1.1 单拥有者不成立，实测证伪**。按任务行「G1.1 若证明单拥有者不成立，必须给出具名条件分叉与失效范围」，本节给出分叉丙的冻结前提与失效范围。**11.R2 不派**，直至分叉丙被编排者冻结。11.R1 框**未勾**（负锚为红、消费者锚 file 腿 SKIP，见 §七.5）。

### 七.0 源树绑定（ORCHESTRATION §② R19 起强制，三件分开记）

1. **读/量的是 HEAD `9547faca`**，不是工作树。隔离树由 `git archive 9547faca | tar -x -C /tmp/recovery-g1/tree` 建立（exit 0），全部原型与既有腿在该树跑。主树正被另一单编辑（`memory/ledger/segment_store.go`、`assembly/symptom_anchor_recovery_test.go` 等），故**主树零 `go test`／零 `go build`／零 `.go` 写入**。
2. **CI 原始记录一律绑 HEAD**；本节不引用任何 CI 结果，也不以工作树结果解释 CI 红。
3. **工作树候选未被任何设计门判过**，本节不把它当已冻结契约消费，也未删改它。`memory/ledger/segment_store.go` 与 `assembly/symptom_anchor_recovery_test.go` 一律以 `git show 9547faca:<path>` 读 HEAD。

**行号勘误（任务行 → HEAD）**：任务行写 `segment_store.go:787`（冷层开档），HEAD 实为 **`:778`**；任务行写 `:604`/`:634-635`/`:584`，HEAD 实为 **`:598`**（`commit := s.commitSeq + 1`）/**`:626-627`**（`s.commitSeq = commit`／`s.count++`）/**`:1014`**（`EventCount`）。任务行写 `journal.go:56`（32MiB），HEAD 实为 **`:59`**（`journalMaxFrameBytes = 32 << 20`）。差值来自工作树候选的 +9 行，非任务行错误。`assembly/facts.go:370/377/395` 与 `config.go:185` 在 HEAD 逐字相符。

### 七.1 G1.1 单存储根写者与锁覆盖面 —— **不成立（实测证伪）**

**写者枚举（`grep -rn "OpenTier\|OpenAtomicStore" --include='*.go' .`，隔离 HEAD 树，只读）**：生产代码 9 处，其余全为 `_test.go`。

| 站点（HEAD） | 角色 | 写/读 |
|---|---|---|
| `assembly/run.go:622` | 生产 boot，经 `relocatableStorage`（`run.go:506`） | 活动写者 |
| `scripts/recover-kv/main.go:103` | 修复入口读视图 | 读 |
| `scripts/recover-kv/main.go:179` | `--restore` 独立写者（`:170` Commit） | 活动写者 |
| `memory/ledger/segment_store.go:778` | 冷层开档 | 读（退位时写） |
| `memory/ledger/kv_original_store.go:198` | 原文冷层开档 | 读 |
| `memory/ledger/migrate.go:83` / `:135` | 迁移先例（复制＋校验） | 写目标根 |
| `examples/contract/tool_recall/Table_example.go:35` | 契约示例（memory 档） | 写（进程内） |
| `memory/ledger/config.go:147` / `:189` | 定义与唯一内部调用 | — |

**`config.go:185` 注释声称**：「A second runtime instance over one directory is refused by the production open path that owns that root, not by an exclusive lock here」。

**判定：该声称为 FALSE。** 三条独立证据：

1. **开档路径无任何独占标记**。`openJournalLog`（`journal.go:885-935`）只做 `os.MkdirAll(dir, 0o755)` → `rejectLegacyShardLayout(dir)` → `os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)`。`O_APPEND` 非独占，无 pid 文件、无 lock 文件、无 `O_EXCL`。全仓生产代码 `Flock|LOCK_EX|lockfile` **零命中**（唯一 `O_EXCL` 在 `examples/wechat-bot/wechat_files.go:273`，是媒体 intake 的建文件，与存储根无关）。
2. **开档路径唯一的拒绝与第二实例无关**。`rejectLegacyShardLayout`（`config.go:120-137`）只拒「目录内有子目录」或「`shard_*.json`」两种旧分片布局，返回 `ErrJournalLegacyLayout`。它不数写者、不认根拥有者。
3. **原型实测第二写者被接纳**。`TestSecondWriterOnOneRootIsNotRefused`：同根第二次 `OpenTier` **err=nil**；两个写者各自 `StoreEvent` 均成功。

**失效范围（具名，实测后果）**：两写者选同一槽位 `{pid:1, window:1, seq:1}` 时，`1:evt:1:1` 与 `1:meta:1` 为共享键，**后写者的正文覆盖先写者的正文，而先写者的 `1:idx:` 条目仍指向该槽**。重开档后 `GetEvent("g1-writer-a")` 返回 **`body="b"`**——身份读回的是另一写者的字节。这是「一身份一不可变事实」的直接违反，且**静默**（无错误、无告警）。两写者各持 `EventCount=1`，互不可见（B 的索引在开档时定型，A 其后才提交）。

**分叉丙（`proposed`，须编排者冻结后才可派 R2）**：单拥有者既无锁也无拒绝，整事实提交必须在**引擎持锁的「读—判断—写」一个临界区**内完成，并把该锁的覆盖范围写成显式前置：
- 覆盖 `assembly/run.go:622` 与 `scripts/recover-kv/main.go:179` 两个活动写者，**二者互斥调用**（运维约束，须写进 recover-kv 的文档面）；
- **只拒写权，不拒任意第二读视图**（任务行明禁「以拒绝全部读取视图替代写入隔离」）；
- 冷层开档（`segment_store.go:778`、`kv_original_store.go:198`）是读视图，不得被该锁一概禁止；
- 锁必须覆盖**判重读与提交之间**，否则 `SameFact` 判断与写入仍可被另一写者插入。

**未证实项**：跨**进程**（非跨 goroutine）的同根并发未测——原型是同进程两个 `OpenTier`。跨进程 `O_APPEND` 的帧交错由 `journal.go` 的「整帧一次 write」保证不产生半帧，但**不保证两进程的槽位分配不撞**（各进程 `commitSeq` 独立）。 settles 它需要：两个独立进程各持一个 `OpenTier` 写同根同槽，断言正文丢失可复现。

### 七.2 G1.2 一次 Batch 承载整事实 —— **成立**

`TestOneBatchCarriesAWholeFact`（exit 0）三腿：

- **腿 A**：一条 20480 字节正文的事实，五键（`blob:`／`ref:`／`{pid}:evt:`／`{pid}:idx:`／`{pid}:meta:`）打成**一个** `Batch` 一次 `tier.Store.Commit`。独立 `OpenTier` 重开档后**五键全部可读且逐字节相等**。
- **腿 B（新发现，纠正本轮初始假设）**：引擎 Batch **只承载耐久真相**；段索引/`commitSeq`/`count` 是**开档时从 journal 派生**的。实测：提交 raw Batch 后**进程内** `EventCount=0`、`GetEvent` not-held；**重开档后** `EventCount=1`、`GetEvent` held、槽位 `{7,1700000000000,1}` 不变、正文 20480 字节相等。故整事实入口**必须自己推进进程内索引**，但**不需要第二笔耐久写**。本轮初始假设「索引不被 Batch 承载故需补写」是错的，已按实测改写。
- **腿 C（等价基线）**：生产两笔路径（`Originals.Put` + `StoreEvent`）落**完全相同的五键集**，`EventCount=1`、槽位不变。故「一次 Batch」与「两笔」的**键集形状等价**，甲案可行。

**32MiB 单帧上限**：`journal.go:59` `journalMaxFrameBytes = 32 << 20`；执行点在 `journal.go:1088`（`append`，**写盘前**检查）与 `:750`（退位重写）、`:504`/`:855`（读侧）。`TestOneBatchRefusesWhatItCannotCarryWhole`（exit 0）：89481435 字节 payload → 具名拒绝 `"tagent: one journal record carries 89481435 bytes, over the 33554432-byte frame bound"`，**进程内 0 键、重开档 0 键、无静默截断**。

**file 档**：`rustvikingKV.Commit`（`rustviking_kv.go:149-161`）逐键 CLI（先 `kv del` 后 `kv put`），无暂存态、无跨键原子。`StoreCapabilities`（`config.go:78-85`）**只有 `Tier`/`Durable` 两字段，无原子性声明**——调用方无法在类型或能力面区分。故 file 档**不能**承载整事实 Batch；R3 的多键具名拒绝等于 **file 档事实层整体不可用**（`StoreEvent` 本身即 3 键批，`segment_store.go:610-614`），部署后果须由编排者明示批准，本域不承诺 file 档可用。

### 七.3 G1.3 提交结果四分类 —— **成立，但有两处具名缺陷**

`TestFactCommitAnswersNewExistingConflictUncertain`（exit 0）实测判据：

| 结果 | 可观测判据（实测） | 调用方处置 |
|---|---|---|
| 新增 | `StoreEvent` 返回 nil，`EventCount` 0→1，三键落地 | 占一个号 |
| 已存在 | 返回 nil，`EventCount` 不变，**键集逐字节零变动** | 不写任何键、不占号、不刷 meta |
| 冲突 | `errors.Is(err, ErrContentConflict)`，`EventCount` 不变，**键集零变动** | 具名上报，不重试覆盖 |
| 不确定 | `errors.Is(err, ErrCommitUncertain)` | **重读裁决**，MUST NOT 报成功或失败 |

**缺陷一：不确定 ≠ 零写入，且进程内重读是假阴性。** 注入「屏障后返回 `ErrCommitUncertain`」（内层 Commit 已成功）：实测 **3 键已耐久落盘**，而**进程内** `GetEvent` 答 **held=false**（`StoreEvent` 在 Commit 出错时提前返回，从未推进 `s.index`）。**能裁决的读是独立 `OpenTier`**：它答 held=true、正文相等、`EventCount=1`。故 §四「受控读取」必须**明确排除进程内段索引**，否则调用方会把已落盘的事实判成未落盘并重交。

**缺陷二：确定性零写拒绝被误分类为不确定。** `journalKV.Commit`（`journal_kv.go:139-165`）把**任何** `append` 错误一律包成 `ErrCommitUncertain`，包括 `journal.go:1088` 的超帧拒绝——那是**写盘前**的确定性拒绝、零键落地。实测 `errors.Is(err, ErrCommitUncertain) == true`。按四分类它应是**冲突/拒绝**（确定、零写），不是不确定。R2 必须把「append 前的确定性拒绝」与「append/sync 后的真不确定」分开，否则调用方会对一个必然失败的提交反复重读。

**现状入口无法表达四分类**：`Run.persistFact`（`facts.go:355`）只返回 `error`/`nil`，「新增」与「已存在」**都是 nil**（`storedFactConflict` 在 `SameFact` 为真时返回 nil，`facts.go:441`）。四分类需要新结果类型；候选签名（`proposed`，未冻结）：

```go
// FactCommitOutcome 是整事实提交的四分类结果。
type FactCommitOutcome int
const (
    FactCommitted FactCommitOutcome = iota // 新增：四键同批落地，占一个号
    FactAlreadyPresent                     // 已存在：领域等价，零键变动，不占号
    FactConflict                           // 冲突：具名拒绝，零键变动
    FactUncertain                          // 不确定：调用方须以独立开档重读裁决
)
```

**【11.R3 回写：四分类 → 五值，验收面扩充不缩水】** 落地时新增第五值 `FactRefused`（`segment_store.go:716`），理由具名：四值对「**写前即可判定、确定零落地**」的拒绝**无槽位**——它不是新增（什么都没落）、不是已存在（无领域等价对象）、不是冲突（无身份/内容 clash）、不是不确定（确知零写入），塞进任何一值即重造 G1.3 缺陷一/二的形状。故「四分类」在原四值不变的前提下扩为五值，判据：`ErrContentConflict`→`FactConflict`；`ErrCommitUncertain`→`FactUncertain`；`ErrJournalFrameTooLarge`/`ErrCommitAfterClose`→`FactRefused`（均写前确定拒绝）。任务行已按此回写；本域 spec 的「四分类」字样**不在 11.R3 白名单内，未同步**，登记待 10 域契约同步单批改。

### 七.4 G1.4 原规范等价 —— **成立**

- **`SameFact` 是领域等价，非字节相等**（`memory/event_body.go:244`）。`TestFactCommitDomainEquivalenceIsNotByteEquality`（exit 0，跑在 `./memory/`，见 §七.5 偏离）：两个 body 仅差一个未声明的范围戳，`canonical()`（`event_body.go` 内）把 `Source`/`Target`/`TaskID` 等归零，故规范正文相等；`sameRangeStamp` 把未声明方当通配 → **`SameFact=true` 而 `Encode()` 字节不同（306 vs 316 字节）**。
- **两层判据不同，必须保持分层**：引擎 `StoreEvent` 的判重是 **`bytes.Equal`**（`segment_store.go:576`/`:589`），领域判重在 `assembly/facts.go:436` `storedFactConflict` → `SameFact`。实测同一对「领域等价但字节不同」的 body，**引擎返回 `ErrContentConflict`**。故领域门**必须在引擎门之前**跑（现状 `facts.go:370-375` 正是此序）。R2 把判重读移进写路径时，`SameFact` **必须留在领域层**，引擎不得退化为字节相等——否则合法的重交会被判成冲突。
- **槽位与 `EventCount`**：`commit := s.commitSeq + 1`（`:598`）、`s.commitSeq = commit`／`s.count++`（`:626-627`）、`EventCount()` 返回 `s.count`（`:1014`，全史标量）。实测一次成功提交＝一个号；重复提交不占号；冲突不占号。
- **冲突分界**：`ErrContentConflict`（`segment_store.go:566`/`:576`/`:586`/`:589`，槽位或字节）是**引擎**门；`ErrFactKeyConflict`（`event_body.go:92`，由 `facts.go:444` 抛出）是**领域身份**门。二者不得合并。
- **退位后已搬身份重交仍判重复**：`StoreEvent` 在热层未命中时走 `coldEventLocked`（`:793`），命中冷层则比槽位与字节，相等返回 nil 不占号（`:583-591`）；`kvOriginalStore.Put` 同样经 `coldEntryLocked` 认已搬原文为已提交（`kv_original_store.go:294-303`）。既有腿 `TestRelocatedFactReadsByIdentity`／`TestRetirementSettlesOnlyTheKeysItsSnapshotRead` 复跑绿（§七.5）。

### 七.5 原型锚、实际命中数、原始退出码

**全部在隔离 HEAD 树 `/tmp/recovery-g1/tree` 跑，非工作树，非已入库产品测试。** 文件：`memory/ledger/g1_batch_prototype_test.go`、`memory/g1_domain_prototype_test.go`、`tests/integration/history/g1_single_owner_prototype_test.go`（三者**只存在于隔离树**，主树零 `.go` 写入）。

| 锚 | 命令 | RUN | PASS | FAIL | SKIP | 原始 exit |
|---|---|---|---|---|---|---|
| 正锚 1 | `go test ./memory/ledger/ -run '^TestOneBatchCarriesAWholeFact$' -count=1 -v` | 1 | 1 | 0 | 0 | **0** |
| 正锚 2 | `go test ./memory/ledger/ -run '^TestFactCommitAnswersNewExistingConflictUncertain$' -count=1 -v` | 1 | 1 | 0 | 0 | **0** |
| 负锚（超帧） | `go test ./memory/ledger/ -run '^TestOneBatchRefusesWhatItCannotCarryWhole$' -count=1 -v` | 1 | 1 | 0 | 0 | **0** |
| 单拥有者判别 | `go test ./memory/ledger/ -run '^TestSecondWriterOnOneRootIsNotRefused$' -count=1 -v` | 1 | 1 | 0 | 0 | **0** |
| 四锚合跑 | `-run '^(TestOneBatchCarriesAWholeFact\|TestFactCommitAnswersNewExistingConflictUncertain\|TestOneBatchRefusesWhatItCannotCarryWhole\|TestSecondWriterOnOneRootIsNotRefused)$'` | **4** | **4** | 0 | 0 | **0** |
| 领域等价腿 | `go test ./memory/ -run '^TestFactCommitDomainEquivalenceIsNotByteEquality$' -count=1 -v` | 1 | 1 | 0 | 0 | **0** |
| **负锚（单拥有者）** | `go test ./tests/integration/history/ -run '^TestConcurrentWritersCannotInterleaveOneFactCommit$' -count=1 -v` | 1 | 0 | **1** | 0 | **1（红＝门证据）** |
| 消费者锚 1 | `go test ./memory/ledger/ -run 'TestAtomicStoreContract\|TestRelocatedFactReadsByIdentity\|TestRetirementSettlesOnlyTheKeysItsSnapshotRead' -count=1 -v` | **15**（3 顶层＋12 子） | 3 顶层＋12 子 | 0 | 0 | **0** |
| 消费者锚 2 | `go test ./tests/integration/history/ -run '^TestStorageCapabilities$' -count=1 -v` | **8**（1 顶层＋7 子） | 1 顶层＋6 子 | 0 | **1** | **0** |

**消费者锚 2 的 SKIP 必须如实记**：file 档腿 `TestStorageCapabilities/file_tier_runs_the_real_rustviking_CLI_and_names_its_absence` 在 `storage_capabilities_test.go:321`（HEAD）`t.Skipf`，因本机无 `rustviking` CLI（resource-gated，既有先例）。故 **file 档的原子性/拒绝面本轮零动态证据**，归 R3 的资源 gated 跑。

**偏离（如实登记，未缩水验收面）**：
1. 领域等价腿跑在 `./memory/` 而非任务行点名的 `./memory/ledger/`——`memory/event_body.go:29` 导入 `ledger`，故 `ledger` 包**不能**导入 `memory`（循环）。`SameFact` 腿无法落在 ledger 包。锚名与断言逐字未改。
2. 任务行点名的 `TestConcurrentWritersCannotInterleaveOneFactCommit` 落在 `tests/integration/history/`（与任务行一致），**为红**。按任务行「若第二写者无拒绝也无有效串行/条件写保障，则记录设计门未通过」，红是**预期门证据**，不是夹具缺陷。
3. 本轮**未**建立 `memory/ledger/atomic_store_test.go`／`segment_store_test.go`／`storage_capabilities_test.go` 的追加用例——主树这三面只读（R19），且另一单正在编辑 `segment_store_test.go`。原型全部落在隔离树的新文件，主树零写。

### 七.6 未 examination 项（不冒充已核）

- **跨进程**同根并发（原型是同进程两个 `OpenTier`）。
- **file 档动态行为**（CLI 缺席，SKIP）；`rustvikingKV.Commit` 只读码确认逐键，未跑。
- **退位趟与整事实提交的接缝**（06/08 域），本轮只读 `coldEventLocked`／`coldEntryLocked` 的判重分支。
- **G2/G3/G4 未判**，本节不声称它们成立；**产品不可发布**，F1–F4 全未完成。
- `scripts/recover-kv/main.go:170` 的 `--restore` 写者**未动态跑**（只枚举与读码）。
- 工作树候选（R19 的 15 文件）**未消费、未删改、未判**。

## 八、11.R3 执行记录（引擎一致性与档位能力面）

**源树绑定**：主树工作树（HEAD `9547faca` ＋ 11.R2/11.R3 已落地候选）。本节全部命令在主树跑，原始 exit 见 §八.3；未引用任何 CI 结果。

### 八.1 file 档的裁定：**具名拒绝多键，不伪装原子**

`rustvikingKV.Commit`（`rustviking_kv.go`）逐键驱动 CLI（`kv del` / `kv put`），无暂存态、无跨键原子。二选一：

- **满足契约**：需要一个原子应用点。CLI 没有；用补偿回滚即设计 §三「乙」，被否（删已提交字节，且崩溃窗口内补偿不执行）。
- **具名拒绝**（采纳）：`Commit` 对 `len(Deletes)+len(Puts) > 1` 的批次**在任何 CLI 调用之前**返回 `ErrBatchNotAtomic`（`atomic_store.go`），零键落地，0 键空批仍只完成屏障，单键仍可用。

故 `AtomicStore` 的 all-or-nothing 契约**对每档成立**：file 档以「拒绝承载」兑现它，而不是以「半批落地」违反它。

能力面（`StoreCapabilities.AtomicBatch`，`config.go`）随档声明：memory/journal/localfile `true`，file `false`。锚钉住「声明 == 行为」：任一档的声明与其三键批的实测结果不一致即红，故能力面不可能靠注释说谎。

### 八.2 部署后果（**上报项，非本叶自裁，未批准**）

`StoreEvent` 本身是 3 键批（`segment_store.go:591`）、`Originals.Put` 是 2 键批（`kv_original_store.go:291`），故 **file 档的事实层整体不可用**：`storage.type: file` 的部署在**首个事实提交处响亮失败**（`ErrSegmentBatchBarrier: ErrBatchNotAtomic: …`），不会有任何静默半批。动态确证：`TestTheFileTierNamesTheFactLayerItCannotCarry`（见 §八.3）在同一夹具上断言 `Put`/`StoreEvent` 双双具名拒绝、CLI 零 mutation、`EventCount()=0`。这是「无法兑现原子承诺时的候选安全处理」，**不是用户已批准的部署降级**；若部署面需要 file 档承载事实，就走「满足契约」那一支，而不是放宽本拒绝。

### 八.3 锚与原始退出码（主树工作树）

| 锚 | 命令 | RUN | PASS | 原始 exit |
|---|---|---|---|---|
| 正锚（能力面 == 行为） | `go test ./memory/ledger/ -run '^TestEveryEngineCommitsABatchWholeOrNamesTheCapabilityItLacks$' -count=1 -v` | 1 顶层＋4 子（journal/retired_name/memory/file） | 5 | **0** |
| 负锚（第 2 键失败不留第 1 键） | `go test ./memory/ledger/ -run '^TestASecondKeyFailureLeavesNoFirstKeyPublished$' -count=1 -v` | 1 | 1 | **0** |
| 事实层具名拒绝（部署后果） | `go test ./memory/ledger/ -run '^TestTheFileTierNamesTheFactLayerItCannotCarry$' -count=1 -v` | 1 | 1 | **0** |

**负锚红绿对（非空转证明，实测）**：把 `rustvikingKV.Commit` 的多键守卫临时改回逐键（`_ = keys`），同一命令 **exit 1**，原文 `atomic_store_test.go:372: the refused batch published "0:evt:1:1" = "first", want no key of it`，同跑的正锚亦红（`the refused three-key batch answered memory: rustviking CLI call failed: …`）；按 md5 复原后复跑 **exit 0**（`rustviking_kv.go` md5 `ea1bf74a…`）。

**file 档 stub CLI**：三锚用 `writeRustVikingStub` 写的 shell stub（每键一文件、`fail-at` 文件指定第 N 次 mutation 失败），故 file 档的引擎级行为**不依赖资源门**；`TestStorageCapabilities` 的 file 腿仍走真 CLI，本机无 `rustviking`（`which` exit 1）→ `storage_capabilities_test.go:333` `t.Skipf`，**该腿仍无动态证据**，如实登记。

### 八.4 file 腿改写（未缩水）

原 file 腿断言「file 档能提交事实并重开读回」，与本裁定直接冲突。改写为同一资源门下的**同强度断言**：能力面 `AtomicBatch=false`、单键仍可提交并重开读回、事实（3 键批）**具名拒绝且零键落地、`EventCount()=0`**、重开档仍无该事实。断言面未缩：原「事实可端到端落盘」这一条被**分档**——journal/localfile 覆盖（既有腿），file 档改由「拒绝承载」覆盖。

### 八.5 未落地项（如实，非「已完成」）

- **`landedSeq` 只在 `FactCommitted` 推进**（R46 自裁二的补锚）：`landedSeq` 住在 `assembly/run.go:379` / 由 `assembly/facts.go:400` 推进，**`assembly/` 不在 11.R3 白名单**，故该负锚**未落地**。在 `memory/ledger` 侧写等价断言会是**假锚**——ledger 的 `commitSeq`/`EventCount` 在重复提交时本就零变动，该断言**修复前即绿**，量不到 `landedSeq` 的那处回归。须派 `assembly/facts_test.go`（11.R2 三例的落点）补，建议形状：同一事实提交两次后，下一新事实须落在 `landedSeq[partition]+1` 而非 `+2`（`Run.persistFact` 未导出，正式入口为 `Run.Invoke` 驱动的事实提交，或经 `CheckpointFiled()` 的 `event.CheckpointOffset` 读回该分区水位）。
- `scripts/recover-kv/main.go` **未改**：其 restore 逐键提交（`main.go:162` 单键 `blob:` 批），不触多键拒绝面，语义无需收紧。

**11.R4 回写**：上条 `landedSeq` 负锚**已落地**为 `assembly/facts_test.go` 的 `TestRepeatedFactDoesNotAdvanceLandedSequence`，走正式入口 `run.commitFactChecked` 读 `checkpointWatermark` 断言「重复不推进水位、不花新号」，并配非空转红绿对（把花号前移到重复前置判重处 → 红）。同轮把 `CommitFact` 的写前拒绝分类由名字白名单改为契约反演（只有 `ErrCommitUncertain` 归不确定，其余一概 `FactRefused`），证据见 tasks.md「11.R4 执行记录」。

### 八.6 全树验证（主树工作树，原始 exit）

`go build ./...` **0**；`go vet ./memory/ledger/ ./scripts/recover-kv/` **0**；`./memory/ledger/` **0（32.251s）**；`./scripts/recover-kv/` **0（1.621s）**；`./tests/...` **0（13 包 ok，0 FAIL）**；`./assembly/` **0（281.152s）**；注释棘轮门 **0 findings／0 beyond baseline**；产品包零容忍门 **0 findings**；四锚合跑 **20 RUN／20 PASS／0 FAIL／exit 0**（新增三锚＋既有 `TestAtomicStoreContract` 全腿）。

**一处如实登记（未按 flaky 关闭）**：`./tests/...` **首跑**红一处——`tests/integration/execution` 的 `TestNativeConversationRoundTrip/a_native_multi-turn_tool-call_roundtrip...`，原文 `native_conversation_roundtrip_test.go:93: settled observation(s) of [call-1] never reached a model request in 10s`。归属取证：该测试独占跑 **exit 0（0.368s）**、该包独占跑 **exit 0（48.971s）**、整包在 13 包并行下复跑 **ok 51.727s**，且它走 localfile 档而本叶 diff 只及 file 档与能力面字段 → 判为「13 包并行 ＋ host 216s 负载」下的 wall-clock 抖动，非本叶引入。**未改断言、未改超时、未跳过**。
