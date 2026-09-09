# event-compaction Specification

## Purpose

本规范定义 event-compaction 能力。The system SHALL provide a `TaskSegmenter` utility in `agent/task_segmenter.go` that exposes two functions: `SegmentMessages(messages []model.Message) []*TaskSe

> **状态核验（2026-09-09，部分落地/部分退役）**：Req1 的 `SegmentMessages` 已落地（`agent/compress/task_segmenter.go`，以 agent_output 闭合回合——边界语义后经 task-skeleton-compression 演进，见该 spec）；`TaskSegmenter` 类型名与 `SegmentReferences` 函数未实现——refs 分组由压缩管线内部承载，无需独立工具。Req2/Req3 的 `UseFrameworkFlow` 双路径开关**已退役**——legacy `Preprocessor.Process` 路径已移除，SmartCompressor 与 Compactor 均由 BeforeModel 回调单路径驱动（现状即 Req2/Req3 的 framework-flow 分支，无开关）。双路径相关 SHALL 条款不约束当前实现。

## Requirements

### Requirement: TaskSegmenter provides unified task boundary segmentation

The system SHALL provide a `TaskSegmenter` utility in `agent/task_segmenter.go` that exposes two functions: `SegmentMessages(messages []model.Message) []*TaskSegment` and `SegmentReferences(refs []memory.EventReference) [][]memory.EventReference`. Both functions SHALL use the same `isTaskBoundary` predicate to determine task boundaries. A task boundary SHALL be detected when:

- For messages: `msg.Role == RoleAssistant && len(msg.ToolCalls) == 0` (assistant final response).
- For references: `ref.EventType == "agent_output"` or `ref.EventType == "" && ref.Role == "assistant"` (fallback for references without explicit event type).

`SmartCompressor.splitByTaskBoundary` SHALL be replaced by `TaskSegmenter.SegmentMessages`. `Compactor.splitTasks` SHALL be replaced by `TaskSegmenter.SegmentReferences`. The old functions SHALL be removed or delegated to the new utility.

#### Scenario: SegmentMessages produces same results as old splitByTaskBoundary

- **WHEN** `SegmentMessages` is called with the same input that `splitByTaskBoundary` previously processed
- **THEN** the resulting segments SHALL be identical in count, message grouping, and `IsComplete` flags

#### Scenario: SegmentReferences produces same results as old splitTasks

- **WHEN** `SegmentReferences` is called with the same input that `splitTasks` previously processed
- **THEN** the resulting task groups SHALL be identical in count and reference grouping

#### Scenario: Consistent boundary detection across messages and references

- **WHEN** a conversation has 3 tasks (each ending with agent_output)
- **THEN** `SegmentMessages` SHALL produce 3 segments
- **AND** `SegmentReferences` SHALL produce 3 task groups
- **AND** the boundary positions SHALL correspond to the same logical task boundaries

### Requirement: Compactor triggers from BeforeModel callback instead of Preprocessor.Process

When `UseFrameworkFlow` is enabled, `Compactor.Compact` SHALL be invoked from a `BeforeModel` callback registered on the framework `LLMAgent`, NOT from inline logic in `Preprocessor.Process`. The callback SHALL execute after the `SmartCompressor` `BeforeModel` callback. When `UseFrameworkFlow` is disabled (legacy path), `Compactor` SHALL continue to be invoked from `Preprocessor.Process` as in the current implementation.

#### Scenario: Framework flow path triggers Compact from BeforeModel

- **WHEN** `UseFrameworkFlow == true` and SmartCompressor cannot bring tokens below `maxTokens`
- **THEN** the Compactor `BeforeModel` callback invokes `Compactor.Compact` on `SessionProjection`
- **AND** `Preprocessor.Process` does NOT contain inline Compact logic

#### Scenario: Legacy path triggers Compact from Preprocessor.Process

- **WHEN** `UseFrameworkFlow == false`
- **THEN** `Preprocessor.Process` SHALL continue to invoke `Compactor.Compact` inline after SmartCompress
- **AND** no `BeforeModel` callback for Compactor is registered

### Requirement: SmartCompressor triggers from BeforeModel callback instead of Preprocessor.Process

When `UseFrameworkFlow` is enabled, `SmartCompressor.Compress` SHALL be invoked from a `BeforeModel` callback registered on the framework `LLMAgent`, NOT from inline logic in `Preprocessor.Process`. The callback SHALL compute token estimate, invoke Compress if above threshold, and restore `KeepRecentTasks` after compression. When `UseFrameworkFlow` is disabled, `SmartCompressor` SHALL continue to be invoked from `Preprocessor.Process`.

#### Scenario: Framework flow path triggers SmartCompress from BeforeModel

- **WHEN** `UseFrameworkFlow == true` and the framework constructs messages exceeding the token threshold
- **THEN** the SmartCompressor `BeforeModel` callback invokes `Compress` on `args.Request.Messages`
- **AND** `Preprocessor.Process` does NOT contain inline SmartCompress logic

#### Scenario: Legacy path triggers SmartCompress from Preprocessor.Process

- **WHEN** `UseFrameworkFlow == false`
- **THEN** `Preprocessor.Process` SHALL continue to invoke `SmartCompressor.Compress` inline
- **AND** no `BeforeModel` callback for SmartCompressor is registered
