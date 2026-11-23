# architecture-review-standards Specification

> **同步注记（2026-11-21 契约同步核验，10.R5e）**：原十一条审查要求的主体对象按已退役架构表述，其点名的符号（`TagentAgent`／`AgentToolWrapper`／`EventBus`／`SessionProjection`／`PlainTool`／`ToolRegistry`／`RegisterBuiltinTools`／`SwappableModel`／`MemoryPlugin`／`SummaryPlugin`／`CommandTool`／`TmuxMonitor`／`InMemRelationStore`／`InMemoryStore`／`RelationStoreProvider`／`TombstoneSet`／`LifecycleManager`／`MockRustVikingClient`／`findAliveAncestor`／`ExtractEventType`／`IsSpecialEventType`／`ReadPartitionIDs`／`resolveMemoryStore` 与生产入口 `tagent.go`）在全仓 Go 树 0 命中，点名的源文件多数已不在树。十条据此退役，逐条现行承载如下（本文件不重复立，MUST NOT 被读作第二来源）：①事件驱动一致性 → `thinking-plan-event-classification` 与 `event-type-unification` 承载角色到事件类型的分类与常量单源，`event-segment-store` 承载原文全文按字节一致；②内存中心一致性 → `event-segment-store` 的不可变事实与计数条款；③压缩判定与执行分层 → `compression-pipeline` 的三段落与恢复不重折条款；④因果关系完整性 → `causal-chain-isolation`（关系独立维护、事件不含父键）；⑤上下文隔离完整性 → `event-segment-store` 的「resolvePartitions 默认隔离」；⑥段存储与生命周期一致性 → `storage-lifecycle-completion`（只拒销毁）与 `event-segment-store` 的单属主 pin；⑧代码一致性 → `code-documentation` 与 `error-handling-consistency`；⑨接口契约 → `event-segment-store` 的存储契约与 `production-wiring` 的开档派生；⑩生产接线完整性 → `production-wiring`；⑪工具执行安全 → 执行回执完整性与资源释放由 `exec-integrity`、`execution-resource-lifecycle` 承载，其执行用户隔离分句的对象（专用用户包装与相应会话选项传递）在全树 0 命中，本文件不把该分句追认为现行承诺。⑦并发安全无替代承载，仍在下方原位。

## Purpose

本规范定义 architecture-review-standards 能力：审查者 SHALL 检查并发安全——任何被多个 goroutine 访问的共享状态（map、结构体字段、句柄表）是否由互斥或原子操作保护，MUST NOT 以「事实上只有一个写者」或「很少并发」的观察替代同步。

## Requirements

### Requirement: 审查 SHALL 验证并发安全

审查者 SHALL 检查所有 goroutine 间共享状态访问是否有正确的同步机制：每条读写路径 SHALL 能指出其保护手段（互斥、原子操作，或由结构本身保证的单写者时序），MUST NOT 以运行时观察作为无保护的依据。

#### Scenario: 共享状态同步验证

- **WHEN** 审查者检查任何被多个 goroutine 访问的 map、结构体字段或句柄表
- **THEN** SHALL 验证访问是否有互斥保护或使用原子操作，无数据竞争风险

#### Scenario: 并发面以竞态检测交付

- **WHEN** 变更触及后台消费循环、缓存或存储句柄
- **THEN** 该变更 SHALL 以竞态检测运行相关测试，MUST NOT 以普通单跑通过充当并发证据
