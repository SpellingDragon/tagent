# hot-log-bounds 规格（delta）

> 01 域验收契约。承诺 id 与条件句的唯一家在 [contracts/archive.md](../../contracts/archive.md) §六；本文件只引 `[C1]`…`[C7]` 与既有量名，不复述承诺句。判据修订 SHALL 先改本文件与 01.102 证据，02/03 域的规格只引用不复述。

## ADDED Requirements

### Requirement: 增长构成先实测，正文单份

任何有界机制 SHALL 以实测的增长构成为依据：按 journal 帧内键族记录字节占比（事件、window 元、正文族、引用族、`checkpoint:` 切片、`quiet:` 标记、派生索引、节奏记录），并给出每个量的可复跑测量定义。判据中的每个量 MUST NOT 用单点读数或未归因残差充当界。

一条事实的正文 SHALL 只有一份权威副本：事实提交 MUST NOT 在事件体之外再造一份正文副本；新根提交后，journal 内 MUST NOT 出现与事件体重复的正文键。读面在引用旁没有正文副本时 SHALL 经事实身份读取事件体，并按引用的摘要与长度对落盘字节复算，复算不符 SHALL 具名拒绝（`ErrIntegrityMismatch`），MUST NOT 返回空、截短或「不存在」。旧构建写出的根（引用旁带正文副本）SHALL 保持只读兼容，读面逐字节不变，MUST NOT 为其补造第二份副本。归档单位 SHALL 是整帧含事件体，MUST NOT 另带正文副本。

#### Scenario: 正文双份先单一化

- **WHEN** 实测发现同一正文存于事件体与正文键两处
- **THEN** 计划 SHALL 先立单一化项，再定归档单位
- **AND** 归档设计 MUST NOT 把两份带进去

#### Scenario: 新根只留单份正文

- **WHEN** 新根提交一条带正文的事实
- **THEN** journal 内 SHALL 无正文键副本，且按身份读与召回读回字节与提交正文全等（`TestFreshRootStagesNoBlobCopy`、`TestRecallReadsTheEventBodyWhenNoBlobIsStaged`）
- **AND** 同身份异字节重试 SHALL 具名拒绝，MUST NOT 覆盖既有引用

#### Scenario: 旧根只读兼容

- **WHEN** 旧构建写出的根在引用旁带正文副本
- **THEN** 读面 SHALL 命中并逐字节全等（`TestOldBlobRootReadsUnchanged`）
- **AND** 读面 MUST NOT 清理或改写旧副本

### Requirement: 热字节平台判据与超界观测 [C1]

有界验收 SHALL 以跨代平台判据为准：满窗代至少三代且总量超过保留窗，热字节落在同一平台带内，分母取窗口内提交量。热字节 SHALL 按构成记录：恒热集合（最新 cut 段、开段、索引侧）加配置保留的旧段。单代数字或单点读数 MUST NOT 宣称平台，MUST NOT 以调低 cut 频率凑平台，MUST NOT 以归档卷增长替代热卷有界。

生效上限 SHALL 取配置上限与恒热结构下限的较大者：配置上限＝保留窗 ＋ 两个 cut 间隔的写入量（以当前热段最大日志区间计）；结构下限是不可被一次 cut 回收的热字节（最新 cut 段及其旁置索引、开段所在日志、派生索引侧 `slots`／`order.log`／`order.line`）。`hot_over_budget` SHALL 是当前未收敛区间状态，取值零或一；MUST NOT 被读作历史累计次数、区间总时长或最长超界时长。只要热字节高于生效上限，或配置上限低于结构下限，该状态 SHALL 成立；只有一次针对热窗的观察同时排除这两种形态时，它才 SHALL 归零。一次成功 cut 本身不构成归零依据：cut 之后观察到的窗口仍高于生效上限时，状态 SHALL 保持。配置上限低于结构下限的部署 MUST NOT 宣称 [C1]，其超界只计入观测。

#### Scenario: 平台未达

- **WHEN** 满窗后某代热字节越过平台带
- **THEN** 判据 SHALL 失败
- **AND** 报告 SHALL 指出增长来自窗口外滞留还是窗口参数

#### Scenario: 配置低于结构下限

- **WHEN** `configured = hot_log_max_bytes + 2×当前热段最大日志区间` 小于某代恒热结构下限 `H`
- **THEN** 最新 cut 段与开段 SHALL 仍不被搬迁，`hot_over_budget` SHALL 为 1
- **AND** 该部署 MUST NOT 宣称热盘有界，且该配置缺口本身 MUST NOT 产生提前 cut 请求

#### Scenario: 超界状态是当前区间而非累计

