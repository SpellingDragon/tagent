# compaction-scheduler 规格（delta，MODIFIED：勘误）

> 头逐字匹配主 spec `openspec/specs/compaction-scheduler/spec.md` 的「### Requirement: 历史条款已退役」。本 delta 只追加一条勘误：原勘误里「现行热层不搬迁、字节随历史增长」对现行行为的断言已被 bounded-hot-log 的段搬迁与归档实现推翻；退役事实本身（分层段压实调度器已退役）不改写，也不复活任何退位机制。

## MODIFIED Requirements

### Requirement: 历史条款已退役

本文件保留已退役的分层段压实调度历史记录。原条款中的规范化关键词（SHALLs/MUSTs）已全部迁出或退役；现行要求见 `task-skeleton-compression` 的三龄段折叠条款与 `session-checkpoint-material` 的有界恢复条款，公共行为锚见折叠相关的现行测试（勘误：原文并列的「退位相关锚」随退位机制删除一并消失，见上段勘误）。

> **勘误（bounded-hot-log 落地）**：上段勘误中「现行热层不搬迁，日志只追加，热层字节随历史增长」对现行行为的断言已被推翻——现行实现按 cut 封段，把段界不晚于倒数第二个 cut 的段搬入归档卷并收缩热索引；在 `hot-log-bounds` [C1] 全部条件成立时，热卷字节有界于保留窗，归档卷增长显式声明，归档事实按身份可定位读取。该形态不得被读作「退位」复活：搬迁以 manifest 为唯一权威、只按段元数据选段、不按内容找锚，也不物理销毁已提交事实。热层厚度与开档读量的分记见 `bounded-recovery` 的 boot 四真量条款。

#### Scenario: 查现行折叠与有界恢复要求

- **WHEN** 需要查询时间线如何收敛、热层如何保持有界
- **THEN** 读 `task-skeleton-compression` 与 `session-checkpoint-material`，而不是本历史文件
- **AND** 物理层的热卷有界与归档可读应读 `hot-log-bounds` 与 `log-archival` 两个能力，不读本文件的退役条款
