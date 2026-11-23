## ADDED Requirements

### Requirement: 热卷字节由保留窗定界

热卷的物理字节 SHALL 有界于保留窗内工作量：`hot_bytes ≤ f(window_workload) + B_overhead`。超出窗的事实 SHALL 迁出热卷（归档），MUST NOT 留在热卷内；稳态运行多代后热字节 SHALL 落到平台（跨代 ± 常数），MUST NOT 随历史总量线性增长。归档卷字节的增长 SHALL 显式声明，MUST NOT 充当热卷有界判据。

#### Scenario: 多代写入后的热盘平台

- **WHEN** 写入 N≥3 代且总量超过保留窗
- **THEN** 每代收尾的热卷字节 SHALL 落在同一平台带内
- **AND** 归档卷字节 SHALL 按历史量增长并被单独记录

#### Scenario: 未配置归档时保持现状

- **WHEN** 部署未声明 `archive_root`
- **THEN** 行为 SHALL 与今日一致（一个未封段），MUST NOT 自动迁移或删除任何字节

### Requirement: 归档事实按身份恒可定位读取

迁入归档的事实 SHALL 保持原身份、原字节：按身份读 SHALL 经“热索引→归档定位索引→段”返回与提交时逐字节相等的正文与原提交序。归档存在但不可读 SHALL 具名拒绝（`ErrArchiveMissing`/`ErrArchiveCorrupt`），MUST NOT 答“不存在”，MUST NOT 静默丢召回。归档介质丢失 SHALL 被登记为运维事故语义，而非事实不存在。

#### Scenario: 老键跨档逐字节相等

- **WHEN** 第 1 代事实已归档、当前处于第 N 代
- **THEN** 按身份读该事实 SHALL 返回原字节与原索引序
- **AND** `recall_get` 经同一路径 SHALL 取回同一正文

#### Scenario: 归档损坏

- **WHEN** 归档段或 manifest 损坏/缺失
- **THEN** 读取 SHALL 具名拒绝
- **AND** MUST NOT 返回空集或“不存在”

### Requirement: 搬迁以 manifest 为单一权威且崩溃可恢复

段搬迁 SHALL 经 copy→fsync→manifest 原子提交→删热段→收缩热索引 的固定序；manifest SHALL 是“哪些段在归档、键域与摘要”的唯一权威。任一切点中断后重开 SHALL 与中断前一致：无半段、无丢段、无热段与 manifest 互相矛盾。热段删除 MUST NOT 先于 manifest 提交。

#### Scenario: 搬迁中途崩溃

- **WHEN** 任意一步后停止并重开
- **THEN** 每一条已提交事实 SHALL 仍可由热段或归档唯一读到
- **AND** 重复搬迁 SHALL 幂等

### Requirement: 热索引随归档收缩

归档完成后，热卷的派生索引（槽表/order 索引/定位表）SHALL 收缩到热段与归档定位指针所需，MUST NOT 随归档键总数增长。归档定位索引 SHALL 位于归档卷，键→段映射查询的成本 SHALL 显式声明。

#### Scenario: 归档后热索引有界

- **WHEN** 大量历史键已归档
- **THEN** 热索引条目数 SHALL 有界于热段键数加段级定位条目
- **AND** 归档定位索引的字节 SHALL 计入归档卷

### Requirement: 旧机制不下线与回退零 diff

本变更 SHALL 与现状并存：无归档配置时行为与今日一致；关闭归档或回退构建后，热卷仍是一个未封段可服务。MUST NOT 删除或改写已提交字节以达成有界；搬迁只移动、不删除历史（热段删除意味着字节已在归档可读）。

#### Scenario: 关闭归档回退

- **WHEN** 移除 `archive_root` 并用回退构建启动同一根
- **THEN** 已归档段 SHALL 仍按身份可读（经归档契约）
- **AND** 行为差异 SHALL 仅为新提交不再轮转

### Requirement: 归档增长与读取成本如实声明

文档与验收 SHALL 分别声明：热卷字节（有界）、归档卷字节（随历史线性）、定位读延迟（跨档 vs 直读）。MUST NOT 以任一项绿替代另一项，也 MUST NOT 把归档卷的增长说成热卷达标。

#### Scenario: 验收口径分离

- **WHEN** 汇报有界达成
- **THEN** SHALL 同时给出热卷平台读数与归档增长读数
- **AND** 跨档读延迟 SHALL 单列
