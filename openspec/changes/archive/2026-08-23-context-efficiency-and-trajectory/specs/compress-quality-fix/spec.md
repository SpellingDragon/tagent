# compress-quality-fix Delta

> 仅移除依赖已删 legacy 摘要管线的两个需求；本规格中关于 `resolveReferenceToMessage` /
> `BuildEventReference` / `findPendingUserMessage` 的投影解析需求描述现役代码，**保留不变**。

## REMOVED Requirements

### Requirement: generateSummary splits and re-summarizes when result exceeds target

**Reason**: `generateSummary`（含超目标 1.5 倍时的二分重摘逻辑）属已删除的 legacy 压缩管线（`generateSummaryRecursive`）。context-efficiency-and-trajectory 移除 legacy 管线后该函数不复存在。

**Migration**: 无需迁移。骨架管线无分段 LLM 摘要；唯一的 LLM 文摘是卡片浓缩 `condenseCardLines`，其输出经单行化清洗后并入卡片序列。

### Requirement: summarizeBatch truncates oversized LLM summary

**Reason**: `summarizeBatch` 属已删除的 legacy 批量摘要路径。

**Migration**: 无。骨架管线无批量摘要调用。
