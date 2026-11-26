# 签发区 · bounded-hot-log 01.104

叶 01.104。候选指纹 `6e90d6f104cc138b2337d15ac637f7d43fb92342`（开工与收口两次 `git rev-parse HEAD` 均此值）。写面：本文件新建；产品代码与本变更其它叶产物只读（`git status --porcelain api/ memory/ assembly/ tool/ docs/` 为空）。

读法：

- 本文件是 02.201、02.202、02.203、02.204、03.301 五个实施叶的**签发区**。每叶十字段，逐字段可 grep：`^- \*\*<字段名>\*\*`。字段名固定为：候选指纹｜已证/已驳｜所选/否决｜精确写面｜接口/配置变化｜真实消费者｜三锚全名与命令｜崩溃切点注入｜回滚触发｜独立设计审。
- `blocked: <原因>` 表示该字段无可如实填写的内容。按 [design.md](../design.md) D4「缺项则该叶 blocked」，含 blocked 字段的叶**不进入实施**；解锁动作写在该叶末行。
- `n/a` 是填写而非缺项：只用于该叶根本不承载该形态者，并附理由（供独立审反驳）。
- 第⑩项 `pending（01.104 审后填）` 是 01.104 自身的待填项，不计入 blocked；叶状态只由①–⑨决定。
- 性质句的 canonical 家不在本文件：承诺在 [archive.md](archive.md) §六（C1–C7），判据形式与符号在 [evidence/bounds.md](../evidence/bounds.md) §一–§二，增长构成在 [evidence/growth.md](../evidence/growth.md)。本文件只引用 id 与锚名，不复述改写。
- 数字一律溯源到上两文件的节号，不在此另立第二份读数。

## 一 签发区总表

| 叶 | ①指纹 | ②已证/已驳 | ③所选/否决 | ④写面 | ⑤接口/配置 | ⑥消费者 | ⑦三锚 | ⑧崩溃切点 | ⑨回滚 | ⑩独立审 | 状态 |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 02.201 段轮转与封段索引 | ok | ok | ok | ok | ok | ok | ok | ok | ok | pass | **ok** |
| 02.202 搬迁与 manifest | ok | ok | ok | ok | ok | ok | ok | ok | ok | pass | **ok** |
| 02.203 热盘 GC 与索引收缩 | ok | ok | ok | ok | ok | ok | ok | ok | ok | pass | **ok** |
| 02.204 定位读路径 | ok | ok | ok | ok | ok | ok | ok | ok | ok | pass | **ok** |
| 03.301 消费面跨档一致性 | ok | ok | ok | ok | ok | ok | ok | ok | ok | pass | **ok** |

三处曾 blocked（同一形态：tasks 行内第二条负锚只有描述、无锚名），已按编排者裁定在 tasks 行内补名并同步本表解除（α⁻ 逐字冻结）：02.201＝`TestUnrebuildableSegmentIndexIsRefusedByName`、02.203＝`TestGCNeverDeletesASegmentTheManifestDoesNotState`、03.301＝`TestRecallRefusesArchiveBytesThatChanged`（签发时标注拟新增，当前均已在测试树实存）。如需改名，由 01.104 独立审提出。

**独立设计审状态（按事实）**：2026-11-25 由独立只读子代理对最终工作树复审，复核者未参与产品实现；范围为五叶签发区及其直接引用的 canonical 契约、验收账与当前 diff。首轮发现 02.204 重复计覆盖，修正后逐叶复审，五叶第⑩项均为 `pass`。未查面＝外部 CI 运行结果不属设计审，由 F2/F3 单独绑定；本表 `ok` 表示①–⑩字段齐全且复审通过。

## 二 叶 02.201 段轮转与封段索引

- **候选指纹**：`6e90d6f104cc138b2337d15ac637f7d43fb92342`。
- **已证/已驳**
  - 已证（段界＝cut）：顶层 design D3「切割原语（唯一时点，2026-11-23 用户裁定）」；[archive.md](archive.md) §六 C3 的 condition／fails_when；[bounds.md](../evidence/bounds.md) §1.1–§1.2（段界≡cut，`max_segment` 取相邻两 cut 的 checkpoint 帧末尾之间的 journal 增量）。
  - 已证（段字节与封段数）：[growth.md](../evidence/growth.md) §3.4 十二个间隔段（均值 2,238,272 B，min 2,151,866／max 2,325,068，双份形态）；[bounds.md](../evidence/bounds.md) §6.1 单份化后 `max_seg` 1,138,507–1,205,415 B、`newest_seg` 8,439 B、`cuts` 4/代。
  - 已证（失败 cut 不封段）：[bounds.md](../evidence/bounds.md) §四 M2 口径裁定；02 design D1 末条（残帧留开段，由 `tailFactKeys` 的安静点界识别，`assembly/checkpoint.go:1066`）。
  - 已驳：按字节阈值独立轮转（顶层 design D3 否决，段界须由 cut 序号与提交序确定）；以段内容找锚／「含锚段不搬」的内容检查（顶层 D6：初稿三特判已废除）；初稿「窗下限校验」（同处删除）。
- **所选/否决**
  - 所选：段界只在 cut 提交批的 barrier（`AppendOrdered`，`assembly/checkpoint.go:425` 声明、`:456` 转发）返回成功后推进；段头＋旁置段索引自描述；字节阈值只发「请求提前 cut」。
  - 否决：以帧字节阈值直接封段——段界会依赖调度时刻，破「同输入段界确定」，且与非安静点封段禁令相抵。
