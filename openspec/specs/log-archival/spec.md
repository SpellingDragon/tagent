# log-archival Specification

> 02 域现行验收契约：段、manifest、搬迁、热索引收缩、定位读与损坏拒绝。承诺 id、条件句与量名以本文件为准；[contracts/archive.md](../../changes/archive/2026-11-25-bounded-hot-log/changes/01-bounds-contract/contracts/archive.md) 是冻结的历史验收证据，不反向定义现行行为。判据与配置语义在 01 域 `hot-log-bounds`，本文件只消费其定义。

## Purpose

热日志的轮转与归档面：段界只由安静点提交推进且索引可从段文件重建；段时间只用于 age 选段；搬迁序以 manifest 为唯一权威；恒热集合与热索引收缩同水位；活体维护在 cut 后自行收敛；超界只请求提前 cut；搬迁与收缩须有生产消费者；定位读三态互不折叠；删除墓碑与损坏拒绝各有具名判据。

## Requirements

### Requirement: 段界只由 cut 推进且索引可重建 [C3]

轮转与封段 SHALL 以 `cut`（安静点提交批）为唯一起点与唯一时机，段界 SHALL 由 cut 序号与提交序确定，同输入 MUST NOT 依赖时间或调度顺序。段界 SHALL 只在 cut 提交批的屏障返回成功之后推进；cut 失败时残帧 SHALL 留在开段，MUST NOT 产生段文件或段界。字节阈值 SHALL 只作为请求提前 cut 的触发器，MUST NOT 在非安静点封段。

热封段索引 SHALL 覆盖段内每一提交键（键到偏移与长度），SHALL 可从段文件重建；重建为读路径的派生重算、不落盘，本构建不接重建事件的通知/计数面（登记为后续观测项），无法重建 SHALL 具名拒绝（`ErrSegmentIndexUnrebuildable`）。归档卷上的段索引 SHALL 由 manifest 陈述其名、条目数与摘要；缺失 SHALL 报 `ErrArchiveMissing`，损坏或摘要不符 SHALL 报 `ErrArchiveCorrupt`，MUST NOT 在读取路径静默重建，离线重建不在本构建承诺内。索引是派生物，MUST NOT 充当第二真相。

#### Scenario: 索引损坏可重建

- **WHEN** 段索引损坏或缺失而段文件完好
- **THEN** 读取 SHALL 重建索引（不落盘），所有键按身份读逐字节相等（`TestSealedSegmentIndexLocatesEveryKey`）
- **AND** 无法重建 SHALL 具名拒绝（`TestUnrebuildableSegmentIndexIsRefusedByName`）

#### Scenario: 失败 cut 不留段界

- **WHEN** cut 在屏障返回前失败
- **THEN** 热卷 SHALL 不出现新段文件或段界，残帧留在开段并可读回（`TestFailedCutLeavesNoSegmentBoundary`）
- **AND** 后续成功 cut SHALL 从原开段封起，段头记录的 cut 序号正确

#### Scenario: 同输入同段界

- **WHEN** 同一输入序列在相同 cut 序列下重放
- **THEN** 段界 SHALL 逐段相同（`TestSegmentRotationIsDeterministic`）

### Requirement: 段时间只用于 age 选择（02.205）

帧 SHALL 携带定宽提交时间，段头 SHALL 携带段内提交时间范围；新写入格式 SHALL 是可辨识的后继版本，旧版本段 SHALL 保持可读。时间 SHALL 只用于选择可搬段，MUST NOT 进入 cut 身份、事实身份或读取校验。

未声明 age 窗 SHALL 不限制搬运。声明 age 窗时，整段提交时间范围严格早于窗边界的段才 SHALL 可搬；时间未知、范围不自洽或落在未来的段 SHALL 被保留，MUST NOT 因此拒读。age 窗 SHALL 只收窄可搬集合，MUST NOT 自行触发搬迁或切割。

#### Scenario: 帧与段头携带时间

- **WHEN** 新版本写入提交事实并封段
- **THEN** 帧 SHALL 携带提交时间，段头 SHALL 携带段内最小与最大提交时间（`TestJournalV2CarriesCommitTimes`）
- **AND** 旧版本段 SHALL 仍可开档与按身份读

#### Scenario: 未知时间保守保留

- **WHEN** 声明 age 窗而某段由旧版本写出、无时间元数据
- **THEN** 该段 SHALL 被保留且可读（`TestArchiveAgeKeepsUnknownV1Segments`）
- **AND** 未声明 age 窗时同一段 SHALL 照常可搬

