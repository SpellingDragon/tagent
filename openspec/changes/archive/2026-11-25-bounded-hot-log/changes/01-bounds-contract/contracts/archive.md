# 契约冻结 · 归档（archive）

叶 01.103。候选 `6e90d6f1`（01.103 开工重核一致）。写面：本文件新建；产品代码只读。

依据：[design.md](../design.md) D3（归档契约/开档校验口径/完整性四层）、顶层 [design.md](../../../design.md) D3（切割原语/水位/搬迁协议/定位读/配置界）与 D5 P1–P8、[02 design.md](../../02-rotation-archive/design.md)（段/搬迁/GC/定位读既有决策）、[03 design.md](../../03-read-paths-release/design.md)（S1/S2 锚面）、[evidence/growth.md](../evidence/growth.md)（构成与开档基线）。

读法：本文件是**性质句的唯一家**。§六每条承诺有 id；wiki／README／CHANGELOG／升级说明只引 id 或逐字句，**不得复述改写**。02/03 各叶的锚名与命令从本文件取用，不得另立第二份定义。

## 一、manifest：归档段的唯一权威（双文件 generation swap）

`archive_root` 下的 `manifest.a` / `manifest.b` 双文件交替写：写新代 → fsync → 另一文件写入同代 → 读者取 `generation` 较大且自洽的一份。两份都不自洽＝损坏。manifest 是「哪些段在归档、其键域与摘要」的**唯一**陈述：热索引、段头、任何内存表 MUST NOT 构成第二份。

搬迁 SHALL 按固定序执行：把段与其索引复制为最终名加 `.tmp` 的暂存名并完成屏障 → manifest 双槽陈述最终名与摘要 → 把暂存名改名为最终名 → 删除热副本并收缩热索引。最终名 SHALL 只在陈述之后出现；因此「归档卷有最终名制品而无 manifest」是陈述已丢失的根，MUST NOT 当作空归档；归档段目录内无陈述的段/索引 `.tmp` 暂存件 SHALL 可被恢复清除，manifest 槽的 `.tmp` 不构成陈述、不参与空归档判定。

| 字段 | 含义 | 判据 |
|---|---|---|
| `generation` | 单调递增的提交代 | 读者取较大代；两代相同取内容一致者，不一致即损坏 |
| `segments[].segment_id` | 段身份（不与热段重名） | 与段文件同名可定位 |
| `segments[].key_domain` | 键域摘要：按分区的键保守区间 `{partition, first_key, last_key}` ＋该段键集排序后的 `sha256` | 域判定用区间（可假阳、不可假阴）；假阳由段索引命中否决 |
| `segments[].bytes` | 段文件字节数 | 与实际 `stat` 不等即损坏 |
| `segments[].sha256` | 搬迁权威摘要（跨卷内容确认） | 搬迁时实算写入，读取该段时实算比对；MUST NOT 取自段头自述 |
| `segments[].cut` | 该段界对应的 cut：`{partition, seq, frame_offset}` | 段界≡cut（顶层 D3）；`frame_offset` 是该 cut 提交批在段内的字节起点 |
| `segments[].offset_map` | 提交序→段内偏移的定位面引用：`{first_seq, last_seq, frame_count, index_path, index_sha256}` | 表体在归档卷旁置（§二），manifest 只陈述其位置与摘要 |
| `segments[].watermark` | 搬迁水位 `archived_through_cut`（已搬到哪个 cut）＋热文件重述基线 `{hot_seq, hot_values_size, hot_order_size}` | 崩溃后**以 manifest 为准**重算热索引，MUST NOT 反向用热索引修 manifest（02 D3） |
| `archived_through_cut` | 全局搬迁水位（＝上条聚合） | 可搬集合＝段界 ≤ 倒数第二个 cut（顶层 D3） |

失败语义（fail-closed；空归档态与读三态见 §三）：

| 情形 | 结果 | 依据 |
|---|---|---|
| 无 manifest 文件 ∧ 无最终名制品（无段文件、无索引文件） | 空归档态：零段（新根常态，非错误）；无陈述的段/索引 `.tmp` 暂存件可由恢复清除 | 搬迁尚未发生 |
| 有最终名制品（段或索引）而 manifest 文件不存在 | `ErrArchiveCorrupt`：最终名只在陈述之后出现，陈述已丢失；该制品 MUST NOT 被删除，也 MUST NOT 被读作零段 | 搬迁协议；02 D2 |
| `archive_root` 已声明而 manifest 文件存在但两份都不可读／不自洽 | `ErrArchiveCorrupt` | 01 D3「损坏→`ErrArchiveCorrupt`」、02 D2「损坏→`ErrArchiveCorrupt`，绝不当作段不存在」 |
| manifest 自洽而归档卷存在未被其陈述的最终名制品 | `ErrArchiveCorrupt`；该制品 MUST NOT 被删除，也 MUST NOT 被读作零段 | 最终名只在陈述之后出现（§一 搬迁协议）；02 D2 |
| manifest 自洽而某段文件缺失 | `ErrArchiveMissing` | 01 D3「manifest 陈述而段缺→`ErrArchiveMissing`」 |
| 段字节数与 `bytes` 不等、或实算 sha256 不等 | `ErrArchiveCorrupt` | 同上 |
| 键不被热索引陈述、也不被任何 manifest 段条目的键域陈述 | `ErrOriginalMissing` | §三 |

归属**不可互换**：无 manifest 且无最终名制品＝空归档态（零段，非错误，仅 `.tmp` 可清）；有最终名制品而无 manifest＝`ErrArchiveCorrupt`（陈述丢失）；manifest 存在但不可读、或归档卷有未被陈述的最终名制品＝`ErrArchiveCorrupt`（陈述本身损坏／制品先于陈述出现）；段缺失＝`ErrArchiveMissing`（权威陈述了而字节不在）。02 实施若认为应改归属，回 01.104 签发区裁决，MUST NOT 在实现里静默改写。