- **精确写面**
  - 改写：`memory/ledger/journal.go`、`memory/ledger/journal_kv.go`（同簇）、`memory/ledger/config.go`（`StorageConfig` 三字段＋Engine seam）、`api/config/{config,decode,fingerprint,clone,skeleton_assert}.go`（声明/严格解码/身份面/克隆/骨架断言）、`assembly/run.go`（`:3464-3466` 半声明拒、`:3463` 为 storageDeclared 判定、`:3468`/`:3490` 装配桥）、`assembly/host_document.go`（`:1607-1611` 装配桥、`:1648` `sameMemoryTier`、`:1627` `memoryFieldPathOf`）＋测试。
  - 只读：`memory/ledger/segment_store.go`（`:613` `planEventLocked`、`:791` `CommitFact`）、`assembly/checkpoint.go`（cut 谓词与标记）。
  - 不触碰：`api/config/**`（配置面按下方「接口/配置变化」作共享批由编排者单写）。
- **接口/配置变化**
  - 段头（段文件首记录）：`segment_id`、`first_seq`/`last_seq`、`cut{partition,seq}`、`bytes`、段头自校验（[archive.md](archive.md) §二）。
  - 段索引：旁置归档卷，键→`(offset,length,checksum)`，逐条记录级校验；热封段索引可从段重建但本构建不接通知/计数面，归档段索引由 manifest 陈述摘要、缺失或损坏一律具名拒绝且不在读取路径静默重建（§二）。
  - 段账户：封段数可按段读出，供判据 `sealed_segments(g)` 与 M2（[bounds.md](../evidence/bounds.md) §二、§四）。HEAD 上该结构不存在，故今日读 0（§四 M2 的 `segments-sealed: 0 != 4`）。
  - 配置面（**共享面，编排者单写，本叶前置**；落点本次逐条只读核对）：

    | # | 面 | 位置 | 义务 |
    |---|---|---|---|
    | 1 | 声明结构 | `api/config/config.go:172` `type MemorySpec struct`（现四键 `Type`:174／`Path`:176／`ReadNamespaces`:179／`Engine`:182） | 三字段加此；`Type`/`Path` 的「同进同出」判据不适用它们 |
    | 2 | 严格解码白名单 | `api/config/decode.go:278` `memoryField` 的 `sectionFields(..., "type", "path", "read_namespaces", "engine")` | 不加即被拒 `unknown field` |
    | 3 | 逐键读法 | `api/config/decode.go:282`（type）／`:285`（path）／`:288`（read_namespaces）／`:291`（engine） | 各按 `stringField`／`countField`／`durationField` 取读法 |
    | 4 | 声明身份面 | `api/config/fingerprint.go:103` `canonicalMemory`、`:236` `MemorySpec.canonical()`、`:249` `MemorySpec.isZero()` | 不加则配置变更不进指纹（发布侧读成 unchanged）；`isZero` 不计会让「只声明归档字段」读成无 memory 段 |
    | 5 | 深拷贝 | `api/config/clone.go:58`（`out.Memory.ReadNamespaces = cloneStrings(...)`） | 新字段随拷贝 |
    | 6 | 骨架断言 | `api/config/skeleton_assert.go:60-65` `_ = MemorySpec{...}` 逐字段列名 | 抓改名与换类型，**抓不到新增**；须手工同步 |
    | 7 | 引擎面 | `memory/ledger/config.go:48` `type StorageConfig struct`（`Root`:52、`Engine` seam、`RetentionWindows`） | 三字段加此；`Root` 与 `archive_root` 是两个目录，不合并 |
    | 8 | 装配桥 | `assembly/run.go:3468`（agent 路径）／`:3490`（顶层 `storage` 路径）构造 `ledger.StorageConfig{Type, Root}`；`:3473`／`:3491` `reflect.DeepEqual` 判同一 tier | 新字段须随桥传递，否则声明被丢弃 |
    | 9 | 宿主文档 | `assembly/host_document.go:1607` `MemoryPlanOf(decl, Storage.Type, Storage.Root)`；`:1648` `sameMemoryTier`（`reflect.DeepEqual`） | 顶层段与 memory 段不得两处各一套语义 |
    | 10 | 热更判定与点名 | `assembly/host_document.go:1621` `memoryRefusalOf` → `:1627` `memoryFieldPathOf`（`:1642` 返回 `agents.<id>.memory`）；`assembly/host_reload.go:52` `ErrHostReloadRestartRequired` | 三字段进重启判定即为本期所需 |

  - 半声明拒（[archive.md](archive.md) §五 裁定 (b)）：`archive_root` 与 `hot_log_max_bytes` 须**成对**声明（四向：两向成对＋缺第三腿＋`archive_max_age` 单声明）；任一单向缺失皆为自包含可判的误配，load 期具名拒（`ErrMemoryDeclaration` 家族；先例 `assembly/run.go:3464-3466` 拒「half a storage tier」）。锚拟新增 `TestMemoryDeclarationRefusesHalfAnArchiveConfig`（两向各一例）。
- **真实消费者**
  - 提交：`memory/ledger/segment_store.go:791` `CommitFact` → `:613` `planEventLocked`（一批一帧，六键同帧）。
  - 开档：`memory/ledger/config.go:282` `OpenTier` → `journal_kv.go:56` `openJournalView` → `journal.go:661` `openJournalLogFace`（全日志完整性 pass）。
  - 判据读数面（本叶不新增）：`journal.go:2723` `IntegrityBytesScanned`、`:2771` `IntegrityPassSpan`、`journal_kv.go:140` `JournalBytesOpen`。
- **三锚全名与命令**
  - α⁺ 拟新增 `TestSegmentRotationIsDeterministic`、`TestSealedSegmentIndexLocatesEveryKey`；命令 `go test ./memory/ledger -run '^(TestSegmentRotationIsDeterministic|TestSealedSegmentIndexLocatesEveryKey)$' -v`。
  - α⁻ 拟新增 `TestFailedCutLeavesNoSegmentBoundary`；命令 `go test ./memory/ledger -run '^TestFailedCutLeavesNoSegmentBoundary$' -v`。
  - α⁻（第二条）ok: `TestUnrebuildableSegmentIndexIsRefusedByName`（拟新增；编排者补名后冻结）。
  - αᶜ 实存生产入口：上条「真实消费者」两条路径（无测试名；αᶜ 以入口为凭，见符号表）。
  - 实存核查：`grep -rn "func <T>" --include='*_test.go' .` 对三个拟新增名与建议名均零命中（拟新增标注为真）。