### Requirement: 搬迁序、manifest 权威与目标协议 [C5]

段搬迁 SHALL 固定为：先把段与索引复制到归档卷的临时名并完成屏障，再让 manifest 陈述目标名与该段摘要，再把临时名改名为目标名，最后删除热段并收缩热索引。manifest 陈述 SHALL 先于目标名落地，热段删除 MUST NOT 先于 manifest 陈述。

manifest SHALL 是「哪些段在归档、其键域与摘要」的唯一权威，双文件按代交替写：读者取可解码的较大代，同代两份内容不一致 SHALL 判损坏；两份皆无且归档卷无最终名制品 SHALL 是空归档态而非错误。归档卷中带最终名而 manifest 未陈述的文件 SHALL 判为损坏（`ErrArchiveCorrupt`，MUST NOT 删除该文件，也 MUST NOT 读作零段），未陈述的段/索引 `.tmp` 临时件 SHALL 可由恢复丢弃，manifest 槽的 `.tmp` 不构成陈述、不参与空归档判定。重复搬迁 SHALL 幂等，任一切点中断后重开 SHALL 收敛到同一归档态。

#### Scenario: 四切点崩溃恢复

- **WHEN** 复制后、屏障后、manifest 陈述后、删热中各注入一次停止
- **THEN** 重开 SHALL 收敛到同一归档态，每条已提交事实恰好可读一次（`TestArchiveCrashRecoveryIsIdempotent`）
- **AND** 重复搬迁 SHALL 幂等

#### Scenario: 目标名先于字节名

- **WHEN** 搬迁在 manifest 陈述之后、临时名改名前停止
- **THEN** 重开 SHALL 依陈述把临时件改名为目标名并完成删热，MUST NOT 判为损坏
- **AND** 归档卷内出现带最终名却无陈述的文件时 SHALL 判损坏，MUST NOT 当作空归档

#### Scenario: manifest 损坏不当空归档

- **WHEN** manifest 存在但不可解码或两份同代异文
- **THEN** 读取 SHALL 具名拒绝（`TestCorruptManifestRefusesArchiveRead`）
- **AND** MUST NOT 退化为零段空归档

### Requirement: 恒热集合与热索引收缩 [C3]

可搬集合 SHALL 只包含段界不晚于倒数第二个 cut 的段；最新 cut 所在段与滚动开段 MUST NOT 被搬迁，判定 SHALL 只用元数据，MUST NOT 扫描段内容找锚。热段删除 SHALL 只发生在 manifest 已陈述该段之后。

归档后热卷派生索引 SHALL 收缩到热段键数加段级定位条目，指向归档键的逐键条目 MUST NOT 保留；槽表与 order 的物理重放水位 SHALL 按收缩后的热文件重述。收缩 SHALL 与 manifest 同水位：崩溃后 SHALL 以 manifest 为准重算，MUST NOT 用热索引反向修正 manifest。

#### Scenario: 收缩水位以 manifest 为准

- **WHEN** 崩溃发生在删热段与收缩索引之间
- **THEN** 重开 SHALL 以 manifest 为准完成删除与收缩（`TestSlotWatermarkRestatesOnShrunkHotFiles`）
- **AND** MUST NOT 用热索引反向修正 manifest

#### Scenario: 热条目有界且不删未陈述段

- **WHEN** 大量历史键已归档
- **THEN** 热索引条目数 SHALL 有界于热段键数加段级定位条目（`TestHotIndexShrinksAfterArchive`）
- **AND** 未被 manifest 陈述的热段 MUST NOT 被删除（`TestGCNeverDeletesASegmentTheManifestDoesNotState`）

### Requirement: 活体维护在 cut 后自行收敛（02.206）

真实 cut 封段之后，若热窗仍高于生效上限（提前 cut 请求成立），同一提交 SHALL 在持锁状态下迁出可搬段、收缩热索引、关闭旧热文件并重开，使后续追加落到新热文件；MUST NOT 在 cut 提交路径之外另立搬迁入口，MUST NOT 在提交内取装配锁或重入提交。

开档 SHALL 依 manifest 的最新 watermark 代增量收敛未完成的结构工作。稳态开档只校验该代段字节数与存在性，MUST NOT 扫描归档正文、读取归档段索引内容或复算摘要；崩溃残留仍有已陈述热副本或待收缩 journal 前缀时，删除最后良好副本前 SHALL 对对应本代段／索引复算摘要，同尺寸损坏 SHALL 报 `ErrArchiveCorrupt` 并保留热字节，且 MUST NOT 扫描既有归档代。全量摘要校验 SHALL 只属搬迁恢复路径与读取该段/索引的时机。

