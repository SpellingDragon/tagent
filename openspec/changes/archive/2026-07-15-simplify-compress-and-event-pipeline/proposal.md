## Why

tagent 的压缩算法和事件管线经过 38 次迭代积累了不合理的复杂性：SmartCompressor (1383 行) 中包含 LLM 价值评估 (event_value.go, 396 行)、语义分块 (chunk_splitter.go, 354 行)、多层降级逻辑等组件，但实际价值评估可以用确定性规则（类型 + 年龄）替代 LLM 调用（当前超时 5 分钟）。事件管线在 Projection-first 重构后虽然消除了 content-based dedup，但 ContextCompressor + SmartCompressor 的两层调用关系仍有冗余，且缺少"主动整理"能力——压缩仅在 BeforeModel 超限时被动触发，无法在空闲期从容整理投影。

本变更的目标：做减法，在保持"信息不丢、降级存储可召回"前提下，精简压缩算法至可外包实现的清晰度，并引入空闲期主动整理机制使长程运行 Agent 的上下文始终健康。

## What Changes

### 压缩算法简化

- **删除 LLM 价值评估**：移除 `event_value.go` (396 行) 和 `ValuationConfig`，替换为确定性规则函数 `deterministicLevel(seg, age, keepRecent) → L0/L1/L2/L3`，基于事件类型 + 段年龄即时确定压缩级别
- **删除语义分块器**：移除 `chunk_splitter.go` (354 行)，大消息的截断应在工具返回时处理（源头控制），不在压缩层做
- **简化 SmartCompressor 主逻辑**：保留 L0（保留）、L1（选择性保留用户消息）、L2（LLM 摘要替代执行过程）、L3（全段归档 + 内联摘要）的四级策略，但移除价值评估阶段和语义分块阶段
- **保留归档机制**：L3 压缩时仍然 archiveSegment 到 MemoryStore，确保"丢弃的信息可召回"

### 事件管线主动整理

- **新增 `organizeProjection` 机制**：在 `runEventLoop` 的 Pull 间隙（空闲期），对 Projection 中年龄较大的 refs 执行主动整理——用 LLM 生成精炼摘要更新 EventSummary，使下次 Compress 时不触发紧急压缩
- **空闲检测规则**：连续 2 次 Pull 返回空 batch（EventBus 无新事件）时触发一轮整理
- **整理与压缩的协作**：整理做得好 → SmartCompressor 很少触发 → LLM 视图始终健康有序

### 可观测性保留

- **压缩指标日志**：保留 `[SmartCompress]` 结构化 JSON 日志（压缩前后 token、各级别段数、耗时）
- **整理指标日志**：新增 `[OrganizeProjection]` 日志（整理了多少 refs、生成了多少摘要、耗时）
- **TrajectoryRecorder 不受影响**：独立于压缩逻辑，继续全保真记录

## Capabilities

### New Capabilities

- `deterministic-compress-level`: 确定性压缩分级规则，基于事件类型和段年龄即时决定 L0/L1/L2/L3，替代 LLM 价值评估
- `projection-organize`: 空闲期主动投影整理机制，在 runEventLoop 间隙用 LLM 为老旧 refs 生成精炼摘要

### Modified Capabilities

- `value-driven-compression`: 移除 LLM 评估阶段，改用确定性规则；保留四级压缩策略和归档机制

## Impact

### 代码变更

- **删除文件**：`agent/event_value.go` (396 行)、`agent/chunk_splitter.go` (354 行)
- **重写文件**：`agent/smart_compress.go` (1383 行 → 预计 ~600 行)
- **修改文件**：`agent/context_compressor.go`（简化与 SmartCompressor 的接口）、`agent/tagent_agent.go`（runEventLoop 增加空闲整理分支）、`agent/context_manager.go`（移除 ValuationConfig 相关配置）
- **新增文件**：`agent/projection_organizer.go`（空闲期整理逻辑，预计 ~150 行）
- **配置变更**：`config.go` 移除 `ValuationTimeoutMs`、`ValueFloors` 配置项；`CompressConfig` 简化

### 测试

- 更新 `agent/smart_compress_test.go`（移除 valuation mock、chunk_splitter 测试）
- 新增 `agent/projection_organizer_test.go`
- 确保 `tests/invariants_test.go` 继续通过

### 向后兼容

- **BREAKING**：`CompressConfig.ValueFloors` 和 `CompressConfig.ValuationTimeoutMs` 字段删除。已配置这些字段的 YAML 需要移除（否则解析报错）
- 压缩行为变化：确定性规则可能与 LLM 评估结果不同，但由于 LLM 评估本身就不稳定（超时、降级），实际行为差异可忽略