- **崩溃切点注入**
  - 本叶的崩溃形态＝cut 提交批中断（quiet 标记已写、checkpoint 事实未落盘）→ 残帧留开段、不产段界；由 α⁻ `TestFailedCutLeavesNoSegmentBoundary` 钉死。
  - 注入面：`memory/ledger/config.go` 的 `StorageConfig.Engine AtomicStore` 字段注释即「the seam a caller wraps to observe, delay or fail commits」——以同形包装在 `CommitFact` 前后注入失败；不建测试专用入口、不改文件布局。
- **回滚触发**
  - 主触发：从未归档的根在无 `archive_root`（且 `hot_log_max_bytes` 不 >0）时不得封段或轮转，字节行为与今日零 diff（[archive.md](archive.md) C6）。
  - 即时回滚判据：段界依赖时间或调度顺序（α⁺ 红）、索引不可从段重建 → 回退本叶改动；从未归档的根不声明归档配置即零 diff，回退不需数据迁移。
- **独立设计审**：pass（2026-11-25 最终树独立只读复审；本叶签发区及直接引用项已核，外部 CI 不属本项）。
- 解锁状态（原 blocked 已解）：第二条负锚由编排者补名并同步 tasks 行，本叶九字段全签。

## 三 叶 02.202 搬迁与 manifest

- **候选指纹**：`6e90d6f104cc138b2337d15ac637f7d43fb92342`。
- **已证/已驳**
  - 已证（固定序与四切点）：顶层 design D3「搬迁协议」；02 design D2；[archive.md](archive.md) §一。
  - 已证（manifest 单一权威、fail-closed）：[archive.md](archive.md) §一 字段表与失败语义表；归属不可互换（无 manifest 且无最终名制品＝空归档态；有最终名制品而无 manifest、或 manifest 存在但坏→`ErrArchiveCorrupt`；段缺→`ErrArchiveMissing`，§一 末段）。
  - 已证（可搬集合）：C3（段界 ≤ 倒数第二个 cut；最新 cut 段与开段恒热）＋[bounds.md](../evidence/bounds.md) §四 M1。
  - 已证（字节预算以单份化后构成为准）：[archive.md](archive.md) §八 末条＋[bounds.md](../evidence/bounds.md) §6.1；MUST NOT 引用单一化前数字（[growth.md](../evidence/growth.md) §3.4 的 2.24 MB／14.4 KB）。
  - 已驳：热段删除先于 manifest 提交（02 proposal「禁止」）；manifest 损坏当「无归档」（本叶 α⁻；无最终名制品且文件不存在＝合法空归档态，有最终名制品而无 manifest＝`ErrArchiveCorrupt`）；在热卷留段头或段索引副本（C5 fails_when——否则归档键仍让热卷付费）。
- **所选/否决**
  - 所选：`manifest.a`/`manifest.b` 双文件 generation swap，读者取 `generation` 较大且自洽的一份；manifest 格式由 01.104 冻结（ORCHESTRATION §释放「归档 manifest 格式」）。
  - 否决：单文件 manifest＋影子副本——同一时刻无法判定哪一份自洽，且「缺失」与「损坏」两种归因不可分，与 §一 的归属区分冲突。
- **精确写面**
  - 改写：新增 `memory/ledger/archive*.go`（搬迁与 manifest 同簇）＋测试。
  - 只读：`memory/ledger/journal.go`（帧追加与 CRC）、`segment_store.go`（`CommitFact`:791）、`kv_original_store.go`（ref 族随帧）。
  - 不触碰：`api/config/**`（配置面在 02.201 的共享批）。
- **接口/配置变化**
  - manifest 字段：[archive.md](archive.md) §一 九项（`generation`／`segments[].segment_id`／`key_domain`／`bytes`／`sha256`／`cut`／`offset_map`／`watermark`／`archived_through_cut`，及 `watermark` 的热文件重述基线），及 `watermark` 的热文件重述基线 `{hot_seq, hot_values_size, hot_order_size}`。
  - 新错误符号：`ErrArchiveCorrupt`、`ErrArchiveMissing`；与既有 `ErrSegmentBodyMissing`（`segment_store.go:210`）、`ErrJournalCorrupt`（`journal.go:32`）、`ErrOriginalMissing`/`ErrOriginalExpired`（`original_store.go:27`/`:31`）并列，实施时不得另造同义名（[archive.md](archive.md) §三 对照表）。
  - 配置消费：`archive_root`（启用条件之一）、`archive_max_age`（可搬性附加条件：段的提交时间范围须整体早于 `now − archive_max_age`；只加保留、不改切割时点）；两字段的声明与解码在 02.201 的共享批落地，本叶只消费。
- **真实消费者**
  - boot 重开：`memory/ledger/config.go:282` `OpenTier` → manifest 双份读取、最终名集合、结构与字节数判定 → 段定位；层④ sha256 在**读取该段时**实算生效，稳态 boot 不预扫归档；崩溃残留删除最后热副本前只对对应本代段／索引补验摘要（[archive.md](archive.md) §四 四层表与校验时机表）。
  - 搬迁驱动：无对外入口，由 02.201 的 cut 提交批推进（`assembly/checkpoint.go` 提交批路径）。
- **三锚全名与命令**
  - α⁺ 拟新增 `TestArchiveCrashRecoveryIsIdempotent`（四子测，见「崩溃切点注入」）；命令 `go test ./memory/ledger -run '^TestArchiveCrashRecoveryIsIdempotent$' -v`。
  - α⁻ 拟新增 `TestCorruptManifestRefusesArchiveRead`；命令 `go test ./memory/ledger -run '^TestCorruptManifestRefusesArchiveRead$' -v`。
  - αᶜ 实存生产入口：上条 boot 重开序（无测试名；αᶜ 以入口为凭）。
  - 实存核查：两个拟新增名 `grep` 零命中。
