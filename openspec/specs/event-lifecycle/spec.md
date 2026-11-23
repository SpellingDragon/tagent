# event-lifecycle Specification

## Purpose

> **superseded（2026-11-21 契约同步核验，10.R5b）**：本规范描述的**事件生命周期扫描整体退役**——TTL 过期扫描、容量淘汰、墓碑标记，以及承载三者的生命周期管理器与声明式策略配置，在 Go 树**零定义、零引用、零构造点**（`LifecycleManager`／`NewLifecycleManager`／`DefaultLifecycleConfig`／`LifecycleConfig`／`checkTTL`／`evictOldest`／`IsTombstone`／`MarkTombstone`／`TombstoneSet`／`PartitionState` 全符号零命中，承载它们的实现文件已不在树），memory 配置面亦无对应字段承载 `global_ttl_days`／`type_ttl`／`check_interval`／`max_events_per_partition`，故「声明式注入」不是部分活条款而是随机制一并退役。
>
> **现行判据是「只拒销毁、不做运行期销毁」**：任何路径 MUST NOT 物理销毁已提交事实，已提交事实按身份恒可读；原文过期由原文仓按召回窗口判定，窗口外读取具名拒绝；未结义务的结算事实由**单属主 pin** 留在热窗内，重启由既有 checkpoint 重建。这两条保护语义的现行权威要求由 `storage-lifecycle-completion`「保留保护与回收协调」与 `event-segment-store`「未确认恢复材料的单属主 pin」**唯一**承载，本文件不再另立、MUST NOT 被读作第二来源。
>
> **勘误（原交叉引用失效）**：原「生命周期策略经声明式配置注入」条款以 `event-segment-store`「未确认恢复材料的**有限保留租约**」与 `storage-lifecycle-completion`「保留保护与回收协调」双名交叉引用；前者标题已随死租约改写为「未确认恢复材料的**单属主 pin**」，后者仍在位。本文件只作历史记录，不再承载现行规范命令。

## Requirements

### Requirement: 历史条款已退役

本文件保留已退役的事件生命周期扫描历史记录（原「Time-based TTL expiration」「Capacity-based eviction」「生命周期策略经声明式配置注入」「TTL 扫描为周期性全量扫描」「容量淘汰基于进程内事件计数与最旧段优先」五条 Requirements）。原条款中的规范化关键词（SHALLs/MUSTs）已全部迁出或退役：其唯一仍在位的实质承诺——任何路径不得物理销毁未确认恢复所需的原文、原文不可按身份读到时须具名拒绝——已由 `storage-lifecycle-completion` 的「保留保护与回收协调」与 `event-segment-store` 的「未确认恢复材料的单属主 pin」唯一承载。本文件不再承载现行规范命令，也不作该承诺的第二来源，更不得以「先标墓碑、压实再删」的形态复活已退役的销毁路径。

#### Scenario: 查现行的原文保护与保留要求

- **WHEN** 需要查询已提交事实不得被销毁、原文过期如何判定、未结义务的结算事实如何留在热窗内
- **THEN** 读 `storage-lifecycle-completion` 与 `event-segment-store` 的现行条款，而不是本历史文件

#### Scenario: 已退役的扫描与淘汰不得复活

- **WHEN** 审查事件存储的任何后台动作（裁剪、折叠、压缩、装配、开档）
- **THEN** 不存在以 TTL 到期、容量超限或墓碑清理为条件的物理销毁调用点
- **AND** 不得以「在册未接线」形态重新引入生命周期管理器、墓碑集或容量淘汰器