- **WHEN** 多代写入后每代收尾观察热窗
- **THEN** 每代在窗口低于生效上限且配置不低于结构下限时 `hot_over_budget` SHALL 为零；此前各代的超界 SHALL NOT 被累加进后续代
- **AND** 该读数 MUST NOT 被写成历史区间计数或最长超界时长（`TestHotLogPlateausAcrossGenerationsAndOldFactsStayReadable`、`TestQuietPointUnreachableToleratesOverBudget`）

#### Scenario: 无归档配置保持现状

- **WHEN** 部署未声明 `archive_root`
- **THEN** 行为 SHALL 与现状一致：一个未封段，MUST NOT 自动迁移或删除任何字节
- **AND** 同一平台判据 SHALL 以具名理由拒绝该根，MUST NOT 判绿

### Requirement: 配置门、结构下限与回退契约 [C6]

开启归档 SHALL 只由受校验的声明字段控制：`archive_root`、`hot_log_max_bytes`、可选 `archive_max_age`。严格解码 SHALL 拒绝未知键与非法值；半声明 SHALL 按名拒绝；三字段 SHALL 属重启档，改值须重启，热更 SHALL 按名拒绝。在受校验的部署声明面，未声明 `archive_root` 即未声明保留窗（半声明按名拒绝），此时行为 SHALL 与现状一致：只有一个未封段，MUST NOT 轮转、封段、搬迁或删除任何字节，根下 MUST NOT 新增归档制品。

回退契约 SHALL 区分两种根形态。从未归档的根：回退构建 SHALL 零新增，行为与基线一致。已归档的根：回退 SHALL 保留归档声明，由继续携带该声明的构建服务归档键；移除声明后归档键按身份读不可得，该形态 MUST NOT 被支持为静默回退，回退 SHALL 走离线折回或前向修复，并如实记作运维前置。`configured`（定义见 [C1]）低于恒热结构下限 `H` 时 SHALL 以结构下限为生效上限，该部署 MUST NOT 宣称 [C1]。

#### Scenario: 未声明归档

- **WHEN** 部署没有 `archive_root`
- **THEN** 轮转、封段与搬迁 SHALL NOT 发生
- **AND** 根下 SHALL 零新增归档制品，字节行为与今日一致（`TestOpenIntegrityScanIsTheWholeLogAndGrowsWithIt`）

#### Scenario: 已归档根移除声明

- **WHEN** 一个已归档的根以不含归档声明的配置或回退构建启动
- **THEN** 归档键 SHALL 读不到，该形态 SHALL 判为不支持的回退（`TestArchivedRootWithoutDeclarationDoesNotServeArchivedKeys`）
- **AND** 受支持的回退 SHALL 是「保留归档声明、只回退代码」或「离线折回／前向修复」，MUST NOT 记作零迁移

#### Scenario: 半声明与位置约束

- **WHEN** 三字段只声明一部分，或声明落在 `agents.<id>.memory` 之外
- **THEN** 装配 SHALL 按名拒绝，MUST NOT 静默补齐或落到顶层存储（`TestMemoryDeclarationRefusesHalfAnArchiveConfig`、`TestArchiveConfigOnlyUnderAgentMemory`）
- **AND** 改归档字段 SHALL 按重启档拒绝热更（`TestMovedArchiveFieldIsNotTheSameTier`）

### Requirement: 成本读数分离与开档口径 [C4]

验收 SHALL 分列四类读数，MUST NOT 以任一类替代另一类：热卷字节（平台判据）、归档卷字节（随历史增长，显式声明）、跨档定位读延迟、开档恢复成本（完整性校验字节与索引重建量）。MUST NOT 把归档增长说成热卷达标；MUST NOT 以单代或单点读数宣布平台收敛。

开档 SHALL NOT 全量预扫归档卷：稳态归档段 SHALL 读时校验；崩溃残留仍有已陈述热副本或待收缩 journal 前缀时，删除最后良好副本前 SHALL 对对应本代段／索引补验摘要。开档完整性校验与索引重建 SHALL 有界于热窗；MUST NOT 以段头自述摘要替代实算，也 MUST NOT 扫描既有归档代。

#### Scenario: 验收口径分离

- **WHEN** 汇报有界达成
- **THEN** 热卷平台读数与归档增长读数 SHALL 同时在场
- **AND** 跨档读延迟与开档恢复成本 SHALL 单列

#### Scenario: 开档不预扫归档

- **WHEN** 归档卷字节随代增长而开档
- **THEN** 开档校验字节 SHALL 有界于热窗，MUST NOT 随归档卷增长
- **AND** 稳态归档段在校验失败时 SHALL 在读时具名拒绝；崩溃残留删除最后热副本前 SHALL 只对对应本代段／索引补验摘要，MUST NOT 于开档期扫既有归档代