- **崩溃切点注入**
  - 四切点（tasks 原文）：①copy 中 ②fsync 后 ③manifest 后 ④删除中，各注入一次；锚 `TestArchiveCrashRecoveryIsIdempotent` 四子测。
  - 与 design 的对应：02 design D2 第三条写作「manifest 后删除前」；ORCHESTRATION §凭据 写作「manifest 前」——后者即 ② 的同一点（fsync 后、manifest 前），四点集一致，本文件按 tasks 四点冻结。
  - 注入面：搬迁器各步骤之间的具名 seam（与 `StorageConfig.Engine` 同形的包装），重开一律走 `OpenTier` 真实路径，不建测试专用入口。
- **回滚触发**
  - 回退分两形态（C6）：从未归档的根可移除 `archive_root` 并零 diff；已归档的根移除声明后归档键按身份读不可得，MUST NOT 被当作受支持回退——受支持形态是保留归档声明、只回退代码，或走离线折回／前向修复；不得宣称「移除 `archive_root` 后已归档段仍按身份可读」。
  - 即时回滚判据：热段先被删、manifest 非唯一、段 sha256 与实算不等 → 回退本叶改动；已搬字节保持只读，不逆搬（逆搬会造第二真相，违 C5）。
- **独立设计审**：pass（2026-11-25 最终树独立只读复审；本叶签发区及直接引用项已核，外部 CI 不属本项）。

## 四 叶 02.203 热盘 GC 与索引收缩

- **候选指纹**：`6e90d6f104cc138b2337d15ac637f7d43fb92342`。
- **已证/已驳**
  - 已证（索引侧必须计入且必须收缩）：[bounds.md](../evidence/bounds.md) §1.2（`hot_bytes = H + R`，`H` 含 `index_side`）、§九❓2（HEAD 上 `index_side` 282 KB→1,131 KB 单调，随槽表容量阶跃）、[growth.md](../evidence/growth.md) §3.8（`order.log` ≈399 B/提交、`slots` 786,496 B＝8,192 槽×96 B、`order.line` 容量×8 B）。
  - 已证（收缩与 manifest 同水位）：02 design D3 第三条；[archive.md](archive.md) C3 的 scenario「收缩水位以 manifest 为准」。
  - 已证（水位结构在代码里）：`slots` 头 `Seq`（`memory/ledger/journal.go:830`）／`ValuesSize`（`:831-832`）／`OrderSize`（`:833-834`），读入与校验 `:1084-1089`。物理重放水位属物理层，与逻辑 cut 不是一回事（[archive.md](archive.md) §七 第 3 条）。
  - 已驳：指向归档键的逐键热索引条目保留（02 spec「热索引随归档收缩」MUST NOT）；两套索引各存一版（02 proposal 禁止）；热卷留段头或段索引副本（C5）。
- **所选/否决**
  - 所选：删热段后按段界清槽表条目、`order.log` 重写为该热段交集（或分段 order 文件）、`slots` 头水位按新热文件重述，三者同一事务面。
  - 否决：保留全量热索引、定位一律走归档——热索引即无界，C1 不成立。
  - 未定项（设计已明许二选一，属本叶权限）：`order.log` 重写交集 vs 分段 order 文件（02 design D3 括注），不影响契约。
- **精确写面**
  - 改写：`memory/ledger/segment_store.go`、`memory/ledger/original_store.go`、`memory/ledger/reference.go`（ORCHESTRATION §三 合单判据：`segment_store.go` 与 GC 叶同簇）。
  - 只读：`memory/ledger/journal.go`（`slots` 头与 order 文件）、`memory/ledger/kv_original_store.go`（ref 族）。
  - 不触碰：`api/config/**`。
- **接口/配置变化**
  - 无新配置字段、无新对外错误符号。
  - 内部面：热索引条目集合 ⇐ 热段键集 ＋ 段级定位条目；`slots` 头 `Seq/ValuesSize/OrderSize` 按收缩后热文件重述（坐标系是收缩后的热文件，不是历史日志）。
  - 索引侧归入 `H`（LHS 的一部分）；`B_overhead` 只含两个 cut 间隔的写入量（RHS 不重复计入，避免判据自消）。**索引侧字节有界于热窗键数**由本叶锚 `TestHotIndexShrinksAfterArchive`（热条目有界于热段键数）承担。
- **真实消费者**
  - 开档收缩后按身份读热段键：`memory/ledger/config.go:282` `OpenTier` → 索引可用性检查 → 读。
  - 三处 `pointReadLog` 窄口共用同一三分：`runtime/state/durable.go:804`、`features/evolution/governance/denial.go:646`、`runtime/task/approval/attention_log.go:148`（[archive.md](archive.md) §三 末段）。
- **三锚全名与命令**
  - α⁺ 拟新增 `TestHotIndexShrinksAfterArchive`、`TestSlotWatermarkRestatesOnShrunkHotFiles`；命令 `go test ./memory/ledger -run '^(TestHotIndexShrinksAfterArchive|TestSlotWatermarkRestatesOnShrunkHotFiles)$' -v`。
  - α⁻ ok: `TestGCNeverDeletesASegmentTheManifestDoesNotState`（拟新增，构造注入；编排者补名后冻结）。
  - αᶜ 实存生产入口：`OpenTier` 收缩序 ＋ 上列三窄口调用面（无测试名；αᶜ 以入口为凭）。
  - 实存核查：两个拟新增名与建议名 `grep` 零命中。
- **崩溃切点注入**
  - 本叶的崩溃窗口＝删热段与收缩索引之间（02 design D3；[archive.md](archive.md) C3 scenario）。注入归 02.202 四切点的 ③④（manifest 后／删除中），本叶只负「重开以 manifest 为准完成删除与收缩，MUST NOT 用热索引反向修正 manifest」，不另立切点。