## 二、段头与段索引：段自描述，热封段索引可重建，归档段索引具名拒绝

| 部件 | 内容 | 判据 |
|---|---|---|
| 段头（段文件首记录） | `segment_id`、首末提交序 `{first_seq, last_seq}`、对应 cut `{partition, seq}`、段字节数、段头自身校验 | 段头可读是段的**自描述**；校验用于「段头是否完好」，MUST NOT 充当 boot 免读依据（顶层 D3 否决「以段头自述摘要代替实算」） |
| 段索引（旁置，归档卷） | 键 → `(offset, length, checksum)`，逐条含记录级校验；manifest 陈述其名、帧数、索引摘要与键集摘要 | 执行键定位；本体在归档卷，热卷不为其付费；内容 SHALL 在读取该段时校验（§四） |
| 归档段索引缺失/损坏（读取时） | 缺失 → `ErrArchiveMissing`；不可读、摘要不符或键集不符 → `ErrArchiveCorrupt` | 本构建 SHALL 具名拒绝，MUST NOT 在读取路径静默重建，MUST NOT 答「键不存在」；离线重建不在本构建承诺内（属后续变更或运维工具） |
| 热封段索引重建 | 热封段索引缺失/损坏而段完好 → 逐帧走段重算（读路径，不落盘） | 重建事件本构建不接通知/计数面（登记为后续观测项）；不可重建 SHALL 具名拒绝（`ErrSegmentIndexUnrebuildable`） |

- 索引是**派生物**，不作第二真相：段是真相字节。热封段索引坏了可在读取路径从段重算；归档段索引因 manifest 陈述其摘要而不能与段互相顶替，缺失或损坏一律具名拒绝，离线重建不属本构建承诺（类比 `order.log` 行校验坏→从日志重述，`memory/ledger/journal.go:86-97`）。
- 索引与 manifest 冲突时不「任一方静默改写另一方」：段集合与 sha256 的裁决面是 manifest，字节内容是段，冲突即 `ErrArchiveCorrupt`。
- 段头/段索引 MUST NOT 在热卷留副本（否则归档键仍让热卷付费，C1 破）。

## 三、定位读序与三态错误

`Read(key)` 的固定序（02 D4，03.301 四类消费面共用同一条，禁第二读路径）：

