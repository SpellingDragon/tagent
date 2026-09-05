## Why

tagent 事件存储架构有 4 个未完成的 change，彼此存在依赖关系且全部围绕存储/内存领域：

1. **harden-event-storage-for-scale** (80 tasks, 0 完成) — FileSegmentStore 生产硬化：RustViking KV range/batch 改进、active partitions bitmap、批量写入 seq 碰撞修复、tombstone 分区隔离、TTL 游标、relation store 重写。经四轮深度评审暴露 24 个工作项，当前代码在生产规模（10 events/s × 3 年 = 1B+ 事件）下不可用。
2. **llm-event-summary** (19 tasks, 0 完成) — LLM 驱动的 L2→L3 摘要生成器，替换 PassthroughSummarizer。显式依赖 harden-event-storage-for-scale 合并后上线观察。
3. **event-storage-layered-architecture** (6 tasks 未完成 / 68 总) — Phase 9 迁移：FileBackend → FileSegmentStore 双写切换、历史数据迁移工具、旧代码清理。依赖 harden 的核心修复完成。
4. **thinking-plan-event-separation** (1 task 未完成 / 18 总) — 文档补全：memory-architecture.md 中 Snowflake int64 格式说明。

这 4 个 change 分散管理导致依赖追踪困难、优先级不清晰。整合为单一 change 后：
- 依赖链清晰：harden (基础) → migration (切换) → llm-summary (增强) + doc-fix (收尾)
- 避免跨 change 追踪 106 个任务的状态
- 统一验收标准：生产级存储硬化 + 迁移 + LLM 摘要

## What Changes

整合 4 个 change 的 106 个未完成任务为 5 个 Phase：

### Phase 1: RustViking KV 改进 (5 tasks)
- RustViking CLI 新增 `kv range` 子命令
- 修复 `kv batch` 原子性
- JSON 响应契约稳定
- 基准测试 + v0.2.0 发布

### Phase 2: FileSegmentStore 硬化 (35 tasks)
- Key schema + active partitions bitmap
- Init 从 bitmap + cursor 恢复（替代全扫 meta）
- StoreEvents 批量写入 seq 碰撞修复
- Tombstone 分区隔离 + 懒初始化
- TTL 游标 + evictOldest 改造
- Compaction 调度器 + 墓碑过滤 + 悬垂引用修复
- RelationStore 重写（LRU + WAL + range scan）

### Phase 3: FileBackend → FileSegmentStore 迁移 (6 tasks)
- 双写模式 → 读取切换 → 迁移工具 → 旧代码清理

### Phase 4: LLM 事件摘要 (19 tasks)
- LLMSummarizer 实现 + 批处理 + 降级 + 配置 + 测试

### Phase 5: 文档收尾 (1 task)
- memory-architecture.md Snowflake int64 格式说明

### Phase 6: 端到端验证 (40 tasks)
- 单元测试 + 集成测试 + 全量 go build/vet/test

## Capabilities

### New Capabilities

- `kv-range-scan`: RustViking KV range 子命令 + tagent 端 KVRange CLI 调用
- `active-partitions-bitmap`: 全局活跃分区 bitmap + cursor 恢复机制
- `per-partition-tombstone`: 分区隔离 tombstone + 懒初始化恢复
- `ttl-cursor-scan`: TTL 前进游标 + evictOldest window 扫描
- `llm-event-summarization`: LLM 驱动的 L2→L3 事件摘要生成

### Modified Capabilities

- `event-storage-migration`: FileBackend → FileSegmentStore 双写迁移 + 清理

## Impact

- `memory/key_schema.go`: 新增 key format helpers
- `memory/file_segment_store.go`: Init/StoreEvents/Tombstone/TTL/Compaction 全面硬化
- `memory/relation_store.go`: 重写为 LRU + WAL + range scan
- `memory/compaction.go`: 调度器 + 墓碑过滤 + 悬垂引用修复
- `memory/summarizer.go`: 新增 LLMSummarizer
- `memory/file_backend.go`: 迁移后删除
- `cmd/migrate-events/`: 新增迁移工具
- `docs/wiki/memory/memory-architecture.md`: Snowflake int64 格式说明
- RustViking 仓库: kv range/batch 改进 + v0.2.0 发布

## Consolidation Source

本 change 整合自以下 4 个已归档 change 的未完成任务：
- `harden-event-storage-for-scale` — Phase 1-2 + Phase 6 前 40 tasks
- `llm-event-summary` — Phase 4
- `event-storage-layered-architecture` — Phase 3 (Phase 9 迁移)
- `thinking-plan-event-separation` — Phase 5