- **回滚触发**
  - 主触发：从未归档的根在无 `archive_root` 时不收缩、不改水位 → 与今日零 diff（C6）。
  - 即时回滚判据：收缩后按身份读热段键读不出或读错字节、水位重述与 `order.log` 不一致 → 回退本叶；水位可弃、索引可从日志重述（派生索引先例：`memory/ledger/journal.go:84-98` 注释，`order.line` 一条字缺失只费一次 seek，不废答案）。
- **独立设计审**：pass（2026-11-25 最终树独立只读复审；本叶签发区及直接引用项已核，外部 CI 不属本项）。
- 解锁状态（原 blocked 已解）：负锚由编排者补名并同步 tasks 行，本叶九字段全签。

## 五 叶 02.204 定位读路径

- **候选指纹**：`6e90d6f104cc138b2337d15ac637f7d43fb92342`。
- **已证/已驳**
  - 已证（读序与三态）：[archive.md](archive.md) §三 读序四步与三态表；`pointReadLog` 三分映射 `ErrOriginalMissing→(nil,false,nil)`、`ErrArchiveMissing`/`ErrArchiveCorrupt→(nil,false,err)`，三处窄口行号本次已核。
  - 已证（boot 不预扫归档）：§四 表（稳态 boot SHALL 只校验热段＋manifest/段索引结构与字节数，MUST NOT 预扫归档段字节；崩溃残留删除最后热副本前只对对应本代段／索引补验摘要，不得以段头自述摘要替代实算）；C4。
  - 已证（现状两条读法）：`FileSegmentStore.GetEvent`（`segment_store.go:877`）＋`kvOriginalStore.ReadSegmentScoped`（`reference.go:297`）；旧根 blob 读法（`resolveLocked`，`kv_original_store.go:169`）与事件体读法（`:196` `eventBodyLocked`／`:219` `storedEventText`）在 HEAD 上并存——跨档不得改变二者的字节结果（[archive.md](archive.md) §八）。
  - 已驳：任一态折叠成「不存在」（C7 fails_when；Corrupt 落到「不持有」被 §三 明文禁止）；把归档正文复制进热缓存形成第二真相（03 proposal 禁止）；为跨档另立第二读路径（03 spec MUST NOT）。
- **所选/否决**
  - 所选：单一路径「热索引→manifest→段索引→段」，三态原样上返。
  - 否决：读失败即回落全史扫描——把损坏藏成慢，且 `archive-tail-read` 只属损坏恢复（顶层 D6），不是常规读形态。
- **精确写面**
  - 改写：`memory/ledger` 读入口一侧的定位分派（`segment_store.go` 的 `GetEvent*` ／ `reference.go` 的 `GetScoped`、`ReadSegmentScoped`）＋测试；具体文件以 02.203 合单边界为准，**串行写**（ORCHESTRATION 授权：共享面编排者单写或串行）。
  - 文档：`api/durable/fact_log.go`（`FactLog` 读契约说明面）。
  - 只读：`memory/ledger/archive*.go`（02.202 产物）、`tool/recall/get.go`（消费者）。
- **接口/配置变化**
  - 读入口行为：三态落到既有三分；`pointReadLog` 窄口语义不变（`Get(ctx, key) ([]byte, bool, error)`）。
  - `hot_over_budget` 的状态与告警落点在本叶（[bounds.md](../evidence/bounds.md) §十 明列 02.204；实现 02.207）：正常负载为 0；状态为 1 时 C1 不可宣称；成功 cut 不单独归零，须 seal 后观察同时排除「高于生效上限」与「配置低于结构下限」才归 0。03.302 的 S1 同锚断言其为 0。
  - 无新增配置字段（消费 02.201 的启用谓词 `archive_root` 非空 ∧ `hot_log_max_bytes > 0`）。
- **真实消费者**
  - `recall_get`：`tool/recall/get.go:115` `getTool.Call` → `:153` `ReadSegmentScoped`。
  - `recall_trace`：`tool/recall/trace.go:87` `traceTool.Call`。
  - governance 尾读：`features/evolution/governance/denial.go:658` `governanceTail`。
  - boot 恢复序：`config.go:282` `OpenTier` → `journal_kv.go:56` `openJournalView` → `journal.go:661` `openJournalLogFace`；稳态 boot 只校验热段与 manifest/段索引结构；崩溃残留删除最后热副本前对对应本代段／索引补验摘要。
- **三锚全名与命令**
  - α⁺ 拟新增 `TestIdentityReadCrossesHotAndArchive`；命令 `go test ./memory/ledger -run '^TestIdentityReadCrossesHotAndArchive$' -v`。
  - α⁻ 拟新增 `TestArchiveMissingIsNamed`、`TestArchiveCorruptIsNamed`；命令 `go test ./memory/ledger -run '^(TestArchiveMissingIsNamed|TestArchiveCorruptIsNamed)$' -v`。
  - αᶜ 实存生产入口：上条四路径（无测试名，以入口为凭）；工具入口的锚归 03.301 `TestRecallGetCrossesArchive`（拟新增，跨叶引用，不重复计入本叶）。
  - 粒度声明：`TestMissingArchiveIsNamedNotAbsent` 是 `TestArchiveMissingIsNamed` 的薄封装别名，`TestCorruptArchiveIsNamedNotAbsent` 同理；二者不构成独立消费面粒度，不得重复计覆盖（[archive.md](archive.md) 附 末条）。
- **崩溃切点注入**
  - n/a：本叶无持久化写入，坏形态由三态 α⁻ 承担；崩溃窗口属 02.202（搬迁）与 02.203（收缩），不在此重复。
- **回滚触发**
  - 从未归档的根在无 `archive_root` 时定位读退化为热索引直读，行为与今日零 diff（C6）；已归档的根移除声明后归档键不可得，不属受支持回退（C6 形态二）。
  - 即时回滚判据：三态任一折叠、`ErrArchiveCorrupt` 落到「不持有」、读路径出现第二份正文 → 回退本叶。
