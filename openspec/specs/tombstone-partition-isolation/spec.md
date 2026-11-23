# tombstone-partition-isolation Specification

## Purpose

> **superseded（2026-11-21 契约同步核验，10.R5e）**：本规范描述的**按分区墓碑隔离整体退役**——按分区惰性实例化墓碑集、把墓碑键写在所属分区前缀下、初始化时从键值仓自动恢复墓碑状态，在 Go 树**零定义、零引用、零构造点**（`TombstoneSet`／`MarkTombstone`／`IsTombstone`／`getTombstoneSet`／`RecoverFromKV`／`PartitionState` 全符号零命中，`{pid}:tomb:{event_key}` 键前缀既无写入方也无读方），承载它们的实现文件已不在树，故「每分区一份墓碑集」不是部分活条款，而是随机制一并退役。
>
> **现行判据是「只拒销毁、不做运行期销毁」**：任何路径 MUST NOT 物理销毁已提交事实，已提交事实按身份恒可读；原文过期由原文仓按召回窗口判定，窗口外读取具名拒绝。分区读取的现行隔离承诺由 `event-segment-store` 的「resolvePartitions 默认隔离（无分区参数返回空）」与「后端不可变与隔离一致性」唯一承载；未结义务的结算事实留在热窗内由该文件「未确认恢复材料的单属主 pin」承载。本文件不再另立、MUST NOT 被读作这些承诺的第二来源。
>
> **同名异义项排除**：日志帧记录里的 tombstone 字段是**键删除记录**（回答「该键是否仍被持有」），不是事件级墓碑，也不构成重放拒绝机制；`generation retirement`／`Retention.Retire`／`headless-retired` 与术语表中的岗位退位义项分属代际、交付终态与人事隐喻，均不在本规范射程内，MUST NOT 据其命中判本能力仍有活条款。

## Requirements

### Requirement: 历史条款已退役

本文件保留已退役的按分区墓碑隔离历史记录（原「TombstoneSet is per-partition」与「TombstoneSet auto-recovers from KV on initialization」两条 Requirements）。原条款中的规范化关键词（SHALLs/MUSTs）已全部迁出或退役：其唯一的实质承诺——一个分区的墓碑不得影响其他分区、重启后墓碑状态可恢复——随墓碑机制整体退役而失去对象，因为现行路径既不写墓碑也无墓碑过滤；分区读取的现行隔离承诺由 `event-segment-store` 的「resolvePartitions 默认隔离」与「后端不可变与隔离一致性」唯一承载。本文件不再承载现行规范命令，也不作该隔离承诺的第二来源，更不得以「按分区持有一份墓碑集」或「在册未接线」的形态复活已退役的墓碑机制。

#### Scenario: 查现行的分区读取隔离要求

- **WHEN** 需要查询跨分区读取的隔离边界、缺少分区参数时如何答复
- **THEN** 读 `event-segment-store` 的现行条款，而不是本历史文件

#### Scenario: 已退役的墓碑登记不得复活

- **WHEN** 审查事件存储的任何写路径、裁剪、折叠或装配
- **THEN** 不存在按分区维护的墓碑登记、墓碑键前缀写入或墓碑过滤调用点
- **AND** 不得以「在册未接线」形态重新引入墓碑集或按分区的墓碑恢复