维护失败 SHALL 分类上报：屏障已过、事实已落而维护未完成者 SHALL 报提交不确定（`ErrCommitUncertain`），已落值 SHALL 保持发布，同一视图 SHALL 可继续使用；不可恢复者 SHALL 具名拒后续提交与读取（`ErrJournalMaintenanceFailed`），直到关闭并重开。

#### Scenario: cut 提交维护活体热卷

- **WHEN** 一次由超界请求触发的真实 cut 提交后存在可搬段
- **THEN** 同一提交 SHALL 完成搬迁与收缩并重开热文件，后续追加 SHALL 落在新文件（`TestCutCommitMaintainsLiveJournal`）
- **AND** 同一视图继续提交与按身份读 SHALL 正常

#### Scenario: 开档增量收敛不预扫

- **WHEN** manifest 已陈述段而热副本或待收缩前缀仍在
- **THEN** 开档 SHALL 先对对应归档段／索引复算摘要，再删除热副本或收缩；同尺寸损坏 SHALL 保留唯一良好热副本（`TestBootArchiveConvergenceRefusesToDeleteHotCopyBehindCorruptArchive`、`TestBootArchiveConvergenceRefusesToShrinkLogBehindCorruptArchive`）
- **AND** 稳态无热副本时 SHALL 只按结构与字节数收敛，MUST NOT 在开档期复算归档段摘要（`TestBootConvergesArchiveWithoutPrescan`）

#### Scenario: 维护失败后可续或拒后续

- **WHEN** 屏障后维护失败
- **THEN** 可恢复失败 SHALL 报 `ErrCommitUncertain`，已落值不撤回，同一视图可续（`TestJournalMaintenanceRecoveryKeepsViewUsable`、`TestSealFailureKeepsCommittedValuePublished`）
- **AND** 不可恢复失败 SHALL 具名拒后续工作，关闭并重开 SHALL 一致（`TestJournalMaintenanceFailureRefusesFurtherWork`）

### Requirement: 超界只请求提前 cut（02.207） [C1]

热字节超过生效上限时，系统 SHALL 只锁存一次提前 cut 请求；请求 SHALL 由 cut 之后对热窗的观察清除，MUST NOT 由阈值直接封段。系统 MUST NOT 引入第二切割形态，也不得为迁就阈值而改写判据。

安静点长期不可达时，系统 SHALL 容忍超界并不封段，超界状态 SHALL 保持供调用方具名上报；装配面 SHALL 在事实落地后查询该请求，并复用既有 checkpoint 请求入口，MUST NOT 自建切点。

#### Scenario: 超界请求提前 cut

- **WHEN** 一次提交使热字节越过生效上限
- **THEN** 系统 SHALL 只置请求标志并等待真实 cut（`TestHotLogBudgetRequestsEarlierCut`）
- **AND** 该请求 MUST NOT 在非安静点产生段界

#### Scenario: 安静点不可达时等待

- **WHEN** 安静点长期不可达而热字节持续超界
- **THEN** 系统 SHALL 容忍超界、不封段，`hot_over_budget` SHALL 为 1 并具名上报（`TestQuietPointUnreachableToleratesOverBudget`）
- **AND** 安静点恢复后的 cut SHALL 落盘，其后的观察在窗口低于生效上限时清除请求，同一运行可继续提交

### Requirement: 搬迁与收缩有生产消费者（02.208）

搬迁与收缩 SHALL 由真实提交路径驱动，MUST NOT 只由测试直调内部函数；配置归档后，一次超界请求触发的真实 cut SHALL 在同一提交内写出非空 manifest 并收缩热卷。未声明保留窗的根 MUST NOT 出现段文件、manifest 或热卷收缩。

重开 SHALL 让归档键与新热键都按身份读回原文与原提交序；该闭环 SHALL 有真实入口锚，MUST NOT 以内部 helper 的调用结果代替。

#### Scenario: 真实提交路径驱动搬迁

- **WHEN** 配置归档与保留窗，经真实提交入口连续 cut
- **THEN** 第二次超界请求的 cut SHALL 自动写 manifest 并收缩 journal，MUST NOT 由测试直调搬迁（`TestCommitCutDrivesArchiveAndShrink`）
- **AND** 同视图继续提交新事件后，旧归档键与新热键 SHALL 均按身份读回