- **独立设计审**：pass（2026-11-25 最终树独立只读复审；本叶签发区及直接引用项已核，外部 CI 不属本项）。

## 六 叶 03.301 消费面跨档一致性

- **候选指纹**：`6e90d6f104cc138b2337d15ac637f7d43fb92342`。
- **已证/已驳**
  - 已证（消费面清单与禁止）：03 design D1；03 spec「消费面共用一条身份定位读路径」（MUST NOT 另立第二读路径或把归档正文复制进热卷）。
  - 已证（入口实存）：`tool/recall/get.go:115`/`:153`、`tool/recall/trace.go:87`、`features/rl/export_chain.go:159` `ChainExporter.Export`、`:644` `EvidenceExporter.SFT`；`memory/query/` 包在（`reading.go`／`inventory.go`／`snapshot.go`）。
  - 已证（四态词汇）：`RecallMissing`/`RecallExpired`/`RecallFailed`/`RecallFound`（`memory/ledger/reference.go`；`tool/recall/get.go:221` `readReason` ↔ `docs/wiki/memory/memory-architecture.md` §3.1）——跨档沿用四态，不新增第五态（[archive.md](archive.md) §三 对照表）。
  - 已驳：`recall` 或导出各自缓存归档正文（第二真相，03 design D1「禁止」）；为跨档改导出格式或事实身份（03 design D3 停止条件）。
- **所选/否决**
  - 所选：四类消费面全部经同一入口（`GetScoped`／`ReadSegmentScoped`），三态映射为既有四态，Missing/Corrupt 不落「不存在」。
  - 否决：在 `tool/recall` 内按归档位置做预取或旁路——把读延迟与第二真相一起带进工具层。
- **精确写面**
  - 改写：`tool/recall/*`、`memory/query/*`、`features/rl/*`（导出读）＋测试。
  - 只读：`memory/ledger`（02.204 的定位读入口）。
- **接口/配置变化**
  - 无新配置字段；工具参数面不变（`recall_get` 的 `offset`/`max_bytes` 已是分段读，`tool/recall/get.go` `clampRead`）。
  - 呈现面：`recall_get` 须把 `ErrArchiveMissing`/`ErrArchiveCorrupt` 映射为具名理由（经 `readReason(err)`，`get.go`），不得复用「不存在」理由串。
- **真实消费者**
  - 真实工具入口：`getTool.Call`（`tool/recall/get.go:115`）、`traceTool.Call`（`trace.go:87`）。
  - 导出入口：`ChainExporter.Export`（`features/rl/export_chain.go:159`）、`EvidenceExporter.SFT`（`:644`）。
  - 历史查询：`memory/query/` 读面（`reading.go`／`inventory.go`）。
- **三锚全名与命令**
  - α⁺ 拟新增 `TestRecallGetCrossesArchive`、`TestRecallTraceReportsArchiveMissing`；命令 `go test ./tool/recall -run '^(TestRecallGetCrossesArchive|TestRecallTraceReportsArchiveMissing)$' -v`。α⁺ 拟新增 `TestExportReadsArchivedFactsByIdentity`；命令 `go test ./features/rl -run '^TestExportReadsArchivedFactsByIdentity$' -v`。
  - α⁻ ok: `TestRecallRefusesArchiveBytesThatChanged`（拟新增；编排者补名后冻结，「代码核查」不作锚）。
  - αᶜ 实存生产入口：上条四入口（无测试名；αᶜ 以入口为凭）。
  - 实存核查：三个拟新增名与建议名 `grep` 零命中。
- **崩溃切点注入**
  - n/a：只读消费面，无持久化写入；坏形态由 α⁻ 与 02.204 的三态锚承担。
- **回滚触发**
  - 从未归档的根在无归档配置时四类消费面输出与今日逐字节相同（03 spec「回退零差异」）；已归档的根移除声明后归档键不可得，MUST NOT 记作零差异回退。
  - 即时回滚判据：任一消费面出现第二读路径或本地缓存正文 → 回退本叶。
- **独立设计审**：pass（2026-11-25 最终树独立只读复审；本叶签发区及直接引用项已核，外部 CI 不属本项）。
- 解锁状态（原 blocked 已解）：负锚由编排者补名并同步 tasks 行（「代码核查」不作锚），本叶九字段全签。

## 七 01.105 实施补登

- **提交**：`6e90d6f104cc138b2337d15ac637f7d43fb92342`（`git show --stat`：10 文件，+399/−74；本叶为 `bounded-hot-log 01.105 A`）。
- **写改动点**
  - `memory/ledger/kv_original_store.go:390` `planFactLocked` 的 `puts` 只含 `ref:`（`:420`）——事实提交不再产 `blob:` 副本；`ref:` 账目照旧（`OwnerAgent/OwnerSession/Format/RetentionPolicy/StoredAt/Digest/Length`，`:407-415`）。
  - `memory/ledger/kv_original_store.go:314` `Put`（无事件可承载的裸写）仍在 `plan.bodyHeld` 时前置追加 `blob:`（`:334-336`）——旧机制不下线。
  - `memory/event_body.go` 包注释改述：正文在事件体内，原文仓只留引用元数据，旁侧不留副本（本提交 +6/−2 段落）。
- **读改动点**
  - `resolveLocked`（`:169`）：`entry.blob != nil` 直接返回（`:171`）——旧根含 blob 的读法逐字节不变。
  - blob 缺席时 `eventBodyLocked`（`:196`）经 `segments.GetEvent`（`segment_store.go:877`）取事件体 → `storedEventText`（`:219`）取 `message.content` → 逐 ref 按 digest/length 复算（`:182-187`），漂移具名 `ErrIntegrityMismatch`。
  - `originalsOf`（`:267`）把 `blob:` 与 `ref:` 合成 entry；blob 缺席时按引用面的事实建档（`:298-301`）。