1. 热索引（槽表 ＋ `order`）命中 → 直读热段；
2. 未中 → manifest 段域判定（`key_domain` 保守区间，成本 O(#segments)）；
3. 命中段 → 段索引二分（键 → offset/len/checksum）；
4. 读段内该记录并校验，逐字节返回正文与原提交序。

| 态 | 条件 | 错误/结果 |
|---|---|---|
| 未陈述 | 热索引与 manifest 都未陈述该键 | `ErrOriginalMissing` |
| 陈述而缺 | manifest／段索引陈述该键，段或记录不在 | `ErrArchiveMissing` |
| 校验失败 | 记录级 checksum／段 sha256／段头不符 | `ErrArchiveCorrupt` |

三态 MUST NOT 互相折叠；任一态 MUST NOT 被答成「不存在」。

与现有词汇的对照（**实存**，实施时不得另造同义名）：

| 现有名 | 语义 | 位置 |
|---|---|---|
| `ErrSegmentBodyMissing` | 索引点名而正文读不出 | `memory/ledger/segment_store.go:210` |
| `ErrJournalCorrupt` | 帧 CRC32C 不符 | `memory/ledger/journal.go:32` |
| `ErrOriginalMissing` / `ErrOriginalExpired` | 无引用指向该键／引用在窗口外 | `memory/ledger/original_store.go:27` / `:31` |
| `RecallMissing`/`RecallExpired`/`RecallFailed`/`RecallFound` | 原文面四态 | `memory/ledger/reference.go`；`tool/recall/get.go:223` ↔ 正文 `docs/wiki/memory/memory-architecture.md` §3.1 |

`pointReadLog` 窄口语义不变：`Get(ctx, key) ([]byte, bool, error)` 三分（持有／不持有／读失败）。归档三态落到这三格的方式**必须冻结**——`ErrOriginalMissing` → `(nil,false,nil)`；`ErrArchiveMissing`／`ErrArchiveCorrupt` → `(nil,false,err)`。MUST NOT 把 Corrupt 落到「不持有」（那会把存储损坏报成事实不存在）。三处窄口：`runtime/state/durable.go:804`、`features/evolution/governance/denial.go:646`、`runtime/task/approval/attention_log.go:148`；同一约束的既有明文见归档契约 `openspec/changes/archive/2026-11-23-runtime-coherence/changes/01-contract-evidence/contracts/store-reads.md` §三。

## 四、开档校验口径：boot 有界于热窗，归档段读时校验

| 项 | 规定 |
|---|---|
| boot SHALL 校验 | 热段逐帧 CRC32C（**实算**）＋ manifest 结构与自洽性（双槽、`generation`、段身份与前缀、最终名无未陈述制品、`bytes` 与段 `stat` 字节数一致）＋已陈述热副本／待收缩前缀的删除前校验与必要收缩 |
| boot MUST NOT | 全量预扫归档段或索引；稳态 boot 只 stat。仅当热卷仍有已陈述副本或 journal 待收缩前缀时，为删除最后良好副本，SHALL 对待删对应段／索引实算摘要；不得扫描既有归档代。以段头自述摘要替代热段实算、以 manifest 的 sha256 免读热段仍属禁止 |
| 归档段校验时机 | **读时校验**（§三三态不变）；稳态 boot 不预扫。崩溃残留删除前，对确有热字节的对应段／索引补验一次；段摘要按 `stat` 签名缓存一次实算（`archiveFileSignature`） |
| 归档段索引不可用（读取时） | 缺失 → `ErrArchiveMissing`；不可读／摘要或键集不符 → `ErrArchiveCorrupt`；MUST NOT 静默重建或答「键不存在」；无热副本的稳态 boot 只 stat，有热副本／待收缩时须摘要验，离线重建不在本构建承诺 |
| 开档校验量 | SHALL 有界于热窗：实测量取 `IntegrityBytesScanned()`（`memory/ledger/journal.go:2823`，开放期实际比对过校验和的日志字节）与 `JournalBytesOpen()`（`memory/ledger/journal_kv.go:252`，水位之后索引重放的窗口）；归档开启后前者 MUST NOT 随归档字节增长 |
| 热索引重建（boot） | SHALL 有界于热窗（槽表/`order` 只覆盖热段）；归档段索引不在 boot 读取或重建 |
| 未开归档 | 从未归档的根在 `archive_root` 空时与今日零 diff；已归档的根移除声明不在零 diff 面（C6 形态二） |

「每字节至少读一次」是下界：热窗内的字节 boot 必读，不得用任何自述摘要省下（顶层 D3「boot 校验量下界＝热窗字节」）。崩溃残留的条件摘要验只针对本代待删热字节，不把既有归档代纳入开档扫描面。

完整性校验四层各防不同失效、不互为重复（01 D3，本契约冻结其归属）：

| 层 | 防什么 | 坏了怎么办 |
|---|---|---|
| ①帧 CRC32C | 真相字节完整性（半帧／静默损坏） | `ErrJournalCorrupt` 拒开 |
| ②`order.log` 行校验 | 提交索引行的可定位性 | 从日志重述（索引可弃） |
| ③`slots` 记录校验 | 快速路径正确性 | 重建 |
| ④manifest sha256 | 搬迁权威与跨卷内容确认 | `ErrArchiveMissing`/`ErrArchiveCorrupt` |

CRC 是检错不是防篡改；本契约 MUST NOT 被读作任何防篡改承诺。

## 五、配置面：三个部署字段、字段落点与拒绝清单

三个字段都是**部署可变面**，MUST 进声明面与严格解码，MUST NOT 以测试钩子式绕过（ORCHESTRATION 授权条）。热更档位是 **Boot／重启必需**：`sameMemoryTier` 对 `StorageConfig` 做 `reflect.DeepEqual`，任一字段改变即以 `ErrHostReloadRestartRequired` 拒载并点名 `agents.<id>.memory`（`assembly/host_document.go:1648`（`sameMemoryTier`）、`:1611`（调用点）、`:1633`（`memoryFieldPathOf` 的 `entry := spec.Memory`）；`assembly/host_reload.go:52`）；同一档位的口径见 `docs/wiki/platform/platform-subsystems.md#config-surface`「Boot（构造消费点，改了须重启）」。

| 字段 | 类型／读法 | 语义与边界 | 默认 |
|---|---|---|---|
| `archive_root` | string，`stringField`（`api/config/decode.go:676`） | 归档卷根；非空才可能启用归档。MUST 落在与热卷**不同的卷/配额域**，否则「目录有界」≠「总盘有界」——该条以部署文档声明承担，运行期**不**加设备探测谓词（ORCHESTRATION 禁「冷档探测谓词形态」） | 空＝不启用归档，行为＝现状 |
| `hot_log_max_bytes` | int，`countField`（`api/config/decode.go:498`） | 目标保留量；`>0` 才启用切割与归档。`configured = hot_log_max_bytes + 2×当前热段最大日志区间` 低于恒热集合（最新 cut 段＋开段＋索引侧）结构下限 `H` 时**以下限为生效上限**并使 `hot_over_budget` 为 1；该部署 MUST NOT 宣称 C1，MUST NOT 因此改变切割时点 | 0＝不启用；无内建默认值（仓库「禁硬编码 tunable」） |
| `archive_max_age` | duration，`durationField`（`api/config/decode.go:535`） | 可选时间窗：保留窗＝「字节上限为主、时间窗可选」（顶层 D2/D3）。声明后，段的提交时间范围须整体早于 `now - archive_max_age` 才可搬；它只加保留、不替代字节界，也不参与切割时点 | 未声明＝只受字节界与水位约束 |

启用条件（两个条件同时成立才切割与归档）：`archive_root` 非空 ∧ `hot_log_max_bytes > 0`。任一不成立＝不启用，字节行为与今日一致。

### 字段落点（只读核对，逐条 file:line）

| # | 面 | 位置 | 新字段在此处的义务 |
|---|---|---|---|
| 1 | 声明结构 | `api/config/config.go:172` `type MemorySpec struct`（现四键 `Type`:174／`Path`:176／`ReadNamespaces`:179／`Engine`:182） | 三个字段加到此处；`Type`/`Path` 的「同进同出」判据不适用于它们 |
| 2 | 严格解码白名单 | `api/config/decode.go:278` `sectionFields(..., "type", "path", "read_namespaces", "engine")` | 不加即被拒 `unknown field` |
| 3 | 逐键读法 | `api/config/decode.go:282-293` | 各键按上表选读法（`stringField`/`countField`/`durationField`） |
| 4 | 声明身份面 | `api/config/fingerprint.go:103` `canonicalMemory`、`:236` `MemorySpec.canonical()`、`:249` `MemorySpec.isZero()` | **不加则配置变更不进指纹**（发布侧读成 unchanged，永不生效）；`isZero` 不把新字段算入会让「只声明归档字段」的声明被读成无 memory 段 |
| 5 | 深拷贝 | `api/config/clone.go:58` | 新字段随拷贝 |
| 6 | 骨架断言 | `api/config/skeleton_assert.go:60-65` | 该断言逐字段列名，能抓改名与换类型，**抓不到新增**（keyed literal 少字段仍编译）——新增字段须手工同步 |
| 7 | 引擎面 | `memory/ledger/config.go:48` `type StorageConfig struct`（`Type`:50／`Root`:52／`RetentionWindows`:76） | 三个字段加到此处；`Root` 与 `archive_root` 是两个目录，不合并 |
| 8 | 装配桥 | `assembly/run.go:3468`（agent 路径）／`:3490`（顶层 `storage` 路径）构造 `ledger.StorageConfig{Type, Root}`；`:3473`／`:3491` 用 `reflect.DeepEqual` 判同一 tier | 新字段须随桥传递，否则声明被丢弃；`DeepEqual` 使「两 agent 声明不同保留窗」自动被拒 |
| 9 | 宿主文档 | `assembly/host_document.go:76-79` `hostStorage{Type,Root}`（顶层 `storage` 段）、`:99` `Memory config.MemorySpec`、`:1607` `MemoryPlanOf(decl, Storage.Type, Storage.Root)` | 顶层段是否承载三字段须与 §五 表一致（不得两处各一套语义） |
| 10 | 热更判定 | `assembly/host_document.go:1647` `sameMemoryTier` 的 `reflect.DeepEqual(*left,*right)` | 新字段自动进重启判定——本期即所需 |

### 解码拒绝清单（逐条，原文照抄）

| # | 输入 | 拒法（`decodeError` 渲染为 `config: <source>:<line>: <field>: <reason>`，`api/config/source.go:282-291`） | 出处 |
|---|---|---|---|
| 1 | memory 段内未知键 | `unknown field` | `api/config/decode.go:111` |
| 2 | 同上，经严格扫面 | 同文；另 `errors.Is(err, ErrUnknownConfigKey)` 成立 | `api/config/strict.go:25`、`:88-94` |
| 3 | 同键重复 | `duplicate field` | `api/config/decode.go:114` |
| 4 | memory 段不是映射 | `expected a mapping` | `api/config/decode.go:266` |
| 5 | `archive_root` 非字符串（含映射/序列/null 以外的标量） | `expected a string` | `api/config/decode.go:704`、`:708` |
| 6 | `hot_log_max_bytes` 非数字 | `expected a number` | `api/config/decode.go:779` |
| 7 | `hot_log_max_bytes` 负数 | `expected a non-negative whole number` | `api/config/decode.go:507` |
| 8 | `hot_log_max_bytes` 小数 | `expected a whole number` | `api/config/decode.go:510` |
| 9 | `archive_max_age` 非 duration | `expected a duration such as 30m: <解析原因>` | `api/config/decode.go:545` |

拒绝 MUST 发生在 load 期（装配前），MUST NOT 静默取默认值（仓库「Misconfiguration fails loud」）。上表是**解码面**；下列语义面不属于解码拒绝，其处置在 §五 表内已定：`archive_root` 空＝现状；`hot_log_max_bytes ≤ 0`＝不启用。

**裁定（01.104 前由编排者按项目纪律先行落定，待 01.104 独立审复核）＝(b) load 期具名拒**：`archive_root` 与 `hot_log_max_bytes` 必须**成对**声明——`archive_root` 空而 `hot_log_max_bytes > 0`，或 `archive_root` 非空而 `hot_log_max_bytes` 缺省/≤0，两者皆为自包含可判的误配，load 期具名拒（`ErrMemoryDeclaration` 家族；先例 `assembly/run.go:3464-3466` 拒「half a storage tier」），MUST NOT 静默取默认。两字段皆空/皆缺＝今日行为（不启用）。**第三腿**：`archive_root` 非空但其 StorageConfig 缺 `type`/`path`（`run.go:3463` 视为未声明）→ 具名拒；`archive_max_age` 单独声明（无 `archive_root`/`hot_log_max_bytes`）→ 具名拒（`archive_max_age` 只是附加 AND 条件，不单独启用归档）。**位置约束**：三字段只接受在 `agents.<id>.memory`（`MemorySpec`）；顶层 host document 的 storage 节不新增键，混用时沿用既有 DeepEqual 异 tier 拒绝。锚（实存）：`TestMemoryDeclarationRefusesHalfAnArchiveConfig`（`assembly/run_test.go:1335`，四向：两向成对＋缺第三腿＋max_age 单声明）、`TestArchiveConfigOnlyUnderAgentMemory`（`assembly/run_test.go:1414`）。

## 六、承诺表（语义承诺，唯一家在本节）

每条＝`{id, claim（canonical 句）, condition, fails_when, anchors, canonical_docs}`。锚标注：**实存**＝测试树已命中（附 `func` 位置）；**别名**＝薄封装既有锚（直接委托，同一 fixture 与断言），不重复计覆盖；**拟新增**＝尚不存在，由所指叶创建。强度词扫描只作发现清单（warn-only），不作判据；成立条件与失败形态由锚成对承担，人只判条件充分性。

### C1 热盘有界

- claim：开启归档后，热卷字节有界于保留窗内工作量，不随历史总量增长。
- condition：`archive_root` 非空 ∧ `hot_log_max_bytes > 0` ∧ **配置上限不低于恒热结构下限**（`configured ≥ H`；低于下限的部署不满足 C1，其超界只入观测） ∧ cut 正常发生（安静点可达）。三量以本节式子为准：`configured ≔ hot_log_max_bytes + 2 × 当前热段最大日志区间`；`H ≔` 最新 cut 段文件及其旁置索引 ＋ 开段所在 journal 文件 ＋ 派生索引侧（`slots`、`order.log`、`order.line`）；`effective ≔ max(configured, H)`。字节比较一律以 `effective` 为界。
- fails_when：安静点长期不可达 → 容忍超界，不改切割时点。`hot_over_budget` 是**当前未收敛区间状态**，取值 0 或 1：热字节高于 `effective`，或 `configured < H`，任一成立即为 1；一次成功 cut 本身不构成归零，只有 cut 之后针对热窗的一次观察同时排除这两种形态才归 0。该状态为 1 时 C1 不可宣称；`configured < H` 的部署无论如何 MUST NOT 宣称 C1，其超界只计入观测。告警 SHALL 具名上报该状态。
- anchors：α⁺ 实存 `TestHotLogPlateausAcrossGenerationsAndOldFactsStayReadable`（`assembly/checkpoint_test.go:7061`，多代平台＋分母）；α⁻ 实存 `TestQuietPointUnreachableToleratesOverBudget`（`assembly/checkpoint_test.go:6300`，故障注入：容忍超界、不封段、状态非零）。
- canonical_docs：本节；验收面同批见 `changes/01-bounds-contract/specs/hot-log-bounds/spec.md`（措辞不得宽于本句）。

### C2 归档可读（按身份恒可定位）

- claim：已归档事实按身份读返回与提交时逐字节相等的正文与原提交序，身份、顺序与授权不因物理搬迁改变。
- condition：`archive_root` 非空且归档卷在线；读取方持有该 fact 的 scope 授权。
- fails_when：归档介质的缺失/损坏具名 `ErrArchiveMissing`／`ErrArchiveCorrupt`，MUST NOT 答「不存在」；归档丢失是运维事故语义，不是事实不存在。
- anchors：α⁺ 实存 `TestHotLogPlateausAcrossGenerationsAndOldFactsStayReadable`（第 1 代老键第 N 代逐字节读回）、实存 `TestIdentityReadCrossesHotAndArchive`（`memory/ledger/archive_read_test.go:57`）；α⁻ 实存 `TestMissingArchiveIsNamedNotAbsent`、`TestCorruptArchiveIsNamedNotAbsent`（`memory/ledger/archive_test.go:1066`／`:1072`；**薄封装别名**，分别委托 `TestArchiveMissingIsNamed`／`TestArchiveCorruptIsNamed`，不重复计覆盖）；αᶜ `recall_get` 正式入口（`tool/recall/get.go:153` → `ReadSegmentScoped`）＋实存 `TestRecallGetCrossesArchive`（`tool/recall/get_public_test.go:294`）。实存守护：`TestRecallCorruptionNotMissing`、`TestRecallGetPublicCorruption`（`tool/recall/get_public_test.go:115`／`:136`）、`TestOldBlobRootReadsUnchanged`（`memory/ledger/kv_original_store_test.go:1085`）。
- canonical_docs：本节。

### C3 恒热（最新 cut 段与开段不被搬）

- claim：最新 cut 所在段与滚动开段恒在热卷；可搬集合＝段界 ≤ 倒数第二个 cut，判定只用元数据，不扫描段内容找锚。
- condition：cut 正常推进。配置低于结构下限时仍以下限为准（不搬恒热段）。
- fails_when：搬迁尝试覆盖恒热段即违约；把水位判据改写成「按内容找安静点/切片」即违约（初稿三特判已废除）。
- anchors：α⁺ 实存 S1 同锚机理断言（`TestHotLogPlateausAcrossGenerationsAndOldFactsStayReadable`：末代关闭后恒热两段均不在 manifest、manifest 段数＝成功 cut 数−1（热卷常态余最新段＋开段））；α⁻ 实存 `TestHottestSegmentNeverArchived`（`memory/ledger/archive_test.go:1080`，搬迁候选与请求均不得覆盖恒热段）。
- canonical_docs：本节。

### C4 开档有界（恢复成本）

- claim：归档开启后，boot 的完整性校验与索引重建有界于热窗，不随归档增长；归档段读时校验。
- condition：`archive_root` 非空。
- fails_when：预扫归档卷、以段头自述摘要替代实算、`IntegrityBytesScanned` 随归档字节增长——任一即违约。
- anchors：α⁺ 实存 S1 同锚断言（开档校验字节 `361663 ≤ 末代热字节 833841`，见 `changes/03-read-paths-release/evidence/s1.md`）＋实测量 `IntegrityBytesScanned()`（`memory/ledger/journal.go:2823`）与 `JournalBytesOpen()`（`memory/ledger/journal_kv.go:252`）；α⁻ 实存 `TestJournalOpenRefusesCorruptionBehindAStatedWatermark`（`memory/ledger/journal_test.go:3217`，水位陈述在末尾仍逐帧实算）＋实存 `TestBootConvergesArchiveWithoutPrescan`（`memory/ledger/archive_test.go:580`，稳态开档只收敛结构、不扫归档正文）＋实存 `TestBootArchiveConvergenceRefusesToDeleteHotCopyBehindCorruptArchive`／`TestBootArchiveConvergenceRefusesToShrinkLogBehindCorruptArchive`（崩溃残留删除最后热副本前验摘要，坏归档不毁唯一好副本）。
- canonical_docs：本节。

### C5 单一权威与四层校验分离

- claim：manifest 是搬迁权威，段自描述，正文单份；四层校验各防不同失效、不互为重复。
- condition：本构建写出的新事实与本构建执行的归档搬迁。旧构建写出的根中「引用旁带正文副本」属历史只读兼容面，不违正文单份。
- fails_when：manifest 不唯一、在热卷留段头/段索引副本、新写事实或归档搬迁出现第二份正文、把索引当第二真相、以层间重复校验充当安全性——任一即违约。
- anchors：α⁺ 实存 `TestSealedSegmentIndexLocatesEveryKey`（`memory/ledger/journal_segment_test.go:526`）＋实存 `TestFreshRootStagesNoBlobCopy`（`memory/ledger/kv_original_store_test.go:959`）＋实存 `TestArchiveCrashRecoveryIsIdempotent`（`memory/ledger/archive_test.go:462`）；α⁻ 实存 `TestCorruptManifestRefusesArchiveRead`＋实存 `TestArchiveManifestRefusesUnstatedNamedArtifact`（有效 manifest 下未陈述最终名制品具名拒且不删）＋实存 `TestBootArchiveConvergenceRefusesToDeleteHotCopyBehindCorruptArchive`／`TestBootArchiveConvergenceRefusesToShrinkLogBehindCorruptArchive`（同尺寸坏归档不毁唯一良好热副本）＋实存 `TestACorruptStoreSeatRederivesFromTheChain`（`memory/ledger/segment_store_test.go:1381`，索引可弃）。
- canonical_docs：本节；层归属表见 §四。

### C6 零迁移与回退

- claim：回退契约按根的两种形态分记。**从未归档的根**：未声明 `archive_root` 时无任何归档机制介入，行为与今日逐字节一致（一个未封段），根下零新增归档制品。**已归档的根**：只有保留归档声明的构建才服务归档键；移除声明即归档键按身份读不可得，该形态不是受支持的回退，须走离线折回或前向修复。
- condition：从未归档根＝`archive_root` 空且根下无归档制品；已归档根＝manifest 陈述过段。
- fails_when：从未归档根被轮转、封段、搬迁或删除任何字节，或根下新增归档制品即违约；已归档根在移除声明后被当作可用根（宣称归档键仍可读，或记作零迁移）即违约。
- anchors：α⁺ 实存 `TestOpenIntegrityScanIsTheWholeLogAndGrowsWithIt`（`memory/ledger/journal_test.go:3249`，无归档时开档量仍是 O(历史)）＋实存 `TestOldBlobRootReadsUnchanged`（`memory/ledger/kv_original_store_test.go:1085`）；α⁻ 实存 S1 无归档臂（`TestHotLogPlateausAcrossGenerationsAndOldFactsStayReadable` 的 `no_archive` 分支：以 `window-never-fills` 拒绝，零 segments/manifest）。已归档根移除声明由实存 `TestArchivedRootWithoutDeclarationDoesNotServeArchivedKeys` 钉住（热键仍可读、归档键不服务），具名形态见 `hot-log-bounds` Scenario「已归档根移除声明」。
- canonical_docs：本节。

### C7 三态不折叠

- claim：`ErrOriginalMissing`／`ErrArchiveMissing`／`ErrArchiveCorrupt` 各按其名返回，任一态不被折叠为「不存在」，并经 `pointReadLog` 三分原样上返。
- condition：按身份读任一键。
- fails_when：Corrupt→absent、Missing→absent 的任一折叠即违约。
- anchors：α⁺ 实存 `TestCorruptArchiveIsNamedNotAbsent`、`TestMissingArchiveIsNamedNotAbsent`（`memory/ledger/archive_test.go:1072`／`:1066`；**薄封装别名**，委托 `TestArchiveCorruptIsNamed`／`TestArchiveMissingIsNamed`，不重复计覆盖）＋实存 `TestArchiveMissingIsNamed`／`TestArchiveCorruptIsNamed`（`memory/ledger/archive_read_test.go:99`／`:136`）；α⁻ 实存 `TestRecallCorruptionNotMissing`、`TestRecallGetPublicCorruption`（`tool/recall/get_public_test.go:115`／`:136`，经真实工具入口区分损坏与缺失）；αᶜ `recall_get` 正式入口同 C2。
- canonical_docs：本节；消费面约束见 §三末段。

## 七、契约修订清单（本叶只登记，写者为所指负责方）

| # | 目标 | 现状原文 | 修订方向 | 写入方 |
|---|---|---|---|---|
| 1 | `openspec/specs/session-checkpoint-material/spec.md:154` | Scenario 名「已提交事实按身份恒可读」；正文「SHALL 恒返回其原字节与原索引位置，SHALL NOT 答 missing」 | 标题与句子改「按身份**恒可定位读取**（在线直读／归档契约读）」；两条实质承诺保留：原字节＋原索引序；索引点名而读不出或校验失败 SHALL 具名拒绝，MUST NOT 降级为「不存在」。理由：「恒可读」若被读成「恒驻热卷直读」即与 C1 有界矛盾 | 01.104 定稿、03 域落笔 |
| 2 | `openspec/specs/bounded-recovery/spec.md`（「完整性和只读保证不因加速降低」所在 Requirement 面） | 无热卷字节界与开档成本界的句子 | 补两条：在 C1 条件下热卷字节有界于保留窗；开档校验与索引重建有界于热窗、归档段与归档索引读时校验（C4）。措辞不得宽于 §六 | 03 域 |
| 3 | `docs/GLOSSARY.md` §一「机制名」 | 现无归档四词 | 增四词，各含「一句话定义＋领域类比＋在哪看」：**归档（archive）**＝按段搬到 `archive_root` 并从热卷删字节、内容仍按身份恒可定位读取（C2）；**定位读**＝热索引→manifest→段索引→段的同一条身份读法（§三）；**段界（segment boundary）**＝物理日志段的边界，≡一次 cut，只在 cut 提交批 barrier 返回后推进；**物理重放水位**＝`slots` 头 `Seq`/`ValuesSize`/`OrderSize`（`memory/ledger/journal.go:831-834` 声明、`:1096` 读入、`:1915` 写出），与**逻辑 cut**（framework checkpoint：`quiet:` 标记 `assembly/checkpoint.go:1002`、界判 `:1041`、尾键 `:1066`）**不是一回事**——水位属物理层、cut 属逻辑层，有界性只在物理层。**并须与既有词条「段（segment）」区分**：既有条目指消费循环的开段/闭段（`runtime/agent/consumer.go` `openSegment`/`closeSegment`），本变更的「段界」指物理日志段界，同形异指，词条 MUST 点名区分，禁止混用 | 01.104 定稿、02 域落笔 |
| 4 | `docs/upgrade-rollback-drill.md` | 现「现行架构下真正需要演练的两件事」 | 增第三条演练口径：开启归档的升级与回滚——三字段属 Boot 档（改字段须重启，`ErrHostReloadRestartRequired` 点名 `agents.<id>.memory`）；回滚分两形态：从未归档根＝移除声明即可，已归档根＝保留归档声明、只回退代码，或走离线折回／前向修复，移除声明后归档键不可得、MUST NOT 记作零迁移；演练须记录 `ErrArchiveMissing`／`ErrArchiveCorrupt`／`hot_over_budget` 三个具名符号与开档校验字节读数 | 按 ORCHESTRATION「须同批写 `docs/upgrad*` 与主 spec 修订记录」 |

第 1–3 条的措辞一经落笔即进入本契约的引用关系：落笔后 MUST 只引 §六 的 id，不得复述句子（F3 逐句审计对象）。

## 八、与 01.105 的接缝：归档单位＝帧＋事件体正文

- **归档单位＝整帧**。一条事实的六个键在同一帧内（`memory/ledger/segment_store.go:791` `CommitFact` 一批提交），按帧搬即天然成组，不存在跨帧引用（`evidence/growth.md` §五推论 1）。
- **正文单份**（01.105 裁定 A，已实施）：事实提交只写 `ref:`（`memory/ledger/kv_original_store.go:390` `planFactLocked` 的 `puts` 只有 ref 键），正文只在事件体 `message.content`；`blob:` 只由无事件可承载的裸 `Put` 写（同文件 `:334`）。因此归档段的正文就是段内事件体字节，**不另带副本**；`ref:` 账目（`{owner,format,retention_policy}→{digest,length,stored_at}`）同帧随段走。
- **旧根 blob 属历史只读面**：搬迁 MUST NOT 依赖 blob 存在，也 MUST NOT 为其造第二份。旧段整帧搬走，其「引用旁有正文」的读法在归档段上照旧成立；新根「无 blob、经事件体读」的读法同样照旧（`internal` 判定在 `memory/ledger/kv_original_store.go:169` `resolveLocked`／`:196` `eventBodyLocked`）。
- **digest 复算跨档不变**：ref 的 `{digest,length}` 由原文仓对落盘字节计算，归档读 MUST 经同一复算，漂移具名 `ErrIntegrityMismatch`（`memory/ledger/original_store.go:20`）。
- **跨包字段名契约须复核**：`message/content` 横跨 `memory.EventBody`（编码）与 `memory/ledger`（解析），无编译期守护（01 design D1 登记残留）。02 复用该路径时以 01.105 三锚复核：`TestFreshRootStagesNoBlobCopy`、`TestRecallReadsTheEventBodyWhenNoBlobIsStaged`、`TestOldBlobRootReadsUnchanged`（`memory/ledger/kv_original_store_test.go:959`／`:1025`／`:1085`）。
- **字节预算以单一化后构成为准**：单份化把每段从 ≈2.24 MB 降到 ≈1.16 MB、cut 帧从 14.4 KB 降到 ≈8.3 KB（`evidence/growth.md` §五推论 3）；`hot_log_max_bytes` 的平台与段预算依赖此构型，MUST NOT 引用单一化前的数字。
- **跨档 tail 只属损坏恢复**：常规 boot 的尾必在开段（切割时点守恒）；仅当安静点标记缺失/损坏时 `tailFactKeys` 退读全日志，可跨入归档——该成本记 `archive-tail-read`，S1 平台锚不得掩盖它（顶层 D6）。

## 九、待审改写清单与扫描命令

扫描在仓根执行；`openspec/changes/archive/**` 是冻结归档，只附勘误不改写，故一律排除。

```sh
# S1 强度词（承诺句可能被改写的地点；warn-only，发现清单不是判据）
grep -rnE "恒可读|恒可定位|恒驻|永不删除|不被声称有界|绝对有界|完全有界" --include='*.md' . \
  | grep -v '^./openspec/changes/archive/' | grep -v '^./openspec/changes/bounded-hot-log/'

# S2 归档缺失被折叠成不存在的措辞
grep -rniE "(归档|archive).{0,12}(缺失|损坏).{0,14}(不存在|视为|等同|折叠)" --include='*.md' . \
  | grep -v 'contracts/archive.md'

# S3 锚名在长期文档面被二次定义（锚名只许在本契约与 02/03 tasks 定义；docs/specs 出现即待审）
grep -rnE "TestHotLogPlateausAcrossGenerationsAndOldFactsStayReadable|TestQuietPointUnreachableToleratesOverBudget|TestHottestSegmentNeverArchived" \
  --include='*.md' docs/ openspec/specs/ README.md 2>/dev/null
```

HEAD `6e90d6f1` 实跑结果，待审项逐条（全部为**非 canonical** 文档，须改引 §六 id 或按第七条改措辞）：

| # | 位置 | 命中句（摘要） | 冲突的承诺 |
|---|---|---|---|
| 1 | `docs/wiki/memory/memory-architecture.md:252` | 「`journal.log` 字节随历史增长…MUST NOT 被声称有界；开档完整性校验的 O(日志字节) 成本同属显式声明面」 | C1／C4：该断言在开启归档后**不成立**，须改为条件句（未开归档＝如此；开归档＝有界于热窗） |
| 2 | `openspec/specs/session-checkpoint-material/spec.md:147` | 「热层字节的增长 SHALL 被显式声明，MUST NOT 被声称为本要求已达成的有界」 | C1：同上是无条件强度，须加「未开归档」条件 |
| 3 | `openspec/specs/storage-lifecycle-completion/spec.md:15` | 「热层字节的增长 SHALL 显式声明，MUST NOT 被声称为本要求已达成的有界」 | C1：同上（且与 `:7`／`:21` 的「恒可读」同族） |
| 4 | `openspec/specs/storage-lifecycle-completion/spec.md:7`、`:21` | 「已提交事实按身份恒可读」 | C2：应为「恒可定位读取」 |
| 5 | `openspec/specs/tombstone-partition-isolation/spec.md:7` | 同句（引用式注记） | C2 |
| 6 | `openspec/specs/event-lifecycle/spec.md:7` | 同句（引用式注记） | C2 |
| 7 | `docs/wiki/platform/platform-subsystems.md:105` | 「任何有界化手段都不删除、覆盖或改写已提交事实，恢复期读全史的消费者仍按身份读到正文」 | C2／C5：句子本身不假，但未声明「跨档经定位读、延迟归恢复面」，属复述改写 |

此外两条由 01.104 独立审补入：`openspec/specs/compaction-scheduler/spec.md:7`（同族强度句）、`docs/wiki/memory/memory-architecture.md:50`（「搬迁已无载体」与新增归档设计冲突）。

S2 在 HEAD 零命中（无「归档缺失⇒不存在」的现成措辞），该约束由 §三 与 C7 前置钉住。S3 在 HEAD 零命中（长期文档面未复述锚名）。S1 另有一处**命中但无关**：`openspec/specs/resource-bounds/spec.md:51` 的「MUST 恒驻事实窗口内」指进程内存驻留窗，不是物理介质的可读性承诺，不入待审清单也不得据此改句。建议把 S1 固化为 `scripts/gates/` 的 warn-only 清单（属后续变更，本变更只做一次性审计，见 ORCHESTRATION §凭据）。

## 附：与 02／03 的取用关系

02/03 各叶的锚名与命令**只从 §六 与 §三 取用**，不自建第二份定义。下表按当前测试树标注状态：**实存**（测试树已命中）、**别名**（薄封装，不重复计覆盖）或**拟新增**（尚不存在）：

| 叶 | 取用的锚（状态） |
|---|---|
| 02.201 | `TestSegmentRotationIsDeterministic`（实存 `assembly/checkpoint_test.go:6016`）、`TestSealedSegmentIndexLocatesEveryKey`（实存 `memory/ledger/journal_segment_test.go:526`）、`TestFailedCutLeavesNoSegmentBoundary`（实存 `memory/ledger/journal_segment_test.go:681`）、`TestUnrebuildableSegmentIndexIsRefusedByName`（实存 `memory/ledger/journal_segment_test.go:601`）（C3／C5） |
| 02.202 | `TestArchiveCrashRecoveryIsIdempotent`（实存 `memory/ledger/archive_test.go:462`）、`TestCorruptManifestRefusesArchiveRead`（实存 `memory/ledger/archive_test.go:1036`）（C5） |
| 02.203 | `TestHotIndexShrinksAfterArchive`（实存 `memory/ledger/hot_shrink_test.go:172`）、`TestSlotWatermarkRestatesOnShrunkHotFiles`（实存 `memory/ledger/hot_shrink_test.go:224`）、`TestGCNeverDeletesASegmentTheManifestDoesNotState`（实存 `memory/ledger/hot_shrink_test.go:294`）（C3／§一 `watermark`） |
| 02.204 | `TestIdentityReadCrossesHotAndArchive`（实存 `memory/ledger/archive_read_test.go:57`）、`TestArchiveMissingIsNamed`（实存 `:99`）、`TestArchiveCorruptIsNamed`（实存 `:136`）（C2／C7） |
| 03.301 | `TestRecallGetCrossesArchive`（实存 `tool/recall/get_public_test.go:294`）、`TestRecallTraceReportsArchiveMissing`（实存 `:315`）、`TestRecallRefusesArchiveBytesThatChanged`（实存 `:329`）、`TestExportReadsArchivedFactsByIdentity`（实存 `tests/integration/learning/chain_export_test.go:93`）（C2／C7） |
| 03.302 | `TestHotLogPlateausAcrossGenerationsAndOldFactsStayReadable`（实存 `assembly/checkpoint_test.go:7061`）、`TestQuietPointUnreachableToleratesOverBudget`（实存 `:6300`）、`TestHottestSegmentNeverArchived`（实存 `memory/ledger/archive_test.go:1080`）、`TestCheckpointSliceReadsArchivedBodyByIdentity`（实存 `memory/ledger/segment_store_test.go:241`）；`TestCorruptArchiveIsNamedNotAbsent`／`TestMissingArchiveIsNamedNotAbsent` 为**别名**（委托 02.204 的引擎面锚，不重复计覆盖）（C1／C2／C4／C7／§八） |

`TestMissingArchiveIsNamedNotAbsent`／`TestCorruptArchiveIsNamedNotAbsent` 是 `TestArchiveMissingIsNamed`／`TestArchiveCorruptIsNamed` 的**薄封装别名**（直接委托，同一 fixture 与断言），不构成第二粒度、不得重复计覆盖；消费面（工具入口）的具名证据由 `TestRecallGetCrossesArchive`／`TestRecallTraceReportsArchiveMissing`／`TestRecallRefusesArchiveBytesThatChanged` 承担。