#### Scenario: 重开仍可读

- **WHEN** 结束运行后以同一配置重开该根
- **THEN** 归档键与新热键 SHALL 仍逐字节可读
- **AND** 未声明保留窗的根 SHALL 零段、零 manifest

### Requirement: 定位读三态不折叠 [C2] [C7]

按身份读 SHALL 走同一条定位读序：热索引、manifest 段域、段索引、段。失败语义 SHALL 分三态：热与归档都未陈述该键 SHALL 返回 `ErrOriginalMissing`；manifest 陈述而段或段索引缺失 SHALL 返回 `ErrArchiveMissing`；字节数、摘要、段头、索引或条目不符 SHALL 返回 `ErrArchiveCorrupt`。三态 MUST NOT 互相折叠，任一态 MUST NOT 被答成「不存在」。归档段索引缺失或损坏 SHALL 具名拒绝，MUST NOT 在读取路径静默重建；离线重建不在本构建承诺内。热命中与未配置归档的读 SHALL 保持零差异，`pointReadLog` 窄口语义不变。

#### Scenario: 三态各按其名

- **WHEN** 分别读一个从未写过的键、一个已陈述但段缺失的键、一个字节或索引不符的键
- **THEN** 三态 SHALL 分别返回未陈述、缺失与损坏（`TestIdentityReadCrossesHotAndArchive`、`TestArchiveMissingIsNamed`、`TestArchiveCorruptIsNamed`）
- **AND** MUST NOT 把缺失或损坏折成不存在

#### Scenario: 工具链引用校验跨档

- **WHEN** 恢复工具校验一条引用，其正文已在归档
- **THEN** 校验 SHALL 经同一定位读完成，MUST NOT 误报正文丢失（`TestRecoverKVVerifiesArchivedReferencesByIdentity`）

### Requirement: 删除墓碑与损坏拒绝（02.209）

段索引 SHALL 携带删除陈述，使按身份读区分「从未写」与「已删除」；热读遇删除陈述 SHALL 直接答不持有，MUST NOT 回落归档旧值；归档读按新到旧查找，遇最新删除陈述 SHALL 立即答不持有，MUST NOT 继续读更旧段。后续写入 SHALL 覆盖删除陈述。

损坏判据 SHALL 收紧：帧长超过上限 SHALL 归日志损坏并拒开，MUST NOT 当作可忽略的撕裂尾截断；归档索引解码后的键集摘要 SHALL 以排序键的规范 JSON 字节实算并与 manifest 比对，MUST NOT 以分隔符拼接拼出键集，不符 SHALL 归归档损坏；段头陈述 SHALL 与 manifest 交叉一致——提交序范围（`offset_map.first_seq`/`last_seq`）、cut 身份（`cut.partition`/`cut.seq`）与键域 partition 任一不符 SHALL 归损坏；`cut.frame_offset` SHALL 精确指向本段 cut 提交帧，同 cut 多帧偏移不一致或缺失 SHALL 归损坏。交叉校验 SHALL 只读段头与索引，MUST NOT 读取段正文。

#### Scenario: 删除不复活

- **WHEN** 一个键先删除、其旧值所在段随后归档
- **THEN** 按身份读该键 SHALL 答不持有（`TestDeletedKeyDoesNotResurrectFromArchive`）
- **AND** 后续重新写入 SHALL 覆盖删除陈述并可读

#### Scenario: 超限帧与键集摘要

- **WHEN** 帧长超过上限，或归档索引键集与 manifest 摘要不符
- **THEN** 前者 SHALL 拒开并报日志损坏（`TestOversizedFrameRefusesJournalOpen`），后者 SHALL 报归档损坏（`TestArchiveIndexKeyDigestMismatchIsCorrupt`）；拼接会拼成同一文本的键集 SHALL 各具摘要（`TestArchiveKeyDomainDigestSeparatesKeys`）
- **AND** MUST NOT 截断或跳过坏字节后继续服务

#### Scenario: cut 偏移与段头陈述精确

- **WHEN** 段内存在多个 checkpoint 帧而只有一个属本段 cut，或 manifest 的提交序范围、cut 身份、键域 partition 与段头不符
- **THEN** manifest 的 cut 偏移 SHALL 指向本 cut 的帧（`TestArchiveCutFrameOffsetNamesItsCut`）
- **AND** 多帧偏移冲突或缺失、交叉陈述不符或索引无该帧 SHALL 判损坏（`TestArchiveManifestContradictsItsSegmentIsCorrupt`），MUST NOT 读段正文