- **三锚（实存）**：`TestFreshRootStagesNoBlobCopy`（`memory/ledger/kv_original_store_test.go:959`）、`TestRecallReadsTheEventBodyWhenNoBlobIsStaged`（`:1025`）、`TestOldBlobRootReadsUnchanged`（`:1085`）。
  - 本次复跑命令与原文：`go test ./memory/ledger -run '^(TestFreshRootStagesNoBlobCopy|TestRecallReadsTheEventBodyWhenNoBlobIsStaged|TestOldBlobRootReadsUnchanged)$' -v` → 三条全 `PASS`，`ok github.com/SpellingDragon/tagent/memory/ledger 0.988s`。
- **残留**：`message/content` 字段名契约横跨 `memory.EventBody`（编码）与 `memory/ledger`（`storedEventText` 解析）两包，**无编译期守护**；由 ref 的 digest/length 复算与 recall 锚兜住。02 的段读面复用该路径时须以本节三锚复核（[archive.md](archive.md) §八 末条）。
- **对 02 的输入**：归档单位＝整帧＋事件体正文（不另带副本）；段字节预算以单一化后构成为准（`newest_seg` 8,439 B、每 64 提交段 1.14–1.21 MB，[bounds.md](../evidence/bounds.md) §6.1）。

## 八 口径裁定登记表

四条裁定在 01 域内已由叶自决或由编排者先行落定，均**未经独立审**；本轮只登记，不复审。

| # | 裁定 | 内容 | 来源 | 待独立审复核的点 |
|---|---|---|---|---|
| 1 | 参照代＝首个满窗代 `G[0]` | `G ≔ {g \| cumulative_bytes(g) ≥ W}`，`G[0]` 即首个满窗代；`D2` 的 `[1]` 明确为 `G[0]`、`[2..N]` 明确为 `G`，D2 写法是 `G = gens[0..N]` 的特例 | [bounds.md](../evidence/bounds.md) §1.1（叶自决；理由：D2 后半式与同段「窗口未满前允许增长」自相矛盾，只有固定参照代才自洽） | 该裁定与 D2 原文的偏离是否被 01 design 追认（D2 是否需改写而非仅"特例化"） |
| 2 | 段数读法＝全史封段数 | 「热段数＝成功 cut 数」按封段侧 1:1 读：成功 cut 数＝**全史封段数**，可搬集合＝段界 ≤ 倒数第二个 cut 的段，故热卷常态只余 {最新 cut 段, 开段} 两段；若按「热卷内段数」直读则恒为 2，与 cut 数无关，不自洽 | [bounds.md](../evidence/bounds.md) §四 M2（叶自决；02 签名时回填实测名） | 与 02.201 段账户的 `sealed_segments` 定义是否一致（本文件 02.201 已按此签发） |
| 3 | `index_side` 计入 `H`（不进 `B_overhead`） | `H = newest + open + index_side`；`B_overhead = 2·max_segment`（RHS 不重复计入，防判据自消）；不计入 H 则「journal 有界而目录无界」 | [bounds.md](../evidence/bounds.md) §1.2、§九❓2（叶自决，依据 [growth.md](../evidence/growth.md) §3.8 实测） | 本形状下 `index_side` 占 `H` 的 99.2%（§6.2）；换负载形状后该层是否仍具鉴别力 |
| 4 | 半声明配置＝load 期具名拒 | `archive_root` 与 `hot_log_max_bytes` 必须**成对**声明；任一单向缺失（`archive_root` 空而 `hot_log_max_bytes > 0`，或反之）皆 load 期具名拒（`ErrMemoryDeclaration` 家族，先例 `assembly/run.go:3464-3466`），MUST NOT 静默取默认；两字段皆空＝今日行为 | [archive.md](archive.md) §五 裁定 (b)（01.104 前由编排者按项目纪律先行落定） | 「成对」是否也应含 `archive_max_age`（现裁定不含）；拒法是否走 `ErrMemoryDeclaration` 而非新错误族 |

## 九 待审改写清单归属（→ F1 契约修订批）

[archive.md](archive.md) §九 的七条，全部为**非 canonical** 文档里的无条件强度句，归属 **F1 契约修订批**；本轮只登记，不改写（`openspec/changes/archive/**` 是冻结归档，只附勘误）。

| # | 位置 | 命中句（摘要） | 冲突的承诺 | 改法 |
|---|---|---|---|---|
| 1 | `docs/wiki/memory/memory-architecture.md:252` | 「`journal.log` 字节随历史增长…MUST NOT 被声称有界；开档完整性校验的 O(日志字节) 成本同属显式声明面」 | C1／C4 | 改条件句：未开归档＝如此；开归档＝有界于热窗 |
| 2 | `openspec/specs/session-checkpoint-material/spec.md:147` | 「热层字节的增长 SHALL 被显式声明，MUST NOT 被声称为本要求已达成的有界」 | C1 | 同上加「未开归档」条件 |
| 3 | `openspec/specs/storage-lifecycle-completion/spec.md:15` | 「热层字节的增长 SHALL 显式声明，MUST NOT 被声称为本要求已达成的有界」 | C1 | 同上 |
| 4 | `openspec/specs/storage-lifecycle-completion/spec.md:7`、`:21` | 「已提交事实按身份恒可读」（Purpose 行与 Scenario 名各一处） | C2 | 改「恒可定位读取」 |
| 5 | `openspec/specs/tombstone-partition-isolation/spec.md:7` | 同句（引用式注记） | C2 | 同上 |
| 6 | `openspec/specs/event-lifecycle/spec.md:7` | 同句（引用式注记） | C2 | 同上 |
| 7 | `docs/wiki/platform/platform-subsystems.md:105` | 「任何有界化手段都不删除、覆盖或改写已提交事实，恢复期读全史的消费者仍按身份读到正文」 | C2／C5 | 句子本身不假，但未声明「跨档经定位读、延迟归恢复面」，属复述改写；改为引用 §六 id |

