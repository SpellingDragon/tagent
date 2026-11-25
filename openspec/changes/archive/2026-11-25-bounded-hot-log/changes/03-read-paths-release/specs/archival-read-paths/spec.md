# archival-read-paths 规格（delta）

> 03 域验收契约：消费面共用同一条身份定位读、缓存纪律、导出读面单契约与验收锚。三态错误、段与 manifest 语义在 02 域 `log-archival`，判据与配置语义在 01 域 `hot-log-bounds`；本文件只消费，不复述。

## ADDED Requirements

### Requirement: 消费面共用一条身份定位读 [C2]

`recall_get`、`recall_trace`、历史查询、训练导出与全史回读 SHALL 共用同一条身份定位读（热索引、manifest、段索引、段）；MUST NOT 为跨档读另立第二读路径，MUST NOT 把归档正文复制进热卷形成第二真相。归档缺失与损坏 SHALL 沿调用链具名上返，MUST NOT 被折成本地「缺失」「关系错误」或空集。

trace 的父链在热 order 未中时 SHALL 经统一身份读取事件正文以定位父关系，MUST NOT 只走热索引而宣布父缺失。

#### Scenario: 召回老键

- **WHEN** 目标事实已归档
- **THEN** `recall_get` SHALL 经定位读返回原字节与原提交序（`TestRecallGetCrossesArchive`）
- **AND** 归档缺失或损坏 SHALL 具名上返（`TestRecallTraceReportsArchiveMissing`）

#### Scenario: 父链跨档

- **WHEN** 一条追踪父关系所在事实已归档
- **THEN** 读取 SHALL 经统一身份读定位父关系，MUST NOT 报「父缺失」
- **AND** 归档损坏 SHALL 以损坏具名上返，MUST NOT 折成关系错误

### Requirement: 归档来源不进暖缓存

驻留缓存 SHALL 只缓存热来源的事实；段读缓存 SHALL 对归档来源绕过命中与写入，MUST NOT 服务旧副本。归档定位读的摘要验证缓存 SHALL 绑定段文件的身份签名（大小与修改时间），签名变化 SHALL 强制重算；未变化时 SHALL 可复用，缓存丢失 SHALL 只降速不影响答案。

前缀读与全史枚举 SHALL 合并归档索引，使读取结果与是否跨档无关；缓存不得成为第二真相。

#### Scenario: 归档字节被改后不服务旧副本

- **WHEN** 归档段字节在两次读之间被改动而签名变化
- **THEN** 读取 SHALL 重算摘要并具名拒绝（`TestRecallRefusesArchiveBytesThatChanged`）
- **AND** 热来源缓存 SHALL 不受影响

#### Scenario: 前缀读跨档一致

- **WHEN** 一次前缀读的区间跨越热段与归档段
- **THEN** 结果 SHALL 与全史枚举的顺序与字节一致
- **AND** 归档来源 MUST NOT 进入段读缓存

### Requirement: 导出读面单契约携带上下文与错误

训练导出的读面 SHALL 由单一 reader 契约承担：按会话读取已提交事实，签名 SHALL 携带上下文参数与错误返回。装配面 SHALL 提供持久化适配器：按会话枚举事实身份，正文只经统一身份读取出，错误 SHALL 原样上返。

全部消费者 SHALL 同批迁移到该契约，MUST NOT 并存无错误的旧签名或吞错适配器；取消与超时 SHALL 原样返回给调用方，MUST NOT 转写为「无事实」。该接口属未稳定公共面，破坏 SHALL 同批更新本仓消费者、示例与升级说明。

#### Scenario: 归档事实导出

- **WHEN** 导出一个含已归档事实的会话
- **THEN** 导出 SHALL 经统一身份读逐字节还原样本（`TestExportReadsArchivedFactsByIdentity`）
- **AND** 归档缺失或损坏 SHALL 原样上返，MUST NOT 产出部分样本

#### Scenario: 旧签名零残留

- **WHEN** 审计导出读面的签名与实现
- **THEN** 仓内 SHALL 只存在 `SessionFacts(ctx, addr) ([]event.Event, error)` 这一契约，消费者、示例与装配适配器全部迁移
- **AND** MUST NOT 保留吞掉错误的适配器或第二 reader 接口

### Requirement: 验收锚钉死平台与损坏语义 [C1] [C2] [C3] [C7]

有界验收 SHALL 包含：多代平台锚（满窗后热字节落带、老键逐字节可读）、损坏与缺失三态负锚、恒热段与失败 cut 的结构负锚、checkpoint 切片引用归档正文的恢复读锚、以及从未归档的根在无归档配置下的零新增探针。正常负载锚与安静点不可达锚 SHALL 成对保留，任一侧措辞越过其条件即与另一侧矛盾。

回退证据 SHALL 区分两件事：移除实现使平台锚复红（锚本身的红绿绑定，须两次入档）；以及部署回退契约（保留归档声明继续服务，移除声明不属受支持回退，见 `hot-log-bounds`）。

#### Scenario: 回滚即红

- **WHEN** 完整移除轮转与归档实现后复跑平台锚
- **THEN** 平台锚 SHALL 红，修复后 SHALL 绿，两次证据同档
- **AND** 该红绿证据 MUST NOT 被读作「移除归档声明即可回退」

#### Scenario: 损坏与缺失具名拒

- **WHEN** 归档卷有最终名制品而无 manifest、manifest 不可读、段或段索引缺失、或字节损坏
- **THEN** 引擎面 SHALL 具名拒绝（`TestArchiveMissingIsNamed`、`TestArchiveCorruptIsNamed`；`TestMissingArchiveIsNamedNotAbsent`／`TestCorruptArchiveIsNamedNotAbsent` 为薄封装别名，不重复计覆盖）
- **AND** 工具入口 SHALL 以具名理由上返（`TestRecallCorruptionNotMissing`、`TestRecallGetPublicCorruption`），MUST NOT 返回空集或不存在；无最终名制品且无 manifest 的根 SHALL 是合法空归档态，不属本场景

#### Scenario: 结构与恢复引用锚

- **WHEN** 搬迁请求覆盖最新 cut 段，或 checkpoint 切片引用的正文已归档
- **THEN** 前者 SHALL 不发生（`TestHottestSegmentNeverArchived`），后者 SHALL 经定位读逐字节读回（`TestCheckpointSliceReadsArchivedBodyByIdentity`）
- **AND** 四类成本读数 SHALL 按 `hot-log-bounds` 的口径同时入档
