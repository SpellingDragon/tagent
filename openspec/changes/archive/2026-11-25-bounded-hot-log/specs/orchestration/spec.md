# orchestration 规格（delta：本变更的一级编排契约）

> 本文件只承载跨域编排约束：规格归属、叶级派发与解锁、口径冲突处置、归档前收尾。产品验收契约落在各域 capability 规格（`hot-log-bounds`、`log-archival`、`archival-read-paths`、`ci-throughput`），本文件 MUST NOT 复述其中的字节判据、读取语义、配置语义或 CI 判据。

## ADDED Requirements

### Requirement: 一级规格只承载跨域编排约束

一级 change 的规格 SHALL 只陈述跨域编排约束：规格归属与转正、叶级派发与解锁、证据归属、口径冲突处置、归档前收尾。产品验收契约 SHALL 陈述在域 capability 规格；一级规格 MUST NOT 复述这些判据，也 MUST NOT 以摘要形态携带第二份判据文本。

#### Scenario: 一级规格出现产品判据

- **WHEN** 一级规格的 Requirements 出现热卷字节界、定位读三态或吞吐阈值等产品判据
- **THEN** 该条 SHALL 判为越界，须下沉到对应域 capability 规格，原处只留引用
- **AND** 一级与域规格冲突时 SHALL 以域规格为准，并登记为待裁冲突

### Requirement: 域验收契约随归档人工转正

归档工具只合并一级规格的 delta，域规格 delta SHALL 在归档前人工转正为对应主规格：转正 SHALL 按原字节搬运并去掉 delta 头，MUST NOT 把 `## ADDED Requirements` 一类节头直写主规格。转正后的主规格 SHALL 通过规格结构校验与同名门禁；结构增长 SHALL 走具名登记，MUST NOT 以放宽门禁换绿。归档 MUST NOT 使用跳过规格同步或跳过校验的开关。

#### Scenario: 归档只合一级 delta

- **WHEN** 一级变更归档而其域规格 delta 尚未转正
- **THEN** 主规格 SHALL 判为缺条，归档 SHALL 停并补转正
- **AND** 归档目录冻结后，转正凭据只能记在该归档的提交或说明里

#### Scenario: 主规格首次进入扫描集

- **WHEN** 某域规格首次转正为某一主规格
- **THEN** 归档提交前 SHALL 预跑规格结构、规格卫生与文档链接门禁
- **AND** 任一门禁红时 MUST NOT 归档

### Requirement: 叶级派发原子按证据解锁

派发原子 SHALL 是就绪叶集合；波次只用于汇报分组，解锁 SHALL 以依赖叶的证据落地为准，MUST NOT 以整域收口或日历波次为闸。同树并发 SHALL 先核触碰集：写面相交，或一叶写入另一叶验证读取的包时，两单 SHALL 合单、串行或隔离工作树，合入后重新跑集成验证。

#### Scenario: 依赖叶证据齐而兄弟叶未完成

- **WHEN** 某叶的直接前置已交出可用证据，而同域其它叶仍未完成
- **THEN** 该叶 SHALL 进入就绪集并补派，不等待整域收口

#### Scenario: 触碰集冲突

- **WHEN** 两单写不同文件，但其中一单写入另一单测试所编译的包
- **THEN** 两单 SHALL 串行或合单，合入后重新跑集成验证，不得以追加兼容放行

### Requirement: 口径冲突停止上报

同一符号、阈值或结论在两处口径不一致时，执行者 SHALL 停止并列出冲突清单（各处原文、所属文件、影响面），MUST NOT 就地改一侧使其自洽。口径统一 SHALL 由唯一家定义，其余处只引用。

#### Scenario: 证据口径与实现不一致

- **WHEN** 证据或设计文档把某观测量写成历史累计，而实现与判据要求当前区间状态
- **THEN** 该冲突 SHALL 上报待裁，先冻结判据口径再回写证据
- **AND** MUST NOT 以改实现迁就旧证据，也 MUST NOT 以改证据迁就实现而不改判据

### Requirement: 收尾凭据先于归档冻结

收尾叶的凭据 SHALL 在归档之前取齐并勾选：锚名、可复跑命令与原始输出、真实 CI 终态。归档目录 SHALL 视为冻结，归档后只附勘误不改写；归档后才出现的红 SHALL 以新提交或新变更承接。

#### Scenario: 归档后补勾

- **WHEN** 收尾叶在归档之后才被勾选
- **THEN** 该勾选 SHALL 判为无效凭据位置，须改记提交或新变更
- **AND** 归档目录内的文件 MUST NOT 被回写