复核注（本次只读复跑，供 F1 决定是否同时补门）：

- 七条在 HEAD `6e90d6f1` 逐条在位（已逐文件核行），但 [archive.md](archive.md) §九 的 S1 强度词扫描**只复现第 4、5、6 条**，另命中 `session-checkpoint-material/spec.md:154`（Scenario 名「已提交事实按身份恒可读」，与第 4 条同族，宜并入修订批）。
- S1 正则对第 1–3 条的形态**结构性漏检**：实际文本是「MUST NOT 被声称（为）…有界」，词表里的 `不被声称有界`／`绝对有界`／`完全有界` 都不匹配。第 7 条不含任何词表词，属人工阅读发现。故 S1 只能作发现清单（warn-only），不得据其零命中宣称"非 canonical 文档无复述"。
- `openspec/specs/resource-bounds/spec.md:51` 的命中仍是**无关项**（指进程内存驻留窗，非物理介质可读性承诺），不入清单、不得据此改句（[archive.md](archive.md) §九 同判）。

## 十 本次只读核对发现的契约勘误（附档，不改 archive.md）

| # | 位置 | 契约原文 | 实测 | 处置建议 |
|---|---|---|---|---|
| 1 | [archive.md](archive.md) §五 第 10 行 | `assembly/host_document.go:1647` `sameMemoryTier` | 函数声明在 `:1648`（`:1645-1647` 为注释）；调用点 `:1611` 与原文一致 | F1 改行号，或改引函数名不引行号 |
| 2 | [archive.md](archive.md) §三 对照表 | `ErrOriginalExpired` 在 `original_store.go:31` | 声明在 `:31`（`:30` 为注释）；`ErrOriginalMissing`:27 一致 | F1 改行号 |
| 3 | [archive.md](archive.md) §九 S1 | 词表含 `恒可读` 等七项 | 对「MUST NOT 被声称（为）…有界」形态零命中（见 §九 复核注） | F1 决定是否补词表；补亦只作发现清单，不宣称为门 |

## 十一 自证命令与原文

本节的 A–E 输出是 01.104 落盘时点的自证原文，不改写。锚名实存状态随后续叶推进已变化（当时标注为拟新增的锚当前多已实存）：当前实存／别名清单以 [archive.md](archive.md) §六 与 §附 为准，本节 D 段的「全部 0」不得再被读作当前事实。

```sh
# 全部命令在仓根执行（D/E 需仓根扫描测试树；A–C 用路径变量，不 cd，避免子目录扫出假零）
C=openspec/changes/bounded-hot-log/changes/01-bounds-contract/contracts/signing.md

# A 结构：H2 十一个＝5 叶节＋6 结构节；叶节另计
grep -c '^## ' "$C"                        # 11
grep -c '^## .*叶 0' "$C"                  # 5
grep -n  '^## ' "$C"                       # 一…十一，叶节为二–六

# B 十字段逐字段齐（每个字段名应各 5 次）
for f in 候选指纹 已证/已驳 所选/否决 精确写面 接口/配置变化 真实消费者 \
         三锚全名与命令 崩溃切点注入 回滚触发 独立设计审; do
  printf '%s=%s ' "$f" "$(grep -c "^- \*\*$f\*\*" "$C")"
done
# 候选指纹=5 已证/已驳=5 所选/否决=5 精确写面=5 接口/配置变化=5 真实消费者=5
# 三锚全名与命令=5 崩溃切点注入=5 回滚触发=5 独立设计审=5

# C 计数如实：正文（本节之前）匹配 12 行，其中字段标记恰 3 行（每叶一条 α⁻）；
#   本节命令的自指行被 sed 截在本节之前，故不把自己算进去
sed -n '/^## 十一/q;p' "$C" | grep -c 'blocked'   # 6（均为约定/历史说明行，非叶级缺项）
grep -c '^  - α⁻.*blocked:' "$C"                  # 0（三处 negative anchor 已补名冻结）

# D 锚名实存核查（零命中＝确为拟新增；仓根执行）
for t in TestSegmentRotationIsDeterministic TestSealedSegmentIndexLocatesEveryKey \
         TestFailedCutLeavesNoSegmentBoundary TestArchiveCrashRecoveryIsIdempotent \
         TestCorruptManifestRefusesArchiveRead TestHotIndexShrinksAfterArchive \
         TestSlotWatermarkRestatesOnShrunkHotFiles TestIdentityReadCrossesHotAndArchive \
         TestArchiveMissingIsNamed TestArchiveCorruptIsNamed TestRecallGetCrossesArchive \
         TestRecallTraceReportsArchiveMissing TestExportReadsArchivedFactsByIdentity \
         TestMemoryDeclarationRefusesHalfAnArchiveConfig; do
  printf '%s:%s ' "$t" "$(grep -rn "func $t" --include='*_test.go' . 2>/dev/null | wc -l | tr -d ' ')"
done
# 全部 0（14 个拟新增名，含配置面 1 个）；实存三锚另在第七节复核

# E 实存锚逐字符（01.105 三锚与 C4/C5/C6 的既有守护）
grep -rn "func TestFreshRootStagesNoBlobCopy\|func TestRecallReadsTheEventBodyWhenNoBlobIsStaged\|func TestOldBlobRootReadsUnchanged\|func TestOpenIntegrityScanIsTheWholeLogAndGrowsWithIt\|func TestACorruptStoreSeatRederivesFromTheChain" --include='*_test.go' .
# memory/ledger/kv_original_store_test.go:959 / :1025 / :1085
# memory/ledger/journal_test.go:3193
# memory/ledger/segment_store_test.go:1151
```

本次落盘时 A–E 五段的实际输出与注释所记一致。三叶的原 blocked 项已由编排者补名解除：读法约定 2 行＋总表说明 1 行＋三叶解锁状态 3 行＝6 处「blocked」字样均为约定/历史说明，叶级缺项为 0。
